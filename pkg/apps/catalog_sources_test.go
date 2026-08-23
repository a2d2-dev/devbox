package apps

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type sourceTestCatalog struct {
	src       CatalogSource
	refreshed bool
}

func (c *sourceTestCatalog) ID() string                    { return c.src.ID }
func (c *sourceTestCatalog) Kind() string                  { return c.src.Kind }
func (c *sourceTestCatalog) Refresh(context.Context) error { c.refreshed = true; return nil }
func (c *sourceTestCatalog) Snapshot() CatalogSnapshot {
	state := CatalogStateSyncing
	count := 0
	if c.refreshed {
		state = CatalogStateOK
		count = 2
	}
	return CatalogSnapshot{SourceID: c.src.ID, SourceName: orStr(c.src.Name, c.src.ID), Kind: c.src.Kind, Status: CatalogStatus{State: state, AppCount: count}}
}
func (c *sourceTestCatalog) GetVersion(context.Context, string, string) (StoreAppVersion, error) {
	return StoreAppVersion{}, nil
}

func newSourceManagerForTest(t *testing.T, configured []CatalogSource) (*CatalogSourceManager, Repository) {
	t.Helper()
	repo, err := OpenRepository(context.Background(), filepath.Join(t.TempDir(), "apps.db"))
	require.NoError(t, err)
	m := NewCatalogSourceManager(repo, configured, NewCatalogSet(nil, zap.NewNop()), t.TempDir(), zap.NewNop())
	m.resolve = func(context.Context, string) ([]net.IP, error) { return []net.IP{net.ParseIP("140.82.121.4")}, nil }
	m.newCatalog = func(src CatalogSource, _ string) (Catalog, error) { return &sourceTestCatalog{src: src}, nil }
	return m, repo
}

func TestCatalogSourceManagerCRUDAndTokenRedaction(t *testing.T) {
	ctx := context.Background()
	m, repo := newSourceManagerForTest(t, nil)
	defer repo.Close()
	created, err := m.Create(ctx, CatalogSourceInput{ID: "onepanel-official", Name: "Official", Kind: "auto", URL: "https://github.com/1Panel-dev/appstore", Token: "secret-token"}, "tester")
	require.NoError(t, err)
	require.True(t, created.TokenConfigured)
	require.Equal(t, "database", created.ManagedBy)
	body, _ := json.Marshal(created)
	require.NotContains(t, string(body), "secret-token")
	require.NotContains(t, string(body), `"token"`)

	disabled := false
	updated, err := m.Update(ctx, created.ID, CatalogSourceInput{Enabled: &disabled}, "tester")
	require.NoError(t, err)
	require.False(t, updated.Enabled)
	require.True(t, updated.TokenConfigured)
	rec, found, err := repo.GetCatalogSource(ctx, created.ID)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "secret-token", rec.Token)

	var auditDetails []string
	rows, err := repo.(*sqliteRepo).db.QueryContext(ctx, `SELECT detail FROM audit WHERE app_id=? ORDER BY id`, "catalog:"+created.ID)
	require.NoError(t, err)
	defer rows.Close()
	for rows.Next() {
		var d string
		require.NoError(t, rows.Scan(&d))
		auditDetails = append(auditDetails, d)
	}
	require.Len(t, auditDetails, 2)
	require.NotContains(t, auditDetails[0]+auditDetails[1], "secret-token")

	require.NoError(t, m.Delete(ctx, created.ID, "tester"))
	_, found, err = repo.GetCatalogSource(ctx, created.ID)
	require.NoError(t, err)
	require.False(t, found)
}

func TestCatalogSourceManagerConfigReadOnlyAndCollision(t *testing.T) {
	configured := []CatalogSource{{ID: "onepanel-config", Name: "Config", Kind: "1panel", URL: "https://github.com/1Panel-dev/appstore"}}
	m, repo := newSourceManagerForTest(t, configured)
	defer repo.Close()
	require.NoError(t, m.Reload(context.Background()))
	views, err := m.List(context.Background())
	require.NoError(t, err)
	require.Len(t, views, 1)
	require.True(t, views[0].ReadOnly)
	require.Equal(t, "config", views[0].ManagedBy)
	_, err = m.Create(context.Background(), CatalogSourceInput{ID: "onepanel-config", Kind: "1panel", URL: "https://github.com/example/fork"}, "tester")
	require.ErrorIs(t, err, ErrCatalogSourceConflict)
	require.ErrorIs(t, m.Delete(context.Background(), "onepanel-config", "tester"), ErrCatalogSourceConflict)
}

func TestCatalogSourceChangeAuditFailureRollsBack(t *testing.T) {
	ctx := context.Background()
	repo, err := OpenRepository(ctx, filepath.Join(t.TempDir(), "apps.db"))
	require.NoError(t, err)
	defer repo.Close()
	sqlRepo := repo.(*sqliteRepo)
	_, err = sqlRepo.db.ExecContext(ctx, `CREATE TRIGGER fail_source_audit BEFORE INSERT ON audit WHEN NEW.action='fail' BEGIN SELECT RAISE(ABORT,'fail'); END`)
	require.NoError(t, err)
	rec := CatalogSourceRecord{ID: "rollback-source", Kind: "1panel", URL: "https://github.com/example/store", Enabled: true}
	err = repo.CommitCatalogSourceChange(ctx, CatalogSourceChange{Record: &rec, Audit: AuditRecord{Action: "fail"}})
	require.Error(t, err)
	_, found, err := repo.GetCatalogSource(ctx, rec.ID)
	require.NoError(t, err)
	require.False(t, found, "audit 失败必须回滚来源写入")
}

func TestCatalogDatabasePermissions(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "apps.db")
	repo, err := OpenRepository(context.Background(), dbPath)
	require.NoError(t, err)
	defer repo.Close()
	rec := CatalogSourceRecord{ID: "perm-source", Kind: "1panel", URL: "https://github.com/example/store", Token: "secret", Enabled: true}
	require.NoError(t, repo.CommitCatalogSourceChange(context.Background(), CatalogSourceChange{Record: &rec, Audit: AuditRecord{Action: "perm"}}))
	info, err := os.Stat(dbPath)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	for _, suffix := range []string{"-wal", "-shm"} {
		if info, err := os.Stat(dbPath + suffix); err == nil {
			require.Equal(t, os.FileMode(0o600), info.Mode().Perm(), suffix)
		}
	}
}

func TestCatalogSetSetSourcesDropsStaleGeneration(t *testing.T) {
	old := &sourceTestCatalog{src: CatalogSource{ID: "old", Kind: "1panel"}}
	cs := NewCatalogSet([]Catalog{old}, zap.NewNop())
	cs.SetSources([]Catalog{&sourceTestCatalog{src: CatalogSource{ID: "new", Kind: "1panel"}}})
	require.Nil(t, cs.Find("old"))
	require.NotNil(t, cs.Find("new"))
	require.Len(t, cs.Statuses(), 1)
}

func TestValidateDynamicCatalogURLRejectsEmptyDNS(t *testing.T) {
	err := validateDynamicCatalogURL(context.Background(), "https://example.com/repo", func(context.Context, string) ([]net.IP, error) { return nil, nil })
	require.Error(t, err)
}
