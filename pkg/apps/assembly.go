package apps

import (
	"context"

	"go.uber.org/zap"
)

// 装配层：在 apps 包内统一构造 Controller（K8s + Compose adapter + SQLite + worker）。
// main 包不接触内部 runtimeAdapter 接口，只拿到对外 Controller + cleanup。

// ControllerConfig 装配 Controller 所需的全部配置。
type ControllerConfig struct {
	DataDir           string // 数据根
	DockerSocket      string // Docker daemon unix socket
	ComposeEnabled    bool
	Kubeconfig        string
	Namespace         string
	KubernetesEnabled bool // 尝试启用 K8s 运行时（不可用则静默降级）
}

// AssembleController 构造并启动 Controller（含 worker 崩溃恢复）。
// 返回 cleanup 用于关闭仓库（进程退出前调用）。
func AssembleController(ctx context.Context, cfg ControllerConfig, logger *zap.Logger) (Controller, func(), error) {
	paths := NewPaths(cfg.DataDir)
	repo, err := OpenRepository(ctx, paths.DBPath())
	if err != nil {
		return nil, nil, err
	}

	adapters := map[RuntimeKind]runtimeAdapter{}
	if cfg.ComposeEnabled {
		adapters[RuntimeCompose] = NewComposeRuntime(cfg.DockerSocket, paths, logger)
		logger.Info("Compose runtime enabled",
			zap.String("data_dir", cfg.DataDir), zap.String("docker_socket", cfg.DockerSocket))
	}
	if cfg.KubernetesEnabled {
		if kr, err := NewKubernetesRuntime(logger, KubeConfig{
			Kubeconfig: cfg.Kubeconfig, Namespace: cfg.Namespace,
		}); err == nil {
			adapters[RuntimeKubernetes] = kr
			logger.Info("Kubernetes runtime enabled", zap.String("namespace", cfg.Namespace))
		} else {
			// K8s 不可用是单机常态，仅 warn 降级，不影响 Compose。
			logger.Warn("Kubernetes runtime unavailable; K8s app management disabled", zap.Error(err))
		}
	}

	worker := NewWorker(repo, adapters, paths, logger)
	worker.Start(ctx) // 崩溃恢复 + 准备消费

	var prechecker composePrechecker
	if cr, ok := adapters[RuntimeCompose].(*composeRuntime); ok {
		prechecker = cr
	}
	controller := NewController(repo, paths, adapters, worker, logger, WithPrechecker(prechecker))
	cleanup := func() { _ = repo.Close() }
	return controller, cleanup, nil
}
