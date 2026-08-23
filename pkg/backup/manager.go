package backup

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"

	"go.uber.org/zap"
)

var ErrConflict = errors.New("backup task is already running")

type Manager struct {
	store  *store
	engine *engine
	logger *zap.Logger
	policy pathPolicy
	slots  chan struct{}
	now    func() time.Time

	mu       sync.Mutex
	running  map[string]runningEntry
	targets  map[string]chan struct{}
	ctx      context.Context
	start    sync.Once
	tickRate time.Duration
}

type runningEntry struct {
	target   chan struct{}
	acquired bool
}

func NewManager(dataDir string, concurrency int, logger *zap.Logger, options ...ManagerOption) (*Manager, error) {
	if concurrency < 1 {
		concurrency = 2
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	if dataDir == "" {
		dataDir = "/var/lib/devbox/backup"
	}
	config := managerConfig{workDir: "/data"}
	for _, option := range options {
		option(&config)
	}
	policy, err := newPathPolicy(config.workDir, dataDir, config.allowedTargetRoots)
	if err != nil {
		return nil, err
	}
	repo, err := openStore(dataDir, logger)
	if err != nil {
		return nil, err
	}
	if err := repo.recoverInterrupted(); err != nil {
		return nil, err
	}
	return &Manager{
		store: repo, engine: newEngine(policy), logger: logger, policy: policy, slots: make(chan struct{}, concurrency),
		now: time.Now, running: map[string]runningEntry{}, targets: map[string]chan struct{}{}, ctx: context.Background(), tickRate: 5 * time.Second,
	}, nil
}

func (m *Manager) Start(ctx context.Context) {
	m.start.Do(func() {
		m.ctx = ctx
		go m.scheduler(ctx)
	})
}

func (m *Manager) scheduler(ctx context.Context) {
	m.runDue()
	ticker := time.NewTicker(m.tickRate)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.runDue()
		}
	}
}

func (m *Manager) runDue() {
	now := m.now()
	for _, task := range m.store.listTasks() {
		if task.Paused || task.NextRunAt == nil || task.NextRunAt.After(now) {
			continue
		}
		next, err := nextSchedule(task.Schedule, now)
		if err != nil {
			m.logger.Warn("backup schedule calculation failed", zap.String("task", task.ID), zap.Error(err))
			continue
		}
		_, _ = m.store.updateTask(task.ID, func(t *Task) {
			t.NextRunAt = &next
			t.UpdatedAt = now
		})
		if _, err := m.enqueueBackup(task.ID); err != nil && !errors.Is(err, ErrConflict) {
			m.logger.Warn("scheduled backup enqueue failed", zap.String("task", task.ID), zap.Error(err))
		}
	}
}

func (m *Manager) Preflight(ctx context.Context, task Task) PreflightResult {
	return preflight(ctx, task, m.policy)
}

func (m *Manager) Create(ctx context.Context, task Task) (Task, PreflightResult, error) {
	check := preflight(ctx, task, m.policy)
	if !check.OK {
		return Task{}, check, fmt.Errorf("backup preflight failed: %s", failedChecks(check))
	}
	task, err := normalizeTaskPaths(task)
	if err != nil {
		return Task{}, check, fmt.Errorf("normalize backup paths: %w", err)
	}
	now := m.now()
	next, err := nextSchedule(task.Schedule, now)
	if err != nil {
		return Task{}, check, err
	}
	task.ID = newID()
	task.Status = StatusIdle
	task.LastResult = ""
	task.LastRunAt = nil
	task.CreatedAt = now
	task.UpdatedAt = now
	if task.Paused {
		task.NextRunAt = nil
	} else {
		task.NextRunAt = &next
	}
	if err := m.store.putTask(task); err != nil {
		return Task{}, check, err
	}
	return task, check, nil
}

func (m *Manager) List() []Task {
	tasks := m.store.listTasks()
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].CreatedAt.After(tasks[j].CreatedAt) })
	return tasks
}

func (m *Manager) Get(id string) (Task, error) { return m.store.getTask(id) }

func (m *Manager) SetPaused(id string, paused bool) (Task, error) {
	now := m.now()
	var next *time.Time
	if !paused {
		task, err := m.store.getTask(id)
		if err != nil {
			return Task{}, err
		}
		value, err := nextSchedule(task.Schedule, now)
		if err != nil {
			return Task{}, err
		}
		next = &value
	}
	return m.store.updateTask(id, func(task *Task) {
		task.Paused = paused
		task.NextRunAt = next
		task.UpdatedAt = now
	})
}

func (m *Manager) Histories(taskID string) ([]History, error) {
	if _, err := m.store.getTask(taskID); err != nil {
		return nil, err
	}
	return m.store.histories(taskID), nil
}

func (m *Manager) History(taskID, historyID string) (History, error) {
	if _, err := m.store.getTask(taskID); err != nil {
		return History{}, err
	}
	for _, history := range m.store.histories(taskID) {
		if history.ID == historyID {
			return history, nil
		}
	}
	return History{}, ErrNotFound
}

func (m *Manager) Versions(ctx context.Context, taskID string) ([]string, error) {
	task, err := m.store.getTask(taskID)
	if err != nil {
		return nil, err
	}
	if task.Mode == ModeMirror {
		return []string{"mirror"}, nil
	}
	return listVersions(ctx, taskTarget(task))
}

func (m *Manager) RunNow(taskID string) (History, error) {
	return m.enqueueBackup(taskID)
}

func (m *Manager) enqueueBackup(taskID string) (History, error) {
	task, err := m.store.getTask(taskID)
	if err != nil {
		return History{}, err
	}
	history, err := m.reserve(task, RunBackup, "")
	if err != nil {
		return History{}, err
	}
	go m.executeBackup(task, history)
	return history, nil
}

func (m *Manager) PreviewRestore(ctx context.Context, taskID string, request RestoreRequest) (RestorePreview, error) {
	task, err := m.store.getTask(taskID)
	if err != nil {
		return RestorePreview{}, err
	}
	return m.engine.previewRestore(ctx, task, request)
}

func (m *Manager) Restore(ctx context.Context, taskID string, request RestoreRequest) (History, error) {
	task, err := m.store.getTask(taskID)
	if err != nil {
		return History{}, err
	}
	if !request.Confirm || request.PreviewToken == "" {
		return History{}, fmt.Errorf("restore requires confirm=true and a preview token")
	}
	preview, err := m.engine.previewRestore(ctx, task, request)
	if err != nil {
		return History{}, err
	}
	if preview.Token != request.PreviewToken {
		return History{}, fmt.Errorf("restore preview changed; preview again before confirming")
	}
	history, err := m.reserve(task, RunRestore, preview.Destination)
	if err != nil {
		return History{}, err
	}
	history.Version = request.Version
	if err := m.store.putHistory(history); err != nil {
		m.release(task.ID)
		return History{}, err
	}
	go m.executeRestore(task, request, history)
	return history, nil
}

func (m *Manager) reserve(task Task, kind RunKind, restoreTarget string) (History, error) {
	m.mu.Lock()
	if _, exists := m.running[task.ID]; exists {
		m.mu.Unlock()
		return History{}, ErrConflict
	}
	targetKey, err := normalizedTargetKey(task.Target)
	if err != nil {
		m.mu.Unlock()
		return History{}, err
	}
	targetSlot := m.targets[targetKey]
	if targetSlot == nil {
		targetSlot = make(chan struct{}, 1)
		m.targets[targetKey] = targetSlot
	}
	m.running[task.ID] = runningEntry{target: targetSlot}
	m.mu.Unlock()

	now := m.now()
	history := History{
		ID: newID(), TaskID: task.ID, Kind: kind, Status: StatusQueued, Phase: "queued",
		RestoreTarget: restoreTarget, StartedAt: now,
	}
	if err := m.store.putHistory(history); err != nil {
		m.release(task.ID)
		return History{}, err
	}
	if _, err := m.store.updateTask(task.ID, func(t *Task) {
		t.Status = StatusQueued
		t.LastResult = "等待执行"
		t.UpdatedAt = now
	}); err != nil {
		m.release(task.ID)
		return History{}, err
	}
	return history, nil
}

func (m *Manager) executeBackup(task Task, history History) {
	if !m.acquire(task, &history) {
		return
	}
	version, bytes, log, err := m.engine.runBackup(m.ctx, task)
	history.Version = version
	history.TransferredBytes = bytes
	history.Log = capLog(log)
	m.finish(task.ID, history, err)
}

func (m *Manager) executeRestore(task Task, request RestoreRequest, history History) {
	if !m.acquire(task, &history) {
		return
	}
	bytes, log, err := m.engine.runRestore(m.ctx, task, request)
	history.TransferredBytes = bytes
	history.Log = capLog(log)
	m.finish(task.ID, history, err)
}

func (m *Manager) acquire(task Task, history *History) bool {
	m.mu.Lock()
	entry, exists := m.running[task.ID]
	m.mu.Unlock()
	if !exists {
		return false
	}
	select {
	case entry.target <- struct{}{}:
		m.mu.Lock()
		entry.acquired = true
		m.running[task.ID] = entry
		m.mu.Unlock()
	case <-m.ctx.Done():
		m.finish(task.ID, *history, m.ctx.Err())
		return false
	}
	select {
	case m.slots <- struct{}{}:
		history.Status = StatusRunning
		history.Phase = "transfer"
		history.StartedAt = m.now()
		_ = m.store.putHistory(*history)
		_, _ = m.store.updateTask(task.ID, func(task *Task) {
			task.Status = StatusRunning
			task.LastResult = "执行中"
			task.UpdatedAt = m.now()
		})
		return true
	case <-m.ctx.Done():
		m.finish(task.ID, *history, m.ctx.Err())
		return false
	}
}

func (m *Manager) finish(taskID string, history History, runErr error) {
	if history.Status == StatusRunning {
		<-m.slots
	}
	finished := m.now()
	history.FinishedAt = &finished
	if runErr == nil {
		history.Status = StatusSuccess
		history.Phase = "completed"
	} else {
		history.Status = StatusFailed
		history.Error = runErr.Error()
		history.Phase = "failed"
		var failure *runFailure
		if errors.As(runErr, &failure) {
			history.Phase = failure.phase
			if history.Log == "" {
				history.Log = capLog(failure.log)
			}
		}
	}
	if err := m.store.finishRun(taskID, history, func(task *Task) {
		task.LastRunAt = &finished
		task.UpdatedAt = finished
		if runErr == nil {
			task.Status = StatusSuccess
			task.LastResult = "成功"
		} else {
			task.Status = StatusFailed
			task.LastResult = runErr.Error()
		}
	}); err != nil {
		m.logger.Error("persist backup completion failed", zap.String("task", taskID), zap.String("history", history.ID), zap.Error(err))
	}
	if runErr != nil {
		m.logger.Warn("backup run failed", zap.String("task", taskID), zap.String("history", history.ID), zap.String("phase", history.Phase), zap.Error(runErr))
	}
	m.release(taskID)
}

func (m *Manager) release(taskID string) {
	m.mu.Lock()
	entry, exists := m.running[taskID]
	delete(m.running, taskID)
	m.mu.Unlock()
	if exists && entry.acquired {
		<-entry.target
	}
}

func normalizedTargetKey(target Endpoint) (string, error) {
	if target.Type == EndpointSSH {
		port := target.Port
		if port == 0 {
			port = 22
		}
		return "ssh://" + target.Host + ":" + strconv.Itoa(port) + filepath.Clean(target.Path), nil
	}
	resolved, err := resolveExistingPath(target.Path)
	if err != nil {
		return "", fmt.Errorf("resolve backup target lock: %w", err)
	}
	return "local:" + resolved, nil
}

func capLog(log string) string {
	const limit = 1 << 20
	if len(log) <= limit {
		return log
	}
	return "[日志已截断，仅保留最后 1 MiB]\n" + log[len(log)-limit:]
}

func newID() string {
	var value [12]byte
	if _, err := rand.Read(value[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(value[:])
}
