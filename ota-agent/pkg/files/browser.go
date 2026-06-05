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
}

// Browser 文件浏览器
type Browser struct {
	rootDir     string
	allowedDirs []string
}

// NewBrowser 创建文件浏览器
func NewBrowser(cfg Config) *Browser {
	root := cfg.RootDir
	if root == "" {
		root = "/"
	}
	allowed := cfg.AllowedDirs
	if len(allowed) == 0 {
		allowed = []string{"/"}  // 允许浏览所有目录，生产环境应限制
	}
	return &Browser{rootDir: root, allowedDirs: allowed}
}

// List 列出目录内容
func (b *Browser) List(path string) ([]FileEntry, error) {
	if path == "" {
		path = b.rootDir
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

func (b *Browser) isAllowed(path string) bool {
	for _, dir := range b.allowedDirs {
		if strings.HasPrefix(path, dir) {
			return true
		}
	}
	return false
}
