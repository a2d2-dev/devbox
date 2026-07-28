package apps

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fixedNow() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }

// TestResolveDiscoveredID 主/副/salt 候选与耗尽返回空串（冲突错误信号）。
func TestResolveDiscoveredID(t *testing.T) {
	project := "gitea"
	primary := ExternalID(project)
	alt := DiscoveredAltID(project)
	require.NotEmpty(t, primary)
	require.NotEmpty(t, alt)
	require.NotEqual(t, primary, alt)

	t.Run("primary_when_free", func(t *testing.T) {
		got := resolveDiscoveredID(project, map[string]bool{})
		assert.Equal(t, primary, got)
	})
	t.Run("alt_when_primary_claimed", func(t *testing.T) {
		got := resolveDiscoveredID(project, map[string]bool{primary: true})
		assert.Equal(t, alt, got)
	})
	t.Run("salt_when_both_claimed", func(t *testing.T) {
		claimed := map[string]bool{primary: true, alt: true}
		got := resolveDiscoveredID(project, claimed)
		assert.NotEmpty(t, got, "应用尽 salt 候选而非返回冲突的 ID")
		assert.False(t, got == primary || got == alt, "应为第三候选")
		assert.True(t, isValidAppID(got))
	})
	t.Run("empty_when_all_exhausted", func(t *testing.T) {
		claimed := map[string]bool{primary: true, alt: true}
		for _, salt := range []string{"x", "y", "z", "w"} {
			claimed[discoveredIDWithPrefix(ExternalIDPrefix, salt+"\x00"+project)] = true
		}
		assert.Empty(t, resolveDiscoveredID(project, claimed), "全部候选被占应返回空串信号")
	})
	t.Run("stable", func(t *testing.T) {
		claimed := map[string]bool{primary: true}
		assert.Equal(t, resolveDiscoveredID(project, claimed), resolveDiscoveredID(project, claimed))
	})
}

// fakeDiscovered 构造一个 compose fake observed 条目（按 RuntimeProject 重键为 project）。
func fakeDiscovered(project string) Application {
	return Application{
		RuntimeProject:      project,
		ObservedWorkingDir:  "/home/user/" + project,
		ObservedConfigFiles: []string{"/home/user/" + project + "/compose.yaml"},
		Observed:            ObservedState{Phase: PhaseRunning, Services: []ServiceStatus{{Name: "web", State: "running"}}},
	}
}

// TestDiscoverUnregisteredDevboxProjectVisible 未登记的 devbox-* project 也必须作为
// discovered 展示（prefix 非所有权证据）。
func TestDiscoverUnregisteredDevboxProjectVisible(t *testing.T) {
	compose := &fakeAdapter{kind: RuntimeCompose, observed: map[string]Application{
		"x": fakeDiscovered("devbox-foo"),
	}}
	ctrl, _, _, _ := newTestController(t, map[RuntimeKind]runtimeAdapter{RuntimeCompose: compose})
	list, err := ctrl.List(context.Background(), Filter{})
	require.NoError(t, err)
	var found *Application
	for i := range list {
		if list[i].Name == "devbox-foo" {
			found = &list[i]
		}
	}
	require.NotNil(t, found, "未登记的 devbox-foo 必须可见")
	assert.Equal(t, OwnershipDiscovered, found.Ownership)
	require.NotNil(t, found.Discovered)
	assert.Equal(t, "devbox-foo", found.Discovered.Project)
	assert.True(t, found.Discovered.ReadOnly)
}

// TestDiscoverWriteProtectionBeforeTakeover 未接管 project 的写操作一律拒绝（not_managed）。
func TestDiscoverWriteProtectionBeforeTakeover(t *testing.T) {
	compose := &fakeAdapter{kind: RuntimeCompose, observed: map[string]Application{
		"x": fakeDiscovered("gitea"),
	}}
	ctrl, _, repo, _ := newTestController(t, map[RuntimeKind]runtimeAdapter{RuntimeCompose: compose})
	ctx := context.Background()

	list, err := ctrl.List(ctx, Filter{})
	require.NoError(t, err)
	var discoveredID string
	for _, a := range list {
		if a.Ownership == OwnershipDiscovered {
			discoveredID = a.ID
		}
	}
	require.NotEmpty(t, discoveredID)

	_, err = ctrl.Operate(ctx, discoveredID, ActionStart, OperationOptions{})
	require.Error(t, err)
	ae, ok := AsError(err)
	require.True(t, ok, "应返回领域错误")
	assert.Equal(t, ErrKindValidation, ae.Kind)
	assert.Equal(t, "not_managed", ae.Reason)

	_, err = ctrl.Remove(ctx, discoveredID, RemoveOptions{})
	ae2, ok := AsError(err)
	require.True(t, ok)
	assert.Equal(t, "not_managed", ae2.Reason)

	// 未受影响：没有产生 meta/任务。
	metas, err := repo.ListAppMetas(ctx)
	require.NoError(t, err)
	assert.Empty(t, metas)
}

// TestDiscoverNotBlockedAfterTakeover 接管后（有 meta + OriginalProject）isDiscovered=false，
// Operate 不再被 not_managed 拦截。
func TestDiscoverNotBlockedAfterTakeover(t *testing.T) {
	project := "gitea"
	compose := &fakeAdapter{kind: RuntimeCompose, observed: map[string]Application{
		"x": fakeDiscovered(project),
	}}
	ctrl, _, repo, _ := newTestController(t, map[RuntimeKind]runtimeAdapter{RuntimeCompose: compose})
	ctx := context.Background()

	// 模拟接管结果：受管 meta 用 discovered 稳定 ID + OriginalProject 原地管理。
	id := ExternalID(project)
	now := fixedNow()
	require.NoError(t, repo.UpsertAppMeta(ctx, AppRecord{
		ID: id, Name: project, Runtime: RuntimeCompose,
		Source: ApplicationSource{Kind: SourceLocal}, OriginalProject: project,
		Revision: 1, ObservedRevision: 1, CreatedAt: now, UpdatedAt: now,
	}))

	// isDiscovered 必须为 false（meta 已存在）。
	svc := ctrl.(*service)
	assert.False(t, svc.isDiscovered(ctx, id), "接管后不得判为 discovered")

	// Operate 应返回任务而非 not_managed。
	task, err := ctrl.Operate(ctx, id, ActionStart, OperationOptions{})
	require.NoError(t, err)
	assert.NotEmpty(t, task.ID)

	// List 中该 project 不再以 discovered 出现（已受管）。
	list, err := ctrl.List(ctx, Filter{})
	require.NoError(t, err)
	for _, a := range list {
		if a.Discovered != nil && a.Discovered.Project == project {
			t.Fatalf("已接管的 project 不应再出现为 discovered")
		}
	}
}

// TestDiscoverIDConflictBothShownStable discovered 主候选与历史受管 meta 冲突时：
// 双方都展示，discovered 回退第二稳定候选，且跨调用稳定。
func TestDiscoverIDConflictBothShownStable(t *testing.T) {
	project := "gitea"
	compose := &fakeAdapter{kind: RuntimeCompose, observed: map[string]Application{
		"x": fakeDiscovered(project),
	}}
	ctrl, _, repo, _ := newTestController(t, map[RuntimeKind]runtimeAdapter{RuntimeCompose: compose})
	ctx := context.Background()

	// 历史合法受管 app 占用了 discovered 主候选 ID（ExternalID(project)）。
	primary := ExternalID(project)
	now := fixedNow()
	require.NoError(t, repo.UpsertAppMeta(ctx, AppRecord{
		ID: primary, Name: "legacy-" + project, Runtime: RuntimeCompose,
		Revision: 1, ObservedRevision: 1, CreatedAt: now, UpdatedAt: now,
	}))

	list1, err := ctrl.List(ctx, Filter{})
	require.NoError(t, err)
	var managedSeen, discoveredSeen bool
	var discoveredID string
	for _, a := range list1 {
		if a.ID == primary {
			managedSeen = true
			assert.Equal(t, OwnershipManaged, a.Ownership, "历史受管 meta 必须仍为 managed")
		}
		if a.Ownership == OwnershipDiscovered && a.Discovered != nil && a.Discovered.Project == project {
			discoveredSeen = true
			discoveredID = a.ID
		}
	}
	assert.True(t, managedSeen, "历史受管 app 必须仍展示")
	assert.True(t, discoveredSeen, "冲突时 discovered 也必须展示（不能隐藏）")
	assert.NotEmpty(t, discoveredID)
	assert.NotEqual(t, primary, discoveredID, "discovered 应回退第二候选而非复用冲突 ID")
	assert.Equal(t, DiscoveredAltID(project), discoveredID)

	// 稳定：再次 List 得到同一 discovered ID。
	list2, err := ctrl.List(ctx, Filter{})
	require.NoError(t, err)
	for _, a := range list2 {
		if a.Ownership == OwnershipDiscovered && a.Discovered != nil && a.Discovered.Project == project {
			assert.Equal(t, discoveredID, a.ID, "discovered ID 必须稳定")
		}
	}

	// Get(discoveredID) 也能命中（与 list 同算法）。
	got, err := ctrl.Get(ctx, discoveredID)
	require.NoError(t, err)
	assert.Equal(t, OwnershipDiscovered, got.Ownership)
}

// TestDiscoverIDAvoidsK8sAppCollision discovered 主候选与某 K8s app ID 碰撞时回退第二候选；
// K8s app 与 discovered 双方都展示，Get 不歧义。
func TestDiscoverIDAvoidsK8sAppCollision(t *testing.T) {
	project := "k8scollide"
	primary := ExternalID(project) // K8s app 占用 discovered 主候选 ID
	compose := &fakeAdapter{kind: RuntimeCompose, observed: map[string]Application{
		"x": fakeDiscovered(project),
	}}
	k8s := &fakeAdapter{kind: RuntimeKubernetes, observed: map[string]Application{
		primary: {ID: primary, Name: "k8s-app", Runtime: RuntimeKubernetes},
	}}
	dir := t.TempDir()
	repo, err := OpenRepository(context.Background(), join(dir, "test.db"))
	require.NoError(t, err)
	defer repo.Close()
	paths := NewPaths(dir)
	ctrl := NewController(repo, paths, map[RuntimeKind]runtimeAdapter{
		RuntimeCompose: compose, RuntimeKubernetes: k8s,
	}, &fakeRunner{}, zapNopTestLogger(),
		WithPrechecker(&echoPrechecker{}),
		WithClock(func() time.Time { return fixedNow() }))
	ctx := context.Background()

	list, err := ctrl.List(ctx, Filter{})
	require.NoError(t, err)

	var k8sSeen, discSeen bool
	var discID string
	for _, a := range list {
		if a.ID == primary && a.Runtime == RuntimeKubernetes {
			k8sSeen = true
		}
		if a.Ownership == OwnershipDiscovered && a.Discovered != nil && a.Discovered.Project == project {
			discSeen = true
			discID = a.ID
		}
	}
	require.True(t, k8sSeen, "K8s app 必须仍展示")
	require.True(t, discSeen, "discovered 必须展示（不隐藏）")
	require.NotEqual(t, primary, discID, "discovered 应回退第二候选避开 K8s app ID")
	assert.Equal(t, DiscoveredAltID(project), discID)

	// Get 不歧义：primary→K8s（无 Discovered），discID→discovered。
	k8sApp, err := ctrl.Get(ctx, primary)
	require.NoError(t, err)
	assert.Nil(t, k8sApp.Discovered)
	discApp, err := ctrl.Get(ctx, discID)
	require.NoError(t, err)
	require.NotNil(t, discApp.Discovered)
	assert.Equal(t, project, discApp.Discovered.Project)
}

func join(parts ...string) string   { return filepath.Join(parts...) }
func zapNopTestLogger() *zap.Logger { return zap.NewNop() }

// TestObserveAllSingleSnapshotPerAdapter observeAll 对每个 adapter 只 Observe 一次（基于同一
// 快照构建 claimed 与输出），且输出 ID 唯一；K8s app 占用 discovered 主候选时不碰撞。
func TestObserveAllSingleSnapshotPerAdapter(t *testing.T) {
	project := "snapapp"
	primary := ExternalID(project) // K8s app 占用 discovered 主候选 → discovered 用第二候选
	compose := &fakeAdapter{kind: RuntimeCompose, observed: map[string]Application{"x": fakeDiscovered(project)}}
	k8s := &fakeAdapter{kind: RuntimeKubernetes, observed: map[string]Application{
		primary: {ID: primary, Name: "k8s-snap", Runtime: RuntimeKubernetes},
	}}
	dir := t.TempDir()
	repo, err := OpenRepository(context.Background(), filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	defer repo.Close()
	ctrl := NewController(repo, NewPaths(dir), map[RuntimeKind]runtimeAdapter{
		RuntimeCompose: compose, RuntimeKubernetes: k8s,
	}, &fakeRunner{}, zap.NewNop(), WithPrechecker(&echoPrechecker{}),
		WithClock(func() time.Time { return fixedNow() }))

	list, err := ctrl.List(context.Background(), Filter{})
	require.NoError(t, err)
	assert.Equal(t, 1, compose.observeCalls, "compose Observe 应只调一次")
	assert.Equal(t, 1, k8s.observeCalls, "k8s Observe 应只调一次")

	// 输出 ID 唯一（K8s app 与 discovered 不碰撞）。
	seen := map[string]int{}
	for _, a := range list {
		seen[a.ID]++
	}
	for id, n := range seen {
		assert.Equal(t, 1, n, "输出 ID 重复: "+id)
	}
}
