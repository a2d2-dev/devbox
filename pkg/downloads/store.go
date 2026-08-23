package downloads

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type persistedState struct {
	Version              int     `json:"version"`
	Tasks                []*Task `json:"tasks"`
	TotalDownloadedBytes int64   `json:"totalDownloadedBytes"`
}

func loadState(path string) (persistedState, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return persistedState{Version: 1, Tasks: []*Task{}}, nil
	}
	if err != nil {
		return persistedState{}, fmt.Errorf("read download state: %w", err)
	}
	var state persistedState
	if err := json.Unmarshal(b, &state); err != nil {
		return persistedState{}, fmt.Errorf("decode download state: %w", err)
	}
	if state.Version != 1 {
		return persistedState{}, fmt.Errorf("unsupported download state version %d", state.Version)
	}
	return state, nil
}

func saveState(path string, state persistedState) error {
	b, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode download state: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("create download state directory: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".downloads-*.tmp")
	if err != nil {
		return fmt.Errorf("create download state temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return fmt.Errorf("write download state: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close download state: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace download state: %w", err)
	}
	return nil
}
