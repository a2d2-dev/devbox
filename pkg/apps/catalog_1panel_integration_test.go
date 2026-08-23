//go:build integration

package apps

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestOnePanelOfficialCatalogIntegration 直接读取官方 dev/default branch，验证真实协议未漂移。
func TestOnePanelOfficialCatalogIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	cat, err := NewCatalog(CatalogSource{ID: "onepanel-official", Kind: "1panel", URL: "https://github.com/1Panel-dev/appstore"}, t.TempDir())
	require.NoError(t, err)
	require.NoError(t, cat.Refresh(ctx))
	snap := cat.Snapshot()
	require.Equal(t, CatalogStateOK, snap.Status.State)
	require.Greater(t, snap.Status.AppCount, 100)
	ver, err := cat.GetVersion(ctx, "adminer", "5.5.0-standalone")
	require.NoError(t, err)
	require.True(t, ver.Installable, ver.NotInstallableReason)
	require.Contains(t, string(ver.ValuesSchema), "PANEL_APP_PORT_HTTP")
	require.Contains(t, ver.ComposeTemplate, "adminer:5.5.0-standalone")
	require.NotContains(t, ver.ComposeTemplate, "container_name")
	require.NotContains(t, ver.ComposeTemplate, "external: true")
}
