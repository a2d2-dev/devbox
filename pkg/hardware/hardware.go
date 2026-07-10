// Package hardware 采集本机硬件清单快照（面向"检查/巡检"场景，非实时监控）。
// 由 pkg/console/handlers_hardware.go 通过 /api/v1/hardware 暴露。
package hardware

import (
	"bufio"
	"context"
	"encoding/json"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Snapshot 一次采集的完整硬件快照。
type Snapshot struct {
	CollectedAt time.Time    `json:"collectedAt"`
	OS          OSInfo       `json:"os"`
	BIOS        BIOSInfo     `json:"bios"`
	Board       BoardInfo    `json:"board"`
	CPU         CPUInfo      `json:"cpu"`
	Memory      MemoryInfo   `json:"memory"`
	GPUs        []GPUInfo    `json:"gpus"`
	PCIe        []PCIeDevice `json:"pcie"`
	Storage     []DiskInfo   `json:"storage"`
	Network     []NICInfo    `json:"network"`
	Warnings    []string     `json:"warnings,omitempty"`
}

type OSInfo struct {
	Hostname      string `json:"hostname"`
	Distro        string `json:"distro"`
	Kernel        string `json:"kernel"`
	Arch          string `json:"arch"`
	UptimeSeconds int64  `json:"uptimeSeconds"`
}

type BIOSInfo struct {
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
	Vendor    string `json:"vendor,omitempty"`
	Version   string `json:"version,omitempty"`
	Date      string `json:"date,omitempty"`
}

type BoardInfo struct {
	Available    bool   `json:"available"`
	Reason       string `json:"reason,omitempty"`
	Manufacturer string `json:"manufacturer,omitempty"`
	Product      string `json:"product,omitempty"`
	Serial       string `json:"serial,omitempty"`
	SystemName   string `json:"systemName,omitempty"`
}

type CPUInfo struct {
	Model     string   `json:"model"`
	Vendor    string   `json:"vendor"`
	Arch      string   `json:"arch"`
	Sockets   int      `json:"sockets"`
	Cores     int      `json:"cores"`
	Threads   int      `json:"threads"`
	MaxMHz    float64  `json:"maxMHz"`
	MinMHz    float64  `json:"minMHz"`
	Caches    []string `json:"caches,omitempty"`
	Virt      string   `json:"virt,omitempty"`
	FlagsHint []string `json:"flagsHint,omitempty"` // 常用子集: avx, avx2, avx512, sse4_2, aes, vt-x
}

type DIMM struct {
	Slot         string `json:"slot"`
	Populated    bool   `json:"populated"`
	SizeBytes    uint64 `json:"sizeBytes,omitempty"`
	Type         string `json:"type,omitempty"`
	SpeedMTs     int    `json:"speedMTs,omitempty"`
	Manufacturer string `json:"manufacturer,omitempty"`
	PartNumber   string `json:"partNumber,omitempty"`
	Serial       string `json:"serial,omitempty"`
}

type MemoryInfo struct {
	TotalBytes     uint64 `json:"totalBytes"`
	AvailableBytes uint64 `json:"availableBytes"`
	DIMMs          []DIMM `json:"dimms,omitempty"`
	DIMMsAvailable bool   `json:"dimmsAvailable"`
	DIMMsReason    string `json:"dimmsReason,omitempty"`
}

type GPUInfo struct {
	PCIAddress string     `json:"pciAddress"`
	Vendor     string     `json:"vendor"`
	Model      string     `json:"model"`
	Class      string     `json:"class"`
	Driver     string     `json:"driver,omitempty"`
	KernelMod  string     `json:"kernelMod,omitempty"`
	LinkGenCap string     `json:"linkGenCap,omitempty"`
	LinkGenCur string     `json:"linkGenCur,omitempty"`
	LinkWidCap string     `json:"linkWidCap,omitempty"`
	LinkWidCur string     `json:"linkWidCur,omitempty"`
	LinkStatus LinkStatus `json:"linkStatus,omitempty"`
	VRAM       string     `json:"vram,omitempty"`
	Notes      string     `json:"notes,omitempty"`
}

// LinkStatus 描述 PCIe 链路当前状态的分类。用于替代单一 degraded=true 判断。
//
//   ok        — 协商速率/宽度与能力一致
//   empty     — Bridge/Root Port 但 widCur=x0：槽位没插东西，不算异常
//   idle      — Endpoint 宽度打满但代降 (widCur==widCap && genCur<genCap)：
//               ASPM 空闲省电，通常自动升速，不算异常
//   downgrade — 真降级：widCur<widCap 且不为空槽。这是需要排查的
type LinkStatus string

const (
	LinkStatusOK        LinkStatus = "ok"
	LinkStatusEmpty     LinkStatus = "empty"
	LinkStatusIdle      LinkStatus = "idle"
	LinkStatusDowngrade LinkStatus = "downgrade"
)

type PCIeDevice struct {
	Address    string     `json:"address"`
	Class      string     `json:"class"`
	ClassCode  string     `json:"classCode"`
	Vendor     string     `json:"vendor"`
	Device     string     `json:"device"`
	VendorID   string     `json:"vendorId"`
	DeviceID   string     `json:"deviceId"`
	Driver     string     `json:"driver,omitempty"`
	LinkGenCap string     `json:"linkGenCap,omitempty"`
	LinkGenCur string     `json:"linkGenCur,omitempty"`
	LinkWidCap string     `json:"linkWidCap,omitempty"`
	LinkWidCur string     `json:"linkWidCur,omitempty"`
	LinkStatus LinkStatus `json:"linkStatus,omitempty"`
	Degraded   bool       `json:"degraded,omitempty"` // 仅当 LinkStatus == downgrade 时为 true
}

type DiskInfo struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	Model      string `json:"model,omitempty"`
	Vendor     string `json:"vendor,omitempty"`
	Serial     string `json:"serial,omitempty"`
	SizeBytes  uint64 `json:"sizeBytes"`
	Rotational bool   `json:"rotational"`
	Transport  string `json:"transport,omitempty"`
	Type       string `json:"type,omitempty"`
	Mountpoint string `json:"mountpoint,omitempty"`
	FSType     string `json:"fstype,omitempty"`
}

type NICInfo struct {
	Name        string   `json:"name"`
	MAC         string   `json:"mac,omitempty"`
	MTU         int      `json:"mtu,omitempty"`
	State       string   `json:"state,omitempty"`
	Driver      string   `json:"driver,omitempty"`
	FWVersion   string   `json:"fwVersion,omitempty"`
	LinkSpeed   string   `json:"linkSpeed,omitempty"`
	LinkDuplex  string   `json:"linkDuplex,omitempty"`
	IPv4        []string `json:"ipv4,omitempty"`
	IPv6        []string `json:"ipv6,omitempty"`
	IsVirtual   bool     `json:"isVirtual"`
	VirtualKind string   `json:"virtualKind,omitempty"` // bridge, veth, docker, tun...
}

// Collector 采集器，带 TTL 缓存避免每次刷屏都跑外部命令。
type Collector struct {
	ttl time.Duration
	mu  sync.Mutex
	last *Snapshot
}

func New(ttl time.Duration) *Collector {
	if ttl <= 0 {
		ttl = 60 * time.Second
	}
	return &Collector{ttl: ttl}
}

// Get 返回一个不老于 TTL 的快照。
func (c *Collector) Get(ctx context.Context) *Snapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.last != nil && time.Since(c.last.CollectedAt) < c.ttl {
		return c.last
	}
	snap := collect(ctx)
	c.last = snap
	return snap
}

// ─── 采集入口 ───────────────────────────────────────────────

func collect(ctx context.Context) *Snapshot {
	s := &Snapshot{CollectedAt: time.Now()}

	// 并行采集，各段互不阻塞。
	var wg sync.WaitGroup
	steps := []func(){
		func() { s.OS = collectOS(ctx) },
		func() { s.BIOS = collectBIOS(ctx) },
		func() { s.Board = collectBoard(ctx) },
		func() { s.CPU = collectCPU(ctx) },
		func() { s.Memory = collectMemory(ctx) },
		func() { s.PCIe = collectPCIe(ctx) },
		func() { s.Storage = collectStorage(ctx) },
		func() { s.Network = collectNetwork(ctx) },
	}
	for _, f := range steps {
		wg.Add(1)
		go func(fn func()) { defer wg.Done(); fn() }(f)
	}
	wg.Wait()

	// GPU 依赖 PCIe 结果，串行在后面。
	s.GPUs = deriveGPUs(s.PCIe)
	return s
}

// ─── OS ─────────────────────────────────────────────────────

func collectOS(ctx context.Context) OSInfo {
	o := OSInfo{}
	o.Hostname = strings.TrimSpace(readFile("/etc/hostname"))
	if o.Hostname == "" {
		if out, err := run(ctx, "hostname"); err == nil {
			o.Hostname = strings.TrimSpace(out)
		}
	}
	if b := readFile("/etc/os-release"); b != "" {
		kv := parseKV(b, "=")
		name := unquote(kv["PRETTY_NAME"])
		if name == "" {
			name = unquote(kv["NAME"]) + " " + unquote(kv["VERSION"])
		}
		o.Distro = strings.TrimSpace(name)
	}
	if out, err := run(ctx, "uname", "-r"); err == nil {
		o.Kernel = strings.TrimSpace(out)
	}
	if out, err := run(ctx, "uname", "-m"); err == nil {
		o.Arch = strings.TrimSpace(out)
	}
	if b := readFile("/proc/uptime"); b != "" {
		parts := strings.Fields(b)
		if len(parts) > 0 {
			if f, err := strconv.ParseFloat(parts[0], 64); err == nil {
				o.UptimeSeconds = int64(f)
			}
		}
	}
	return o
}

// ─── DMI (BIOS / Board) ────────────────────────────────────

func collectBIOS(ctx context.Context) BIOSInfo {
	v, err1 := run(ctx, "dmidecode", "-s", "bios-vendor")
	ver, err2 := run(ctx, "dmidecode", "-s", "bios-version")
	d, err3 := run(ctx, "dmidecode", "-s", "bios-release-date")
	if err1 != nil && err2 != nil && err3 != nil {
		return BIOSInfo{Available: false, Reason: "dmidecode unavailable (root required)"}
	}
	return BIOSInfo{
		Available: true,
		Vendor:    strings.TrimSpace(v),
		Version:   strings.TrimSpace(ver),
		Date:      strings.TrimSpace(d),
	}
}

func collectBoard(ctx context.Context) BoardInfo {
	man, err1 := run(ctx, "dmidecode", "-s", "baseboard-manufacturer")
	pr, err2 := run(ctx, "dmidecode", "-s", "baseboard-product-name")
	sr, _ := run(ctx, "dmidecode", "-s", "baseboard-serial-number")
	sn, _ := run(ctx, "dmidecode", "-s", "system-product-name")
	if err1 != nil && err2 != nil {
		return BoardInfo{Available: false, Reason: "dmidecode unavailable (root required)"}
	}
	return BoardInfo{
		Available:    true,
		Manufacturer: strings.TrimSpace(man),
		Product:      strings.TrimSpace(pr),
		Serial:       strings.TrimSpace(sr),
		SystemName:   strings.TrimSpace(sn),
	}
}

// ─── CPU ────────────────────────────────────────────────────

func collectCPU(ctx context.Context) CPUInfo {
	c := CPUInfo{}
	out, err := run(ctx, "lscpu", "-J")
	if err == nil {
		var v struct {
			LSCPU []struct {
				Field string `json:"field"`
				Data  string `json:"data"`
			} `json:"lscpu"`
		}
		if json.Unmarshal([]byte(out), &v) == nil {
			for _, kv := range v.LSCPU {
				f := strings.TrimSuffix(kv.Field, ":")
				switch f {
				case "Model name":
					c.Model = kv.Data
				case "Vendor ID":
					c.Vendor = kv.Data
				case "Architecture":
					c.Arch = kv.Data
				case "Socket(s)":
					c.Sockets = atoi(kv.Data)
				case "Core(s) per socket":
					if c.Sockets == 0 {
						c.Sockets = 1
					}
					c.Cores = atoi(kv.Data) * c.Sockets
				case "CPU(s)":
					c.Threads = atoi(kv.Data)
				case "CPU max MHz":
					c.MaxMHz, _ = strconv.ParseFloat(kv.Data, 64)
				case "CPU min MHz":
					c.MinMHz, _ = strconv.ParseFloat(kv.Data, 64)
				case "L1d cache", "L1i cache", "L2 cache", "L3 cache":
					c.Caches = append(c.Caches, f+": "+kv.Data)
				case "Virtualization":
					c.Virt = kv.Data
				case "Flags":
					c.FlagsHint = extractFlags(kv.Data)
				}
			}
		}
	}
	if c.Cores == 0 {
		// fallback via /proc/cpuinfo
		c.Cores = countUniqueCoreIDs()
		c.Threads = countLogicalCPUs()
	}
	return c
}

func extractFlags(all string) []string {
	want := []string{"avx512f", "avx2", "avx", "sse4_2", "aes", "vmx", "svm"}
	set := map[string]bool{}
	for _, f := range strings.Fields(all) {
		set[f] = true
	}
	var got []string
	for _, k := range want {
		if set[k] {
			got = append(got, k)
		}
	}
	return got
}

// ─── Memory ────────────────────────────────────────────────

func collectMemory(ctx context.Context) MemoryInfo {
	m := MemoryInfo{}
	if b := readFile("/proc/meminfo"); b != "" {
		kv := parseKV(b, ":")
		m.TotalBytes = kbToBytes(kv["MemTotal"])
		m.AvailableBytes = kbToBytes(kv["MemAvailable"])
	}
	// DIMM 详情来自 dmidecode -t memory (root only)
	out, err := run(ctx, "dmidecode", "-t", "memory")
	if err != nil {
		m.DIMMsAvailable = false
		m.DIMMsReason = "dmidecode unavailable (root required)"
		return m
	}
	m.DIMMs = parseDMIMemory(out)
	m.DIMMsAvailable = true
	return m
}

// parseDMIMemory 解析 `dmidecode -t memory` 的 "Memory Device" 段。
func parseDMIMemory(out string) []DIMM {
	var dimms []DIMM
	var cur *DIMM
	scan := bufio.NewScanner(strings.NewReader(out))
	scan.Buffer(make([]byte, 0, 1024*1024), 1024*1024)
	for scan.Scan() {
		line := scan.Text()
		if strings.HasPrefix(line, "Memory Device") {
			if cur != nil {
				dimms = append(dimms, *cur)
			}
			cur = &DIMM{}
			continue
		}
		if cur == nil {
			continue
		}
		line = strings.TrimSpace(line)
		colon := strings.Index(line, ":")
		if colon < 0 {
			continue
		}
		k := strings.TrimSpace(line[:colon])
		v := strings.TrimSpace(line[colon+1:])
		switch k {
		case "Locator":
			cur.Slot = v
		case "Size":
			if strings.Contains(v, "No Module") || v == "" {
				cur.Populated = false
			} else {
				cur.Populated = true
				cur.SizeBytes = parseDMISize(v)
			}
		case "Type":
			if v != "Unknown" {
				cur.Type = v
			}
		case "Speed", "Configured Memory Speed":
			if v != "Unknown" && v != "" {
				parts := strings.Fields(v)
				if len(parts) > 0 {
					cur.SpeedMTs = atoi(parts[0])
				}
			}
		case "Manufacturer":
			if v != "Unknown" && v != "" {
				cur.Manufacturer = v
			}
		case "Part Number":
			if v != "Unknown" && v != "" {
				cur.PartNumber = v
			}
		case "Serial Number":
			if v != "Unknown" && v != "" {
				cur.Serial = v
			}
		}
	}
	if cur != nil {
		dimms = append(dimms, *cur)
	}
	return dimms
}

var dmiSizeRe = regexp.MustCompile(`^(\d+)\s*(GB|MB|TB)$`)

func parseDMISize(s string) uint64 {
	m := dmiSizeRe.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return 0
	}
	n, _ := strconv.ParseUint(m[1], 10, 64)
	switch strings.ToUpper(m[2]) {
	case "MB":
		return n * 1024 * 1024
	case "GB":
		return n * 1024 * 1024 * 1024
	case "TB":
		return n * 1024 * 1024 * 1024 * 1024
	}
	return 0
}

// ─── PCIe ──────────────────────────────────────────────────

// lspci -vvv -nn 段头正则
var pciHeadRe = regexp.MustCompile(`^([0-9a-f]{2,4}:[0-9a-f]{2}\.[0-9a-f])\s+([^:]+)\s+\[([0-9a-f]{4})\]:\s+(.+?)\s+\[([0-9a-f]{4}):([0-9a-f]{4})\]`)

func collectPCIe(ctx context.Context) []PCIeDevice {
	out, err := run(ctx, "lspci", "-vvv", "-nn")
	if err != nil {
		return nil
	}
	var devs []PCIeDevice
	var cur *PCIeDevice
	scan := bufio.NewScanner(strings.NewReader(out))
	scan.Buffer(make([]byte, 0, 1024*1024), 4*1024*1024)
	for scan.Scan() {
		line := scan.Text()
		if !strings.HasPrefix(line, "\t") && strings.Contains(line, ":") {
			// new device block
			if cur != nil {
				devs = append(devs, *cur)
			}
			m := pciHeadRe.FindStringSubmatch(line)
			if m == nil {
				cur = nil
				continue
			}
			// tail: model contains rev token; keep as-is (concise)
			cur = &PCIeDevice{
				Address:   m[1],
				Class:     strings.TrimSpace(m[2]),
				ClassCode: m[3],
				Device:    strings.TrimSpace(m[4]),
				VendorID:  m[5],
				DeviceID:  m[6],
			}
			continue
		}
		if cur == nil {
			continue
		}
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "Kernel driver in use:"):
			cur.Driver = strings.TrimSpace(strings.TrimPrefix(trimmed, "Kernel driver in use:"))
		case strings.HasPrefix(trimmed, "LnkCap:"):
			cur.LinkGenCap, cur.LinkWidCap = parseLnk(trimmed)
		case strings.HasPrefix(trimmed, "LnkSta:"):
			cur.LinkGenCur, cur.LinkWidCur = parseLnk(trimmed)
		}
	}
	if cur != nil {
		devs = append(devs, *cur)
	}
	// vendor name derivation from PCIe class heading isn't in `-nn` output;
	// use the `-mm` machine format to backfill vendor names.
	backfillVendor(ctx, devs)
	for i := range devs {
		devs[i].LinkStatus = classifyLink(devs[i])
		devs[i].Degraded = devs[i].LinkStatus == LinkStatusDowngrade
	}
	return devs
}

// classifyLink 根据 LnkCap/LnkSta 判定链路状态。
// 关键区分：
//   - 桥/根端口 widCur=x0 => 空槽 (empty)，不算异常
//   - endpoint 宽度打满但速率降 => ASPM 空闲省电 (idle)，不算异常
//   - 宽度实际低于能力 => 真降级 (downgrade)
func classifyLink(d PCIeDevice) LinkStatus {
	if d.LinkGenCap == "" || d.LinkGenCur == "" || d.LinkWidCap == "" || d.LinkWidCur == "" {
		return LinkStatusOK // 数据不全，不下结论
	}
	isBridge := d.ClassCode == "0604" || d.ClassCode == "0600"
	if d.LinkWidCur == "x0" {
		if isBridge {
			return LinkStatusEmpty
		}
		// endpoint 报 x0 罕见，按空处理避免误报
		return LinkStatusEmpty
	}
	wCap, wCur := widRank(d.LinkWidCap), widRank(d.LinkWidCur)
	if wCur > 0 && wCap > 0 && wCur < wCap {
		return LinkStatusDowngrade
	}
	gCap, gCur := genRank(d.LinkGenCap), genRank(d.LinkGenCur)
	if gCur > 0 && gCap > 0 && gCur < gCap {
		return LinkStatusIdle
	}
	return LinkStatusOK
}

var (
	lnkSpeedRe = regexp.MustCompile(`Speed\s+([\d.]+GT/s)`)
	lnkWidthRe = regexp.MustCompile(`Width\s+(x\d+)`)
)

func parseLnk(line string) (gen, width string) {
	if m := lnkSpeedRe.FindStringSubmatch(line); m != nil {
		gen = m[1]
	}
	if m := lnkWidthRe.FindStringSubmatch(line); m != nil {
		width = m[1]
	}
	return
}

func genRank(g string) int {
	switch g {
	case "2.5GT/s":
		return 1
	case "5GT/s":
		return 2
	case "8GT/s":
		return 3
	case "16GT/s":
		return 4
	case "32GT/s":
		return 5
	case "64GT/s":
		return 6
	}
	return 0
}

func widRank(w string) int {
	if !strings.HasPrefix(w, "x") {
		return 0
	}
	n, _ := strconv.Atoi(w[1:])
	return n
}

func backfillVendor(ctx context.Context, devs []PCIeDevice) {
	out, err := run(ctx, "lspci", "-mm")
	if err != nil {
		return
	}
	// each line: "01:00.0" "VGA compatible controller" "NVIDIA Corporation" "GP104 [GeForce GTX 1080]" ...
	m := map[string]string{}
	scan := bufio.NewScanner(strings.NewReader(out))
	for scan.Scan() {
		toks := parseQuoted(scan.Text())
		if len(toks) >= 3 {
			m[toks[0]] = toks[2]
		}
	}
	for i := range devs {
		// lspci -mm addresses omit domain if 0000; match by suffix
		short := devs[i].Address
		if strings.HasPrefix(short, "0000:") {
			short = strings.TrimPrefix(short, "0000:")
		}
		if v, ok := m[short]; ok {
			devs[i].Vendor = v
		} else if v, ok := m[devs[i].Address]; ok {
			devs[i].Vendor = v
		}
	}
}

// parseQuoted 拆解 lspci -mm 那种带引号的字段。
func parseQuoted(line string) []string {
	var out []string
	var buf strings.Builder
	in := false
	for _, r := range line {
		switch {
		case r == '"':
			if in {
				out = append(out, buf.String())
				buf.Reset()
			}
			in = !in
		case in:
			buf.WriteRune(r)
		}
	}
	return out
}

// ─── GPU derivation ─────────────────────────────────────────

func deriveGPUs(all []PCIeDevice) []GPUInfo {
	var gpus []GPUInfo
	for _, d := range all {
		if !isGPUClass(d.ClassCode) {
			continue
		}
		gpus = append(gpus, GPUInfo{
			PCIAddress: d.Address,
			Vendor:     d.Vendor,
			Model:      d.Device,
			Class:      d.Class,
			Driver:     d.Driver,
			LinkGenCap: d.LinkGenCap,
			LinkGenCur: d.LinkGenCur,
			LinkWidCap: d.LinkWidCap,
			LinkWidCur: d.LinkWidCur,
			LinkStatus: d.LinkStatus,
		})
	}
	return gpus
}

func isGPUClass(code string) bool {
	// 0300 = VGA, 0302 = 3D controller, 0301 = XGA
	return code == "0300" || code == "0302" || code == "0301" || code == "0380"
}

// ─── Storage ───────────────────────────────────────────────

func collectStorage(ctx context.Context) []DiskInfo {
	out, err := run(ctx, "lsblk", "-J", "-b", "-o",
		"NAME,PATH,MODEL,VENDOR,SERIAL,SIZE,ROTA,TRAN,TYPE,MOUNTPOINT,FSTYPE")
	if err != nil {
		return nil
	}
	var v struct {
		Blockdevices []struct {
			Name       string `json:"name"`
			Path       string `json:"path"`
			Model      string `json:"model"`
			Vendor     string `json:"vendor"`
			Serial     string `json:"serial"`
			Size       uint64 `json:"size"`
			Rota       bool   `json:"rota"`
			Tran       string `json:"tran"`
			Type       string `json:"type"`
			Mountpoint string `json:"mountpoint"`
			Fstype     string `json:"fstype"`
		} `json:"blockdevices"`
	}
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		return nil
	}
	var disks []DiskInfo
	for _, d := range v.Blockdevices {
		if d.Type != "disk" {
			continue
		}
		disks = append(disks, DiskInfo{
			Name:       d.Name,
			Path:       d.Path,
			Model:      strings.TrimSpace(d.Model),
			Vendor:     strings.TrimSpace(d.Vendor),
			Serial:     strings.TrimSpace(d.Serial),
			SizeBytes:  d.Size,
			Rotational: d.Rota,
			Transport:  d.Tran,
			Type:       d.Type,
			Mountpoint: d.Mountpoint,
			FSType:     d.Fstype,
		})
	}
	return disks
}

// ─── Network ───────────────────────────────────────────────

func collectNetwork(ctx context.Context) []NICInfo {
	out, err := run(ctx, "ip", "-j", "-d", "addr")
	if err != nil {
		return nil
	}
	var raw []struct {
		Ifname   string `json:"ifname"`
		Address  string `json:"address"`
		MTU      int    `json:"mtu"`
		Operstate string `json:"operstate"`
		Linkinfo *struct {
			InfoKind string `json:"info_kind"`
		} `json:"linkinfo"`
		AddrInfo []struct {
			Family string `json:"family"`
			Local  string `json:"local"`
		} `json:"addr_info"`
	}
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		return nil
	}
	var nics []NICInfo
	for _, r := range raw {
		if r.Ifname == "lo" {
			continue
		}
		n := NICInfo{
			Name:  r.Ifname,
			MAC:   r.Address,
			MTU:   r.MTU,
			State: r.Operstate,
		}
		for _, a := range r.AddrInfo {
			if a.Family == "inet" {
				n.IPv4 = append(n.IPv4, a.Local)
			} else if a.Family == "inet6" {
				n.IPv6 = append(n.IPv6, a.Local)
			}
		}
		if r.Linkinfo != nil && r.Linkinfo.InfoKind != "" {
			n.IsVirtual = true
			n.VirtualKind = r.Linkinfo.InfoKind
		}
		if !n.IsVirtual {
			// veth / docker guess: prefix based, since some kernel builds omit linkinfo.
			if strings.HasPrefix(n.Name, "veth") ||
				strings.HasPrefix(n.Name, "br-") ||
				strings.HasPrefix(n.Name, "docker") ||
				strings.HasPrefix(n.Name, "tun") ||
				strings.HasPrefix(n.Name, "tap") ||
				strings.HasPrefix(n.Name, "cni") ||
				strings.HasPrefix(n.Name, "cali") {
				n.IsVirtual = true
				if n.VirtualKind == "" {
					n.VirtualKind = "guessed:" + prefixKind(n.Name)
				}
			}
		}
		if !n.IsVirtual {
			// physical NIC — try ethtool for driver / firmware / link speed
			enrichPhysicalNIC(ctx, &n)
		}
		nics = append(nics, n)
	}
	return nics
}

func prefixKind(name string) string {
	switch {
	case strings.HasPrefix(name, "veth"):
		return "veth"
	case strings.HasPrefix(name, "br-"), strings.HasPrefix(name, "docker"):
		return "bridge"
	case strings.HasPrefix(name, "tun"), strings.HasPrefix(name, "tap"):
		return "tun/tap"
	case strings.HasPrefix(name, "cni"), strings.HasPrefix(name, "cali"):
		return "cni"
	}
	return "virtual"
}

func enrichPhysicalNIC(ctx context.Context, n *NICInfo) {
	if out, err := run(ctx, "ethtool", "-i", n.Name); err == nil {
		for _, line := range strings.Split(out, "\n") {
			kv := strings.SplitN(line, ":", 2)
			if len(kv) != 2 {
				continue
			}
			k := strings.TrimSpace(kv[0])
			v := strings.TrimSpace(kv[1])
			switch k {
			case "driver":
				n.Driver = v
			case "firmware-version":
				n.FWVersion = v
			}
		}
	}
	if out, err := run(ctx, "ethtool", n.Name); err == nil {
		for _, line := range strings.Split(out, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "Speed:") {
				n.LinkSpeed = strings.TrimSpace(strings.TrimPrefix(line, "Speed:"))
			}
			if strings.HasPrefix(line, "Duplex:") {
				n.LinkDuplex = strings.TrimSpace(strings.TrimPrefix(line, "Duplex:"))
			}
		}
	}
}

// ─── helpers ────────────────────────────────────────────────

func run(ctx context.Context, name string, args ...string) (string, error) {
	subCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(subCtx, name, args...).Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func readFile(path string) string {
	out, err := exec.Command("cat", path).Output()
	if err != nil {
		return ""
	}
	return string(out)
}

func parseKV(text, sep string) map[string]string {
	m := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		idx := strings.Index(line, sep)
		if idx < 0 {
			continue
		}
		k := strings.TrimSpace(line[:idx])
		v := strings.TrimSpace(line[idx+len(sep):])
		m[k] = v
	}
	return m
}

func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}

func atoi(s string) int {
	s = strings.TrimSpace(s)
	n, _ := strconv.Atoi(s)
	return n
}

func kbToBytes(s string) uint64 {
	f := strings.Fields(strings.TrimSuffix(strings.TrimSuffix(s, " kB"), "kB"))
	if len(f) == 0 {
		return 0
	}
	n, _ := strconv.ParseUint(f[0], 10, 64)
	return n * 1024
}

func countUniqueCoreIDs() int {
	b := readFile("/proc/cpuinfo")
	set := map[string]bool{}
	for _, line := range strings.Split(b, "\n") {
		if strings.HasPrefix(line, "core id") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				set[strings.TrimSpace(parts[1])] = true
			}
		}
	}
	return len(set)
}

func countLogicalCPUs() int {
	b := readFile("/proc/cpuinfo")
	n := 0
	for _, line := range strings.Split(b, "\n") {
		if strings.HasPrefix(line, "processor") {
			n++
		}
	}
	return n
}
