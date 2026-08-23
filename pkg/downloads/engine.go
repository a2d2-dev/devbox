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
	RootDir              string
	StatePath            string
	MaxConcurrent        int
	AllowPrivateNetworks bool
	HTTPClient           *http.Client
	Protocols            []Protocol
}

// Engine owns the persisted task state and a bounded set of protocol workers.
type Engine struct {
	mu                   sync.RWMutex
	rootDir              string
	root                 *os.Root
	statePath            string
	tasks                map[string]*Task
	totalDownloadedBytes int64
	protocols            map[string]Protocol
	slots                chan struct{}
	cancels              map[string]context.CancelFunc
	workerDone           map[string]chan struct{}
	deleting             map[string]bool
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
	rootHandle, err := os.OpenRoot(root)
	if err != nil {
		return nil, fmt.Errorf("open download root: %w", err)
	}
	limit := cfg.MaxConcurrent
	if limit <= 0 {
		limit = 3
	}
	e := &Engine{
		rootDir: root, root: rootHandle, statePath: statePath, tasks: make(map[string]*Task),
		totalDownloadedBytes: state.TotalDownloadedBytes,
		protocols:            make(map[string]Protocol), slots: make(chan struct{}, limit),
		cancels: make(map[string]context.CancelFunc), workerDone: make(map[string]chan struct{}), deleting: make(map[string]bool), scheduled: make(map[string]bool),
		generation: make(map[string]uint64),
	}
	protocols := cfg.Protocols
	if len(protocols) == 0 {
		protocols = []Protocol{&HTTPProtocol{Client: newDownloadHTTPClient(cfg.HTTPClient, cfg.AllowPrivateNetworks)}}
	}
	for _, protocol := range protocols {
		for _, scheme := range protocol.Schemes() {
			e.protocols[strings.ToLower(scheme)] = protocol
		}
	}
	for _, task := range state.Tasks {
		if task == nil || task.ID == "" {
			_ = rootHandle.Close()
			return nil, errors.New("download state contains a task without an ID")
		}
		if !pathWithin(root, task.Destination) {
			_ = rootHandle.Close()
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
		_ = rootHandle.Close()
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
	if e.deleting[id] {
		e.mu.Unlock()
		return Task{}, fmt.Errorf("%w: task is being deleted", ErrInvalidTransition)
	}
	if done := e.workerDone[id]; done != nil && (task.Status == StatusPaused || task.Status == StatusError) {
		e.mu.Unlock()
		<-done
		e.mu.Lock()
		task, ok = e.tasks[id]
		if !ok {
			e.mu.Unlock()
			return Task{}, ErrTaskNotFound
		}
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
		err = e.markPersistErrorLocked(task, "start task", err)
		e.mu.Unlock()
		return Task{}, err
	}
	result := cloneTask(task)
	ctx, generation, done := e.scheduleLocked(id)
	e.mu.Unlock()
	go e.run(ctx, id, generation, done)
	return result, nil
}

// Enqueue starts a newly-created waiting task without requiring an intermediate
// paused transition. It is used by the create endpoint's default auto-start.
func (e *Engine) Enqueue(id string) (Task, error) {
	e.mu.Lock()
	task, ok := e.tasks[id]
	started := e.started && e.ctx != nil
	if !ok {
		e.mu.Unlock()
		return Task{}, ErrTaskNotFound
	}
	if task.Status != StatusWaiting {
		e.mu.Unlock()
		return Task{}, fmt.Errorf("%w: task is %s", ErrInvalidTransition, task.Status)
	}
	if e.deleting[id] {
		e.mu.Unlock()
		return Task{}, fmt.Errorf("%w: task is being deleted", ErrInvalidTransition)
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
	ctx, generation, done := e.scheduleLocked(id)
	e.mu.Unlock()
	go e.run(ctx, id, generation, done)
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
	if e.deleting[id] {
		e.mu.Unlock()
		return Task{}, fmt.Errorf("%w: task is being deleted", ErrInvalidTransition)
	}
	task.Status = StatusPaused
	e.generation[id]++
	e.scheduled[id] = false
	task.SpeedBytesPerSec = 0
	task.EstimatedSeconds = 0
	task.UpdatedAt = time.Now().UTC()
	cancel := e.cancels[id]
	err := e.persistLocked()
	if err != nil {
		err = e.markPersistErrorLocked(task, "pause task", err)
	}
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
	if e.deleting[id] {
		e.mu.Unlock()
		return fmt.Errorf("%w: task is being deleted", ErrInvalidTransition)
	}
	e.deleting[id] = true
	e.generation[id]++
	e.scheduled[id] = false
	cancel := e.cancels[id]
	done := e.workerDone[id]
	e.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	task, ok = e.tasks[id]
	if !ok {
		delete(e.deleting, id)
		return ErrTaskNotFound
	}
	if deleteFile {
		destinationRelative, err := e.relativePath(task.Destination)
		if err == nil {
			for _, file := range []string{destinationRelative, destinationRelative + ".part"} {
				if removeErr := e.root.Remove(file); removeErr != nil && !os.IsNotExist(removeErr) {
					err = removeErr
					break
				}
			}
		}
		if err != nil {
			delete(e.deleting, id)
			delete(e.cancels, id)
			deleteErr := fmt.Errorf("delete downloaded file: %w", err)
			task.Status = StatusError
			task.Error = deleteErr.Error()
			task.SpeedBytesPerSec = 0
			task.EstimatedSeconds = 0
			task.UpdatedAt = time.Now().UTC()
			if persistErr := e.persistLocked(); persistErr != nil {
				return errors.Join(deleteErr, e.markPersistErrorLocked(task, "save file deletion failure", persistErr))
			}
			return deleteErr
		}
	}

	delete(e.tasks, id)
	if err := e.persistLocked(); err != nil {
		e.tasks[id] = task
		delete(e.deleting, id)
		return e.markPersistErrorLocked(task, "delete task", err)
	}
	delete(e.cancels, id)
	delete(e.workerDone, id)
	delete(e.scheduled, id)
	delete(e.generation, id)
	delete(e.deleting, id)
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

func (e *Engine) scheduleLocked(id string) (context.Context, uint64, chan struct{}) {
	e.generation[id]++
	e.scheduled[id] = true
	ctx, cancel := context.WithCancel(e.ctx)
	done := make(chan struct{})
	e.cancels[id] = cancel
	e.workerDone[id] = done
	return ctx, e.generation[id], done
}

func (e *Engine) run(ctx context.Context, id string, generation uint64, done chan struct{}) {
	defer e.workerFinished(id, generation, done)
	select {
	case e.slots <- struct{}{}:
		defer func() { <-e.slots }()
	case <-ctx.Done():
		return
	}

	e.mu.Lock()
	task, ok := e.tasks[id]
	if !ok || task.Status != StatusWaiting || !e.scheduled[id] || e.generation[id] != generation {
		e.mu.Unlock()
		return
	}
	e.scheduled[id] = false
	startedAt := time.Now().UTC()
	task.Status = StatusDownloading
	task.SpeedBytesPerSec = 0
	task.EstimatedSeconds = 0
	task.Error = ""
	task.StartedAt = &startedAt
	task.UpdatedAt = startedAt
	if err := e.persistLocked(); err != nil {
		e.markPersistErrorLocked(task, "mark task downloading", err)
		e.mu.Unlock()
		return
	}
	input := cloneTask(task)
	e.mu.Unlock()

	destinationRelative, err := e.relativePath(input.Destination)
	if err != nil {
		e.finishTransfer(id, generation, TransferResult{}, err)
		return
	}
	partial := destinationRelative + ".part"
	offset := int64(0)
	if info, err := e.root.Stat(partial); err == nil {
		offset = info.Size()
	}
	parsed, _ := url.Parse(input.URL)
	protocol := e.protocols[strings.ToLower(parsed.Scheme)]
	lastSampleAt := time.Now()
	lastSampleBytes := offset

	result, transferErr := protocol.Download(ctx, TransferRequest{
		URL: input.URL, PartialPath: partial, Root: e.root, Offset: offset,
		ETag: input.ETag, LastModified: input.LastModified,
		CanWrite: func() bool {
			e.mu.RLock()
			defer e.mu.RUnlock()
			current, exists := e.tasks[id]
			return exists && current.Status == StatusDownloading && e.generation[id] == generation
		},
	}, func(progress TransferProgress) {
		e.mu.Lock()
		defer e.mu.Unlock()
		e.totalDownloadedBytes += progress.BytesReceived
		current, exists := e.tasks[id]
		if !exists || current.Status != StatusDownloading || e.generation[id] != generation {
			if progress.BytesReceived > 0 {
				if err := e.persistLocked(); err != nil && exists {
					e.markPersistErrorLocked(current, "save canceled download traffic", err)
				}
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
			if err := e.persistLocked(); err != nil {
				e.markPersistErrorLocked(current, "save download progress", err)
				if cancel := e.cancels[id]; cancel != nil {
					cancel()
				}
				return
			}
			e.lastPersist = now
		}
	})

	e.finishTransfer(id, generation, result, transferErr)
}

func (e *Engine) workerFinished(id string, generation uint64, done chan struct{}) {
	close(done)
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.workerDone[id] == done {
		delete(e.workerDone, id)
	}
	if e.generation[id] == generation {
		delete(e.cancels, id)
	}
}

func (e *Engine) finishTransfer(id string, generation uint64, result TransferResult, transferErr error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	current, ok := e.tasks[id]
	if !ok || e.generation[id] != generation {
		return
	}
	if current.Status == StatusPaused {
		return
	}
	if current.Status == StatusError && errors.Is(transferErr, context.Canceled) {
		if err := e.persistLocked(); err != nil {
			e.markPersistErrorLocked(current, "save canceled task error", err)
		}
		return
	}
	if transferErr == nil {
		destinationRelative, err := e.relativePath(current.Destination)
		if err != nil {
			transferErr = err
		} else if _, err := e.root.Lstat(destinationRelative); err == nil {
			transferErr = fmt.Errorf("%w: destination file appeared during download", ErrTaskConflict)
		} else if !os.IsNotExist(err) {
			transferErr = err
		} else if err := e.root.Rename(destinationRelative+".part", destinationRelative); err != nil {
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
	if err := e.persistLocked(); err != nil {
		e.markPersistErrorLocked(current, "save final task state", err)
	}
}

func (e *Engine) markPersistErrorLocked(task *Task, operation string, persistErr error) error {
	err := fmt.Errorf("persist download state while trying to %s: %w", operation, persistErr)
	task.Status = StatusError
	task.Error = err.Error()
	task.SpeedBytesPerSec = 0
	task.EstimatedSeconds = 0
	task.CompletedAt = nil
	task.UpdatedAt = time.Now().UTC()
	return err
}

func (e *Engine) relativePath(destination string) (string, error) {
	relative, err := filepath.Rel(e.rootDir, destination)
	if err != nil || relative == "." || filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", ErrPathOutsideRoot
	}
	return relative, nil
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
