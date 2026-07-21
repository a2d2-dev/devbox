package apps

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// fakeAdapter / fakeRunner 用于隔离测试 controller 协调逻辑（不触达真实 docker/k8s）。

type fakeAdapter struct {
	kind       RuntimeKind
	observed   map[string]Application
	applyErr   error
	operateErr error
	removeErr  error
	delay      time.Duration // Apply/Operate 模拟耗时（串行测试用）
	panicsRemaining int      // Apply 触发 panic 的次数（测试 recover）
	applyNoObserve bool      // Apply 成功但不产生运行态（测试 verifyObserved 失败）
	mu         sync.Mutex
	applied    []string
	applyTimes []time.Time
	operated   []string
	removed    []string
}

func (f *fakeAdapter) Kind() RuntimeKind { return f.kind }
func (f *fakeAdapter) Observe(context.Context) (map[string]Application, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	// 返回副本，避免测试间共享底层数据。
	out := make(map[string]Application, len(f.observed))
	for k, v := range f.observed {
		out[k] = v
	}
	return out, nil
}
func (f *fakeAdapter) Apply(_ context.Context, app Application, _ string) error {
	if f.delay > 0 {
		time.Sleep(f.delay)
	}
	if f.panicsRemaining > 0 {
		f.mu.Lock()
		f.panicsRemaining--
		f.mu.Unlock()
		panic("simulated adapter panic")
	}
	f.mu.Lock()
	f.applied = append(f.applied, app.ID)
	f.applyTimes = append(f.applyTimes, time.Now())
	// 模拟 apply 成功后容器实际出现（供 worker 的 verifyObserved 通过）。
	if f.applyErr == nil && !f.applyNoObserve {
		f.setObservedLocked(app.ID, ActionStart)
	}
	f.mu.Unlock()
	return f.applyErr
}
func (f *fakeAdapter) Operate(_ context.Context, app Application, action Action) error {
	f.mu.Lock()
	f.operated = append(f.operated, app.ID+":"+string(action))
	if f.operateErr == nil {
		f.setObservedLocked(app.ID, action)
	}
	f.mu.Unlock()
	return f.operateErr
}

// setObservedLocked 按动作设置模拟运行态（调用方持锁）。
func (f *fakeAdapter) setObservedLocked(appID string, action Action) {
	if f.observed == nil {
		f.observed = map[string]Application{}
	}
	switch action {
	case ActionStop:
		f.observed[appID] = Application{ID: appID, Observed: ObservedState{Phase: PhaseStopped, Services: []ServiceStatus{{Name: "web", State: "exited"}}}}
	default: // start/restart/redeploy/apply
		f.observed[appID] = Application{ID: appID, Observed: ObservedState{Phase: PhaseRunning, Services: []ServiceStatus{{Name: "web", State: "running"}}}}
	}
}
func (f *fakeAdapter) Remove(_ context.Context, app Application, _ bool) error {
	f.mu.Lock()
	f.removed = append(f.removed, app.ID)
	f.mu.Unlock()
	return f.removeErr
}
func (f *fakeAdapter) Logs(_ context.Context, app Application, _ LogOptions) (LogPage, error) {
	return LogPage{AppID: app.ID, Logs: "fake"}, nil
}

type fakeRunner struct {
	mu     sync.Mutex
	queued []string
}

func (f *fakeRunner) Enqueue(id string) {
	f.mu.Lock()
	f.queued = append(f.queued, id)
	f.mu.Unlock()
}

// echoPrechecker 模拟「渲染成功」：直接透传 content（供单元测试跑通预检/风险路径，
// 不依赖真实 docker compose 二进制）。
type echoPrechecker struct {
	err error // 非空时渲染返回该错误
}

func (e *echoPrechecker) RenderConfig(_ context.Context, content, _ string) (string, error) {
	if e.err != nil {
		return "", e.err
	}
	return content, nil
}

func newTestController(t *testing.T, adapters map[RuntimeKind]runtimeAdapter) (Controller, *fakeRunner, Repository, *Paths) {
	t.Helper()
	dir := t.TempDir()
	repo, err := OpenRepository(context.Background(), filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	paths := NewPaths(dir)
	runner := &fakeRunner{}
	ctrl := NewController(repo, paths, adapters, runner, zap.NewNop(),
		WithPrechecker(&echoPrechecker{}),
		WithClock(func() time.Time {
			return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		}))
	t.Cleanup(func() { _ = repo.Close() })
	return ctrl, runner, repo, paths
}

const safeCompose = `services:
  web:
    image: nginx:1.27
    ports: ["8080:80"]`

func TestControllerApplyCreate(t *testing.T) {
	compose := &fakeAdapter{kind: RuntimeCompose}
	ctrl, runner, repo, paths := newTestController(t, map[RuntimeKind]runtimeAdapter{RuntimeCompose: compose})

	task, err := ctrl.Apply(context.Background(), DesiredApplication{
		Name: "My App", Source: ApplicationSource{Kind: SourceInline},
		ComposeContent: safeCompose,
		Parameters:     map[string]string{"PORT": "8080"},
	}, ApplyOptions{Actor: "tester"})
	require.NoError(t, err)
	assert.Equal(t, TaskQueued, task.Status)
	assert.Equal(t, int64(1), task.Revision)
	assert.Contains(t, runner.queued, task.ID)

	// 元数据落盘。
	meta, ok, err := repo.GetAppMeta(context.Background(), "my-app")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "My App", meta.Name)
	assert.Equal(t, int64(1), meta.Revision)
	// compose 文件落盘（事实源）。
	assert.FileExists(t, paths.ComposeFile("my-app"))
	assert.FileExists(t, paths.RevisionFile("my-app", 1))
	// secret 不进 revision/audit（这里无 secret，验证参数快照在）。
	revs, _ := repo.ListRevisions(context.Background(), "my-app")
	require.Len(t, revs, 1)
	assert.Equal(t, "8080", revs[0].Parameters["PORT"])
}

func TestControllerApplyRevisionMismatch(t *testing.T) {
	ctrl, _, repo, _ := newTestController(t, map[RuntimeKind]runtimeAdapter{RuntimeCompose: &fakeAdapter{kind: RuntimeCompose}})
	ctx := context.Background()
	// 首次创建 → rev1。
	_, err := ctrl.Apply(ctx, DesiredApplication{Name: "app1", ComposeContent: safeCompose}, ApplyOptions{})
	require.NoError(t, err)
	// 更新 expectedRevision=1 → rev2 成功。
	_, err = ctrl.Apply(ctx, DesiredApplication{Name: "app1", ID: "app1", ComposeContent: safeCompose, ExpectedRevision: 1}, ApplyOptions{})
	require.NoError(t, err)
	// 再用 stale expectedRevision=1（实际 2）→ 409。
	_, err = ctrl.Apply(ctx, DesiredApplication{Name: "app1", ID: "app1", ComposeContent: safeCompose, ExpectedRevision: 1}, ApplyOptions{})
	ae, ok := AsError(err)
	require.True(t, ok)
	assert.Equal(t, ErrKindConflict, ae.Kind)
	assert.Equal(t, "revision_mismatch", ae.Reason)
	// 库未被改写：仍 rev2。
	meta, _, _ := repo.GetAppMeta(ctx, "app1")
	assert.Equal(t, int64(2), meta.Revision)
}

func TestControllerApplyBlockedRisk(t *testing.T) {
	ctrl, _, _, _ := newTestController(t, map[RuntimeKind]runtimeAdapter{RuntimeCompose: &fakeAdapter{kind: RuntimeCompose}})
	dangerous := `services:
  web:
    image: nginx:1.27
    privileged: true`
	_, err := ctrl.Apply(context.Background(), DesiredApplication{Name: "bad", ComposeContent: dangerous}, ApplyOptions{})
	ae, ok := AsError(err)
	require.True(t, ok)
	assert.Equal(t, ErrKindRiskBlocked, ae.Kind)
}

func TestControllerApplyConfirmationRequiresExplicit(t *testing.T) {
	ctrl, _, _, _ := newTestController(t, map[RuntimeKind]runtimeAdapter{RuntimeCompose: &fakeAdapter{kind: RuntimeCompose}})
	confirm := `services:
  web:
    image: nginx:1.27
    cap_add: [SYS_ADMIN]`
	// 未确认 → 阻断。
	_, err := ctrl.Apply(context.Background(), DesiredApplication{Name: "c", ComposeContent: confirm}, ApplyOptions{})
	ae, ok := AsError(err)
	require.True(t, ok)
	assert.Equal(t, ErrKindRiskBlocked, ae.Kind)
	// 显式确认 → 通过。
	_, err = ctrl.Apply(context.Background(), DesiredApplication{Name: "c", ComposeContent: confirm}, ApplyOptions{AllowRiskyConfirmation: true})
	require.NoError(t, err)
}

func TestControllerApplyIdempotent(t *testing.T) {
	ctrl, runner, _, _ := newTestController(t, map[RuntimeKind]runtimeAdapter{RuntimeCompose: &fakeAdapter{kind: RuntimeCompose}})
	desired := DesiredApplication{Name: "idem", ComposeContent: safeCompose}
	t1, err := ctrl.Apply(context.Background(), desired, ApplyOptions{IdempotencyKey: "k1"})
	require.NoError(t, err)
	// 同 key 同请求 → 返回同一 task。
	t2, err := ctrl.Apply(context.Background(), desired, ApplyOptions{IdempotencyKey: "k1"})
	require.NoError(t, err)
	assert.Equal(t, t1.ID, t2.ID)
	// runner 只入队一次（幂等命中不再创建新 task）。
	assert.Len(t, runner.queued, 1)

	// 同 key 不同请求 → 409。
	_, err = ctrl.Apply(context.Background(), DesiredApplication{Name: "idem", ComposeContent: safeCompose + "\n# changed"}, ApplyOptions{IdempotencyKey: "k1"})
	ae, ok := AsError(err)
	require.True(t, ok)
	assert.Equal(t, ErrKindConflict, ae.Kind)
	assert.Equal(t, "idempotency_conflict", ae.Reason)
}

func TestControllerValidate(t *testing.T) {
	ctrl, _, _, _ := newTestController(t, nil)
	// 合法。
	res, err := ctrl.Validate(context.Background(), ValidateRequest{ComposeContent: safeCompose})
	require.NoError(t, err)
	assert.True(t, res.OK)
	assert.Len(t, res.Services, 1)
	// 阻断。
	res, _ = ctrl.Validate(context.Background(), ValidateRequest{ComposeContent: "services:\n  a:\n    image: nginx:1.27\n    privileged: true"})
	assert.False(t, res.OK)
	assert.True(t, len(res.Risks) > 0)
}

func TestControllerListMerge(t *testing.T) {
	compose := &fakeAdapter{kind: RuntimeCompose}
	k8s := &fakeAdapter{
		kind: RuntimeKubernetes,
		observed: map[string]Application{
			"kb-deploy": {ID: "kb-deploy", Name: "kb-deploy", Runtime: RuntimeKubernetes, Kind: "app",
				Observed: ObservedState{Phase: PhaseRunning}},
		},
	}
	ctrl, _, repo, _ := newTestController(t, map[RuntimeKind]runtimeAdapter{RuntimeCompose: compose, RuntimeKubernetes: k8s})
	ctx := context.Background()
	// 登记一个 compose app（无运行态 → stopped）。
	_, err := ctrl.Apply(ctx, DesiredApplication{Name: "down-app", ComposeContent: safeCompose}, ApplyOptions{})
	require.NoError(t, err)

	// 模拟 compose 运行态：再登记一个有运行态的。
	require.NoError(t, repo.UpsertAppMeta(ctx, AppRecord{ID: "up-app", Name: "up-app", Runtime: RuntimeCompose, Revision: 1, CreatedAt: time.Now(), UpdatedAt: time.Now(), ObservedRevision: 1}))
	compose.observed = map[string]Application{
		"up-app": {ID: "up-app", Observed: ObservedState{Phase: PhaseRunning, Services: []ServiceStatus{{Name: "web", Image: "nginx:1.27", State: "running", Health: "healthy"}}}},
	}

	list, err := ctrl.List(ctx, Filter{})
	require.NoError(t, err)
	byID := map[string]Application{}
	for _, a := range list {
		byID[a.ID] = a
	}
	// down-app：apply 已提交但 worker 未执行（revision>observed）→ deploying/pending。
	if a, ok := byID["down-app"]; ok {
		assert.Equal(t, PhaseDeploying, a.Observed.Phase)
		assert.Equal(t, "pending", a.State)
		assert.Equal(t, RuntimeCompose, a.Runtime)
	} else {
		t.Fatal("down-app missing")
	}
	// up-app：运行态 → running，兼容字段填充。
	if a, ok := byID["up-app"]; ok {
		assert.Equal(t, PhaseRunning, a.Observed.Phase)
		assert.Equal(t, "running", a.State)
		assert.Equal(t, int32(1), a.Replicas)
		assert.Equal(t, int32(1), a.Ready)
		assert.Equal(t, "nginx:1.27", a.Image)
	} else {
		t.Fatal("up-app missing")
	}
	// K8s app 也在列表。
	if a, ok := byID["kb-deploy"]; ok {
		assert.Equal(t, RuntimeKubernetes, a.Runtime)
	} else {
		t.Fatal("kb-deploy missing")
	}
}

func TestControllerListFilter(t *testing.T) {
	adapters := map[RuntimeKind]runtimeAdapter{
		RuntimeCompose:    &fakeAdapter{kind: RuntimeCompose},
		RuntimeKubernetes: &fakeAdapter{kind: RuntimeKubernetes, observed: map[string]Application{"k": {ID: "k", Runtime: RuntimeKubernetes}}},
	}
	ctrl, _, _, _ := newTestController(t, adapters)
	list, err := ctrl.List(context.Background(), Filter{Runtime: RuntimeKubernetes})
	require.NoError(t, err)
	assert.Len(t, list, 1)
	assert.Equal(t, RuntimeKubernetes, list[0].Runtime)
}

func TestControllerOperateRemoveSubmit(t *testing.T) {
	compose := &fakeAdapter{kind: RuntimeCompose}
	ctrl, runner, repo, _ := newTestController(t, map[RuntimeKind]runtimeAdapter{RuntimeCompose: compose})
	ctx := context.Background()
	_, err := ctrl.Apply(ctx, DesiredApplication{Name: "op-app", ComposeContent: safeCompose}, ApplyOptions{})
	require.NoError(t, err)

	// operate
	t1, err := ctrl.Operate(ctx, "op-app", ActionStop, OperationOptions{})
	require.NoError(t, err)
	assert.Equal(t, TaskOperate, t1.Type)
	assert.Contains(t, runner.queued, t1.ID)

	// 不存在的 app
	_, err = ctrl.Operate(ctx, "nope", ActionStart, OperationOptions{})
	ae, ok := AsError(err)
	require.True(t, ok)
	assert.Equal(t, ErrKindNotFound, ae.Kind)

	// remove
	t2, err := ctrl.Remove(ctx, "op-app", RemoveOptions{Purge: true})
	require.NoError(t, err)
	assert.True(t, t2.Purge)

	// meta 仍在（remove 由 worker 执行后才删；此处仅提交）。
	_, ok, _ = repo.GetAppMeta(ctx, "op-app")
	assert.True(t, ok)
}

// newTestControllerWithPrechecker 用指定 prechecker 构造 controller（测试预检拒绝路径）。
func newTestControllerWithPrechecker(t *testing.T, adapters map[RuntimeKind]runtimeAdapter, pc composePrechecker) (Controller, *fakeRunner, Repository, *Paths) {
	t.Helper()
	dir := t.TempDir()
	repo, err := OpenRepository(context.Background(), filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	paths := NewPaths(dir)
	runner := &fakeRunner{}
	ctrl := NewController(repo, paths, adapters, runner, zap.NewNop(),
		WithPrechecker(pc),
		WithClock(func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }))
	t.Cleanup(func() { _ = repo.Close() })
	return ctrl, runner, repo, paths
}

// HIGH#3：预检失败（配置非法）时不得落盘/建 revision/建 task。
func TestControllerApplyPrecheckRejectsBeforePersist(t *testing.T) {
	compose := &fakeAdapter{kind: RuntimeCompose}
	ctrl, runner, repo, paths := newTestControllerWithPrechecker(t,
		map[RuntimeKind]runtimeAdapter{RuntimeCompose: compose},
		&echoPrechecker{err: ValidationErr("compose 配置无效: syntax")})

	_, err := ctrl.Apply(context.Background(), DesiredApplication{
		Name: "bad-app", ComposeContent: "garbage", Source: ApplicationSource{Kind: SourceInline},
	}, ApplyOptions{Actor: "tester"})
	ae, ok := AsError(err)
	require.True(t, ok)
	assert.Equal(t, ErrKindValidation, ae.Kind)

	// 无任何副作用：无文件、无 meta、无 revision、无 task、未入队。
	assert.NoFileExists(t, paths.ComposeFile("bad-app"))
	_, okMeta, _ := repo.GetAppMeta(context.Background(), "bad-app")
	assert.False(t, okMeta)
	revs, _ := repo.ListRevisions(context.Background(), "bad-app")
	assert.Empty(t, revs)
	assert.Empty(t, runner.queued)
}

// HIGH#3：compose CLI 不可用（capability）时 Apply 明确拒绝，不落盘。
func TestControllerApplyCapabilityWhenPrecheckUnavailable(t *testing.T) {
	compose := &fakeAdapter{kind: RuntimeCompose}
	ctrl, runner, repo, paths := newTestControllerWithPrechecker(t,
		map[RuntimeKind]runtimeAdapter{RuntimeCompose: compose},
		&echoPrechecker{err: CapabilityErr("docker compose 不可用")})

	_, err := ctrl.Apply(context.Background(), DesiredApplication{
		Name: "cap-app", ComposeContent: safeCompose, Source: ApplicationSource{Kind: SourceInline},
	}, ApplyOptions{})
	ae, ok := AsError(err)
	require.True(t, ok)
	assert.Equal(t, ErrKindCapability, ae.Kind)
	assert.NoFileExists(t, paths.ComposeFile("cap-app"))
	_, okMeta, _ := repo.GetAppMeta(context.Background(), "cap-app")
	assert.False(t, okMeta)
	assert.Empty(t, runner.queued)
}

// HIGH#2：RestoreRevision 对不存在的 app 返回 NotFound，不以空 ID 污染表。
func TestControllerRestoreRevisionAppNotFound(t *testing.T) {
	compose := &fakeAdapter{kind: RuntimeCompose}
	ctrl, _, repo, _ := newTestController(t, map[RuntimeKind]runtimeAdapter{RuntimeCompose: compose})
	ctx := context.Background()
	// 先建一个 app + revision，再单独 restore 一个不存在的 app。
	_, err := ctrl.Apply(ctx, DesiredApplication{Name: "exists", ComposeContent: safeCompose}, ApplyOptions{})
	require.NoError(t, err)

	_, err = ctrl.RestoreRevision(ctx, "ghost-app", 1, ApplyOptions{})
	ae, ok := AsError(err)
	require.True(t, ok)
	assert.Equal(t, ErrKindNotFound, ae.Kind)

	// ghost-app 不应被写入 apps 表（无空 ID 行）。
	metas, err := repo.ListAppMetas(ctx)
	require.NoError(t, err)
	for _, m := range metas {
		assert.NotEqual(t, "", m.ID, "不应存在空 ID 元数据行")
		assert.NotEqual(t, "ghost-app", m.ID)
	}
}

// MED#7：渲染后风险分析覆盖 ${VAR} 绕过 —— 真实渲染展开验证见
// compose_cli_test.TestRenderConfigInterpolationBlocked（使用真实 docker compose 二进制）。

