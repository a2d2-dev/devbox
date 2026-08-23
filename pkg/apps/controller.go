package apps

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// Controller 是对外稳定 seam（HTTP 依赖这个接口，不依赖任何运行时实现）。
//
// 写操作统一返回 Task（Apply/Operate/Remove/RestoreRevision）；
// Apply 成功仅表示期望状态已可靠持久化，不代表容器已运行。
type Controller interface {
	Capability(ctx context.Context) (CapabilityReport, error)
	List(ctx context.Context, filter Filter) ([]Application, error)
	Get(ctx context.Context, id string) (Application, error)
	Logs(ctx context.Context, id string, opts LogOptions) (LogPage, error)

	Validate(ctx context.Context, req ValidateRequest) (ValidateResult, error)
	Apply(ctx context.Context, desired DesiredApplication, opts ApplyOptions) (Task, error)
	Operate(ctx context.Context, id string, action Action, opts OperationOptions) (Task, error)
	Remove(ctx context.Context, id string, opts RemoveOptions) (Task, error)
	RestoreRevision(ctx context.Context, id string, rev int64, opts ApplyOptions) (Task, error)
	// Takeover 接管一个 discovered compose project（显式用户动作），转为受管：保留原 project
	// name 原地管理，源目录只读不改。返回接管后的 managed Application。
	Takeover(ctx context.Context, req TakeoverRequest, opts ApplyOptions) (Application, error)

	GetTask(ctx context.Context, taskID string) (Task, error)
	ListOperations(ctx context.Context, id string) ([]Task, error)
	GetCompose(ctx context.Context, id string) (ComposeContent, error)
	ListRevisions(ctx context.Context, id string) ([]Revision, error)

	// 详情（要求 6/7）：从事实源静态推导，不查 daemon。
	StorageInventory(ctx context.Context, id string) (StorageInventory, error)
	EnvInventory(ctx context.Context, id string) (EnvInventory, error)
	RemovePreview(ctx context.Context, id string, purge bool) (RemovePreview, error)
}

// runtimeAdapter 是内部 seam：Compose 与 K8s 两个实现，不暴露给 HTTP。
//
// Observe 返回该运行时下所有（受管）应用的运行态，keyed by app ID。
// controller 负责：与 SQLite 元数据合并、过滤未登记项目、计算兼容旧字段。
type runtimeAdapter interface {
	Kind() RuntimeKind
	// Observe 全量观测运行态。Docker/K8s 不可用时返回空 map + nil（不报错刷屏）。
	Observe(ctx context.Context) (map[string]Application, error)
	// Apply 部署期望：compose 为 `compose up`，k8s 为商店部署（阶段4）。
	Apply(ctx context.Context, app Application, composeFile string, progress func(TaskPhase, string)) error
	Operate(ctx context.Context, app Application, action Action, progress func(TaskPhase, string)) error
	Remove(ctx context.Context, app Application, purge bool) error
	Logs(ctx context.Context, app Application, opts LogOptions) (LogPage, error)
}

// taskRunner 由 worker 实现，controller 提交任务后通知它执行。
type taskRunner interface {
	Enqueue(taskID string)
}

// composePrechecker 落盘前用 `docker compose config` 渲染 Compose，用于真实预检
// 与「渲染后」风险分析（避免 ${VAR} 绕过 privileged/socket/root bind/host mode）。
// 渲染输出可能含 secret 值，调用方不得写入 task/revision/audit/log/error。
type composePrechecker interface {
	RenderConfig(ctx context.Context, content, env string) (string, error)
}

// takeoverPrechecker 接管归一化与 socket 定位（生产为 composeRuntime，测试可注入 fake）。
// files 必须是 devbox 控制临时目录里的副本（非原始 config path，消除 TOCTOU）；envFile 为
// 受控空 .env（阻止自动读取 working_dir/.env）。noInterpolate=true 输出用于托管副本（变量
// 保留），false 输出仅用于内存风险分析（可能含 secret，不持久化）。
type takeoverPrechecker interface {
	RenderProjectConfig(ctx context.Context, projectDir, project string, files []string, envFile string, noInterpolate bool) (string, error)
	SocketPath() string
}

// service Controller 的默认实现。
type service struct {
	repo       Repository
	paths      *Paths
	adapters   map[RuntimeKind]runtimeAdapter
	runner     taskRunner
	prechecker composePrechecker
	takeover   takeoverPrechecker // 接管归一化；nil 时 Takeover 从 compose adapter 类型断言
	logger     *zap.Logger
	now        func() time.Time
	mutationMu sync.Mutex
	mutations  map[string]*sync.Mutex
	docker     *DockerManager
}

// ServiceOption 构造选项。
type ServiceOption func(*service)

// WithClock 注入时钟（测试用）。
func WithClock(now func() time.Time) ServiceOption {
	return func(s *service) { s.now = now }
}

// WithPrechecker 注入 Compose 预检器（生产用真实 compose adapter，测试用 fake）。
func WithPrechecker(p composePrechecker) ServiceOption {
	return func(s *service) { s.prechecker = p }
}

// WithTakeoverPrechecker 注入接管归一化器（生产用真实 compose adapter，测试用 fake）。
func WithTakeoverPrechecker(p takeoverPrechecker) ServiceOption {
	return func(s *service) { s.takeover = p }
}

// WithDockerManager 注入与 Compose 共用端点的 Docker 主机管理能力。
func WithDockerManager(m *DockerManager) ServiceOption {
	return func(s *service) { s.docker = m }
}

// NewController 构造 Controller。adapters 至少含一个运行时；runner 由 worker 提供。
func NewController(repo Repository, paths *Paths, adapters map[RuntimeKind]runtimeAdapter, runner taskRunner, logger *zap.Logger, opts ...ServiceOption) Controller {
	s := &service{
		repo:      repo,
		paths:     paths,
		adapters:  adapters,
		runner:    runner,
		logger:    logger,
		now:       time.Now,
		mutations: map[string]*sync.Mutex{},
	}
	for _, o := range opts {
		o(s)
	}
	if s.docker == nil {
		if compose, ok := adapters[RuntimeCompose].(*composeRuntime); ok {
			runner := realDockerCommandRunner{}
			s.docker = newDockerManagerWithDeps(compose.engine, &systemDockerServiceHost{runner: runner}, &osDockerStorage{daemonPath: defaultDaemonJSON}, runner)
		}
	}
	return s
}

func (s *service) DockerOverview(ctx context.Context) (DockerOverview, error) {
	if s.docker == nil {
		return DockerOverview{Service: DockerServiceSummary{State: DockerServiceNotInstalled, Diagnostic: "Docker 管理能力未装配"}, CheckedAt: s.now()}, nil
	}
	return s.docker.Overview(ctx)
}

func (s *service) DockerStats(ctx context.Context) (DockerStats, error) {
	if s.docker == nil {
		return DockerStats{Diagnostic: "Docker 管理能力未装配", SampledAt: s.now()}, nil
	}
	return s.docker.Stats(ctx)
}

func (s *service) DockerServiceAction(ctx context.Context, req DockerServiceActionRequest) (DockerOverview, error) {
	if s.docker == nil {
		return DockerOverview{}, CapabilityErr("Docker 管理能力未装配")
	}
	return s.docker.ServiceAction(ctx, req)
}

func (s *service) SetDockerAutostart(ctx context.Context, req DockerAutostartRequest) (DockerOverview, error) {
	if s.docker == nil {
		return DockerOverview{}, CapabilityErr("Docker 管理能力未装配")
	}
	return s.docker.SetAutostart(ctx, req)
}

func (s *service) PlanDockerMigration(ctx context.Context, req DockerMigrationRequest) (DockerMigrationPlan, error) {
	if s.docker == nil {
		return DockerMigrationPlan{}, CapabilityErr("Docker 管理能力未装配")
	}
	return s.docker.MigrationPlan(ctx, req)
}

func (s *service) ExecuteDockerMigration(ctx context.Context, req DockerMigrationExecuteRequest) (DockerMigrationResult, error) {
	if s.docker == nil {
		return DockerMigrationResult{}, CapabilityErr("Docker 管理能力未装配")
	}
	return s.docker.ExecuteMigration(ctx, req)
}

func (s *service) lockMutation(appID string) func() {
	s.mutationMu.Lock()
	lock := s.mutations[appID]
	if lock == nil {
		lock = &sync.Mutex{}
		s.mutations[appID] = lock
	}
	s.mutationMu.Unlock()
	lock.Lock()
	return lock.Unlock
}

// --- 读路径 ---

func (s *service) Capability(ctx context.Context) (CapabilityReport, error) {
	rep := CapabilityReport{}
	if a, ok := s.adapters[RuntimeCompose]; ok {
		rep.Compose = composeCapability(ctx, a)
	}
	if a, ok := s.adapters[RuntimeKubernetes]; ok {
		rep.Kubernetes = k8sCapability(ctx, a)
	}
	return rep, nil
}

func composeCapability(ctx context.Context, a runtimeAdapter) RuntimeCapability {
	ca, ok := a.(*composeRuntime)
	if !ok {
		return RuntimeCapability{Available: true}
	}
	return ca.Capability(ctx)
}

func k8sCapability(ctx context.Context, a runtimeAdapter) RuntimeCapability {
	ka, ok := a.(*kubernetesRuntime)
	if !ok {
		return RuntimeCapability{Available: true}
	}
	return ka.Capability(ctx)
}

func (s *service) List(ctx context.Context, filter Filter) ([]Application, error) {
	out, err := s.observeAll(ctx)
	if err != nil {
		return nil, err
	}
	// 排序：名字，稳定。
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	if filter.Runtime != "" {
		filt := out[:0]
		for _, a := range out {
			if a.Runtime == filter.Runtime {
				filt = append(filt, a)
			}
		}
		out = filt
	}
	return out, nil
}

// observeAll 合并所有 adapter 的运行态与 SQLite 元数据，返回完整 Application 列表。
//
// Compose：SQLite 元数据为权威（受管 app），运行态从 compose adapter 取（keyed by
// compose project name，经 ComposeProjectName(meta) 与 meta 互转匹配）。obs 中所有
// 「未被受管 project 集合覆盖」的 compose project 作为 discovered（只读）追加——
// 含未登记的 devbox-* project（prefix 非所有权证据，验收要求所有 compose project）。
// discovered 的稳定 ID 若与受管 meta 冲突，由 resolveDiscoveredID 消解到第二候选，
// 双方都展示且 ID 稳定。
// 单次 snapshot：一次 ListAppMetas + 一次 compose Observe + 一次 k8s Observe，基于同一快照
// 构建 claimed 与输出。避免多次 Observe 返回不同结果导致 discovered ID 分配与最终 K8s 列表
// 不一致/重复 ID，且不重复 K8s API 成本。
func (s *service) observeAll(ctx context.Context) ([]Application, error) {
	var out []Application

	metas, err := s.repo.ListAppMetas(ctx)
	if err != nil {
		return nil, fmt.Errorf("list app metas: %w", err)
	}
	var composeObs map[string]Application
	if ca, ok := s.adapters[RuntimeCompose]; ok {
		composeObs, _ = ca.Observe(ctx) // keyed by compose project name；不可用时空 map
	}
	var k8sObs map[string]Application
	if ka, ok := s.adapters[RuntimeKubernetes]; ok {
		k8sObs, _ = ka.Observe(ctx)
	}

	// 基于「同一 snapshot」构建 claimed（受管 meta ID + 本快照 K8s observed ID）+ managedProject。
	claimed, managedProject := managedSets(metas)
	for id := range k8sObs {
		if id != "" {
			claimed[id] = true
		}
	}

	// 受管 compose apps。
	for _, m := range metas {
		if m.Runtime != RuntimeCompose {
			continue
		}
		project := ComposeProjectName(m)
		var obsApp Application
		var hasObs bool
		if composeObs != nil {
			obsApp, hasObs = composeObs[project]
		}
		app := s.buildComposeApp(m, obsApp, hasObs)
		s.decorateTaskState(ctx, &app)
		out = append(out, app)
	}
	// discovered：所有未被受管 project 集合覆盖的 compose project（含未登记 devbox-*）。
	// ID 经 resolveDiscoveredIDs 按 project 名排序后确定性分配（claimed 含受管 meta + 本快照 K8s ID）。
	if composeObs != nil {
		idByProject := resolveDiscoveredIDs(claimed, managedProject, composeObs)
		for project, obsApp := range composeObs {
			if managedProject[project] {
				continue
			}
			id, ok := idByProject[project]
			if !ok {
				continue // 无法分配稳定 ID（候选全被占），跳过而非给出冲突 ID
			}
			app := s.buildDiscoveredApp(project, obsApp, id)
			s.decorateTaskState(ctx, &app)
			out = append(out, app)
		}
	}
	// K8s apps（同一 k8sObs 快照）。
	for _, app := range k8sObs {
		s.decorateTaskState(ctx, &app)
		out = append(out, app)
	}
	return out, nil
}

// managedSets 从 metas 派生：claimed=所有受管 app ID（任意 runtime，discovered 冲突消解用）；
// managedProject=受管 compose project name 集合（ComposeProjectName(meta)，发现去重用）。
func managedSets(metas []AppRecord) (claimed, managedProject map[string]bool) {
	claimed = map[string]bool{}
	managedProject = map[string]bool{}
	for _, m := range metas {
		claimed[m.ID] = true
		if m.Runtime == RuntimeCompose {
			managedProject[ComposeProjectName(m)] = true
		}
	}
	return claimed, managedProject
}

// discoveryClaimed 构建发现用的 claimed/managedProject：claimed = 所有受管 app ID（任意 runtime
// 的 meta）∪ K8s observed app ID（K8s 无 meta，但 ID 不得与 discovered 碰撞）；managedProject =
// 受管 compose project name 集合。List/Get/Takeover 共用，保证 discovered ID 与 K8s app 不碰撞。
func (s *service) discoveryClaimed(ctx context.Context) (claimed, managedProject map[string]bool, err error) {
	metas, err := s.repo.ListAppMetas(ctx)
	if err != nil {
		return nil, nil, err
	}
	claimed, managedProject = managedSets(metas)
	if ka := s.adapters[RuntimeKubernetes]; ka != nil {
		if kobs, kerr := ka.Observe(ctx); kerr == nil {
			for id := range kobs {
				if id != "" {
					claimed[id] = true
				}
			}
		}
	}
	return claimed, managedProject, nil
}

// resolveDiscoveredIDs 为 obs 中所有未受管 compose project 分配稳定、互不冲突的 discovered ID
// （claimed 含受管 meta + K8s observed ID）。按 project 名排序后依次分配（确定性），已分配 ID
// 并入 claimed。List/Get/Takeover 共用，保证同一 project 三处同 ID 且不与 K8s app 碰撞。
func resolveDiscoveredIDs(claimed, managedProject map[string]bool, obs map[string]Application) map[string]string {
	projects := make([]string, 0, len(obs))
	for project := range obs {
		if !managedProject[project] {
			projects = append(projects, project)
		}
	}
	sort.Strings(projects) // 确定性分配顺序（不依赖 map 迭代序）
	idByProject := make(map[string]string, len(projects))
	for _, project := range projects {
		id := resolveDiscoveredID(project, claimed)
		if id == "" {
			continue
		}
		idByProject[project] = id
		claimed[id] = true
	}
	return idByProject
}

// buildComposeApp 把 compose 元数据与运行态合并为对外 Application（受管：可读写）。
func (s *service) buildComposeApp(meta AppRecord, obs Application, hasObs bool) Application {
	app := Application{
		ID:             meta.ID,
		Name:           meta.Name,
		Kind:           "app",
		Runtime:        RuntimeCompose,
		Source:         meta.Source,
		Revision:       meta.Revision,
		CreatedAt:      meta.CreatedAt,
		Ownership:      OwnershipManaged,
		RuntimeProject: ComposeProjectName(meta), // runtime adapter 与 worker 内部 identity（接管保留原名）
	}
	app.Observed.Revision = meta.ObservedRevision

	if hasObs {
		// 有运行态。
		app.Observed.Phase = obs.Observed.Phase
		app.Observed.Services = obs.Observed.Services
		app.Observed.Endpoints = obs.Observed.Endpoints
		app.Observed.Message = obs.Observed.Message
	} else {
		// 无运行态：期望未同步（部署任务待执行/执行中）→ deploying；
		// 已同步但容器不在（曾 stop/卸载残留）→ stopped。
		switch {
		case meta.Revision > meta.ObservedRevision:
			app.Observed.Phase = PhaseDeploying
			app.Observed.Message = "期望状态未同步，等待部署任务完成"
		default:
			app.Observed.Phase = PhaseStopped
		}
	}
	s.fillCompatFields(&app)
	return app
}

// buildDiscoveredApp 把一个未登记的 compose project 观测态构造为只读 Application。
//
// OwnershipDiscovered + Discovered 只读诊断（project/来源路径字符串，非文件内容）。
// 写操作一律拒绝，UI 引导用户「接管并编辑」。完全 down 且容器记录已删除的 project
// 无法从 daemon 发现（obs 不含它），此边界由 UI/文档说明。ID 由 resolveDiscoveredID
// 在 claimed 约束下消解（与受管冲突时回退第二候选），与 list/get/takeover 同算法。
func (s *service) buildDiscoveredApp(project string, obs Application, id string) Application {
	di := &DiscoveredInfo{
		Project:     project,
		WorkingDir:  obs.ObservedWorkingDir,
		ConfigFiles: obs.ObservedConfigFiles,
		ReadOnly:    true,
	}
	// 可接管性：project name 合法 + 容器标签一致 + 有 working_dir/config_files。
	di.TakeoverAvailable, di.Reason = takeoverAvailability(project, obs, di)
	app := Application{
		ID:             id,
		Name:           project,
		Kind:           "app",
		Runtime:        RuntimeCompose,
		Ownership:      OwnershipDiscovered,
		RuntimeProject: project,
		Discovered:     di,
	}
	app.Observed.Phase = obs.Observed.Phase
	app.Observed.Services = obs.Observed.Services
	app.Observed.Endpoints = obs.Observed.Endpoints
	s.fillCompatFields(&app)
	return app
}

// takeoverAvailability 评估 discovered project 是否可接管，返回原因（不含文件内容）。
func takeoverAvailability(project string, obs Application, di *DiscoveredInfo) (bool, string) {
	if !ValidComposeProjectName(project) {
		return false, "compose project name 非法（含控制字符/换行或不符合命名规则），无法安全接管"
	}
	if obs.ObservedDiscoveredConflict {
		return false, "同 project 容器的 working_dir/config_files 标签不一致，无法确定接管来源"
	}
	if di.WorkingDir == "" || len(di.ConfigFiles) == 0 {
		return false, "缺少 working_dir/config_files 标签"
	}
	return true, ""
}

// findDiscoveredByID 在 compose 观测中按稳定 ID 查找未接管 project（Get/Logs/Takeover 用）。
// 用与 list 完全相同的 discoveryClaimed + resolveDiscoveredIDs 算法定位，保证 ID 一致。
func (s *service) findDiscoveredByID(ctx context.Context, id string) (Application, bool) {
	if id == "" {
		return Application{}, false
	}
	ca := s.adapters[RuntimeCompose]
	if ca == nil {
		return Application{}, false
	}
	claimed, managedProject, err := s.discoveryClaimed(ctx)
	if err != nil {
		return Application{}, false
	}
	obs, err := ca.Observe(ctx)
	if err != nil {
		return Application{}, false
	}
	idByProject := resolveDiscoveredIDs(claimed, managedProject, obs)
	for project, obsApp := range obs {
		if idByProject[project] == id {
			return s.buildDiscoveredApp(project, obsApp, id), true
		}
	}
	return Application{}, false
}

// isDiscovered 判断 id 当前是否为未接管的 compose project（写保护用）。
// 必须先查 meta：接管后 managed id 与 discovered 同算法，若不先排除受管会导致
// Operate/Remove/Restore 永久 not_managed。
func (s *service) isDiscovered(ctx context.Context, id string) bool {
	if _, ok, err := s.repo.GetAppMeta(ctx, id); err != nil || ok {
		return false // 有 meta（含已接管）→ 非只读 discovered
	}
	_, found := s.findDiscoveredByID(ctx, id)
	return found
}

// notManagedErr 未接管写操作的统一错误（→ HTTP 400 validation, reason=not_managed）。
func notManagedErr() *Error {
	return newErr(ErrKindValidation, "not_managed",
		"应用未接管，请先点击「接管并编辑」后再执行写操作", nil)
}

// decorateTaskState 把持久 Task 状态合并进 Application。运行时 Observe 描述容器
// 现状，Task 描述控制面正在发生什么；两者必须同时呈现，前端不自行推断。
func (s *service) decorateTaskState(ctx context.Context, app *Application) {
	tasks, err := s.repo.ListTasksByApp(ctx, app.ID, 1)
	if err != nil || len(tasks) == 0 {
		return
	}
	last := tasks[0]
	app.LastTask = &last
	if last.Status == TaskQueued || last.Status == TaskRunning {
		switch last.Type {
		case TaskRemove:
			app.Observed.Phase = PhaseRemoving
			app.Observed.Message = "应用正在卸载"
		case TaskApply, TaskRestore:
			app.Observed.Phase = PhaseDeploying
			app.Observed.Message = "正在应用期望配置"
		case TaskOperate:
			if last.Action == ActionStart || last.Action == ActionRestart || last.Action == ActionRedeploy {
				app.Observed.Phase = PhaseDeploying
				app.Observed.Message = "正在执行 " + string(last.Action)
			}
		}
	}
	if last.Status == TaskFailed && last.Revision > app.Observed.Revision {
		app.Observed.Phase = PhaseFailed
		app.Observed.Message = last.Message
	}
	app.State = app.Observed.Phase.LegacyState()
}

// fillCompatFields 填充旧前端 useApps() 期望的兼容字段（State/Image/Ports/Replicas/Ready）。
func (s *service) fillCompatFields(app *Application) {
	app.State = app.Observed.Phase.LegacyState()
	app.Version = app.Source.Version
	var ports []PortMapping
	replicas := int32(0)
	ready := int32(0)
	for _, svc := range app.Observed.Services {
		if app.Runtime == RuntimeCompose && svc.Replicas > 0 {
			replicas += svc.Replicas
			ready += svc.Ready
		} else {
			replicas++
			state := strings.ToLower(svc.State)
			if state == "running" {
				ready++
			}
		}
		if app.Image == "" {
			app.Image = svc.Image
		}
		ports = append(ports, svc.Ports...)
	}
	app.Replicas = replicas
	app.Ready = ready
	app.Ports = ports
	// 聚合 phase 修正：desired running 且有 unhealthy service → degraded。
	if app.Observed.Phase == PhaseRunning {
		for _, svc := range app.Observed.Services {
			if svc.Health == "unhealthy" {
				app.Observed.Phase = PhaseDegraded
				app.State = PhaseDegraded.LegacyState()
				break
			}
		}
	}
}

func (s *service) Get(ctx context.Context, id string) (Application, error) {
	// 优先 compose（SQLite 登记）。
	if meta, ok, err := s.repo.GetAppMeta(ctx, id); err != nil {
		return Application{}, err
	} else if ok {
		var obsApp Application
		var hasObs bool
		if ca := s.adapters[RuntimeCompose]; ca != nil {
			obs, _ := ca.Observe(ctx)
			obsApp, hasObs = obs[ComposeProjectName(meta)]
		}
		app := s.buildComposeApp(meta, obsApp, hasObs)
		s.decorateTaskState(ctx, &app)
		return app, nil
	}
	// discovered compose project（未登记，只读）。
	if app, ok := s.findDiscoveredByID(ctx, id); ok {
		s.decorateTaskState(ctx, &app)
		return app, nil
	}
	// 否则 K8s 观测。
	if ka := s.adapters[RuntimeKubernetes]; ka != nil {
		obs, _ := ka.Observe(ctx)
		if app, ok := obs[id]; ok {
			s.decorateTaskState(ctx, &app)
			return app, nil
		}
	}
	return Application{}, NotFoundErr(id)
}

// resolveRuntime 查 app 所属运行时（worker 与 Operate/Remove 共用）。
func (s *service) resolveRuntime(ctx context.Context, id string) (RuntimeKind, AppRecord, bool, error) {
	if meta, ok, err := s.repo.GetAppMeta(ctx, id); err != nil {
		return "", AppRecord{}, false, err
	} else if ok {
		return meta.Runtime, meta, true, nil
	}
	// 无 meta：检查 K8s 观测是否含此 id。
	if ka := s.adapters[RuntimeKubernetes]; ka != nil {
		obs, _ := ka.Observe(ctx)
		if _, ok := obs[id]; ok {
			return RuntimeKubernetes, AppRecord{}, false, nil
		}
	}
	return "", AppRecord{}, false, NotFoundErr(id)
}

func (s *service) Logs(ctx context.Context, id string, opts LogOptions) (LogPage, error) {
	if meta, ok, err := s.repo.GetAppMeta(ctx, id); err != nil {
		return LogPage{}, err
	} else if ok {
		adapter := s.adapters[meta.Runtime]
		if adapter == nil {
			return LogPage{}, CapabilityErr(fmt.Sprintf("runtime %s unavailable", meta.Runtime))
		}
		app := Application{ID: id, Runtime: meta.Runtime, Name: meta.Name}
		if meta.Runtime == RuntimeCompose {
			app.RuntimeProject = ComposeProjectName(meta) // 接管保留原名，按原 project 取容器
		}
		return adapter.Logs(ctx, app, opts)
	}
	// discovered compose（只读日志；未接管也允许读取诊断日志）。
	if app, ok := s.findDiscoveredByID(ctx, id); ok {
		if ca := s.adapters[RuntimeCompose]; ca != nil {
			return ca.Logs(ctx, app, opts)
		}
	}
	// 否则 K8s 观测。
	if ka := s.adapters[RuntimeKubernetes]; ka != nil {
		obs, _ := ka.Observe(ctx)
		if app, ok := obs[id]; ok {
			return ka.Logs(ctx, app, opts)
		}
	}
	return LogPage{}, NotFoundErr(id)
}

// --- Validate（不落盘）---

func (s *service) Validate(ctx context.Context, req ValidateRequest) (ValidateResult, error) {
	res := ValidateResult{}
	if strings.TrimSpace(req.ComposeContent) == "" {
		res.Errors = append(res.Errors, "compose 内容为空")
		return res, nil
	}
	effectiveCompose, err := applyDeploymentSettings(req.ComposeContent, req.Settings)
	if err != nil {
		res.Errors = append(res.Errors, err.Error())
		return res, nil
	}
	literalSecretRisks, err := AnalyzeLiteralSecrets(effectiveCompose)
	if err != nil {
		res.Errors = append(res.Errors, "解析失败: "+err.Error())
		return res, nil
	}
	res.Risks = append(res.Risks, literalSecretRisks...)
	fileAccessRisks, err := AnalyzeComposeFileAccess(effectiveCompose)
	if err != nil {
		res.Errors = append(res.Errors, "解析失败: "+err.Error())
		return res, nil
	}
	res.Risks = append(res.Risks, fileAccessRisks...)
	if HasBlocked(res.Risks) {
		res.OK = false
		return res, nil
	}
	// 真实预检：新建时仅 params 插值；编辑已有应用时可在 Controller 内部复用
	// 当前 .env，secret 仍不经过 HTTP。
	// compose CLI 不可用时回退静态分析并加 warning；配置非法 → res.Errors。
	env := renderEnvFile(req.Secrets, req.Parameters)
	if req.RetainEnvironment {
		if strings.TrimSpace(req.AppID) == "" {
			res.Errors = append(res.Errors, "retainEnvironment 需要 appId")
			return res, nil
		}
		if _, ok, err := s.repo.GetAppMeta(ctx, req.AppID); err != nil {
			return res, err
		} else if !ok {
			return res, NotFoundErr(req.AppID)
		}
		b, err := os.ReadFile(s.paths.EnvFile(req.AppID))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return res, fmt.Errorf("读取现有环境配置: %w", err)
		}
		env = mergeEnvFile(string(b), req.Secrets, req.Parameters)
	}
	rendered, warn, rerr := s.renderForCheck(ctx, effectiveCompose, env, false)
	if rerr != nil {
		res.Errors = append(res.Errors, rerr.Error())
		return res, nil
	}
	if warn != "" {
		res.Warnings = append(res.Warnings, warn)
	}
	// 解析与风险分析均基于渲染后内容（插值已展开，${VAR} 无法绕过风险策略）。
	previews, err := ExtractServicePreviews(rendered)
	if err != nil {
		res.Errors = append(res.Errors, "解析失败: "+err.Error())
		return res, nil
	}
	res.Services = previews
	if res.Networks, err = composeNetworkInventory(rendered); err != nil {
		res.Errors = append(res.Errors, "网络解析失败: "+err.Error())
		return res, nil
	}
	res.Secrets = preflightSecretKeys(effectiveCompose, env, req.Secrets)
	findings, err := AnalyzeCompose(rendered)
	if err != nil {
		res.Errors = append(res.Errors, "风险分析失败: "+err.Error())
		return res, nil
	}
	for _, f := range findings {
		switch f.Level {
		case RiskBlocked, RiskConfirmation:
			res.Risks = append(res.Risks, f)
		case RiskWarning:
			res.Warnings = append(res.Warnings, fmt.Sprintf("[%s] %s", f.Service, f.Message))
		}
	}
	// 端口冲突提示（宿主端口在本次 compose 内重复声明）。
	if dup := detectDuplicateHostPorts(previews); len(dup) > 0 {
		res.Warnings = append(res.Warnings, "宿主端口重复声明: "+strings.Join(dup, ", "))
	}
	if conflicts := s.detectExistingPortConflicts(ctx, req.AppID, previews); len(conflicts) > 0 {
		res.Warnings = append(res.Warnings, "宿主端口可能已被应用占用: "+strings.Join(conflicts, ", "))
	}
	if conflicts := s.detectExistingPathConflicts(ctx, req.AppID, rendered); len(conflicts) > 0 {
		res.Warnings = append(res.Warnings, "宿主路径已被其他应用挂载: "+strings.Join(conflicts, ", "))
	}
	res.OK = len(res.Errors) == 0 && !HasBlocked(res.Risks)
	return res, nil
}

func (s *service) detectExistingPortConflicts(ctx context.Context, excludeAppID string, previews []ServicePreview) []string {
	wanted := map[int32]bool{}
	for _, preview := range previews {
		for _, spec := range preview.Ports {
			if raw := extractHostPort(spec); raw != "" {
				if port, err := strconv.ParseInt(raw, 10, 32); err == nil {
					wanted[int32(port)] = true
				}
			}
		}
	}
	if len(wanted) == 0 {
		return nil
	}
	list, err := s.List(ctx, Filter{})
	if err != nil {
		return nil // capability/observe failure must not turn preflight into a hard failure
	}
	var conflicts []string
	for _, app := range list {
		if app.ID == excludeAppID {
			continue
		}
		for _, port := range app.Ports {
			if port.HostPort > 0 && wanted[port.HostPort] {
				conflicts = appendIfMissing(conflicts, fmt.Sprintf("%d（%s）", port.HostPort, app.Name))
			}
		}
	}
	return conflicts
}

func (s *service) detectExistingPathConflicts(ctx context.Context, excludeAppID, rendered string) []string {
	wanted := absoluteBindSources(rendered)
	if len(wanted) == 0 {
		return nil
	}
	metas, err := s.repo.ListAppMetas(ctx)
	if err != nil {
		return nil
	}
	var conflicts []string
	for _, meta := range metas {
		if meta.ID == excludeAppID || meta.Runtime != RuntimeCompose {
			continue
		}
		content, err := os.ReadFile(s.paths.ComposeFile(meta.ID))
		if err != nil {
			continue
		}
		for source := range absoluteBindSources(string(content)) {
			if wanted[source] {
				conflicts = appendIfMissing(conflicts, fmt.Sprintf("%s（%s）", source, meta.Name))
			}
		}
	}
	sort.Strings(conflicts)
	return conflicts
}

func absoluteBindSources(composeYAML string) map[string]bool {
	volumes, _, err := analyzeStorage(composeYAML, "")
	if err != nil {
		return nil
	}
	out := map[string]bool{}
	for _, volume := range volumes {
		if volume.Kind == VolumeBind && filepath.IsAbs(volume.Source) {
			out[filepath.Clean(volume.Source)] = true
		}
	}
	return out
}

// renderForCheck 用 prechecker 渲染 compose 用于预检/风险分析。
//   - 成功：返回渲染后文本（可能含 secret，调用方不得持久化）。
//   - compose CLI 不可用（capability）：strict=true 返回该错误；strict=false 回退原文
//     并通过 warn 提示「仅静态预检」。
//   - 配置非法（validation）：返回该错误，调用方应拒绝/展示。
func (s *service) renderForCheck(ctx context.Context, content, env string, strict bool) (rendered, warn string, err error) {
	if s.prechecker == nil {
		return content, "", nil
	}
	out, rerr := s.prechecker.RenderConfig(ctx, content, env)
	if rerr == nil {
		return out, "", nil
	}
	if ae, ok := AsError(rerr); ok && ae.Kind == ErrKindCapability {
		if strict {
			return "", "", rerr
		}
		return content, "docker compose 不可用，仅做静态预检: " + ae.Message, nil
	}
	return "", "", rerr
}

// --- Apply（创建/更新）---

func (s *service) Apply(ctx context.Context, desired DesiredApplication, opts ApplyOptions) (Task, error) {
	lockID := strings.TrimSpace(desired.ID)
	if lockID == "" {
		lockID = Slugify(desired.Name)
	}
	if lockID == "" {
		lockID = "__invalid__"
	}
	defer s.lockMutation(lockID)()
	if desired.Source.Kind == "" {
		desired.Source.Kind = SourceInline
	}
	effectiveCompose, err := applyDeploymentSettings(desired.ComposeContent, desired.Settings)
	if err != nil {
		return Task{}, err
	}
	desired.ComposeContent = effectiveCompose
	desired.Settings = nil // 最终 YAML 是唯一事实源，不重复持久化向导状态。
	// 0. 幂等短路（在任何副作用前）。
	applyHash := hashApplyRequest(desired)
	if opts.IdempotencyKey != "" {
		if t, hit, err := s.lookupIdempotency(ctx, opts.IdempotencyKey, applyHash); err != nil {
			return Task{}, err
		} else if hit {
			return t, nil
		}
	}

	// 安全编辑可在不向前端回传 secret 的前提下复用当前 .env。
	var retainedEnv string
	if desired.RetainEnvironment {
		appID := strings.TrimSpace(desired.ID)
		if appID == "" {
			return Task{}, ValidationErr("retainEnvironment 仅适用于更新已有应用")
		}
		meta, ok, err := s.repo.GetAppMeta(ctx, appID)
		if err != nil {
			return Task{}, err
		}
		if !ok {
			return Task{}, NotFoundErr(appID)
		}
		if desired.Parameters == nil {
			desired.Parameters = meta.Parameters
		}
		b, err := os.ReadFile(s.paths.EnvFile(appID))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return Task{}, fmt.Errorf("读取现有环境配置: %w", err)
		}
		retainedEnv = string(b)
	}

	// 1. 在调用 Compose CLI 前先检查原始事实源，阻断额外宿主文件读取与 secret 明文。
	env := renderEnvFile(desired.Secrets, desired.Parameters)
	if desired.RetainEnvironment {
		env = mergeEnvFile(retainedEnv, desired.Secrets, desired.Parameters)
	}
	literalSecretRisks, err := AnalyzeLiteralSecrets(desired.ComposeContent)
	if err != nil {
		return Task{}, ValidationErr("compose 解析失败: " + err.Error())
	}
	if HasBlocked(literalSecretRisks) {
		return Task{}, RiskBlockedErr("敏感环境变量禁止写入 Compose 明文", literalSecretRisks)
	}
	fileAccessRisks, err := AnalyzeComposeFileAccess(desired.ComposeContent)
	if err != nil {
		return Task{}, ValidationErr("compose 解析失败: " + err.Error())
	}
	if HasBlocked(fileAccessRisks) {
		return Task{}, RiskBlockedErr("Compose 包含未受管的宿主文件读取", fileAccessRisks)
	}
	// 2. 真实预检：docker compose config 渲染（strict：CLI 不可用 → capability，
	//    配置非法 → validation；均不落盘）。secret 仅用于本次渲染，不入任何持久层。
	rendered, _, err := s.renderForCheck(ctx, desired.ComposeContent, env, true)
	if err != nil {
		return Task{}, err
	}
	// 3. 风险分析基于「渲染后」内容（${VAR} 已展开，无法绕过 privileged/socket/root bind）。
	findings, ferr := AnalyzeCompose(rendered)
	if ferr != nil {
		return Task{}, ValidationErr("渲染后风险分析失败: " + ferr.Error())
	}
	if HasBlocked(findings) {
		return Task{}, RiskBlockedErr("存在阻断级风险，已拒绝", findings)
	}
	// 商店/catalog 包禁止可变镜像标签（latest/main/master/edge/nightly）：违反项目红线
	// （禁 latest/main，无法锁定版本），升格为阻断，不可 override。inline 来源仅 warning
	// 暴露，保留显式 confirmation override（AllowRiskyConfirmation，审计留痕）。
	if desired.Source.Kind == SourceStore || desired.Source.Kind == SourceCatalog {
		if mr := HasMutableImageRisk(findings); len(mr) > 0 {
			return Task{}, RiskBlockedErr("商店/catalog 包禁止使用 latest/main 等可变镜像标签", mr)
		}
	}
	if NeedsConfirmation(findings, opts.AllowRiskyConfirmation) {
		return Task{}, RiskBlockedErr("存在需确认的风险，请显式确认后重试", findings)
	}

	// 3. 解析 app ID（创建/更新）+ 乐观并发。
	appID := strings.TrimSpace(desired.ID)
	creating := false
	meta, exists, err := s.repo.GetAppMeta(ctx, appID)
	if err != nil {
		return Task{}, err
	}
	now := s.now()
	if !exists {
		creating = true
		if appID == "" {
			appID = Slugify(desired.Name)
		}
		if err := ValidateAppID(appID); err != nil {
			return Task{}, err
		}
		// 重名检查（含 K8s 观测）。
		if occupied, oerr := s.idExists(ctx, appID); oerr != nil {
			return Task{}, oerr
		} else if occupied {
			return Task{}, ConflictErr("id_taken", fmt.Sprintf("应用 ID %q 已被占用", appID))
		}
		meta = AppRecord{ID: appID, Name: desired.Name, Runtime: RuntimeCompose, CreatedAt: now}
	} else {
		if desired.ExpectedRevision != 0 && desired.ExpectedRevision != meta.Revision {
			return Task{}, ConflictErr("revision_mismatch",
				fmt.Sprintf("expected revision %d but current is %d", desired.ExpectedRevision, meta.Revision))
		}
	}

	// 相同已部署定义重复 Apply 是语义 no-op：不制造 revision/task。提交新 secret
	// 或上次 revision 尚未成功 observed 时必须继续执行，避免吞掉轮换或失败重试。
	if !creating && len(desired.Secrets) == 0 && meta.ObservedRevision == meta.Revision &&
		meta.Name == desired.Name && meta.Source == desired.Source && maps.Equal(meta.Parameters, desired.Parameters) {
		if current, ok, err := s.repo.GetRevision(ctx, appID, meta.Revision); err != nil {
			return Task{}, err
		} else if ok && current.ComposeHash == composeHash(desired.ComposeContent, desired.Parameters) {
			if task, found, err := s.successfulRevisionTask(ctx, appID, meta.Revision); err != nil {
				return Task{}, err
			} else if found {
				return task, nil
			}
		}
	}

	// 4. 可靠写入不可变 revision 与临时期望 env，再提交 DB。worker 执行前按
	// task.Revision 提升事实源，覆盖 DB commit 后进程崩溃的恢复窗口。
	if err := s.paths.EnsureAppDir(appID); err != nil {
		return Task{}, err
	}
	revNum, err := s.nextRevisionNumber(ctx, appID)
	if err != nil {
		return Task{}, err
	}
	revisionFile := s.paths.RevisionFile(appID, revNum)
	pendingEnv := s.paths.PendingEnvFile(appID, revNum)
	if err := s.paths.AtomicWriteFile(appID, fmt.Sprintf("revisions/%d.yaml", revNum), []byte(desired.ComposeContent), 0o644); err != nil {
		return Task{}, err
	}
	if err := s.paths.AtomicWriteFile(appID, filepath.Base(pendingEnv), []byte(env), 0o600); err != nil {
		os.Remove(revisionFile)
		return Task{}, err
	}
	cleanupPrepared := func() { os.Remove(revisionFile); os.Remove(pendingEnv) }

	// 5. revision + meta + task (+ idempotency) 单事务提交（任一失败回滚，无半状态）。
	hash := composeHash(desired.ComposeContent, desired.Parameters)
	meta.Name = desired.Name
	meta.Source = desired.Source
	meta.Parameters = desired.Parameters
	meta.Revision = revNum
	meta.Runtime = RuntimeCompose
	meta.UpdatedAt = now
	rev := Revision{
		Number: revNum, AppID: appID, ComposeHash: hash, Source: desired.Source,
		Parameters: desired.Parameters, CreatedAt: now, CreatedBy: opts.Actor,
		Note: ternary(creating, "initial", "update"),
	}
	summary := sanitizeSummary(desired)
	task := Task{
		ID: uuid.NewString(), AppID: appID, Type: TaskApply,
		Status: TaskQueued, Revision: revNum, IdempotencyKey: opts.IdempotencyKey,
		RequestSummary: summary, CreatedAt: now,
	}
	if err := s.repo.CommitApply(ctx, meta, rev, task, opts.IdempotencyKey, applyHash); err != nil {
		cleanupPrepared()
		return Task{}, err
	}
	s.writeAppMetaSidecar(meta)

	// 6. 最佳努力立即提升，保证 202 返回后读路径看到新期望；即使这里失败，
	// worker 仍会从 prepared 文件恢复并把 task 标为 succeeded/failed。
	_ = s.paths.AtomicWriteFile(appID, "compose.yaml", []byte(desired.ComposeContent), 0o644)
	_ = s.paths.AtomicWriteFile(appID, ".env", []byte(env), 0o600)

	// 7. 入队执行 + 审计。
	if s.runner != nil {
		s.runner.Enqueue(task.ID)
	}
	s.audit(ctx, opts.Actor, appID, "apply:rev="+itoa(revNum), task.ID, summary)
	return task, nil
}

// nextRevisionNumber 避免默认卸载保留的数据目录在同 ID 再安装时覆盖旧快照。
// SQLite 仍是受管 revision 的权威；磁盘最大编号只用于给保留文件让出编号。
func (s *service) nextRevisionNumber(ctx context.Context, appID string) (int64, error) {
	next, err := s.repo.NextRevisionNumber(ctx, appID)
	if err != nil {
		return 0, err
	}
	entries, err := os.ReadDir(s.paths.RevisionsDir(appID))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return 0, err
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}
		n, parseErr := strconv.ParseInt(strings.TrimSuffix(entry.Name(), ".yaml"), 10, 64)
		if parseErr == nil && n >= next {
			next = n + 1
		}
	}
	return next, nil
}

// markTaskFailed 把任务标为 failed（带脱敏信息），用于提交后文件提升失败等补偿路径。
func (s *service) markTaskFailed(ctx context.Context, taskID, msg string) {
	finished := s.now()
	_ = s.repo.UpdateTask(ctx, taskID, func(t *Task) {
		t.Status = TaskFailed
		t.Phase = PhaseTaskVerifying
		t.Message = sanitizeLog(msg)
		t.FinishedAt = &finished
	})
}

// --- Operate / Remove / Restore ---

func (s *service) Operate(ctx context.Context, id string, action Action, opts OperationOptions) (Task, error) {
	defer s.lockMutation(id)()
	if s.isDiscovered(ctx, id) {
		return Task{}, notManagedErr()
	}
	if opts.IdempotencyKey != "" {
		if t, hit, err := s.lookupIdempotency(ctx, opts.IdempotencyKey, hashOperateRequest(id, action)); err != nil {
			return Task{}, err
		} else if hit {
			return t, nil
		}
	}
	rt, _, _, err := s.resolveRuntime(ctx, id)
	if err != nil {
		return Task{}, err
	}
	if rt == RuntimeKubernetes && (action == ActionRedeploy) {
		// K8s redeploy 等价 restart。
		action = ActionRestart
	}
	task, err := s.submit(ctx, submitReq{
		appID: id, taskType: TaskOperate, action: action, idemKey: opts.IdempotencyKey,
		requestHash: hashOperateRequest(id, action), summary: string(action) + " " + id,
	})
	if err != nil {
		return Task{}, err
	}
	s.audit(ctx, opts.Actor, id, "operate:"+string(action), task.ID, "")
	return task, nil
}

func (s *service) Remove(ctx context.Context, id string, opts RemoveOptions) (Task, error) {
	defer s.lockMutation(id)()
	if s.isDiscovered(ctx, id) {
		return Task{}, notManagedErr()
	}
	if opts.IdempotencyKey != "" {
		if t, hit, err := s.lookupIdempotency(ctx, opts.IdempotencyKey, hashRemoveRequest(id, opts.Purge)); err != nil {
			return Task{}, err
		} else if hit {
			return t, nil
		}
	}
	if _, _, _, err := s.resolveRuntime(ctx, id); err != nil {
		return Task{}, err
	}
	task, err := s.submit(ctx, submitReq{
		appID: id, taskType: TaskRemove, idemKey: opts.IdempotencyKey,
		requestHash: hashRemoveRequest(id, opts.Purge), purge: opts.Purge,
		summary: ternary(opts.Purge, "remove+purge "+id, "remove "+id),
	})
	if err != nil {
		return Task{}, err
	}
	s.audit(ctx, opts.Actor, id, "remove:"+ternary(opts.Purge, "purge", "keep-data"), task.ID, "")
	return task, nil
}

func (s *service) RestoreRevision(ctx context.Context, id string, rev int64, opts ApplyOptions) (Task, error) {
	defer s.lockMutation(id)()
	if s.isDiscovered(ctx, id) {
		return Task{}, notManagedErr()
	}
	restoreHash := hashRestoreRequest(id, rev)
	if opts.IdempotencyKey != "" {
		if t, hit, err := s.lookupIdempotency(ctx, opts.IdempotencyKey, restoreHash); err != nil {
			return Task{}, err
		} else if hit {
			return t, nil
		}
	}
	r, ok, err := s.repo.GetRevision(ctx, id, rev)
	if err != nil {
		return Task{}, err
	}
	if !ok {
		return Task{}, NotFoundErr(fmt.Sprintf("revision %d of %s", rev, id))
	}
	// HIGH#2：严格处理 meta 读取，避免空 ID Upsert 污染表。
	meta, ok, err := s.repo.GetAppMeta(ctx, id)
	if err != nil {
		return Task{}, err
	}
	if !ok {
		return Task{}, NotFoundErr(id)
	}
	content, err := os.ReadFile(s.paths.RevisionFile(id, rev))
	if err != nil {
		return Task{}, fmt.Errorf("read revision snapshot: %w", err)
	}
	currentEnv, readEnvErr := os.ReadFile(s.paths.EnvFile(id))
	if readEnvErr != nil && !errors.Is(readEnvErr, os.ErrNotExist) {
		return Task{}, fmt.Errorf("read current environment: %w", readEnvErr)
	}
	restoredEnv := restoreEnvParameters(string(currentEnv), meta.Parameters, r.Parameters)
	// 预检历史内容（strict）+ 渲染后风险分析（策略可能已变更）。
	rendered, _, err := s.renderForCheck(ctx, string(content), restoredEnv, true)
	if err != nil {
		return Task{}, err
	}
	findings, ferr := AnalyzeCompose(rendered)
	if ferr != nil {
		return Task{}, ValidationErr("渲染后风险分析失败: " + ferr.Error())
	}
	if HasBlocked(findings) {
		return Task{}, RiskBlockedErr("历史 revision 存在阻断级风险（策略可能已变更），已拒绝", findings)
	}
	if r.Source.Kind == SourceStore || r.Source.Kind == SourceCatalog {
		if mr := HasMutableImageRisk(findings); len(mr) > 0 {
			return Task{}, RiskBlockedErr("历史 revision 含 latest/main 可变标签（商店/catalog 禁止）", mr)
		}
	}
	if NeedsConfirmation(findings, opts.AllowRiskyConfirmation) {
		return Task{}, RiskBlockedErr("历史 revision 存在需确认的风险，请显式确认后重试", findings)
	}

	now := s.now()
	newRev, err := s.nextRevisionNumber(ctx, id)
	if err != nil {
		return Task{}, err
	}
	newSnapshot := s.paths.RevisionFile(id, newRev)
	if err := s.paths.AtomicWriteFile(id, fmt.Sprintf("revisions/%d.yaml", newRev), content, 0o644); err != nil {
		return Task{}, err
	}
	pendingEnv := s.paths.PendingEnvFile(id, newRev)
	if err := s.paths.AtomicWriteFile(id, filepath.Base(pendingEnv), []byte(restoredEnv), 0o600); err != nil {
		os.Remove(newSnapshot)
		return Task{}, err
	}
	newRevision := Revision{
		Number: newRev, AppID: id, ComposeHash: r.ComposeHash, Source: r.Source,
		Parameters: r.Parameters, CreatedAt: now, CreatedBy: opts.Actor,
		Note: fmt.Sprintf("restored from revision %d", rev),
	}
	meta.Revision = newRev
	meta.Source = r.Source
	meta.Parameters = r.Parameters
	meta.UpdatedAt = now
	task := Task{
		ID: uuid.NewString(), AppID: id, Type: TaskRestore,
		Status: TaskQueued, Revision: newRev, IdempotencyKey: opts.IdempotencyKey,
		RequestSummary: fmt.Sprintf("restore rev %d→%d", rev, newRev), CreatedAt: now,
	}
	if err := s.repo.CommitApply(ctx, meta, newRevision, task, opts.IdempotencyKey, restoreHash); err != nil {
		os.Remove(newSnapshot)
		os.Remove(pendingEnv)
		return Task{}, err
	}
	s.writeAppMetaSidecar(meta)
	_ = s.paths.AtomicWriteFile(id, "compose.yaml", content, 0o644)
	_ = s.paths.AtomicWriteFile(id, ".env", []byte(restoredEnv), 0o600)
	if s.runner != nil {
		s.runner.Enqueue(task.ID)
	}
	s.audit(ctx, opts.Actor, id, fmt.Sprintf("restore:%d", rev), task.ID, "")
	return task, nil
}

// --- 任务/配置/版本查询 ---

func (s *service) GetTask(ctx context.Context, taskID string) (Task, error) {
	return s.repo.GetTask(ctx, taskID)
}

func (s *service) ListOperations(ctx context.Context, id string) ([]Task, error) {
	return s.repo.ListTasksByApp(ctx, id, 50)
}

func (s *service) GetCompose(ctx context.Context, id string) (ComposeContent, error) {
	meta, ok, err := s.repo.GetAppMeta(ctx, id)
	if err != nil {
		return ComposeContent{}, err
	}
	if !ok {
		return ComposeContent{}, NotFoundErr(id)
	}
	data, err := os.ReadFile(s.paths.ComposeFile(id))
	if err != nil {
		return ComposeContent{}, fmt.Errorf("read compose: %w", err)
	}
	return ComposeContent{AppID: id, Source: meta.Source, Compose: string(data), Revision: meta.Revision}, nil
}

func (s *service) ListRevisions(ctx context.Context, id string) ([]Revision, error) {
	if _, ok, err := s.repo.GetAppMeta(ctx, id); err != nil {
		return nil, err
	} else if !ok {
		return nil, NotFoundErr(id)
	}
	return s.repo.ListRevisions(ctx, id)
}

// --- 详情：storage / env / remove preview（要求 6/7）---
//
// 从事实源（compose.yaml + .env）静态推导，不查 daemon。仅 compose 应用有意义。

// readFactSources 读 compose.yaml + .env（.env 可不存在）。仅 compose 应用。
func (s *service) readFactSources(ctx context.Context, id string) (compose, envFile string, err error) {
	meta, ok, err := s.repo.GetAppMeta(ctx, id)
	if err != nil {
		return "", "", err
	}
	if !ok {
		return "", "", NotFoundErr(id)
	}
	if meta.Runtime != RuntimeCompose {
		return "", "", ValidationErr("storage/env 详情仅支持 compose 应用")
	}
	data, err := os.ReadFile(s.paths.ComposeFile(id))
	if err != nil {
		return "", "", fmt.Errorf("read compose: %w", err)
	}
	env, _ := os.ReadFile(s.paths.EnvFile(id)) // .env 可能不存在
	return string(data), string(env), nil
}

func (s *service) StorageInventory(ctx context.Context, id string) (StorageInventory, error) {
	compose, envFile, err := s.readFactSources(ctx, id)
	if err != nil {
		return StorageInventory{}, err
	}
	vols, _, err := analyzeStorage(compose, envFile)
	if err != nil {
		return StorageInventory{}, err
	}
	return StorageInventory{
		AppID:          id,
		Volumes:        vols,
		ManagedDataDir: s.paths.AppDir(id),
		Note:           "external 卷永不删除；受管命名卷仅在 purge 时删除；bind 挂载生命周期由宿主管",
	}, nil
}

func (s *service) EnvInventory(ctx context.Context, id string) (EnvInventory, error) {
	compose, envFile, err := s.readFactSources(ctx, id)
	if err != nil {
		return EnvInventory{}, err
	}
	_, vars, err := analyzeStorage(compose, envFile)
	if err != nil {
		return EnvInventory{}, err
	}
	return EnvInventory{AppID: id, Vars: vars}, nil
}

func (s *service) RemovePreview(ctx context.Context, id string, purge bool) (RemovePreview, error) {
	if _, ok, err := s.repo.GetAppMeta(ctx, id); err != nil {
		return RemovePreview{}, err
	} else if !ok {
		return RemovePreview{}, NotFoundErr(id)
	}
	pre := RemovePreview{AppID: id, Purge: purge}
	if compose, envFile, rerr := s.readFactSources(ctx, id); rerr == nil {
		if vols, _, aerr := analyzeStorage(compose, envFile); aerr == nil {
			for _, v := range vols {
				switch v.Kind {
				case VolumeExternal:
					pre.WillKeep = append(pre.WillKeep, "external volume: "+v.Source+"（永不删除）")
				case VolumeBind:
					pre.WillKeep = append(pre.WillKeep, "bind mount: "+v.Source+"（宿主管，不删）")
				case VolumeSocket:
					pre.WillKeep = append(pre.WillKeep, "socket: "+v.Source+"（不删）")
				case VolumeManaged:
					name := v.Source
					if name == "" {
						name = "anonymous → " + v.Target
					}
					if purge {
						pre.WillDelete = append(pre.WillDelete, "managed volume: "+name)
					} else {
						pre.WillKeep = append(pre.WillKeep, "managed volume: "+name+"（保留数据）")
					}
				}
			}
		}
	}
	dir := s.paths.AppDir(id)
	if purge {
		pre.WillDelete = append(pre.WillDelete, "managed data dir: "+dir+"（compose.yaml/.env/revisions）")
		pre.Note = "purge：删除受管命名卷 + 受管数据目录；external 卷与非受管 bind 永不删除。"
	} else {
		pre.WillKeep = append(pre.WillKeep, "managed data dir: "+dir+"（保留）")
		pre.Note = "默认保留数据：仅移除容器与网络，所有卷与数据目录保留。"
	}
	// 容器/网络始终由 compose down 移除。
	pre.WillDelete = append(pre.WillDelete, "containers & networks（compose down）")
	return pre, nil
}

// --- 提交 + 幂等 ---

type submitReq struct {
	appID       string
	taskType    TaskType
	action      Action
	idemKey     string
	requestHash string
	summary     string
	targetRev   int64
	purge       bool
}

func (s *service) submit(ctx context.Context, r submitReq) (Task, error) {
	task := Task{
		ID: uuid.NewString(), AppID: r.appID, Type: r.taskType, Action: r.action,
		Status: TaskQueued, Revision: r.targetRev, Purge: r.purge, IdempotencyKey: r.idemKey,
		RequestSummary: r.summary, CreatedAt: s.now(),
	}
	// task + idempotency 单事务（operate/remove）。
	if err := s.repo.CommitTask(ctx, task, r.idemKey, r.requestHash); err != nil {
		return Task{}, err
	}
	if s.runner != nil {
		s.runner.Enqueue(task.ID)
	}
	return task, nil
}

// lookupIdempotency 幂等短路：同 key 同请求返回原 task(true)；同 key 异请求返回冲突 error；
// 无记录返回 (zero, false, nil) 表示继续。必须在任何副作用前调用。
func (s *service) lookupIdempotency(ctx context.Context, key, requestHash string) (Task, bool, error) {
	rec, ok, err := s.repo.GetIdempotency(ctx, key)
	if err != nil {
		return Task{}, false, err
	}
	if !ok {
		return Task{}, false, nil
	}
	if rec.RequestHash == requestHash {
		t, err := s.repo.GetTask(ctx, rec.TaskID)
		return t, true, err
	}
	return Task{}, false, ConflictErr("idempotency_conflict",
		"idempotency key reused with a different request body")
}

func (s *service) successfulRevisionTask(ctx context.Context, appID string, revision int64) (Task, bool, error) {
	tasks, err := s.repo.ListTasksByApp(ctx, appID, 50)
	if err != nil {
		return Task{}, false, err
	}
	for _, task := range tasks {
		if task.Revision == revision && task.Status == TaskSucceeded && (task.Type == TaskApply || task.Type == TaskRestore) {
			return task, true, nil
		}
	}
	return Task{}, false, nil
}

func (s *service) audit(ctx context.Context, actor, appID, action, taskID, detail string) {
	_ = s.repo.InsertAudit(ctx, AuditRecord{
		At: s.now(), Actor: actor, AppID: appID, Action: action, TaskID: taskID, Detail: detail,
	})
}

func (s *service) writeAppMetaSidecar(meta AppRecord) {
	type sidecar struct {
		ID        string            `json:"id"`
		Name      string            `json:"name"`
		Runtime   RuntimeKind       `json:"runtime"`
		Source    ApplicationSource `json:"source"`
		Revision  int64             `json:"revision"`
		UpdatedAt time.Time         `json:"updatedAt"`
	}
	data, err := json.MarshalIndent(sidecar{
		ID: meta.ID, Name: meta.Name, Runtime: meta.Runtime, Source: meta.Source, Revision: meta.Revision, UpdatedAt: meta.UpdatedAt,
	}, "", "  ")
	if err != nil {
		return
	}
	if err := s.paths.AtomicWriteFile(meta.ID, "app.json", append(data, '\n'), 0o644); err != nil {
		s.logger.Warn("write app metadata sidecar failed", zap.String("app", meta.ID), zap.Error(err))
	}
}

func (s *service) idExists(ctx context.Context, id string) (bool, error) {
	if _, ok, err := s.repo.GetAppMeta(ctx, id); err != nil {
		return false, err
	} else if ok {
		return true, nil
	}
	if ka := s.adapters[RuntimeKubernetes]; ka != nil {
		obs, _ := ka.Observe(ctx)
		if _, ok := obs[id]; ok {
			return true, nil
		}
	}
	return false, nil
}

// --- 请求哈希（幂等比较，不含 secret/时间）---

func hashApplyRequest(d DesiredApplication) string {
	type bare struct {
		ID      string            `json:"id,omitempty"`
		Name    string            `json:"name"`
		Source  ApplicationSource `json:"source"`
		Hash    string            `json:"hash"`
		Params  map[string]string `json:"params"`
		Exp     int64             `json:"exp"`
		Secrets string            `json:"secrets,omitempty"`
		Retain  bool              `json:"retain,omitempty"`
	}
	secretDigest := ""
	if len(d.Secrets) > 0 {
		secretDigest = StoreInstallFingerprint(nil, d.Secrets)
	}
	b, _ := json.Marshal(bare{
		ID: d.ID, Name: d.Name, Source: d.Source,
		Hash: composeHash(d.ComposeContent, d.Parameters), Params: d.Parameters,
		Exp: d.ExpectedRevision, Secrets: secretDigest, Retain: d.RetainEnvironment,
	})
	return sha256hex(b)
}

func hashOperateRequest(id string, action Action) string {
	return sha256hex([]byte(fmt.Sprintf("operate|%s|%s", id, action)))
}

func hashRemoveRequest(id string, purge bool) string {
	return sha256hex([]byte(fmt.Sprintf("remove|%s|%v", id, purge)))
}

func hashRestoreRequest(id string, rev int64) string {
	return sha256hex([]byte(fmt.Sprintf("restore|%s|%d", id, rev)))
}

func composeHash(content string, params map[string]string) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var pb strings.Builder
	for _, k := range keys {
		pb.WriteString(k)
		pb.WriteByte('=')
		pb.WriteString(params[k])
		pb.WriteByte('\n')
	}
	h := sha256.New()
	h.Write([]byte(content))
	h.Write([]byte(pb.String()))
	return hex.EncodeToString(h.Sum(nil))
}

func sha256hex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
