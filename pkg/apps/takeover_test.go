package apps

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// fakeTakeoverRenderer 注入 Takeover 的归一化器（避免真实 docker compose）。
type fakeTakeoverRenderer struct {
	normalized  string
	socketPath  string
	err         error
	callCount   int
	lastFiles   []string
	lastEnvFile string
	lastBodies  []string
}

func (f *fakeTakeoverRenderer) RenderProjectConfig(_ context.Context, _, _ string, files []string, envFile string, _ bool) (string, error) {
	f.callCount++
	f.lastFiles = append([]string(nil), files...)
	f.lastEnvFile = envFile
	f.lastBodies = nil
	for _, p := range files {
		b, _ := os.ReadFile(p)
		f.lastBodies = append(f.lastBodies, string(b))
	}
	if f.err != nil {
		return "", f.err
	}
	return f.normalized, nil
}
func (f *fakeTakeoverRenderer) SocketPath() string { return f.socketPath }

// newTakeoverController 构造带 fakeAdapter（发现）+ fakeTakeoverRenderer（归一化）+ echoPrechecker（风险）的 controller。
func newTakeoverController(t *testing.T, compose runtimeAdapter, renderer takeoverPrechecker) (Controller, Repository, *Paths) {
	t.Helper()
	dir := t.TempDir()
	repo, err := OpenRepository(context.Background(), filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	paths := NewPaths(dir)
	ctrl := NewController(repo, paths, map[RuntimeKind]runtimeAdapter{RuntimeCompose: compose}, &fakeRunner{}, zap.NewNop(),
		WithPrechecker(&echoPrechecker{}), WithTakeoverPrechecker(renderer),
		WithClock(func() time.Time { return fixedNow() }))
	t.Cleanup(func() { _ = repo.Close() })
	return ctrl, repo, paths
}

const takeoverCompose = `services:
  web:
    image: nginx:1.27
    ports: ["${PORT:-8080}:80"]`

// realDiscovered 构造一个指向真实 temp 工作目录（含 compose.yaml）的 discovered 条目，
// 供 Takeover 端到端测试（takeover 会真实校验/读取 working_dir/config_files）。
func realDiscovered(t *testing.T, project string) Application {
	t.Helper()
	work := filepath.Join(t.TempDir(), project)
	require.NoError(t, os.MkdirAll(work, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(work, "compose.yaml"), []byte(takeoverCompose), 0o644))
	return Application{
		RuntimeProject:      project,
		ObservedWorkingDir:  work,
		ObservedConfigFiles: []string{filepath.Join(work, "compose.yaml")},
		Observed:            ObservedState{Phase: PhaseRunning, Services: []ServiceStatus{{Name: "web", State: "running"}}},
	}
}

// --- 路径安全 ---

func TestValidateTakeoverPaths(t *testing.T) {
	tmp := t.TempDir()
	dataDir := filepath.Join(tmp, "data")
	sockDir := filepath.Join(tmp, "sock")
	require.NoError(t, os.MkdirAll(dataDir, 0o755))
	require.NoError(t, os.MkdirAll(sockDir, 0o755))

	validWork := filepath.Join(tmp, "work")
	require.NoError(t, os.MkdirAll(validWork, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(validWork, "compose.yaml"), []byte("services:\n  web:\n    image: nginx\n"), 0o644))

	t.Run("valid", func(t *testing.T) {
		wd, sources, err := validateTakeoverPaths(validWork, []string{"compose.yaml"}, dataDir, sockDir)
		require.NoError(t, err)
		assert.Equal(t, validWork, wd)
		require.Len(t, sources, 1)
		assert.Equal(t, filepath.Join(validWork, "compose.yaml"), sources[0].Path)
		assert.Contains(t, sources[0].Body, "services:")
	})

	t.Run("root_rejected", func(t *testing.T) {
		_, _, err := validateTakeoverPaths("/", []string{"compose.yaml"}, dataDir, sockDir)
		require.Error(t, err)
	})
	t.Run("etc_rejected", func(t *testing.T) {
		_, _, err := validateTakeoverPaths("/etc", []string{"compose.yaml"}, dataDir, sockDir)
		require.Error(t, err)
	})
	t.Run("etc_subdir_rejected", func(t *testing.T) {
		// /etc 之下也必须拒绝（之前 isDirOrAncestor 漏判后代）。
		_, _, err := validateTakeoverPaths("/etc/devboxd", []string{"compose.yaml"}, dataDir, sockDir)
		require.Error(t, err)
	})
	t.Run("proc_subdir_rejected", func(t *testing.T) {
		_, _, err := validateTakeoverPaths("/proc/1", []string{"compose.yaml"}, dataDir, sockDir)
		require.Error(t, err)
	})
	t.Run("non_sensitive_allowed", func(t *testing.T) {
		// / 不得拒绝所有目录：普通 temp 目录应通过（已由 valid 覆盖，这里显式断言非敏感）。
		_, _, err := validateTakeoverPaths(validWork, []string{"compose.yaml"}, dataDir, sockDir)
		require.NoError(t, err)
	})
	t.Run("under_data_dir_rejected", func(t *testing.T) {
		under := filepath.Join(dataDir, "proj")
		require.NoError(t, os.MkdirAll(under, 0o755))
		_, _, err := validateTakeoverPaths(under, []string{"compose.yaml"}, dataDir, sockDir)
		require.Error(t, err)
	})
	t.Run("working_dir_symlink_rejected", func(t *testing.T) {
		link := filepath.Join(tmp, "worklink")
		require.NoError(t, os.Symlink(validWork, link))
		_, _, err := validateTakeoverPaths(link, []string{"compose.yaml"}, dataDir, sockDir)
		require.Error(t, err)
	})
	t.Run("parent_symlink_escape_rejected", func(t *testing.T) {
		// 父目录是 symlink → 逐级 Lstat 拒绝，防 canonical 逃逸。
		elsewhere := filepath.Join(tmp, "elsewhere")
		require.NoError(t, os.MkdirAll(filepath.Join(elsewhere, "inner"), 0o755))
		link := filepath.Join(tmp, "parentlink")
		require.NoError(t, os.Symlink(elsewhere, link))
		_, _, err := validateTakeoverPaths(filepath.Join(link, "inner"), []string{"compose.yaml"}, dataDir, sockDir)
		require.Error(t, err)
	})
	t.Run("config_symlink_rejected", func(t *testing.T) {
		target := filepath.Join(tmp, "real-compose.yaml")
		require.NoError(t, os.WriteFile(target, []byte("services:\n  web:\n    image: nginx\n"), 0o644))
		link := filepath.Join(validWork, "link.yaml")
		require.NoError(t, os.Symlink(target, link))
		_, _, err := validateTakeoverPaths(validWork, []string{"link.yaml"}, dataDir, sockDir)
		require.Error(t, err)
	})
	t.Run("config_traversal_rejected", func(t *testing.T) {
		_, _, err := validateTakeoverPaths(validWork, []string{"../../etc/passwd"}, dataDir, sockDir)
		require.Error(t, err)
	})
	t.Run("config_non_yaml_rejected", func(t *testing.T) {
		bad := filepath.Join(validWork, "notyaml")
		require.NoError(t, os.WriteFile(bad, []byte("::: not [[ yaml"), 0o644))
		_, _, err := validateTakeoverPaths(validWork, []string{"notyaml"}, dataDir, sockDir)
		require.Error(t, err)
	})
	t.Run("config_too_large_rejected", func(t *testing.T) {
		big := filepath.Join(validWork, "big.yaml")
		require.NoError(t, os.WriteFile(big, append([]byte("services:\n  web:\n    image: nginx\n"), make([]byte, maxTakeoverComposeFile+1)...), 0o644))
		_, _, err := validateTakeoverPaths(validWork, []string{"big.yaml"}, dataDir, sockDir)
		require.Error(t, err)
	})
}

// --- 插值门 ---

func TestUnsafeInterpolations(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want []string // nil/empty 表示无 unsafe
	}{
		{"bare brace", "image: nginx\nenv: ${PASSWORD}\n", []string{"PASSWORD"}},
		{"bare dollar", "image: nginx\nenv: $TOKEN\n", []string{"TOKEN"}},
		{"required modifier", "env: ${VAR:?need}\n", []string{"VAR"}},
		{"empty default", "env: ${VAR:-}\n", []string{"VAR"}},
		{"safe default allowed", "ports: [\"${PORT:-8080}:80\"]\n", nil},
		{"dash default allowed", "env: ${VAR-def}\n", nil},
		{"escaped dollar ignored", "cmd: echo $$HOME\n", nil},
		{"none", "image: nginx:1.27\n", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := unsafeInterpolations(c.yaml)
			if len(c.want) == 0 {
				assert.Empty(t, got)
			} else {
				assert.Equal(t, c.want, got)
			}
		})
	}
}

// --- Takeover 端到端（fake renderer + fake adapter）---

func TestTakeoverSuccessPersistsAtomic(t *testing.T) {
	project := "gitea"
	compose := &fakeAdapter{kind: RuntimeCompose, observed: map[string]Application{
		"x": realDiscovered(t, project),
	}}
	renderer := &fakeTakeoverRenderer{normalized: takeoverCompose}
	ctrl, repo, paths := newTakeoverController(t, compose, renderer)
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

	app, err := ctrl.Takeover(ctx, TakeoverRequest{ID: discoveredID}, ApplyOptions{Actor: "tester"})
	require.NoError(t, err)
	assert.Equal(t, OwnershipManaged, app.Ownership)
	assert.Equal(t, project, app.RuntimeProject)

	// meta 持久化 OriginalProject；ObservedRevision=0（接管未执行 runtime Apply）。
	meta, ok, err := repo.GetAppMeta(ctx, app.ID)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, project, meta.OriginalProject)
	assert.Equal(t, SourceLocal, meta.Source.Kind)
	assert.Equal(t, int64(1), meta.Revision)
	assert.Equal(t, int64(0), meta.ObservedRevision, "接管不得臆造 observed revision")

	// operation history 准确记录 TaskTypeTakeover（非 Apply），summary 含 confirmed。
	tasks, err := ctrl.ListOperations(ctx, app.ID)
	require.NoError(t, err)
	require.NotEmpty(t, tasks)
	assert.Equal(t, TaskTakeover, tasks[0].Type)
	assert.Equal(t, TaskSucceeded, tasks[0].Status)
	assert.Contains(t, tasks[0].RequestSummary, "confirmed=false")
	assert.Contains(t, tasks[0].RequestSummary, "project="+project)

	// 事实源已落盘（compose.yaml + revisions/1.yaml），marker 已清理。
	_, err = os.Stat(paths.ComposeFile(app.ID))
	require.NoError(t, err, "compose.yaml 必须存在")
	_, err = os.Stat(paths.RevisionFile(app.ID, 1))
	require.NoError(t, err, "revisions/1.yaml 必须存在")
	_, err = os.Stat(filepath.Join(paths.AppDir(app.ID), takeoverMarker))
	require.Error(t, err, "接管成功后 marker 应已清理")

	// 接管后写操作不再被拦。
	task, err := ctrl.Operate(ctx, app.ID, ActionStart, OperationOptions{})
	require.NoError(t, err)
	assert.NotEmpty(t, task.ID)

	// 审计 detail 不含宿主路径。
	var auditDetail string
	for _, a := range auditRecords(t, repo) {
		if a.AppID == app.ID && strings.HasPrefix(a.Action, "takeover") {
			auditDetail = a.Detail
		}
	}
	assert.NotEmpty(t, auditDetail)
	assert.NotContains(t, auditDetail, "/home/", "审计不得含宿主路径")
	assert.Contains(t, auditDetail, "configFiles=")
}

func TestTakeoverBlocksBareSecretVar(t *testing.T) {
	project := "secretapp"
	compose := &fakeAdapter{kind: RuntimeCompose, observed: map[string]Application{"x": realDiscovered(t, project)}}
	renderer := &fakeTakeoverRenderer{normalized: "services:\n  web:\n    image: nginx\n    environment:\n      PASSWORD: ${PASSWORD}\n"}
	ctrl, repo, _ := newTakeoverController(t, compose, renderer)
	ctx := context.Background()

	list, _ := ctrl.List(ctx, Filter{})
	var id string
	for _, a := range list {
		if a.Ownership == OwnershipDiscovered {
			id = a.ID
		}
	}
	_, err := ctrl.Takeover(ctx, TakeoverRequest{ID: id}, ApplyOptions{})
	require.Error(t, err)
	ae, ok := AsError(err)
	require.True(t, ok)
	assert.Contains(t, ae.Message, "PASSWORD")
	// 值不进错误（这里只有变量名）；无 meta 落库。
	metas, _ := repo.ListAppMetas(ctx)
	assert.Empty(t, metas)
}

func TestTakeoverCommitFailCleansDir(t *testing.T) {
	project := "failapp"
	compose := &fakeAdapter{kind: RuntimeCompose, observed: map[string]Application{"x": realDiscovered(t, project)}}
	renderer := &fakeTakeoverRenderer{normalized: takeoverCompose}
	tmp := t.TempDir()
	repo, err := OpenRepository(context.Background(), filepath.Join(tmp, "test.db"))
	require.NoError(t, err)
	defer repo.Close()
	paths := NewPaths(tmp)
	failing := &failingCommitRepo{Repository: repo, fail: true}
	ctrl := NewController(failing, paths, map[RuntimeKind]runtimeAdapter{RuntimeCompose: compose}, &fakeRunner{}, zap.NewNop(),
		WithPrechecker(&echoPrechecker{}), WithTakeoverPrechecker(renderer), WithClock(func() time.Time { return fixedNow() }))
	ctx := context.Background()

	list, _ := ctrl.List(ctx, Filter{})
	var id string
	for _, a := range list {
		if a.Ownership == OwnershipDiscovered {
			id = a.ID
		}
	}
	_, err = ctrl.Takeover(ctx, TakeoverRequest{ID: id}, ApplyOptions{})
	require.Error(t, err)
	// DB 失败：本次新建的 AppDir 必须被清理；无 meta。
	metas, _ := repo.ListAppMetas(ctx)
	assert.Empty(t, metas)
	appID := ExternalID(project)
	_, statErr := os.Stat(paths.AppDir(appID))
	assert.Error(t, statErr, "CommitApply 失败须清理本次新建目录")
}

func TestTakeoverReusesMatchingOrphanDir(t *testing.T) {
	project := "orphanapp"
	compose := &fakeAdapter{kind: RuntimeCompose, observed: map[string]Application{"x": realDiscovered(t, project)}}
	renderer := &fakeTakeoverRenderer{normalized: takeoverCompose}
	ctrl, repo, paths := newTakeoverController(t, compose, renderer)
	ctx := context.Background()

	appID := ExternalID(project)
	hash := composeHash(takeoverCompose, nil)
	// 预置前次崩溃留下的完整 orphan dir（marker 匹配 project+hash）。
	require.NoError(t, os.MkdirAll(filepath.Join(paths.AppDir(appID), "revisions"), 0o755))
	require.NoError(t, os.WriteFile(paths.ComposeFile(appID), []byte(takeoverCompose), 0o644))
	require.NoError(t, os.WriteFile(paths.RevisionFile(appID, 1), []byte(takeoverCompose), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(paths.AppDir(appID), takeoverMarker), []byte(formatTakeoverMarker(project, hash)), 0o644))

	list, _ := ctrl.List(ctx, Filter{})
	var id string
	for _, a := range list {
		if a.Ownership == OwnershipDiscovered {
			id = a.ID
		}
	}
	require.Equal(t, appID, id)
	app, err := ctrl.Takeover(ctx, TakeoverRequest{ID: id}, ApplyOptions{})
	require.NoError(t, err, "匹配的 orphan dir 应被复用而非冲突")
	assert.Equal(t, OwnershipManaged, app.Ownership)
	meta, ok, _ := repo.GetAppMeta(ctx, appID)
	require.True(t, ok)
	assert.Equal(t, project, meta.OriginalProject)
}

func TestTakeoverConflictsOnMismatchedOrphanDir(t *testing.T) {
	project := "mismatchapp"
	compose := &fakeAdapter{kind: RuntimeCompose, observed: map[string]Application{"x": realDiscovered(t, project)}}
	renderer := &fakeTakeoverRenderer{normalized: takeoverCompose}
	ctrl, _, paths := newTakeoverController(t, compose, renderer)
	ctx := context.Background()

	appID := ExternalID(project)
	// orphan marker 的 hash 与本次不一致 → 冲突，不盲删。
	require.NoError(t, os.MkdirAll(paths.AppDir(appID), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(paths.AppDir(appID), takeoverMarker), []byte(formatTakeoverMarker(project, "stalehash")), 0o644))

	list, _ := ctrl.List(ctx, Filter{})
	var id string
	for _, a := range list {
		if a.Ownership == OwnershipDiscovered {
			id = a.ID
		}
	}
	_, err := ctrl.Takeover(ctx, TakeoverRequest{ID: id}, ApplyOptions{})
	require.Error(t, err)
	ae, ok := AsError(err)
	require.True(t, ok)
	assert.Equal(t, "takeover_dir_conflict", ae.Reason)
	// 目录仍在（未盲删）。
	_, statErr := os.Stat(paths.AppDir(appID))
	assert.NoError(t, statErr)
}

// failingCommitRepo 包装 Repository，按需让 CommitApply 失败。
type failingCommitRepo struct {
	Repository
	fail bool
}

func (f *failingCommitRepo) CommitApply(ctx context.Context, meta AppRecord, rev Revision, task Task, idemKey, requestHash string) error {
	if f.fail {
		return errors.New("simulated commit failure")
	}
	return f.Repository.CommitApply(ctx, meta, rev, task, idemKey, requestHash)
}

// auditRecords 读取全部审计（仅测试用）。
func auditRecords(t *testing.T, repo Repository) []AuditRecord {
	t.Helper()
	// Repository 接口未暴露 ListAudit；通过 apps.db 直接查。
	sqlite := repo.(*sqliteRepo)
	rows, err := sqlite.db.QueryContext(context.Background(), `SELECT actor,app_id,action,task_id,detail FROM audit ORDER BY id`)
	require.NoError(t, err)
	defer rows.Close()
	var out []AuditRecord
	for rows.Next() {
		var r AuditRecord
		require.NoError(t, rows.Scan(&r.Actor, &r.AppID, &r.Action, &r.TaskID, &r.Detail))
		out = append(out, r)
	}
	return out
}

func TestTakeoverUnavailableOnLabelConflict(t *testing.T) {
	project := "splitapp"
	// 同 project 容器标签不一致 → aggregateApp 标 conflict（这里直接置标志模拟）。
	compose := &fakeAdapter{kind: RuntimeCompose, observed: map[string]Application{
		"x": {RuntimeProject: project, ObservedDiscoveredConflict: true,
			ObservedWorkingDir: "/tmp/x", ObservedConfigFiles: []string{"/tmp/x/compose.yaml"},
			Observed: ObservedState{Phase: PhaseRunning, Services: []ServiceStatus{{Name: "web", State: "running"}}}},
	}}
	renderer := &fakeTakeoverRenderer{normalized: takeoverCompose}
	ctrl, repo, _ := newTakeoverController(t, compose, renderer)
	ctx := context.Background()

	list, _ := ctrl.List(ctx, Filter{})
	var id string
	for _, a := range list {
		if a.Ownership == OwnershipDiscovered {
			require.False(t, a.Discovered.TakeoverAvailable, "标签冲突不得可接管")
			assert.Contains(t, a.Discovered.Reason, "不一致")
			id = a.ID
		}
	}
	require.NotEmpty(t, id)
	_, err := ctrl.Takeover(ctx, TakeoverRequest{ID: id}, ApplyOptions{})
	require.Error(t, err)
	metas, _ := repo.ListAppMetas(ctx)
	assert.Empty(t, metas, "不可接管的项目不得落库")
}

func TestTakeoverUnavailableOnInvalidProjectName(t *testing.T) {
	// 非法 project name（含换行/大写/空格）→ 列表可展示但 takeoverAvailable=false。
	project := "Bad\nName"
	compose := &fakeAdapter{kind: RuntimeCompose, observed: map[string]Application{
		"x": {RuntimeProject: project, ObservedWorkingDir: "/tmp/x", ObservedConfigFiles: []string{"/tmp/x/compose.yaml"},
			Observed: ObservedState{Phase: PhaseRunning, Services: []ServiceStatus{{Name: "web", State: "running"}}}},
	}}
	renderer := &fakeTakeoverRenderer{normalized: takeoverCompose}
	ctrl, repo, _ := newTakeoverController(t, compose, renderer)
	ctx := context.Background()

	list, _ := ctrl.List(ctx, Filter{})
	var id string
	for _, a := range list {
		if a.Ownership == OwnershipDiscovered {
			require.False(t, a.Discovered.TakeoverAvailable, "非法 project name 不得可接管")
			id = a.ID
		}
	}
	require.NotEmpty(t, id)
	_, err := ctrl.Takeover(ctx, TakeoverRequest{ID: id}, ApplyOptions{})
	require.Error(t, err)
	metas, _ := repo.ListAppMetas(ctx)
	assert.Empty(t, metas)
}

// realDiscoveredContent 构造指向真实 temp 工作目录、内容自定的 discovered 条目。
func realDiscoveredContent(t *testing.T, project, content string) Application {
	t.Helper()
	work := filepath.Join(t.TempDir(), project)
	require.NoError(t, os.MkdirAll(work, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(work, "compose.yaml"), []byte(content), 0o644))
	return Application{
		RuntimeProject:      project,
		ObservedWorkingDir:  work,
		ObservedConfigFiles: []string{filepath.Join(work, "compose.yaml")},
		Observed:            ObservedState{Phase: PhaseRunning, Services: []ServiceStatus{{Name: "web", State: "running"}}},
	}
}

// TestTakeoverBlocksFileAccessBeforeCLI 文件访问指令必须在任何 compose CLI 调用前阻断
// （否则 CLI 会读取额外宿主文件绕过顶层路径校验）。断言 fake renderer 未被调用。
func TestTakeoverBlocksFileAccessBeforeCLI(t *testing.T) {
	cases := []struct {
		name    string
		compose string
		field   string
	}{
		{"include", "services:\n  web:\n    image: nginx\ninclude:\n  - ../../secret.yml\n", "include"},
		{"extends_file", "services:\n  web:\n    image: nginx\n    extends:\n      file: ../base.yml\n      service: base\n", "extends.file"},
		{"env_file", "services:\n  web:\n    image: nginx\n    env_file: [./foo.env]\n", "env_file"},
		{"secrets_file", "services:\n  web:\n    image: nginx\nsecrets:\n  db:\n    file: ./db.txt\n", "secrets"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			project := "proj-" + c.name
			compose := &fakeAdapter{kind: RuntimeCompose, observed: map[string]Application{
				"x": realDiscoveredContent(t, project, c.compose),
			}}
			renderer := &fakeTakeoverRenderer{normalized: takeoverCompose}
			ctrl, repo, _ := newTakeoverController(t, compose, renderer)
			ctx := context.Background()

			list, _ := ctrl.List(ctx, Filter{})
			var id string
			for _, a := range list {
				if a.Ownership == OwnershipDiscovered {
					id = a.ID
				}
			}
			require.NotEmpty(t, id)

			_, err := ctrl.Takeover(ctx, TakeoverRequest{ID: id}, ApplyOptions{})
			require.Error(t, err)
			ae, ok := AsError(err)
			require.True(t, ok)
			assert.Equal(t, ErrKindRiskBlocked, ae.Kind, "文件访问指令应为 risk_blocked")
			assert.Equal(t, 0, renderer.callCount, "须在任何 compose CLI 调用前阻断")
			metas, _ := repo.ListAppMetas(ctx)
			assert.Empty(t, metas)
		})
	}
}

func TestTakeoverIdempotencySameKeyDiffTarget409(t *testing.T) {
	p1, p2 := "appa", "appb"
	compose := &fakeAdapter{kind: RuntimeCompose, observed: map[string]Application{
		"x": realDiscovered(t, p1), "y": realDiscovered(t, p2),
	}}
	renderer := &fakeTakeoverRenderer{normalized: takeoverCompose}
	ctrl, repo, _ := newTakeoverController(t, compose, renderer)
	ctx := context.Background()

	list, _ := ctrl.List(ctx, Filter{})
	var id1, id2 string
	for _, a := range list {
		if a.Ownership != OwnershipDiscovered {
			continue
		}
		if a.Discovered.Project == p1 {
			id1 = a.ID
		}
		if a.Discovered.Project == p2 {
			id2 = a.ID
		}
	}
	require.NotEmpty(t, id1)
	require.NotEmpty(t, id2)

	_, err := ctrl.Takeover(ctx, TakeoverRequest{ID: id1}, ApplyOptions{IdempotencyKey: "K1"})
	require.NoError(t, err)
	// 同 key 异 target → 409 idempotency_conflict。
	_, err = ctrl.Takeover(ctx, TakeoverRequest{ID: id2}, ApplyOptions{IdempotencyKey: "K1"})
	require.Error(t, err)
	ae, ok := AsError(err)
	require.True(t, ok)
	assert.Equal(t, ErrKindConflict, ae.Kind)
	// 同 key 同 target 幂等返回（meta 已存在，顶部即返回）。
	_, err = ctrl.Takeover(ctx, TakeoverRequest{ID: id1}, ApplyOptions{IdempotencyKey: "K1"})
	require.NoError(t, err)
	metas, _ := repo.ListAppMetas(ctx)
	assert.Len(t, metas, 1, "p2 须被 409 拦截不落库")
}

// TestTakeoverIDConsistentListGetTakeover 多 discovered 候选冲突下，List/Get/Takeover
// 使用同一 deterministic discovery index：主候选被受管占用时三处都回退到同一第二候选。
func TestTakeoverIDConsistentListGetTakeover(t *testing.T) {
	project := "consistencyapp"
	compose := &fakeAdapter{kind: RuntimeCompose, observed: map[string]Application{
		"x": realDiscovered(t, project),
	}}
	renderer := &fakeTakeoverRenderer{normalized: takeoverCompose}
	ctrl, repo, _ := newTakeoverController(t, compose, renderer)
	ctx := context.Background()

	// 受管 meta 占用 project 的主候选 ID → 迫使 discovered 用第二候选。
	primary := ExternalID(project)
	now := fixedNow()
	require.NoError(t, repo.UpsertAppMeta(ctx, AppRecord{
		ID: primary, Name: "legacy", Runtime: RuntimeCompose,
		Revision: 1, ObservedRevision: 1, CreatedAt: now, UpdatedAt: now,
	}))

	// List 给出的 discovered ID。
	list, _ := ctrl.List(ctx, Filter{})
	var listID string
	for _, a := range list {
		if a.Ownership == OwnershipDiscovered && a.Discovered.Project == project {
			listID = a.ID
		}
	}
	require.NotEmpty(t, listID)
	require.NotEqual(t, primary, listID, "主候选被占用应回退第二候选")
	assert.Equal(t, DiscoveredAltID(project), listID)

	// Get 同一 ID 命中同一 project。
	got, err := ctrl.Get(ctx, listID)
	require.NoError(t, err)
	assert.Equal(t, OwnershipDiscovered, got.Ownership)
	assert.Equal(t, listID, got.ID)

	// Takeover 用该 ID，落库 ID == 该 ID（与 List/Get 一致，不漂移到主候选/其它）。
	app, err := ctrl.Takeover(ctx, TakeoverRequest{ID: listID}, ApplyOptions{})
	require.NoError(t, err)
	assert.Equal(t, listID, app.ID, "Takeover 落库 ID 必须与 List/Get 一致")
	meta, ok, _ := repo.GetAppMeta(ctx, listID)
	require.True(t, ok)
	assert.Equal(t, project, meta.OriginalProject)
}

// realDiscoveredMulti 构造多 config_files 的 discovered 条目（按给定内容创建真实文件）。
func realDiscoveredMulti(t *testing.T, project string, bodies map[string]string, order []string) Application {
	t.Helper()
	work := filepath.Join(t.TempDir(), project)
	require.NoError(t, os.MkdirAll(work, 0o755))
	var files []string
	for _, name := range order {
		p := filepath.Join(work, name)
		require.NoError(t, os.WriteFile(p, []byte(bodies[name]), 0o644))
		files = append(files, p)
	}
	return Application{
		RuntimeProject:      project,
		ObservedWorkingDir:  work,
		ObservedConfigFiles: files,
		Observed:            ObservedState{Phase: PhaseRunning, Services: []ServiceStatus{{Name: "web", State: "running"}}},
	}
}

// TestTakeoverRendererGetsTempCopiesNotOriginalPaths CLI 只得到 devbox 控制临时副本（非原始
// config path），消除 TOCTOU；且得到的是验证时刻的 body 快照（验证后替换原文件不影响）。
func TestTakeoverRendererGetsTempCopiesNotOriginalPaths(t *testing.T) {
	project := "toctouapp"
	body := "services:\n  web:\n    image: nginx:1.27\n    # MARKER-V1\n"
	compose := &fakeAdapter{kind: RuntimeCompose, observed: map[string]Application{
		"x": realDiscoveredContent(t, project, body),
	}}
	renderer := &fakeTakeoverRenderer{normalized: takeoverCompose}
	ctrl, _, _ := newTakeoverController(t, compose, renderer)
	ctx := context.Background()

	list, _ := ctrl.List(ctx, Filter{})
	var id string
	for _, a := range list {
		if a.Ownership == OwnershipDiscovered {
			id = a.ID
		}
	}
	require.NotEmpty(t, id)

	_, err := ctrl.Takeover(ctx, TakeoverRequest{ID: id}, ApplyOptions{})
	require.NoError(t, err)
	require.Equal(t, 1, renderer.callCount)
	require.NotEmpty(t, renderer.lastFiles)
	require.NotEmpty(t, renderer.lastEnvFile, "必须传入受控 env-file 阻止自动读 working_dir/.env")
	// 副本不在原始 working_dir 下（而是 devbox 控制临时目录）。
	for _, p := range renderer.lastFiles {
		assert.False(t, strings.Contains(p, "/toctouapp/"), "renderer 不得收到原始 config path: %s", p)
	}
	// 得到的是验证时刻的 body（MARKER-V1）。
	require.Len(t, renderer.lastBodies, 1)
	assert.Contains(t, renderer.lastBodies[0], "MARKER-V1")

	// 验证后替换原文件（TOCTOU 尝试）→ renderer 当时得到的快照不受影响。
	work := filepath.Dir(renderer.lastFiles[0]) // 这是 temp dir，不是源；用源路径替换：
	// 取 discovered 条目的真实源路径（list 已含 Discovered.ConfigFiles）。
	var srcPath string
	for _, a := range list {
		if a.Ownership == OwnershipDiscovered && len(a.Discovered.ConfigFiles) > 0 {
			srcPath = a.Discovered.ConfigFiles[0]
		}
	}
	require.NotEmpty(t, srcPath)
	require.NoError(t, os.WriteFile(srcPath, []byte("services:\n  web:\n    image: nginx\n    # MARKER-V2\n"), 0o644))
	assert.NotContains(t, renderer.lastBodies[0], "MARKER-V2", "快照不得被验证后的替换影响")
	_ = work
}

// TestTakeoverMultiFileOverrideAcceptsNoServicesFile 多文件中允许 override 文件只含
// networks/volumes（无 services），只要集合至少一个文件有非空 services。
func TestTakeoverMultiFileOverrideAcceptsNoServicesFile(t *testing.T) {
	project := "multifileapp"
	compose := &fakeAdapter{kind: RuntimeCompose, observed: map[string]Application{
		"x": realDiscoveredMulti(t, project,
			map[string]string{
				"compose.yaml":  "services:\n  web:\n    image: nginx:1.27\n",
				"override.yaml": "networks:\n  extra:\n    driver: bridge\n",
			},
			[]string{"compose.yaml", "override.yaml"}),
	}}
	renderer := &fakeTakeoverRenderer{normalized: takeoverCompose}
	ctrl, repo, _ := newTakeoverController(t, compose, renderer)
	ctx := context.Background()

	list, _ := ctrl.List(ctx, Filter{})
	var id string
	for _, a := range list {
		if a.Ownership == OwnershipDiscovered {
			id = a.ID
		}
	}
	require.NotEmpty(t, id)
	app, err := ctrl.Takeover(ctx, TakeoverRequest{ID: id}, ApplyOptions{})
	require.NoError(t, err, "override（无 services）应被集合 services 检查接受")
	assert.Equal(t, OwnershipManaged, app.Ownership)
	// renderer 收到 2 个临时副本（按顺序）。
	assert.Len(t, renderer.lastFiles, 2)
	metas, _ := repo.ListAppMetas(ctx)
	assert.Len(t, metas, 1)
}

// TestTakeoverRejectsCollectionWithoutServices 集合无任何非空 services → 拒绝。
func TestTakeoverRejectsCollectionWithoutServices(t *testing.T) {
	project := "noservicesapp"
	compose := &fakeAdapter{kind: RuntimeCompose, observed: map[string]Application{
		"x": realDiscoveredMulti(t, project,
			map[string]string{
				"compose.yaml": "networks:\n  only:\n    driver: bridge\n",
			},
			[]string{"compose.yaml"}),
	}}
	renderer := &fakeTakeoverRenderer{normalized: takeoverCompose}
	ctrl, repo, _ := newTakeoverController(t, compose, renderer)
	ctx := context.Background()

	list, _ := ctrl.List(ctx, Filter{})
	var id string
	for _, a := range list {
		if a.Ownership == OwnershipDiscovered {
			id = a.ID
		}
	}
	require.NotEmpty(t, id)
	_, err := ctrl.Takeover(ctx, TakeoverRequest{ID: id}, ApplyOptions{})
	require.Error(t, err)
	assert.Equal(t, 0, renderer.callCount, "缺少 services 应在 CLI 调用前拒绝")
	metas, _ := repo.ListAppMetas(ctx)
	assert.Empty(t, metas)
}

func TestTakeoverIdempotencyConfirmAndManagedBypass(t *testing.T) {
	p1, p2 := "idemappa", "idemappb"
	compose := &fakeAdapter{kind: RuntimeCompose, observed: map[string]Application{
		"x": realDiscovered(t, p1), "y": realDiscovered(t, p2),
	}}
	renderer := &fakeTakeoverRenderer{normalized: takeoverCompose}
	ctrl, repo, _ := newTakeoverController(t, compose, renderer)
	ctx := context.Background()

	list, _ := ctrl.List(ctx, Filter{})
	var id1, id2 string
	for _, a := range list {
		if a.Discovered != nil && a.Discovered.Project == p1 {
			id1 = a.ID
		}
		if a.Discovered != nil && a.Discovered.Project == p2 {
			id2 = a.ID
		}
	}
	require.NotEmpty(t, id1)
	require.NotEmpty(t, id2)

	// 1) 同 key 同 (target,confirm) → 返回同 app。
	app1, err := ctrl.Takeover(ctx, TakeoverRequest{ID: id1}, ApplyOptions{IdempotencyKey: "K"})
	require.NoError(t, err)
	app1b, err := ctrl.Takeover(ctx, TakeoverRequest{ID: id1}, ApplyOptions{IdempotencyKey: "K"})
	require.NoError(t, err)
	assert.Equal(t, app1.ID, app1b.ID)

	// 2) 同 key 不同 target → 409。
	_, err = ctrl.Takeover(ctx, TakeoverRequest{ID: id2}, ApplyOptions{IdempotencyKey: "K"})
	require.Error(t, err)
	ae, _ := AsError(err)
	assert.Equal(t, ErrKindConflict, ae.Kind)

	// 3) 同 key 同 target(id1 已受管) 但 confirm 不同 → 409（不得因「已受管」早返回绕过 key 冲突）。
	_, err = ctrl.Takeover(ctx, TakeoverRequest{ID: id1}, ApplyOptions{IdempotencyKey: "K", AllowRiskyConfirmation: true})
	require.Error(t, err)
	ae2, _ := AsError(err)
	assert.Equal(t, ErrKindConflict, ae2.Kind)

	// 仅 p1 落库。
	metas, _ := repo.ListAppMetas(ctx)
	assert.Len(t, metas, 1)
}

// TestTakeoverLiteralSecretRejectedBeforeStaging 原始 body 含明文 secret → 在任何临时落盘前拒绝：
// renderer 未被调用，dataDir 无 takeover-render-* 残留，错误/未落库。
func TestTakeoverLiteralSecretRejectedBeforeStaging(t *testing.T) {
	project := "leakyapp"
	// 明文 PASSWORD 值（AnalyzeLiteralSecrets 应阻断）。
	body := "services:\n  web:\n    image: nginx\n    environment:\n      DB_PASSWORD: supersecret-value-1234\n"
	compose := &fakeAdapter{kind: RuntimeCompose, observed: map[string]Application{
		"x": realDiscoveredContent(t, project, body),
	}}
	renderer := &fakeTakeoverRenderer{normalized: takeoverCompose}
	ctrl, repo, paths := newTakeoverController(t, compose, renderer)
	ctx := context.Background()

	list, _ := ctrl.List(ctx, Filter{})
	var id string
	for _, a := range list {
		if a.Ownership == OwnershipDiscovered {
			id = a.ID
		}
	}
	require.NotEmpty(t, id)

	_, err := ctrl.Takeover(ctx, TakeoverRequest{ID: id}, ApplyOptions{})
	require.Error(t, err)
	ae, ok := AsError(err)
	require.True(t, ok)
	assert.Equal(t, ErrKindRiskBlocked, ae.Kind)
	assert.Equal(t, 0, renderer.callCount, "明文 secret 须在任何 CLI/落盘前拒绝")
	metas, _ := repo.ListAppMetas(ctx)
	assert.Empty(t, metas)

	// dataDir 下不得残留 takeover-render-* 临时目录。
	entries, err := os.ReadDir(paths.DataDir)
	require.NoError(t, err)
	for _, e := range entries {
		assert.False(t, strings.HasPrefix(e.Name(), "takeover-render-"),
			"明文 secret 拒绝不得留下临时副本残留: %s", e.Name())
	}
}

// TestCleanupStaleTakeoverRenderOnlyRemovesAgedDedicatedDirs 仅清理专用 takeover-render-* 且超龄目录；
// 不删新近目录、不删其它路径。
func TestCleanupStaleTakeoverRenderOnlyRemovesAgedDedicatedDirs(t *testing.T) {
	dir := t.TempDir()
	// 陈旧的专用目录（超龄）→ 应被清理。
	old := filepath.Join(dir, "takeover-render-old")
	require.NoError(t, os.MkdirAll(filepath.Join(old, "revisions"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(old, "compose.yaml"), []byte("x"), 0o600))
	past := time.Now().Add(-2 * time.Hour)
	require.NoError(t, os.Chtimes(old, past, past))

	// 新近的专用目录（进行中模拟）→ 不删。
	fresh := filepath.Join(dir, "takeover-render-fresh")
	require.NoError(t, os.MkdirAll(fresh, 0o700))

	// 无关目录/文件 → 不删。
	other := filepath.Join(dir, "apps")
	require.NoError(t, os.MkdirAll(other, 0o755))

	cleanupStaleTakeoverRender(dir, 1*time.Hour)

	_, err := os.Stat(old)
	assert.Error(t, err, "陈旧 takeover-render 应被清理")
	_, err = os.Stat(fresh)
	assert.NoError(t, err, "新近 takeover-render 不应被删")
	_, err = os.Stat(other)
	assert.NoError(t, err, "无关目录不应被删")
}

// TestTakeoverLiteralSecretInMultiFileBlocked 多文件中主文件含明文 secret（override 无 services）
// → lenient 预检不因 override 报错，但对主文件 services 的明文 secret 阻断；renderer=0。
func TestTakeoverLiteralSecretInMultiFileBlocked(t *testing.T) {
	project := "multileak"
	compose := &fakeAdapter{kind: RuntimeCompose, observed: map[string]Application{
		"x": realDiscoveredMulti(t, project,
			map[string]string{
				"compose.yaml":  "services:\n  web:\n    image: nginx\n    environment:\n      DB_PASSWORD: hunter2-leak\n",
				"override.yaml": "networks:\n  extra:\n    driver: bridge\n",
			},
			[]string{"compose.yaml", "override.yaml"}),
	}}
	renderer := &fakeTakeoverRenderer{normalized: takeoverCompose}
	ctrl, repo, _ := newTakeoverController(t, compose, renderer)
	ctx := context.Background()
	list, _ := ctrl.List(ctx, Filter{})
	var id string
	for _, a := range list {
		if a.Ownership == OwnershipDiscovered {
			id = a.ID
		}
	}
	require.NotEmpty(t, id)
	_, err := ctrl.Takeover(ctx, TakeoverRequest{ID: id}, ApplyOptions{})
	require.Error(t, err)
	ae, _ := AsError(err)
	assert.Equal(t, ErrKindRiskBlocked, ae.Kind)
	assert.Equal(t, 0, renderer.callCount)
	metas, _ := repo.ListAppMetas(ctx)
	assert.Empty(t, metas)
}
