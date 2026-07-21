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

// MED#4：adapter panic 必须被 recover，task 标 failed，worker 继续消费后续任务。
func TestWorkerPanicRecovered(t *testing.T) {
	compose := &fakeAdapter{kind: RuntimeCompose, panicsRemaining: 1}
	w, repo, paths := newTestWorker(t, map[RuntimeKind]runtimeAdapter{RuntimeCompose: compose})
	ctx := context.Background()
	prepApp(t, repo, paths, "a")
	require.NoError(t, repo.CreateTask(ctx, Task{ID: "t1", AppID: "a", Type: TaskApply, Revision: 1, Status: TaskQueued, CreatedAt: time.Now()}))

	// 通过消费循环执行（executeSafe recover）；不应 panic 外溢。
	require.NotPanics(t, func() { w.executeSafe(ctx, "t1") })
	got, _ := repo.GetTask(ctx, "t1")
	assert.Equal(t, TaskFailed, got.Status)

	// 后续任务仍可正常执行（worker 未死）。
	require.NoError(t, repo.CreateTask(ctx, Task{ID: "t2", AppID: "a", Type: TaskApply, Revision: 1, Status: TaskQueued, CreatedAt: time.Now()}))
	w.executeSafe(ctx, "t2")
	got2, _ := repo.GetTask(ctx, "t2")
	assert.Equal(t, TaskSucceeded, got2.Status)
}

// MED#4：入队数量超过 channel 缓冲（64）也不丢弃已持久化 task。
func TestWorkerQueueFullDoesNotDrop(t *testing.T) {
	compose := &fakeAdapter{kind: RuntimeCompose, delay: 5 * time.Millisecond}
	w, repo, paths := newTestWorker(t, map[RuntimeKind]runtimeAdapter{RuntimeCompose: compose})
	ctx := context.Background()
	prepApp(t, repo, paths, "a")

	const n = 80 // 超过缓冲 64
	for i := 0; i < n; i++ {
		require.NoError(t, repo.CreateTask(ctx, Task{
			ID: "t" + itoa(int64(i)), AppID: "a", Type: TaskApply, Revision: 1,
			Status: TaskQueued, CreatedAt: time.Now(),
		}))
	}
	for i := 0; i < n; i++ {
		w.Enqueue("t" + itoa(int64(i)))
	}
	// 全部应最终到达终态（无丢弃）。
	require.Eventually(t, func() bool {
		for i := 0; i < n; i++ {
			tk, _ := repo.GetTask(ctx, "t"+itoa(int64(i)))
			if !tk.Status.IsTerminal() {
				return false
			}
		}
		return true
	}, 10*time.Second, 50*time.Millisecond)
}

// MED#8：apply 后未观测到容器（desired running）→ 任务判 failed，而非 succeeded。
func TestWorkerApplyFailsWhenNoContainers(t *testing.T) {
	compose := &fakeAdapter{kind: RuntimeCompose, applyNoObserve: true}
	w, repo, paths := newTestWorker(t, map[RuntimeKind]runtimeAdapter{RuntimeCompose: compose})
	ctx := context.Background()
	prepApp(t, repo, paths, "a")
	require.NoError(t, repo.CreateTask(ctx, Task{ID: "t1", AppID: "a", Type: TaskApply, Revision: 1, Status: TaskQueued, CreatedAt: time.Now()}))

	w.execute(ctx, "t1")
	got, _ := repo.GetTask(ctx, "t1")
	assert.Equal(t, TaskFailed, got.Status, "无容器应判 failed")
	assert.Contains(t, got.Message, "未观测到")
}

// MED#8：start 后容器出现 → 任务 succeeded。
func TestWorkerStartVerifiesRunning(t *testing.T) {
	compose := &fakeAdapter{kind: RuntimeCompose}
	w, repo, paths := newTestWorker(t, map[RuntimeKind]runtimeAdapter{RuntimeCompose: compose})
	ctx := context.Background()
	prepApp(t, repo, paths, "a")
	require.NoError(t, repo.CreateTask(ctx, Task{ID: "t1", AppID: "a", Type: TaskOperate, Action: ActionStart, Status: TaskQueued, CreatedAt: time.Now()}))
	w.execute(ctx, "t1")
	got, _ := repo.GetTask(ctx, "t1")
	assert.Equal(t, TaskSucceeded, got.Status)
}

// MED#11：remove 成功后回收 app 的串行队列（不再永久驻留）。
func TestWorkerStopQueueOnRemove(t *testing.T) {
	compose := &fakeAdapter{kind: RuntimeCompose}
	w, repo, paths := newTestWorker(t, map[RuntimeKind]runtimeAdapter{RuntimeCompose: compose})
	ctx := context.Background()
	prepApp(t, repo, paths, "a")
	// 触发 ensureQueue 创建队列。
	q := w.ensureQueue("a")
	require.NotNil(t, q)
	require.Contains(t, w.queues, "a")

	require.NoError(t, repo.CreateTask(ctx, Task{ID: "t1", AppID: "a", Type: TaskRemove, Purge: true, Status: TaskQueued, CreatedAt: time.Now()}))
	w.execute(ctx, "t1") // dispatch 中 TaskRemove 成功后调用 stopQueue

	require.Eventually(t, func() bool {
		w.mu.Lock()
		defer w.mu.Unlock()
		_, ok := w.queues["a"]
		return !ok
	}, time.Second, 10*time.Millisecond, "remove 后队列应被回收")

	// 二次 stopQueue 不应 panic（幂等）。
	require.NotPanics(t, func() { w.stopQueue("a") })
	_ = paths
}
