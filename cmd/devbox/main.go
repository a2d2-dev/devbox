package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/a2d2-dev/devbox/pkg/apps"
	"github.com/a2d2-dev/devbox/pkg/collector"
	"github.com/a2d2-dev/devbox/pkg/config"
	"github.com/a2d2-dev/devbox/pkg/console"
	"go.uber.org/zap"
)

var (
	configFile = flag.String("config", "", "Path to config file")
	version    = "dev" // 由编译时注入
)

func main() {
	flag.Parse()

	cfg, err := config.Load(*configFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	logger, err := initLogger(cfg.Logging)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync()

	logger.Info("A2D2 Devbox starting", zap.String("version", version))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if !cfg.Console.Enabled {
		logger.Info("Console disabled, nothing to serve; exiting")
		return
	}

	col := collector.New(logger, version)
	go col.Start(ctx)

	// K8s 应用管理器：可选，初始化失败仅禁用相关功能。
	var appMgr *apps.Manager
	if mgr, err := apps.NewManager(logger, apps.Config{
		Kubeconfig: cfg.Kubernetes.Kubeconfig,
		Namespace:  cfg.Kubernetes.Namespace,
	}); err != nil {
		logger.Warn("K8s app manager unavailable; app management disabled", zap.Error(err))
	} else {
		appMgr = mgr
	}

	// 应用商店管理器：仅在显式配置 APIServerURL 时启用。
	var storeMgr *apps.StoreManager
	if cfg.Kubernetes.APIServerURL != "" {
		if mgr, err := apps.NewStoreManager(logger, apps.StoreConfig{
			APIServerURL: cfg.Kubernetes.APIServerURL,
			ConsoleURL:   cfg.Console.ConsoleURL,
			Kubeconfig:   cfg.Kubernetes.Kubeconfig,
		}); err != nil {
			logger.Warn("App store manager unavailable", zap.Error(err))
		} else {
			storeMgr = mgr
		}
	}

	consoleServer := console.NewServer(logger, console.Config{
		Enabled:           cfg.Console.Enabled,
		Port:              cfg.Console.Port,
		StaticDir:         cfg.Console.StaticDir,
		WorkDir:           cfg.Console.WorkDir,
		SupervisorSocket:  cfg.Console.SupervisorSocket,
		SupervisorConfDir: cfg.Console.SupervisorConfDir,
		ConsoleURL:        cfg.Console.ConsoleURL,
		AuthPassword:      cfg.Auth.Password,
		AuthSessionTTL:    cfg.Auth.SessionTTL,
		LinksPath:         cfg.Console.LinksPath,
		BrowserDataPath:   cfg.Console.BrowserDataPath,
		BrowserInsecureTLS: cfg.Console.BrowserInsecureTLS,
	}, col, appMgr, storeMgr)

	go func() {
		if err := consoleServer.Start(ctx); err != nil {
			logger.Error("Console HTTP server failed", zap.Error(err))
		}
	}()

	logger.Info("A2D2 Devbox is ready", zap.Int("console_port", cfg.Console.Port))

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	logger.Info("Shutting down A2D2 Devbox...")
	cancel()
	logger.Info("A2D2 Devbox stopped")
}

func initLogger(cfg config.LoggingConfig) (*zap.Logger, error) {
	var level zap.AtomicLevel
	switch cfg.Level {
	case "debug":
		level = zap.NewAtomicLevelAt(zap.DebugLevel)
	case "info":
		level = zap.NewAtomicLevelAt(zap.InfoLevel)
	case "warn":
		level = zap.NewAtomicLevelAt(zap.WarnLevel)
	case "error":
		level = zap.NewAtomicLevelAt(zap.ErrorLevel)
	default:
		level = zap.NewAtomicLevelAt(zap.InfoLevel)
	}

	zapCfg := zap.NewProductionConfig()
	zapCfg.Level = level

	if cfg.File != "" {
		zapCfg.OutputPaths = []string{cfg.File}
		zapCfg.ErrorOutputPaths = []string{cfg.File}
	}

	return zapCfg.Build()
}
