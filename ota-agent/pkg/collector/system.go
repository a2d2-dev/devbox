package collector

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// collectSystemMetrics 采集系统实时指标
func collectSystemMetrics(prevCPU *cpuSample) SystemMetrics {
	m := SystemMetrics{
		ShotTime: time.Now(),
	}

	// CPU
	cur := readCPUSample()
	if prevCPU != nil {
		m.CPUUsedPercent = calcCPUPercent(*prevCPU, cur)
	}
	m.CPUPercent = readPerCoreCPU()

	// Memory
	m.MemoryTotal, m.MemoryUsed, m.MemoryAvailable = readMemory()
	if m.MemoryTotal > 0 {
		m.MemoryUsedPercent = float64(m.MemoryUsed) / float64(m.MemoryTotal) * 100
	}

	// Disk
	m.DiskData = readDisks()

	// Load average
	m.Load1, m.Load5, m.Load15 = readLoadAvg()

	// Network IO
	m.NetBytesSent, m.NetBytesRecv = readNetIO()

	return m
}

// --- CPU ---

type cpuSample struct {
	user, nice, system, idle, iowait, irq, softirq, steal uint64
}

func (s cpuSample) total() uint64 {
	return s.user + s.nice + s.system + s.idle + s.iowait + s.irq + s.softirq + s.steal
}

func (s cpuSample) busy() uint64 {
	return s.total() - s.idle - s.iowait
}

func readCPUSample() cpuSample {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return cpuSample{}
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "cpu ") {
			return parseCPULine(line)
		}
	}
	return cpuSample{}
}

func parseCPULine(line string) cpuSample {
	fields := strings.Fields(line)
	if len(fields) < 8 {
		return cpuSample{}
	}
	var s cpuSample
	s.user, _ = strconv.ParseUint(fields[1], 10, 64)
	s.nice, _ = strconv.ParseUint(fields[2], 10, 64)
	s.system, _ = strconv.ParseUint(fields[3], 10, 64)
	s.idle, _ = strconv.ParseUint(fields[4], 10, 64)
	if len(fields) > 5 {
		s.iowait, _ = strconv.ParseUint(fields[5], 10, 64)
	}
	if len(fields) > 6 {
		s.irq, _ = strconv.ParseUint(fields[6], 10, 64)
	}
	if len(fields) > 7 {
		s.softirq, _ = strconv.ParseUint(fields[7], 10, 64)
	}
	if len(fields) > 8 {
		s.steal, _ = strconv.ParseUint(fields[8], 10, 64)
	}
	return s
}

func calcCPUPercent(prev, cur cpuSample) float64 {
	totalDelta := cur.total() - prev.total()
	if totalDelta == 0 {
		return 0
	}
	busyDelta := cur.busy() - prev.busy()
	return float64(busyDelta) / float64(totalDelta) * 100
}

func readPerCoreCPU() []float64 {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return nil
	}
	var percents []float64
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "cpu") && !strings.HasPrefix(line, "cpu ") {
			s := parseCPULine(line)
			total := s.total()
			if total > 0 {
				percents = append(percents, float64(s.busy())/float64(total)*100)
			}
		}
	}
	return percents
}

// --- Memory ---

func readMemory() (total, used, available uint64) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return
	}
	var free, buffers, cached uint64
	for _, line := range strings.Split(string(data), "\n") {
		var val uint64
		switch {
		case strings.HasPrefix(line, "MemTotal:"):
			fmt.Sscanf(line, "MemTotal: %d kB", &val)
			total = val * 1024
		case strings.HasPrefix(line, "MemFree:"):
			fmt.Sscanf(line, "MemFree: %d kB", &val)
			free = val * 1024
		case strings.HasPrefix(line, "MemAvailable:"):
			fmt.Sscanf(line, "MemAvailable: %d kB", &val)
			available = val * 1024
		case strings.HasPrefix(line, "Buffers:"):
			fmt.Sscanf(line, "Buffers: %d kB", &val)
			buffers = val * 1024
		case strings.HasPrefix(line, "Cached:"):
			fmt.Sscanf(line, "Cached: %d kB", &val)
			cached = val * 1024
		}
	}
	if available == 0 {
		available = free + buffers + cached
	}
	used = total - available
	return
}

// --- Disk ---

func readDisks() []DiskInfo {
	data, err := os.ReadFile("/proc/mounts")
	if err != nil {
		return nil
	}

	seen := make(map[string]bool)
	var disks []DiskInfo

	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		device, mountPoint, fsType := fields[0], fields[1], fields[2]

		// 只统计真实文件系统
		switch fsType {
		case "ext4", "ext3", "xfs", "btrfs", "vfat", "ntfs", "zfs":
		default:
			continue
		}
		if seen[device] {
			continue
		}
		seen[device] = true

		var stat syscall.Statfs_t
		if err := syscall.Statfs(mountPoint, &stat); err != nil {
			continue
		}

		total := stat.Blocks * uint64(stat.Bsize)
		free := stat.Bavail * uint64(stat.Bsize)
		used := total - free
		var pct float64
		if total > 0 {
			pct = float64(used) / float64(total) * 100
		}

		disks = append(disks, DiskInfo{
			Path:        mountPoint,
			Device:      device,
			Type:        fsType,
			Total:       total,
			Free:        free,
			Used:        used,
			UsedPercent: pct,
		})
	}
	return disks
}

// --- Load average ---

func readLoadAvg() (load1, load5, load15 float64) {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return
	}
	fmt.Sscanf(string(data), "%f %f %f", &load1, &load5, &load15)
	return
}

// --- Network IO ---

func readNetIO() (sent, recv uint64) {
	data, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, ":") || strings.HasPrefix(line, "Inter") || strings.HasPrefix(line, "face") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) < 2 {
			continue
		}
		iface := strings.TrimSpace(parts[0])
		if iface == "lo" {
			continue
		}
		fields := strings.Fields(parts[1])
		if len(fields) < 9 {
			continue
		}
		r, _ := strconv.ParseUint(fields[0], 10, 64)
		s, _ := strconv.ParseUint(fields[8], 10, 64)
		recv += r
		sent += s
	}
	return
}
