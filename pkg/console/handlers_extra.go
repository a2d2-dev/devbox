package console

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/a2d2-dev/devbox/pkg/auth"
	"go.uber.org/zap"
)

// registerExtraRoutes 注册文件/模型/告警/端口路由
func (s *Server) registerExtraRoutes() {
	s.mux.HandleFunc("/api/v1/files", s.handleFiles)
	s.mux.HandleFunc("/api/v1/files/upload", s.handleFileUpload)
	s.mux.HandleFunc("/api/v1/files/content", s.handleFileContent)
	s.mux.HandleFunc("/api/v1/models", s.handleModels)
	s.mux.HandleFunc("/api/v1/alerts", s.handleAlerts)
	s.mux.HandleFunc("/api/v1/alerts/", s.requireAdminWrites(s.handleAlertAction))
	s.mux.HandleFunc("/api/v1/ports", s.handlePorts)
}

func (s *Server) handleFiles(w http.ResponseWriter, r *http.Request) {
	if s.fileBrowser == nil {
		http.Error(w, "file browser not initialized", http.StatusServiceUnavailable)
		return
	}
	path := r.URL.Query().Get("path")
	allowed, err := s.authorizedFilePaths(r, path, true)
	if err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	entries, err := s.fileBrowser.List(path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if allowed != nil {
		filtered := entries[:0]
		for _, entry := range entries {
			if pathWithinAny(entry.AbsPath, allowed, true) {
				filtered = append(filtered, entry)
			}
		}
		entries = filtered
	}
	s.jsonOK(w, entries)
}

// handleFileUpload 接收 multipart form (path, name, file)，把文件写到当前目录。
// 供 Files 页面「粘贴图片」使用，20MB 上限够贴截图，超出返 413。
func (s *Server) handleFileUpload(w http.ResponseWriter, r *http.Request) {
	if s.fileBrowser == nil {
		http.Error(w, "file browser not initialized", http.StatusServiceUnavailable)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	const maxUpload = 20 << 20
	r.Body = http.MaxBytesReader(w, r.Body, maxUpload)
	if err := r.ParseMultipartForm(maxUpload); err != nil {
		http.Error(w, err.Error(), http.StatusRequestEntityTooLarge)
		return
	}

	path := r.FormValue("path")
	if _, err := s.authorizedFilePaths(r, path, false); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	name := r.FormValue("name")

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "missing file field", http.StatusBadRequest)
		return
	}
	defer file.Close()

	if name == "" && header != nil {
		name = header.Filename
	}

	data, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	saved, err := s.fileBrowser.Save(path, name, data)
	if err != nil {
		msg := err.Error()
		switch {
		case strings.Contains(msg, "file exists"):
			http.Error(w, msg, http.StatusConflict)
		case strings.Contains(msg, "access denied"):
			http.Error(w, msg, http.StatusForbidden)
		case strings.Contains(msg, "invalid filename"), strings.Contains(msg, "invalid path"),
			strings.Contains(msg, "not a directory"), strings.Contains(msg, "failed to stat directory"):
			http.Error(w, msg, http.StatusBadRequest)
		default:
			http.Error(w, msg, http.StatusInternalServerError)
		}
		return
	}
	s.jsonOK(w, map[string]string{"name": saved})
}

// handleFileContent 直出 dirPath/name 指向的文件，给 Files 页预览用。
// http.ServeFile 自带 Content-Type / Range 处理，图片/PDF/文本都能直接展。
func (s *Server) handleFileContent(w http.ResponseWriter, r *http.Request) {
	if s.fileBrowser == nil {
		http.Error(w, "file browser not initialized", http.StatusServiceUnavailable)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if _, err := s.authorizedFilePaths(r, r.URL.Query().Get("path"), false); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	full, err := s.fileBrowser.ResolveFile(r.URL.Query().Get("path"), r.URL.Query().Get("name"))
	if err != nil {
		msg := err.Error()
		switch {
		case strings.Contains(msg, "access denied"):
			http.Error(w, msg, http.StatusForbidden)
		case strings.Contains(msg, "file not found"):
			http.Error(w, msg, http.StatusNotFound)
		case strings.Contains(msg, "invalid"), strings.Contains(msg, "not a regular file"):
			http.Error(w, msg, http.StatusBadRequest)
		default:
			http.Error(w, msg, http.StatusInternalServerError)
		}
		return
	}
	http.ServeFile(w, r, full)
}

func (s *Server) authorizedFilePaths(r *http.Request, requested string, allowAncestor bool) ([]string, error) {
	if s.users == nil {
		return nil, nil
	}
	p, ok := auth.PrincipalFromContext(r.Context())
	if !ok || p.IsAdmin() || p.Legacy {
		return nil, nil
	}
	paths, err := s.users.AllowedPaths(r.Context(), p.UserID)
	if err != nil {
		return nil, err
	}
	work := s.config.WorkDir
	if work == "" {
		work = "/data"
	}
	target := requested
	if target == "" {
		target = work
	} else if !filepath.IsAbs(target) {
		target = filepath.Join(work, target)
	}
	target = filepath.Clean(target)
	realTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		return nil, errors.New("access denied: path cannot be resolved")
	}
	realPaths := make([]string, 0, len(paths))
	for _, path := range paths {
		real, err := filepath.EvalSymlinks(path)
		if err != nil {
			return nil, errors.New("access denied: assigned path cannot be resolved")
		}
		realPaths = append(realPaths, real)
	}
	if !pathWithinAny(realTarget, realPaths, allowAncestor) {
		return nil, errors.New("access denied: path is not assigned to this account")
	}
	return realPaths, nil
}

func pathWithinAny(path string, roots []string, allowAncestor bool) bool {
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false
	}
	path = real
	for _, root := range roots {
		root, err = filepath.EvalSymlinks(root)
		if err != nil {
			continue
		}
		if path == root || strings.HasPrefix(path, root+string(filepath.Separator)) {
			return true
		}
		if allowAncestor && strings.HasPrefix(root, path+string(filepath.Separator)) {
			return true
		}
	}
	return false
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
