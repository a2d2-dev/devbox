package console

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strings"

	"go.uber.org/zap"
)

// registerExtraRoutes 注册文件/模型/告警/端口路由
func (s *Server) registerExtraRoutes() {
	s.registerFileRoutes()
	s.mux.HandleFunc("/api/v1/models", s.handleModels)
	s.mux.HandleFunc("/api/v1/alerts", s.handleAlerts)
	s.mux.HandleFunc("/api/v1/alerts/", s.handleAlertAction)
	s.mux.HandleFunc("/api/v1/ports", s.handlePorts)
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	if s.modelScanner == nil {
		http.Error(w, "model scanner not initialized", http.StatusServiceUnavailable)
		return
	}
	s.jsonOK(w, s.modelScanner.Scan())
}

func (s *Server) handleAlerts(w http.ResponseWriter, r *http.Request) {
	if s.alertEngine == nil {
		s.jsonOK(w, []interface{}{})
		return
	}
	s.jsonOK(w, s.alertEngine.GetAlerts())
}

func (s *Server) handleAlertAction(w http.ResponseWriter, r *http.Request) {
	if s.alertEngine == nil {
		http.Error(w, "alert engine not initialized", http.StatusServiceUnavailable)
		return
	}
	// /api/v1/alerts/{id}/ack
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	s.alertEngine.Ack(req.ID)
	s.jsonOK(w, map[string]string{"status": "ok"})
}

func (s *Server) handlePorts(w http.ResponseWriter, r *http.Request) {
	type PortInfo struct {
		Proto   string `json:"protocol"`
		Local   string `json:"local"`
		Port    int    `json:"port"`
		State   string `json:"state"`
		Process string `json:"process"`
		PID     int    `json:"pid"`
	}

	// 用 ss -tlnp 获取本机监听端口
	out, err := exec.CommandContext(r.Context(), "ss", "-tlnp").Output()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var ports []PortInfo
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.HasPrefix(line, "LISTEN") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}

		local := fields[3]
		// 解析 addr:port
		var addr string
		var port int
		if idx := strings.LastIndex(local, ":"); idx >= 0 {
			addr = local[:idx]
			fmt.Sscanf(local[idx+1:], "%d", &port)
		}
		if port == 0 {
			continue
		}

		// 解析进程名
		process := ""
		pid := 0
		if len(fields) >= 6 {
			procField := fields[5]
			// users:(("nginx",pid=1234,fd=6))
			if i := strings.Index(procField, "((\""); i >= 0 {
				end := strings.Index(procField[i+3:], "\"")
				if end > 0 {
					process = procField[i+3 : i+3+end]
				}
			}
			if i := strings.Index(procField, "pid="); i >= 0 {
				fmt.Sscanf(procField[i+4:], "%d", &pid)
			}
		}

		// 过滤 loopback 上的内部端口（可选）
		state := "listening"
		if addr == "127.0.0.1" || addr == "::1" {
			state = "local-only"
		}

		ports = append(ports, PortInfo{
			Proto:   "TCP",
			Local:   addr,
			Port:    port,
			State:   state,
			Process: process,
			PID:     pid,
		})
	}
	s.jsonOK(w, ports)
}

func (s *Server) handleStoreApps(w http.ResponseWriter, r *http.Request) {
	if s.storeManager == nil {
		s.jsonOK(w, []interface{}{})
		return
	}
	apps, err := s.storeManager.ListStoreApps(r.Context())
	if err != nil {
		s.logger.Error("Failed to list store apps", zap.Error(err))
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.jsonOK(w, apps)
}
