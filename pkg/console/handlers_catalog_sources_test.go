package console

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/a2d2-dev/devbox/pkg/apps"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestHandleCatalogSourcesListRedactsTokensAndMarksOwnership(t *testing.T) {
	repo, err := apps.OpenRepository(context.Background(), filepath.Join(t.TempDir(), "apps.db"))
	require.NoError(t, err)
	defer repo.Close()
	rec := apps.CatalogSourceRecord{ID: "onepanel-db", Name: "DB Store", Kind: "1panel", URL: "https://github.com/example/store", Token: "never-return-me", Enabled: false}
	require.NoError(t, repo.CommitCatalogSourceChange(context.Background(), apps.CatalogSourceChange{Record: &rec, Audit: apps.AuditRecord{Action: "test"}}))
	configured := []apps.CatalogSource{{ID: "onepanel-config", Name: "Config Store", Kind: "1panel", URL: "https://github.com/1Panel-dev/appstore", Token: "config-secret"}}
	cs := apps.NewCatalogSetFromConfigs(configured, t.TempDir(), zap.NewNop())
	mgr := apps.NewCatalogSourceManager(repo, configured, cs, t.TempDir(), zap.NewNop())
	require.NoError(t, mgr.Reload(context.Background()))
	s := &Server{catalogSourceManager: mgr, catalogs: cs, logger: zap.NewNop(), mux: http.NewServeMux()}
	s.mux.HandleFunc("/api/v1/catalogs/sources", s.handleCatalogSources)
	w := do(s, http.MethodGet, "/api/v1/catalogs/sources", nil)
	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.NotContains(t, body, "never-return-me")
	assert.NotContains(t, body, "config-secret")
	assert.NotContains(t, body, "\"token\":")
	assert.Contains(t, body, "\"managedBy\":\"config\"")
	assert.Contains(t, body, "\"readOnly\":true")
	assert.Contains(t, body, "\"managedBy\":\"database\"")
	assert.Contains(t, body, "\"tokenConfigured\":true")
}
