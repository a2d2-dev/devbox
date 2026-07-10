// diskio.go — iotop 式磁盘 I/O 采集。
//
// 两层数据：
//   1. 设备级（物理磁盘为主）：/proc/diskstats 差分 → 每盘读/写速率、IOPS、util%
//      只取 /sys/block 下的整盘设备（sda / nvme0n1 / dm-* / md*），排除 loop* / ram*，
//      分区不单独列（避免与整盘双重计数）；带 physical 标记区分物理盘和映射设备。
//   2. 进程级（iotop 核心）：/proc/<pid>/io 的 read_bytes / write_bytes 差分
//      → Top 进程按实际磁盘读写速率排序（与 iotop 一致，统计真正落盘的 I/O，
//      不是 rchar/wchar 的缓存读写）。读 /proc/<pid>/io 需要 root。
//
// 速率计算需要两个采样点：包级缓存上一次快照，请求间差分（前端 3s 轮询正好构成
// 采样窗口）；首次请求或快照过期（>15s）时同步做一次 500ms 双采样，保证第一屏
// 就有真实速率而不是一排 0。
package system

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// ─── 对外类型 ───────────────────────────────────────────────

type DiskIO struct {
	// SampleMs 本次速率的实际采样窗口（差分间隔），前端可展示"过去 Ns 均值"
	SampleMs  int64       `json:"sampleMs"`
	Devices   []DeviceIO  `json:"devices"`
	Processes []ProcessIO `json:"processes"`
	// ProcessesAvailable=false 表示 /proc/<pid>/io 不可读（非 root 运行），
	// 前端应提示而不是显示空表
	ProcessesAvailable bool `json:"processesAvailable"`
}

type DeviceIO struct {
	Device     string  `json:"device"`     // sda / nvme0n1 / dm-0
	Alias      string  `json:"alias,omitempty"` // dm-* 的 LVM 友好名（如 ubuntu--vg-ubuntu--lv）
	Physical   bool    `json:"physical"`   // true=物理盘（/sys/block/<dev>/device 存在），false=dm/md 等映射设备
	Rotational bool    `json:"rotational"`
	ReadBps    float64 `json:"readBps"`
	WriteBps   float64 `json:"writeBps"`
	ReadIops   float64 `json:"readIops"`
	WriteIops  float64 `json:"writeIops"`
	UtilPct    float64 `json:"utilPct"` // iostat -x 的 %util：采样窗口内设备忙的时间占比
}

type ProcessIO struct {
	PID      int     `json:"pid"`
	Name     string  `json:"name"`
	User     string  `json:"user,omitempty"`
	Cmdline  string  `json:"cmdline,omitempty"`
	ReadBps  float64 `json:"readBps"`
	WriteBps float64 `json:"writeBps"`
}

// ─── 采样缓存 ───────────────────────────────────────────────

type diskCounters struct {
	readIOs, readSectors, writeIOs, writeSectors, ioMs uint64
}

type procCounters struct {
	name       string // pid 复用防护：名字变了则丢弃该进程的差分
	readBytes  uint64
	writeBytes uint64
}

var (
	ioMu       sync.Mutex
	lastDisks  map[string]diskCounters
	lastProcs  map[int]procCounters
	lastSample time.Time
)

// staleAfter 超过该时长的旧快照作废（长时间没人看页面，差分窗口过大没有意义）
const staleAfter = 15 * time.Second

// GetDiskIO 返回设备级 + 进程级磁盘 I/O 速率。
func GetDiskIO(ctx context.Context) (*DiskIO, error) {
	ioMu.Lock()
	defer ioMu.Unlock()

	now := time.Now()
	if lastDisks == nil || now.Sub(lastSample) > staleAfter {
		// 无可用快照：先采一次作基线，短暂等待后走下面的正常差分
		lastDisks = readDiskStats()
		lastProcs, _ = readAllProcIO()
		lastSample = now
		select {
		case <-time.After(500 * time.Millisecond):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		now = time.Now()
	}

	curDisks := readDiskStats()
	curProcs, procOK := readAllProcIO()
	dt := now.Sub(lastSample).Seconds()
	if dt <= 0 {
		dt = 0.001
	}

	res := &DiskIO{
		SampleMs:           int64(dt * 1000),
		ProcessesAvailable: procOK,
	}

	// 设备级差分
	physical, aliases, rotational := readBlockMeta()
	for dev, cur := range curDisks {
		prev, ok := lastDisks[dev]
		if !ok {
			continue
		}
		d := DeviceIO{
			Device:     dev,
			Alias:      aliases[dev],
			Physical:   physical[dev],
			Rotational: rotational[dev],
			ReadBps:    rate(cur.readSectors, prev.readSectors, dt) * 512,
			WriteBps:   rate(cur.writeSectors, prev.writeSectors, dt) * 512,
			ReadIops:   rate(cur.readIOs, prev.readIOs, dt),
			WriteIops:  rate(cur.writeIOs, prev.writeIOs, dt),
		}
		if cur.ioMs >= prev.ioMs {
			d.UtilPct = float64(cur.ioMs-prev.ioMs) / (dt * 1000) * 100
			if d.UtilPct > 100 {
				d.UtilPct = 100
			}
		}
		res.Devices = append(res.Devices, d)
	}
	// 物理盘在前，其余按名称稳定排序
	sortDevices(res.Devices)

	// 进程级差分（iotop）：只保留有活动的，按总速率排序取 Top 20
	if procOK {
		uidToUser := loadUsers()
		for pid, cur := range curProcs {
			prev, ok := lastProcs[pid]
			if !ok || prev.name != cur.name {
				continue // 新进程 / pid 复用：本轮无差分基线
			}
			r := rate(cur.readBytes, prev.readBytes, dt)
			w := rate(cur.writeBytes, prev.writeBytes, dt)
			if r <= 0 && w <= 0 {
				continue
			}
			p := ProcessIO{PID: pid, Name: cur.name, ReadBps: r, WriteBps: w}
			fillProcMeta(&p, uidToUser)
			res.Processes = append(res.Processes, p)
		}
		sortProcesses(res.Processes)
		if len(res.Processes) > 20 {
			res.Processes = res.Processes[:20]
		}
	}

	lastDisks, lastProcs, lastSample = curDisks, curProcs, now
	return res, nil
}

// rate 差分速率，计数器回绕/复位时返回 0 而不是负数
func rate(cur, prev uint64, dt float64) float64 {
	if cur < prev {
		return 0
	}
	return float64(cur-prev) / dt
}

// ─── /proc/diskstats ────────────────────────────────────────

// readDiskStats 只保留 /sys/block 下的整盘设备（分区与整盘双重计数，取整盘）。
func readDiskStats() map[string]diskCounters {
	wholeDisks := listWholeDisks()
	out := map[string]diskCounters{}
	b, err := os.ReadFile("/proc/diskstats")
	if err != nil {
		return out
	}
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		// major minor name reads rmerged rsectors rms writes wmerged wsectors wms inflight ioms ...
		if len(f) < 13 {
			continue
		}
		name := f[2]
		if !wholeDisks[name] {
			continue
		}
		var c diskCounters
		c.readIOs, _ = strconv.ParseUint(f[3], 10, 64)
		c.readSectors, _ = strconv.ParseUint(f[5], 10, 64)
		c.writeIOs, _ = strconv.ParseUint(f[7], 10, 64)
		c.writeSectors, _ = strconv.ParseUint(f[9], 10, 64)
		c.ioMs, _ = strconv.ParseUint(f[12], 10, 64)
		out[name] = c
	}
	return out
}

// listWholeDisks /sys/block 下的顶层块设备，排除 loop/ram/zram 等伪设备
func listWholeDisks() map[string]bool {
	out := map[string]bool{}
	entries, err := os.ReadDir("/sys/block")
	if err != nil {
		return out
	}
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, "loop") || strings.HasPrefix(name, "ram") ||
			strings.HasPrefix(name, "zram") || strings.HasPrefix(name, "fd") {
			continue
		}
		out[name] = true
	}
	return out
}

// readBlockMeta 物理盘标记（有 device 符号链接）+ dm 友好名 + rotational
func readBlockMeta() (physical map[string]bool, aliases map[string]string, rotational map[string]bool) {
	physical = map[string]bool{}
	aliases = map[string]string{}
	rotational = map[string]bool{}
	entries, err := os.ReadDir("/sys/block")
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		if _, err := os.Stat("/sys/block/" + name + "/device"); err == nil {
			physical[name] = true
		}
		if b, err := os.ReadFile("/sys/block/" + name + "/dm/name"); err == nil {
			aliases[name] = strings.TrimSpace(string(b))
		}
		if b, err := os.ReadFile("/sys/block/" + name + "/queue/rotational"); err == nil {
			rotational[name] = strings.TrimSpace(string(b)) == "1"
		}
	}
	return
}

func sortDevices(devs []DeviceIO) {
	// 物理盘优先，其次按名称；不引 sort 包外的依赖，冒泡即可（设备数很小）
	for i := 0; i < len(devs); i++ {
		for j := i + 1; j < len(devs); j++ {
			a, b := devs[i], devs[j]
			if (b.Physical && !a.Physical) || (a.Physical == b.Physical && b.Device < a.Device) {
				devs[i], devs[j] = devs[j], devs[i]
			}
		}
	}
}

// ─── /proc/<pid>/io ─────────────────────────────────────────

// readAllProcIO 遍历所有进程的 io 计数。ok=false 表示一个都读不到
//（通常是非 root），调用方以此区分"没有 I/O"和"没有权限"。
func readAllProcIO() (map[int]procCounters, bool) {
	out := map[int]procCounters{}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return out, false
	}
	readable := false
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		b, err := os.ReadFile(fmt.Sprintf("/proc/%d/io", pid))
		if err != nil {
			continue
		}
		readable = true
		var c procCounters
		for _, line := range strings.Split(string(b), "\n") {
			if v, ok := strings.CutPrefix(line, "read_bytes: "); ok {
				c.readBytes, _ = strconv.ParseUint(strings.TrimSpace(v), 10, 64)
			} else if v, ok := strings.CutPrefix(line, "write_bytes: "); ok {
				c.writeBytes, _ = strconv.ParseUint(strings.TrimSpace(v), 10, 64)
			}
		}
		if nb, err := os.ReadFile(fmt.Sprintf("/proc/%d/comm", pid)); err == nil {
			c.name = strings.TrimSpace(string(nb))
		}
		out[pid] = c
	}
	return out, readable
}

// fillProcMeta 补充 user / cmdline（只对进入 Top 榜的进程调用，避免全量开销）
func fillProcMeta(p *ProcessIO, uidToUser map[int]string) {
	if b, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", p.PID)); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(line, "Uid:") {
				f := strings.Fields(line)
				if len(f) > 1 {
					uid, _ := strconv.Atoi(f[1])
					if u, ok := uidToUser[uid]; ok {
						p.User = u
					} else {
						p.User = f[1]
					}
				}
				break
			}
		}
	}
	if b, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", p.PID)); err == nil {
		s := strings.ReplaceAll(strings.TrimRight(string(b), "\x00"), "\x00", " ")
		if len(s) > 160 {
			s = s[:160] + "…"
		}
		p.Cmdline = s
	}
}

// ─── Prometheus 用原始计数器 ─────────────────────────────────
//
// /metrics 暴露的是累计 counter（rate 由 Prometheus 端 rate() 计算），
// 与上面 API 用的差分速率是同一数据源、两种消费方式。

type DiskCounterSnapshot struct {
	Device     string
	Alias      string
	Physical   bool
	Rotational bool
	ReadIOs    uint64
	ReadBytes  uint64
	WriteIOs   uint64
	WriteBytes uint64
	IOTimeMs   uint64
}

// ReadDiskCounters 返回所有整盘设备的累计 I/O 计数（/proc/diskstats 原值）。
func ReadDiskCounters() []DiskCounterSnapshot {
	physical, aliases, rotational := readBlockMeta()
	stats := readDiskStats()
	var out []DiskCounterSnapshot
	for dev, c := range stats {
		out = append(out, DiskCounterSnapshot{
			Device: dev, Alias: aliases[dev],
			Physical: physical[dev], Rotational: rotational[dev],
			ReadIOs: c.readIOs, ReadBytes: c.readSectors * 512,
			WriteIOs: c.writeIOs, WriteBytes: c.writeSectors * 512,
			IOTimeMs: c.ioMs,
		})
	}
	return out
}

// FilesystemUsage 挂载点容量（statfs），供 /metrics 的 filesystem gauge。
type FilesystemUsage struct {
	Device     string
	MountPoint string
	FSType     string
	SizeBytes  uint64
	UsedBytes  uint64
}

// ListFilesystemUsage 遍历 /proc/mounts 中的真实块设备挂载点做 statfs。
// 只统计 /dev/ 开头的设备，跳过 proc/sysfs/tmpfs 等伪文件系统。
func ListFilesystemUsage() []FilesystemUsage {
	b, err := os.ReadFile("/proc/mounts")
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []FilesystemUsage
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) < 3 || !strings.HasPrefix(f[0], "/dev/") || seen[f[0]] {
			continue
		}
		var st syscall.Statfs_t
		if err := syscall.Statfs(f[1], &st); err != nil {
			continue
		}
		seen[f[0]] = true
		bs := uint64(st.Bsize)
		out = append(out, FilesystemUsage{
			Device: f[0], MountPoint: f[1], FSType: f[2],
			SizeBytes: st.Blocks * bs,
			UsedBytes: (st.Blocks - st.Bfree) * bs,
		})
	}
	return out
}

func sortProcesses(procs []ProcessIO) {
	for i := 0; i < len(procs); i++ {
		for j := i + 1; j < len(procs); j++ {
			if procs[j].ReadBps+procs[j].WriteBps > procs[i].ReadBps+procs[i].WriteBps {
				procs[i], procs[j] = procs[j], procs[i]
			}
		}
	}
}
