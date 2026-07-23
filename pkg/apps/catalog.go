package apps

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	pathpkg "path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

// 第三方 Docker Compose catalog source（Issue #2 阶段4 扩展，用户明确要求）。
//
// 与 edge-apiserver storeapps 协议并列：edge 商店走 StoreManager；第三方文件原生
// catalog 走本模块。两者最终都经 Controller.Apply / Task / Revision 落地，安装复制
// compose 到 devbox 托管目录后即与普通 Compose 应用同构。
//
// 设计目标（Issue #2 + 用户约束）：
//   - 文件原生 manifest（简单稳定）：一个 catalog.json 列出若干 app，每个 app 声明
//     版本/compose 模板（内联或相对路径）/可选 valuesSchema。模板沿用 store 的
//     {{ .param }} 与 ${secret} 约定，渲染复用 RenderStoreCompose。
//   - 多 catalog 配置 / 来源筛选 / 错误状态 / 缓存：CatalogSet 聚合多个 source，
//     每个 app 带 CatalogID/CatalogName；单 source 不可用只影响该 source（状态标
//     error），不影响其它 source 与已安装应用（用上次可信缓存）。
//   - 两类 source：
//       · httpCatalog：按 HTTPS base URL 逐文件取（catalog.json + 相对 compose）。
//       · gitCatalog：受控 Git HTTP(S) shallow clone（exec.CommandContext 参数数组、
//         无 shell、--depth 1、固定超时/输出/总大小、拒绝 file/ssh/local、token 脱敏、
//         相对路径防 traversal/symlink）。见下方 git 引擎。
//   - 安全：URL 仅 https（http 仅 localhost/127.0.0.1/.test/.internal 或显式 insecure）；
//     manifest/响应/模板/图标/错误体均有限长；只读 token 视为 secret，绝不入
//     日志/审计/revision。模板渲染不注册 FuncMap（store_render 已保证），第三方模板
//     无法 shell / 读文件 / 调任意函数。
//   - 不做 K8s→Compose 自动转换：catalog 仅产出 compose 包。
//
// 不是 GitOps 自动同步：git source 仅按显式 refresh 刷新缓存，不触发已装应用 reconcile。

// --- 配置 ---

// CatalogSource 描述一个第三方 catalog source 的构造参数。
type CatalogSource struct {
	ID   string // 稳定标识（来源筛选与 install 路由）；为空时按 URL 派生
	Name string // 人读来源名（UI）；为空时用 ID
	Kind string // "http" | "git"
	// URL：http = manifest 所在 base URL（取 <base>/catalog.json）；git = 完整 https
	// 仓库地址（如 https://github.com/owner/name）；拒绝 file/ssh/local/裸 owner/name。
	URL   string
	Ref   string // git: 分支/tag（默认 main）
	Path  string // git: 仓库内子目录（默认根）；catalog.json 与相对 compose 均相对此目录
	Token string // 可选只读 token（secret，不入日志/审计；git 经 http.extraHeader 注入）
	// Platform/Host 为兼容保留（git clone 不依赖；URL 已含 host）。
	Platform string
	Host     string
	Insecure bool // 允许 http（仅 localhost 类地址才生效；见 validateSourceURL）
}

// --- 状态 ---

// CatalogState 单个 source 的健康状态。
type CatalogState string

const (
	CatalogStateOK           CatalogState = "ok"
	CatalogStateError        CatalogState = "error"
	CatalogStateSyncing      CatalogState = "syncing"
	CatalogStateUnconfigured CatalogState = "unconfigured"
)

// CatalogStatus 单个 source 的同步状态（UI 展示用，不含敏感信息）。
type CatalogStatus struct {
	State     CatalogState `json:"state"`
	Message   string       `json:"message,omitempty"` // 人读错误摘要（已脱敏/裁剪）
	AppCount  int          `json:"appCount"`
	FetchedAt *time.Time   `json:"fetchedAt,omitempty"`
}

// CatalogSnapshot 单个 source 的一次快照（缓存）。
type CatalogSnapshot struct {
	SourceID   string        `json:"sourceId"`
	SourceName string        `json:"sourceName"`
	Kind       string        `json:"kind"`
	Status     CatalogStatus `json:"status"`
	Apps       []StoreApp    `json:"apps"`
}

// Catalog 单个 catalog source 的 seam（CatalogSet 聚合多个实现）。
type Catalog interface {
	ID() string
	Kind() string
	// Refresh 拉取并缓存 manifest；失败返回 error（由 CatalogSet 隔离记录状态）。
	Refresh(ctx context.Context) error
	// Snapshot 返回当前缓存（可能为空，状态反映是否曾成功）。
	Snapshot() CatalogSnapshot
	// GetVersion 取某 app 指定版本（含 compose 模板，json:"-" 不回前端）。
	GetVersion(ctx context.Context, appID, version string) (StoreAppVersion, error)
}

// --- manifest ---

// catalogManifest devbox catalog v1 清单文件。
//
// 示例：
//
//	{
//	  "apiVersion": "devbox/v1",
//	  "name": "My Catalog",
//	  "apps": [
//	    {"id":"ghost","name":"Ghost","version":"5.90.0",
//	     "compose":"ghost/compose.yaml",
//	     "valuesSchema":{"version":"v1","fields":[{"key":"tag","type":"text"}]}}
//	  ]
//	}
type catalogManifest struct {
	APIVersion string         `json:"apiVersion"`
	Name       string         `json:"name"`
	Apps       []catalogEntry `json:"apps"`
}

// catalogEntry manifest 内单个 app（可带多版本历史）。
type catalogEntry struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description,omitempty"`
	Icon        string `json:"icon,omitempty"`
	Category    string `json:"category,omitempty"`
	Provider    string `json:"provider,omitempty"`
	Pinned      bool   `json:"pinned,omitempty"`
	// Compose 模板来源二选一：ComposeTemplate 内联；Compose 为相对 manifest 的路径
	// （由 source 解析为 URL 拉取）。两者都空则该版本不可安装。
	ComposeTemplate string                     `json:"composeTemplate,omitempty"`
	Compose         string                     `json:"compose,omitempty"`
	ValuesSchema    json.RawMessage            `json:"valuesSchema,omitempty"`
	DefaultValues   map[string]json.RawMessage `json:"defaultValues,omitempty"`
	// Versions 可选多版本历史（同结构）。
	Versions []catalogEntry `json:"versions,omitempty"`
}

// --- 共享 HTTP 取数（http / git 复用）---

// catalogFetcher 受限的 HTTPS 取数器（bounded read + 可选只读 token + 超时）。
type catalogFetcher struct {
	client *http.Client
	token  string
}

func newCatalogFetcher(token string) *catalogFetcher {
	return &catalogFetcher{
		client: &http.Client{
			Timeout: 15 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) == 0 {
					return nil
				}
				origin := via[0].URL
				if req.URL.Scheme != origin.Scheme || req.URL.Host != origin.Host {
					return fmt.Errorf("catalog redirect 禁止跨 origin")
				}
				return nil
			},
		},
		token: token,
	}
}

// fetchText 取 URL 文本，受 maxCatalogBytes 限制；错误体裁剪，绝不回显 token。
func (f *catalogFetcher) fetchText(ctx context.Context, u string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	if f.token != "" {
		req.Header.Set("Authorization", "Bearer "+f.token)
	}
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := readLimited(resp.Body, maxCatalogBytes)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		snippet := scrubToken(string(body), f.token)
		if len(snippet) > 256 {
			snippet = snippet[:256]
		}
		return nil, fmt.Errorf("catalog %s returned %d: %s", redactURL(u), resp.StatusCode, snippet)
	}
	return body, nil
}

// redactURL 去掉 URL 的 query（可能含 token），仅保留 scheme/host/path。
func redactURL(u string) string {
	p, err := url.Parse(u)
	if err != nil {
		return u
	}
	p.RawQuery = ""
	p.Fragment = ""
	p.User = nil
	return p.String()
}

// --- baseCatalog：http / git 共享的 manifest 缓存与映射逻辑 ---

// baseCatalog 持有 source 元数据、manifestURL、文件解析回调与缓存（httpCatalog 用）。
// gitCatalog 是独立的 clone-based 实现，不复用 baseCatalog。
type baseCatalog struct {
	id     string
	name   string
	kind   string
	source CatalogSource

	fetcher     *catalogFetcher
	manifestURL string
	resolveFile func(rel string) (string, error) // 相对路径 → 可取 URL
	cacheDir    string
	refreshMu   sync.Mutex

	mu        sync.RWMutex
	manifest  *catalogManifest
	fetchedAt *time.Time
	lastErr   string
}

// validateSourceURL 校验 URL scheme：仅 https；http 仅允许 localhost 类地址或显式 insecure。
func validateSourceURL(raw string, insecure bool) error {
	p, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid url: %w", err)
	}
	if p.Hostname() == "" {
		return fmt.Errorf("catalog url 缺少 host")
	}
	if p.User != nil {
		return fmt.Errorf("catalog url 禁止 userinfo；请使用只读 token 配置")
	}
	if p.RawQuery != "" || p.Fragment != "" {
		return fmt.Errorf("catalog url 禁止 query/fragment；请使用 token 配置")
	}
	switch strings.ToLower(p.Scheme) {
	case "https":
		return nil
	case "http":
		host := strings.ToLower(p.Hostname())
		if insecure || host == "localhost" || host == "127.0.0.1" || strings.HasSuffix(host, ".localhost") ||
			strings.HasSuffix(host, ".test") || strings.HasSuffix(host, ".internal") {
			return nil
		}
		return fmt.Errorf("http catalog 仅允许 localhost/内网地址；公网须 https")
	default:
		return fmt.Errorf("unsupported scheme %q（仅 https）", p.Scheme)
	}
}

// parseManifest 解析并基础校验 manifest。
func parseManifest(body []byte) (*catalogManifest, error) {
	var m catalogManifest
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, fmt.Errorf("invalid manifest json: %w", err)
	}
	if strings.TrimSpace(m.APIVersion) != "" && !strings.HasPrefix(m.APIVersion, "devbox/") {
		return nil, fmt.Errorf("unsupported manifest apiVersion %q", m.APIVersion)
	}
	if len(m.Name) > maxCatalogNameBytes {
		return nil, fmt.Errorf("catalog name 超过 %d 字节上限", maxCatalogNameBytes)
	}
	if len(m.Apps) > maxCatalogApps {
		return nil, fmt.Errorf("catalog apps 超过 %d 条上限", maxCatalogApps)
	}
	// 空 id 剔除；重复 id 保留首项，兼容已有 catalog。
	seen := map[string]bool{}
	clean := m.Apps[:0]
	for _, a := range m.Apps {
		a.ID = strings.TrimSpace(a.ID)
		if a.ID == "" {
			continue
		}
		if err := ValidateAppID(a.ID); err != nil {
			return nil, fmt.Errorf("catalog app id: %w", err)
		}
		if seen[a.ID] {
			continue
		}
		if err := validateCatalogEntry(a); err != nil {
			return nil, fmt.Errorf("catalog app %q: %w", a.ID, err)
		}
		seen[a.ID] = true
		clean = append(clean, a)
	}
	m.Apps = clean
	return &m, nil
}

func validateCatalogEntry(e catalogEntry) error {
	if len(e.Name) > maxCatalogNameBytes || len(e.Provider) > maxCatalogNameBytes || len(e.Category) > maxCatalogNameBytes {
		return fmt.Errorf("name/provider/category 字段过长")
	}
	if len(e.Description) > maxCatalogDescriptionBytes {
		return fmt.Errorf("description 超过 %d 字节上限", maxCatalogDescriptionBytes)
	}
	if len(e.Icon) > maxCatalogIconBytes {
		return fmt.Errorf("icon 超过 %d 字节上限", maxCatalogIconBytes)
	}
	if len(e.Version) > maxCatalogVersionBytes {
		return fmt.Errorf("version 超过 %d 字节上限", maxCatalogVersionBytes)
	}
	if len(e.ComposeTemplate) > maxCatalogFileBytes {
		return fmt.Errorf("inline compose 超过 %d 字节上限", maxCatalogFileBytes)
	}
	if len(e.Versions) > maxCatalogVersions {
		return fmt.Errorf("versions 超过 %d 条上限", maxCatalogVersions)
	}
	seen := map[string]bool{}
	for _, v := range e.Versions {
		v.Version = strings.TrimSpace(v.Version)
		if v.Version == "" {
			return fmt.Errorf("versions 中存在空版本")
		}
		if seen[v.Version] {
			return fmt.Errorf("version %q 重复", v.Version)
		}
		seen[v.Version] = true
		if len(v.Versions) != 0 {
			return fmt.Errorf("versions 不允许继续嵌套")
		}
		if err := validateCatalogEntry(v); err != nil {
			return fmt.Errorf("version %q: %w", v.Version, err)
		}
	}
	return nil
}

func (c *baseCatalog) ID() string   { return c.id }
func (c *baseCatalog) Kind() string { return c.kind }

func (c *baseCatalog) Refresh(ctx context.Context) error {
	c.refreshMu.Lock()
	defer c.refreshMu.Unlock()

	body, err := c.fetcher.fetchText(ctx, c.manifestURL)
	if err != nil {
		c.mu.RLock()
		hasManifest := c.manifest != nil
		c.mu.RUnlock()
		if !hasManifest {
			_ = c.loadCachedManifest()
		}
		c.mu.Lock()
		c.lastErr = truncateMsg(err.Error(), 256)
		c.mu.Unlock()
		return err
	}
	m, err := parseManifest(body)
	if err != nil {
		c.mu.Lock()
		c.lastErr = truncateMsg(err.Error(), 256)
		c.mu.Unlock()
		return err
	}
	now := time.Now().UTC()
	if c.cacheDir != "" {
		if err := writeCatalogCacheFile(c.cacheDir, "catalog.json", body); err != nil {
			return fmt.Errorf("persist catalog cache: %w", err)
		}
	}
	c.mu.Lock()
	c.manifest = m
	c.fetchedAt = &now
	c.lastErr = ""
	c.mu.Unlock()
	return nil
}

func (c *baseCatalog) loadCachedManifest() error {
	if c.cacheDir == "" {
		return os.ErrNotExist
	}
	body, err := safeReadCatalogFile(c.cacheDir, "catalog.json", maxCatalogBytes)
	if err != nil {
		return err
	}
	m, err := parseManifest(body)
	if err != nil {
		return err
	}
	info, _ := os.Stat(filepath.Join(c.cacheDir, "catalog.json"))
	c.mu.Lock()
	c.manifest = m
	if info != nil {
		t := info.ModTime().UTC()
		c.fetchedAt = &t
	}
	c.mu.Unlock()
	return nil
}

func (c *baseCatalog) Snapshot() CatalogSnapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()
	snap := CatalogSnapshot{SourceID: c.id, SourceName: c.displayName(), Kind: c.kind}
	if c.manifest == nil {
		snap.Status = CatalogStatus{State: CatalogStateError, Message: orStr(c.lastErr, "尚未同步")}
		return snap
	}
	apps := make([]StoreApp, 0, len(c.manifest.Apps))
	for _, e := range c.manifest.Apps {
		apps = append(apps, c.entryToStoreApp(e))
	}
	st := CatalogStatus{State: CatalogStateOK, AppCount: len(apps), FetchedAt: c.fetchedAt}
	if c.lastErr != "" {
		st.State = CatalogStateError
		st.Message = c.lastErr
	}
	snap.Apps = apps
	snap.Status = st
	return snap
}

func (c *baseCatalog) displayName() string {
	if c.name != "" {
		return c.name
	}
	return c.id
}

// storeAppFromEntry manifest entry → 对外 StoreApp（compose-only；可安装性由 handler
// 按 Docker capability 覆盖）。http / git-clone source 共用此映射。
func storeAppFromEntry(srcID, srcName string, e catalogEntry) StoreApp {
	latest := latestCatalogEntry(e)
	return StoreApp{
		ID:           e.ID,
		Name:         orStr(strings.TrimSpace(e.Name), e.ID),
		Category:     e.Category,
		Version:      latest.Version,
		Description:  e.Description,
		Icon:         e.Icon,
		Provider:     e.Provider,
		VersionCount: versionCount(e),
		Runtime:      RuntimeCompose,
		Runtimes:     []string{string(RuntimeCompose)},
		Installable:  true, // Docker 可用性由 handler 用 capability 覆盖
		Pinned:       e.Pinned,
		CatalogID:    srcID,
		CatalogName:  srcName,
	}
}

func latestCatalogEntry(e catalogEntry) catalogEntry {
	best := e
	for _, candidate := range e.Versions {
		if best.Version == "" || compareVersionStrings(candidate.Version, best.Version) > 0 {
			best = mergeCatalogEntry(e, candidate)
		}
	}
	return best
}

// mergeCatalogEntry 允许 versions[] 仅覆盖版本相关字段，继承 app 级展示与参数元数据。
func mergeCatalogEntry(parent, child catalogEntry) catalogEntry {
	child.ID = orStr(strings.TrimSpace(child.ID), parent.ID)
	child.Name = orStr(strings.TrimSpace(child.Name), parent.Name)
	child.Description = orStr(strings.TrimSpace(child.Description), parent.Description)
	child.Icon = orStr(strings.TrimSpace(child.Icon), parent.Icon)
	child.Category = orStr(strings.TrimSpace(child.Category), parent.Category)
	child.Provider = orStr(strings.TrimSpace(child.Provider), parent.Provider)
	if len(child.ValuesSchema) == 0 {
		child.ValuesSchema = parent.ValuesSchema
	}
	if child.DefaultValues == nil {
		child.DefaultValues = parent.DefaultValues
	}
	return child
}

func (c *baseCatalog) entryToStoreApp(e catalogEntry) StoreApp {
	return storeAppFromEntry(c.id, c.displayName(), e)
}

func versionCount(e catalogEntry) int {
	if len(e.Versions) > 0 {
		count := len(e.Versions)
		if strings.TrimSpace(e.Version) != "" {
			for _, v := range e.Versions {
				if v.Version == e.Version {
					return count
				}
			}
			count++
		}
		return count
	}
	return 1
}

// findEntryInManifest 在 manifest 中查找 app（精确版本优先；空版本取 entry.Version）。
// 多版本：先在 entry.Versions 找精确；否则用 entry 本身。http / git-clone source 共用。
func findEntryInManifest(m *catalogManifest, appID, version string) (catalogEntry, bool) {
	if m == nil {
		return catalogEntry{}, false
	}
	want := strings.TrimSpace(version)
	for _, e := range m.Apps {
		if e.ID != appID {
			continue
		}
		if want == "" {
			return latestCatalogEntry(e), true
		}
		if e.Version == want {
			return e, true
		}
		for _, v := range e.Versions {
			if v.Version == want {
				return mergeCatalogEntry(e, v), true
			}
		}
	}
	return catalogEntry{}, false
}

// findEntry 在缓存 manifest 中查找 app（加锁）。
func (c *baseCatalog) findEntry(appID, version string) (catalogEntry, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return findEntryInManifest(c.manifest, appID, version)
}

// resolveCompose 取 entry 的 compose 模板（内联优先；否则按路径拉取）。
func (c *baseCatalog) resolveCompose(ctx context.Context, e catalogEntry) (string, error) {
	if strings.TrimSpace(e.ComposeTemplate) != "" {
		return e.ComposeTemplate, nil
	}
	p := strings.TrimSpace(e.Compose)
	if p == "" {
		return "", fmt.Errorf("应用 %q 未提供 compose 模板", e.ID)
	}
	fileURL, err := c.resolveFile(p)
	if err != nil {
		return "", err
	}
	body, err := c.fetcher.fetchText(ctx, fileURL)
	if err != nil {
		if c.cacheDir == "" {
			return "", err
		}
		cached, cacheErr := safeReadCatalogFile(c.cacheDir, cacheNameForRelative(p), maxCatalogFileBytes)
		if cacheErr != nil {
			return "", err
		}
		return string(cached), nil
	}
	if c.cacheDir != "" {
		if err := writeCatalogCacheFile(c.cacheDir, cacheNameForRelative(p), body); err != nil {
			return "", err
		}
	}
	return string(body), nil
}

func (c *baseCatalog) GetVersion(ctx context.Context, appID, version string) (StoreAppVersion, error) {
	e, ok := c.findEntry(appID, version)
	if !ok {
		return StoreAppVersion{}, fmt.Errorf("catalog %q 中未找到应用 %q", c.id, appID)
	}
	compose, err := c.resolveCompose(ctx, e)
	if err != nil {
		return StoreAppVersion{}, err
	}
	ver := StoreAppVersion{
		AppID:           appID,
		Version:         e.Version,
		Runtime:         RuntimeCompose,
		Installable:     true,
		ComposeTemplate: compose,
		ValuesSchema:    e.ValuesSchema,
		DefaultValues:   e.DefaultValues,
	}
	return ver, nil
}

// --- httpCatalog ---

// httpCatalog manifest 与 compose 均相对 base URL 取（<base>/catalog.json、<base>/<path>）。
type httpCatalog struct {
	*baseCatalog
}

// NewHTTPCatalog 构造 HTTP catalog source。manifestURL 为 catalog.json 完整 URL。
func NewHTTPCatalog(src CatalogSource) (Catalog, error) {
	return newHTTPCatalog(src, "")
}

func newHTTPCatalog(src CatalogSource, cacheRoot string) (Catalog, error) {
	base := strings.TrimRight(strings.TrimSpace(src.URL), "/")
	if base == "" {
		return nil, fmt.Errorf("http catalog 缺 url")
	}
	manifestURL := base + "/catalog.json"
	if err := validateSourceURL(manifestURL, src.Insecure); err != nil {
		return nil, err
	}
	bc := &baseCatalog{
		id:          sourceID(src, base),
		name:        src.Name,
		kind:        "http",
		source:      src,
		fetcher:     newCatalogFetcher(src.Token),
		manifestURL: manifestURL,
		resolveFile: func(rel string) (string, error) {
			return resolveHTTPRelative(base, rel, src.Insecure)
		},
	}
	if cacheRoot != "" {
		bc.cacheDir = filepath.Join(cacheRoot, "http-"+shortHash(bc.id))
		_ = bc.loadCachedManifest()
	}
	return &httpCatalog{baseCatalog: bc}, nil
}

func resolveHTTPRelative(base, rel string, insecure bool) (string, error) {
	raw := strings.TrimSpace(rel)
	if strings.ContainsAny(raw, "?#") {
		return "", fmt.Errorf("catalog file path 禁止 query/fragment: %q", rel)
	}
	clean := pathpkg.Clean(raw)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") || strings.Contains(clean, `\`) {
		return "", fmt.Errorf("catalog file path must stay under base: %q", rel)
	}
	baseURL, err := url.Parse(strings.TrimRight(base, "/") + "/")
	if err != nil {
		return "", err
	}
	ref, err := url.Parse(clean)
	if err != nil {
		return "", err
	}
	resolved := baseURL.ResolveReference(ref)
	if resolved.Scheme != baseURL.Scheme || resolved.Host != baseURL.Host || !strings.HasPrefix(resolved.EscapedPath(), baseURL.EscapedPath()) {
		return "", fmt.Errorf("catalog file escapes base: %q", rel)
	}
	if err := validateSourceURL(resolved.String(), insecure); err != nil {
		return "", err
	}
	return resolved.String(), nil
}

func cacheNameForRelative(rel string) string {
	h := sha256.Sum256([]byte(strings.TrimSpace(rel)))
	return filepath.Join("files", hex.EncodeToString(h[:]))
}

func writeCatalogCacheFile(root, rel string, data []byte) error {
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// --- gitCatalog：受控 Git HTTP(S) shallow clone（Issue #2 要求 2）---
//
// 与 httpCatalog 的「按 URL 逐文件取」不同：git source 把整个仓库 shallow clone 到一个
// 受管的稳定目录，manifest / compose 文件从本地读取。这样能用单一 git 协议拉取私有/多
// 文件仓库，且相对路径解析在受控 FS 边界内进行。
//
// 安全约束（要求 2）：
//   - 仅 https 仓库（拒绝 file/local/ssh/git scheme；http 仅 localhost/内网）。
//   - exec.CommandContext 参数数组，绝无 shell；--depth 1 --single-branch --no-tags。
//   - 固定超时（gitCloneTimeout）、固定输出上限（裁剪到 8KiB）、固定 clone 总大小上限
//     （maxCloneTotalBytes，walk 校验）。
//   - token 视为 secret：仅以 http.extraHeader 注入本次进程，绝不写 URL / 日志 / 审计；
//     所有 git 输出经 scrubToken 抹除 token 串。
//   - manifest 指向的相对 Compose 文件经 safeReadCatalogFile 读取：Clean + EvalSymlinks
//     + root 前缀校验，防 path traversal 与 symlink 逃逸。
//   - 原子刷新：clone 到临时兄弟目录，成功后 rename 替换稳定目录；失败保留上次可信
//     clone（满足「catalog 不可用 → 用上次缓存，不影响已安装应用」）。
//   - 不是 GitOps 自动同步：仅按显式 refresh 刷新缓存，不触发已装应用 reconcile。

const (
	maxCatalogNameBytes        = 128
	maxCatalogDescriptionBytes = 2048
	maxCatalogIconBytes        = 2048
	maxCatalogVersionBytes     = 128
	maxCatalogApps             = 2000
	maxCatalogVersions         = 200

	gitCloneTimeout     = 60 * time.Second // 单次 clone 超时
	maxCloneTotalBytes  = 64 << 20         // 64 MiB：clone 目录（含 .git）总大小上限
	maxCatalogFileBytes = 1 << 20          // 1 MiB：单个 catalog.json / compose 文件上限
	gitOutputCap        = 8 << 10          // 8 KiB：clone 输出（错误体）裁剪上限
)

// gitCatalog 把 Git 仓库 shallow clone 到受管目录后从本地读取。
type gitCatalog struct {
	id     string
	name   string
	source CatalogSource
	gitBin string

	cacheDir  string // 稳定 clone 目录（持久到下次成功 refresh）
	refreshMu sync.Mutex

	mu          sync.RWMutex
	manifest    *catalogManifest
	catalogRoot string // = cacheDir/<subdir>；安全读的 FS root
	fetchedAt   *time.Time
	lastErr     string
}

// NewGitCatalog 构造 Git clone catalog source。
//   - url：完整 https 仓库地址（如 https://github.com/owner/name）。
//   - ref：分支/tag（默认 main）。
//   - path：仓库内子目录（默认根）。
//   - cacheRoot：受管 clone 缓存根（通常 <dataDir>/catalog-cache）。
func NewGitCatalog(src CatalogSource, cacheRoot string) (Catalog, error) {
	u := strings.TrimSpace(src.URL)
	if u == "" {
		return nil, fmt.Errorf("git catalog 缺 url（须完整 https 仓库地址）")
	}
	if err := validateGitURL(u); err != nil {
		return nil, err
	}
	if _, err := cleanCatalogSubdir(src.Path); err != nil {
		return nil, err
	}
	id := sourceID(src, "git:"+u)
	var dir string
	if cacheRoot != "" {
		dir = filepath.Join(cacheRoot, "git-"+shortHash(id))
	} else {
		dir = filepath.Join(os.TempDir(), "devbox-catalog-git-"+shortHash(id))
	}
	c := &gitCatalog{
		id:       id,
		name:     src.Name,
		source:   src,
		gitBin:   "git",
		cacheDir: dir,
	}
	_ = recoverCatalogBackup(c.cacheDir)
	_ = c.loadCachedManifest()
	return c, nil
}

func (c *gitCatalog) ID() string   { return c.id }
func (c *gitCatalog) Kind() string { return "git" }

func (c *gitCatalog) displayName() string {
	if c.name != "" {
		return c.name
	}
	return c.id
}

func (c *gitCatalog) setErr(msg string) {
	c.mu.Lock()
	c.lastErr = truncateMsg(msg, 512)
	c.mu.Unlock()
}

// Refresh：clone 到临时兄弟目录 → 校验大小 → 读 manifest → 原子替换稳定目录。
// 任一步失败仅记录状态并返回 error，**不破坏**已缓存的 manifest 与稳定目录。
func (c *gitCatalog) refresh(ctx context.Context) error {
	c.refreshMu.Lock()
	defer c.refreshMu.Unlock()
	_ = recoverCatalogBackup(c.cacheDir)
	c.mu.RLock()
	hasManifest := c.manifest != nil
	c.mu.RUnlock()
	if !hasManifest {
		_ = c.loadCachedManifest()
	}

	cloneCtx, cancel := context.WithTimeout(ctx, gitCloneTimeout)
	defer cancel()

	parent := filepath.Dir(c.cacheDir)
	if err := os.MkdirAll(parent, 0o750); err != nil {
		c.setErr("create cache root: " + err.Error())
		return err
	}
	tmp, err := os.MkdirTemp(parent, filepath.Base(c.cacheDir)+".tmp-*")
	if err != nil {
		c.setErr("create clone temp dir: " + err.Error())
		return err
	}
	cleanup := func() { _ = os.RemoveAll(tmp) }

	out, gerr := gitClone(cloneCtx, c.gitBin, c.source.URL, c.source.Ref, tmp, c.source.Token)
	if gerr != nil {
		cleanup()
		// 保留旧稳定目录 + 旧 manifest（last good cache）；仅记录错误。
		msg := truncateMsg(scrubToken(out, c.source.Token), gitOutputCap)
		c.setErr("clone failed: " + msg)
		return fmt.Errorf("git clone %s: %w", redactURL(c.source.URL), gerr)
	}
	if err := boundCloneSize(tmp, maxCloneTotalBytes); err != nil {
		cleanup()
		c.setErr(err.Error())
		return err
	}
	sub, err := cleanCatalogSubdir(c.source.Path)
	if err != nil {
		cleanup()
		c.setErr(err.Error())
		return err
	}
	newRoot, err := catalogRootWithin(tmp, sub)
	if err != nil {
		cleanup()
		c.setErr(err.Error())
		return err
	}
	manifestBody, err := safeReadCatalogFile(newRoot, "catalog.json", maxCatalogFileBytes)
	if err != nil {
		cleanup()
		c.setErr("read catalog.json: " + err.Error())
		return err
	}
	m, err := parseManifest(manifestBody)
	if err != nil {
		cleanup()
		c.setErr(err.Error())
		return err
	}
	// 成功：原子替换稳定目录（同文件系统 rename）。
	if err := swapDir(tmp, c.cacheDir); err != nil {
		cleanup()
		c.setErr("swap clone dir: " + err.Error())
		return err
	}
	now := time.Now().UTC()
	c.mu.Lock()
	c.manifest = m
	c.catalogRoot = filepath.Join(c.cacheDir, sub)
	c.fetchedAt = &now
	c.lastErr = ""
	c.mu.Unlock()
	return nil
}

func (c *gitCatalog) loadCachedManifest() error {
	sub, err := cleanCatalogSubdir(c.source.Path)
	if err != nil {
		return err
	}
	root, err := catalogRootWithin(c.cacheDir, sub)
	if err != nil {
		return err
	}
	body, err := safeReadCatalogFile(root, "catalog.json", maxCatalogFileBytes)
	if err != nil {
		return err
	}
	m, err := parseManifest(body)
	if err != nil {
		return err
	}
	info, _ := os.Stat(filepath.Join(root, "catalog.json"))
	c.mu.Lock()
	c.manifest = m
	c.catalogRoot = root
	if info != nil {
		t := info.ModTime().UTC()
		c.fetchedAt = &t
	}
	c.mu.Unlock()
	return nil
}

func cleanCatalogSubdir(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if filepath.IsAbs(raw) || strings.Contains(raw, `\`) {
		return "", fmt.Errorf("catalog path 必须是仓库内相对目录")
	}
	clean := pathpkg.Clean(raw)
	if clean == ".." || strings.HasPrefix(clean, "../") || clean == "." {
		return "", fmt.Errorf("catalog path 逃逸仓库根目录")
	}
	return filepath.FromSlash(clean), nil
}

func catalogRootWithin(repoRoot, sub string) (string, error) {
	realRepo, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		return "", err
	}
	realRoot, err := filepath.EvalSymlinks(filepath.Join(realRepo, sub))
	if err != nil {
		return "", err
	}
	if realRoot != realRepo && !strings.HasPrefix(realRoot, realRepo+string(filepath.Separator)) {
		return "", fmt.Errorf("catalog path 通过 symlink 逃逸仓库根目录")
	}
	return realRoot, nil
}

func (c *gitCatalog) Refresh(ctx context.Context) error { return c.refresh(ctx) }

func (c *gitCatalog) Snapshot() CatalogSnapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()
	snap := CatalogSnapshot{SourceID: c.id, SourceName: c.displayName(), Kind: "git"}
	if c.manifest == nil {
		snap.Status = CatalogStatus{State: CatalogStateError, Message: orStr(c.lastErr, "尚未同步")}
		return snap
	}
	apps := make([]StoreApp, 0, len(c.manifest.Apps))
	for _, e := range c.manifest.Apps {
		apps = append(apps, storeAppFromEntry(c.id, c.displayName(), e))
	}
	st := CatalogStatus{State: CatalogStateOK, AppCount: len(apps), FetchedAt: c.fetchedAt}
	if c.lastErr != "" {
		st.State = CatalogStateError
		st.Message = c.lastErr
	}
	snap.Apps = apps
	snap.Status = st
	return snap
}

func (c *gitCatalog) GetVersion(ctx context.Context, appID, version string) (StoreAppVersion, error) {
	c.mu.RLock()
	manifest, root := c.manifest, c.catalogRoot
	c.mu.RUnlock()
	e, ok := findEntryInManifest(manifest, appID, version)
	if !ok {
		return StoreAppVersion{}, fmt.Errorf("catalog %q 中未找到应用 %q", c.id, appID)
	}
	compose := strings.TrimSpace(e.ComposeTemplate)
	if compose == "" {
		rel := strings.TrimSpace(e.Compose)
		if rel == "" {
			return StoreAppVersion{}, fmt.Errorf("应用 %q 未提供 compose 模板", e.ID)
		}
		data, err := safeReadCatalogFile(root, rel, maxCatalogFileBytes)
		if err != nil {
			return StoreAppVersion{}, fmt.Errorf("读取 compose %q 失败: %w", rel, err)
		}
		compose = string(data)
	}
	return StoreAppVersion{
		AppID:           appID,
		Version:         e.Version,
		Runtime:         RuntimeCompose,
		Installable:     true,
		ComposeTemplate: compose,
		ValuesSchema:    e.ValuesSchema,
		DefaultValues:   e.DefaultValues,
	}, nil
}

// --- git 引擎（exec clone + 受控读取）---

// validateGitURL 仅允许 https 仓库；http 仅 localhost/内网；其余（file/ssh/git/空）拒绝。
func validateGitURL(raw string) error {
	p, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid git url: %w", err)
	}
	if p.Hostname() == "" {
		return fmt.Errorf("git catalog url 缺少 host")
	}
	if p.User != nil {
		return fmt.Errorf("git catalog url 禁止 userinfo；请使用只读 token 配置")
	}
	if p.RawQuery != "" || p.Fragment != "" {
		return fmt.Errorf("git catalog url 禁止 query/fragment；请使用 token 配置")
	}
	switch strings.ToLower(p.Scheme) {
	case "https":
		return nil
	case "http":
		host := strings.ToLower(p.Hostname())
		if host == "localhost" || host == "127.0.0.1" || strings.HasSuffix(host, ".localhost") ||
			strings.HasSuffix(host, ".test") || strings.HasSuffix(host, ".internal") {
			return nil
		}
		return fmt.Errorf("http git 仓库仅允许 localhost/内网；公网须 https")
	case "":
		return fmt.Errorf("git catalog 须完整 https 仓库地址（拒绝裸 owner/name）")
	default:
		return fmt.Errorf("git catalog 仅支持 https 仓库（拒绝 scheme %q：file/ssh/git 等被禁）", p.Scheme)
	}
}

// gitClone 执行受限 shallow clone。参数数组、无 shell；token 仅经 extraHeader 注入，
// 不写 URL。输出经 capWriter 硬限到 gitOutputCap（避免恶意仓库用海量 clone 输出耗内存），
// 再脱敏 token，返回供错误展示。
func gitClone(ctx context.Context, gitBin, repoURL, ref, destDir, token string) (string, error) {
	if gitBin == "" {
		gitBin = "git"
	}
	args := []string{"-c", "credential.helper=", "-c", "core.hooksPath=/dev/null"}
	// 生产构造器只允许 HTTP(S)。本地路径仅由 clone 引擎测试直接构造使用。
	if parsed, _ := url.Parse(repoURL); parsed != nil && (parsed.Scheme == "http" || parsed.Scheme == "https") {
		args = append(args,
			"-c", "protocol.file.allow=never",
			"-c", "protocol.ssh.allow=never",
			"-c", "protocol.git.allow=never",
			"-c", "protocol.ext.allow=never",
		)
	}
	args = append(args, "clone", "--depth", "1", "--no-tags", "--single-branch")
	if ref != "" {
		args = append(args, "--branch", ref)
	}
	args = append(args, "--", repoURL, destDir)

	cmd := exec.CommandContext(ctx, gitBin, args...)
	cmd.Env = append(sanitizedGitEnv(os.Environ()),
		"GIT_TERMINAL_PROMPT=0", // 禁止交互提示挂起 clone
		"GIT_CONFIG_NOSYSTEM=1", // 忽略系统级 gitconfig
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_ATTR_NOSYSTEM=1",
	)
	if token != "" {
		// 用一次性进程环境注入 header，避免 token 出现在 argv / remote URL / 日志。
		cmd.Env = append(cmd.Env,
			"GIT_CONFIG_COUNT=1",
			"GIT_CONFIG_KEY_0=http.extraHeader",
			"GIT_CONFIG_VALUE_0=Authorization: Bearer "+token,
		)
	}
	out := &capWriter{max: gitOutputCap}
	cmd.Stdout = out
	cmd.Stderr = out
	err := cmd.Run()
	scrubbed := scrubToken(out.String(), token)
	if out.truncated {
		scrubbed += "...(truncated)"
	}
	if err != nil {
		return scrubbed, fmt.Errorf("clone: %w", err)
	}
	return scrubbed, nil
}

func sanitizedGitEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, item := range env {
		key := item
		if idx := strings.IndexByte(item, '='); idx >= 0 {
			key = item[:idx]
		}
		if strings.HasPrefix(strings.ToUpper(key), "GIT_") {
			continue
		}
		out = append(out, item)
	}
	return out
}

// capWriter 把写入限制在 max 字节以内（超出部分丢弃但如实上报写入量，避免管道阻塞）。
type capWriter struct {
	buf       bytes.Buffer
	max       int
	truncated bool
}

func (c *capWriter) Write(p []byte) (int, error) {
	if c.buf.Len() >= c.max {
		c.truncated = true
		return len(p), nil // 丢弃，但报告已写（防止 git 阻塞在写满的管道上）
	}
	remain := c.max - c.buf.Len()
	if len(p) > remain {
		_, _ = c.buf.Write(p[:remain])
		c.truncated = true
		return len(p), nil
	}
	return c.buf.Write(p)
}

func (c *capWriter) String() string { return c.buf.String() }

// scrubToken 把输出中出现的 token 明文替换为 ***（git 偶尔在错误中回显 remote）。
func scrubToken(s, token string) string {
	if token == "" {
		return s
	}
	return strings.ReplaceAll(s, token, "***")
}

// boundCloneSize 校验 clone 目录总大小不超过 max；walk 不跟随 symlink（link 按链接大小计）。
func boundCloneSize(root string, max int64) error {
	var total int64
	err := filepath.Walk(root, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info != nil && !info.IsDir() {
			total += info.Size()
			if total > max {
				return fmt.Errorf("clone 超过 %d 字节上限", max)
			}
		}
		return nil
	})
	return err
}

// safeReadCatalogFile 在 root 下安全读取相对路径文件：拒绝绝对路径，Clean + EvalSymlinks
// 解析后必须严格位于 root 之内（防 traversal / symlink 逃逸）。超过 maxBytes 拒绝。
func safeReadCatalogFile(root, rel string, maxBytes int64) ([]byte, error) {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return nil, fmt.Errorf("empty file path")
	}
	if filepath.IsAbs(rel) {
		return nil, fmt.Errorf("catalog file path must be relative: %q", rel)
	}
	rootClean, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return nil, err
	}
	target := filepath.Join(rootClean, rel)
	realTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		return nil, err
	}
	realRoot, err := filepath.EvalSymlinks(rootClean)
	if err != nil {
		return nil, err
	}
	if realTarget != realRoot && !strings.HasPrefix(realTarget, realRoot+string(filepath.Separator)) {
		return nil, fmt.Errorf("catalog file escapes root (traversal/symlink): %q", rel)
	}
	f, err := os.Open(realTarget)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("catalog file 超过 %d 字节上限", maxBytes)
	}
	return data, nil
}

// swapDir 通过同文件系统 rename 替换目录；失败时恢复 last-good。
func swapDir(newDir, oldDir string) error {
	if _, err := os.Stat(newDir); err != nil {
		return fmt.Errorf("new dir missing: %w", err)
	}
	backup := oldDir + ".backup"
	_ = os.RemoveAll(backup)
	hadOld := false
	if _, err := os.Stat(oldDir); err == nil {
		if err := os.Rename(oldDir, backup); err != nil {
			return fmt.Errorf("backup old dir: %w", err)
		}
		hadOld = true
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat old dir: %w", err)
	}
	if err := os.Rename(newDir, oldDir); err != nil {
		if hadOld {
			_ = os.Rename(backup, oldDir)
		}
		return fmt.Errorf("rename clone dir: %w", err)
	}
	if hadOld {
		_ = os.RemoveAll(backup)
	}
	return nil
}

// recoverCatalogBackup 修复进程在 old→backup 与 new→old 两次 rename 之间退出的状态。
func recoverCatalogBackup(oldDir string) error {
	backup := oldDir + ".backup"
	if _, err := os.Stat(oldDir); err == nil {
		_ = os.RemoveAll(backup)
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if _, err := os.Stat(backup); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return os.Rename(backup, oldDir)
}

// shortHash 稳定短哈希（用于派生 clone 目录名）。
func shortHash(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])[:12]
}

// CatalogLocalAppID 为 catalog 应用生成稳定、合法、来源隔离的 devbox app ID（desired.ID）。
//
// upstreamKey 是 catalog 内原始 app key（如 1Panel 的 act_runner，可含下划线/大写/超长）；
// StoreApp.ID 与可信路由（GetVersion/install）仍保留原始 upstreamKey，本函数仅用于本地命名空间。
//
//   - base = Slugify(upstreamKey)（非法字符→-、折叠/裁剪首尾 -）。
//   - 追加 -<shortHash(sourceID + \x00 + upstreamKey)>：保证 a_b/a-b、多来源同 upstreamKey
//     不碰撞（hash 含原始 upstreamKey，slug 相同也区分）。
//   - base 截断预留 "-"+hash（13 字符），最终过 ValidateAppID（3..63，[a-z0-9-]）。
//
// 兼容已装：installResolvedVersion 先 findInstalledVersion 命中则复用其 ID，仅全新安装用本 ID。
func CatalogLocalAppID(upstreamKey, sourceID string) string {
	base := Slugify(upstreamKey)
	hash := shortHash(sourceID + "\x00" + upstreamKey)
	maxBase := 63 - len("-"+hash) // = 50；预留 hash 后缀
	if maxBase < 1 {
		maxBase = 1
	}
	if len(base) > maxBase {
		base = strings.Trim(base[:maxBase], "-")
	}
	if base == "" {
		base = "app" // 全非法字符（如 ___）→ 兜底前缀
	}
	id := base + "-" + hash
	if !isValidAppID(id) {
		id = "app-" + hash // 兜底（理论上不会触发）
	}
	return id
}

// --- CatalogSet：多 source 聚合 ---

// CatalogSet 聚合多个 catalog source，提供统一 list / 路由 install / 状态视图。
// 单 source 故障被隔离（其状态标 error，不污染整体 list，不影响已安装应用）。
type CatalogSet struct {
	sources []Catalog
	logger  *zap.Logger

	mu         sync.RWMutex
	snapshots  map[string]CatalogSnapshot // by source id
	now        func() time.Time
	generation uint64
}

// SetSources 原子替换当前来源集合。调用方可随后 RefreshAll；构造时已加载的
// last-good snapshot 会立即可见。与周期 RefreshAll 并发安全。
func (cs *CatalogSet) SetSources(sources []Catalog) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.sources = append([]Catalog(nil), sources...)
	cs.generation++
	next := make(map[string]CatalogSnapshot, len(sources))
	for _, c := range sources {
		snap := c.Snapshot()
		if len(snap.Apps) == 0 && snap.Status.Message == "尚未同步" {
			snap = CatalogSnapshot{SourceID: c.ID(), SourceName: c.ID(), Kind: c.Kind(), Status: CatalogStatus{State: CatalogStateSyncing, Message: "首次同步中"}}
		}
		next[c.ID()] = snap
	}
	cs.snapshots = next
}

// NewCatalogSet 构造聚合器。sources 可为空（返回可用但空集合，UI 显示无 catalog）。
func NewCatalogSet(sources []Catalog, logger *zap.Logger) *CatalogSet {
	if logger == nil {
		logger = zap.NewNop()
	}
	cs := &CatalogSet{sources: sources, logger: logger, snapshots: map[string]CatalogSnapshot{}, now: time.Now}
	// 构造器可能已加载持久 last-good，先暴露它；否则显示首次同步中。
	for _, c := range sources {
		snap := c.Snapshot()
		if len(snap.Apps) == 0 && snap.Status.Message == "尚未同步" {
			snap = CatalogSnapshot{SourceID: c.ID(), SourceName: c.ID(), Kind: c.Kind(), Status: CatalogStatus{State: CatalogStateSyncing, Message: "首次同步中"}}
		}
		cs.snapshots[c.ID()] = snap
	}
	return cs
}

// WithCatalogClock 注入时钟（测试）。
func (cs *CatalogSet) WithCatalogClock(now func() time.Time) *CatalogSet {
	cs.now = now
	return cs
}

// Start 启动：先同步刷新一次，再按 interval 周期刷新（失败隔离）。interval<=0 时不周期刷新。
func (cs *CatalogSet) Start(ctx context.Context, interval time.Duration) {
	cs.RefreshAll(ctx)
	if interval <= 0 {
		return
	}
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				cs.RefreshAll(ctx)
			}
		}
	}()
}

// RefreshAll 逐个刷新 source，单 source 失败仅记录其状态，不影响其它。
func (cs *CatalogSet) RefreshAll(ctx context.Context) {
	cs.mu.RLock()
	sources := append([]Catalog(nil), cs.sources...)
	generation := cs.generation
	cs.mu.RUnlock()
	var wg sync.WaitGroup
	for _, c := range sources {
		wg.Add(1)
		go func(c Catalog) {
			defer wg.Done()
			if err := c.Refresh(ctx); err != nil {
				cs.logger.Warn("catalog refresh failed",
					zap.String("source", c.ID()), zap.Error(err))
			}
			cs.mu.Lock()
			if cs.generation == generation {
				cs.snapshots[c.ID()] = c.Snapshot()
			}
			cs.mu.Unlock()
		}(c)
	}
	wg.Wait()
}

// RefreshOne 只刷新指定来源并更新其聚合快照。来源集合在刷新期间被替换时，
// 旧 generation 的结果会被丢弃。
func (cs *CatalogSet) RefreshOne(ctx context.Context, id string) (CatalogSnapshot, error) {
	cs.mu.RLock()
	generation := cs.generation
	var target Catalog
	for _, c := range cs.sources {
		if c.ID() == id {
			target = c
			break
		}
	}
	cs.mu.RUnlock()
	if target == nil {
		return CatalogSnapshot{}, fmt.Errorf("catalog source %q 不存在", id)
	}
	err := target.Refresh(ctx)
	snap := target.Snapshot()
	cs.mu.Lock()
	if cs.generation == generation {
		cs.snapshots[id] = snap
	}
	cs.mu.Unlock()
	return snap, err
}

// ListApps 合并所有 source 的 app（来源隔离：某 source 出错则其贡献 0 个 app）。
// 不做已安装判断（由 handler 用 capability + installed map 增补）。
func (cs *CatalogSet) ListApps() []StoreApp {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	var out []StoreApp
	for _, c := range cs.sources {
		snap := cs.snapshots[c.ID()]
		out = append(out, snap.Apps...)
	}
	return out
}

// Statuses 返回所有 source 的状态视图（UI 展示来源健康）。
func (cs *CatalogSet) Statuses() []CatalogSnapshot {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	out := make([]CatalogSnapshot, 0, len(cs.sources))
	for _, c := range cs.sources {
		out = append(out, cs.snapshots[c.ID()])
	}
	return out
}

// GetVersion 路由到指定 source 取版本。
func (cs *CatalogSet) GetVersion(ctx context.Context, sourceID, appID, version string) (StoreAppVersion, error) {
	c := cs.Find(sourceID)
	if c == nil {
		return StoreAppVersion{}, fmt.Errorf("catalog source %q 不存在", sourceID)
	}
	ver, err := c.GetVersion(ctx, appID, version)
	if err != nil {
		return StoreAppVersion{}, err
	}
	ver.CatalogID = sourceID
	if snap, ok := cs.snapshotOf(sourceID); ok && snap.SourceName != "" {
		ver.CatalogName = snap.SourceName
	}
	return ver, nil
}

// Find 按 id 取 source；不存在返回 nil。
func (cs *CatalogSet) Find(id string) Catalog {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	for _, c := range cs.sources {
		if c.ID() == id {
			return c
		}
	}
	return nil
}

// Configured 是否配置了任何 source（UI 决定是否展示 catalog 区）。
func (cs *CatalogSet) Configured() bool {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return len(cs.sources) > 0
}

func (cs *CatalogSet) snapshotOf(id string) (CatalogSnapshot, bool) {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	s, ok := cs.snapshots[id]
	return s, ok
}

// --- 构造 ---

// NewCatalog 按 CatalogSource.Kind 构造对应 Catalog。cacheRoot 供 HTTP/Git last-good 缓存。
func NewCatalog(src CatalogSource, cacheRoot string) (Catalog, error) {
	switch strings.ToLower(strings.TrimSpace(src.Kind)) {
	case "http":
		return newHTTPCatalog(src, cacheRoot)
	case "git":
		return NewGitCatalog(src, cacheRoot)
	case "1panel":
		return newOnePanelCatalog(src, cacheRoot)
	default:
		return nil, fmt.Errorf("unknown catalog kind %q（仅 http/git/1panel）", src.Kind)
	}
}

// NewCatalogSetFromConfigs 批量构造（单个失败仅记录 warn 并跳过，不整体失败）。
// cacheRoot 供 git clone 缓存根（通常 <dataDir>/catalog-cache）。
func NewCatalogSetFromConfigs(cfgs []CatalogSource, cacheRoot string, logger *zap.Logger) *CatalogSet {
	var sources []Catalog
	for _, c := range cfgs {
		cat, err := NewCatalog(c, cacheRoot)
		if err != nil {
			if logger != nil {
				logger.Warn("catalog source 构造失败，已跳过", zap.String("id", c.ID), zap.String("url", redactURL(c.URL)), zap.Error(err))
			}
			continue
		}
		sources = append(sources, cat)
	}
	return NewCatalogSet(sources, logger)
}

// --- catalog install DTO（HTTP /catalogs/install 请求）---

// CatalogInstallRequest 第三方 catalog 安装请求（前端提交）。
// 后端按 SourceID+AppID+Version 从可信 catalog source 重新取版本；前端不传 compose 原文。
type CatalogInstallRequest struct {
	SourceID string         `json:"sourceId"`
	AppID    string         `json:"appId"`
	Version  string         `json:"version"`
	Values   map[string]any `json:"values,omitempty"`
	// ConfirmRisky 表示用户已显式确认 confirmation 级运行权限；blocked 风险仍不可绕过。
	ConfirmRisky bool `json:"confirmRisky,omitempty"`
	// IdempotencyKey 可选；仅在调用方显式提供时启用跨请求幂等。
	IdempotencyKey string `json:"idempotencyKey,omitempty"`
}

// --- 小工具 ---

func sourceID(src CatalogSource, fallback string) string {
	if id := strings.TrimSpace(src.ID); id != "" {
		return id
	}
	return fallback
}

func orStr(v, def string) string {
	if strings.TrimSpace(v) != "" {
		return v
	}
	return def
}

func truncateMsg(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}
