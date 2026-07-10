package console

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/a2d2-dev/devbox/pkg/system"
)

// registerPromRoutes 注册 Prometheus 抓取端点。
//
// 路径用标准 /metrics（不在 /api/v1/ 下 → authGate 天然放行，抓取器无需带
// session）。指标遵循 Prometheus 惯例：暴露累计 counter 原值，速率由服务端
// rate() 计算 —— 与 node_exporter 的 node_disk_* 语义对齐，前缀 devbox_ 区分。
func (s *Server) registerPromRoutes() {
	s.mux.HandleFunc("/metrics", s.handlePromMetrics)
}

func (s *Server) handlePromMetrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	var b strings.Builder

	// 磁盘 I/O 累计计数器（/proc/diskstats，整盘设备，含物理盘标记）
	counters := system.ReadDiskCounters()
	writeHelp(&b, "devbox_disk_read_bytes_total", "counter", "Total bytes read from the block device")
	for _, c := range counters {
		fmt.Fprintf(&b, "devbox_disk_read_bytes_total{%s} %d\n", diskLabels(c), c.ReadBytes)
	}
	writeHelp(&b, "devbox_disk_written_bytes_total", "counter", "Total bytes written to the block device")
	for _, c := range counters {
		fmt.Fprintf(&b, "devbox_disk_written_bytes_total{%s} %d\n", diskLabels(c), c.WriteBytes)
	}
	writeHelp(&b, "devbox_disk_reads_completed_total", "counter", "Total read I/Os completed")
	for _, c := range counters {
		fmt.Fprintf(&b, "devbox_disk_reads_completed_total{%s} %d\n", diskLabels(c), c.ReadIOs)
	}
	writeHelp(&b, "devbox_disk_writes_completed_total", "counter", "Total write I/Os completed")
	for _, c := range counters {
		fmt.Fprintf(&b, "devbox_disk_writes_completed_total{%s} %d\n", diskLabels(c), c.WriteIOs)
	}
	writeHelp(&b, "devbox_disk_io_time_seconds_total", "counter", "Total seconds the device spent doing I/O (util = rate of this)")
	for _, c := range counters {
		fmt.Fprintf(&b, "devbox_disk_io_time_seconds_total{%s} %.3f\n", diskLabels(c), float64(c.IOTimeMs)/1000)
	}

	// 文件系统容量 gauge（statfs，真实块设备挂载点）
	fs := system.ListFilesystemUsage()
	writeHelp(&b, "devbox_filesystem_size_bytes", "gauge", "Filesystem total size in bytes")
	for _, f := range fs {
		fmt.Fprintf(&b, "devbox_filesystem_size_bytes{%s} %d\n", fsLabels(f), f.SizeBytes)
	}
	writeHelp(&b, "devbox_filesystem_used_bytes", "gauge", "Filesystem used bytes")
	for _, f := range fs {
		fmt.Fprintf(&b, "devbox_filesystem_used_bytes{%s} %d\n", fsLabels(f), f.UsedBytes)
	}

	w.Write([]byte(b.String()))
}

func writeHelp(b *strings.Builder, name, typ, help string) {
	fmt.Fprintf(b, "# HELP %s %s\n# TYPE %s %s\n", name, help, name, typ)
}

func diskLabels(c system.DiskCounterSnapshot) string {
	l := fmt.Sprintf(`device=%q,physical="%t"`, c.Device, c.Physical)
	if c.Alias != "" {
		l += fmt.Sprintf(`,alias=%q`, c.Alias)
	}
	return l
}

func fsLabels(f system.FilesystemUsage) string {
	return fmt.Sprintf(`device=%q,mountpoint=%q,fstype=%q`, f.Device, f.MountPoint, f.FSType)
}
