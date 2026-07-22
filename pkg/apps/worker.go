package apps

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

// worker 实现 taskRunner：异步执行持久化任务。
//
//   - per-app 串行：每个 app 一个有序 channel + 一个消费 goroutine，同一 app 的
//     变更任务按入队顺序串行执行；不同 app 之间通过全局 slots 有限并发。
//   - 崩溃恢复：进程重启后扫描 queued/running 任务重新入队；执行时以 adapter 的
//     实际观测状态为准（compose up/operate 幂等），不盲信进程内状态。
//   - 审计与 observed revision 更新随执行结果落地。
type worker struct {
	repo          Repository
	adapters      map[RuntimeKind]runtimeAdapter
	paths         *Paths
	logger        *zap.Logger
	now           func() time.Time
	healthTimeout time.Duration
	observeGrace  time.Duration
	pollInterval  time.Duration

	mu     sync.Mutex
	queues map[string]*appQueue
	ctx    context.Context
	slots  chan struct{}
}

// appQueue 单个 app 的串行队列 + 退出信号。
type appQueue struct {
	ch   chan string
	done chan struct{}
}

// NewWorker 构造 worker（未启动）。调用 Start 后开始消费 + 恢复。
func NewWorker(repo Repository, adapters map[RuntimeKind]runtimeAdapter, paths *Paths, logger *zap.Logger) *worker {
	return &worker{
		repo:          repo,
		adapters:      adapters,
		paths:         paths,
		logger:        logger,
		now:           time.Now,
		healthTimeout: 2 * time.Minute,
		observeGrace:  3 * time.Second,
		pollInterval:  2 * time.Second,
		queues:        map[string]*appQueue{},
		slots:         make(chan struct{}, 4),
	}
}

// WithWorkerClock 注入时钟（测试用）。
func (w *worker) WithWorkerClock(now func() time.Time) *worker { w.now = now; return w }

// WithWorkerHealthTiming 注入健康等待时序（测试用）。
func (w *worker) WithWorkerHealthTiming(timeout, grace, poll time.Duration) *worker {
	w.healthTimeout, w.observeGrace, w.pollInterval = timeout, grace, poll
	return w
}

// WithWorkerConcurrency 设置不同应用间的最大并发任务数（测试/装配调优用）。
func (w *worker) WithWorkerConcurrency(limit int) *worker {
	if limit > 0 {
		w.slots = make(chan struct{}, limit)
	}
	return w
}

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

// Enqueue 把 taskID 投递到对应 app 的串行队列。队列满时阻塞等待（尊重 ctx），
// 不丢弃已持久化的 task。
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
	q := w.ensureQueue(task.AppID)
	select {
	case q.ch <- taskID:
	case <-ctx.Done():
		w.logger.Warn("enqueue canceled (worker shutting down)", zap.String("task", taskID))
	}
}

func (w *worker) ensureQueue(appID string) *appQueue {
	w.mu.Lock()
	defer w.mu.Unlock()
	q, ok := w.queues[appID]
	if !ok {
		q = &appQueue{ch: make(chan string, 64), done: make(chan struct{})}
		w.queues[appID] = q
		go w.runAppQueue(appID, q)
	}
	return q
}

// stopQueue 安全回收某 app 的队列：从 map 移除并通知消费 goroutine 退出。
// 在 app 被彻底删除（remove 完成）后调用，避免队列/goroutine 永久增长。
// done 仅此处关闭，且 map 删除保证只关闭一次。
func (w *worker) stopQueue(appID string) {
	w.mu.Lock()
	q, ok := w.queues[appID]
	if ok {
		delete(w.queues, appID)
	}
	w.mu.Unlock()
	if ok {
		close(q.done)
	}
}

func (w *worker) runAppQueue(appID string, q *appQueue) {
	ctx := w.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-q.done:
			return
		case taskID := <-q.ch:
			w.executeSafe(ctx, taskID)
		}
	}
}

// executeSafe 包裹 execute：recover 任一 panic → task failed，goroutine 继续消费。
func (w *worker) executeSafe(ctx context.Context, taskID string) {
	select {
	case w.slots <- struct{}{}:
		defer func() { <-w.slots }()
	case <-ctx.Done():
		return
	}
	defer func() {
		if r := recover(); r != nil {
			w.logger.Error("task panic recovered", zap.String("task", taskID), zap.Any("panic", r))
			finished := w.now()
			_ = w.repo.UpdateTask(ctx, taskID, func(t *Task) {
				t.Status = TaskFailed
				t.Phase = PhaseTaskVerifying
				t.Message = "任务执行异常（panic），已恢复"
				t.FinishedAt = &finished
			})
		}
	}()
	w.execute(ctx, taskID)
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
	// HIGH#2：meta 读取错误必须传播为 task 失败，避免错误降级到 K8s。
	meta, hasMeta, gerr := w.repo.GetAppMeta(ctx, task.AppID)
	var execErr error
	var envFile string
	if gerr != nil {
		execErr = fmt.Errorf("resolve runtime: %w", gerr)
	} else {
		rt := RuntimeKubernetes
		if hasMeta {
			rt = meta.Runtime
			if rt == RuntimeCompose {
				if b, readErr := os.ReadFile(w.paths.EnvFile(task.AppID)); readErr == nil {
					envFile = string(b)
				}
			}
		}
		adapter := w.adapters[rt]
		app := Application{ID: task.AppID, Runtime: rt, Revision: task.Revision}
		if hasMeta {
			app.Name = meta.Name
			app.Source = meta.Source
		}
		if adapter == nil {
			execErr = CapabilityErr("runtime " + string(rt) + " unavailable")
		} else {
			execErr = w.dispatch(ctx, adapter, task, app)
		}
	}

	finished := w.now()
	safeExecMessage := ""
	if execErr != nil {
		safeExecMessage = sanitizeWithEnvValues(execErr.Error(), envFile)
	}
	_ = w.repo.UpdateTask(ctx, taskID, func(t *Task) {
		t.FinishedAt = &finished
		if execErr == nil {
			t.Status = TaskSucceeded
			t.Phase = PhaseTaskVerifying
			t.Message = ""
		} else {
			t.Status = TaskFailed
			t.Message = safeExecMessage
		}
	})
	if execErr != nil {
		w.logger.Warn("task failed", zap.String("id", taskID), zap.String("app", task.AppID), zap.String("error", safeExecMessage))
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
		// up 返回成功不代表容器已就绪；有 healthcheck 时等待全部 healthy。
		if err := w.waitForHealthy(ctx, task.ID, adapter, task.AppID); err != nil {
			return err
		}
		if task.Revision > 0 {
			_ = w.repo.SetObservedRevision(ctx, task.AppID, task.Revision)
		}
		return nil
	case TaskOperate:
		if err := adapter.Operate(ctx, app, task.Action); err != nil {
			return err
		}
		// desired running 的动作需校验容器实际出现；stop 不校验（容器停止/消失均正常）。
		if task.Action == ActionStart || task.Action == ActionRestart || task.Action == ActionRedeploy {
			if err := w.waitForHealthy(ctx, task.ID, adapter, task.AppID); err != nil {
				return err
			}
		}
		return nil
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
		// MED#11：app 已彻底删除，回收其串行队列/goroutine，避免永久增长。
		w.stopQueue(task.AppID)
		return nil
	default:
		return nil
	}
}

// waitForHealthy 在 apply/restore/start/restart/redeploy 后重新 Observe。没有 healthcheck
// 的服务在全部 running 后成功；声明了 healthcheck 的服务必须全部 healthy。容器短暂
// 未出现使用 observeGrace，有界健康等待使用 healthTimeout，均尊重 ctx。
func (w *worker) waitForHealthy(ctx context.Context, taskID string, adapter runtimeAdapter, appID string) error {
	w.setPhase(ctx, taskID, PhaseTaskWaitingHealth, "等待服务健康")
	started := time.Now()
	deadline := started.Add(w.healthTimeout)
	for {
		ready, retryable, err := w.checkObservedOnce(ctx, adapter, appID)
		if ready || err != nil && !retryable {
			return err
		}
		now := time.Now()
		if now.After(deadline) {
			return fmt.Errorf("等待服务健康超时")
		}
		if err != nil && now.Sub(started) >= w.observeGrace {
			return err
		}
		timer := time.NewTimer(w.pollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

// checkObservedOnce 返回 ready、是否可重试以及错误。starting/created 属于可重试；
// unhealthy/failed/exited 属于确定失败。
func (w *worker) checkObservedOnce(ctx context.Context, adapter runtimeAdapter, appID string) (bool, bool, error) {
	obs, err := adapter.Observe(ctx)
	if err != nil {
		w.logger.Warn("verify: observe runtime state failed", zap.String("app", appID), zap.Error(err))
		return false, true, fmt.Errorf("部署后无法观测运行时状态")
	}
	app, hasObs := obs[appID]
	if !hasObs || len(app.Observed.Services) == 0 {
		return false, true, fmt.Errorf("部署后未观测到任何容器，请检查镜像/配置")
	}
	if app.Observed.Phase == PhaseFailed || app.Observed.Phase == PhaseDegraded {
		return false, false, fmt.Errorf("部署后容器进入失败或降级状态")
	}
	for _, service := range app.Observed.Services {
		if service.Health == "unhealthy" || service.State == "exited" || service.State == "dead" {
			return false, false, fmt.Errorf("服务 %s 未通过健康检查", service.Name)
		}
		if service.State != "running" || service.Health == "starting" {
			return false, true, nil
		}
	}
	return true, false, nil
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
