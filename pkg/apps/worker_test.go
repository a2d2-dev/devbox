package apps

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func newTestWorker(t *testing.T, adapters map[RuntimeKind]runtimeAdapter) (*worker, Repository, *Paths) {
	t.Helper()
	dir := t.TempDir()
	repo, err := OpenRepository(context.Background(), filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	paths := NewPaths(dir)
	w := NewWorker(repo, adapters, paths, zap.NewNop())
	w.ctx = context.Background()
	t.Cleanup(func() { _ = repo.Close() })
	return w, repo, paths
}

func prepApp(t *testing.T, repo Repository, paths *Paths, id string) {
	t.Helper()
	require.NoError(t, repo.UpsertAppMeta(context.Background(), AppRecord{
		ID: id, Name: id, Runtime: RuntimeCompose, Revision: 1,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}))
	require.NoError(t, paths.EnsureAppDir(id))
	require.NoError(t, os.WriteFile(paths.ComposeFile(id), []byte(safeCompose), 0o644))
}

func TestWorkerExecuteApply(t *testing.T) {
	compose := &fakeAdapter{kind: RuntimeCompose}
	w, repo, paths := newTestWorker(t, map[RuntimeKind]runtimeAdapter{RuntimeCompose: compose})
	ctx := context.Background()
	prepApp(t, repo, paths, "a")
	require.NoError(t, repo.CreateTask(ctx, Task{ID: "t1", AppID: "a", Type: TaskApply, Revision: 1, Status: TaskQueued, CreatedAt: time.Now()}))

	w.execute(ctx, "t1")

	got, err := repo.GetTask(ctx, "t1")
	require.NoError(t, err)
	assert.Equal(t, TaskSucceeded, got.Status)
	assert.NotNil(t, got.StartedAt)
	assert.NotNil(t, got.FinishedAt)
	assert.Equal(t, []string{"a"}, compose.applied)

	// observed revision 已更新。
	meta, _, _ := repo.GetAppMeta(ctx, "a")
	assert.Equal(t, int64(1), meta.ObservedRevision)
}

func TestWorkerExecuteRemovePurgesMetaAndDir(t *testing.T) {
	compose := &fakeAdapter{kind: RuntimeCompose}
	w, repo, paths := newTestWorker(t, map[RuntimeKind]runtimeAdapter{RuntimeCompose: compose})
	ctx := context.Background()
	prepApp(t, repo, paths, "a")
	require.NoError(t, repo.CreateTask(ctx, Task{ID: "t1", AppID: "a", Type: TaskRemove, Purge: true, Status: TaskQueued, CreatedAt: time.Now()}))

	w.execute(ctx, "t1")

	got, _ := repo.GetTask(ctx, "t1")
	assert.Equal(t, TaskSucceeded, got.Status)
	assert.Equal(t, []string{"a"}, compose.removed)
	_, ok, _ := repo.GetAppMeta(ctx, "a")
	assert.False(t, ok, "meta 应已清理")
	_, statErr := os.Stat(paths.AppDir("a"))
	assert.True(t, os.IsNotExist(statErr), "目录应已删除")
}

func TestWorkerExecuteFailureSanitized(t *testing.T) {
	compose := &fakeAdapter{kind: RuntimeCompose, applyErr: errors.New("pull failed DB_PASSWORD=hunter2")}
	w, repo, paths := newTestWorker(t, map[RuntimeKind]runtimeAdapter{RuntimeCompose: compose})
	ctx := context.Background()
	prepApp(t, repo, paths, "a")
	require.NoError(t, repo.CreateTask(ctx, Task{ID: "t1", AppID: "a", Type: TaskApply, Revision: 1, Status: TaskQueued, CreatedAt: time.Now()}))

	w.execute(ctx, "t1")
	got, _ := repo.GetTask(ctx, "t1")
	assert.Equal(t, TaskFailed, got.Status)
	assert.Contains(t, got.Message, "DB_PASSWORD=***")
	assert.NotContains(t, got.Message, "hunter2")
}

func TestWorkerStartRecovers(t *testing.T) {
	compose := &fakeAdapter{kind: RuntimeCompose}
	w, repo, paths := newTestWorker(t, map[RuntimeKind]runtimeAdapter{RuntimeCompose: compose})
	ctx := context.Background()
	prepApp(t, repo, paths, "a")
	// 预置一个 running 任务（进程崩溃残留）。
	require.NoError(t, repo.CreateTask(ctx, Task{ID: "t1", AppID: "a", Type: TaskApply, Revision: 1, Status: TaskRunning, CreatedAt: time.Now()}))

	w.Start(ctx) // 应重投并执行

	require.Eventually(t, func() bool {
		tt, _ := repo.GetTask(ctx, "t1")
		return tt.Status == TaskSucceeded
	}, time.Second, 20*time.Millisecond)
	assert.Equal(t, []string{"a"}, compose.applied)
}

func TestWorkerPerAppSerial(t *testing.T) {
	compose := &fakeAdapter{kind: RuntimeCompose, delay: 80 * time.Millisecond}
	w, repo, paths := newTestWorker(t, map[RuntimeKind]runtimeAdapter{RuntimeCompose: compose})
	ctx := context.Background()
	prepApp(t, repo, paths, "a")
	require.NoError(t, repo.CreateTask(ctx, Task{ID: "t1", AppID: "a", Type: TaskApply, Revision: 1, Status: TaskQueued, CreatedAt: time.Now()}))
	require.NoError(t, repo.CreateTask(ctx, Task{ID: "t2", AppID: "a", Type: TaskOperate, Action: ActionRestart, Status: TaskQueued, CreatedAt: time.Now()}))

	w.Enqueue("t1")
	w.Enqueue("t2")

	require.Eventually(t, func() bool {
		t1, _ := repo.GetTask(ctx, "t1")
		t2, _ := repo.GetTask(ctx, "t2")
		return t1.Status.IsTerminal() && t2.Status.IsTerminal()
	}, 2*time.Second, 20*time.Millisecond)

	// 同 app 两任务都执行；apply 因 delay 先占用，operate 排在其后（串行）。
	compose.mu.Lock()
	applies := len(compose.applied)
	operates := len(compose.operated)
	compose.mu.Unlock()
	assert.Equal(t, 1, applies)
	assert.Equal(t, 1, operates)
	// operate 必须在 apply（含 80ms 延迟）完成之后才入执行。
	o2, _ := repo.GetTask(ctx, "t2")
	o1, _ := repo.GetTask(ctx, "t1")
	assert.NotNil(t, o2.StartedAt)
	assert.NotNil(t, o1.FinishedAt)
	assert.True(t, o2.StartedAt.After(o1.FinishedAt.Add(-10*time.Millisecond)), "第二个任务应在第一个完成后才开始")
}

func TestWorkerSkipsTerminalRequeue(t *testing.T) {
	compose := &fakeAdapter{kind: RuntimeCompose}
	w, repo, paths := newTestWorker(t, map[RuntimeKind]runtimeAdapter{RuntimeCompose: compose})
	ctx := context.Background()
	prepApp(t, repo, paths, "a")
	// 已终态任务不应被重复执行。
	require.NoError(t, repo.CreateTask(ctx, Task{ID: "t1", AppID: "a", Type: TaskApply, Revision: 1, Status: TaskSucceeded, CreatedAt: time.Now()}))
	w.execute(ctx, "t1")
	compose.mu.Lock()
	assert.Empty(t, compose.applied)
	compose.mu.Unlock()
}
