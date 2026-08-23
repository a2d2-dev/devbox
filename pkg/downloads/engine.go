package downloads

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type Config struct {
	RootDir       string
	StatePath     string
	MaxConcurrent int
	HTTPClient    *http.Client
	Protocols     []Protocol
}

// Engine owns the persisted task state and a bounded set of protocol workers.
type Engine struct {
	mu                   sync.RWMutex
	rootDir              string
	statePath            string
	tasks                map[string]*Task
	totalDownloadedBytes int64
	protocols            map[string]Protocol
	slots                chan struct{}
	cancels              map[string]context.CancelFunc
	scheduled            map[string]bool
	generation           map[string]uint64
	ctx                  context.Context
	started              bool
	lastPersist          time.Time
}

func New(cfg Config) (*Engine, error) {
	root, err := normalizeRoot(cfg.RootDir)
	if err != nil {
		return nil, err
	}
	statePath := cfg.StatePath
	if statePath == "" {
		statePath = filepath.Join(root, ".devbox", "downloads.json")
	}
	statePath, err = filepath.Abs(statePath)
	if err != nil {
		return nil, fmt.Errorf("resolve download state path: %w", err)
	}
	state, err := loadState(statePath)
	if err != nil {
		return nil, err
	}
	limit := cfg.MaxConcurrent
	if limit <= 0 {
		limit = 3
	}
	e := &Engine{
		rootDir: root, statePath: statePath, tasks: make(map[string]*Task),
		totalDownloadedBytes: state.TotalDownloadedBytes,
		protocols:            make(map[string]Protocol), slots: make(chan struct{}, limit),
		cancels: make(map[string]context.CancelFunc), scheduled: make(map[string]bool),
		generation: make(map[string]uint64),
	}
	protocols := cfg.Protocols
	if len(protocols) == 0 {
		protocols = []Protocol{&HTTPProtocol{Client: cfg.HTTPClient}}
	}
	for _, protocol := range protocols {
		for _, scheme := range protocol.Schemes() {
			e.protocols[strings.ToLower(scheme)] = protocol
		}
	}
	for _, task := range state.Tasks {
		if task == nil || task.ID == "" {
			return nil, errors.New("download state contains a task without an ID")
		}
		if !pathWithin(root, task.Destination) {
			return nil, fmt.Errorf("download state task %s has destination outside root", task.ID)
		}
		copy := *task
		copy.SpeedBytesPerSec = 0
		copy.EstimatedSeconds = 0
		if copy.Status == StatusWaiting || copy.Status == StatusDownloading {
			copy.Status = StatusPaused
			copy.Error = ""
		}
		e.tasks[copy.ID] = &copy
	}
	if err := e.persistLocked(); err != nil {
		return nil, err
	}
	return e, nil
}

// Start attaches the engine to the process lifetime. Persisted incomplete tasks
// have already been restored as paused and are never started implicitly.
func (e *Engine) Start(ctx context.Context) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.started {
		return
	}
	e.ctx = ctx
	e.started = true
}

func (e *Engine) RootDir() string { return e.rootDir }

func (e *Engine) Add(req AddRequest) (Task, error) {
	parsed, err := validateURL(req.URL)
	if err != nil {
		return Task{}, err
	}
	protocol := e.protocols[strings.ToLower(parsed.Scheme)]
	if protocol == nil {
		return Task{}, fmt.Errorf("%w: unsupported scheme %q", ErrInvalidURL, parsed.Scheme)
	}
	target, targetRelative, err := ResolveTargetDirectory(e.rootDir, req.TargetDirectory)
	if err != nil {
		return Task{}, err
	}
	decodedPath, _ := url.PathUnescape(parsed.Path)
	name := safeFilename(path.Base(decodedPath))
	destination := filepath.Join(target, name)
	if !pathWithin(e.rootDir, destination) {
		return Task{}, ErrPathOutsideRoot
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	for _, candidate := range []string{destination, destination + ".part"} {
		if _, err := os.Lstat(candidate); err == nil {
			return Task{}, fmt.Errorf("%w: destination or partial file already exists", ErrTaskConflict)
		} else if !os.IsNotExist(err) {
			return Task{}, fmt.Errorf("inspect destination: %w", err)
		}
	}
	for _, existing := range e.tasks {
		if existing.Destination == destination {
			return Task{}, fmt.Errorf("%w: another task uses this destination", ErrTaskConflict)
		}
	}
	now := time.Now().UTC()
	task := &Task{
		ID: newID(), URL: parsed.String(), Name: name,
		TargetDirectory: targetRelative, Destination: destination,
		Status: StatusWaiting, CreatedAt: now, UpdatedAt: now,
	}
	e.tasks[task.ID] = task
	if err := e.persistLocked(); err != nil {
		delete(e.tasks, task.ID)
		return Task{}, err
	}
	return cloneTask(task), nil
}

func (e *Engine) StartTask(id string) (Task, error) {
	e.mu.Lock()
	task, ok := e.tasks[id]
	if !ok {
		e.mu.Unlock()
		return Task{}, ErrTaskNotFound
	}
	if !e.started || e.ctx == nil {
		e.mu.Unlock()
		return Task{}, errors.New("download engine is not started")
	}
	switch task.Status {
	case StatusPaused, StatusError:
		task.Status = StatusWaiting
		task.Error = ""
		task.CompletedAt = nil
	case StatusWaiting:
		if e.scheduled[id] {
			e.mu.Unlock()
			return Task{}, fmt.Errorf("%w: task is already waiting", ErrInvalidTransition)
		}
	default:
		e.mu.Unlock()
		return Task{}, fmt.Errorf("%w: task is %s", ErrInvalidTransition, task.Status)
	}
	task.UpdatedAt = time.Now().UTC()
	if err := e.persistLocked(); err != nil {
		e.mu.Unlock()
		return Task{}, err
	}
	result := cloneTask(task)
	ctx := e.ctx
	generation := e.scheduleLocked(id)
	e.mu.Unlock()
	go e.run(ctx, id, generation)
	return result, nil
}

// Enqueue starts a newly-created waiting task without requiring an intermediate
// paused transition. It is used by the create endpoint's default auto-start.
func (e *Engine) Enqueue(id string) (Task, error) {
	e.mu.Lock()
	task, ok := e.tasks[id]
	started := e.started && e.ctx != nil
	var ctx context.Context
	if started {
		ctx = e.ctx
	}
	if !ok {
		e.mu.Unlock()
		return Task{}, ErrTaskNotFound
	}
	if task.Status != StatusWaiting {
		e.mu.Unlock()
		return Task{}, fmt.Errorf("%w: task is %s", ErrInvalidTransition, task.Status)
	}
	if e.scheduled[id] {
		e.mu.Unlock()
		return Task{}, fmt.Errorf("%w: task is already waiting", ErrInvalidTransition)
	}
	result := cloneTask(task)
	if !started {
		e.mu.Unlock()
		return Task{}, errors.New("download engine is not started")
	}
	generation := e.scheduleLocked(id)
	e.mu.Unlock()
	go e.run(ctx, id, generation)
	return result, nil
}

func (e *Engine) Pause(id string) (Task, error) {
	e.mu.Lock()
	task, ok := e.tasks[id]
	if !ok {
		e.mu.Unlock()
		return Task{}, ErrTaskNotFound
	}
	if task.Status != StatusWaiting && task.Status != StatusDownloading {
		e.mu.Unlock()
		return Task{}, fmt.Errorf("%w: task is %s", ErrInvalidTransition, task.Status)
	}
	task.Status = StatusPaused
	e.generation[id]++
	e.scheduled[id] = false
	task.SpeedBytesPerSec = 0
	task.EstimatedSeconds = 0
	task.UpdatedAt = time.Now().UTC()
	cancel := e.cancels[id]
	delete(e.cancels, id)
	err := e.persistLocked()
	result := cloneTask(task)
	e.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return result, err
}

func (e *Engine) Delete(id string, deleteFile bool) error {
	e.mu.Lock()
	task, ok := e.tasks[id]
	if !ok {
		e.mu.Unlock()
		return ErrTaskNotFound
	}
	cancel := e.cancels[id]
	delete(e.cancels, id)
	delete(e.scheduled, id)
	delete(e.generation, id)
	delete(e.tasks, id)
	if err := e.persistLocked(); err != nil {
		e.tasks[id] = task
		e.mu.Unlock()
		return err
	}
	e.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if deleteFile {
		for _, file := range []string{task.Destination, task.Destination + ".part"} {
			if err := os.Remove(file); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("delete downloaded file: %w", err)
			}
		}
	}
	return nil
}

func (e *Engine) Get(id string) (Task, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	task, ok := e.tasks[id]
	if !ok {
		return Task{}, ErrTaskNotFound
	}
	return cloneTask(task), nil
}

func (e *Engine) Snapshot() Snapshot {
	e.mu.RLock()
	defer e.mu.RUnlock()
	tasks := make([]Task, 0, len(e.tasks))
	var counts Counts
	var speed int64
	for _, task := range e.tasks {
		copy := cloneTask(task)
		tasks = append(tasks, copy)
		counts.All++
		switch task.Status {
		case StatusWaiting:
			counts.Waiting++
		case StatusDownloading:
			counts.Downloading++
			speed += task.SpeedBytesPerSec
		case StatusPaused:
			counts.Paused++
		case StatusCompleted:
			counts.Completed++
		case StatusError:
			counts.Error++
		}
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].CreatedAt.After(tasks[j].CreatedAt) })
	return Snapshot{
		Tasks: tasks, Counts: counts, RootDir: e.rootDir,
		Statistics: Statistics{DownloadSpeedBytesPerSec: speed, TotalDownloadedBytes: e.totalDownloadedBytes},
	}
}

func (e *Engine) scheduleLocked(id string) uint64 {
	e.generation[id]++
	e.scheduled[id] = true
	return e.generation[id]
}

func (e *Engine) run(parent context.Context, id string, generation uint64) {
	select {
	case e.slots <- struct{}{}:
		defer func() { <-e.slots }()
	case <-parent.Done():
		return
	}

	e.mu.Lock()
	task, ok := e.tasks[id]
	if !ok || task.Status != StatusWaiting || !e.scheduled[id] || e.generation[id] != generation {
		e.mu.Unlock()
		return
	}
	e.scheduled[id] = false
	ctx, cancel := context.WithCancel(parent)
	e.cancels[id] = cancel
	startedAt := time.Now().UTC()
	task.Status = StatusDownloading
	task.SpeedBytesPerSec = 0
	task.EstimatedSeconds = 0
	task.Error = ""
	task.StartedAt = &startedAt
	task.UpdatedAt = startedAt
	_ = e.persistLocked()
	input := cloneTask(task)
	e.mu.Unlock()
	defer cancel()

	partial := input.Destination + ".part"
	offset := int64(0)
	if info, err := os.Stat(partial); err == nil {
		offset = info.Size()
	}
	parsed, _ := url.Parse(input.URL)
	protocol := e.protocols[strings.ToLower(parsed.Scheme)]
	lastSampleAt := time.Now()
	lastSampleBytes := offset

	result, transferErr := protocol.Download(ctx, TransferRequest{
		URL: input.URL, PartialPath: partial, Offset: offset,
		ETag: input.ETag, LastModified: input.LastModified,
	}, func(progress TransferProgress) {
		e.mu.Lock()
		defer e.mu.Unlock()
		e.totalDownloadedBytes += progress.BytesReceived
		current, exists := e.tasks[id]
		if !exists || current.Status != StatusDownloading || e.generation[id] != generation {
			if progress.BytesReceived > 0 {
				_ = e.persistLocked()
			}
			return
		}
		current.DownloadedBytes = progress.Downloaded
		current.TotalBytes = progress.Total
		current.ResumeSupported = progress.ResumeSupported
		current.ETag = progress.ETag
		current.LastModified = progress.LastModified
		now := time.Now()
		if elapsed := now.Sub(lastSampleAt); elapsed >= 250*time.Millisecond {
			bytesSince := progress.Downloaded - lastSampleBytes
			if bytesSince >= 0 {
				current.SpeedBytesPerSec = int64(float64(bytesSince) / elapsed.Seconds())
			}
			lastSampleAt = now
			lastSampleBytes = progress.Downloaded
		}
		if current.TotalBytes > current.DownloadedBytes && current.SpeedBytesPerSec > 0 {
			current.EstimatedSeconds = (current.TotalBytes - current.DownloadedBytes) / current.SpeedBytesPerSec
		} else {
			current.EstimatedSeconds = 0
		}
		current.UpdatedAt = now.UTC()
		if now.Sub(e.lastPersist) >= 500*time.Millisecond {
			_ = e.persistLocked()
			e.lastPersist = now
		}
	})

	e.mu.Lock()
	defer e.mu.Unlock()
	current, ok := e.tasks[id]
	if !ok || e.generation[id] != generation {
		return
	}
	delete(e.cancels, id)
	if current.Status == StatusPaused {
		return
	}
	if transferErr == nil {
		if _, err := os.Lstat(current.Destination); err == nil {
			transferErr = fmt.Errorf("%w: destination file appeared during download", ErrTaskConflict)
		} else if !os.IsNotExist(err) {
			transferErr = err
		} else if err := os.Rename(partial, current.Destination); err != nil {
			transferErr = fmt.Errorf("finalize download: %w", err)
		}
	}
	finishedAt := time.Now().UTC()
	current.SpeedBytesPerSec = 0
	current.EstimatedSeconds = 0
	current.UpdatedAt = finishedAt
	if transferErr == nil {
		current.Status = StatusCompleted
		current.DownloadedBytes = result.Downloaded
		current.TotalBytes = result.Total
		current.ResumeSupported = result.ResumeSupported
		current.ETag = result.ETag
		current.LastModified = result.LastModified
		current.CompletedAt = &finishedAt
		current.Error = ""
	} else if !errors.Is(transferErr, context.Canceled) {
		current.Status = StatusError
		current.Error = transferErr.Error()
	} else {
		current.Status = StatusPaused
	}
	_ = e.persistLocked()
}

func (e *Engine) persistLocked() error {
	tasks := make([]*Task, 0, len(e.tasks))
	for _, task := range e.tasks {
		copy := cloneTask(task)
		tasks = append(tasks, &copy)
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].CreatedAt.Before(tasks[j].CreatedAt) })
	return saveState(e.statePath, persistedState{Version: 1, Tasks: tasks, TotalDownloadedBytes: e.totalDownloadedBytes})
}

func validateURL(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
		return nil, fmt.Errorf("%w: only http and https URLs without embedded credentials are allowed", ErrInvalidURL)
	}
	u.Fragment = ""
	return u, nil
}

func cloneTask(task *Task) Task {
	copy := *task
	return copy
}

func newID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
