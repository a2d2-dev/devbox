// Package links 加载和维护"服务导航"清单（原 tkeel-links 静态页替代品）。
// 数据源：YAML 文件（默认 /etc/devbox/links.yaml，路径由 Config.LinksPath 覆盖）。
// 后端仅负责读文件 + 结构化，不判断内容合法性——链接是不是活的、端口是不是通，
// 都留给前端或 supervisor 关联层做。
package links

import (
	"fmt"
	"os"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// Registry 一份可热重载的清单。
type Registry struct {
	path string

	mu       sync.RWMutex
	sections []Section
	loadedAt time.Time
	loadErr  string // 最近一次加载失败的原因；成功时为空
}

type Section struct {
	Title       string `yaml:"title" json:"title"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
	// Kind 特殊标记："supervisor" 时前端会用 items[].supervisor 字段
	// 拉本机 supervisor 状态覆盖 items[].badge。空 = 静态显示。
	Kind  string `yaml:"kind,omitempty" json:"kind,omitempty"`
	Items []Item `yaml:"items" json:"items"`
}

type Item struct {
	Name       string `yaml:"name" json:"name"`
	URL        string `yaml:"url,omitempty" json:"url,omitempty"`
	Project    string `yaml:"project,omitempty" json:"project,omitempty"`
	Supervisor string `yaml:"supervisor,omitempty" json:"supervisor,omitempty"` // 关联的 supervisor 程序名
	Badge      string `yaml:"badge,omitempty" json:"badge,omitempty"`
	BadgeKind  string `yaml:"badgeKind,omitempty" json:"badgeKind,omitempty"` // ok / warn / muted，前端着色用
	Note       string `yaml:"note,omitempty" json:"note,omitempty"`
}

// New 从指定路径读取清单。path 空时用默认位置。
func New(path string) *Registry {
	if path == "" {
		path = "/etc/devbox/links.yaml"
	}
	r := &Registry{path: path}
	r.Reload()
	return r
}

// Reload 强制重新读取磁盘文件。任何时刻都可以调用，读失败会保留旧 sections。
func (r *Registry) Reload() {
	sections, err := loadYAML(r.path)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.loadedAt = time.Now()
	if err != nil {
		r.loadErr = err.Error()
		// 文件不存在不覆盖 sections，直接返回空清单，避免"文件写错 = 页面清零"
		if os.IsNotExist(err) {
			r.sections = nil
			r.loadErr = fmt.Sprintf("links file not found: %s", r.path)
		}
		return
	}
	r.loadErr = ""
	r.sections = sections
}

// Snapshot 返回当前清单的只读拷贝（含元数据）。
type Snapshot struct {
	Sections []Section `json:"sections"`
	LoadedAt time.Time `json:"loadedAt"`
	Path     string    `json:"path"`
	Error    string    `json:"error,omitempty"`
}

func (r *Registry) Snapshot() Snapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := Snapshot{
		Sections: make([]Section, len(r.sections)),
		LoadedAt: r.loadedAt,
		Path:     r.path,
		Error:    r.loadErr,
	}
	copy(out.Sections, r.sections)
	return out
}

func loadYAML(path string) ([]Section, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc struct {
		Sections []Section `yaml:"sections"`
	}
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("parse yaml: %w", err)
	}
	return doc.Sections, nil
}
