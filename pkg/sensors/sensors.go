// Package sensors 采集温度 / 风扇 / 功耗类"动态"数据。
// 与 pkg/hardware 的静态硬件清单分开：这里每次请求都实时读，
// RAPL 功耗需要采样两次差分 (~200ms)。
package sensors

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Snapshot struct {
	CollectedAt time.Time    `json:"collectedAt"`
	CPU         CPUSensors   `json:"cpu"`
	GPUs        []GPUSensors `json:"gpus"`
	Fans        []FanReading `json:"fans,omitempty"`
}

type CPUSensors struct {
	PackageTempC float64      `json:"packageTempC,omitempty"`
	MaxTempC     float64      `json:"maxTempC,omitempty"`
	CritTempC    float64      `json:"critTempC,omitempty"`
	CoreTemps    []CoreTemp   `json:"coreTemps,omitempty"`
	PackagePowerW float64     `json:"packagePowerW,omitempty"`
	PowerAvailable bool       `json:"powerAvailable"`
	PowerReason   string      `json:"powerReason,omitempty"`
}

type CoreTemp struct {
	Label   string  `json:"label"`
	TempC   float64 `json:"tempC"`
	MaxC    float64 `json:"maxC,omitempty"`
	CritC   float64 `json:"critC,omitempty"`
}

type GPUSensors struct {
	Source     string  `json:"source"`          // "nvidia-smi" 或 "hwmon"
	HWMon      string  `json:"hwmon"`           // hwmon name (nouveau/amdgpu/…) 或 GPU 型号 (nvidia-smi)
	PCIAddress string  `json:"pciAddress,omitempty"`
	TempC      float64 `json:"tempC,omitempty"`
	MaxTempC   float64 `json:"maxTempC,omitempty"`
	CritTempC  float64 `json:"critTempC,omitempty"`
	FanRPM     int     `json:"fanRpm,omitempty"`
	FanPct     int     `json:"fanPct,omitempty"`
	FanKnown   bool    `json:"fanKnown"`
	// 以下仅 nvidia-smi 分支填充。util/mem/pstate 用零值合法 (0% util、0 MiB 使用)，
	// 不能 omitempty 否则前端会当"缺失"渲染成空。用 powerKnown 显式区分"这个 GPU 有没有 nvidia-smi 数据"。
	PowerDrawW  float64 `json:"powerDrawW,omitempty"`
	PowerLimitW float64 `json:"powerLimitW,omitempty"`
	UtilPct     int     `json:"utilPct"`
	MemUsedMiB  int     `json:"memUsedMiB"`
	MemTotalMiB int     `json:"memTotalMiB"`
	PState      string  `json:"pState,omitempty"`
	PowerKnown  bool    `json:"powerKnown"`
}

type FanReading struct {
	Source string `json:"source"`
	RPM    int    `json:"rpm"`
}

// Collect 采集当前传感器快照。RAPL 会阻塞 sampleDelay。
// GPU 数据优先走 nvidia-smi (有官方驱动时能拿到功耗/util/显存)，
// 回退到 /sys/class/hwmon (nouveau/amdgpu)。
func Collect(ctx context.Context) *Snapshot {
	s := &Snapshot{CollectedAt: time.Now()}
	s.CPU = collectCPU(ctx)
	if gpus := collectGPUsNvidiaSMI(ctx); len(gpus) > 0 {
		s.GPUs = gpus
	} else {
		s.GPUs = collectGPUs()
	}
	s.Fans = collectFans()
	return s
}

const sampleDelay = 200 * time.Millisecond

// ─── CPU (coretemp + RAPL) ────────────────────────────────────

func collectCPU(ctx context.Context) CPUSensors {
	c := CPUSensors{}

	// coretemp 在 /sys/class/hwmon/hwmonN name=coretemp
	if h := findHWMon("coretemp"); h != "" {
		items := readTempChannels(h)
		for _, it := range items {
			if strings.HasPrefix(strings.ToLower(it.Label), "package") {
				c.PackageTempC = it.TempC
				c.MaxTempC = it.MaxC
				c.CritTempC = it.CritC
			} else {
				c.CoreTemps = append(c.CoreTemps, it)
			}
		}
	}
	// x86_pkg_temp 作为 fallback
	if c.PackageTempC == 0 {
		if b, err := os.ReadFile("/sys/class/thermal/thermal_zone0/type"); err == nil &&
			strings.TrimSpace(string(b)) == "x86_pkg_temp" {
			if v, err := os.ReadFile("/sys/class/thermal/thermal_zone0/temp"); err == nil {
				if n, err := strconv.ParseFloat(strings.TrimSpace(string(v)), 64); err == nil {
					c.PackageTempC = n / 1000
				}
			}
		}
	}

	// Intel RAPL 功耗：采样两次算差分
	if e1, ok := readRAPLPackage(); ok {
		select {
		case <-ctx.Done():
			c.PowerAvailable = false
			c.PowerReason = "context cancelled"
			return c
		case <-time.After(sampleDelay):
		}
		if e2, ok := readRAPLPackage(); ok {
			deltaJ := float64(e2-e1) / 1_000_000 // uj → J
			dtSec := sampleDelay.Seconds()
			c.PackagePowerW = deltaJ / dtSec
			c.PowerAvailable = true
			return c
		}
	}
	c.PowerAvailable = false
	c.PowerReason = "Intel RAPL not available"
	return c
}

func readRAPLPackage() (uint64, bool) {
	b, err := os.ReadFile("/sys/class/powercap/intel-rapl:0/energy_uj")
	if err != nil {
		return 0, false
	}
	n, err := strconv.ParseUint(strings.TrimSpace(string(b)), 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

// ─── GPU · nvidia-smi 分支 (温度 + 功耗 + util + 显存, 官方驱动才有) ──

func collectGPUsNvidiaSMI(ctx context.Context) []GPUSensors {
	if _, err := exec.LookPath("nvidia-smi"); err != nil {
		return nil
	}
	subCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	// 一次拿全，避免多次 fork
	out, err := exec.CommandContext(subCtx, "nvidia-smi",
		"--query-gpu=name,pci.bus_id,temperature.gpu,fan.speed,power.draw,power.limit,utilization.gpu,memory.used,memory.total,pstate",
		"--format=csv,noheader,nounits").Output()
	if err != nil {
		return nil
	}
	var gpus []GPUSensors
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := splitCSV(line)
		if len(fields) < 10 {
			continue
		}
		g := GPUSensors{
			Source:     "nvidia-smi",
			HWMon:      fields[0],
			PCIAddress: strings.ToLower(fields[1]),
			PowerKnown: true,
		}
		if v, err := strconv.ParseFloat(fields[2], 64); err == nil {
			g.TempC = v
		}
		if v, err := strconv.Atoi(fields[3]); err == nil {
			g.FanPct = v
			g.FanKnown = true
		}
		if v, err := strconv.ParseFloat(fields[4], 64); err == nil {
			g.PowerDrawW = v
		}
		if v, err := strconv.ParseFloat(fields[5], 64); err == nil {
			g.PowerLimitW = v
		}
		if v, err := strconv.Atoi(fields[6]); err == nil {
			g.UtilPct = v
		}
		if v, err := strconv.Atoi(fields[7]); err == nil {
			g.MemUsedMiB = v
		}
		if v, err := strconv.Atoi(fields[8]); err == nil {
			g.MemTotalMiB = v
		}
		g.PState = fields[9]
		gpus = append(gpus, g)
	}
	return gpus
}

// splitCSV 拆 nvidia-smi CSV 输出 (逗号分隔 + 前后空格)
func splitCSV(line string) []string {
	parts := strings.Split(line, ",")
	out := make([]string, len(parts))
	for i, p := range parts {
		out[i] = strings.TrimSpace(p)
	}
	return out
}

// ─── GPU (nouveau / amdgpu / nvidia sysfs) ────────────────────

func collectGPUs() []GPUSensors {
	// 匹配所有 hwmon，凡 name 命中 GPU 系列的都算
	var gpus []GPUSensors
	entries, err := os.ReadDir("/sys/class/hwmon")
	if err != nil {
		return nil
	}
	for _, e := range entries {
		p := filepath.Join("/sys/class/hwmon", e.Name())
		name := readTrim(filepath.Join(p, "name"))
		if !isGPUHWMonName(name) {
			continue
		}
		g := GPUSensors{Source: "hwmon", HWMon: name}
		if v, ok := readMilliDegC(filepath.Join(p, "temp1_input")); ok {
			g.TempC = v
		}
		if v, ok := readMilliDegC(filepath.Join(p, "temp1_max")); ok {
			g.MaxTempC = v
		}
		if v, ok := readMilliDegC(filepath.Join(p, "temp1_crit")); ok {
			g.CritTempC = v
		}
		if b, err := os.ReadFile(filepath.Join(p, "fan1_input")); err == nil {
			if n, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil {
				g.FanRPM = n
				g.FanKnown = true
			}
		}
		gpus = append(gpus, g)
	}
	return gpus
}

func isGPUHWMonName(n string) bool {
	switch strings.ToLower(n) {
	case "nouveau", "amdgpu", "nvidia", "radeon", "i915":
		return true
	}
	return false
}

// ─── Fans (union of fan*_input across hwmon, excluding those already tied to GPU) ─

func collectFans() []FanReading {
	var fans []FanReading
	entries, err := os.ReadDir("/sys/class/hwmon")
	if err != nil {
		return nil
	}
	for _, e := range entries {
		p := filepath.Join("/sys/class/hwmon", e.Name())
		name := readTrim(filepath.Join(p, "name"))
		if isGPUHWMonName(name) {
			continue // GPU 风扇已在 GPUs 段单独展示
		}
		// look for fan*_input files
		files, _ := os.ReadDir(p)
		for _, f := range files {
			if !strings.HasPrefix(f.Name(), "fan") || !strings.HasSuffix(f.Name(), "_input") {
				continue
			}
			if b, err := os.ReadFile(filepath.Join(p, f.Name())); err == nil {
				if n, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil {
					fans = append(fans, FanReading{
						Source: name + "/" + strings.TrimSuffix(f.Name(), "_input"),
						RPM:    n,
					})
				}
			}
		}
	}
	return fans
}

// ─── helpers ─────────────────────────────────────────────────

func findHWMon(want string) string {
	entries, err := os.ReadDir("/sys/class/hwmon")
	if err != nil {
		return ""
	}
	for _, e := range entries {
		p := filepath.Join("/sys/class/hwmon", e.Name())
		if readTrim(filepath.Join(p, "name")) == want {
			return p
		}
	}
	return ""
}

func readTrim(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func readMilliDegC(path string) (float64, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	n, err := strconv.ParseFloat(strings.TrimSpace(string(b)), 64)
	if err != nil {
		return 0, false
	}
	return n / 1000, true
}

// readTempChannels 解析一个 hwmon 目录下所有 tempN_input+label+max+crit。
func readTempChannels(dir string) []CoreTemp {
	files, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	// 收集所有 tempN 通道号
	seen := map[string]bool{}
	for _, f := range files {
		n := f.Name()
		if !strings.HasPrefix(n, "temp") || !strings.HasSuffix(n, "_input") {
			continue
		}
		idx := strings.TrimSuffix(strings.TrimPrefix(n, "temp"), "_input")
		seen[idx] = true
	}
	// 按数字序排
	var idxs []int
	for k := range seen {
		if n, err := strconv.Atoi(k); err == nil {
			idxs = append(idxs, n)
		}
	}
	// 简单排序
	for i := 0; i < len(idxs); i++ {
		for j := i + 1; j < len(idxs); j++ {
			if idxs[j] < idxs[i] {
				idxs[i], idxs[j] = idxs[j], idxs[i]
			}
		}
	}
	var items []CoreTemp
	for _, n := range idxs {
		p := func(kind string) string { return filepath.Join(dir, "temp"+strconv.Itoa(n)+"_"+kind) }
		v, ok := readMilliDegC(p("input"))
		if !ok {
			continue
		}
		it := CoreTemp{
			Label: readTrim(p("label")),
			TempC: v,
		}
		if v, ok := readMilliDegC(p("max")); ok {
			it.MaxC = v
		}
		if v, ok := readMilliDegC(p("crit")); ok {
			it.CritC = v
		}
		items = append(items, it)
	}
	return items
}
