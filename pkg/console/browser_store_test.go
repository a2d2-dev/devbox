package console

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func tmpStore(t *testing.T) (*browserStore, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "browser.json")
	return newBrowserStore(path), path
}

func TestAddBookmarkAndList(t *testing.T) {
	s, _ := tmpStore(t)
	bm, err := s.AddBookmark("Site", "http://example.com")
	if err != nil {
		t.Fatal(err)
	}
	if bm.ID == "" || bm.URL != "http://example.com" {
		t.Errorf("unexpected bookmark: %+v", bm)
	}
	list := s.ListBookmarks()
	if len(list) != 1 || list[0].URL != "http://example.com" {
		t.Errorf("ListBookmarks = %+v", list)
	}
}

func TestRemoveBookmark(t *testing.T) {
	s, _ := tmpStore(t)
	bm, _ := s.AddBookmark("A", "http://a.com")
	s.AddBookmark("B", "http://b.com")
	if err := s.RemoveBookmark(bm.ID); err != nil {
		t.Fatal(err)
	}
	list := s.ListBookmarks()
	if len(list) != 1 || list[0].URL != "http://b.com" {
		t.Errorf("after remove: %+v", list)
	}
}

func TestAddHistoryDedupAndOrder(t *testing.T) {
	s, _ := tmpStore(t)
	s.AddHistory("http://a.com", "A")
	s.AddHistory("http://b.com", "B")
	// 再次访问 a → 应置顶且只剩 2 条
	s.AddHistory("http://a.com", "A2")
	h := s.ListHistory()
	if len(h) != 2 {
		t.Fatalf("want 2 entries, got %d: %+v", len(h), h)
	}
	if h[0].URL != "http://a.com" || h[0].Title != "A2" {
		t.Errorf("top should be a.com/A2, got %+v", h[0])
	}
	if h[1].URL != "http://b.com" {
		t.Errorf("second should be b.com, got %+v", h[1])
	}
}

func TestMaxHistoryTruncation(t *testing.T) {
	s, _ := tmpStore(t)
	s.maxHistory = 3
	for i := 0; i < 5; i++ {
		s.AddHistory("http://x"+string(rune('a'+i))+".com", "")
	}
	h := s.ListHistory()
	if len(h) != 3 {
		t.Fatalf("want 3 (truncated), got %d", len(h))
	}
}

func TestClearHistory(t *testing.T) {
	s, _ := tmpStore(t)
	s.AddHistory("http://a.com", "A")
	if err := s.ClearHistory(); err != nil {
		t.Fatal(err)
	}
	if len(s.ListHistory()) != 0 {
		t.Error("history not cleared")
	}
}

func TestAtomicWriteConcurrent(t *testing.T) {
	s, path := tmpStore(t)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s.AddHistory("http://c"+string(rune('a'+i%10))+".com", "")
		}(i)
	}
	wg.Wait()

	// 重新加载，验证落盘的 JSON 能正确解析且无损坏
	s2 := newBrowserStore(path)
	if len(s2.ListHistory()) == 0 {
		t.Error("history lost after reload")
	}
	// 文件不是 .tmp 残留
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Error(".tmp leftover detected")
	}
}
