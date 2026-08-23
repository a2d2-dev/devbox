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

func TestAssembleControllerRejectsRemoteDockerEndpoint(t *testing.T) {
	for _, endpoint := range []string{"tcp://host:2375", "http://host:2375", "https://host:2376", "relative.sock"} {
		controller, cleanup, err := AssembleController(context.Background(), ControllerConfig{
			DataDir: filepath.Join(t.TempDir(), "data"), ComposeEnabled: true, DockerSocket: endpoint,
		}, zap.NewNop())
		require.ErrorContains(t, err, "仅允许本机 Unix socket", endpoint)
		require.Nil(t, controller)
		require.Nil(t, cleanup)
	}
}
