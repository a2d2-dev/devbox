package files

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	defaultRoot       = "/data"
	defaultAppsRoot   = "/var/lib/devbox/apps"
	defaultMountsFile = "/proc/mounts"
	maxSearchDepth    = 8
	maxSearchResults  = 200
	maxCopyEntries    = 10000
)

// Config controls the roots and persistence used by the file manager.
type Config struct {
	RootDir     string   `mapstructure:"root_dir"`
	AllowedDirs []string `mapstructure:"allowed_dirs"` // legacy additional configured roots
	AppsDir     string   `mapstructure:"apps_dir"`
	StateDir    string   `mapstructure:"state_dir"`
	MountsFile  string   `mapstructure:"mounts_file"`
}

// Error is a stable error contract for HTTP handlers and tests.
type Error struct {
	Code string
	Err  error
}

func (e *Error) Error() string { return e.Code + ": " + e.Err.Error() }
func (e *Error) Unwrap() error { return e.Err }

func fileError(code, format string, args ...any) error {
	return &Error{Code: code, Err: fmt.Errorf(format, args...)}
}

func ErrorCode(err error) string {
	var target *Error
	if errors.As(err, &target) {
		return target.Code
	}
	return "INTERNAL"
}

type Capabilities struct {
	Read     bool `json:"read"`
	Upload   bool `json:"upload"`
	Download bool `json:"download"`
	Rename   bool `json:"rename"`
	Move     bool `json:"move"`
	Copy     bool `json:"copy"`
	Mkdir    bool `json:"mkdir"`
	Delete   bool `json:"delete"`
	Trash    bool `json:"trash"`
	Share    bool `json:"share"`
	Favorite bool `json:"favorite"`
}

type Source struct {
	ID           string       `json:"id"`
	Name         string       `json:"name"`
	Kind         string       `json:"kind"`
	Available    bool         `json:"available"`
	Reason       string       `json:"reason,omitempty"`
	Root         string       `json:"root,omitempty"`
	Capabilities Capabilities `json:"capabilities"`
}

type FileEntry struct {
	Name     string    `json:"name"`
	Path     string    `json:"path"`
	Source   string    `json:"source"`
	Type     string    `json:"type"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
	IsDir    bool      `json:"isDir"`
	Count    int       `json:"count,omitempty"`
	AbsPath  string    `json:"absPath"`
}

type Browser struct {
	rootDir     string
	allowedDirs []string
	appsDir     string
	stateDir    string
	mountsFile  string
	mu          sync.Mutex
	now         func() time.Time
}

func NewBrowser(cfg Config) *Browser {
	root := cleanRoot(cfg.RootDir, defaultRoot)
	apps := cleanRoot(cfg.AppsDir, defaultAppsRoot)
	state := cfg.StateDir
	if state == "" {
		state = filepath.Join(root, ".devbox-files")
	}
	mounts := cfg.MountsFile
	if mounts == "" {
		mounts = defaultMountsFile
	}
	allowed := make([]string, 0, len(cfg.AllowedDirs))
	for _, dir := range cfg.AllowedDirs {
		if dir = strings.TrimSpace(dir); dir != "" {
			allowed = append(allowed, cleanRoot(dir, dir))
		}
	}
	return &Browser{rootDir: root, allowedDirs: allowed, appsDir: apps, stateDir: filepath.Clean(state), mountsFile: mounts, now: time.Now}
}

func cleanRoot(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		value = fallback
	}
	abs, err := filepath.Abs(filepath.Clean(value))
	if err != nil {
		return filepath.Clean(value)
	}
	return abs
}

func readWriteCapabilities(trash bool) Capabilities {
	return Capabilities{Read: true, Upload: true, Download: true, Rename: true, Move: true, Copy: true, Mkdir: true, Delete: true, Trash: trash, Share: true, Favorite: true}
}

func readOnlyCapabilities() Capabilities {
	return Capabilities{Read: true, Download: true, Favorite: true}
}

// Sources returns the configured roots plus currently mounted external/network filesystems.
func (b *Browser) Sources() []Source {
	sources := []Source{b.source("my", "我的文件", "personal", b.rootDir, readWriteCapabilities(true))}
	for i, dir := range b.allowedDirs {
		sources = append(sources, b.source(fmt.Sprintf("configured-%d", i+1), filepath.Base(dir), "configured", dir, readWriteCapabilities(false)))
	}
	sources = append(sources, b.source("apps", "应用文件", "applications", b.appsDir, readWriteCapabilities(false)))
	sources = append(sources, discoverMountSources(b.mountsFile, b.rootDir, b.appsDir)...)
	return sources
}

func (b *Browser) source(id, name, kind, root string, caps Capabilities) Source {
	s := Source{ID: id, Name: name, Kind: kind, Root: root, Capabilities: caps}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		s.Available = false
		s.Reason = "目录未配置或当前不可用"
		s.Capabilities = Capabilities{}
		return s
	}
	s.Available = true
	return s
}

func (b *Browser) getSource(id string) (Source, error) {
	if id == "" {
		id = "my"
	}
	for _, source := range b.Sources() {
		if source.ID == id {
			if !source.Available {
				return Source{}, fileError("SOURCE_UNAVAILABLE", "%s", source.Reason)
			}
			return source, nil
		}
	}
	return Source{}, fileError("SOURCE_NOT_FOUND", "unknown source")
}

// Source returns one source and its current capabilities.
func (b *Browser) Source(id string) (Source, error) { return b.getSource(id) }

func normalizeRelative(path string) (string, error) {
	if strings.ContainsRune(path, 0) || strings.Contains(path, "\\") || filepath.IsAbs(path) || filepath.VolumeName(path) != "" {
		return "", fileError("PATH_FORBIDDEN", "absolute or malformed path")
	}
	clean := filepath.Clean(path)
	if clean == "." {
		return "", nil
	}
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fileError("PATH_FORBIDDEN", "path traversal")
	}
	return clean, nil
}

// resolve rejects all symlink components. Besides preventing escapes, this gives mutating
// operations one consistent rule and avoids silently operating on a different filesystem tree.
func (b *Browser) resolve(source Source, rel string, allowMissingFinal bool) (string, string, error) {
	clean, err := normalizeRelative(rel)
	if err != nil {
		return "", "", err
	}
	if filepath.Clean(source.Root) == filepath.Clean(b.rootDir) && isReservedPath(clean) {
		return "", "", fileError("PATH_FORBIDDEN", "file manager metadata is not directly accessible")
	}
	if source.Kind == "applications" && containsPathPart(clean, ".env") {
		return "", "", fileError("PATH_FORBIDDEN", "application secret files are not accessible")
	}
	root, err := filepath.EvalSymlinks(source.Root)
	if err != nil {
		return "", "", fileError("SOURCE_UNAVAILABLE", "resolve source root: %v", err)
	}
	root, _ = filepath.Abs(root)
	full := filepath.Join(root, clean)
	if b.isForeignMount(source, full) {
		return "", "", fileError("PATH_FORBIDDEN", "mounted filesystem must be accessed through its own source")
	}
	parts := strings.Split(clean, string(filepath.Separator))
	current := root
	if clean != "" {
		for i, part := range parts {
			current = filepath.Join(current, part)
			info, statErr := os.Lstat(current)
			if statErr != nil {
				if os.IsNotExist(statErr) && allowMissingFinal && i == len(parts)-1 {
					break
				}
				if os.IsNotExist(statErr) {
					return "", "", fileError("PATH_NOT_FOUND", "path does not exist")
				}
				return "", "", fileError("IO_ERROR", "inspect path: %v", statErr)
			}
			if info.Mode()&os.ModeSymlink != 0 {
				return "", "", fileError("PATH_FORBIDDEN", "symlinks are not allowed")
			}
		}
	}
	within, err := filepath.Rel(root, full)
	if err != nil || within == ".." || strings.HasPrefix(within, ".."+string(filepath.Separator)) {
		return "", "", fileError("PATH_FORBIDDEN", "path escaped source root")
	}
	return full, clean, nil
}

func isReservedPath(rel string) bool {
	if rel == "" {
		return false
	}
	first := strings.Split(rel, string(filepath.Separator))[0]
	return first == ".trash" || first == ".devbox-files"
}

func containsPathPart(rel, wanted string) bool {
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if part == wanted {
			return true
		}
	}
	return false
}

func (b *Browser) List(path string) ([]FileEntry, error) {
	return b.ListSource("my", path, "name", "asc")
}

func (b *Browser) ListSource(sourceID, path, sortBy, order string) ([]FileEntry, error) {
	source, err := b.getSource(sourceID)
	if err != nil {
		return nil, err
	}
	full, clean, err := b.resolve(source, path, false)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(full)
	if err != nil {
		return nil, fileError("IO_ERROR", "read directory: %v", err)
	}
	foreignMounts := b.foreignMountRoots(source)
	result := make([]FileEntry, 0, len(entries))
	for _, entry := range entries {
		if source.ID == "my" && clean == "" && (entry.Name() == ".trash" || entry.Name() == ".devbox-files") {
			continue
		}
		if entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		if withinMountRoots(foreignMounts, filepath.Join(full, entry.Name())) {
			continue
		}
		if source.Kind == "applications" && entry.Name() == ".env" {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			continue
		}
		rel := filepath.ToSlash(filepath.Join(clean, entry.Name()))
		item := FileEntry{Name: entry.Name(), Path: rel, Source: source.ID, IsDir: entry.IsDir(), Size: info.Size(), Modified: info.ModTime(), AbsPath: filepath.Join(full, entry.Name())}
		if entry.IsDir() {
			item.Type = "dir"
			if children, readErr := os.ReadDir(filepath.Join(full, entry.Name())); readErr == nil {
				item.Count = len(children)
			}
		} else {
			item.Type = fileType(entry.Name())
		}
		result = append(result, item)
	}
	sortEntries(result, sortBy, order)
	return result, nil
}

func fileType(name string) string {
	ext := strings.TrimPrefix(filepath.Ext(name), ".")
	if ext == "" {
		return "file"
	}
	return strings.ToLower(ext)
}

func sortEntries(entries []FileEntry, sortBy, order string) {
	desc := strings.EqualFold(order, "desc")
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir
		}
		comparison := 0
		switch sortBy {
		case "size":
			if entries[i].Size < entries[j].Size {
				comparison = -1
			} else if entries[i].Size > entries[j].Size {
				comparison = 1
			}
		case "time":
			if entries[i].Modified.Before(entries[j].Modified) {
				comparison = -1
			} else if entries[i].Modified.After(entries[j].Modified) {
				comparison = 1
			}
		default:
			comparison = strings.Compare(strings.ToLower(entries[i].Name), strings.ToLower(entries[j].Name))
		}
		if desc {
			return comparison > 0
		}
		return comparison < 0
	})
}

func (b *Browser) Search(sourceID, path, query string) ([]FileEntry, error) {
	source, err := b.getSource(sourceID)
	if err != nil {
		return nil, err
	}
	base, baseRel, err := b.resolve(source, path, false)
	if err != nil {
		return nil, err
	}
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return []FileEntry{}, nil
	}
	results := make([]FileEntry, 0)
	foreignMounts := b.foreignMountRoots(source)
	err = filepath.WalkDir(base, func(current string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if current == base {
			return nil
		}
		relFromBase, _ := filepath.Rel(base, current)
		depth := len(strings.Split(relFromBase, string(filepath.Separator)))
		if depth > maxSearchDepth {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if withinMountRoots(foreignMounts, current) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if source.Kind == "applications" && entry.Name() == ".env" {
			return nil
		}
		if source.ID == "my" && (relFromBase == ".trash" || relFromBase == ".devbox-files") {
			return filepath.SkipDir
		}
		if !strings.Contains(strings.ToLower(entry.Name()), query) {
			return nil
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return nil
		}
		rel := filepath.ToSlash(filepath.Join(baseRel, relFromBase))
		item := FileEntry{Name: entry.Name(), Path: rel, Source: source.ID, IsDir: entry.IsDir(), Size: info.Size(), Modified: info.ModTime(), AbsPath: current}
		if entry.IsDir() {
			item.Type = "dir"
		} else {
			item.Type = fileType(entry.Name())
		}
		results = append(results, item)
		if len(results) >= maxSearchResults {
			return io.EOF
		}
		return nil
	})
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, fileError("IO_ERROR", "search: %v", err)
	}
	sortEntries(results, "name", "asc")
	return results, nil
}

func validName(name string) error {
	if name == "" || name == "." || name == ".." || filepath.Base(name) != name || strings.ContainsAny(name, "/\\\x00") {
		return fileError("INVALID_NAME", "invalid filename")
	}
	return nil
}

func (b *Browser) Save(dirPath, name string, data []byte) (string, error) {
	return b.SaveSource("my", dirPath, name, data)
}

func (b *Browser) SaveSource(sourceID, dirPath, name string, data []byte) (string, error) {
	source, err := b.require(sourceID, func(c Capabilities) bool { return c.Upload })
	if err != nil {
		return "", err
	}
	if err := validName(name); err != nil {
		return "", err
	}
	dir, _, err := b.resolve(source, dirPath, false)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return "", fileError("NOT_DIRECTORY", "target is not a directory")
	}
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	final := name
	for i := 0; i < 1000; i++ {
		full := filepath.Join(dir, final)
		f, openErr := os.OpenFile(full, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if openErr == nil {
			if _, writeErr := f.Write(data); writeErr != nil {
				f.Close()
				os.Remove(full)
				return "", fileError("IO_ERROR", "write file: %v", writeErr)
			}
			if closeErr := f.Close(); closeErr != nil {
				return "", fileError("IO_ERROR", "close file: %v", closeErr)
			}
			return final, nil
		}
		if !os.IsExist(openErr) {
			return "", fileError("IO_ERROR", "create file: %v", openErr)
		}
		final = fmt.Sprintf("%s-%d%s", stem, i+1, ext)
	}
	return "", fileError("CONFLICT", "no free filename")
}

func (b *Browser) ResolveFile(dirPath, name string) (string, error) {
	if err := validName(name); err != nil {
		return "", err
	}
	return b.ResolveDownload("my", filepath.ToSlash(filepath.Join(dirPath, name)))
}

func (b *Browser) ResolveDownload(sourceID, path string) (string, error) {
	source, err := b.require(sourceID, func(c Capabilities) bool { return c.Download })
	if err != nil {
		return "", err
	}
	full, clean, err := b.resolve(source, path, false)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(full)
	if err != nil {
		return "", fileError("PATH_NOT_FOUND", "file not found")
	}
	if !info.Mode().IsRegular() {
		return "", fileError("NOT_FILE", "not a regular file")
	}
	b.recordRecent(source.ID, clean, info)
	return full, nil
}

func (b *Browser) require(sourceID string, allowed func(Capabilities) bool) (Source, error) {
	source, err := b.getSource(sourceID)
	if err != nil {
		return Source{}, err
	}
	if !allowed(source.Capabilities) {
		return Source{}, fileError("OPERATION_UNSUPPORTED", "operation is not supported by this source")
	}
	return source, nil
}

func (b *Browser) Mkdir(sourceID, parent, name string) error {
	source, err := b.require(sourceID, func(c Capabilities) bool { return c.Mkdir })
	if err != nil {
		return err
	}
	if err := validName(name); err != nil {
		return err
	}
	dir, _, err := b.resolve(source, parent, false)
	if err != nil {
		return err
	}
	target := filepath.Join(dir, name)
	if err := os.Mkdir(target, 0o755); err != nil {
		if os.IsExist(err) {
			return fileError("CONFLICT", "path already exists")
		}
		return fileError("IO_ERROR", "create directory: %v", err)
	}
	return nil
}

func (b *Browser) Rename(sourceID, path, name string) error {
	source, err := b.require(sourceID, func(c Capabilities) bool { return c.Rename })
	if err != nil {
		return err
	}
	if err := validName(name); err != nil {
		return err
	}
	from, clean, err := b.resolve(source, path, false)
	if err != nil {
		return err
	}
	if clean == "" {
		return fileError("PATH_FORBIDDEN", "cannot rename source root")
	}
	toRel := filepath.Join(filepath.Dir(clean), name)
	to, _, err := b.resolve(source, toRel, true)
	if err != nil {
		return err
	}
	if _, statErr := os.Lstat(to); statErr == nil {
		return fileError("CONFLICT", "destination exists")
	}
	if err := os.Rename(from, to); err != nil {
		return fileError("IO_ERROR", "rename: %v", err)
	}
	return nil
}

func (b *Browser) Transfer(sourceID, path, destination string, copyOnly bool) error {
	capability := func(c Capabilities) bool {
		if copyOnly {
			return c.Copy
		}
		return c.Move
	}
	source, err := b.require(sourceID, capability)
	if err != nil {
		return err
	}
	from, clean, err := b.resolve(source, path, false)
	if err != nil {
		return err
	}
	if clean == "" {
		return fileError("PATH_FORBIDDEN", "cannot transfer source root")
	}
	destDir, destRel, err := b.resolve(source, destination, false)
	if err != nil {
		return err
	}
	info, err := os.Stat(destDir)
	if err != nil || !info.IsDir() {
		return fileError("NOT_DIRECTORY", "destination is not a directory")
	}
	fromInfo, err := os.Stat(from)
	if err != nil {
		return fileError("PATH_NOT_FOUND", "source path not found")
	}
	if fromInfo.IsDir() {
		rel, relErr := filepath.Rel(from, destDir)
		if relErr == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fileError("PATH_FORBIDDEN", "cannot transfer a directory into itself")
		}
	}
	to := filepath.Join(destDir, filepath.Base(from))
	if _, statErr := os.Lstat(to); statErr == nil {
		return fileError("CONFLICT", "destination exists")
	}
	if !copyOnly {
		if err := os.Rename(from, to); err != nil {
			return fileError("IO_ERROR", "move: %v", err)
		}
		return nil
	}
	_ = destRel
	if err := copyTree(from, to); err != nil {
		_ = os.RemoveAll(to)
		return err
	}
	return nil
}

func copyTree(from, to string) error {
	count := 0
	return filepath.WalkDir(from, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fileError("IO_ERROR", "copy walk: %v", walkErr)
		}
		count++
		if count > maxCopyEntries {
			return fileError("LIMIT_EXCEEDED", "copy exceeds %d entries", maxCopyEntries)
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fileError("PATH_FORBIDDEN", "symlinks are not allowed")
		}
		rel, _ := filepath.Rel(from, path)
		target := filepath.Join(to, rel)
		info, err := entry.Info()
		if err != nil {
			return fileError("IO_ERROR", "copy inspect: %v", err)
		}
		if entry.IsDir() {
			return os.Mkdir(target, info.Mode().Perm())
		}
		if !info.Mode().IsRegular() {
			return fileError("OPERATION_UNSUPPORTED", "copy only supports regular files and directories")
		}
		in, err := os.Open(path)
		if err != nil {
			return fileError("IO_ERROR", "copy open: %v", err)
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
		if err != nil {
			in.Close()
			return fileError("IO_ERROR", "copy create: %v", err)
		}
		_, copyErr := io.Copy(out, in)
		inCloseErr := in.Close()
		closeErr := out.Close()
		if copyErr != nil {
			return fileError("IO_ERROR", "copy data: %v", copyErr)
		}
		if inCloseErr != nil {
			return fileError("IO_ERROR", "copy source close: %v", inCloseErr)
		}
		if closeErr != nil {
			return fileError("IO_ERROR", "copy close: %v", closeErr)
		}
		return nil
	})
}

func randomID(bytes int) (string, error) {
	buf := make([]byte, bytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
