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
	mu         sync.Mutex
	applied    []string
	applyTimes []time.Time
	operated   []string
	removed    []string
}

func (f *fakeAdapter) Kind() RuntimeKind { return f.kind }
func (f *fakeAdapter) Observe(context.Context) (map[string]Application, error) {
	return f.observed, nil
}
func (f *fakeAdapter) Apply(_ context.Context, app Application, _ string) error {
	if f.delay > 0 {
		time.Sleep(f.delay)
	}
	f.mu.Lock()
	f.applied = append(f.applied, app.ID)
	f.applyTimes = append(f.applyTimes, time.Now())
	f.mu.Unlock()
	return f.applyErr
}
func (f *fakeAdapter) Operate(_ context.Context, app Application, action Action) error {
	f.mu.Lock()
	f.operated = append(f.operated, app.ID+":"+string(action))
	f.mu.Unlock()
	return f.operateErr
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

func newTestController(t *testing.T, adapters map[RuntimeKind]runtimeAdapter) (Controller, *fakeRunner, Repository, *Paths) {
	t.Helper()
	dir := t.TempDir()
	repo, err := OpenRepository(context.Background(), filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	paths := NewPaths(dir)
	runner := &fakeRunner{}
	ctrl := NewController(repo, paths, adapters, runner, zap.NewNop(), WithClock(func() time.Time {
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
