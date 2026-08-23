package console

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/a2d2-dev/devbox/pkg/apps"
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
	s := &Server{controller: controller, logger: zap.NewNop(), mux: http.NewServeMux()}
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

func TestDockerHandlerReturnsStructuredDiagnostic(t *testing.T) {
	s := newDockerHandlerServer(&dockerStubController{err: apps.CapabilityDetailErr("permission_denied", "操作失败", "journal lines", errors.New("exit 1"))})
	w := do(s, http.MethodPost, "/api/v1/docker/service", apps.DockerServiceActionRequest{Action: "stop"})
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.JSONEq(t, `{"error":"操作失败","reason":"permission_denied","detail":"journal lines"}`, w.Body.String())
}
