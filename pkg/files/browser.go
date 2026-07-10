package files

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Config 文件浏览配置
type Config struct {
	RootDir     string   `mapstructure:"root_dir"`
	AllowedDirs []string `mapstructure:"allowed_dirs"`
}

// FileEntry 文件/目录条目
type FileEntry struct {
	Name     string    `json:"name"`
	Type     string    `json:"type"` // "dir" | 文件扩展名
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
	IsDir    bool      `json:"isDir"`
	Count    int       `json:"count,omitempty"` // 目录内文件数
	// AbsPath 是解析后的绝对宿主路径，供前端「复制路径」直接用。
	// 已在 chroot 白名单内的目录里，不会 leak 越界路径。
	AbsPath string `json:"absPath"`
}

// Browser 文件浏览器
type Browser struct {
	rootDir     string
	allowedDirs []string
}

// NewBrowser 创建文件浏览器。
// 前端「工作区」= rootDir，chroot 语义。cfg.RootDir 缺省 /data；
// allowedDirs 缺省只包含 rootDir，越界直接 access denied。
func NewBrowser(cfg Config) *Browser {
	root := cfg.RootDir
	if root == "" {
		root = "/data"
	}
	root = filepath.Clean(root)
	allowed := cfg.AllowedDirs
	if len(allowed) == 0 {
		allowed = []string{root}
	}
	return &Browser{rootDir: root, allowedDirs: allowed}
}

// List 列出目录内容。
// 前端可能传相对路径 (如 "etc" / "etc/apt"，见 Files.jsx 里的 curPath 递进逻辑)，
// 这里统一按 rootDir 拼接绝对，避免落到进程 CWD 上。
func (b *Browser) List(path string) ([]FileEntry, error) {
	if path == "" {
		path = b.rootDir
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(b.rootDir, path)
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("invalid path: %w", err)
	}

	if !b.isAllowed(absPath) {
		return nil, fmt.Errorf("access denied: %s", absPath)
	}

	entries, err := os.ReadDir(absPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory: %w", err)
	}

	result := make([]FileEntry, 0, len(entries))
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		entry := FileEntry{
			Name:     e.Name(),
			IsDir:    e.IsDir(),
			Size:     info.Size(),
			Modified: info.ModTime(),
			AbsPath:  filepath.Join(absPath, e.Name()),
		}

		if e.IsDir() {
			entry.Type = "dir"
			if sub, err := os.ReadDir(filepath.Join(absPath, e.Name())); err == nil {
				entry.Count = len(sub)
			}
		} else {
			ext := filepath.Ext(e.Name())
			if ext != "" {
				entry.Type = strings.TrimPrefix(ext, ".")
			} else {
				entry.Type = "file"
			}
		}

		result = append(result, entry)
	}

	return result, nil
}

// Save 把 data 写到 dirPath/name 下。dirPath 为空时使用 rootDir，与 List 一致。
// 不覆盖已有文件：同名冲突时自动在扩展名前追加 -1 / -2 ... 直到不冲突。
// 返回实际落盘的文件名。
func (b *Browser) Save(dirPath, name string, data []byte) (string, error) {
	if dirPath == "" {
		dirPath = b.rootDir
	}
	if !filepath.IsAbs(dirPath) {
		dirPath = filepath.Join(b.rootDir, dirPath)
	}
	absDir, err := filepath.Abs(dirPath)
	if err != nil {
		return "", fmt.Errorf("invalid path: %w", err)
	}
	if !b.isAllowed(absDir) {
		return "", fmt.Errorf("access denied: %s", absDir)
	}

	info, err := os.Stat(absDir)
	if err != nil {
		return "", fmt.Errorf("failed to stat directory: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("not a directory: %s", absDir)
	}

	clean := filepath.Base(name)
	if clean == "" || clean == "." || clean == ".." || clean == "/" || strings.ContainsAny(clean, "/\\") {
		return "", fmt.Errorf("invalid filename")
	}

	// 冲突时自动加序号：foo.png → foo-1.png → foo-2.png ...
	// 用 O_CREATE|O_EXCL 打开做原子占位，避免并发粘贴撞车。
	ext := filepath.Ext(clean)
	stem := strings.TrimSuffix(clean, ext)
	final := clean
	var f *os.File
	for i := 0; i < 1000; i++ {
		full := filepath.Join(absDir, final)
		f, err = os.OpenFile(full, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
		if err == nil {
			break
		}
		if !os.IsExist(err) {
			return "", fmt.Errorf("failed to create file: %w", err)
		}
		final = fmt.Sprintf("%s-%d%s", stem, i+1, ext)
	}
	if f == nil {
		return "", fmt.Errorf("failed to find free filename")
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return "", fmt.Errorf("failed to write file: %w", err)
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("failed to close file: %w", err)
	}
	return final, nil
}

// ResolveFile 校验 dirPath+name 落在允许目录里且指向一个普通文件，返回绝对路径。
// 供 /files/content 直出用（http.ServeFile 会自动填 Content-Type / Range）。
func (b *Browser) ResolveFile(dirPath, name string) (string, error) {
	if dirPath == "" {
		dirPath = b.rootDir
	}
	if !filepath.IsAbs(dirPath) {
		dirPath = filepath.Join(b.rootDir, dirPath)
	}
	absDir, err := filepath.Abs(dirPath)
	if err != nil {
		return "", fmt.Errorf("invalid path: %w", err)
	}
	if !b.isAllowed(absDir) {
		return "", fmt.Errorf("access denied: %s", absDir)
	}
	clean := filepath.Base(name)
	if clean == "" || clean == "." || clean == ".." || clean == "/" || strings.ContainsAny(clean, "/\\") {
		return "", fmt.Errorf("invalid filename")
	}
	full := filepath.Join(absDir, clean)
	info, err := os.Stat(full)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("file not found")
		}
		return "", fmt.Errorf("failed to stat file: %w", err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("not a regular file")
	}
	return full, nil
}

// isAllowed 收紧成 "等于白名单目录" 或 "以 <白名单>/ 开头"，
// 避免 /data 白名单误放行 /data-secret 这种同前缀目录。
func (b *Browser) isAllowed(path string) bool {
	for _, dir := range b.allowedDirs {
		if path == dir || strings.HasPrefix(path, dir+string(filepath.Separator)) {
			return true
		}
	}
	return false
}
