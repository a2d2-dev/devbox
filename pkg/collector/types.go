package collector

import "time"

// DeviceInfo 设备静态信息（不频繁变化）
type DeviceInfo struct {
	Hostname      string `json:"hostname"`
	OS            string `json:"os"`
	Platform      string `json:"platform"`
	KernelArch    string `json:"kernelArch"`
	KernelVersion string `json:"kernelVersion"`
	CPUModel      string `json:"cpuModel"`
	CPUCores      int    `json:"cpuCores"`
	MemoryTotal   uint64 `json:"memoryTotal"` // bytes
	Uptime        uint64 `json:"uptime"`      // seconds
	UptimeHuman   string `json:"uptimeHuman"`
	IP            string `json:"ip"`
	AgentVersion  string `json:"agentVersion"`
}

// SystemMetrics 系统实时指标
type SystemMetrics struct {
	// CPU
	CPUUsedPercent float64   `json:"cpuUsedPercent"`
	CPUPercent     []float64 `json:"cpuPercent"` // 每核使用率

	// Memory
	MemoryTotal       uint64  `json:"memoryTotal"`     // bytes
	MemoryUsed        uint64  `json:"memoryUsed"`      // bytes
	MemoryAvailable   uint64  `json:"memoryAvailable"` // bytes
	MemoryUsedPercent float64 `json:"memoryUsedPercent"`

	// Disk
	DiskData       []DiskInfo   `json:"diskData"`
	DiskReadBytes  uint64       `json:"diskReadBytes"`
	DiskWriteBytes uint64       `json:"diskWriteBytes"`
	DiskIO         []DiskIOInfo `json:"diskIO"`

	// GPU
	GPUData []GPUInfo `json:"gpuData"`

	// Load
	Load1  float64 `json:"load1"`
	Load5  float64 `json:"load5"`
	Load15 float64 `json:"load15"`

	// Network IO
	NetBytesSent uint64 `json:"netBytesSent"`
	NetBytesRecv uint64 `json:"netBytesRecv"`

	// Timestamp
	ShotTime time.Time `json:"shotTime"`
}

// DiskInfo 磁盘信息（参考 1Panel dto.DiskInfo）
type DiskInfo struct {
	Path        string  `json:"path"`
	Device      string  `json:"device"`
	Type        string  `json:"type"`
	Total       uint64  `json:"total"` // bytes
	Free        uint64  `json:"free"`  // bytes
	Used        uint64  `json:"used"`  // bytes
	UsedPercent float64 `json:"usedPercent"`
}

// DiskIOInfo 物理盘实时 I/O 指标（由 /proc/diskstats 差分计算）
type DiskIOInfo struct {
	Name              string   `json:"name"`
	Path              string   `json:"path"`
	Model             string   `json:"model,omitempty"`
	Rotational        bool     `json:"rotational"`
	Kind              string   `json:"kind"` // HDD / SSD
	ReadBytes         uint64   `json:"readBytes"`
	WriteBytes        uint64   `json:"writeBytes"`
	ReadBytesPerSec   float64  `json:"readBytesPerSec"`
	WriteBytesPerSec  float64  `json:"writeBytesPerSec"`
	ReadIOPS          float64  `json:"readIops"`
	WriteIOPS         float64  `json:"writeIops"`
	UtilPercent       float64  `json:"utilPercent"`
	AvgAwaitMs        float64  `json:"avgAwaitMs"`
	ReadAwaitMs       float64  `json:"readAwaitMs"`
	WriteAwaitMs      float64  `json:"writeAwaitMs"`
	AvgQueueSize      float64  `json:"avgQueueSize"`
	InFlight          uint64   `json:"inFlight"`
	TemperatureC      *float64 `json:"temperatureC,omitempty"`
	TemperatureStatus string   `json:"temperatureStatus"` // available / unsupported
}

// GPUInfo GPU 信息（参考 1Panel dto.GPUInfo + nvidia-smi 字段）
type GPUInfo struct {
	Index       int     `json:"index"`
	ProductName string  `json:"productName"`
	GPUUtil     float64 `json:"gpuUtil"`     // 使用率 %
	MemUsed     uint64  `json:"memUsed"`     // MiB
	MemTotal    uint64  `json:"memTotal"`    // MiB
	Temperature int     `json:"temperature"` // °C
	PowerDraw   float64 `json:"powerDraw"`   // W
	PowerLimit  float64 `json:"powerLimit"`  // W
	FanSpeed    int     `json:"fanSpeed"`    // %
}

// MetricsHistory 指标历史（环形缓冲区快照）
type MetricsHistory struct {
	Timestamps     []time.Time `json:"timestamps"`
	CPUPercent     []float64   `json:"cpuPercent"`
	MemoryPercent  []float64   `json:"memoryPercent"`
	DiskPercent    []float64   `json:"diskPercent"` // 主分区
	DiskReadBytes  []uint64    `json:"diskReadBytes"`
	DiskWriteBytes []uint64    `json:"diskWriteBytes"`
	GPUUtil        []float64   `json:"gpuUtil"`       // GPU 0
	GPUMemPercent  []float64   `json:"gpuMemPercent"` // GPU 0
	NetBytesSent   []uint64    `json:"netBytesSent"`
	NetBytesRecv   []uint64    `json:"netBytesRecv"`
}
