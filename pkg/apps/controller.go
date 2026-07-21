package apps

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
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

	GetTask(ctx context.Context, taskID string) (Task, error)
	ListOperations(ctx context.Context, id string) ([]Task, error)
	GetCompose(ctx context.Context, id string) (ComposeContent, error)
	ListRevisions(ctx context.Context, id string) ([]Revision, error)
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
	Apply(ctx context.Context, app Application, composeFile string) error
	Operate(ctx context.Context, app Application, action Action) error
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

// service Controller 的默认实现。
type service struct {
	repo       Repository
	paths      *Paths
	adapters   map[RuntimeKind]runtimeAdapter
	runner     taskRunner
	prechecker composePrechecker
	logger     *zap.Logger
	now        func() time.Time
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

// NewController 构造 Controller。adapters 至少含一个运行时；runner 由 worker 提供。
func NewController(repo Repository, paths *Paths, adapters map[RuntimeKind]runtimeAdapter, runner taskRunner, logger *zap.Logger, opts ...ServiceOption) Controller {
	s := &service{
		repo:     repo,
		paths:    paths,
		adapters: adapters,
		runner:   runner,
		logger:   logger,
		now:      time.Now,
	}
	for _, o := range opts {
		o(s)
	}
	return s
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
func (s *service) observeAll(ctx context.Context) ([]Application, error) {
	var out []Application

	// Compose apps：SQLite 元数据为权威，运行态从 compose adapter 取。
	if ca, ok := s.adapters[RuntimeCompose]; ok {
		metas, err := s.repo.ListAppMetas(ctx)
		if err != nil {
			return nil, fmt.Errorf("list app metas: %w", err)
		}
		obs, _ := ca.Observe(ctx) // 不可用时空 map，不报错
		registered := map[string]bool{}
		for _, m := range metas {
			if m.Runtime != RuntimeCompose {
				continue
			}
			registered[m.ID] = true
			out = append(out, s.buildComposeApp(m, obs[m.ID], true))
		}
		// 容错：受管容器存在但元数据缺失（应不会发生），跳过——以登记为准。
	}

	// K8s apps：无 SQLite 记录，纯观测。
	if ka, ok := s.adapters[RuntimeKubernetes]; ok {
		obs, _ := ka.Observe(ctx)
		for _, app := range obs {
			out = append(out, app)
		}
	}
	return out, nil
}

// buildComposeApp 把 compose 元数据与运行态合并为对外 Application。
func (s *service) buildComposeApp(meta AppRecord, obs Application, registered bool) Application {
	app := Application{
		ID:        meta.ID,
		Name:      meta.Name,
		Kind:      "app",
		Runtime:   RuntimeCompose,
		Source:    meta.Source,
		Revision:  meta.Revision,
		CreatedAt: meta.CreatedAt,
		Namespace: ProjectName(meta.ID),
	}
	app.Observed.Revision = meta.ObservedRevision

	if obs.ID != "" {
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

// fillCompatFields 填充旧前端 useApps() 期望的兼容字段（State/Image/Ports/Replicas/Ready）。
func (s *service) fillCompatFields(app *Application) {
	app.State = app.Observed.Phase.LegacyState()
	app.Version = app.Source.Version
	var ports []PortMapping
	replicas := int32(0)
	ready := int32(0)
	for _, svc := range app.Observed.Services {
		replicas++
		state := strings.ToLower(svc.State)
		if state == "running" {
			ready++
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
		var obs map[string]Application
		if ca := s.adapters[RuntimeCompose]; ca != nil {
			obs, _ = ca.Observe(ctx)
		}
		return s.buildComposeApp(meta, obs[id], true), nil
	}
	// 否则 K8s 观测。
	if ka := s.adapters[RuntimeKubernetes]; ka != nil {
		obs, _ := ka.Observe(ctx)
		if app, ok := obs[id]; ok {
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
	rt, meta, hasMeta, err := s.resolveRuntime(ctx, id)
	if err != nil {
		return LogPage{}, err
	}
	adapter := s.adapters[rt]
	if adapter == nil {
		return LogPage{}, CapabilityErr(fmt.Sprintf("runtime %s unavailable", rt))
	}
	app := Application{ID: id, Runtime: rt}
	if hasMeta {
		app.Name = meta.Name
	}
	return adapter.Logs(ctx, app, opts)
}

// --- Validate（不落盘）---

func (s *service) Validate(ctx context.Context, req ValidateRequest) (ValidateResult, error) {
	res := ValidateResult{}
	if strings.TrimSpace(req.ComposeContent) == "" {
		res.Errors = append(res.Errors, "compose 内容为空")
		return res, nil
	}
	// 真实预检：渲染（docker compose config，无 secret——Validate 仅 params 插值）。
	// compose CLI 不可用时回退静态分析并加 warning；配置非法 → res.Errors。
	env := renderEnvFile(nil, req.Parameters)
	rendered, warn, rerr := s.renderForCheck(ctx, req.ComposeContent, env, false)
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
	res.OK = len(res.Errors) == 0 && !HasBlocked(res.Risks)
	return res, nil
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
	// 0. 幂等短路（在任何副作用前）。
	applyHash := hashApplyRequest(desired)
	if opts.IdempotencyKey != "" {
		if t, hit, err := s.lookupIdempotency(ctx, opts.IdempotencyKey, applyHash); err != nil {
			return Task{}, err
		} else if hit {
			return t, nil
		}
	}

	// 1. 真实预检：docker compose config 渲染（strict：CLI 不可用 → capability，
	//    配置非法 → validation；均不落盘）。secret 仅用于本次渲染，不入任何持久层。
	env := renderEnvFile(desired.Secrets, desired.Parameters)
	rendered, _, err := s.renderForCheck(ctx, desired.ComposeContent, env, true)
	if err != nil {
		return Task{}, err
	}
	// 2. 风险分析基于「渲染后」内容（${VAR} 已展开，无法绕过 privileged/socket/root bind）。
	findings, ferr := AnalyzeCompose(rendered)
	if ferr != nil {
		return Task{}, ValidationErr("渲染后风险分析失败: " + ferr.Error())
	}
	if HasBlocked(findings) {
		return Task{}, RiskBlockedErr("存在阻断级风险，已拒绝", findings)
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

	// 4. 暂存 compose.yaml/.env 到 staging（不覆盖现有事实源；DB 提交后再 atomic rename）。
	if err := s.paths.EnsureAppDir(appID); err != nil {
		return Task{}, err
	}
	composeStage, composeFinal, err := s.stageFile(appID, "compose.yaml", []byte(desired.ComposeContent), 0o644)
	if err != nil {
		return Task{}, err
	}
	cleanupStage := func() { os.Remove(composeStage) }
	var envStage, envFinal string
	if env != "" {
		if envStage, envFinal, err = s.stageFile(appID, ".env", []byte(env), 0o600); err != nil {
			cleanupStage()
			return Task{}, err
		}
		prev := cleanupStage
		cleanupStage = func() { prev(); os.Remove(envStage) }
	}

	// 5. revision + meta + task (+ idempotency) 单事务提交（任一失败回滚，无半状态）。
	revNum, err := s.repo.NextRevisionNumber(ctx, appID)
	if err != nil {
		cleanupStage()
		return Task{}, err
	}
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
		cleanupStage()
		return Task{}, err
	}

	// 6. 提交后提升 staging → 事实源（同目录 rename 原子）+ revision 快照。
	//    rename 失败（极罕见）补偿：清 staging 并标 task failed，状态仍一致。
	if err := os.Rename(composeStage, composeFinal); err != nil {
		cleanupStage()
		s.markTaskFailed(ctx, task.ID, "落盘 compose.yaml 失败: "+err.Error())
		s.logger.Error("promote compose.yaml after commit failed", zap.String("app", appID), zap.Error(err))
		t, _ := s.repo.GetTask(ctx, task.ID)
		return t, nil
	}
	if envStage != "" {
		if err := os.Rename(envStage, envFinal); err != nil {
			os.Remove(envStage) // .env 缺失非致命；保留 compose，继续。
			s.logger.Warn("promote .env after commit failed", zap.String("app", appID), zap.Error(err))
		}
	}
	_ = s.paths.SafeWriteFile(appID, fmt.Sprintf("revisions/%d.yaml", revNum), []byte(desired.ComposeContent), 0o644)

	// 7. 入队执行 + 审计。
	if s.runner != nil {
		s.runner.Enqueue(task.ID)
	}
	s.audit(ctx, opts.Actor, appID, "apply:rev="+itoa(revNum), task.ID, summary)
	return task, nil
}

// stageFile 写入暂存文件（同目录、唯一后缀），返回 staging 与最终绝对路径。
// DB 提交后由调用方 os.Rename(stage, final) 原子提升。
func (s *service) stageFile(appID, rel string, data []byte, mode os.FileMode) (stage, final string, err error) {
	final = filepath.Join(s.paths.AppDir(appID), rel)
	stage = final + ".stage-" + uuid.NewString()
	if err = os.WriteFile(stage, data, mode); err != nil {
		return "", "", err
	}
	return stage, final, nil
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
	// 预检历史内容（strict）+ 渲染后风险分析（策略可能已变更）。
	rendered, _, err := s.renderForCheck(ctx, string(content), "", true)
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
	if NeedsConfirmation(findings, opts.AllowRiskyConfirmation) {
		return Task{}, RiskBlockedErr("历史 revision 存在需确认的风险，请显式确认后重试", findings)
	}

	// staging compose.yaml；.env（含 secret）保留现状不动。
	composeStage, composeFinal, err := s.stageFile(id, "compose.yaml", content, 0o644)
	if err != nil {
		return Task{}, err
	}
	now := s.now()
	newRev, err := s.repo.NextRevisionNumber(ctx, id)
	if err != nil {
		os.Remove(composeStage)
		return Task{}, err
	}
	newRevision := Revision{
		Number: newRev, AppID: id, ComposeHash: r.ComposeHash, Source: r.Source,
		Parameters: r.Parameters, CreatedAt: now, CreatedBy: opts.Actor,
		Note: fmt.Sprintf("restored from revision %d", rev),
	}
	meta.Revision = newRev
	meta.UpdatedAt = now
	task := Task{
		ID: uuid.NewString(), AppID: id, Type: TaskRestore,
		Status: TaskQueued, Revision: newRev, IdempotencyKey: opts.IdempotencyKey,
		RequestSummary: fmt.Sprintf("restore rev %d→%d", rev, newRev), CreatedAt: now,
	}
	if err := s.repo.CommitApply(ctx, meta, newRevision, task, opts.IdempotencyKey, restoreHash); err != nil {
		os.Remove(composeStage)
		return Task{}, err
	}
	if err := os.Rename(composeStage, composeFinal); err != nil {
		os.Remove(composeStage)
		s.markTaskFailed(ctx, task.ID, "落盘 compose.yaml 失败: "+err.Error())
		s.logger.Error("promote compose.yaml after restore failed", zap.String("app", id), zap.Error(err))
		t, _ := s.repo.GetTask(ctx, task.ID)
		return t, nil
	}
	_ = s.paths.SafeWriteFile(id, fmt.Sprintf("revisions/%d.yaml", newRev), content, 0o644)
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

func (s *service) audit(ctx context.Context, actor, appID, action, taskID, detail string) {
	_ = s.repo.InsertAudit(ctx, AuditRecord{
		At: s.now(), Actor: actor, AppID: appID, Action: action, TaskID: taskID, Detail: detail,
	})
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
		ID     string            `json:"id,omitempty"`
		Name   string            `json:"name"`
		Source ApplicationSource `json:"source"`
		Hash   string            `json:"hash"`
		Params map[string]string `json:"params"`
		Exp    int64             `json:"exp"`
	}
	b, _ := json.Marshal(bare{
		ID: d.ID, Name: d.Name, Source: d.Source,
		Hash: composeHash(d.ComposeContent, d.Parameters), Params: d.Parameters,
		Exp: d.ExpectedRevision,
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
