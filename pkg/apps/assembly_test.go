package apps

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestAssembleControllerCreatesMissingDataDir(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "nested", "devbox")
	controller, cleanup, err := AssembleController(context.Background(), ControllerConfig{
		DataDir: dataDir,
	}, zap.NewNop())
	require.NoError(t, err)
	require.NotNil(t, controller)
	require.NotNil(t, cleanup)
	t.Cleanup(cleanup)

	info, err := os.Stat(dataDir)
	require.NoError(t, err)
	require.True(t, info.IsDir())
	_, err = os.Stat(filepath.Join(dataDir, "apps.db"))
	require.NoError(t, err)
}

func TestAssembleControllerRejectsDataDirThatIsAFile(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "not-a-directory")
	require.NoError(t, os.WriteFile(dataDir, []byte("x"), 0o600))

	controller, cleanup, err := AssembleController(context.Background(), ControllerConfig{
		DataDir: dataDir,
	}, zap.NewNop())
	require.ErrorContains(t, err, "create apps data directory")
	require.Nil(t, controller)
	require.Nil(t, cleanup)
}
