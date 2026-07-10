// Package system 提供 Linux 系统查询：进程 / 磁盘 / 网络连接 / GPU 进程。
// 面向 devbox "无 UI 的 Linux 管理工具" 定位，尽量走 /proc 和一次性 shell 命令，
// 不引第三方依赖。
package system

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ─── Processes ──────────────────────────────────────────────

type ProcessBasic struct {
	PID       int    `json:"pid"`
	PPID      int    `json:"ppid"`
	Name      string `json:"name"`
	Cmdline   string `json:"cmdline"`
	User      string `json:"user"`
	State     string `json:"state"`
	Threads   int    `json:"threads"`
	MemBytes  uint64 `json:"memBytes"`
	StartTime string `json:"startTime"`
}

type ProcessDetail struct {
	Basic    ProcessBasic       `json:"basic"`
	Memory   map[string]uint64  `json:"memory,omitempty"`
	Env      map[string]string  `json:"env,omitempty"`
	FDList   []FDEntry          `json:"fdList,omitempty"`
	NetConns []NetworkConn      `json:"netConns,omitempty"`
}

type FDEntry struct {
	FD     string `json:"fd"`
	Target string `json:"target"`
}

var pageSize uint64 = uint64(os.Getpagesize())

// ListProcesses walks /proc/*/stat, /proc/*/status.
// 每个进程做 ~4 次文件读，20 核机器上 200 进程约 20 ms 内完成。
func ListProcesses() ([]ProcessBasic, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	uidToUser := loadUsers()
	bootSec := readBootTime()
	var list []ProcessBasic
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		p, err := readBasic(pid, uidToUser, bootSec)
		if err != nil {
			continue // process died mid-read
		}
		list = append(list, p)
	}
	return list, nil
}

func readBasic(pid int, uidToUser map[int]string, bootSec int64) (ProcessBasic, error) {
	p := ProcessBasic{PID: pid}

	// /proc/<pid>/stat: comm state ppid ... 22:starttime rss (num 23)
	statB, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return p, err
	}
	// parse Comm safely (parenthesized, may contain spaces)
	openIdx := strings.IndexByte(string(statB), '(')
	closeIdx := strings.LastIndexByte(string(statB), ')')
	if openIdx < 0 || closeIdx < 0 || closeIdx <= openIdx {
		return p, fmt.Errorf("malformed stat")
	}
	p.Name = string(statB[openIdx+1 : closeIdx])
	rest := strings.Fields(string(statB[closeIdx+2:]))
	if len(rest) < 22 {
		return p, fmt.Errorf("stat short")
	}
	p.State = rest[0]
	p.PPID, _ = strconv.Atoi(rest[1])
	p.Threads, _ = strconv.Atoi(rest[17])
	startClk, _ := strconv.ParseInt(rest[19], 10, 64)
	// RSS is field 23 (index 21 after Comm), pages
	rssPages, _ := strconv.ParseUint(rest[21], 10, 64)
	p.MemBytes = rssPages * pageSize

	// UID from /proc/<pid>/status
	if b, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid)); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(line, "Uid:") {
				f := strings.Fields(line)
				if len(f) > 1 {
					uid, _ := strconv.Atoi(f[1])
					if u, ok := uidToUser[uid]; ok {
						p.User = u
					} else {
						p.User = strconv.Itoa(uid)
					}
				}
				break
			}
		}
	}

	// cmdline
	if b, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid)); err == nil {
		p.Cmdline = strings.ReplaceAll(strings.TrimRight(string(b), "\x00"), "\x00", " ")
	}

	// start time (bootSec + startClk / hz)
	if bootSec > 0 && startClk > 0 {
		hz := int64(100) // sysconf(_SC_CLK_TCK)—标准值
		startUnix := bootSec + startClk/hz
		p.StartTime = time.Unix(startUnix, 0).Format(time.RFC3339)
	}

	return p, nil
}

func loadUsers() map[int]string {
	m := map[int]string{}
	f, err := os.Open("/etc/passwd")
	if err != nil {
		return m
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Split(sc.Text(), ":")
		if len(fields) >= 3 {
			uid, _ := strconv.Atoi(fields[2])
			m[uid] = fields[0]
		}
	}
	return m
}

func readBootTime() int64 {
	b, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "btime ") {
			t, _ := strconv.ParseInt(strings.TrimPrefix(line, "btime "), 10, 64)
			return t
		}
	}
	return 0
}

// GetProcessDetail 补充内存 / env / fd。
func GetProcessDetail(pid int) (*ProcessDetail, error) {
	uidToUser := loadUsers()
	bootSec := readBootTime()
	basic, err := readBasic(pid, uidToUser, bootSec)
	if err != nil {
		return nil, err
	}
	d := &ProcessDetail{Basic: basic}

	// memory：解析 /proc/<pid>/status VmRSS/VmSize/…
	if b, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid)); err == nil {
		mem := map[string]uint64{}
		for _, line := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(line, "Vm") {
				parts := strings.Fields(line)
				if len(parts) >= 2 {
					k := strings.TrimSuffix(parts[0], ":")
					v, _ := strconv.ParseUint(parts[1], 10, 64)
					mem[k] = v * 1024 // kB → B
				}
			}
		}
		d.Memory = mem
	}

	// env
	if b, err := os.ReadFile(fmt.Sprintf("/proc/%d/environ", pid)); err == nil {
		env := map[string]string{}
		for _, kv := range strings.Split(string(b), "\x00") {
			if idx := strings.IndexByte(kv, '='); idx > 0 {
				env[kv[:idx]] = kv[idx+1:]
			}
		}
		d.Env = env
	}

	// fd (最多 200 个，防超大目录)
	fds, _ := os.ReadDir(fmt.Sprintf("/proc/%d/fd", pid))
	for i, fd := range fds {
		if i >= 200 {
			break
		}
		target, _ := os.Readlink(fmt.Sprintf("/proc/%d/fd/%s", pid, fd.Name()))
		d.FDList = append(d.FDList, FDEntry{FD: fd.Name(), Target: target})
	}
	return d, nil
}

// ─── Disks ──────────────────────────────────────────────────

type Disk struct {
	Device     string      `json:"device"`
	Model      string      `json:"model,omitempty"`
	Serial     string      `json:"serial,omitempty"`
	Vendor     string      `json:"vendor,omitempty"`
	SizeBytes  uint64      `json:"sizeBytes"`
	Type       string      `json:"type"`
	Rotational bool        `json:"rotational"`
	Transport  string      `json:"transport,omitempty"`
	Partitions []Partition `json:"partitions,omitempty"`
}

type Partition struct {
	Name         string   `json:"name"`
	Parent       string   `json:"parent"`
	SizeBytes    uint64   `json:"sizeBytes"`
	FSType       string   `json:"fsType,omitempty"`
	MountPoint   string   `json:"mountPoint,omitempty"`
	MountOptions []string `json:"mountOptions,omitempty"`
	UsagePct     float64  `json:"usagePct"`
	IsSystemDisk bool     `json:"isSystemDisk"`
}

func ListDisks(ctx context.Context) ([]Disk, error) {
	out, err := runOut(ctx, 5*time.Second, "lsblk", "-J", "-b", "-O")
	if err != nil {
		return nil, err
	}
	var v struct {
		BlockDevices []struct {
			Name       string  `json:"name"`
			Path       string  `json:"path"`
			Model      string  `json:"model"`
			Serial     string  `json:"serial"`
			Vendor     string  `json:"vendor"`
			Size       uint64  `json:"size"`
			Type       string  `json:"type"`
			Rota       bool    `json:"rota"`
			Tran       string  `json:"tran"`
			Children   []struct {
				Name       string   `json:"name"`
				Path       string   `json:"path"`
				Size       uint64   `json:"size"`
				Fstype     string   `json:"fstype"`
				Mountpoint string   `json:"mountpoint"`
				Mountpoints []string `json:"mountpoints"`
			} `json:"children"`
		} `json:"blockdevices"`
	}
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		return nil, err
	}
	// 拉一次 df 拿使用率
	usage := readMountUsage(ctx)
	var disks []Disk
	for _, d := range v.BlockDevices {
		if d.Type != "disk" {
			continue
		}
		disk := Disk{
			Device: d.Path, Model: strings.TrimSpace(d.Model),
			Serial: strings.TrimSpace(d.Serial), Vendor: strings.TrimSpace(d.Vendor),
			SizeBytes: d.Size, Type: d.Type, Rotational: d.Rota, Transport: d.Tran,
		}
		for _, c := range d.Children {
			mp := c.Mountpoint
			if mp == "" && len(c.Mountpoints) > 0 {
				mp = c.Mountpoints[0]
			}
			p := Partition{
				Name: c.Name, Parent: d.Path, SizeBytes: c.Size,
				FSType: c.Fstype, MountPoint: mp,
				IsSystemDisk: mp == "/",
			}
			if mp != "" {
				if u, ok := usage[mp]; ok {
					p.UsagePct = u.pct
					p.MountOptions = u.opts
				}
			}
			disk.Partitions = append(disk.Partitions, p)
		}
		disks = append(disks, disk)
	}
	return disks, nil
}

type mountUsage struct {
	pct  float64
	opts []string
}

func readMountUsage(ctx context.Context) map[string]mountUsage {
	out := map[string]mountUsage{}
	// df -B1 --output=source,target,size,used
	if b, err := runOut(ctx, 3*time.Second, "df", "-B1", "--output=target,size,used"); err == nil {
		for _, line := range strings.Split(b, "\n")[1:] {
			f := strings.Fields(line)
			if len(f) < 3 {
				continue
			}
			size, _ := strconv.ParseUint(f[1], 10, 64)
			used, _ := strconv.ParseUint(f[2], 10, 64)
			pct := 0.0
			if size > 0 {
				pct = float64(used) / float64(size) * 100
			}
			out[f[0]] = mountUsage{pct: pct}
		}
	}
	// mount opts from /proc/mounts
	if b, err := os.ReadFile("/proc/mounts"); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			f := strings.Fields(line)
			if len(f) < 4 {
				continue
			}
			mu := out[f[1]]
			mu.opts = strings.Split(f[3], ",")
			out[f[1]] = mu
		}
	}
	return out
}

// ─── Network Connections ────────────────────────────────────

type NetworkConn struct {
	Type        string `json:"type"`  // tcp / tcp6 / udp / udp6
	Local       string `json:"local"`
	Remote      string `json:"remote,omitempty"`
	State       string `json:"state"`
	PID         int    `json:"pid,omitempty"`
	ProcessName string `json:"processName,omitempty"`
}

var ssProcessRe = regexp.MustCompile(`\(\("([^"]+)",pid=(\d+),`)

func ListNetworkConnections(ctx context.Context, limit int) ([]NetworkConn, error) {
	if limit <= 0 {
		limit = 2000
	}
	out, err := runOut(ctx, 5*time.Second, "ss", "-tunap")
	if err != nil {
		return nil, err
	}
	var conns []NetworkConn
	for i, line := range strings.Split(out, "\n") {
		if i == 0 || line == "" {
			continue
		}
		if len(conns) >= limit {
			break
		}
		// Netid State Recv-Q Send-Q Local Remote Process...
		fields := strings.Fields(line)
		if len(fields) < 6 {
			continue
		}
		nc := NetworkConn{
			Type:  strings.ToLower(fields[0]),
			State: fields[1],
			Local: fields[4],
		}
		if len(fields) > 5 {
			nc.Remote = fields[5]
		}
		if m := ssProcessRe.FindStringSubmatch(line); m != nil {
			nc.ProcessName = m[1]
			nc.PID, _ = strconv.Atoi(m[2])
		}
		conns = append(conns, nc)
	}
	return conns, nil
}

// ─── GPU Processes ──────────────────────────────────────────

type GPUProcess struct {
	PID            int    `json:"pid"`
	Name           string `json:"name"`
	GPUUUID        string `json:"gpuUuid"`
	MemUsedMiB     int    `json:"memUsedMiB"`
	ContainerHint  string `json:"containerHint,omitempty"`
}

func ListGPUProcesses(ctx context.Context) []GPUProcess {
	if _, err := exec.LookPath("nvidia-smi"); err != nil {
		return nil
	}
	out, err := runOut(ctx, 3*time.Second, "nvidia-smi",
		"--query-compute-apps=pid,process_name,gpu_uuid,used_memory",
		"--format=csv,noheader,nounits")
	if err != nil {
		return nil
	}
	var procs []GPUProcess
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		f := splitCSV(line)
		if len(f) < 4 {
			continue
		}
		p := GPUProcess{Name: f[1], GPUUUID: f[2]}
		p.PID, _ = strconv.Atoi(f[0])
		p.MemUsedMiB, _ = strconv.Atoi(f[3])
		// 容器提示：如果进程 cgroup 里有 docker/kubepods 迹象
		if b, err := os.ReadFile(fmt.Sprintf("/proc/%d/cgroup", p.PID)); err == nil {
			s := string(b)
			switch {
			case strings.Contains(s, "docker"):
				p.ContainerHint = "docker"
			case strings.Contains(s, "kubepods"):
				p.ContainerHint = "k8s"
			}
		}
		procs = append(procs, p)
	}
	return procs
}

func splitCSV(line string) []string {
	parts := strings.Split(line, ",")
	out := make([]string, len(parts))
	for i, p := range parts {
		out[i] = strings.TrimSpace(p)
	}
	return out
}

// ─── helpers ────────────────────────────────────────────────

func runOut(ctx context.Context, timeout time.Duration, name string, args ...string) (string, error) {
	subCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	out, err := exec.CommandContext(subCtx, name, args...).Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// ExposeUsageDir 帮 tests / debug 用：把 filepath 常量导出
var _ = filepath.Separator
