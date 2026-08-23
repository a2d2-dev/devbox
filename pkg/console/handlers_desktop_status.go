package console

import (
	"net/http"
	"os"
)

func (s *Server) registerDesktopStatusRoutes() {
	s.mux.HandleFunc("/api/v1/desktop/status/cpu", s.handleDesktopCPU)
	s.mux.HandleFunc("/api/v1/desktop/status/memory", s.handleDesktopMemory)
	s.mux.HandleFunc("/api/v1/desktop/status/network", s.handleDesktopNetwork)
	s.mux.HandleFunc("/api/v1/desktop/status/storage", s.handleDesktopStorage)
	s.mux.HandleFunc("/api/v1/desktop/status/uptime", s.handleDesktopUptime)
}

func (s *Server) desktopStatusMetrics(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return false
	}
	if s.collector == nil {
		http.Error(w, "collector not initialized", http.StatusServiceUnavailable)
		return false
	}
	return true
}

func (s *Server) handleDesktopCPU(w http.ResponseWriter, r *http.Request) {
	if !s.desktopStatusMetrics(w, r) {
		return
	}
	metrics := s.collector.GetCurrentMetrics()
	s.jsonOK(w, map[string]interface{}{"percent": metrics.CPUUsedPercent, "updatedAt": metrics.ShotTime})
}

func (s *Server) handleDesktopMemory(w http.ResponseWriter, r *http.Request) {
	if !s.desktopStatusMetrics(w, r) {
		return
	}
	m := s.collector.GetCurrentMetrics()
	s.jsonOK(w, map[string]interface{}{"percent": m.MemoryUsedPercent, "used": m.MemoryUsed, "total": m.MemoryTotal, "updatedAt": m.ShotTime})
}

func (s *Server) handleDesktopNetwork(w http.ResponseWriter, r *http.Request) {
	if !s.desktopStatusMetrics(w, r) {
		return
	}
	m := s.collector.GetCurrentMetrics()
	s.jsonOK(w, map[string]interface{}{"sent": m.NetBytesSent, "received": m.NetBytesRecv, "updatedAt": m.ShotTime})
}

func (s *Server) handleDesktopStorage(w http.ResponseWriter, r *http.Request) {
	if !s.desktopStatusMetrics(w, r) {
		return
	}
	m := s.collector.GetCurrentMetrics()
	var read, write float64
	for _, disk := range m.DiskIO {
		read += disk.ReadBytesPerSec
		write += disk.WriteBytesPerSec
	}
	workDir := s.config.WorkDir
	if workDir == "" {
		workDir = "/data"
	}
	configured := false
	if info, err := os.Stat(workDir); err == nil && info.IsDir() {
		configured = true
	}
	s.jsonOK(w, map[string]interface{}{"readBytesPerSec": read, "writeBytesPerSec": write, "configured": configured, "updatedAt": m.ShotTime})
}

func (s *Server) handleDesktopUptime(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.collector == nil {
		http.Error(w, "collector not initialized", http.StatusServiceUnavailable)
		return
	}
	device := s.collector.GetDeviceInfo()
	s.jsonOK(w, map[string]interface{}{"seconds": device.Uptime, "human": device.UptimeHuman})
}
