package console

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"sync"
	"time"
)

// browserStore 浏览器应用的书签 / 历史持久化存储。
// 数据源：单个 JSON 文件（默认 /etc/devbox/browser.json，路径由 Config.BrowserDataPath 覆盖）。
// 并发范式仿 pkg/links：RWMutex + 返回深拷贝；区别在于这里需要可写，落盘走原子 rename。
type browserStore struct {
	path       string
	maxHistory int

	mu        sync.RWMutex
	bookmarks []Bookmark
	history   []HistoryEntry
}

// Bookmark 一条书签。
type Bookmark struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	URL       string    `json:"url"`
	Icon      string    `json:"icon,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

// HistoryEntry 一条访问记录（最新在前）。
type HistoryEntry struct {
	URL       string    `json:"url"`
	Title     string    `json:"title"`
	VisitedAt time.Time `json:"visitedAt"`
}

type browserStoreDoc struct {
	Bookmarks []Bookmark      `json:"bookmarks"`
	History   []HistoryEntry `json:"history"`
}

// newBrowserStore 从指定路径加载书签/历史。path 空时用默认位置。
// 文件不存在或解析失败时返回空 store，不报错（首次使用是常态）。
func newBrowserStore(path string) *browserStore {
	if path == "" {
		path = "/etc/devbox/browser.json"
	}
	s := &browserStore{path: path, maxHistory: 200}
	s.reload()
	return s
}

func (s *browserStore) reload() {
	b, err := os.ReadFile(s.path)
	if err != nil {
		return // 文件不存在：留空，首次写入时创建
	}
	var doc browserStoreDoc
	if err := json.Unmarshal(b, &doc); err != nil {
		return // 损坏文件：不覆盖内存，等下次写操作纠正
	}
	s.mu.Lock()
	s.bookmarks = doc.Bookmarks
	if s.bookmarks == nil {
		s.bookmarks = []Bookmark{}
	}
	s.history = doc.History
	if s.history == nil {
		s.history = []HistoryEntry{}
	}
	s.mu.Unlock()
}

// saveLocked 把当前数据原子写入磁盘。调用方必须已持有（写）锁。
func (s *browserStore) saveLocked() error {
	doc := browserStoreDoc{Bookmarks: s.bookmarks, History: s.history}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// AddBookmark 新建书签，prepend 到列表头。返回新建的书签。
func (s *browserStore) AddBookmark(title, url string) (Bookmark, error) {
	bm := Bookmark{
		ID:        randID(),
		Title:     title,
		URL:       url,
		CreatedAt: time.Now(),
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bookmarks = append([]Bookmark{bm}, s.bookmarks...)
	return bm, s.saveLocked()
}

// RemoveBookmark 删除指定 ID 的书签。不存在视为成功。
func (s *browserStore) RemoveBookmark(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.bookmarks[:0]
	for _, b := range s.bookmarks {
		if b.ID != id {
			next = append(next, b)
		}
	}
	s.bookmarks = next
	return s.saveLocked()
}

// ListBookmarks 返回书签的只读拷贝。
func (s *browserStore) ListBookmarks() []Bookmark {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Bookmark, len(s.bookmarks))
	copy(out, s.bookmarks)
	return out
}

// AddHistory 记录一次访问：同 URL 去重并置顶 + 更新访问时间，超过 maxHistory 截断尾部。
func (s *browserStore) AddHistory(url, title string) error {
	entry := HistoryEntry{URL: url, Title: title, VisitedAt: time.Now()}
	s.mu.Lock()
	defer s.mu.Unlock()
	// 去重：移除已存在的同 URL 条目
	next := make([]HistoryEntry, 0, len(s.history)+1)
	for _, h := range s.history {
		if h.URL != url {
			next = append(next, h)
		}
	}
	next = append([]HistoryEntry{entry}, next...)
	if len(next) > s.maxHistory {
		next = next[:s.maxHistory]
	}
	s.history = next
	return s.saveLocked()
}

// ClearHistory 清空全部历史。
func (s *browserStore) ClearHistory() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.history = []HistoryEntry{}
	return s.saveLocked()
}

// ListHistory 返回历史记录的只读拷贝（最新在前）。
func (s *browserStore) ListHistory() []HistoryEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]HistoryEntry, len(s.history))
	copy(out, s.history)
	return out
}

// randID 生成 8 字节随机 hex 作为书签 ID。
func randID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		// rand.Read 极少失败；退化用时间戳兜底，保证非空
		return hex.EncodeToString([]byte(time.Now().Format("20060102150405")))[:16]
	}
	return hex.EncodeToString(b[:])
}
