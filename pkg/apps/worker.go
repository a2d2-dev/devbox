package apps

import (
	"context"
	"os"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

// worker 实现 taskRunner：异步执行持久化任务。
//
//   - per-app 串行：每个 app 一个有序 channel + 一个消费 goroutine，同一 app 的
//     变更任务按入队顺序串行执行；不同 app 之间并发（受 GOMAXPROCS 自然限制）。
//   - 崩溃恢复：进程重启后扫描 queued/running 任务重新入队；执行时以 adapter 的
//     实际观测状态为准（compose up/operate 幂等），不盲信进程内状态。
//   - 审计与 observed revision 更新随执行结果落地。
type worker struct {
	repo     Repository
	adapters map[RuntimeKind]runtimeAdapter
	paths    *Paths
	logger   *zap.Logger
	now      func() time.Time

	mu     sync.Mutex
	queues map[string]chan string
	ctx    context.Context
}

// NewWorker 构造 worker（未启动）。调用 Start 后开始消费 + 恢复。
func NewWorker(repo Repository, adapters map[RuntimeKind]runtimeAdapter, paths *Paths, logger *zap.Logger) *worker {
	return &worker{
		repo:     repo,
		adapters: adapters,
		paths:    paths,
		logger:   logger,
		now:      time.Now,
		queues:   map[string]chan string{},
	}
}

// WithWorkerClock 注入时钟（测试用）。
func (w *worker) WithWorkerClock(now func() time.Time) *worker { w.now = now; return w }

// Start 启动消费：崩溃恢复 + 准备好接收 Enqueue。
func (w *worker) Start(ctx context.Context) {
	w.ctx = ctx
	tasks, err := w.repo.ListNonTerminalTasks(ctx)
	if err != nil {
		w.logger.Warn("recover: list non-terminal tasks failed", zap.Error(err))
		return
	}
	recovered := 0
	for _, t := range tasks {
		// running 的任务进程已死，重置为 queued 重新执行。
		if t.Status == TaskRunning {
			_ = w.repo.UpdateTask(ctx, t.ID, func(x *Task) {
				x.Status = TaskQueued
				x.Phase = ""
				x.Message = "re-queued after restart"
			})
		}
		w.Enqueue(t.ID)
		recovered++
	}
	if recovered > 0 {
		w.logger.Info("worker recovered tasks", zap.Int("count", recovered))
	}
}

// Enqueue 把 taskID 投递到对应 app 的串行队列。
func (w *worker) Enqueue(taskID string) {
	ctx := w.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	task, err := w.repo.GetTask(ctx, taskID)
	if err != nil {
		w.logger.Warn("enqueue: task not found", zap.String("id", taskID), zap.Error(err))
		return
	}
	if task.Status.IsTerminal() {
		return
	}
	ch := w.ensureQueue(task.AppID)
	select {
	case ch <- taskID:
	default:
		w.logger.Warn("task queue full", zap.String("app", task.AppID))
	}
}

func (w *worker) ensureQueue(appID string) chan string {
	w.mu.Lock()
	defer w.mu.Unlock()
	ch, ok := w.queues[appID]
	if !ok {
		ch = make(chan string, 64)
		w.queues[appID] = ch
		go w.runAppQueue(appID, ch)
	}
	return ch
}

func (w *worker) runAppQueue(appID string, ch chan string) {
	for {
		select {
		case <-w.ctx.Done():
			return
		case taskID := <-ch:
			w.execute(w.ctx, taskID)
		}
	}
}

func (w *worker) setPhase(ctx context.Context, taskID string, phase TaskPhase, msg string) {
	_ = w.repo.UpdateTask(ctx, taskID, func(t *Task) {
		t.Phase = phase
		if msg != "" {
			t.Message = msg
		}
	})
}

// execute 执行单个任务（幂等；以 adapter 实际状态为准）。
func (w *worker) execute(ctx context.Context, taskID string) {
	task, err := w.repo.GetTask(ctx, taskID)
	if err != nil {
		return
	}
	if task.Status.IsTerminal() {
		return // 已被处理（重投去重）
	}

	started := w.now()
	_ = w.repo.UpdateTask(ctx, taskID, func(t *Task) {
		t.Status = TaskRunning
		t.Phase = PhaseTaskValidating
		t.StartedAt = &started
	})
	w.logger.Info("task started", zap.String("id", taskID),
		zap.String("app", task.AppID), zap.String("type", string(task.Type)))

	// 解析运行时：有 meta 用 meta.Runtime（compose 登记项），否则 K8s。
	meta, hasMeta, _ := w.repo.GetAppMeta(ctx, task.AppID)
	rt := RuntimeKubernetes
	if hasMeta {
		rt = meta.Runtime
	}
	adapter := w.adapters[rt]
	app := Application{ID: task.AppID, Runtime: rt, Revision: task.Revision}
	if hasMeta {
		app.Name = meta.Name
		app.Source = meta.Source
	}

	var execErr error
	if adapter == nil {
		execErr = CapabilityErr("runtime " + string(rt) + " unavailable")
	} else {
		execErr = w.dispatch(ctx, adapter, task, app)
	}

	finished := w.now()
	_ = w.repo.UpdateTask(ctx, taskID, func(t *Task) {
		t.FinishedAt = &finished
		if execErr == nil {
			t.Status = TaskSucceeded
			t.Phase = PhaseTaskVerifying
			t.Message = ""
		} else {
			t.Status = TaskFailed
			t.Message = sanitizeLog(execErr.Error())
		}
	})
	if execErr != nil {
		w.logger.Warn("task failed", zap.String("id", taskID), zap.String("app", task.AppID), zap.Error(execErr))
	} else {
		w.logger.Info("task succeeded", zap.String("id", taskID), zap.String("app", task.AppID))
	}
}

// dispatch 按 task 类型路由到 adapter，并处理副作用（observed revision / 清理）。
func (w *worker) dispatch(ctx context.Context, adapter runtimeAdapter, task Task, app Application) error {
	switch task.Type {
	case TaskApply, TaskRestore:
		w.setPhase(ctx, task.ID, PhaseTaskApplying, "")
		composeFile := w.paths.ComposeFile(task.AppID)
		if err := adapter.Apply(ctx, app, composeFile); err != nil {
			return err
		}
		w.setPhase(ctx, task.ID, PhaseTaskVerifying, "")
		// 执行后重新观察以实际状态为准；这里先落 observed revision。
		if task.Revision > 0 {
			_ = w.repo.SetObservedRevision(ctx, task.AppID, task.Revision)
		}
		return nil
	case TaskOperate:
		return adapter.Operate(ctx, app, task.Action)
	case TaskRemove:
		w.setPhase(ctx, task.ID, PhaseTaskCleaningUp, "")
		if err := adapter.Remove(ctx, app, task.Purge); err != nil {
			return err
		}
		// 容器/网络已下线；清理元数据 + 目录（受管 volume 由 adapter purge 处理）。
		if err := w.repo.PurgeApp(ctx, task.AppID); err != nil {
			return err
		}
		_ = os.RemoveAll(w.paths.AppDir(task.AppID))
		return nil
	default:
		return nil
	}
}

// sanitizeLog 任务/日志脱敏：截断超长 + 折叠明显的环境变量赋值（KEY=value）。
func sanitizeLog(s string) string {
	const max = 1000
	// 折叠形如 PASSWORD=xxx 的 env 泄漏（compose CLI 偶尔回显）。
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		lines[i] = redactEnvLine(ln)
	}
	out := strings.Join(lines, "\n")
	if len(out) > max {
		out = out[:max] + "...(truncated)"
	}
	return out
}

func redactEnvLine(line string) string {
	idx := strings.IndexByte(line, '=')
	if idx <= 0 {
		return line
	}
	key := strings.TrimSpace(line[:idx])
	if isSecretyKey(key) {
		return key + "=***"
	}
	return line
}

// isSecretyKey 启发式：键名暗示敏感则脱敏。
func isSecretyKey(k string) bool {
	up := strings.ToUpper(k)
	for _, hint := range []string{"PASS", "SECRET", "TOKEN", "KEY", "CRED", "AUTH"} {
		if strings.Contains(up, hint) {
			return true
		}
	}
	return false
}
