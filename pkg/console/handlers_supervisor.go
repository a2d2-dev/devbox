package console

import (
	"encoding/json"
	"net/http"
	"strings"
)

// registerSupervisorRoutes registers supervisor-related API routes
func (s *Server) registerSupervisorRoutes() {
	s.mux.HandleFunc("/api/v1/supervisor/status", s.handleSupervisorStatus)
	s.mux.HandleFunc("/api/v1/supervisor/services/", s.requireAdminWrites(s.handleSupervisorService))
}

func (s *Server) handleSupervisorStatus(w http.ResponseWriter, r *http.Request) {
	if s.supervisorMgr == nil {
		http.Error(w, "supervisor not available", http.StatusServiceUnavailable)
		return
	}

	status, err := s.supervisorMgr.GetStatus()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.jsonOK(w, status)
}

// handleSupervisorService handles:
//
//	GET  /api/v1/supervisor/services/{name}/logs
//	POST /api/v1/supervisor/services/{name}/control
func (s *Server) handleSupervisorService(w http.ResponseWriter, r *http.Request) {
	if s.supervisorMgr == nil {
		http.Error(w, "supervisor not available", http.StatusServiceUnavailable)
		return
	}

	// Parse path: /api/v1/supervisor/services/{name}/{action}
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/supervisor/services/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) != 2 || parts[0] == "" {
		http.Error(w, "invalid path, expected /api/v1/supervisor/services/{name}/{logs|control}", http.StatusBadRequest)
		return
	}

	name := parts[0]
	action := parts[1]

	switch action {
	case "logs":
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		logResp, err := s.supervisorMgr.GetServiceLogs(name)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.jsonOK(w, logResp)

	case "control":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Action string `json:"action"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if req.Action != "start" && req.Action != "stop" && req.Action != "restart" {
			http.Error(w, "action must be start, stop, or restart", http.StatusBadRequest)
			return
		}
		if err := s.supervisorMgr.ControlService(name, req.Action); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.jsonOK(w, map[string]string{"status": "ok"})

	default:
		http.Error(w, "unknown action: "+action, http.StatusNotFound)
	}
}
