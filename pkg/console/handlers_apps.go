package console

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/a2d2-dev/devbox/pkg/apps"
)

// registerAppRoutes 注册应用管理路由
func (s *Server) registerAppRoutes() {
	s.mux.HandleFunc("/api/v1/apps", s.handleApps)
	s.mux.HandleFunc("/api/v1/apps/", s.handleAppByID)
}

func (s *Server) handleApps(w http.ResponseWriter, r *http.Request) {
	// devbox 无 K8s 时 appManager==nil 是常态，不是错误。
	// 返回空 list + 200，避免全局 useApps() 轮询在每个页面刷屏 503 error。
	// 前端拿到空数组自然把"云端应用"段渲染成空。
	if s.appManager == nil {
		s.jsonOK(w, []any{})
		return
	}

	list, err := s.appManager.ListApps(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.jsonOK(w, list)
}

func (s *Server) handleAppByID(w http.ResponseWriter, r *http.Request) {
	if s.appManager == nil {
		http.Error(w, "app manager not initialized", http.StatusServiceUnavailable)
		return
	}

	// Parse: /api/v1/apps/{id} or /api/v1/apps/{id}/{action}
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/apps/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) == 0 || parts[0] == "" {
		http.Error(w, "app id required", http.StatusBadRequest)
		return
	}

	appID := parts[0]
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}

	switch {
	case action == "" && r.Method == http.MethodGet:
		s.handleGetApp(w, r, appID)
	case action == "" && r.Method == http.MethodDelete:
		s.handleDeleteApp(w, r, appID)
	case action == "start" && r.Method == http.MethodPost:
		s.handleStartApp(w, r, appID)
	case action == "stop" && r.Method == http.MethodPost:
		s.handleStopApp(w, r, appID)
	case action == "restart" && r.Method == http.MethodPost:
		s.handleRestartApp(w, r, appID)
	case action == "logs" && r.Method == http.MethodGet:
		s.handleAppLogs(w, r, appID)
	case action == "op" && r.Method == http.MethodPost:
		s.handleAppOp(w, r, appID)
	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
}

func (s *Server) handleGetApp(w http.ResponseWriter, r *http.Request, id string) {
	app, err := s.appManager.GetApp(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	s.jsonOK(w, app)
}

func (s *Server) handleStartApp(w http.ResponseWriter, r *http.Request, id string) {
	if err := s.appManager.StartApp(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.jsonOK(w, map[string]string{"status": "ok", "operation": "start", "app": id})
}

func (s *Server) handleStopApp(w http.ResponseWriter, r *http.Request, id string) {
	if err := s.appManager.StopApp(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.jsonOK(w, map[string]string{"status": "ok", "operation": "stop", "app": id})
}

func (s *Server) handleRestartApp(w http.ResponseWriter, r *http.Request, id string) {
	if err := s.appManager.RestartApp(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.jsonOK(w, map[string]string{"status": "ok", "operation": "restart", "app": id})
}

func (s *Server) handleDeleteApp(w http.ResponseWriter, r *http.Request, id string) {
	if err := s.appManager.DeleteApp(r.Context(), id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.jsonOK(w, map[string]string{"status": "ok", "operation": "delete", "app": id})
}

func (s *Server) handleAppLogs(w http.ResponseWriter, r *http.Request, id string) {
	tailStr := r.URL.Query().Get("tail")
	tail := int64(100)
	if tailStr != "" {
		if v, err := strconv.ParseInt(tailStr, 10, 64); err == nil && v > 0 {
			tail = v
		}
	}

	logs, err := s.appManager.GetLogs(r.Context(), id, tail)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.jsonOK(w, map[string]string{"app": id, "logs": logs})
}

// handleAppOp 统一操作入口（参考 1Panel apps/installed/op）
func (s *Server) handleAppOp(w http.ResponseWriter, r *http.Request, id string) {
	var req apps.AppOperation
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	var err error
	switch req.Operation {
	case "start":
		err = s.appManager.StartApp(r.Context(), id)
	case "stop":
		err = s.appManager.StopApp(r.Context(), id)
	case "restart":
		err = s.appManager.RestartApp(r.Context(), id)
	default:
		http.Error(w, "unknown operation: "+req.Operation, http.StatusBadRequest)
		return
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.jsonOK(w, map[string]string{"status": "ok", "operation": req.Operation, "app": id})
}
