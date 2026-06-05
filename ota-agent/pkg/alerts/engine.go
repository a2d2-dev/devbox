package alerts

import (
	"fmt"
	"time"

	"github.com/theriseunion/edge-ota/agent/pkg/collector"
)

// AlertInfo 告警信息
type AlertInfo struct {
	ID       string   `json:"id"`
	Severity string   `json:"severity"` // "critical" | "warning" | "info"
	Title    string   `json:"title"`
	Target   string   `json:"target"`
	Time     string   `json:"time"`
	State    string   `json:"state"` // "active" | "recovered"
	Detail   string   `json:"detail"`
	Suggest  []string `json:"suggest,omitempty"`
}

// Engine 告警引擎
type Engine struct {
	col    *collector.Collector
	acked  map[string]bool // 已确认的告警 ID
	alerts []AlertInfo
}

// NewEngine 创建告警引擎
func NewEngine(col *collector.Collector) *Engine {
	return &Engine{
		col:   col,
		acked: make(map[string]bool),
	}
}

// Evaluate 评估当前指标生成告警
func (e *Engine) Evaluate() []AlertInfo {
	if e.col == nil {
		return nil
	}

	m := e.col.GetCurrentMetrics()
	now := time.Now().Format("15:04")
	var alerts []AlertInfo

	// CPU 高负载
	if m.CPUUsedPercent > 90 {
		alerts = append(alerts, AlertInfo{
			ID: "cpu-high", Severity: "warning",
			Title:  "CPU 使用率过高",
			Target: "系统 · CPU",
			Time:   now, State: "active",
			Detail:  fmt.Sprintf("CPU 当前使用率 %.1f%%，超过 90%% 阈值", m.CPUUsedPercent),
			Suggest: []string{"检查是否有异常进程", "考虑停止非关键应用"},
		})
	}

	// 内存不足
	if m.MemoryUsedPercent > 85 {
		alerts = append(alerts, AlertInfo{
			ID: "mem-high", Severity: "warning",
			Title:  "内存使用率过高",
			Target: "系统 · 内存",
			Time:   now, State: "active",
			Detail:  fmt.Sprintf("内存当前使用 %.1f%%", m.MemoryUsedPercent),
			Suggest: []string{"释放缓存", "停止低优先级应用"},
		})
	}

	// 磁盘空间不足
	for _, d := range m.DiskData {
		if d.UsedPercent > 85 {
			alerts = append(alerts, AlertInfo{
				ID: "disk-" + d.Path, Severity: "warning",
				Title:  "磁盘空间不足",
				Target: "存储 · " + d.Path,
				Time:   now, State: "active",
				Detail:  fmt.Sprintf("%s 已使用 %.1f%%", d.Path, d.UsedPercent),
				Suggest: []string{"清理临时文件", "删除旧的 checkpoint", "扩展存储"},
			})
		}
	}

	// GPU 温度过高
	for _, g := range m.GPUData {
		if g.Temperature > 85 {
			alerts = append(alerts, AlertInfo{
				ID: fmt.Sprintf("gpu-temp-%d", g.Index), Severity: "warning",
				Title:  fmt.Sprintf("GPU %d 温度过高", g.Index),
				Target: g.ProductName,
				Time:   now, State: "active",
				Detail:  fmt.Sprintf("温度 %d°C，超过 85°C 阈值", g.Temperature),
				Suggest: []string{"检查散热", "降低 GPU 负载"},
			})
		}
	}

	// 过滤已确认的
	var active []AlertInfo
	for _, a := range alerts {
		if !e.acked[a.ID] {
			active = append(active, a)
		}
	}

	e.alerts = active
	return active
}

// Ack 确认告警
func (e *Engine) Ack(id string) {
	e.acked[id] = true
}

// GetAlerts 获取当前告警
func (e *Engine) GetAlerts() []AlertInfo {
	return e.Evaluate()
}
