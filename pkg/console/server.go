package console

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/a2d2-dev/devbox/pkg/alerts"
	"github.com/a2d2-dev/devbox/pkg/apps"
	"github.com/a2d2-dev/devbox/pkg/auth"
	"github.com/a2d2-dev/devbox/pkg/backup"
	"github.com/a2d2-dev/devbox/pkg/collector"
	"github.com/a2d2-dev/devbox/pkg/downloads"
	"github.com/a2d2-dev/devbox/pkg/files"
	"github.com/a2d2-dev/devbox/pkg/gpuhistory"
	"github.com/a2d2-dev/devbox/pkg/hardware"
	"github.com/a2d2-dev/devbox/pkg/links"
	"github.com/a2d2-dev/devbox/pkg/maintenance"
	"github.com/a2d2-dev/devbox/pkg/models"
	"github.com/a2d2-dev/devbox/pkg/network"
	"github.com/a2d2-dev/devbox/pkg/security"
	"github.com/a2d2-dev/devbox/pkg/shares"
	"github.com/a2d2-dev/devbox/pkg/supervisor"
	eventlog "github.com/a2d2-dev/devbox/pkg/syslog"
	"github.com/a2d2-dev/devbox/pkg/users"
	"github.com/a2d2-dev/devbox/pkg/vms"
	"go.uber.org/zap"
)

// Config 控制台 HTTP 服务器配置
type Config struct {
	Enabled           bool     `mapstructure:"enabled"`
	Port              int      `mapstructure:"port"`
	StaticDir         string   `mapstructure:"static_dir"`
	SupervisorSocket  string   `mapstructure:"supervisor_socket"`
	SupervisorConfDir string   `mapstructure:"supervisor_conf_dir"`
	ConsoleURL        string   `mapstructure:"console_url"`
	AuthPassword      string   `mapstructure:"auth_password"`
	AuthSessionTTL    int      `mapstructure:"auth_session_ttl"`
	TrustedProxies    []string `mapstructure:"trusted_proxies"`
	LinksPath         string   `mapstructure:"links_path"`
	// WorkDir 是文件浏览器的工作区根（chroot 语义）。留空默认 /data。
	// 前端「工作区」= 这里，path="" 落到这里，越界返 403。
	WorkDir string `mapstructure:"work_dir"`
	// AllowPrivateNetworks 显式允许下载访问私网、回环和链路本地地址。
	AllowPrivateNetworks bool `mapstructure:"allow_private_networks"`
	// AppsDir 是 Compose 受管应用文件根，由 compose.data_dir 派生。
	AppsDir string `mapstructure:"-"`
	// BrowserDataPath 浏览器应用的书签/历史 JSON 路径；空 = /etc/devbox/browser.json。
	BrowserDataPath string `mapstructure:"browser_data_path"`
	// BrowserInsecureTLS 浏览器代理是否跳过远端 TLS 校验（内网自签证书场景），默认 false。
	BrowserInsecureTLS bool `mapstructure:"browser_insecure_tls"`
	// UsersDataPath is optional for tests and custom embedding. The main service
	// stores users.db beside browser_data_path by default.
	UsersDataPath string `mapstructure:"users_data_path"`
	// BackupDataDir 保存备份任务与历史；空 = /var/lib/devbox/backup。
	BackupDataDir string `mapstructure:"backup_data_dir"`
	// BackupConcurrency 是备份与恢复共享的进程内并发上限；小于 1 时为 2。
	BackupConcurrency int `mapstructure:"backup_concurrency"`
	// BackupAllowedRoots 扩展本地备份/恢复目标允许根；WorkDir 与 /data 始终包含。
	BackupAllowedRoots []string `mapstructure:"backup_allowed_roots"`
	// SystemLogPath is the single persistent store for system and audit events.
	// Empty uses /var/lib/devbox/system-events.jsonl.
	SystemLogPath string `mapstructure:"system_log_path"`
	// Catalogs 第三方 HTTP/Git catalog source 聚合器（Issue #2 阶段4 扩展）。
	// 为 nil 表示未配置 catalog（UI 隐藏 catalog 区）。
	Catalogs             *apps.CatalogSet           `mapstructure:"-"`
	CatalogSourceManager *apps.CatalogSourceManager `mapstructure:"-"`
}

// Server 本地控制台 HTTP 服务器
type Server struct {
	config               Config
	logger               *zap.Logger
	mux                  *http.ServeMux
	collector            *collector.Collector
	controller           apps.Controller
	storeManager         *apps.StoreManager
	catalogs             *apps.CatalogSet
	catalogSourceManager *apps.CatalogSourceManager
	fileBrowser          *files.Browser
	modelScanner         *models.Scanner
	alertEngine          *alerts.Engine
	auth                 *auth.Auth
	users                *users.Store
	supervisorMgr        *supervisor.Manager
	hardware             *hardware.Collector
	links                *links.Registry
	gpuHistory           *gpuhistory.Collector
	vmManager            *vms.Manager
	browser              *browserStore // 浏览器应用的书签/历史持久化
	browserClient        *http.Client  // 浏览器代理复用的 HTTP client（剥离嵌套限制头）
	network              *network.Collector
	security             *security.Store
	bans                 *security.BanManager
	certificates         *security.CertificateManager
	loginLimiter         loginRateLimiter
	backup               *backup.Manager
	systemLog            *eventlog.Store
	processResources     *processResourceSampler
	sessionUsersMu       sync.RWMutex
	sessionUsers         map[string]string
	maintenanceStore     *maintenance.Store
	webdav               *shares.WebDAVService
	notifier             maintenance.Notifier
	restoreMu            sync.Mutex
	pendingRestores      map[string]pendingRestore
	downloadEngine       *downloads.Engine
	downloadEngineError  string
	onboarding           *onboardingStore
}

// NewServer 创建控制台服务器
func NewServer(logger *zap.Logger, cfg Config, col *collector.Collector, controller apps.Controller, storeMgr *apps.StoreManager) *Server {
	securityPath, keyPath, certDir := securityPaths()
	securityStore, securityErr := security.NewStore(securityPath, keyPath)
	if securityErr != nil {
		logger.Error("Security settings persistence unavailable; using encrypted in-memory state", zap.Error(securityErr))
		securityStore, _ = security.NewStore("", "")
	}
	if err := securityStore.InitializeHTTPPort(cfg.Port); err != nil {
		logger.Warn("Could not initialize persisted HTTP port", zap.Error(err))
	}

	usersPath := cfg.UsersDataPath
	if usersPath == "" {
		if cfg.BrowserDataPath != "" {
			usersPath = filepath.Join(filepath.Dir(cfg.BrowserDataPath), "users.db")
		} else {
			usersPath = "/etc/devbox/users.db"
		}
	}
	var userStore *users.Store
	if err := os.MkdirAll(filepath.Dir(usersPath), 0o750); err != nil {
		logger.Error("User database directory unavailable", zap.String("path", usersPath), zap.Error(err))
	} else if opened, err := users.Open(usersPath); err != nil {
		logger.Error("User database unavailable", zap.String("path", usersPath), zap.Error(err))
	} else {
		userStore = opened
	}

	logPath := strings.TrimSpace(cfg.SystemLogPath)
	if logPath == "" {
		logPath = strings.TrimSpace(os.Getenv("DEVBOX_SYSTEM_LOG_PATH"))
	}
	if logPath == "" {
		logPath = eventlog.DefaultPath
	}
	systemLog, logErr := eventlog.New(logPath)
	if logErr != nil {
		logger.Error("System log unavailable", zap.String("path", logPath), zap.Error(logErr))
	}
	backupManager, backupErr := backup.NewManager(cfg.BackupDataDir, cfg.BackupConcurrency, logger,
		backup.WithWorkDir(cfg.WorkDir), backup.WithAllowedTargetRoots(cfg.BackupAllowedRoots...))
	if backupErr != nil {
		logger.Warn("Backup manager unavailable; backup management disabled", zap.Error(backupErr))
	}
	s := &Server{
		config:               cfg,
		logger:               logger,
		mux:                  http.NewServeMux(),
		collector:            col,
		controller:           controller,
		storeManager:         storeMgr,
		catalogs:             cfg.Catalogs,
		catalogSourceManager: cfg.CatalogSourceManager,
		fileBrowser:          files.NewBrowser(files.Config{RootDir: cfg.WorkDir, AppsDir: cfg.AppsDir}),
		modelScanner:         models.NewScanner(models.Config{}),
		alertEngine:          alerts.NewEngine(col),
		users:                userStore,
		auth: auth.New(auth.Config{
			Password:        cfg.AuthPassword,
			SessionTTL:      cfg.AuthSessionTTL,
			Users:           userStore,
			UsersConfigured: true,
		}),
		supervisorMgr: supervisor.NewManager(cfg.SupervisorSocket, cfg.SupervisorConfDir, logger),
		hardware:      hardware.New(60 * time.Second),
		links:         links.New(cfg.LinksPath),
		gpuHistory:    gpuhistory.New(10*time.Second, 6*time.Hour),
		vmManager:     vms.NewManager(),
		browser:       newBrowserStore(cfg.BrowserDataPath),
		browserClient: newBrowserClient(cfg.BrowserInsecureTLS),
		network:       network.NewCollector(),
		security:      securityStore,
		bans: security.NewBanManager(security.BanRule{
			Threshold: 5, WindowSec: 600, BanMinutes: 30,
		}),
		certificates:     security.NewCertificateManager(certDir),
		systemLog:        systemLog,
		processResources: newProcessResourceSampler(),
		sessionUsers:     make(map[string]string),
		webdav:           shares.NewWebDAVService(),
		pendingRestores:  make(map[string]pendingRestore),
		onboarding:       newOnboardingStore(onboardingPath(cfg.BrowserDataPath)),
		backup:           backupManager,
	}
	if store, err := maintenance.NewStore("", cfg.WorkDir); err != nil {
		logger.Error("Maintenance settings unavailable", zap.Error(err))
	} else {
		s.maintenanceStore = store
		s.notifier = maintenance.SMTPNotifier{Config: func() maintenance.SMTPConfig {
			return store.Get().SMTP
		}}
	}
	s.installAuthSessionCleanup()
	s.installTaskAuditObserver()
	downloadEngine, err := downloads.New(downloads.Config{
		RootDir: cfg.WorkDir, MaxConcurrent: 3, AllowPrivateNetworks: cfg.AllowPrivateNetworks,
	})
	if err != nil {
		s.downloadEngineError = err.Error()
		logger.Warn("Download engine unavailable", zap.Error(err))
	} else {
		s.downloadEngine = downloadEngine
	}
	s.bans.SetProtectedFailureHook(func(ip, source string) {
		logger.Warn("Skipped ban for protected management address", zap.String("ip", ip), zap.String("source", source))
	})
	s.gpuHistory.Start(context.Background())
	s.registerRoutes()
	if s.auth.Enabled() {
		logger.Info("Local password auth enabled",
			zap.Int("session_ttl_sec", cfg.AuthSessionTTL))
	} else {
		logger.Info("Local password auth disabled (no password configured)")
	}
	return s
}

func securityPaths() (string, string, string) {
	if strings.HasSuffix(os.Args[0], ".test") {
		return "", "", os.TempDir()
	}
	base := os.Getenv("DEVBOX_SECURITY_DATA_DIR")
	if base == "" {
		base = "/var/lib/devbox/security"
	}
	return base + "/settings.json", base + "/master.key", base + "/certificates"
}

// authGate 用一个"路径豁免 + Middleware 兜底"的组合包住整个 mux。
// 静态资源和登录/健康探测直接放行；其它 /api/v1/* 全部走 auth.Middleware。
// 认证未启用 (password 为空) 时 Middleware 内部会直接透传。
func (s *Server) authGate(inner http.Handler) http.Handler {
	if s.auth == nil {
		return inner
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		// 免鉴权路径：登录接口 + 健康探测 + 登录页设备信息 + 所有非 /api/v1/ 前缀（静态文件）
		// /api/v1/device 供登录页展示机器身份 (hostname/CPU/内存/uptime/IP)。
		// /api/v1/cloud/status 与 /api/v1/about 是 edge-ota 遗留的登录前探测点，
		// devbox 侧固定返回"离线"占位，无敏感数据，公开访问以避免登录页 401 噪音。
		if !strings.HasPrefix(p, "/api/v1/") ||
			strings.HasPrefix(p, "/api/v1/auth/") ||
			strings.HasPrefix(p, "/api/v1/files/public/") ||
			p == "/api/v1/health" ||
			p == "/api/v1/device" ||
			p == "/api/v1/cloud/status" ||
			p == "/api/v1/about" {
			inner.ServeHTTP(w, r)
			return
		}
		s.auth.Middleware(inner.ServeHTTP, func() bool {
			return s.security != nil && s.security.ProtectionEnabled()
		})(w, r)
	})
}

// Start 启动 HTTP 服务器（阻塞，应在 goroutine 中调用）
func (s *Server) Start(ctx context.Context) error {
	if s.backup != nil {
		s.backup.Start(ctx)
	}
	if s.downloadEngine != nil {
		s.downloadEngine.Start(ctx)
	}
	settings := s.security.Settings()
	addr := fmt.Sprintf(":%d", settings.HTTPPort)
	handler := security.RateLimit(s.authGate(s.mux), s.security.Settings)
	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// 监听 context 取消，优雅关闭
	go func() {
		<-ctx.Done()
		s.logger.Info("Shutting down console HTTP server...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if s.webdav != nil {
			_ = s.webdav.Stop(shutdownCtx)
		}
		srv.Shutdown(shutdownCtx)
	}()

	if settings.HTTPSCertificate != "" {
		certPath, keyPath, err := s.certificates.Paths(settings.HTTPSCertificate)
		if err != nil {
			return fmt.Errorf("HTTPS certificate binding: %w", err)
		}
		httpsAddr := fmt.Sprintf(":%d", settings.HTTPSPort)
		httpsSrv := &http.Server{Addr: httpsAddr, Handler: handler, ReadHeaderTimeout: 10 * time.Second}
		go func() {
			<-ctx.Done()
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = httpsSrv.Shutdown(shutdownCtx)
		}()
		go func() {
			s.logger.Info("Console HTTPS server starting", zap.String("addr", httpsAddr), zap.String("certificate", settings.HTTPSCertificate))
			if err := httpsSrv.ListenAndServeTLS(certPath, keyPath); err != nil && err != http.ErrServerClosed {
				s.logger.Error("Console HTTPS server failed", zap.Error(err))
			}
		}()
	}

	s.logger.Info("Console HTTP server starting",
		zap.String("addr", addr),
		zap.String("static_dir", s.config.StaticDir))

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}
	if s.maintenanceStore != nil {
		settings := s.maintenanceStore.Get()
		if settings.WebDAV.Enabled {
			if err := s.webdav.Start(s.config.WorkDir, settings.WebDAV, s.config.AuthPassword); err != nil {
				s.logger.Warn("Configured WebDAV service could not start", zap.Error(err))
			}
		}
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
	s.mux.HandleFunc("/api/v1/terminal/exec", s.requireAdmin(s.handleTerminalExec))
	// 网络信息
	s.mux.HandleFunc("/api/v1/network", s.handleNetwork)

	// 应用商店路由（阶段4 Compose 商店统一）
	s.mux.HandleFunc("/api/v1/store/apps", s.handleStoreApps)
	s.mux.HandleFunc("/api/v1/store/version", s.handleStoreVersion)
	s.mux.HandleFunc("/api/v1/store/preflight", s.handleStorePreflight)
	s.mux.HandleFunc("/api/v1/store/install", s.requireAdmin(s.handleStoreInstall))
	// 第三方 catalog 路由（阶段4 扩展：HTTP/Git 文件原生 catalog source）
	s.mux.HandleFunc("/api/v1/catalogs", s.requireAdminWrites(s.handleCatalogs))
	s.mux.HandleFunc("/api/v1/catalogs/apps", s.handleCatalogApps)
	s.mux.HandleFunc("/api/v1/catalogs/version", s.handleCatalogVersion)
	s.mux.HandleFunc("/api/v1/catalogs/preflight", s.handleCatalogPreflight)
	s.mux.HandleFunc("/api/v1/catalogs/install", s.requireAdmin(s.handleCatalogInstall))
	s.mux.HandleFunc("/api/v1/catalogs/sources", s.requireAdminWrites(s.handleCatalogSources))
	s.mux.HandleFunc("/api/v1/catalogs/sources/", s.requireAdminWrites(s.handleCatalogSources))
	// 代理应用图标到 apiserver
	s.mux.HandleFunc("/app-icons/", s.handleProxyAppIcons)

	// 应用管理路由
	s.registerAppRoutes()
	// Docker 服务概览与主机运行配置（与应用管理共用 controller）。
	s.registerDockerRoutes()

	// 文件/模型/告警/端口路由
	s.registerExtraRoutes()

	// Supervisor 路由
	s.registerSupervisorRoutes()

	// 硬件清单路由
	s.registerHardwareRoutes()

	// Console accounts, groups and file-root grants.
	s.registerUserRoutes()

	// 服务导航路由 (tkeel-links 的功能吸收)
	s.registerLinksRoutes()

	// 系统查询路由 (processes/disks/network/gpu/history + cloud/audit 存根)
	s.registerSystemRoutes()

	// Prometheus 抓取端点 (/metrics，免鉴权)
	s.registerPromRoutes()

	// 认证路由
	s.registerAuthRoutes()
	// 本人登录历史（脱敏）与退出其他设备（Issue #30 T3）
	s.registerAccountSessionRoutes()
	s.registerOnboardingRoutes()

	// 浏览器应用（代理 + 书签/历史）
	s.registerBrowserRoutes()

	// 网络、远程访问与安全设置（Issue #13）。
	s.registerNetworkSecurityRoutes()

	// 本机、外接设备与 rsync over SSH 备份任务。
	s.registerBackupRoutes()

	// 文件访问服务与系统维护（Issue #14）
	s.registerMaintenanceRoutes()

	// 下载任务中心
	s.registerDownloadRoutes()

	// 静态文件兜底
	fileServer := http.FileServer(staticFS)
	s.mux.Handle("/", fileServer)
}

func onboardingPath(browserDataPath string) string {
	if browserDataPath == "" {
		return "/etc/devbox/onboarding.json"
	}
	return filepath.Join(filepath.Dir(browserDataPath), "onboarding.json")
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
	s.jsonOK(w, s.collector.GetMetricsHistoryWindow(parseWindow(r.URL.Query().Get("window")), 720))
}
