package apps

import (
	"context"
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
		Source: ApplicationSource{Kind: SourceInline, Version: "1.0"}, Revision: 3,
		Parameters: map[string]string{"P": "1"}, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, repo.UpsertAppMeta(ctx, a))

	got, ok, err := repo.GetAppMeta(ctx, "my-app")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "My App", got.Name)
	assert.Equal(t, RuntimeCompose, got.Runtime)
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
	require.NoError(t, repo.InsertRevision(ctx, Revision{Number: 1, AppID: "a", ComposeHash: "h1", CreatedAt: time.Now()}))
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
