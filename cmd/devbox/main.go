package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

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

	// 应用管理 Controller（统一 K8s + Docker Compose 运行时，Issue #2）。
	// 装配失败仅禁用应用管理，不影响控制台其它功能。
	var appController apps.Controller
	var appCleanup func()
	if c, cleanup, err := apps.AssembleController(ctx, apps.ControllerConfig{
		DataDir:           cfg.Compose.DataDir,
		DockerSocket:      cfg.Compose.DockerSocket,
		ComposeEnabled:    cfg.Compose.Enabled,
		Kubeconfig:        cfg.Kubernetes.Kubeconfig,
		Namespace:         cfg.Kubernetes.Namespace,
		KubernetesEnabled: true,
	}, logger); err != nil {
		logger.Warn("App controller unavailable; app management disabled", zap.Error(err))
	} else {
		appController = c
		appCleanup = cleanup
	}
	defer func() {
		if appCleanup != nil {
			appCleanup()
		}
	}()

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

	// 第三方 catalog source 聚合（HTTP/Git 文件原生 catalog，Issue #2 阶段4）。
	// source 仅来自配置（config.yaml/env）；启动时同步一次，按 catalog_poll 周期刷新；
	// 单 source 失败被隔离，catalog 不可用时用上次可信缓存，不影响已安装应用。
	var catalogs *apps.CatalogSet
	if len(cfg.Compose.Catalogs) > 0 {
		cacheRoot := filepath.Join(cfg.Compose.DataDir, "catalog-cache")
		catalogs = apps.NewCatalogSetFromConfigs(toCatalogSources(cfg.Compose.Catalogs), cacheRoot, logger)
		poll := time.Duration(cfg.Compose.CatalogPoll) * time.Second
		catalogs.Start(ctx, poll)
		logger.Info("Catalog sources started",
			zap.Int("sources", len(cfg.Compose.Catalogs)),
			zap.Duration("poll_interval", poll))
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
		Catalogs:          catalogs,
	}, col, appController, storeMgr)

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

// toCatalogSources 把 config.CatalogSourceConfig 映射为 apps.CatalogSource。
// 字段一一对应；token 作为 secret 透传（不入日志/审计；git 经 http.extraHeader 注入）。
func toCatalogSources(cfgs []config.CatalogSourceConfig) []apps.CatalogSource {
	out := make([]apps.CatalogSource, 0, len(cfgs))
	for _, c := range cfgs {
		out = append(out, apps.CatalogSource{
			ID:       c.ID,
			Name:     c.Name,
			Kind:     c.Kind,
			URL:      c.URL,
			Platform: c.Platform,
			Host:     c.Host,
			Ref:      c.Ref,
			Path:     c.Path,
			Token:    c.Token,
			Insecure: c.Insecure,
		})
	}
	return out
}
