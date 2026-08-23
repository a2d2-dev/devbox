package console

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/a2d2-dev/devbox/pkg/aiactivity"
	eventlog "github.com/a2d2-dev/devbox/pkg/syslog"
	"github.com/a2d2-dev/devbox/pkg/system"
)

// registerSystemRoutes 注册系统级查询路由。
func (s *Server) registerSystemRoutes() {
	s.mux.HandleFunc("/api/v1/processes", s.handleProcesses)
	s.mux.HandleFunc("/api/v1/processes/", s.handleProcessDetail)
	s.mux.HandleFunc("/api/v1/disks", s.handleDisks)
	s.mux.HandleFunc("/api/v1/disks/io", s.handleDiskIO)
	s.mux.HandleFunc("/api/v1/network/connections", s.handleNetworkConnections)
	s.mux.HandleFunc("/api/v1/gpu/processes", s.handleGPUProcesses)
	s.mux.HandleFunc("/api/v1/metrics/history/gpu", s.handleGPUHistory)
	s.mux.HandleFunc("/api/v1/ai/activity", s.handleAIActivity)
	s.mux.HandleFunc("/api/v1/ai/transcript", s.handleAITranscript)
	s.mux.HandleFunc("/api/v1/ai/codex/cleanup-stale", s.handleCodexCleanupStale)
	s.registerVMRoutes()
	// devbox 无云端；日志中心复用本机统一结构化事件存储。
	s.mux.HandleFunc("/api/v1/cloud/status", s.handleCloudStatus)
	s.mux.HandleFunc("/api/v1/audit/events", s.handleAuditEvents)
	s.mux.HandleFunc("/api/v1/about", s.handleAbout)
}

func (s *Server) handleProcesses(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	list, err := system.ListProcesses()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if s.processResources == nil {
		s.processResources = newProcessResourceSampler()
	}
	s.jsonOK(w, s.processResources.sample(r.Context(), list))
}

func (s *Server) handleProcessDetail(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/processes/"), "/")
	parts := strings.Split(path, "/")
	pidStr := parts[0]
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		http.Error(w, "invalid pid", http.StatusBadRequest)
		return
	}
	if len(parts) == 2 && parts[1] == "terminate" {
		s.handleTerminateProcess(w, r, pid)
		return
	}
	if len(parts) != 1 || r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	d, err := system.GetProcessDetail(pid)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if connections, err := system.ListNetworkConnections(r.Context(), 5000); err == nil {
		for _, connection := range connections {
			if connection.PID == pid {
				d.NetConns = append(d.NetConns, connection)
			}
		}
	}
	s.jsonOK(w, d)
}

var processExists = func(pid int) bool {
	_, err := os.Stat("/proc/" + strconv.Itoa(pid))
	return err == nil
}

var terminateProcess = func(pid int) error { return syscall.Kill(pid, syscall.SIGTERM) }

func (s *Server) handleTerminateProcess(w http.ResponseWriter, r *http.Request, pid int) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.auth == nil || !s.auth.Enabled() {
		writeJSONErrStatus(w, http.StatusForbidden, map[string]any{
			"error": "终止进程前必须启用控制台密码认证", "reason": "permission_required",
		})
		return
	}
	if !s.auth.ValidateToken(r.Header.Get("Authorization")) {
		writeJSONErrStatus(w, http.StatusUnauthorized, map[string]any{"error": "身份验证失败", "reason": "unauthorized"})
		return
	}
	if pid <= 1 || pid == os.Getpid() {
		writeJSONErrStatus(w, http.StatusForbidden, map[string]any{
			"error": "不允许终止系统关键进程或 DevBox 自身", "reason": "protected_process",
		})
		return
	}
	if !processExists(pid) {
		writeJSONErrStatus(w, http.StatusNotFound, map[string]any{"error": "进程不存在或已退出", "reason": "process_not_found"})
		return
	}
	err := terminateProcess(pid)
	if err != nil {
		status := http.StatusInternalServerError
		reason := "signal_failed"
		if errors.Is(err, syscall.ESRCH) {
			status, reason = http.StatusNotFound, "process_not_found"
		} else if errors.Is(err, syscall.EPERM) {
			status, reason = http.StatusForbidden, "permission_denied"
		}
		s.recordEvent(r, eventlog.Input{
			Level: "error", Module: "process", Event: "终止进程失败", EventType: "PROCESS_TERMINATE",
			Outcome: "failure", ResourceKind: "process", ResourceID: strconv.Itoa(pid), Payload: map[string]any{"reason": reason},
		})
		writeJSONErrStatus(w, status, map[string]any{"error": "终止进程失败", "reason": reason})
		return
	}
	s.recordEvent(r, eventlog.Input{
		Level: "warning", Module: "process", Event: "终止进程", EventType: "PROCESS_TERMINATE",
		Outcome: "success", ResourceKind: "process", ResourceID: strconv.Itoa(pid), Payload: map[string]any{"signal": "SIGTERM"},
	})
	s.jsonStatus(w, http.StatusAccepted, map[string]any{"status": "terminating", "pid": pid, "signal": "SIGTERM"})
}

func (s *Server) handleDisks(w http.ResponseWriter, r *http.Request) {
	list, err := system.ListDisks(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.jsonOK(w, list)
}

// handleDiskIO iotop 式实时 I/O：设备级读写速率 + Top 进程磁盘读写。
func (s *Server) handleDiskIO(w http.ResponseWriter, r *http.Request) {
	io, err := system.GetDiskIO(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.jsonOK(w, io)
}

func (s *Server) handleNetworkConnections(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	list, err := system.ListNetworkConnections(r.Context(), limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.jsonOK(w, list)
}

func (s *Server) handleGPUProcesses(w http.ResponseWriter, r *http.Request) {
	s.jsonOK(w, system.ListGPUProcesses(r.Context()))
}

func (s *Server) handleGPUHistory(w http.ResponseWriter, r *http.Request) {
	if s.gpuHistory == nil {
		s.jsonOK(w, []any{})
		return
	}
	dur := parseWindow(r.URL.Query().Get("window"))
	s.jsonOK(w, s.gpuHistory.Window(dur))
}

func (s *Server) handleAIActivity(w http.ResponseWriter, r *http.Request) {
	snap, err := aiactivity.Collect(aiactivity.Config{})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.jsonOK(w, snap)
}

func (s *Server) handleAITranscript(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	tail, _ := strconv.Atoi(r.URL.Query().Get("tail"))
	out, err := aiactivity.ReadTranscriptTail(r.URL.Query().Get("path"), tail)
	if err != nil {
		if errors.Is(err, aiactivity.ErrTranscriptForbidden) {
			http.Error(w, "transcript path forbidden", http.StatusForbidden)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.jsonOK(w, out)
}

func (s *Server) handleCodexCleanupStale(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.jsonOK(w, aiactivity.CleanupStaleCodexDeletedCWD())
}

// devbox 无云端：始终 offline，前端把"云端在线"badge 显示为离线。
func (s *Server) handleCloudStatus(w http.ResponseWriter, r *http.Request) {
	s.jsonOK(w, map[string]any{
		"cloudOnline":  false,
		"lastSyncTime": nil,
		"reason":       "devbox 定位为本地管理工具，未连云端",
	})
}

func (s *Server) handleAuditEvents(w http.ResponseWriter, r *http.Request) {
	if s.systemLog == nil {
		http.Error(w, "system log unavailable", http.StatusServiceUnavailable)
		return
	}
	switch r.Method {
	case http.MethodGet:
		query, err := parseEventQuery(r)
		if err != nil {
			writeJSONErrStatus(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		s.jsonOK(w, s.systemLog.Query(query))
	case http.MethodDelete:
		if s.auth == nil || !s.auth.Enabled() || !s.auth.ValidateToken(r.Header.Get("Authorization")) {
			writeJSONErrStatus(w, http.StatusForbidden, map[string]any{"error": "清空日志需要已启用的控制台认证", "reason": "permission_required"})
			return
		}
		cleared, event, err := s.systemLog.Clear(s.actorFromRequest(r), requestIP(r), r.UserAgent())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.jsonOK(w, map[string]any{"cleared": cleared, "auditEvent": event})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func parseEventQuery(r *http.Request) (eventlog.Query, error) {
	values := r.URL.Query()
	limit, err := parseOptionalInt(values.Get("limit"), 50)
	if err != nil || limit < 1 || limit > 200 {
		return eventlog.Query{}, fmt.Errorf("limit must be between 1 and 200")
	}
	offset, err := parseOptionalInt(values.Get("offset"), 0)
	if err != nil || offset < 0 {
		return eventlog.Query{}, fmt.Errorf("offset must be zero or greater")
	}
	query := eventlog.Query{
		Levels: values["level"], Modules: values["module"], Username: strings.TrimSpace(values.Get("user")),
		Limit: limit, Offset: offset,
	}
	if since := values.Get("since"); since != "" {
		query.Since, err = time.Parse(time.RFC3339, since)
		if err != nil {
			return eventlog.Query{}, fmt.Errorf("since must be RFC3339")
		}
	}
	if until := values.Get("until"); until != "" {
		query.Until, err = time.Parse(time.RFC3339, until)
		if err != nil {
			return eventlog.Query{}, fmt.Errorf("until must be RFC3339")
		}
	}
	return query, nil
}

func parseOptionalInt(value string, fallback int) (int, error) {
	if value == "" {
		return fallback, nil
	}
	return strconv.Atoi(value)
}

// 前端 LoginScreen 遗留调用 /api/v1/about，用 device 数据兜底一份让它别 404。
func (s *Server) handleAbout(w http.ResponseWriter, r *http.Request) {
	// 复用 collector 的 device 数据（跟 /api/v1/device 一样）
	if s.collector == nil {
		s.jsonOK(w, map[string]any{})
		return
	}
	dev := s.collector.GetDeviceInfo()
	b, _ := json.Marshal(dev)
	// 也带上 agent 版本 / uptime 已在 dev 里
	w.Header().Set("Content-Type", "application/json")
	w.Write(b)
}

// parseWindow 解析 "1h" / "30m" / "6h" 这种；空/非法时默认 1h。
func parseWindow(s string) time.Duration {
	if s == "" {
		return time.Hour
	}
	if d, err := time.ParseDuration(s); err == nil {
		return d
	}
	return time.Hour
}
