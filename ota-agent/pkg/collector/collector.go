package collector

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"
)

const (
	collectInterval = 5 * time.Second
	historySize     = 30 // 保留最近 30 个采集点
)

// Collector 系统指标采集器
type Collector struct {
	logger       *zap.Logger
	agentVersion string

	mu          sync.RWMutex
	deviceInfo  DeviceInfo
	current     SystemMetrics
	prevCPU     *cpuSample
	history     []SystemMetrics // 环形缓冲区
	historyIdx  int
	historyFull bool
}

// New 创建采集器
func New(logger *zap.Logger, agentVersion string) *Collector {
	return &Collector{
		logger:       logger,
		agentVersion: agentVersion,
		history:      make([]SystemMetrics, historySize),
	}
}

// Start 启动采集循环（阻塞，应在 goroutine 中调用）
func (c *Collector) Start(ctx context.Context) {
	// 初次采集设备信息
	c.mu.Lock()
	c.deviceInfo = collectDeviceInfo(c.agentVersion)
	// 初始 CPU 样本（用于计算 diff）
	sample := readCPUSample()
	c.prevCPU = &sample
	c.mu.Unlock()

	c.logger.Info("Collector started",
		zap.String("hostname", c.deviceInfo.Hostname),
		zap.Int("cpu_cores", c.deviceInfo.CPUCores))

	// 首次采集
	c.collect()

	ticker := time.NewTicker(collectInterval)
	defer ticker.Stop()

	// 设备信息刷新（uptime 等）每分钟一次
	deviceTicker := time.NewTicker(60 * time.Second)
	defer deviceTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			c.logger.Info("Collector stopped")
			return
		case <-ticker.C:
			c.collect()
		case <-deviceTicker.C:
			c.mu.Lock()
			c.deviceInfo = collectDeviceInfo(c.agentVersion)
			c.mu.Unlock()
		}
	}
}

func (c *Collector) collect() {
	c.mu.Lock()
	defer c.mu.Unlock()

	m := collectSystemMetrics(c.prevCPU)

	// GPU
	m.GPUData = collectGPU()

	// 更新 CPU 样本
	sample := readCPUSample()
	c.prevCPU = &sample

	// 存入当前值
	c.current = m

	// 存入历史
	c.history[c.historyIdx] = m
	c.historyIdx = (c.historyIdx + 1) % historySize
	if c.historyIdx == 0 {
		c.historyFull = true
	}
}

// GetDeviceInfo 获取设备静态信息
func (c *Collector) GetDeviceInfo() DeviceInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.deviceInfo
}

// GetCurrentMetrics 获取当前系统指标
func (c *Collector) GetCurrentMetrics() SystemMetrics {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.current
}

// GetMetricsHistory 获取指标历史
func (c *Collector) GetMetricsHistory() MetricsHistory {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var count int
	if c.historyFull {
		count = historySize
	} else {
		count = c.historyIdx
	}

	h := MetricsHistory{
		Timestamps:    make([]time.Time, 0, count),
		CPUPercent:    make([]float64, 0, count),
		MemoryPercent: make([]float64, 0, count),
		DiskPercent:   make([]float64, 0, count),
		GPUUtil:       make([]float64, 0, count),
		GPUMemPercent: make([]float64, 0, count),
		NetBytesSent:  make([]uint64, 0, count),
		NetBytesRecv:  make([]uint64, 0, count),
	}

	// 按时间顺序读取
	start := 0
	if c.historyFull {
		start = c.historyIdx
	}

	for i := 0; i < count; i++ {
		idx := (start + i) % historySize
		m := c.history[idx]

		h.Timestamps = append(h.Timestamps, m.ShotTime)
		h.CPUPercent = append(h.CPUPercent, m.CPUUsedPercent)
		h.MemoryPercent = append(h.MemoryPercent, m.MemoryUsedPercent)

		// 主分区磁盘使用率
		if len(m.DiskData) > 0 {
			h.DiskPercent = append(h.DiskPercent, m.DiskData[0].UsedPercent)
		} else {
			h.DiskPercent = append(h.DiskPercent, 0)
		}

		// GPU 0
		if len(m.GPUData) > 0 {
			h.GPUUtil = append(h.GPUUtil, m.GPUData[0].GPUUtil)
			if m.GPUData[0].MemTotal > 0 {
				h.GPUMemPercent = append(h.GPUMemPercent, float64(m.GPUData[0].MemUsed)/float64(m.GPUData[0].MemTotal)*100)
			} else {
				h.GPUMemPercent = append(h.GPUMemPercent, 0)
			}
		} else {
			h.GPUUtil = append(h.GPUUtil, 0)
			h.GPUMemPercent = append(h.GPUMemPercent, 0)
		}

		h.NetBytesSent = append(h.NetBytesSent, m.NetBytesSent)
		h.NetBytesRecv = append(h.NetBytesRecv, m.NetBytesRecv)
	}

	return h
}
