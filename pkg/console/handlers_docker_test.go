package console

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/a2d2-dev/devbox/pkg/apps"
	"github.com/a2d2-dev/devbox/pkg/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type dockerStubController struct {
	stubController
	overview apps.DockerOverview
	err      error
}

func (s *dockerStubController) DockerOverview(context.Context) (apps.DockerOverview, error) {
	return s.overview, s.err
}
func (s *dockerStubController) DockerStats(context.Context) (apps.DockerStats, error) {
	return apps.DockerStats{Available: true}, s.err
}
func (s *dockerStubController) DockerNetworks(context.Context) (apps.DockerNetworkList, error) {
	return apps.DockerNetworkList{Available: true, Networks: []apps.DockerNetworkSummary{{Name: "bridge", Driver: "bridge", Scope: "local", Containers: 2}}}, s.err
}
func (s *dockerStubController) DockerVolumes(context.Context) (apps.DockerVolumeList, error) {
	return apps.DockerVolumeList{Available: true, Volumes: []apps.DockerVolumeSummary{{Name: "data", Driver: "local", ComposeProject: "alpha"}}}, s.err
}
func (s *dockerStubController) DockerServiceAction(context.Context, apps.DockerServiceActionRequest) (apps.DockerOverview, error) {
	return s.overview, s.err
}
func (s *dockerStubController) SetDockerAutostart(context.Context, apps.DockerAutostartRequest) (apps.DockerOverview, error) {
	return s.overview, s.err
}
func (s *dockerStubController) PlanDockerMigration(context.Context, apps.DockerMigrationRequest) (apps.DockerMigrationPlan, error) {
	return apps.DockerMigrationPlan{ID: "plan-1"}, s.err
}
func (s *dockerStubController) ExecuteDockerMigration(context.Context, apps.DockerMigrationExecuteRequest) (apps.DockerMigrationResult, error) {
	return apps.DockerMigrationResult{Completed: true}, s.err
}

func newDockerHandlerServer(controller apps.Controller) *Server {
	s := &Server{controller: controller, logger: zap.NewNop(), mux: http.NewServeMux(), auth: auth.New(auth.Config{})}
	s.registerDockerRoutes()
	return s
}

func TestDockerOverviewHandler(t *testing.T) {
	s := newDockerHandlerServer(&dockerStubController{overview: apps.DockerOverview{Containers: apps.DockerCountSummary{Running: 2, Total: 3}}})
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/docker/overview", nil))
	require.Equal(t, http.StatusOK, w.Code)
	var overview apps.DockerOverview
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &overview))
	assert.Equal(t, 2, overview.Containers.Running)
}

func TestDockerNetworksVolumesHandlers(t *testing.T) {
	s := newDockerHandlerServer(&dockerStubController{})

	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/docker/networks", nil))
	require.Equal(t, http.StatusOK, w.Code)
	var networks apps.DockerNetworkList
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &networks))
	assert.True(t, networks.Available)
	require.Len(t, networks.Networks, 1)
	assert.Equal(t, "bridge", networks.Networks[0].Name)
	assert.Equal(t, 2, networks.Networks[0].Containers)

	w = httptest.NewRecorder()
	s.mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/docker/volumes", nil))
	require.Equal(t, http.StatusOK, w.Code)
	var volumes apps.DockerVolumeList
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &volumes))
	assert.True(t, volumes.Available)
	require.Len(t, volumes.Volumes, 1)
	assert.Equal(t, "alpha", volumes.Volumes[0].ComposeProject)
}

// Docker 能力未装配：清单接口返回 200 空态而非 5xx（不让前端轮询白屏）。
func TestDockerNetworksVolumesUnassembledEmptyState(t *testing.T) {
	s := newDockerHandlerServer(&stubController{})
	for _, path := range []string{"/api/v1/docker/networks", "/api/v1/docker/volumes"} {
		w := httptest.NewRecorder()
		s.mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		require.Equal(t, http.StatusOK, w.Code, path)
		var body struct {
			Available  bool   `json:"available"`
			Diagnostic string `json:"diagnostic"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body), path)
		assert.False(t, body.Available, path)
		assert.Contains(t, body.Diagnostic, "未装配", path)
	}
}

func TestDockerHandlerReturnsStructuredDiagnostic(t *testing.T) {
	s := newDockerHandlerServer(&dockerStubController{err: apps.CapabilityDetailErr("permission_denied", "操作失败", "journal lines", errors.New("exit 1"))})
	w := do(s, http.MethodPost, "/api/v1/docker/service", apps.DockerServiceActionRequest{Action: "stop"})
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.JSONEq(t, `{"error":"操作失败","reason":"permission_denied","detail":"journal lines"}`, w.Body.String())
}
