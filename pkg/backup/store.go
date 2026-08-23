package backup

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"go.uber.org/zap"
)

var ErrNotFound = errors.New("backup record not found")

type store struct {
	path string
	mu   sync.Mutex
	data state
}

func openStore(dataDir string, logger *zap.Logger) (*store, error) {
	if dataDir == "" {
		dataDir = "/var/lib/devbox/backup"
	}
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		return nil, fmt.Errorf("create backup data directory: %w", err)
	}
	s := &store{path: filepath.Join(dataDir, "state.json"), data: state{Tasks: []Task{}, Histories: []History{}}}
	b, err := os.ReadFile(s.path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read backup state: %w", err)
	}
	if err == nil {
		if err := json.Unmarshal(b, &s.data); err != nil {
			backupPath := s.path + ".corrupt-" + fmt.Sprintf("%d", time.Now().UTC().UnixNano())
			if renameErr := os.Rename(s.path, backupPath); renameErr != nil {
				return nil, fmt.Errorf("backup corrupt backup state: %w (decode error: %v)", renameErr, err)
			}
			logger.Warn("Backup state was corrupt; moved aside and starting empty",
				zap.String("path", s.path), zap.String("corrupt_backup", backupPath), zap.Error(err))
			s.data = state{Tasks: []Task{}, Histories: []History{}}
		}
		if s.data.Tasks == nil {
			s.data.Tasks = []Task{}
		}
		if s.data.Histories == nil {
			s.data.Histories = []History{}
		}
	}
	return s, nil
}

func (s *store) saveLocked() error {
	b, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".backup-state-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, s.path)
}

func cloneTask(t Task) Task {
	t.Excludes = append([]string(nil), t.Excludes...)
	return t
}

func cloneHistory(h History) History { return h }

func (s *store) listTasks() []Task {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Task, len(s.data.Tasks))
	for i := range s.data.Tasks {
		out[i] = cloneTask(s.data.Tasks[i])
	}
	return out
}

func (s *store) getTask(id string) (Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.data.Tasks {
		if t.ID == id {
			return cloneTask(t), nil
		}
	}
	return Task{}, ErrNotFound
}

func (s *store) putTask(task Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.data.Tasks {
		if s.data.Tasks[i].ID == task.ID {
			s.data.Tasks[i] = cloneTask(task)
			return s.saveLocked()
		}
	}
	s.data.Tasks = append(s.data.Tasks, cloneTask(task))
	return s.saveLocked()
}

func (s *store) updateTask(id string, mutate func(*Task)) (Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.data.Tasks {
		if s.data.Tasks[i].ID == id {
			mutate(&s.data.Tasks[i])
			if err := s.saveLocked(); err != nil {
				return Task{}, err
			}
			return cloneTask(s.data.Tasks[i]), nil
		}
	}
	return Task{}, ErrNotFound
}

func (s *store) putHistory(history History) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.data.Histories {
		if s.data.Histories[i].ID == history.ID {
			s.data.Histories[i] = cloneHistory(history)
			return s.saveLocked()
		}
	}
	s.data.Histories = append(s.data.Histories, cloneHistory(history))
	return s.saveLocked()
}

func (s *store) finishRun(taskID string, history History, mutateTask func(*Task)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	foundTask, foundHistory := false, false
	for i := range s.data.Tasks {
		if s.data.Tasks[i].ID == taskID {
			mutateTask(&s.data.Tasks[i])
			foundTask = true
			break
		}
	}
	for i := range s.data.Histories {
		if s.data.Histories[i].ID == history.ID {
			s.data.Histories[i] = cloneHistory(history)
			foundHistory = true
			break
		}
	}
	if !foundTask || !foundHistory {
		return ErrNotFound
	}
	return s.saveLocked()
}

func (s *store) histories(taskID string) []History {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]History, 0)
	for i := len(s.data.Histories) - 1; i >= 0; i-- {
		if s.data.Histories[i].TaskID == taskID {
			out = append(out, cloneHistory(s.data.Histories[i]))
		}
	}
	return out
}

func (s *store) recoverInterrupted() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := false
	finished := time.Now()
	for i := range s.data.Tasks {
		if s.data.Tasks[i].Status == StatusQueued || s.data.Tasks[i].Status == StatusRunning {
			s.data.Tasks[i].Status = StatusFailed
			s.data.Tasks[i].LastResult = "进程重启，运行已中断"
			changed = true
		}
	}
	for i := range s.data.Histories {
		if s.data.Histories[i].Status == StatusQueued || s.data.Histories[i].Status == StatusRunning {
			s.data.Histories[i].Status = StatusFailed
			s.data.Histories[i].Phase = "interrupted"
			s.data.Histories[i].Error = "进程重启，运行已中断"
			s.data.Histories[i].FinishedAt = &finished
			changed = true
		}
	}
	if changed {
		return s.saveLocked()
	}
	return nil
}
