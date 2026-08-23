package files

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type TrashEntry struct {
	ID           string    `json:"id"`
	Source       string    `json:"source"`
	Name         string    `json:"name"`
	OriginalPath string    `json:"originalPath"`
	DeletedAt    time.Time `json:"deletedAt"`
	IsDir        bool      `json:"isDir"`
	Size         int64     `json:"size"`
}

type trashIndex struct {
	Entries []TrashEntry `json:"entries"`
}

func (b *Browser) trashDir() string { return filepath.Join(b.rootDir, ".trash") }

func (b *Browser) loadTrashLocked() (trashIndex, error) {
	var index trashIndex
	data, err := os.ReadFile(filepath.Join(b.trashDir(), "index.json"))
	if os.IsNotExist(err) {
		return index, nil
	}
	if err != nil {
		return index, fileError("IO_ERROR", "read trash index: %v", err)
	}
	if err := json.Unmarshal(data, &index); err != nil {
		return index, fileError("STATE_CORRUPT", "decode trash index: %v", err)
	}
	return index, nil
}

func (b *Browser) saveTrashLocked(index trashIndex) error {
	dir := b.trashDir()
	if err := os.MkdirAll(filepath.Join(dir, "files"), 0o700); err != nil {
		return fileError("IO_ERROR", "create trash: %v", err)
	}
	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return fileError("IO_ERROR", "encode trash: %v", err)
	}
	tmp, err := os.CreateTemp(dir, "index-*.json")
	if err != nil {
		return fileError("IO_ERROR", "create trash index: %v", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fileError("IO_ERROR", "protect trash index: %v", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fileError("IO_ERROR", "write trash index: %v", err)
	}
	if err := tmp.Close(); err != nil {
		return fileError("IO_ERROR", "close trash index: %v", err)
	}
	if err := os.Rename(tmpName, filepath.Join(dir, "index.json")); err != nil {
		return fileError("IO_ERROR", "replace trash index: %v", err)
	}
	return nil
}

func (b *Browser) Delete(sourceID, path string, permanent bool) error {
	source, err := b.require(sourceID, func(c Capabilities) bool { return c.Delete })
	if err != nil {
		return err
	}
	full, clean, err := b.resolve(source, path, false)
	if err != nil {
		return err
	}
	if clean == "" {
		return fileError("PATH_FORBIDDEN", "cannot delete source root")
	}
	info, err := os.Lstat(full)
	if err != nil {
		return fileError("PATH_NOT_FOUND", "path not found")
	}
	if permanent || !source.Capabilities.Trash {
		if err := os.RemoveAll(full); err != nil {
			return fileError("IO_ERROR", "permanent delete: %v", err)
		}
		return b.appendAudit("files.permanent_delete", source.ID, cleanDisplayPath(clean))
	}
	id, err := randomID(12)
	if err != nil {
		return fileError("IO_ERROR", "generate trash id: %v", err)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	index, err := b.loadTrashLocked()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(b.trashDir(), "files"), 0o700); err != nil {
		return fileError("IO_ERROR", "create trash: %v", err)
	}
	destination := filepath.Join(b.trashDir(), "files", id)
	if err := os.Rename(full, destination); err != nil {
		return fileError("IO_ERROR", "move to trash: %v", err)
	}
	entry := TrashEntry{ID: id, Source: source.ID, Name: filepath.Base(clean), OriginalPath: filepath.ToSlash(clean), DeletedAt: b.now().UTC(), IsDir: info.IsDir(), Size: info.Size()}
	index.Entries = append([]TrashEntry{entry}, index.Entries...)
	if err := b.saveTrashLocked(index); err != nil {
		_ = os.Rename(destination, full)
		return err
	}
	return nil
}

func (b *Browser) Trash(query string) ([]TrashEntry, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	index, err := b.loadTrashLocked()
	if err != nil {
		return nil, err
	}
	query = strings.ToLower(strings.TrimSpace(query))
	result := make([]TrashEntry, 0, len(index.Entries))
	for _, entry := range index.Entries {
		if query == "" || strings.Contains(strings.ToLower(entry.Name), query) || strings.Contains(strings.ToLower(entry.OriginalPath), query) {
			result = append(result, entry)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].DeletedAt.After(result[j].DeletedAt) })
	return result, nil
}

func (b *Browser) RestoreTrash(id string) error {
	if !validTrashID(id) {
		return fileError("TRASH_NOT_FOUND", "trash entry not found")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	index, err := b.loadTrashLocked()
	if err != nil {
		return err
	}
	position := -1
	var entry TrashEntry
	for i, candidate := range index.Entries {
		if candidate.ID == id {
			position, entry = i, candidate
			break
		}
	}
	if position < 0 {
		return fileError("TRASH_NOT_FOUND", "trash entry not found")
	}
	source, err := b.getSource(entry.Source)
	if err != nil {
		return err
	}
	clean, err := normalizeRelative(entry.OriginalPath)
	if err != nil || clean == "" {
		return fileError("PATH_FORBIDDEN", "invalid original path")
	}
	if err := b.ensureSafeDirectories(source, filepath.Dir(clean)); err != nil {
		return err
	}
	destination, _, err := b.resolve(source, clean, true)
	if err != nil {
		return err
	}
	if _, statErr := os.Lstat(destination); statErr == nil {
		return fileError("CONFLICT", "original path is occupied")
	}
	stored := filepath.Join(b.trashDir(), "files", id)
	if _, err := os.Lstat(stored); err != nil {
		return fileError("TRASH_NOT_FOUND", "trash content not found")
	}
	if err := os.Rename(stored, destination); err != nil {
		return fileError("IO_ERROR", "restore trash: %v", err)
	}
	index.Entries = append(index.Entries[:position], index.Entries[position+1:]...)
	if err := b.saveTrashLocked(index); err != nil {
		_ = os.Rename(destination, stored)
		return err
	}
	return nil
}

func (b *Browser) ensureSafeDirectories(source Source, rel string) error {
	clean, err := normalizeRelative(rel)
	if err != nil {
		return err
	}
	if clean == "" {
		return nil
	}
	current := source.Root
	for _, part := range strings.Split(clean, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, statErr := os.Lstat(current)
		if os.IsNotExist(statErr) {
			if err := os.Mkdir(current, 0o755); err != nil {
				return fileError("IO_ERROR", "recreate original directory: %v", err)
			}
			continue
		}
		if statErr != nil {
			return fileError("IO_ERROR", "inspect original directory: %v", statErr)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fileError("PATH_FORBIDDEN", "symlinks are not allowed")
		}
		if !info.IsDir() {
			return fileError("NOT_DIRECTORY", "original parent is not a directory")
		}
	}
	return nil
}

func (b *Browser) PurgeTrash(id string) error {
	if !validTrashID(id) {
		return fileError("TRASH_NOT_FOUND", "trash entry not found")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	index, err := b.loadTrashLocked()
	if err != nil {
		return err
	}
	position := -1
	var entry TrashEntry
	for i, candidate := range index.Entries {
		if candidate.ID == id {
			position, entry = i, candidate
			break
		}
	}
	if position < 0 {
		return fileError("TRASH_NOT_FOUND", "trash entry not found")
	}
	if err := os.RemoveAll(filepath.Join(b.trashDir(), "files", id)); err != nil {
		return fileError("IO_ERROR", "purge trash: %v", err)
	}
	index.Entries = append(index.Entries[:position], index.Entries[position+1:]...)
	if err := b.saveTrashLocked(index); err != nil {
		return err
	}
	return b.appendAudit("files.trash_purge", entry.Source, entry.OriginalPath)
}

func validTrashID(id string) bool {
	if len(id) != 24 {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
}

func (b *Browser) EmptyTrash() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	index, err := b.loadTrashLocked()
	if err != nil {
		return err
	}
	if err := os.RemoveAll(filepath.Join(b.trashDir(), "files")); err != nil {
		return fileError("IO_ERROR", "empty trash: %v", err)
	}
	index.Entries = nil
	if err := b.saveTrashLocked(index); err != nil {
		return err
	}
	return b.appendAudit("files.trash_empty", "my", "")
}
