package console

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/a2d2-dev/devbox/pkg/aiactivity"
	"github.com/a2d2-dev/devbox/pkg/system"
)

// registerSystemRoutes 注册系统级查询路由。
func (s *Server) registerSystemRoutes() {
	s.registerDesktopStatusRoutes()
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
	// devbox 无云端 / 无集中审计的存根，避免前端 404
	s.mux.HandleFunc("/api/v1/cloud/status", s.handleCloudStatus)
	s.mux.HandleFunc("/api/v1/audit/events", s.handleAuditEvents)
	s.mux.HandleFunc("/api/v1/about", s.handleAbout)
}

func (s *Server) handleProcesses(w http.ResponseWriter, r *http.Request) {
	list, err := system.ListProcesses()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.jsonOK(w, list)
}

func (s *Server) handleProcessDetail(w http.ResponseWriter, r *http.Request) {
	pidStr := strings.TrimPrefix(r.URL.Path, "/api/v1/processes/")
	// 支持 /processes/{pid}/action 或直接 /processes/{pid}
	pidStr = strings.SplitN(pidStr, "/", 2)[0]
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		http.Error(w, "invalid pid", http.StatusBadRequest)
		return
	}
	d, err := system.GetProcessDetail(pid)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	s.jsonOK(w, d)
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

// devbox 无集中审计：始终返回空事件列表，前端页面自然显示"暂无数据"。
func (s *Server) handleAuditEvents(w http.ResponseWriter, r *http.Request) {
	s.jsonOK(w, map[string]any{
		"events": []any{},
		"total":  0,
	})
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
