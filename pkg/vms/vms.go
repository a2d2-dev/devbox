// Package vms wraps the host libvirt CLI for DevBox VM management.
package vms

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Manager struct {
	virsh string
}

func NewManager() *Manager {
	return &Manager{virsh: "virsh"}
}

type Domain struct {
	Name        string         `json:"name"`
	UUID        string         `json:"uuid,omitempty"`
	State       string         `json:"state"`
	Persistent  bool           `json:"persistent"`
	Autostart   bool           `json:"autostart"`
	VCPUs       int            `json:"vcpus"`
	CPUTimeSec  float64        `json:"cpuTimeSec"`
	Memory      Memory         `json:"memory"`
	Disks       []Disk         `json:"disks"`
	Filesystems []Filesystem   `json:"filesystems,omitempty"`
	Interfaces  []Interface    `json:"interfaces"`
	Guest       *GuestSnapshot `json:"guest,omitempty"`
	Error       string         `json:"error,omitempty"`
	UpdatedAt   string         `json:"updatedAt"`
}

type Memory struct {
	MaxKiB       uint64 `json:"maxKiB"`
	UsedKiB      uint64 `json:"usedKiB"`
	ActualKiB    uint64 `json:"actualKiB,omitempty"`
	AvailableKiB uint64 `json:"availableKiB,omitempty"`
	UsableKiB    uint64 `json:"usableKiB,omitempty"`
	UnusedKiB    uint64 `json:"unusedKiB,omitempty"`
	RSSKiB       uint64 `json:"rssKiB,omitempty"`
}

type Disk struct {
	Target        string `json:"target"`
	Device        string `json:"device"`
	Type          string `json:"type"`
	Source        string `json:"source,omitempty"`
	CapacityBytes uint64 `json:"capacityBytes,omitempty"`
	Allocation    uint64 `json:"allocationBytes,omitempty"`
	PhysicalBytes uint64 `json:"physicalBytes,omitempty"`
	ReadBytes     uint64 `json:"readBytes,omitempty"`
	WriteBytes    uint64 `json:"writeBytes,omitempty"`
}

type Filesystem struct {
	Target     string `json:"target"`
	Source     string `json:"source,omitempty"`
	Type       string `json:"type,omitempty"`
	AccessMode string `json:"accessMode,omitempty"`
	Driver     string `json:"driver,omitempty"`
}

type Interface struct {
	Name     string `json:"name"`
	MAC      string `json:"mac,omitempty"`
	Protocol string `json:"protocol,omitempty"`
	Address  string `json:"address,omitempty"`
}

type GuestSnapshot struct {
	AgentOK        bool         `json:"agentOK"`
	LoadAverage    []float64    `json:"loadAverage,omitempty"`
	Memory         *GuestMem    `json:"memory,omitempty"`
	Mounts         []GuestMount `json:"mounts,omitempty"`
	MemoryPressure []Pressure   `json:"memoryPressure,omitempty"`
	Error          string       `json:"error,omitempty"`
}

type GuestMem struct {
	TotalBytes     uint64 `json:"totalBytes"`
	UsedBytes      uint64 `json:"usedBytes"`
	FreeBytes      uint64 `json:"freeBytes"`
	SharedBytes    uint64 `json:"sharedBytes"`
	BuffCacheBytes uint64 `json:"buffCacheBytes"`
	AvailableBytes uint64 `json:"availableBytes"`
	SwapTotalBytes uint64 `json:"swapTotalBytes"`
	SwapUsedBytes  uint64 `json:"swapUsedBytes"`
	SwapFreeBytes  uint64 `json:"swapFreeBytes"`
}

type Pressure struct {
	Kind   string  `json:"kind"`
	Avg10  float64 `json:"avg10"`
	Avg60  float64 `json:"avg60"`
	Avg300 float64 `json:"avg300"`
}

type GuestMount struct {
	Source string `json:"source"`
	Target string `json:"target"`
	FSType string `json:"fstype"`
}

type ConfigUpdate struct {
	VCPUs     int   `json:"vcpus"`
	MemoryMiB int   `json:"memoryMiB"`
	Autostart *bool `json:"autostart"`
}

func (m *Manager) List(ctx context.Context) ([]Domain, error) {
	names, err := m.domainNames(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Domain, 0, len(names))
	for _, name := range names {
		d, err := m.Get(ctx, name)
		if err != nil {
			out = append(out, Domain{Name: name, State: "unknown", Error: err.Error(), UpdatedAt: time.Now().Format(time.RFC3339)})
			continue
		}
		out = append(out, *d)
	}
	return out, nil
}

func (m *Manager) Get(ctx context.Context, name string) (*Domain, error) {
	if err := m.ensureDomain(ctx, name); err != nil {
		return nil, err
	}
	d := &Domain{Name: name, UpdatedAt: time.Now().Format(time.RFC3339)}

	if uuid, err := m.run(ctx, 3*time.Second, "domuuid", name); err == nil {
		d.UUID = strings.TrimSpace(uuid)
	}
	if out, err := m.run(ctx, 4*time.Second, "dominfo", name); err == nil {
		parseDomInfo(d, out)
	} else {
		return nil, err
	}
	if out, err := m.run(ctx, 5*time.Second, "domstats", name, "--state", "--cpu-total", "--balloon", "--vcpu", "--block", "--interface"); err == nil {
		parseDomStats(d, out)
	}
	if out, err := m.run(ctx, 4*time.Second, "domblklist", name, "--details"); err == nil {
		mergeBlockList(d, out)
	}
	if out, err := m.run(ctx, 4*time.Second, "dumpxml", name); err == nil {
		d.Filesystems = parseFilesystems(out)
	}
	if out, err := m.run(ctx, 4*time.Second, "domifaddr", name, "--source", "lease"); err == nil {
		d.Interfaces = parseInterfaces(out)
	}
	if strings.EqualFold(d.State, "running") {
		d.Guest = m.guestSnapshot(ctx, name)
	}
	return d, nil
}

func (m *Manager) Control(ctx context.Context, name, action string) error {
	if err := m.ensureDomain(ctx, name); err != nil {
		return err
	}
	switch action {
	case "start", "shutdown", "reboot", "destroy":
	default:
		return fmt.Errorf("unsupported VM action %q", action)
	}
	_, err := m.run(ctx, 20*time.Second, action, name)
	return err
}

func (m *Manager) Configure(ctx context.Context, name string, req ConfigUpdate) error {
	if err := m.ensureDomain(ctx, name); err != nil {
		return err
	}
	if req.VCPUs < 0 || req.MemoryMiB < 0 {
		return fmt.Errorf("vcpus and memoryMiB must be non-negative")
	}
	if req.MemoryMiB > 0 && req.MemoryMiB < 512 {
		return fmt.Errorf("memoryMiB must be at least 512")
	}
	if req.VCPUs > 128 {
		return fmt.Errorf("vcpus must be <= 128")
	}
	if req.MemoryMiB > 0 || req.VCPUs > 0 {
		if err := m.defineInactiveConfig(ctx, name, req); err != nil {
			return err
		}
	}
	if req.Autostart != nil {
		if *req.Autostart {
			if _, err := m.run(ctx, 10*time.Second, "autostart", name); err != nil {
				return err
			}
		} else {
			if _, err := m.run(ctx, 10*time.Second, "autostart", name, "--disable"); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *Manager) defineInactiveConfig(ctx context.Context, name string, req ConfigUpdate) error {
	xml, err := m.run(ctx, 10*time.Second, "dumpxml", "--inactive", name)
	if err != nil {
		return err
	}
	if req.MemoryMiB > 0 {
		kib := strconv.Itoa(req.MemoryMiB * 1024)
		xml, err = replaceOne(xml, `<memory\s+unit='KiB'>\d+</memory>`, "<memory unit='KiB'>"+kib+"</memory>")
		if err != nil {
			return err
		}
		xml, err = replaceOne(xml, `<currentMemory\s+unit='KiB'>\d+</currentMemory>`, "<currentMemory unit='KiB'>"+kib+"</currentMemory>")
		if err != nil {
			return err
		}
	}
	if req.VCPUs > 0 {
		count := strconv.Itoa(req.VCPUs)
		xml, err = replaceOne(xml, `<vcpu(?:\s+[^>]*)?>\d+</vcpu>`, "<vcpu placement='static'>"+count+"</vcpu>")
		if err != nil {
			return err
		}
	}
	tmp, err := os.CreateTemp("", "devbox-vm-*.xml")
	if err != nil {
		return err
	}
	path := tmp.Name()
	defer os.Remove(path)
	if _, err := tmp.WriteString(xml); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	_, err = m.run(ctx, 10*time.Second, "define", path)
	return err
}

func replaceOne(in, pattern, repl string) (string, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", err
	}
	if !re.MatchString(in) {
		return "", fmt.Errorf("domain XML field not found for pattern %s", pattern)
	}
	return re.ReplaceAllString(in, repl), nil
}

func (m *Manager) ensureDomain(ctx context.Context, name string) error {
	if strings.TrimSpace(name) == "" || strings.Contains(name, "/") {
		return fmt.Errorf("invalid domain name")
	}
	names, err := m.domainNames(ctx)
	if err != nil {
		return err
	}
	for _, n := range names {
		if n == name {
			return nil
		}
	}
	return fmt.Errorf("domain %q not found", name)
}

func (m *Manager) domainNames(ctx context.Context) ([]string, error) {
	out, err := m.run(ctx, 4*time.Second, "list", "--all", "--name")
	if err != nil {
		return nil, err
	}
	var names []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			names = append(names, line)
		}
	}
	return names, nil
}

func (m *Manager) run(ctx context.Context, timeout time.Duration, args ...string) (string, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, m.virsh, args...)
	b, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return string(b), ctx.Err()
	}
	if err != nil {
		return string(b), fmt.Errorf("%s: %w", strings.TrimSpace(string(b)), err)
	}
	return string(b), nil
}

func parseDomInfo(d *Domain, out string) {
	for _, line := range strings.Split(out, "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		switch strings.TrimSpace(k) {
		case "State":
			d.State = v
		case "CPU(s)":
			d.VCPUs, _ = strconv.Atoi(v)
		case "CPU time":
			d.CPUTimeSec = parseFloat(strings.TrimSuffix(v, "s"))
		case "Max memory":
			d.Memory.MaxKiB = parseFirstUint(v)
		case "Used memory":
			d.Memory.UsedKiB = parseFirstUint(v)
		case "Persistent":
			d.Persistent = strings.EqualFold(v, "yes")
		case "Autostart":
			d.Autostart = strings.EqualFold(v, "enable") || strings.EqualFold(v, "yes")
		}
	}
}

func parseDomStats(d *Domain, out string) {
	stats := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if ok {
			stats[k] = v
		}
	}
	if v := stats["balloon.current"]; v != "" {
		d.Memory.ActualKiB = parseUint(v)
	}
	if v := stats["balloon.available"]; v != "" {
		d.Memory.AvailableKiB = parseUint(v)
	}
	if v := stats["balloon.usable"]; v != "" {
		d.Memory.UsableKiB = parseUint(v)
	}
	if v := stats["balloon.unused"]; v != "" {
		d.Memory.UnusedKiB = parseUint(v)
	}
	if v := stats["balloon.rss"]; v != "" {
		d.Memory.RSSKiB = parseUint(v)
	}
	count := int(parseUint(stats["block.count"]))
	disks := make([]Disk, 0, count)
	for i := 0; i < count; i++ {
		p := fmt.Sprintf("block.%d.", i)
		disks = append(disks, Disk{
			Target:        stats[p+"name"],
			Source:        stats[p+"path"],
			CapacityBytes: parseUint(stats[p+"capacity"]),
			Allocation:    parseUint(stats[p+"allocation"]),
			PhysicalBytes: parseUint(stats[p+"physical"]),
			ReadBytes:     parseUint(stats[p+"rd.bytes"]),
			WriteBytes:    parseUint(stats[p+"wr.bytes"]),
		})
	}
	if len(disks) > 0 {
		d.Disks = disks
	}
}

func mergeBlockList(d *Domain, out string) {
	byTarget := map[string]*Disk{}
	for i := range d.Disks {
		byTarget[d.Disks[i].Target] = &d.Disks[i]
	}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 4 || f[0] == "Type" || strings.HasPrefix(f[0], "-") {
			continue
		}
		target := f[2]
		source := strings.Join(f[3:], " ")
		if source == "-" {
			source = ""
		}
		if disk := byTarget[target]; disk != nil {
			disk.Type = f[0]
			disk.Device = f[1]
			if disk.Source == "" {
				disk.Source = source
			}
		} else {
			d.Disks = append(d.Disks, Disk{Type: f[0], Device: f[1], Target: target, Source: source})
		}
	}
}

func parseFilesystems(out string) []Filesystem {
	var dom struct {
		Devices struct {
			Filesystems []struct {
				Type       string `xml:"type,attr"`
				AccessMode string `xml:"accessmode,attr"`
				Driver     struct {
					Type string `xml:"type,attr"`
				} `xml:"driver"`
				Source struct {
					Dir string `xml:"dir,attr"`
				} `xml:"source"`
				Target struct {
					Dir string `xml:"dir,attr"`
				} `xml:"target"`
			} `xml:"filesystem"`
		} `xml:"devices"`
	}
	if err := xml.Unmarshal([]byte(out), &dom); err != nil {
		return nil
	}
	filesystems := make([]Filesystem, 0, len(dom.Devices.Filesystems))
	for _, fs := range dom.Devices.Filesystems {
		target := strings.TrimSpace(fs.Target.Dir)
		source := strings.TrimSpace(fs.Source.Dir)
		if target == "" && source == "" {
			continue
		}
		filesystems = append(filesystems, Filesystem{
			Target:     target,
			Source:     source,
			Type:       strings.TrimSpace(fs.Type),
			AccessMode: strings.TrimSpace(fs.AccessMode),
			Driver:     strings.TrimSpace(fs.Driver.Type),
		})
	}
	return filesystems
}

func parseInterfaces(out string) []Interface {
	var list []Interface
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 4 || f[0] == "Name" || strings.HasPrefix(f[0], "-") {
			continue
		}
		list = append(list, Interface{Name: f[0], MAC: f[1], Protocol: f[2], Address: f[3]})
	}
	return list
}

func (m *Manager) guestSnapshot(ctx context.Context, name string) *GuestSnapshot {
	snap := &GuestSnapshot{}
	cmd := "cat /proc/loadavg; free -b; cat /proc/pressure/memory; printf 'DEVBOX_MOUNTS\\n'; findmnt -rn -t virtiofs -o SOURCE,TARGET,FSTYPE 2>/dev/null"
	req := map[string]any{
		"execute": "guest-exec",
		"arguments": map[string]any{
			"path":           "/bin/sh",
			"arg":            []string{"-lc", cmd},
			"capture-output": true,
		},
	}
	b, _ := json.Marshal(req)
	out, err := m.run(ctx, 3*time.Second, "qemu-agent-command", name, string(b))
	if err != nil {
		snap.Error = err.Error()
		return snap
	}
	var start struct {
		Return struct {
			PID int `json:"pid"`
		} `json:"return"`
	}
	if err := json.Unmarshal([]byte(out), &start); err != nil || start.Return.PID == 0 {
		snap.Error = "guest agent did not return an exec pid"
		return snap
	}
	for i := 0; i < 10; i++ {
		time.Sleep(150 * time.Millisecond)
		statusReq := fmt.Sprintf(`{"execute":"guest-exec-status","arguments":{"pid":%d}}`, start.Return.PID)
		out, err = m.run(ctx, 3*time.Second, "qemu-agent-command", name, statusReq)
		if err != nil {
			snap.Error = err.Error()
			return snap
		}
		var status struct {
			Return struct {
				Exited   bool   `json:"exited"`
				ExitCode int    `json:"exitcode"`
				OutData  string `json:"out-data"`
				ErrData  string `json:"err-data"`
			} `json:"return"`
		}
		if json.Unmarshal([]byte(out), &status) != nil || !status.Return.Exited {
			continue
		}
		snap.AgentOK = true
		if status.Return.ExitCode != 0 {
			snap.Error = decodeB64(status.Return.ErrData)
			return snap
		}
		parseGuestOutput(snap, decodeB64(status.Return.OutData))
		return snap
	}
	snap.Error = "guest command timed out"
	return snap
}

func parseGuestOutput(s *GuestSnapshot, out string) {
	lines := strings.Split(out, "\n")
	if len(lines) > 0 {
		f := strings.Fields(lines[0])
		for i := 0; i < len(f) && i < 3; i++ {
			s.LoadAverage = append(s.LoadAverage, parseFloat(f[i]))
		}
	}
	inMounts := false
	for _, line := range lines {
		if strings.TrimSpace(line) == "DEVBOX_MOUNTS" {
			inMounts = true
			continue
		}
		f := strings.Fields(line)
		if inMounts {
			if len(f) >= 3 {
				s.Mounts = append(s.Mounts, GuestMount{Source: f[0], Target: f[1], FSType: f[2]})
			}
			continue
		}
		if len(f) >= 7 && f[0] == "Mem:" {
			s.Memory = &GuestMem{
				TotalBytes:     parseUint(f[1]),
				UsedBytes:      parseUint(f[2]),
				FreeBytes:      parseUint(f[3]),
				SharedBytes:    parseUint(f[4]),
				BuffCacheBytes: parseUint(f[5]),
				AvailableBytes: parseUint(f[6]),
			}
		}
		if len(f) >= 4 && f[0] == "Swap:" && s.Memory != nil {
			s.Memory.SwapTotalBytes = parseUint(f[1])
			s.Memory.SwapUsedBytes = parseUint(f[2])
			s.Memory.SwapFreeBytes = parseUint(f[3])
		}
		if len(f) >= 4 && (f[0] == "some" || f[0] == "full") {
			p := Pressure{Kind: f[0]}
			for _, field := range f[1:] {
				k, v, ok := strings.Cut(field, "=")
				if !ok {
					continue
				}
				switch k {
				case "avg10":
					p.Avg10 = parseFloat(v)
				case "avg60":
					p.Avg60 = parseFloat(v)
				case "avg300":
					p.Avg300 = parseFloat(v)
				}
			}
			s.MemoryPressure = append(s.MemoryPressure, p)
		}
	}
}

func parseFirstUint(s string) uint64 {
	f := strings.Fields(s)
	if len(f) == 0 {
		return 0
	}
	return parseUint(f[0])
}

func parseUint(s string) uint64 {
	v, _ := strconv.ParseUint(strings.TrimSpace(s), 10, 64)
	return v
}

func parseFloat(s string) float64 {
	v, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return v
}

func decodeB64(s string) string {
	if s == "" {
		return ""
	}
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return s
	}
	return string(b)
}
