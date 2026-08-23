package apps

import (
	"context"
	"os"
	"path/filepath"
	"sort"
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
		cli:    newComposeCLI(sockPath),
		paths:  paths,
		logger: logger,
	}
}

func (c *composeRuntime) Kind() RuntimeKind { return RuntimeCompose }

// RenderConfig 透传到 compose CLI（实现 composePrechecker）。
func (c *composeRuntime) RenderConfig(ctx context.Context, content, env string) (string, error) {
	return c.cli.RenderConfig(ctx, content, env)
}

// RenderProjectConfig 在 projectDir(--project-directory)下用显式多 -f 文件（devbox 控制临时副本）
// + 受控空 envFile 归一化合并 compose（接管用）。noInterpolate=true 生成托管单一 compose.yaml
// （变量保留）；false 用于内存风险分析。
func (c *composeRuntime) RenderProjectConfig(ctx context.Context, projectDir, project string, files []string, envFile string, noInterpolate bool) (string, error) {
	return c.cli.renderProjectConfig(ctx, projectDir, project, files, envFile, noInterpolate)
}

// SocketPath 返回受管 docker daemon 的 unix socket 路径（非 unix 端点则空）。
// 接管路径校验用它拒绝 working_dir 落在 socket 所在目录。
func (c *composeRuntime) SocketPath() string {
	host := c.cli.dockerHost
	if strings.HasPrefix(host, "unix://") {
		return strings.TrimPrefix(host, "unix://")
	}
	return ""
}

// Capability 探测 docker daemon + compose 可用性。不可用返回清晰原因。
func (c *composeRuntime) Capability(ctx context.Context) RuntimeCapability {
	if err := c.engine.ping(ctx); err != nil {
		return RuntimeCapability{Available: false, Reason: err.Error()}
	}
	ver, _ := c.engine.version(ctx)
	composeVersion, err := c.cli.command(ctx, "compose", "version", "--short").Output()
	if err != nil {
		return RuntimeCapability{Available: false, Reason: "docker compose 插件不可用: " + err.Error()}
	}
	return RuntimeCapability{
		Available: true,
		Version:   "docker " + ver + " · compose " + strings.TrimSpace(string(composeVersion)),
		Features:  []string{"discover", "apply", "start", "stop", "restart", "redeploy", "remove", "logs"},
	}
}

// composeProject 解析 app 真实 compose project name（runtime adapter 内部统一入口）。
//
// 优先 app.RuntimeProject（controller 由 ComposeProjectName(meta) 注入：接管的外部
// project 保留原名、devbox 创建的为 devbox-<id>）；缺失时回退 devbox-<id>。Apply/
// Operate/Remove/Logs/projectEmpty 全部经此 helper，不直接 ProjectName(app.ID)，确保
// 接管后用原 project name 原地管理现有容器/网络/named volume。
func composeProject(app Application) string {
	if app.RuntimeProject != "" {
		return app.RuntimeProject
	}
	return ProjectName(app.ID)
}

// aggregateProjectLabels 聚合同 project 全部容器的 working_dir/config_files 标签。
// 取首个容器的值作为代表；若任一容器的 (working_dir, 规范化 config_files) 与首个不一致，
// 标记 conflict（任取其一接管不安全 → Discovered.TakeoverAvailable=false）。
func aggregateProjectLabels(cts []engineContainer) (workDir string, configFiles []string, conflict bool) {
	if len(cts) == 0 {
		return "", nil, false
	}
	workDir = cts[0].Labels["com.docker.compose.project.working_dir"]
	configFiles = splitComposeFiles(cts[0].Labels["com.docker.compose.project.config_files"])
	canonical := workDir + "\x00" + strings.Join(configFiles, "\x00")
	for _, ct := range cts[1:] {
		wd := ct.Labels["com.docker.compose.project.working_dir"]
		cf := splitComposeFiles(ct.Labels["com.docker.compose.project.config_files"])
		if wd+"\x00"+strings.Join(cf, "\x00") != canonical {
			conflict = true
			break
		}
	}
	return workDir, configFiles, conflict
}

// splitComposeFiles 解析 com.docker.compose.project.config_files label（逗号分隔的路径列表）。
// 仅切分字符串，不触碰文件系统；空/空白项丢弃。列表扫描与接管共用此切分语义。
func splitComposeFiles(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// Observe 聚合 daemon 上所有 compose project 的运行态，keyed by compose project name。
//
// key 为 com.docker.compose.project（运行时身份），而非 app-id：controller 持有 meta，
// 由 ComposeProjectName(meta) 把 domain app-id ↔ project name 互转后合并；discovered
// （未登记、非 devbox-*）project 由 controller 据此 map 直接构造只读 Application。
// 列表扫描只读容器 labels 的 project/service/image/state/ports 与 working_dir/
// config_files 的「路径字符串」，绝不读取这些路径指向的宿主文件。
func (c *composeRuntime) Observe(ctx context.Context) (map[string]Application, error) {
	containers, err := c.engine.listContainers(ctx, nil)
	if err != nil {
		// daemon 不可用：空 map + nil（controller 层降级，不影响 K8s）。
		c.logger.Warn("observe compose apps failed; returning Kubernetes results only", zap.Error(err))
		return map[string]Application{}, nil
	}
	// 按 compose project name 分组容器（含 stopped，因 listContainers all=true）。
	groups := map[string][]engineContainer{}
	for _, ct := range containers {
		project := ct.Labels["com.docker.compose.project"]
		if project == "" {
			continue // 非 compose 容器
		}
		groups[project] = append(groups[project], ct)
	}
	out := make(map[string]Application, len(groups))
	for project, cts := range groups {
		out[project] = c.aggregateApp(project, cts)
	}
	return out, nil
}

// aggregateApp 把一组同 project 容器聚合成一个 Application（运行态）。
//
// RuntimeProject 携带真实 project name（controller 与 runtime 的内部 identity）；
// ID 仅对 devbox-* project 还原为 app-id（便于受管匹配），其余留空——discovered 的
// 稳定 ID 由 controller 用 ExternalID(project) 赋予。Namespace 刻意不设（K8s 专属）。
func (c *composeRuntime) aggregateApp(project string, cts []engineContainer) Application {
	app := Application{
		ID:             AppIDFromProject(project),
		Name:           project,
		Kind:           "app",
		Runtime:        RuntimeCompose,
		RuntimeProject: project,
	}
	// 诊断用 compose labels 路径字符串（非文件内容）：聚合同 project 全部容器的标签，
	// 不一致则标记 conflict（任取其一接管不安全 → Discovered.TakeoverAvailable=false）。
	// 列表扫描不读取这些路径；仅未接管 project 经 Discovered 暴露给 UI。
	app.ObservedWorkingDir, app.ObservedConfigFiles, app.ObservedDiscoveredConflict = aggregateProjectLabels(cts)
	services := aggregateServices(cts)
	var endpoints []Endpoint
	for _, svc := range services {
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

// aggregateServices 把同一 Compose service 的多个 replica 聚合成一条状态。
// ContainerID 保留一个诊断样本，Replicas/Ready 表示容器总数与运行数。
func aggregateServices(containers []engineContainer) []ServiceStatus {
	byName := map[string][]engineContainer{}
	for _, container := range containers {
		name := container.Labels["com.docker.compose.service"]
		byName[name] = append(byName[name], container)
	}
	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]ServiceStatus, 0, len(names))
	for _, name := range names {
		group := byName[name]
		service := serviceFromContainer(group[0])
		service.Replicas = int32(len(group))
		service.Ready = 0
		seenPorts := map[PortMapping]bool{}
		service.Ports = nil
		for _, container := range group {
			status := serviceFromContainer(container)
			if status.State == "running" && status.Health != "unhealthy" && status.Health != "starting" {
				service.Ready++
			}
			service.State = worseContainerState(service.State, status.State)
			service.Health = worseHealth(service.Health, status.Health)
			for _, port := range status.Ports {
				if !seenPorts[port] {
					seenPorts[port] = true
					service.Ports = append(service.Ports, port)
				}
			}
		}
		out = append(out, service)
	}
	return out
}

func worseContainerState(a, b string) string {
	rank := func(state string) int {
		switch state {
		case "running":
			return 0
		case "created":
			return 1
		case "restarting":
			return 2
		case "paused":
			return 3
		case "exited":
			return 4
		case "dead":
			return 5
		default:
			return 1
		}
	}
	if rank(b) > rank(a) {
		return b
	}
	return a
}

func worseHealth(a, b string) string {
	rank := map[string]int{"none": 0, "healthy": 1, "starting": 2, "unhealthy": 3}
	if rank[b] > rank[a] {
		return b
	}
	return a
}

func serviceFromContainer(ct engineContainer) ServiceStatus {
	svc := ServiceStatus{
		Name:        ct.Labels["com.docker.compose.service"],
		Image:       ct.Image,
		State:       ct.State,
		Health:      parseHealth(ct.Status),
		ContainerID: shortID(ct.ID),
		Replicas:    1,
	}
	if ct.State == "running" && svc.Health != "unhealthy" && svc.Health != "starting" {
		svc.Ready = 1
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
	var running, unhealthy, starting, exited, dead, created int
	for _, s := range services {
		switch s.State {
		case "running":
			running++
			if s.Health == "unhealthy" {
				unhealthy++
			} else if s.Health == "starting" {
				starting++
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
	case starting > 0:
		return PhaseDeploying
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
// 超时 15 分钟（拉大镜像）。project 取自 app.RuntimeProject（接管保留原名）。
func (c *composeRuntime) Apply(ctx context.Context, app Application, composeFile string, progress func(TaskPhase, string)) error {
	dir := filepath.Dir(composeFile)
	project := composeProject(app)
	progress(PhaseTaskResolving, "解析 Compose 配置")
	if err := c.cli.config(ctx, dir, project); err != nil {
		return err
	}
	runCtx, cancel := withTimeout(ctx, 15*time.Minute)
	defer cancel()
	return c.pullUp(runCtx, dir, project, app.ID, progress)
}

// pullUp 执行 pull（best-effort，失败仅记录）+ up -d。pull 失败不中断：私有镜像
// 可能已存在，由 up 自拉取并在真正缺失时报错。
func (c *composeRuntime) pullUp(ctx context.Context, dir, project, appID string, progress func(TaskPhase, string)) error {
	progress(PhaseTaskPulling, "拉取镜像")
	if _, perr := c.cli.pull(ctx, dir, project); perr != nil {
		c.logger.Warn("compose pull failed; continuing to up",
			zap.String("app", appID), zap.String("error", sanitizeWithEnvValues(perr.Error(), c.envFile(appID))))
	}
	progress(PhaseTaskApplying, "应用 Compose 项目")
	return c.cli.up(ctx, dir, project)
}

// Operate start/stop/restart/redeploy。project 取自 app.RuntimeProject（接管保留原名）。
func (c *composeRuntime) Operate(ctx context.Context, app Application, action Action, progress func(TaskPhase, string)) error {
	dir, project := c.dirProject(app)
	runCtx, cancel := withTimeout(ctx, 10*time.Minute)
	defer cancel()
	switch action {
	case ActionStart:
		progress(PhaseTaskApplying, "启动应用")
		return c.cli.start(runCtx, dir, project)
	case ActionStop:
		progress(PhaseTaskApplying, "停止应用")
		return c.cli.stop(runCtx, dir, project)
	case ActionRestart:
		progress(PhaseTaskApplying, "重启应用")
		return c.cli.restart(runCtx, dir, project)
	case ActionRedeploy:
		// redeploy = 重新 up（应用最新 compose，并尝试拉取新镜像）。
		return c.pullUp(runCtx, dir, project, app.ID, progress)
	default:
		return ValidationErr("unknown action: " + string(action))
	}
}

// Remove 卸载：compose down。purge 时 --volumes 删除受管 volume（external 永不删）。
// 幂等：若项目已不存在（容器为 0），视为已删除。project 取自 app.RuntimeProject。
func (c *composeRuntime) Remove(ctx context.Context, app Application, purge bool) error {
	dir, project := c.dirProject(app)
	runCtx, cancel := withTimeout(ctx, 10*time.Minute)
	defer cancel()
	err := c.cli.down(runCtx, dir, project, purge)
	if err != nil {
		// 容错：检查容器是否已清空。
		if c.projectEmpty(ctx, app) {
			return nil
		}
		return err
	}
	return nil
}

// Logs 取指定 Compose service 容器日志；service 为空时兼容取第一个容器。
func (c *composeRuntime) Logs(ctx context.Context, app Application, opts LogOptions) (LogPage, error) {
	containers, err := c.engine.listContainers(ctx, []string{"com.docker.compose.project=" + composeProject(app)})
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
	return LogPage{AppID: app.ID, Logs: sanitizeWithEnvValues(logs, c.envFile(app.ID))}, nil
}

func (c *composeRuntime) envFile(appID string) string {
	if c.paths == nil {
		return ""
	}
	b, _ := os.ReadFile(c.paths.EnvFile(appID))
	return string(b)
}

// dirProject 返回受管 app 目录与真实 compose project name（接管保留原名）。
func (c *composeRuntime) dirProject(app Application) (string, string) {
	return c.paths.AppDir(app.ID), composeProject(app)
}

// projectEmpty 该 project 受管容器是否为 0（幂等删除判定）。
func (c *composeRuntime) projectEmpty(ctx context.Context, app Application) bool {
	containers, err := c.engine.listContainers(ctx, []string{"com.docker.compose.project=" + composeProject(app)})
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
