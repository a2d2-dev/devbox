package apps

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestRepo(t *testing.T) (Repository, func()) {
	t.Helper()
	dir := t.TempDir()
	repo, err := OpenRepository(context.Background(), filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	return repo, func() { _ = repo.Close() }
}

func TestRepoAppMetaCRUD(t *testing.T) {
	repo, cleanup := newTestRepo(t)
	defer cleanup()
	ctx := context.Background()
	now := time.Now()

	_, ok, err := repo.GetAppMeta(ctx, "x")
	require.NoError(t, err)
	assert.False(t, ok)

	a := AppRecord{
		ID: "my-app", Name: "My App", Runtime: RuntimeCompose,
		Source: ApplicationSource{Kind: SourceCatalog, StoreID: "package-id", CatalogID: "catalog-id", Version: "1.0"}, Revision: 3,
		Parameters: map[string]string{"P": "1"}, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, repo.UpsertAppMeta(ctx, a))

	got, ok, err := repo.GetAppMeta(ctx, "my-app")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "My App", got.Name)
	assert.Equal(t, RuntimeCompose, got.Runtime)
	assert.Equal(t, a.Source, got.Source)
	assert.Equal(t, int64(3), got.Revision)
	assert.Equal(t, "1", got.Parameters["P"])

	// 更新。
	a.Revision = 4
	a.ObservedRevision = 3
	require.NoError(t, repo.UpsertAppMeta(ctx, a))
	require.NoError(t, repo.SetObservedRevision(ctx, "my-app", 4))
	got, _, _ = repo.GetAppMeta(ctx, "my-app")
	assert.Equal(t, int64(4), got.Revision)
	assert.Equal(t, int64(4), got.ObservedRevision)

	list, err := repo.ListAppMetas(ctx)
	require.NoError(t, err)
	assert.Len(t, list, 1)

	require.NoError(t, repo.PurgeApp(ctx, "my-app"))
	_, ok, _ = repo.GetAppMeta(ctx, "my-app")
	assert.False(t, ok)
}

func TestRepoRevisions(t *testing.T) {
	repo, cleanup := newTestRepo(t)
	defer cleanup()
	ctx := context.Background()

	n1, err := repo.NextRevisionNumber(ctx, "a")
	require.NoError(t, err)
	assert.Equal(t, int64(1), n1)
	source := ApplicationSource{Kind: SourceCatalog, StoreID: "package-id", CatalogID: "catalog-id", Version: "1.2.3"}
	require.NoError(t, repo.InsertRevision(ctx, Revision{Number: 1, AppID: "a", ComposeHash: "h1", Source: source, CreatedAt: time.Now()}))
	n2, _ := repo.NextRevisionNumber(ctx, "a")
	assert.Equal(t, int64(2), n2)
	require.NoError(t, repo.InsertRevision(ctx, Revision{Number: 2, AppID: "a", ComposeHash: "h2", CreatedAt: time.Now()}))

	revs, err := repo.ListRevisions(ctx, "a")
	require.NoError(t, err)
	assert.Len(t, revs, 2)
	assert.Equal(t, int64(2), revs[0].Number) // DESC

	got, ok, err := repo.GetRevision(ctx, "a", 1)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "h1", got.ComposeHash)
	assert.Equal(t, source, got.Source)

	_, ok, _ = repo.GetRevision(ctx, "a", 99)
	assert.False(t, ok)
}

func TestRepoTaskLifecycle(t *testing.T) {
	repo, cleanup := newTestRepo(t)
	defer cleanup()
	ctx := context.Background()

	task := Task{
		ID: "t1", AppID: "a", Type: TaskApply, Status: TaskQueued,
		Revision: 1, Purge: false, CreatedAt: time.Now(),
	}
	require.NoError(t, repo.CreateTask(ctx, task))

	got, err := repo.GetTask(ctx, "t1")
	require.NoError(t, err)
	assert.Equal(t, TaskQueued, got.Status)
	assert.False(t, got.Purge == true && got.Purge != false)

	// UpdateTask mut。
	now := time.Now()
	require.NoError(t, repo.UpdateTask(ctx, "t1", func(t *Task) {
		t.Status = TaskRunning
		t.Phase = PhaseTaskApplying
		t.StartedAt = &now
	}))
	got, _ = repo.GetTask(ctx, "t1")
	assert.Equal(t, TaskRunning, got.Status)
	assert.Equal(t, PhaseTaskApplying, got.Phase)

	// 非终态查询（崩溃恢复用）。
	require.NoError(t, repo.CreateTask(ctx, Task{ID: "t2", AppID: "a", Type: TaskOperate, Status: TaskQueued, Action: ActionStop, CreatedAt: time.Now()}))
	require.NoError(t, repo.CreateTask(ctx, Task{ID: "t3", AppID: "a", Type: TaskRemove, Status: TaskSucceeded, CreatedAt: time.Now()}))
	nonTerm, err := repo.ListNonTerminalTasks(ctx)
	require.NoError(t, err)
	assert.Len(t, nonTerm, 2) // t1(running) + t2(queued)，t3 终态排除

	byApp, err := repo.ListTasksByApp(ctx, "a", 10)
	require.NoError(t, err)
	assert.Len(t, byApp, 3)
}

func TestRepoIdempotency(t *testing.T) {
	repo, cleanup := newTestRepo(t)
	defer cleanup()
	ctx := context.Background()

	_, ok, err := repo.GetIdempotency(ctx, "k1")
	require.NoError(t, err)
	assert.False(t, ok)

	require.NoError(t, repo.SaveIdempotency(ctx, "k1", "hash-abc", "task-1"))
	rec, ok, err := repo.GetIdempotency(ctx, "k1")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "hash-abc", rec.RequestHash)
	assert.Equal(t, "task-1", rec.TaskID)
}

func TestRepoMigrateIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "m.db")
	r1, err := OpenRepository(context.Background(), path)
	require.NoError(t, err)
	_ = r1.Close()
	// 二次打开同库应成功（migration IF NOT EXISTS 幂等）。
	r2, err := OpenRepository(context.Background(), path)
	require.NoError(t, err)
	_ = r2.Close()
}

func TestRepoMigratePreservesCatalogSourceAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "legacy.db")
	legacy, err := sql.Open("sqlite", "file:"+path)
	require.NoError(t, err)
	_, err = legacy.Exec(`CREATE TABLE apps (
		id TEXT PRIMARY KEY, name TEXT NOT NULL, runtime TEXT NOT NULL,
		source_kind TEXT NOT NULL DEFAULT 'inline', source_store_id TEXT NOT NULL DEFAULT '',
		source_version TEXT NOT NULL DEFAULT '', revision INTEGER NOT NULL DEFAULT 0,
		observed_revision INTEGER NOT NULL DEFAULT 0, parameters TEXT NOT NULL DEFAULT '{}',
		created_at TEXT NOT NULL, updated_at TEXT NOT NULL
	); CREATE TABLE revisions (
		app_id TEXT NOT NULL, number INTEGER NOT NULL, compose_hash TEXT NOT NULL,
		source_kind TEXT NOT NULL DEFAULT 'inline', source_version TEXT NOT NULL DEFAULT '',
		parameters TEXT NOT NULL DEFAULT '{}', created_at TEXT NOT NULL,
		created_by TEXT NOT NULL DEFAULT '', note TEXT NOT NULL DEFAULT '',
		PRIMARY KEY (app_id, number)
	)`)
	require.NoError(t, err)
	require.NoError(t, legacy.Close())

	ctx := context.Background()
	repo, err := OpenRepository(ctx, path)
	require.NoError(t, err)
	now := time.Now()
	source := ApplicationSource{Kind: SourceCatalog, StoreID: "package-id", CatalogID: "catalog-id", Version: "2.0.0"}
	require.NoError(t, repo.UpsertAppMeta(ctx, AppRecord{
		ID: "catalog-app", Name: "Catalog App", Runtime: RuntimeCompose, Source: source,
		Revision: 1, CreatedAt: now, UpdatedAt: now,
	}))
	require.NoError(t, repo.InsertRevision(ctx, Revision{
		AppID: "catalog-app", Number: 1, ComposeHash: "hash", Source: source, CreatedAt: now,
	}))
	require.NoError(t, repo.Close())

	reopened, err := OpenRepository(ctx, path)
	require.NoError(t, err)
	defer reopened.Close()
	meta, ok, err := reopened.GetAppMeta(ctx, "catalog-app")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, source, meta.Source)
	revision, ok, err := reopened.GetRevision(ctx, "catalog-app", 1)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, source, revision.Source)
}

// MED#5：CommitApply 任一步失败必须整体回滚，不留半状态。
func TestCommitApplyAtomicRollback(t *testing.T) {
	repo, cleanup := newTestRepo(t)
	defer cleanup()
	ctx := context.Background()
	now := time.Now()
	// 预置一个已存在的 task id，迫使 CommitApply 中途 createTask 撞主键。
	require.NoError(t, repo.CreateTask(ctx, Task{ID: "dup", AppID: "a", Type: TaskApply, Status: TaskQueued, CreatedAt: now}))

	meta := AppRecord{ID: "a", Name: "a", Runtime: RuntimeCompose, Revision: 1, CreatedAt: now, UpdatedAt: now}
	rev := Revision{Number: 1, AppID: "a", ComposeHash: "h", CreatedAt: now}
	task := Task{ID: "dup", AppID: "a", Type: TaskApply, Status: TaskQueued, Revision: 1, CreatedAt: now}

	err := repo.CommitApply(ctx, meta, rev, task, "", "")
	assert.Error(t, err, "重复 task id 应使事务失败")

	// 回滚：revision 与 meta 都不应残留。
	_, okRev, err := repo.GetRevision(ctx, "a", 1)
	require.NoError(t, err)
	assert.False(t, okRev, "事务回滚后 revision 不应存在")
	_, okMeta, err := repo.GetAppMeta(ctx, "a")
	require.NoError(t, err)
	assert.False(t, okMeta, "事务回滚后 meta 不应存在")
}

// MED#5：CommitTask 原子写入 task + idempotency。
func TestCommitTaskAtomic(t *testing.T) {
	repo, cleanup := newTestRepo(t)
	defer cleanup()
	ctx := context.Background()
	now := time.Now()
	task := Task{ID: "t1", AppID: "a", Type: TaskOperate, Status: TaskQueued, CreatedAt: now}
	require.NoError(t, repo.CommitTask(ctx, task, "k1", "hash1"))
	got, err := repo.GetTask(ctx, "t1")
	require.NoError(t, err)
	assert.Equal(t, TaskOperate, got.Type)
	rec, ok, err := repo.GetIdempotency(ctx, "k1")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "t1", rec.TaskID)

	// 重复 task id → 失败，idempotency 不残留第二条。
	err = repo.CommitTask(ctx, Task{ID: "t1", AppID: "a", Type: TaskOperate, Status: TaskQueued, CreatedAt: now}, "k2", "hash2")
	assert.Error(t, err)
	_, ok2, _ := repo.GetIdempotency(ctx, "k2")
	assert.False(t, ok2, "回滚后 k2 不应存在")
}
