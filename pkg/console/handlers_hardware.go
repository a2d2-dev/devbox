package console

import (
	"net/http"

	"github.com/a2d2-dev/devbox/pkg/sensors"
)

// registerHardwareRoutes registers the hardware snapshot APIs.
//   /api/v1/hardware         — 静态硬件清单 (60s 缓存)
//   /api/v1/hardware/sensors — 动态传感器 (温度/风扇/RAPL 功耗, 无缓存, ~200ms)
func (s *Server) registerHardwareRoutes() {
	s.mux.HandleFunc("/api/v1/hardware", s.handleHardware)
	s.mux.HandleFunc("/api/v1/hardware/sensors", s.handleHardwareSensors)
}

func (s *Server) handleHardware(w http.ResponseWriter, r *http.Request) {
	if s.hardware == nil {
		http.Error(w, "hardware collector not initialised", http.StatusServiceUnavailable)
		return
	}
	snap := s.hardware.Get(r.Context())
	s.jsonOK(w, snap)
}

func (s *Server) handleHardwareSensors(w http.ResponseWriter, r *http.Request) {
	snap := sensors.Collect(r.Context())
	s.jsonOK(w, snap)
}
