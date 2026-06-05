package console

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/theriseunion/edge-ota/agent/pkg/alerts"
	"github.com/theriseunion/edge-ota/agent/pkg/apps"
	"github.com/theriseunion/edge-ota/agent/pkg/auth"
	"github.com/theriseunion/edge-ota/agent/pkg/collector"
	"github.com/theriseunion/edge-ota/agent/pkg/files"
	"github.com/theriseunion/edge-ota/agent/pkg/models"
	"github.com/theriseunion/edge-ota/agent/pkg/supervisor"
	"go.uber.org/zap"
)

// Config 控制台 HTTP 服务器配置
type Config struct {
	Enabled            bool   `mapstructure:"enabled"`
	Port               int    `mapstructure:"port"`
	StaticDir          string `mapstructure:"static_dir"`
	SupervisorSocket   string `mapstructure:"supervisor_socket"`
	SupervisorConfDir  string `mapstructure:"supervisor_conf_dir"`
}

// Server 本地控制台 HTTP 服务器
type Server struct {
	config        Config
	logger        *zap.Logger
	mux           *http.ServeMux
	collector     *collector.Collector
	appManager    *apps.Manager
	storeManager  *apps.StoreManager
	fileBrowser   *files.Browser
	modelScanner  *models.Scanner
	alertEngine   *alerts.Engine
	auth          *auth.Auth
	supervisorMgr *supervisor.Manager
}

// NewServer 创建控制台服务器
func NewServer(logger *zap.Logger, cfg Config, col *collector.Collector, appMgr *apps.Manager, storeMgr *apps.StoreManager) *Server {
	s := &Server{
		config:        cfg,
		logger:        logger,
		mux:           http.NewServeMux(),
		collector:     col,
		appManager:    appMgr,
		storeManager:  storeMgr,
		fileBrowser:   files.NewBrowser(files.Config{}),
		modelScanner:  models.NewScanner(models.Config{}),
		alertEngine:   alerts.NewEngine(col),
		supervisorMgr: supervisor.NewManager(cfg.SupervisorSocket, cfg.SupervisorConfDir, logger),
	}
	s.registerRoutes()
	return s
}

// Start 启动 HTTP 服务器（阻塞，应在 goroutine 中调用）
func (s *Server) Start(ctx context.Context) error {
	addr := fmt.Sprintf(":%d", s.config.Port)
	srv := &http.Server{
		Addr:              addr,
		Handler:           s.mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// 监听 context 取消，优雅关闭
	go func() {
		<-ctx.Done()
		s.logger.Info("Shutting down console HTTP server...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutdownCtx)
	}()

	s.logger.Info("Console HTTP server starting",
		zap.String("addr", addr),
		zap.String("static_dir", s.config.StaticDir))

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}

	// 打印实际监听地址（端口 0 时有用）
	s.logger.Info("Console HTTP server listening",
		zap.String("addr", ln.Addr().String()))

	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("HTTP server error: %w", err)
	}
	return nil
}

func (s *Server) registerRoutes() {
	// 静态文件：优先本地目录，否则使用 embed
	var staticFS http.FileSystem
	if s.config.StaticDir != "" {
		if info, err := os.Stat(s.config.StaticDir); err == nil && info.IsDir() {
			staticFS = http.Dir(s.config.StaticDir)
			s.logger.Info("Serving console from local directory",
				zap.String("dir", s.config.StaticDir))
		} else {
			s.logger.Warn("Configured static_dir not found, falling back to embedded",
				zap.String("dir", s.config.StaticDir),
				zap.Error(err))
		}
	}

	if staticFS == nil {
		sub, err := fs.Sub(embeddedStatic, "dist")
		if err != nil {
			s.logger.Fatal("Failed to access embedded static files", zap.Error(err))
		}
		staticFS = http.FS(sub)
		s.logger.Info("Serving console from embedded static files")
	}

	// API 路由
	s.mux.HandleFunc("/api/v1/health", s.handleHealth)
	s.mux.HandleFunc("/api/v1/device", s.handleDevice)
	s.mux.HandleFunc("/api/v1/metrics", s.handleMetrics)
	s.mux.HandleFunc("/api/v1/metrics/history", s.handleMetricsHistory)

	// 终端执行路由
	s.mux.HandleFunc("/api/v1/terminal/exec", s.handleTerminalExec)
	// 网络信息
	s.mux.HandleFunc("/api/v1/network", s.handleNetwork)

	// 应用商店路由
	s.mux.HandleFunc("/api/v1/store/apps", s.handleStoreApps)
	// 代理应用图标到 apiserver
	s.mux.HandleFunc("/app-icons/", s.handleProxyAppIcons)

	// 应用管理路由
	s.registerAppRoutes()

	// 文件/模型/告警/端口路由
	s.registerExtraRoutes()

	// Supervisor 路由
	s.registerSupervisorRoutes()

	// 认证路由
	s.registerAuthRoutes()

	// 静态文件兜底
	fileServer := http.FileServer(staticFS)
	s.mux.Handle("/", fileServer)
}

// --- JSON helpers ---

func (s *Server) jsonOK(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

// --- Handlers ---

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	s.jsonOK(w, map[string]string{
		"status": "ok",
		"time":   time.Now().Format(time.RFC3339),
	})
}

func (s *Server) handleDevice(w http.ResponseWriter, r *http.Request) {
	if s.collector == nil {
		http.Error(w, "collector not initialized", http.StatusServiceUnavailable)
		return
	}
	s.jsonOK(w, s.collector.GetDeviceInfo())
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if s.collector == nil {
		http.Error(w, "collector not initialized", http.StatusServiceUnavailable)
		return
	}
	s.jsonOK(w, s.collector.GetCurrentMetrics())
}

func (s *Server) handleMetricsHistory(w http.ResponseWriter, r *http.Request) {
	if s.collector == nil {
		http.Error(w, "collector not initialized", http.StatusServiceUnavailable)
		return
	}
	s.jsonOK(w, s.collector.GetMetricsHistory())
}
