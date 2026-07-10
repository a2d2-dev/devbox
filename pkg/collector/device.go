package collector

import (
	"fmt"
	"net"
	"os"
	"runtime"
	"strings"
	"time"
)

// collectDeviceInfo 采集设备静态信息
func collectDeviceInfo(agentVersion string) DeviceInfo {
	info := DeviceInfo{
		Hostname:     getHostname(),
		KernelArch:   runtime.GOARCH,
		CPUCores:     runtime.NumCPU(),
		IP:           getLocalIP(),
		AgentVersion: agentVersion,
	}

	// OS 信息
	info.OS, info.Platform, info.KernelVersion = getOSInfo()

	// CPU 型号
	info.CPUModel = getCPUModel()

	// 内存总量
	info.MemoryTotal = getMemoryTotal()

	// Uptime
	info.Uptime = getUptime()
	info.UptimeHuman = formatUptime(info.Uptime)

	return info
}

func getHostname() string {
	h, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return h
}

func getLocalIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "127.0.0.1"
	}
	defer conn.Close()
	return conn.LocalAddr().(*net.UDPAddr).IP.String()
}

func getOSInfo() (osName, platform, kernelVersion string) {
	osName = runtime.GOOS

	// /etc/os-release
	if data, err := os.ReadFile("/etc/os-release"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "PRETTY_NAME=") {
				platform = strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), `"`)
			}
			if strings.HasPrefix(line, "ID=") {
				if osName == "linux" {
					osName = strings.Trim(strings.TrimPrefix(line, "ID="), `"`)
				}
			}
		}
	}

	// kernel version
	if data, err := os.ReadFile("/proc/version"); err == nil {
		fields := strings.Fields(string(data))
		if len(fields) >= 3 {
			kernelVersion = fields[2]
		}
	}

	return
}

func getCPUModel() string {
	data, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return "unknown"
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "model name") || strings.HasPrefix(line, "Model") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				return strings.TrimSpace(parts[1])
			}
		}
	}
	return "unknown"
}

func getMemoryTotal() uint64 {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "MemTotal:") {
			var val uint64
			fmt.Sscanf(line, "MemTotal: %d kB", &val)
			return val * 1024 // kB → bytes
		}
	}
	return 0
}

func getUptime() uint64 {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0
	}
	var uptime float64
	fmt.Sscanf(string(data), "%f", &uptime)
	return uint64(uptime)
}

func formatUptime(seconds uint64) string {
	d := time.Duration(seconds) * time.Second
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	mins := int(d.Minutes()) % 60

	if days > 0 {
		return fmt.Sprintf("%d 天 %d 小时 %d 分钟", days, hours, mins)
	}
	if hours > 0 {
		return fmt.Sprintf("%d 小时 %d 分钟", hours, mins)
	}
	return fmt.Sprintf("%d 分钟", mins)
}
