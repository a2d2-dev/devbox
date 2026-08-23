package console

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/a2d2-dev/devbox/pkg/apps"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// fakeCatalog console 测试用 apps.Catalog 实现。
type fakeCatalog struct {
	id, kind string
	snap     apps.CatalogSnapshot
	version  apps.StoreAppVersion
	verr     error
	refreshs int
}

func (f *fakeCatalog) ID() string                     { return f.id }
func (f *fakeCatalog) Kind() string                   { return f.kind }
func (f *fakeCatalog) Refresh(context.Context) error  { f.refreshs++; return nil }
func (f *fakeCatalog) Snapshot() apps.CatalogSnapshot { return f.snap }
func (f *fakeCatalog) GetVersion(_ context.Context, _, _ string) (apps.StoreAppVersion, error) {
	return f.version, f.verr
}

func newCatalogTestServer(t *testing.T, ctrl apps.Controller, cs *apps.CatalogSet) *Server {
	t.Helper()
	s := &Server{controller: ctrl, catalogs: cs, logger: zap.NewNop(), mux: http.NewServeMux()}
	s.mux.HandleFunc("/api/v1/catalogs", s.handleCatalogs)
	s.mux.HandleFunc("/api/v1/catalogs/apps", s.handleCatalogApps)
	s.mux.HandleFunc("/api/v1/catalogs/version", s.handleCatalogVersion)
	s.mux.HandleFunc("/api/v1/catalogs/install", s.handleCatalogInstall)
	s.registerAppRoutes()
	return s
}

func catalogSetWith(t *testing.T, cats ...apps.Catalog) *apps.CatalogSet {
	t.Helper()
	cs := apps.NewCatalogSet(cats, zap.NewNop())
	cs.RefreshAll(context.Background())
	return cs
}

func TestHandleCatalogApps_SourceMetadata(t *testing.T) {
	cat := &fakeCatalog{id: "src1", kind: "http", snap: apps.CatalogSnapshot{
		SourceID: "src1", SourceName: "Acme", Status: apps.CatalogStatus{State: apps.CatalogStateOK},
		Apps: []apps.StoreApp{{ID: "ghost", Name: "Ghost", CatalogID: "src1", CatalogName: "Acme",
			Installable: true, Runtime: apps.RuntimeCompose, Runtimes: []string{"compose"}}},
	}}
	cs := catalogSetWith(t, cat)
	ctrl := &stubController{capability: apps.CapabilityReport{Compose: apps.RuntimeCapability{Available: true}}}
	s := newCatalogTestServer(t, ctrl, cs)

	w := do(s, http.MethodGet, "/api/v1/catalogs/apps", nil)
	assert.Equal(t, http.StatusOK, w.Code)

	var got []apps.StoreApp
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.Len(t, got, 1)
	assert.Equal(t, "src1", got[0].CatalogID)
	assert.Equal(t, "Acme", got[0].CatalogName)
	assert.True(t, got[0].Installable, "capability available → installable")
}

func TestHandleCatalogApps_ComposeUnavailableNotInstallable(t *testing.T) {
	cat := &fakeCatalog{id: "s", kind: "http", snap: apps.CatalogSnapshot{
		SourceID: "s", Status: apps.CatalogStatus{State: apps.CatalogStateOK},
		Apps: []apps.StoreApp{{ID: "x"}},
	}}
	cs := catalogSetWith(t, cat)
	ctrl := &stubController{capability: apps.CapabilityReport{}} // compose 不可用
	s := newCatalogTestServer(t, ctrl, cs)
	w := do(s, http.MethodGet, "/api/v1/catalogs/apps", nil)
	var got []apps.StoreApp
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.Len(t, got, 1)
	assert.False(t, got[0].Installable)
	assert.NotEmpty(t, got[0].NotInstallableReason)
}

func TestHandleCatalogVersion_ComposeNotLeaked(t *testing.T) {
	cat := &fakeCatalog{id: "src1", kind: "http", snap: apps.CatalogSnapshot{SourceID: "src1", Status: apps.CatalogStatus{State: apps.CatalogStateOK}},
		version: apps.StoreAppVersion{AppID: "ghost", Version: "1.0.0", Runtime: apps.RuntimeCompose,
			Installable: true, ComposeTemplate: "SECRET-COMPOSE-BODY"},
	}
	cs := catalogSetWith(t, cat)
	s := newCatalogTestServer(t, &stubController{}, cs)

	w := do(s, http.MethodGet, "/api/v1/catalogs/version?sourceId=src1&appId=ghost&v=1.0.0", nil)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.NotContains(t, w.Body.String(), "SECRET-COMPOSE-BODY", "compose template must not leak")
}

func TestHandleCatalogVersion_RequiresSourceAndApp(t *testing.T) {
	cs := catalogSetWith(t, &fakeCatalog{id: "s", kind: "http"})
	s := newCatalogTestServer(t, &stubController{}, cs)
	w := do(s, http.MethodGet, "/api/v1/catalogs/version?sourceId=s", nil)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// catalog install：后端按 sourceId+appId+version 从可信 source 重取 → Apply；
// 前端不传 compose，Controller 收到 SourceCatalog + CatalogID。
func TestHandleCatalogInstall_TrustedRefetch(t *testing.T) {
	cat := &fakeCatalog{id: "src1", kind: "http",
		snap: apps.CatalogSnapshot{SourceID: "src1", Status: apps.CatalogStatus{State: apps.CatalogStateOK}},
		version: apps.StoreAppVersion{AppID: "ghost", Version: "1.0.0", Runtime: apps.RuntimeCompose, Installable: true,
			ComposeTemplate: "services:\n  web:\n    image: ghost:5.90\n"},
	}
	cs := catalogSetWith(t, cat)
	ctrl := &stubController{applyTask: apps.Task{ID: "t-cat", Revision: 3}}
	s := newCatalogTestServer(t, ctrl, cs)

	body := map[string]any{"sourceId": "src1", "appId": "ghost", "version": "1.0.0"}
	w := do(s, http.MethodPost, "/api/v1/catalogs/install", body)
	assert.Equal(t, http.StatusAccepted, w.Code)

	var res apps.StoreInstallResult
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &res))
	assert.Equal(t, "t-cat", res.TaskID)

	// Controller 收到的 source 是 catalog 来源，带 CatalogID；compose 来自可信重取（非前端）。
	require.Equal(t, apps.SourceCatalog, ctrl.lastDesired.Source.Kind)
	assert.Equal(t, "src1", ctrl.lastDesired.Source.CatalogID)
	assert.Equal(t, "1.0.0", ctrl.lastDesired.Source.Version)
	assert.Contains(t, ctrl.lastDesired.ComposeContent, "ghost:5.90")
}

func TestHandleCatalogInstall_PassesRiskConfirmation(t *testing.T) {
	cat := &fakeCatalog{id: "src1", kind: "http",
		snap: apps.CatalogSnapshot{SourceID: "src1", Status: apps.CatalogStatus{State: apps.CatalogStateOK}},
		version: apps.StoreAppVersion{AppID: "ghost", Version: "1.0.0", Runtime: apps.RuntimeCompose,
			Installable: true, ComposeTemplate: "services:\n  web:\n    image: ghost:5.90\n"},
	}
	ctrl := &stubController{applyTask: apps.Task{ID: "t-risk"}}
	s := newCatalogTestServer(t, ctrl, catalogSetWith(t, cat))
	w := do(s, http.MethodPost, "/api/v1/catalogs/install", map[string]any{
		"sourceId": "src1", "appId": "ghost", "version": "1.0.0", "confirmRisky": true,
	})
	require.Equal(t, http.StatusAccepted, w.Code)
	assert.True(t, ctrl.lastApplyOpts.AllowRiskyConfirmation)
}

// catalog install：版本不可安装（kubernetes 包）→ 422。
func TestHandleCatalogInstall_NotInstallable(t *testing.T) {
	cat := &fakeCatalog{id: "src1", kind: "http",
		snap:    apps.CatalogSnapshot{SourceID: "src1", Status: apps.CatalogStatus{State: apps.CatalogStateOK}},
		version: apps.StoreAppVersion{AppID: "x", Version: "1.0.0", Runtime: apps.RuntimeKubernetes, Installable: false},
	}
	cs := catalogSetWith(t, cat)
	s := newCatalogTestServer(t, &stubController{}, cs)
	w := do(s, http.MethodPost, "/api/v1/catalogs/install",
		map[string]any{"sourceId": "src1", "appId": "x", "version": "1.0.0"})
	assert.Equal(t, http.StatusUnprocessableEntity, w.Code)
}

func TestHandleCatalogs_StatusAndRefresh(t *testing.T) {
	cat := &fakeCatalog{id: "src1", kind: "http", snap: apps.CatalogSnapshot{
		SourceID: "src1", Status: apps.CatalogStatus{State: apps.CatalogStateOK, AppCount: 2}}}
	cs := catalogSetWith(t, cat)
	s := newCatalogTestServer(t, &stubController{}, cs)

	// GET statuses.
	w := do(s, http.MethodGet, "/api/v1/catalogs", nil)
	assert.Equal(t, http.StatusOK, w.Code)
	var snaps []apps.CatalogSnapshot
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &snaps))
	require.Len(t, snaps, 1)
	assert.Equal(t, apps.CatalogStateOK, snaps[0].Status.State)

	// POST refresh.
	before := cat.refreshs
	w = do(s, http.MethodPost, "/api/v1/catalogs", nil)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Greater(t, cat.refreshs, before, "POST triggers refresh")
}

func TestHandleCatalogs_NotConfigured(t *testing.T) {
	s := &Server{controller: &stubController{}, catalogs: nil, logger: zap.NewNop(), mux: http.NewServeMux()}
	s.mux.HandleFunc("/api/v1/catalogs/apps", s.handleCatalogApps)
	w := do(s, http.MethodGet, "/api/v1/catalogs/apps", nil)
	assert.Equal(t, http.StatusOK, w.Code)
	// 未配置 → 空数组。
	var got []any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Empty(t, got)
}

// --- app 详情路由（要求 6/7）---

func TestHandleStorageInventory(t *testing.T) {
	s := &Server{controller: &stubController{}, logger: zap.NewNop(), mux: http.NewServeMux()}
	s.mux.HandleFunc("/api/v1/apps/", s.handleAppByID)
	w := do(s, http.MethodGet, "/api/v1/apps/ghost/storage", nil)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleEnvInventory(t *testing.T) {
	s := &Server{controller: &stubController{}, logger: zap.NewNop(), mux: http.NewServeMux()}
	s.mux.HandleFunc("/api/v1/apps/", s.handleAppByID)
	w := do(s, http.MethodGet, "/api/v1/apps/ghost/env", nil)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleRemovePreview(t *testing.T) {
	s := &Server{controller: &stubController{}, logger: zap.NewNop(), mux: http.NewServeMux()}
	s.mux.HandleFunc("/api/v1/apps/", s.handleAppByID)
	w := do(s, http.MethodGet, "/api/v1/apps/ghost/remove-preview?purge=true", nil)
	assert.Equal(t, http.StatusOK, w.Code)
	var pre apps.RemovePreview
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &pre))
	assert.True(t, pre.Purge)
}
