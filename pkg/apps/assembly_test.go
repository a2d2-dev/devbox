package apps

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

func TestAssembleControllerKeepsRemoteDockerOverviewReadOnlyWhenComposeDisabled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/info":
			_ = json.NewEncoder(w).Encode(engineInfo{ServerVersion: "remote", DockerRootDir: "/remote/docker"})
		case "/containers/json":
			_ = json.NewEncoder(w).Encode([]engineContainer{})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	controller, cleanup, err := AssembleController(ctx, ControllerConfig{
		DataDir:        filepath.Join(t.TempDir(), "data"),
		ComposeEnabled: false,
		DockerSocket:   server.URL,
	}, zap.NewNop())
	require.NoError(t, err)
	t.Cleanup(cleanup)
	dockerController, ok := controller.(DockerController)
	require.True(t, ok)

	overview, err := dockerController.DockerOverview(context.Background())
	require.NoError(t, err)
	require.Equal(t, DockerServiceRunning, overview.Service.State)
	require.False(t, overview.Service.ControlSupported)
	require.False(t, overview.Service.AutostartSupported)
	require.False(t, overview.Storage.MigrationSupported)
	require.Contains(t, overview.Service.Diagnostic, "远程 Docker daemon")
	require.Equal(t, "/remote/docker", overview.Storage.Path)

	_, err = dockerController.DockerServiceAction(context.Background(), DockerServiceActionRequest{Action: "stop"})
	require.Error(t, err)
	appErr, ok := AsError(err)
	require.True(t, ok)
	require.Equal(t, "remote_daemon_read_only", appErr.Reason)
	_, err = dockerController.PlanDockerMigration(context.Background(), DockerMigrationRequest{TargetPath: "/data/docker"})
	require.Error(t, err)
	appErr, ok = AsError(err)
	require.True(t, ok)
	require.Equal(t, "remote_daemon_read_only", appErr.Reason)
}

func TestAssembleControllerPassesDockerMigrationAllowedRoots(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	controller, cleanup, err := AssembleController(ctx, ControllerConfig{
		DataDir:                     filepath.Join(t.TempDir(), "data"),
		DockerMigrationAllowedRoots: []string{"/srv/docker-data"},
	}, zap.NewNop())
	require.NoError(t, err)
	t.Cleanup(cleanup)
	service, ok := controller.(*service)
	require.True(t, ok)
	require.Equal(t, []string{"/srv/docker-data"}, service.docker.allowedRoots)
}
