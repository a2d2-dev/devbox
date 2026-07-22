package apps

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"go.uber.org/zap"
)

// composeRuntime：Docker Compose 运行时 adapter（阶段1-3）。
//
//   - 写操作走 Compose CLI（compose_cli.go）；读操作走 Docker Engine API（docker_engine.go）。
//   - 只按 com.docker.compose.project 前缀 devbox- 聚合（controller 用 SQLite 登记二次过滤，
//     未登记的 devbox-* 不视为受管，满足"只发现 devbox 受管 Compose project"）。
//   - phase 聚合由后端完成；external volume 由 compose down 语义保证永不删。
type composeRuntime struct {
	engine *dockerEngine
	cli    *composeCLI
	paths  *Paths
	logger *zap.Logger
}

// NewComposeRuntime 构造 Compose adapter（不立即连接，探活在 Capability）。
func NewComposeRuntime(sockPath string, paths *Paths, logger *zap.Logger) *composeRuntime {
	return &composeRuntime{
		engine: newDockerEngine(sockPath),
		cli:    newComposeCLI(),
		paths:  paths,
		logger: logger,
	}
}

func (c *composeRuntime) Kind() RuntimeKind { return RuntimeCompose }

// RenderConfig 透传到 compose CLI（实现 composePrechecker）。
func (c *composeRuntime) RenderConfig(ctx context.Context, content, env string) (string, error) {
	return c.cli.RenderConfig(ctx, content, env)
}

// Capability 探测 docker daemon + compose 可用性。不可用返回清晰原因。
func (c *composeRuntime) Capability(ctx context.Context) RuntimeCapability {
	if err := c.engine.ping(ctx); err != nil {
		return RuntimeCapability{Available: false, Reason: err.Error()}
	}
	ver, _ := c.engine.version(ctx)
	if _, err := exec.CommandContext(ctx, "docker", "compose", "version").Output(); err != nil {
		return RuntimeCapability{Available: false, Reason: "docker compose 插件不可用: " + err.Error()}
	}
	return RuntimeCapability{
		Available: true,
		Version:   "docker " + ver,
		Features:  []string{"discover", "apply", "start", "stop", "restart", "redeploy", "remove", "logs"},
	}
}

// Observe 聚合所有 devbox-* compose project 的运行态。
func (c *composeRuntime) Observe(ctx context.Context) (map[string]Application, error) {
	containers, err := c.engine.listContainers(ctx, nil)
	if err != nil {
		// daemon 不可用：空 map + nil（controller 层降级，不影响 K8s）。
		c.logger.Warn("observe compose apps failed; returning Kubernetes results only", zap.Error(err))
		return map[string]Application{}, nil
	}
	// 按 app-id（来自 compose project）分组容器。
	groups := map[string][]engineContainer{}
	for _, ct := range containers {
		project := ct.Labels["com.docker.compose.project"]
		appID := AppIDFromProject(project)
		if appID == "" {
			continue // 非受管前缀或非法 id
		}
		groups[appID] = append(groups[appID], ct)
	}
	out := make(map[string]Application, len(groups))
	for appID, cts := range groups {
		out[appID] = c.aggregateApp(appID, cts)
	}
	return out, nil
}

// aggregateApp 把一组同 project 容器聚合成一个 Application（运行态）。
func (c *composeRuntime) aggregateApp(appID string, cts []engineContainer) Application {
	app := Application{
		ID:        appID,
		Name:      appID,
		Kind:      "app",
		Runtime:   RuntimeCompose,
		Namespace: ProjectName(appID),
	}
	var services []ServiceStatus
	var endpoints []Endpoint
	for _, ct := range cts {
		svc := serviceFromContainer(ct)
		services = append(services, svc)
		if app.Image == "" {
			app.Image = svc.Image
		}
		for _, p := range svc.Ports {
			if p.HostPort > 0 {
				endpoints = append(endpoints, Endpoint{
					Name: svc.Name, Protocol: "http", Port: p.HostPort,
					URL: "http://localhost:" + itoa32(p.HostPort),
				})
			}
		}
	}
	app.Observed.Services = services
	app.Observed.Endpoints = endpoints
	app.Observed.Phase = aggregatePhase(services)
	return app
}

func serviceFromContainer(ct engineContainer) ServiceStatus {
	svc := ServiceStatus{
		Name:        ct.Labels["com.docker.compose.service"],
		Image:       ct.Image,
		State:       ct.State,
		Health:      parseHealth(ct.Status),
		ContainerID: shortID(ct.ID),
	}
	for _, p := range ct.Ports {
		if p.PublicPort > 0 {
			svc.Ports = append(svc.Ports, PortMapping{
				HostPort: int32(p.PublicPort), ContainerPort: int32(p.PrivatePort), Protocol: p.Type,
			})
		} else {
			svc.Ports = append(svc.Ports, PortMapping{
				ContainerPort: int32(p.PrivatePort), Protocol: p.Type,
			})
		}
	}
	return svc
}

// parseHealth 从 docker Status 字段（如 "Up 5 minutes (healthy)"）解析健康状态。
func parseHealth(status string) string {
	s := strings.ToLower(status)
	switch {
	case strings.Contains(s, "unhealthy"):
		return "unhealthy"
	case strings.Contains(s, "starting") && strings.Contains(s, "health"):
		return "starting"
	case strings.Contains(s, "healthy"):
		return "healthy"
	}
	return "none"
}

// aggregatePhase 后端聚合 service 状态 → 应用 phase。
//
// best-effort：dead 容器（异常终止，常为崩溃）在「全部未运行」时判 failed，
// 以与「全部 exited（主动停止）」区分；removing 不在此推断（由 worker 在删除流程标注）。
func aggregatePhase(services []ServiceStatus) Phase {
	if len(services) == 0 {
		return PhaseUnknown
	}
	var running, unhealthy, exited, dead, created int
	for _, s := range services {
		switch s.State {
		case "running":
			running++
			if s.Health == "unhealthy" {
				unhealthy++
			}
		case "exited":
			exited++
		case "dead":
			dead++
		case "created", "restarting":
			created++
		default:
			created++
		}
	}
	switch {
	case unhealthy > 0:
		return PhaseDegraded
	case running == len(services):
		return PhaseRunning
	case exited+dead == len(services) && dead > 0:
		return PhaseFailed // 全部未运行且存在 dead → 崩溃而非主动停止
	case exited+dead == len(services):
		return PhaseStopped
	case created > 0 && running < len(services):
		return PhaseDeploying
	case running > 0:
		return PhaseDegraded // 部分运行
	default:
		return PhasePending
	}
}

// Apply 部署：compose config 预检 → pull（best-effort，失败记录）→ up -d。
// 超时 15 分钟（拉大镜像）。
func (c *composeRuntime) Apply(ctx context.Context, app Application, composeFile string) error {
	dir := filepath.Dir(composeFile)
	project := ProjectName(app.ID)
	if err := c.cli.config(ctx, dir, project); err != nil {
		return err
	}
	runCtx, cancel := withTimeout(ctx, 15*time.Minute)
	defer cancel()
	return c.pullUp(runCtx, dir, project, app.ID)
}

// pullUp 执行 pull（best-effort，失败仅记录）+ up -d。pull 失败不中断：私有镜像
// 可能已存在，由 up 自拉取并在真正缺失时报错。
func (c *composeRuntime) pullUp(ctx context.Context, dir, project, appID string) error {
	if _, perr := c.cli.pull(ctx, dir, project); perr != nil {
		c.logger.Warn("compose pull failed; continuing to up",
			zap.String("app", appID), zap.Error(perr))
	}
	return c.cli.up(ctx, dir, project)
}

// Operate start/stop/restart/redeploy。
func (c *composeRuntime) Operate(ctx context.Context, app Application, action Action) error {
	dir, project := c.dirProject(app.ID)
	runCtx, cancel := withTimeout(ctx, 10*time.Minute)
	defer cancel()
	switch action {
	case ActionStart:
		return c.cli.start(runCtx, dir, project)
	case ActionStop:
		return c.cli.stop(runCtx, dir, project)
	case ActionRestart:
		return c.cli.restart(runCtx, dir, project)
	case ActionRedeploy:
		// redeploy = 重新 up（应用最新 compose，并尝试拉取新镜像）。
		return c.pullUp(runCtx, dir, project, app.ID)
	default:
		return ValidationErr("unknown action: " + string(action))
	}
}

// Remove 卸载：compose down。purge 时 --volumes 删除受管 volume（external 永不删）。
// 幂等：若项目已不存在（容器为 0），视为已删除。
func (c *composeRuntime) Remove(ctx context.Context, app Application, purge bool) error {
	dir, project := c.dirProject(app.ID)
	runCtx, cancel := withTimeout(ctx, 10*time.Minute)
	defer cancel()
	err := c.cli.down(runCtx, dir, project, purge)
	if err != nil {
		// 容错：检查容器是否已清空。
		if c.projectEmpty(ctx, app.ID) {
			return nil
		}
		return err
	}
	return nil
}

// Logs 取指定 Compose service 容器日志；service 为空时兼容取第一个容器。
func (c *composeRuntime) Logs(ctx context.Context, app Application, opts LogOptions) (LogPage, error) {
	containers, err := c.engine.listContainers(ctx, []string{"com.docker.compose.project=" + ProjectName(app.ID)})
	if err != nil {
		return LogPage{}, err
	}
	if len(containers) == 0 {
		return LogPage{AppID: app.ID, Logs: ""}, nil
	}
	container := containers[0]
	if opts.Service != "" {
		found := false
		for _, candidate := range containers {
			if candidate.Labels["com.docker.compose.service"] == opts.Service {
				container = candidate
				found = true
				break
			}
		}
		if !found {
			return LogPage{}, NotFoundErr("service " + opts.Service)
		}
	}
	logs, err := c.engine.containerLogs(ctx, container.ID, opts.Tail)
	if err != nil {
		return LogPage{}, err
	}
	return LogPage{AppID: app.ID, Logs: sanitizeLog(logs)}, nil
}

func (c *composeRuntime) dirProject(appID string) (string, string) {
	return c.paths.AppDir(appID), ProjectName(appID)
}

// projectEmpty 该 app 受管容器是否为 0（幂等删除判定）。
func (c *composeRuntime) projectEmpty(ctx context.Context, appID string) bool {
	containers, err := c.engine.listContainers(ctx, []string{"com.docker.compose.project=" + ProjectName(appID)})
	if err != nil {
		return false
	}
	return len(containers) == 0
}

func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

func itoa32(n int32) string { return itoa(int64(n)) }
