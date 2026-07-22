package apps

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// --- HTTP catalog ---

const manifestJSON = `{
  "apiVersion": "devbox/v1",
  "name": "Test Catalog",
  "apps": [
    {"id":"inline-app","name":"Inline","version":"1.0.0","composeTemplate":"services:\n  web:\n    image: nginx:1.27\n"},
    {"id":"file-app","name":"File","version":"2.0.0","compose":"ghost/compose.yaml",
     "valuesSchema":{"version":"v1","fields":[{"key":"tag","type":"text","required":true}]}}
  ]
}`

func newHTTPTestCatalog(t *testing.T) (Catalog, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/catalog.json":
			fmt.Fprint(w, manifestJSON)
		case r.URL.Path == "/ghost/compose.yaml":
			fmt.Fprint(w, "services:\n  web:\n    image: ghost:{{ .tag }}\n")
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	cat, err := NewHTTPCatalog(CatalogSource{Kind: "http", URL: srv.URL, Name: "test"})
	require.NoError(t, err)
	return cat, srv
}

func TestHTTPCatalog_RefreshAndSnapshot(t *testing.T) {
	cat, _ := newHTTPTestCatalog(t)
	require.NoError(t, cat.Refresh(context.Background()))

	snap := cat.Snapshot()
	assert.Equal(t, CatalogStateOK, snap.Status.State)
	assert.Equal(t, 2, snap.Status.AppCount)
	assert.Len(t, snap.Apps, 2)

	// 列表带 source 元数据 + runtimes。
	var fileApp *StoreApp
	for i := range snap.Apps {
		if snap.Apps[i].ID == "file-app" {
			fileApp = &snap.Apps[i]
		}
	}
	require.NotNil(t, fileApp)
	assert.Equal(t, "test", fileApp.CatalogName)
	assert.Equal(t, []string{"compose"}, fileApp.Runtimes)
	assert.True(t, fileApp.Installable)
}

func TestHTTPCatalog_GetVersionInline(t *testing.T) {
	cat, _ := newHTTPTestCatalog(t)
	require.NoError(t, cat.Refresh(context.Background()))
	v, err := cat.GetVersion(context.Background(), "inline-app", "1.0.0")
	require.NoError(t, err)
	assert.Equal(t, RuntimeCompose, v.Runtime)
	assert.Contains(t, v.ComposeTemplate, "nginx:1.27")
}

func TestHTTPCatalog_GetVersionRelativeFile(t *testing.T) {
	cat, _ := newHTTPTestCatalog(t)
	require.NoError(t, cat.Refresh(context.Background()))
	v, err := cat.GetVersion(context.Background(), "file-app", "2.0.0")
	require.NoError(t, err)
	assert.Contains(t, v.ComposeTemplate, "ghost:")
	assert.NotEmpty(t, v.ValuesSchema, "valuesSchema passthrough")
}

// ComposeTemplate 必须 json:"-" 不回前端。
func TestStoreAppVersion_CatalogComposeNotSerialized(t *testing.T) {
	cat, _ := newHTTPTestCatalog(t)
	require.NoError(t, cat.Refresh(context.Background()))
	v, err := cat.GetVersion(context.Background(), "inline-app", "1.0.0")
	require.NoError(t, err)
	b, err := json.Marshal(v)
	require.NoError(t, err)
	assert.NotContains(t, string(b), "nginx:1.27", "compose template must not leak to JSON")
}

func TestHTTPCatalog_NotYetSynced(t *testing.T) {
	cat, _ := newHTTPTestCatalog(t)
	snap := cat.Snapshot()
	assert.Equal(t, CatalogStateError, snap.Status.State)
	assert.Empty(t, snap.Apps, "no apps before first successful refresh")
}

func TestHTTPCatalog_OversizedManifestRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(make([]byte, maxCatalogBytes+16))
	}))
	t.Cleanup(srv.Close)
	cat, err := NewHTTPCatalog(CatalogSource{Kind: "http", URL: srv.URL})
	require.NoError(t, err)
	err = cat.Refresh(context.Background())
	require.Error(t, err)
}

// --- CatalogSet：聚合 / 故障隔离 / 可信重取 / 离线缓存 ---

// fakeCatalogSrc 测试用 Catalog 实现（可控 snapshot/version）。
type fakeCatalogSrc struct {
	id, kind     string
	snapshot     CatalogSnapshot
	getVersion   func(appID, version string) (StoreAppVersion, error)
	refreshErr   error
	refreshCalls int
}

func (f *fakeCatalogSrc) ID() string   { return f.id }
func (f *fakeCatalogSrc) Kind() string { return f.kind }
func (f *fakeCatalogSrc) Refresh(context.Context) error {
	f.refreshCalls++
	return f.refreshErr
}
func (f *fakeCatalogSrc) Snapshot() CatalogSnapshot {
	// 模拟真实 catalog：refresh 失败 → error 快照（无 apps）；成功 → 好快照。
	if f.refreshErr != nil {
		return CatalogSnapshot{SourceID: f.id, Kind: f.kind,
			Status: CatalogStatus{State: CatalogStateError, Message: f.refreshErr.Error()}}
	}
	return f.snapshot
}
func (f *fakeCatalogSrc) GetVersion(_ context.Context, appID, version string) (StoreAppVersion, error) {
	if f.getVersion != nil {
		return f.getVersion(appID, version)
	}
	return StoreAppVersion{}, fmt.Errorf("not found")
}

func TestCatalogSet_SourceFailureIsolation(t *testing.T) {
	ok := &fakeCatalogSrc{
		id: "ok", kind: "http",
		snapshot: CatalogSnapshot{SourceID: "ok", Status: CatalogStatus{State: CatalogStateOK},
			Apps: []StoreApp{{ID: "a1", CatalogID: "ok"}}},
	}
	bad := &fakeCatalogSrc{
		id: "bad", kind: "http", refreshErr: fmt.Errorf("boom"),
		snapshot: CatalogSnapshot{SourceID: "bad", Status: CatalogStatus{State: CatalogStateOK},
			Apps: []StoreApp{{ID: "a2", CatalogID: "bad"}}},
	}
	cs := NewCatalogSet([]Catalog{ok, bad}, zap.NewNop())
	cs.RefreshAll(context.Background())

	// bad 刷新失败 → 状态 error，但其上次缓存的 app 仍不出现在合并列表（隔离）。
	apps := cs.ListApps()
	assert.Len(t, apps, 1)
	assert.Equal(t, "a1", apps[0].ID)

	statuses := cs.Statuses()
	var badStatus *CatalogSnapshot
	for i := range statuses {
		if statuses[i].SourceID == "bad" {
			badStatus = &statuses[i]
		}
	}
	require.NotNil(t, badStatus)
	assert.Equal(t, CatalogStateError, badStatus.Status.State)
}

func TestCatalogSet_OfflineKeepsLastGoodCache(t *testing.T) {
	// httpCatalog：首次成功后断网，ListApps 仍返回缓存，状态标 error。
	cat, srv := newHTTPTestCatalog(t)
	require.NoError(t, cat.Refresh(context.Background()))
	cs := NewCatalogSet([]Catalog{cat}, zap.NewNop())
	cs.RefreshAll(context.Background())
	require.Len(t, cs.ListApps(), 2)

	srv.Close() // 模拟 catalog 不可用
	cs.RefreshAll(context.Background())

	apps := cs.ListApps()
	assert.Len(t, apps, 2, "offline: last good cache still serves list")
	statuses := cs.Statuses()
	require.Len(t, statuses, 1)
	assert.Equal(t, CatalogStateError, statuses[0].Status.State)
}

func TestCatalogSet_GetVersionRoutingAndMetadata(t *testing.T) {
	cat := &fakeCatalogSrc{
		id: "src1", kind: "http",
		snapshot: CatalogSnapshot{SourceID: "src1", SourceName: "Source One", Status: CatalogStatus{State: CatalogStateOK}},
		getVersion: func(appID, version string) (StoreAppVersion, error) {
			return StoreAppVersion{AppID: appID, Version: version, Runtime: RuntimeCompose, ComposeTemplate: "x"}, nil
		},
	}
	cs := NewCatalogSet([]Catalog{cat}, zap.NewNop())
	cs.RefreshAll(context.Background())

	v, err := cs.GetVersion(context.Background(), "src1", "app", "1.0.0")
	require.NoError(t, err)
	assert.Equal(t, "src1", v.CatalogID)
	assert.Equal(t, "Source One", v.CatalogName, "CatalogSet 注入 source 名称")
}

func TestCatalogSet_GetVersionUnknownSource(t *testing.T) {
	cs := NewCatalogSet(nil, zap.NewNop())
	_, err := cs.GetVersion(context.Background(), "nope", "app", "1.0.0")
	require.Error(t, err)
}

// 两个 source 出现同 appID：来源隔离，互不覆盖（按 CatalogID 区分）。
func TestCatalogSet_DuplicateAppIDAcrossSources(t *testing.T) {
	s1 := &fakeCatalogSrc{id: "s1", kind: "http", snapshot: CatalogSnapshot{
		SourceID: "s1", Status: CatalogStatus{State: CatalogStateOK},
		Apps: []StoreApp{{ID: "ghost", CatalogID: "s1"}}}}
	s2 := &fakeCatalogSrc{id: "s2", kind: "http", snapshot: CatalogSnapshot{
		SourceID: "s2", Status: CatalogStatus{State: CatalogStateOK},
		Apps: []StoreApp{{ID: "ghost", CatalogID: "s2"}}}}
	cs := NewCatalogSet([]Catalog{s1, s2}, zap.NewNop())
	cs.RefreshAll(context.Background())
	apps := cs.ListApps()
	assert.Len(t, apps, 2, "same appID from different sources both appear")
}

// --- Git URL 校验 ---

func TestValidateGitURL(t *testing.T) {
	cases := []struct {
		url string
		ok  bool
	}{
		{"https://github.com/owner/name", true},
		{"https://gitlab.com/owner/name", true},
		{"http://localhost/owner/name", true},
		{"http://127.0.0.1/owner/name", true},
		{"http://example.com/owner/name", false}, // 公网 http 拒绝
		{"file:///etc/passwd", false},
		{"ssh://git@github.com/owner/name", false},
		{"git@github.com:owner/name", false}, // 空 scheme
		{"owner/name", false},                // 裸 owner/name 拒绝
		{"", false},
	}
	for _, c := range cases {
		err := validateGitURL(c.url)
		if c.ok {
			assert.NoErrorf(t, err, "expected ok for %q", c.url)
		} else {
			assert.Errorf(t, err, "expected error for %q", c.url)
		}
	}
}

// --- safeReadCatalogFile：traversal / symlink / 绝对路径 / 大小 ---

func TestSafeReadCatalogFile(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "catalog.json"), []byte("{}"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "sub"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "sub", "compose.yaml"), []byte("ok"), 0o644))

	// 合法相对路径。
	b, err := safeReadCatalogFile(root, "catalog.json", maxCatalogFileBytes)
	require.NoError(t, err)
	assert.Equal(t, "{}", string(b))

	b, err = safeReadCatalogFile(root, "sub/compose.yaml", maxCatalogFileBytes)
	require.NoError(t, err)
	assert.Equal(t, "ok", string(b))

	// traversal 逃逸。
	_, err = safeReadCatalogFile(root, "../../../etc/passwd", maxCatalogFileBytes)
	assert.Error(t, err)

	// 绝对路径拒绝。
	_, err = safeReadCatalogFile(root, "/etc/passwd", maxCatalogFileBytes)
	assert.Error(t, err)

	// 空。
	_, err = safeReadCatalogFile(root, "  ", maxCatalogFileBytes)
	assert.Error(t, err)
}

func TestSafeReadCatalogFile_SymlinkEscape(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "catalog.json"), []byte("{}"), 0o644))
	// 仓外敏感文件（独立目录，确保在 root 之外）。
	outsideDir := t.TempDir()
	outside := filepath.Join(outsideDir, "secret.txt")
	require.NoError(t, os.WriteFile(outside, []byte("TOPSECRET"), 0o600))
	// 仓内 symlink 指向仓外。
	require.NoError(t, os.Symlink(outside, filepath.Join(root, "escape.yaml")))

	_, err := safeReadCatalogFile(root, "escape.yaml", maxCatalogFileBytes)
	require.Error(t, err, "symlink escaping root must be rejected")
}

func TestSafeReadCatalogFile_Oversized(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "big.yaml"), make([]byte, 10), 0o644))
	_, err := safeReadCatalogFile(root, "big.yaml", 4)
	require.Error(t, err)
}

func TestSafeReadCatalogFile_SymlinkWithinRoot(t *testing.T) {
	// 仓内 symlink 指向仓内文件：允许（不逃逸）。
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "real.yaml"), []byte("ok"), 0o644))
	require.NoError(t, os.Symlink("real.yaml", filepath.Join(root, "link.yaml")))
	b, err := safeReadCatalogFile(root, "link.yaml", maxCatalogFileBytes)
	require.NoError(t, err)
	assert.Equal(t, "ok", string(b))
}

// --- scrubToken / boundCloneSize ---

func TestScrubToken(t *testing.T) {
	// token 整体出现一次 → 替换为 ***。
	assert.Equal(t, "fatal: ***", scrubToken("fatal: abc def", "abc def"))
	assert.Equal(t, "no token", scrubToken("no token", ""))
	// token 作为子串也抹除。
	assert.Equal(t, "x***y", scrubToken("xSECRETy", "SECRET"))
}

func TestBoundCloneSize(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "a"), make([]byte, 50), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "b"), make([]byte, 60), 0o644))
	// 总 110 > 100 → 拒绝。
	require.Error(t, boundCloneSize(root, 100))
	// 总 110 <= 200 → ok。
	require.NoError(t, boundCloneSize(root, 200))
}

// --- manifest 解析：去重 / 空 id ---

func TestParseManifest_Dedup(t *testing.T) {
	body := `{"apiVersion":"devbox/v1","apps":[
		{"id":"a","version":"1"},
		{"id":"a","version":"2"},
		{"id":"","version":"3"},
		{"id":"b","version":"1"}
	]}`
	m, err := parseManifest([]byte(body))
	require.NoError(t, err)
	// a 去重为 1 条；空 id 剔除；b 保留。
	assert.Len(t, m.Apps, 2)
}

func TestParseManifest_BadAPIVersion(t *testing.T) {
	_, err := parseManifest([]byte(`{"apiVersion":"other/v2","apps":[]}`))
	require.Error(t, err)
}
