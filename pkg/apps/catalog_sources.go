package apps

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

var (
	ErrCatalogSourceConflict    = errors.New("catalog source already exists")
	ErrCatalogSourceNotFound    = errors.New("catalog source not found")
	ErrCatalogSourceUnreachable = errors.New("catalog source unreachable")
)

// CatalogSourceInput 是动态来源写入模型。Token 只写；空值在更新时表示保留。
type CatalogSourceInput struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Kind    string `json:"kind"` // auto | 1panel
	URL     string `json:"url"`
	Ref     string `json:"ref"`
	Token   string `json:"token"`
	Enabled *bool  `json:"enabled,omitempty"`
}

// CatalogSourceView 是前端可见的脱敏来源；永不包含 token。
type CatalogSourceView struct {
	ID              string        `json:"id"`
	Name            string        `json:"name"`
	Kind            string        `json:"kind"`
	URL             string        `json:"url"`
	Ref             string        `json:"ref,omitempty"`
	Enabled         bool          `json:"enabled"`
	ManagedBy       string        `json:"managedBy"` // config | database
	ReadOnly        bool          `json:"readOnly"`
	TokenConfigured bool          `json:"tokenConfigured"`
	Status          CatalogStatus `json:"status"`
	UpdatedAt       *time.Time    `json:"updatedAt,omitempty"`
}

// CatalogSourceManager 合并启动配置与 apps.db 动态来源，并原子更新 CatalogSet。
type CatalogSourceManager struct {
	repo       Repository
	configured []CatalogSource
	catalogs   *CatalogSet
	cacheRoot  string
	logger     *zap.Logger
	resolve    hostResolver
	newCatalog func(CatalogSource, string) (Catalog, error)
	mu         sync.Mutex
}

func NewCatalogSourceManager(repo Repository, configured []CatalogSource, catalogs *CatalogSet, cacheRoot string, logger *zap.Logger) *CatalogSourceManager {
	if logger == nil {
		logger = zap.NewNop()
	}
	if catalogs == nil {
		catalogs = NewCatalogSet(nil, logger)
	}
	return &CatalogSourceManager{repo: repo, configured: append([]CatalogSource(nil), configured...), catalogs: catalogs, cacheRoot: cacheRoot, logger: logger, resolve: defaultHostResolver, newCatalog: NewCatalog}
}

func (m *CatalogSourceManager) Catalogs() *CatalogSet { return m.catalogs }

// Reload 从同一 apps.db 重建启用来源。YAML 来源优先；DB 同 ID 不覆盖。
func (m *CatalogSourceManager) Reload(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	records, err := m.repo.ListCatalogSources(ctx)
	if err != nil {
		return err
	}
	all := append([]CatalogSource(nil), m.configured...)
	seen := map[string]bool{}
	for _, src := range m.configured {
		seen[sourceID(src, src.Kind+":"+src.URL)] = true
	}
	for _, rec := range records {
		if !rec.Enabled || seen[rec.ID] {
			continue
		}
		all = append(all, recordToCatalogSource(rec))
	}
	var cats []Catalog
	for _, src := range all {
		cat, err := m.newCatalog(src, m.cacheRoot)
		if err != nil {
			m.logger.Warn("catalog source 构造失败", zap.String("id", src.ID), zap.Error(err))
			continue
		}
		cats = append(cats, cat)
	}
	m.catalogs.SetSources(cats)
	return nil
}

func (m *CatalogSourceManager) List(ctx context.Context) ([]CatalogSourceView, error) {
	records, err := m.repo.ListCatalogSources(ctx)
	if err != nil {
		return nil, err
	}
	statuses := map[string]CatalogStatus{}
	for _, s := range m.catalogs.Statuses() {
		statuses[s.SourceID] = s.Status
	}
	out := make([]CatalogSourceView, 0, len(m.configured)+len(records))
	for _, src := range m.configured {
		id := sourceID(src, src.Kind+":"+src.URL)
		out = append(out, CatalogSourceView{ID: id, Name: orStr(src.Name, id), Kind: src.Kind, URL: src.URL, Ref: src.Ref, Enabled: true, ManagedBy: "config", ReadOnly: true, TokenConfigured: src.Token != "", Status: statusOrDisabled(statuses[id], true)})
	}
	for _, rec := range records {
		t := rec.UpdatedAt
		out = append(out, CatalogSourceView{ID: rec.ID, Name: orStr(rec.Name, rec.ID), Kind: rec.Kind, URL: rec.URL, Ref: rec.Ref, Enabled: rec.Enabled, ManagedBy: "database", TokenConfigured: rec.Token != "", Status: statusOrDisabled(statuses[rec.ID], rec.Enabled), UpdatedAt: &t})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func statusOrDisabled(st CatalogStatus, enabled bool) CatalogStatus {
	if !enabled {
		return CatalogStatus{State: CatalogStateUnconfigured, Message: "已停用"}
	}
	if st.State == "" {
		return CatalogStatus{State: CatalogStateSyncing, Message: "等待同步"}
	}
	return st
}

// Test 验证公网 URL，并真实 partial+sparse refresh；不持久化。
func (m *CatalogSourceManager) Test(ctx context.Context, in CatalogSourceInput) (CatalogSnapshot, error) {
	src, err := m.normalizeDynamic(ctx, in, true)
	if err != nil {
		return CatalogSnapshot{}, err
	}
	tmp, err := os.MkdirTemp("", "devbox-catalog-test-*")
	if err != nil {
		return CatalogSnapshot{}, err
	}
	defer os.RemoveAll(tmp)
	cat, err := m.newCatalog(src, tmp)
	if err != nil {
		return CatalogSnapshot{}, err
	}
	if err := cat.Refresh(ctx); err != nil {
		return CatalogSnapshot{}, fmt.Errorf("%w: %v", ErrCatalogSourceUnreachable, err)
	}
	return cat.Snapshot(), nil
}

func (m *CatalogSourceManager) Create(ctx context.Context, in CatalogSourceInput, actor string) (CatalogSourceView, error) {
	src, err := m.normalizeDynamic(ctx, in, true)
	if err != nil {
		return CatalogSourceView{}, err
	}
	if m.configID(src.ID) {
		return CatalogSourceView{}, ErrCatalogSourceConflict
	}
	if _, found, err := m.repo.GetCatalogSource(ctx, src.ID); err != nil {
		return CatalogSourceView{}, err
	} else if found {
		return CatalogSourceView{}, ErrCatalogSourceConflict
	}
	if _, err := m.testSource(ctx, src); err != nil {
		return CatalogSourceView{}, err
	}
	rec := sourceToRecord(src, enabledValue(in.Enabled, true))
	if err := m.repo.CommitCatalogSourceChange(ctx, CatalogSourceChange{Record: &rec, Audit: sourceAudit(actor, rec.ID, "catalog.source.create", rec.Kind)}); err != nil {
		return CatalogSourceView{}, err
	}
	if err := m.Reload(ctx); err != nil {
		return CatalogSourceView{}, err
	}
	return m.view(ctx, rec.ID)
}

func (m *CatalogSourceManager) Update(ctx context.Context, id string, in CatalogSourceInput, actor string) (CatalogSourceView, error) {
	if m.configID(id) {
		return CatalogSourceView{}, ErrCatalogSourceConflict
	}
	old, found, err := m.repo.GetCatalogSource(ctx, id)
	if err != nil {
		return CatalogSourceView{}, err
	}
	if !found {
		return CatalogSourceView{}, ErrCatalogSourceNotFound
	}
	in.ID = id
	toggleOnly := in.Enabled != nil && strings.TrimSpace(in.URL) == "" && strings.TrimSpace(in.Kind) == "" &&
		strings.TrimSpace(in.Name) == "" && strings.TrimSpace(in.Ref) == "" && in.Token == ""
	if strings.TrimSpace(in.URL) == "" {
		in.URL = old.URL
	}
	if strings.TrimSpace(in.Kind) == "" {
		in.Kind = old.Kind
	}
	if strings.TrimSpace(in.Name) == "" {
		in.Name = old.Name
	}
	if strings.TrimSpace(in.Ref) == "" {
		in.Ref = old.Ref
	}
	enabled := enabledValue(in.Enabled, old.Enabled)
	var src CatalogSource
	if toggleOnly && !enabled {
		src = recordToCatalogSource(old)
	} else {
		src, err = m.normalizeDynamic(ctx, in, false)
		if err != nil {
			return CatalogSourceView{}, err
		}
	}
	if src.Token == "" {
		src.Token = old.Token
	}
	if enabled && (src.URL != old.URL || src.Ref != old.Ref || in.Token != "" || !old.Enabled) {
		if _, err := m.testSource(ctx, src); err != nil {
			return CatalogSourceView{}, err
		}
	}
	rec := sourceToRecord(src, enabled)
	rec.CreatedAt = old.CreatedAt
	if err := m.repo.CommitCatalogSourceChange(ctx, CatalogSourceChange{Record: &rec, PreserveEmptyToken: true, Audit: sourceAudit(actor, id, "catalog.source.update", rec.Kind)}); err != nil {
		return CatalogSourceView{}, err
	}
	if err := m.Reload(ctx); err != nil {
		return CatalogSourceView{}, err
	}
	return m.view(ctx, id)
}

func (m *CatalogSourceManager) Delete(ctx context.Context, id, actor string) error {
	if m.configID(id) {
		return ErrCatalogSourceConflict
	}
	if _, found, err := m.repo.GetCatalogSource(ctx, id); err != nil {
		return err
	} else if !found {
		return ErrCatalogSourceNotFound
	}
	if err := m.repo.CommitCatalogSourceChange(ctx, CatalogSourceChange{DeleteID: id, Audit: sourceAudit(actor, id, "catalog.source.delete", "1panel")}); err != nil {
		return err
	}
	return m.Reload(ctx)
}

func (m *CatalogSourceManager) Refresh(ctx context.Context, id string) (CatalogSnapshot, error) {
	cat := m.catalogs.Find(id)
	if cat == nil {
		return CatalogSnapshot{}, ErrCatalogSourceNotFound
	}
	snap, err := m.catalogs.RefreshOne(ctx, id)
	if err != nil {
		return snap, fmt.Errorf("%w: %v", ErrCatalogSourceUnreachable, err)
	}
	return snap, nil
}

func (m *CatalogSourceManager) testSource(ctx context.Context, src CatalogSource) (CatalogSnapshot, error) {
	cat, err := m.newCatalog(src, m.cacheRoot)
	if err != nil {
		return CatalogSnapshot{}, err
	}
	if err := cat.Refresh(ctx); err != nil {
		return CatalogSnapshot{}, fmt.Errorf("%w: %v", ErrCatalogSourceUnreachable, err)
	}
	return cat.Snapshot(), nil
}

func (m *CatalogSourceManager) normalizeDynamic(ctx context.Context, in CatalogSourceInput, generateID bool) (CatalogSource, error) {
	kind := strings.ToLower(strings.TrimSpace(in.Kind))
	if kind == "" || kind == "auto" {
		kind = "1panel"
	}
	if kind != "1panel" {
		return CatalogSource{}, ValidationErr("动态来源当前仅支持 auto/1panel")
	}
	if err := validateDynamicCatalogURL(ctx, in.URL, m.resolve); err != nil {
		return CatalogSource{}, ValidationErr(err.Error())
	}
	id := strings.TrimSpace(in.ID)
	if id == "" && generateID {
		id = "onepanel-" + shortHash(strings.TrimSpace(in.URL))
	}
	if !isValidAppID(id) {
		return CatalogSource{}, ValidationErr("来源 ID 仅允许小写字母、数字、连字符，长度 3-63")
	}
	if len(in.Name) > 128 || len(in.URL) > 2048 || len(in.Ref) > 256 || len(in.Token) > 4096 {
		return CatalogSource{}, ValidationErr("来源字段超过长度限制")
	}
	return CatalogSource{ID: id, Name: strings.TrimSpace(in.Name), Kind: kind, URL: strings.TrimSpace(in.URL), Ref: strings.TrimSpace(in.Ref), Token: in.Token}, nil
}

func (m *CatalogSourceManager) configID(id string) bool {
	for _, src := range m.configured {
		if sourceID(src, src.Kind+":"+src.URL) == id {
			return true
		}
	}
	return false
}

func (m *CatalogSourceManager) view(ctx context.Context, id string) (CatalogSourceView, error) {
	list, err := m.List(ctx)
	if err != nil {
		return CatalogSourceView{}, err
	}
	for _, v := range list {
		if v.ID == id {
			return v, nil
		}
	}
	return CatalogSourceView{}, ErrCatalogSourceNotFound
}

func sourceToRecord(src CatalogSource, enabled bool) CatalogSourceRecord {
	return CatalogSourceRecord{ID: src.ID, Name: src.Name, Kind: src.Kind, URL: src.URL, Ref: src.Ref, Path: src.Path, Token: src.Token, Enabled: enabled}
}
func recordToCatalogSource(r CatalogSourceRecord) CatalogSource {
	return CatalogSource{ID: r.ID, Name: r.Name, Kind: r.Kind, URL: r.URL, Ref: r.Ref, Path: r.Path, Token: r.Token}
}
func enabledValue(v *bool, fallback bool) bool {
	if v == nil {
		return fallback
	}
	return *v
}
func sourceAudit(actor, id, action, kind string) AuditRecord {
	return AuditRecord{Actor: actor, AppID: "catalog:" + id, Action: action, Detail: fmt.Sprintf(`{"sourceId":%q,"kind":%q,"managedBy":"database"}`, id, kind)}
}

// CatalogDBPath returns the shared application database path.
func CatalogDBPath(dataDir string) string { return filepath.Join(dataDir, "apps.db") }
