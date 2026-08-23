package collector

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// collectSystemMetrics 采集系统实时指标
func collectSystemMetrics(prevCPU *cpuSample, prevPerCoreCPU []cpuSample, prevDiskIO map[string]diskIOSample, prevDiskIOTime time.Time) SystemMetrics {
	m := SystemMetrics{
		ShotTime: time.Now(),
	}

	// CPU
	cur := readCPUSample()
	if prevCPU != nil {
		m.CPUUsedPercent = calcCPUPercent(*prevCPU, cur)
	}
	m.CPUPercent = calcPerCoreCPUPercent(prevPerCoreCPU, readPerCoreCPUSamples())

	// Memory
	m.MemoryTotal, m.MemoryUsed, m.MemoryAvailable = readMemory()
	if m.MemoryTotal > 0 {
		m.MemoryUsedPercent = float64(m.MemoryUsed) / float64(m.MemoryTotal) * 100
	}

	// Disk
	m.DiskData = readDisks()
	curDiskIO := readDiskIOSamples()
	m.DiskReadBytes, m.DiskWriteBytes = sumDiskIOBytes(curDiskIO)
	m.DiskIO = calcDiskIO(prevDiskIO, curDiskIO, m.ShotTime.Sub(prevDiskIOTime).Seconds())

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

func readPerCoreCPUSamples() []cpuSample {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return nil
	}
	var samples []cpuSample
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "cpu") && !strings.HasPrefix(line, "cpu ") {
			s := parseCPULine(line)
			if s.total() > 0 {
				samples = append(samples, s)
			}
		}
	}
	return samples
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
	curTotal, prevTotal := cur.total(), prev.total()
	curBusy, prevBusy := cur.busy(), prev.busy()
	if curTotal < prevTotal || curBusy < prevBusy {
		return 0
	}
	totalDelta := curTotal - prevTotal
	if totalDelta == 0 {
		return 0
	}
	busyDelta := curBusy - prevBusy
	return float64(busyDelta) / float64(totalDelta) * 100
}

func calcPerCoreCPUPercent(prev, cur []cpuSample) []float64 {
	percents := make([]float64, 0, len(cur))
	for i, s := range cur {
		if i < len(prev) {
			percents = append(percents, calcCPUPercent(prev[i], s))
			continue
		}
		total := s.total()
		if total == 0 {
			percents = append(percents, 0)
			continue
		}
		percents = append(percents, float64(s.busy())/float64(total)*100)
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

type diskIOSample struct {
	name         string
	readIOs      uint64
	readSectors  uint64
	readMs       uint64
	writeIOs     uint64
	writeSectors uint64
	writeMs      uint64
	inFlight     uint64
	ioMs         uint64
	weightedIOMs uint64
}

func readDiskIOSamples() map[string]diskIOSample {
	data, err := os.ReadFile("/proc/diskstats")
	if err != nil {
		return nil
	}
	samples := make(map[string]diskIOSample)
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 14 {
			continue
		}
		name := fields[2]
		if !isWholeDiskDevice(name) {
			continue
		}
		s := diskIOSample{name: name}
		s.readIOs, _ = strconv.ParseUint(fields[3], 10, 64)
		s.readSectors, _ = strconv.ParseUint(fields[5], 10, 64)
		s.readMs, _ = strconv.ParseUint(fields[6], 10, 64)
		s.writeIOs, _ = strconv.ParseUint(fields[7], 10, 64)
		s.writeSectors, _ = strconv.ParseUint(fields[9], 10, 64)
		s.writeMs, _ = strconv.ParseUint(fields[10], 10, 64)
		s.inFlight, _ = strconv.ParseUint(fields[11], 10, 64)
		s.ioMs, _ = strconv.ParseUint(fields[12], 10, 64)
		s.weightedIOMs, _ = strconv.ParseUint(fields[13], 10, 64)
		samples[name] = s
	}
	return samples
}

func sumDiskIOBytes(samples map[string]diskIOSample) (readBytes, writeBytes uint64) {
	const sectorSize = uint64(512)
	for _, s := range samples {
		readBytes += s.readSectors * sectorSize
		writeBytes += s.writeSectors * sectorSize
	}
	return
}

func calcDiskIO(prev, cur map[string]diskIOSample, seconds float64) []DiskIOInfo {
	if seconds <= 0 {
		seconds = collectInterval.Seconds()
	}
	out := make([]DiskIOInfo, 0, len(cur))
	for name, s := range cur {
		info := DiskIOInfo{
			Name:              name,
			Path:              "/dev/" + name,
			Model:             readBlockDeviceModel(name),
			Rotational:        isRotationalDisk(name),
			ReadBytes:         s.readSectors * 512,
			WriteBytes:        s.writeSectors * 512,
			InFlight:          s.inFlight,
			TemperatureStatus: "unsupported",
		}
		if temp, ok := readBlockDeviceTemperature(name); ok {
			info.TemperatureC = &temp
			info.TemperatureStatus = "available"
		}
		if info.Rotational {
			info.Kind = "HDD"
		} else {
			info.Kind = "SSD"
		}
		p, ok := prev[name]
		if ok {
			readIOs := deltaUint64(p.readIOs, s.readIOs)
			writeIOs := deltaUint64(p.writeIOs, s.writeIOs)
			readMs := deltaUint64(p.readMs, s.readMs)
			writeMs := deltaUint64(p.writeMs, s.writeMs)
			ioMs := deltaUint64(p.ioMs, s.ioMs)
			weightedIOMs := deltaUint64(p.weightedIOMs, s.weightedIOMs)
			readSectors := deltaUint64(p.readSectors, s.readSectors)
			writeSectors := deltaUint64(p.writeSectors, s.writeSectors)
			totalIOs := readIOs + writeIOs

			info.ReadBytesPerSec = float64(readSectors*512) / seconds
			info.WriteBytesPerSec = float64(writeSectors*512) / seconds
			info.ReadIOPS = float64(readIOs) / seconds
			info.WriteIOPS = float64(writeIOs) / seconds
			info.UtilPercent = clampPercent(float64(ioMs) / (seconds * 10))
			info.AvgQueueSize = float64(weightedIOMs) / (seconds * 1000)
			if totalIOs > 0 {
				info.AvgAwaitMs = float64(readMs+writeMs) / float64(totalIOs)
			}
			if readIOs > 0 {
				info.ReadAwaitMs = float64(readMs) / float64(readIOs)
			}
			if writeIOs > 0 {
				info.WriteAwaitMs = float64(writeMs) / float64(writeIOs)
			}
		}
		out = append(out, info)
	}
	return out
}

func readBlockDeviceTemperature(name string) (float64, bool) {
	return readBlockDeviceTemperatureAt("/sys/block", name)
}

func readBlockDeviceTemperatureAt(sysBlockRoot, name string) (float64, bool) {
	patterns := []string{
		filepath.Join(sysBlockRoot, name, "device", "hwmon", "hwmon*", "temp*_input"),
		filepath.Join(sysBlockRoot, name, "device", "device", "hwmon", "hwmon*", "temp*_input"),
	}
	for _, pattern := range patterns {
		paths, _ := filepath.Glob(pattern)
		for _, path := range paths {
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			milliC, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64)
			if err != nil {
				continue
			}
			temp := milliC / 1000
			if temp >= -50 && temp <= 200 {
				return temp, true
			}
		}
	}
	return 0, false
}

func deltaUint64(prev, cur uint64) uint64 {
	if cur < prev {
		return 0
	}
	return cur - prev
}

func clampPercent(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

func readBlockDeviceModel(name string) string {
	data, err := os.ReadFile("/sys/block/" + name + "/device/model")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func isRotationalDisk(name string) bool {
	data, err := os.ReadFile("/sys/block/" + name + "/queue/rotational")
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(data)) == "1"
}

func isWholeDiskDevice(name string) bool {
	if name == "" {
		return false
	}
	switch {
	case strings.HasPrefix(name, "loop"),
		strings.HasPrefix(name, "ram"),
		strings.HasPrefix(name, "dm-"),
		strings.HasPrefix(name, "md"):
		return false
	case strings.HasPrefix(name, "sd"), strings.HasPrefix(name, "vd"), strings.HasPrefix(name, "xvd"):
		return !isDigit(name[len(name)-1])
	case strings.HasPrefix(name, "nvme"), strings.HasPrefix(name, "mmcblk"):
		return !strings.Contains(name, "p")
	default:
		return !isDigit(name[len(name)-1])
	}
}

func isDigit(b byte) bool {
	return b >= '0' && b <= '9'
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
