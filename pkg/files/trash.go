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
	PendingPurge bool      `json:"pendingPurge,omitempty"`
}

type trashIndex struct {
	Entries []TrashEntry `json:"entries"`
}

func (b *Browser) loadTrashLocked() (trashIndex, error) {
	var index trashIndex
	root, err := os.OpenRoot(b.rootDir)
	if err != nil {
		return index, fileError("SOURCE_UNAVAILABLE", "open trash root: %v", err)
	}
	defer root.Close()
	data, err := rootReadFile(root, ".trash/index.json")
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
	root, err := os.OpenRoot(b.rootDir)
	if err != nil {
		return fileError("SOURCE_UNAVAILABLE", "open trash root: %v", err)
	}
	defer root.Close()
	if err := rootMkdirAll(root, ".trash/files", 0o700); err != nil {
		return fileError("IO_ERROR", "create trash: %v", err)
	}
	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return fileError("IO_ERROR", "encode trash: %v", err)
	}
	tmpID, err := randomID(8)
	if err != nil {
		return fileError("IO_ERROR", "generate trash index name: %v", err)
	}
	tmpName := filepath.ToSlash(filepath.Join(".trash", "index-"+tmpID+".json"))
	tmp, err := root.OpenFile(tmpName, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fileError("IO_ERROR", "create trash index: %v", err)
	}
	defer root.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fileError("IO_ERROR", "write trash index: %v", err)
	}
	if err := tmp.Close(); err != nil {
		return fileError("IO_ERROR", "close trash index: %v", err)
	}
	if err := renameReplace(b.rootDir, tmpName, ".trash/index.json"); err != nil {
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
	root, err := os.OpenRoot(source.Root)
	if err != nil {
		return fileError("SOURCE_UNAVAILABLE", "open source root: %v", err)
	}
	defer root.Close()
	info, err := root.Lstat(rootPath(clean))
	if err != nil {
		return fileError("PATH_NOT_FOUND", "path not found")
	}
	if permanent || !source.Capabilities.Trash {
		if info.IsDir() && b.containsForeignMount(source, full) {
			return fileError("PATH_FORBIDDEN", "recursive delete cannot cross a mounted filesystem")
		}
		if err := b.appendAuditEvent("files.permanent_delete", source.ID, cleanDisplayPath(clean), "intent", nil); err != nil {
			return err
		}
		if b.beforeRemove != nil {
			b.beforeRemove()
		}
		removeErr := removeAllInRoot(source.Root, clean)
		result := "success"
		if removeErr != nil {
			result = "failure"
		}
		auditErr := b.appendAuditEvent("files.permanent_delete", source.ID, cleanDisplayPath(clean), result, removeErr)
		if removeErr != nil {
			return fileError("IO_ERROR", "permanent delete: %v", removeErr)
		}
		return auditErr
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
	if err := rootMkdirAll(root, ".trash/files", 0o700); err != nil {
		return fileError("IO_ERROR", "create trash: %v", err)
	}
	destination := filepath.Join(".trash", "files", id)
	if err := renameNoReplace(source.Root, clean, destination); err != nil {
		return renameError("move to trash", err)
	}
	entry := TrashEntry{ID: id, Source: source.ID, Name: filepath.Base(clean), OriginalPath: filepath.ToSlash(clean), DeletedAt: b.now().UTC(), IsDir: info.IsDir(), Size: info.Size()}
	index.Entries = append([]TrashEntry{entry}, index.Entries...)
	if err := b.saveTrashLocked(index); err != nil {
		_ = renameNoReplace(source.Root, destination, clean)
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
		if entry.PendingPurge {
			continue
		}
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
	_, destinationClean, err := b.resolve(source, clean, true)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(source.Root)
	if err != nil {
		return fileError("SOURCE_UNAVAILABLE", "open source root: %v", err)
	}
	defer root.Close()
	storedClean := filepath.Join(".trash", "files", id)
	if _, err := root.Lstat(rootPath(storedClean)); err != nil {
		return fileError("TRASH_NOT_FOUND", "trash content not found")
	}
	if b.beforeRename != nil {
		b.beforeRename()
	}
	if err := renameNoReplace(source.Root, storedClean, destinationClean); err != nil {
		return renameError("restore trash", err)
	}
	index.Entries = append(index.Entries[:position], index.Entries[position+1:]...)
	if err := b.saveTrashLocked(index); err != nil {
		_ = renameNoReplace(source.Root, destinationClean, storedClean)
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
	root, err := os.OpenRoot(source.Root)
	if err != nil {
		return fileError("SOURCE_UNAVAILABLE", "open source root: %v", err)
	}
	defer root.Close()
	current := ""
	for _, part := range strings.Split(clean, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, statErr := root.Lstat(rootPath(current))
		if os.IsNotExist(statErr) {
			if err := root.Mkdir(rootPath(current), 0o755); err != nil {
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
	if !entry.PendingPurge {
		index.Entries[position].PendingPurge = true
		if err := b.saveTrashLocked(index); err != nil {
			return err
		}
	}
	root, err := os.OpenRoot(b.rootDir)
	if err != nil {
		return fileError("SOURCE_UNAVAILABLE", "open trash root: %v", err)
	}
	defer root.Close()
	if b.beforeRemove != nil {
		b.beforeRemove()
	}
	removeErr := removeAllInRoot(b.rootDir, filepath.ToSlash(filepath.Join(".trash", "files", id)))
	result := "success"
	if removeErr != nil {
		result = "failure"
	}
	auditErr := b.appendAuditEvent("files.trash_purge", entry.Source, entry.OriginalPath, result, removeErr)
	if removeErr != nil {
		return fileError("IO_ERROR", "purge trash: %v", removeErr)
	}
	index.Entries = append(index.Entries[:position], index.Entries[position+1:]...)
	if err := b.saveTrashLocked(index); err != nil {
		return err
	}
	return auditErr
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
	for i := range index.Entries {
		index.Entries[i].PendingPurge = true
	}
	if err := b.saveTrashLocked(index); err != nil {
		return err
	}
	root, err := os.OpenRoot(b.rootDir)
	if err != nil {
		return fileError("SOURCE_UNAVAILABLE", "open trash root: %v", err)
	}
	defer root.Close()
	if b.beforeRemove != nil {
		b.beforeRemove()
	}
	var removeErr error
	for _, entry := range index.Entries {
		if err := removeAllInRoot(b.rootDir, filepath.ToSlash(filepath.Join(".trash", "files", entry.ID))); err != nil {
			removeErr = err
			break
		}
	}
	result := "success"
	if removeErr != nil {
		result = "failure"
	}
	auditErr := b.appendAuditEvent("files.trash_empty", "my", "", result, removeErr)
	if removeErr != nil {
		return fileError("IO_ERROR", "empty trash: %v", removeErr)
	}
	index.Entries = nil
	if err := b.saveTrashLocked(index); err != nil {
		return err
	}
	return auditErr
}
