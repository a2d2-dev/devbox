package apps

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeDockerHost struct {
	mu         sync.Mutex
	status     DockerServiceSummary
	actions    []string
	controlErr error
	enabledErr error
}

func (h *fakeDockerHost) Status(context.Context) DockerServiceSummary {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.status
}
func (h *fakeDockerHost) Control(_ context.Context, action string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.actions = append(h.actions, action)
	if h.controlErr != nil {
		return h.controlErr
	}
	if action == "stop" {
		h.status.State = DockerServiceStopped
	} else {
		h.status.State = DockerServiceRunning
	}
	return nil
}
func (h *fakeDockerHost) SetAutostart(_ context.Context, enabled bool) error {
	if h.enabledErr != nil {
		return h.enabledErr
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.status.AutostartEnabled = &enabled
	return nil
}
func (h *fakeDockerHost) Diagnostic(context.Context) string { return "journal excerpt" }

type fakeDockerStorage struct {
	daemon    []byte
	disk      DockerDiskSummary
	dirSize   uint64
	targetErr error
	written   []byte
}

func (s *fakeDockerStorage) ReadDaemonJSON() ([]byte, error) { return s.daemon, nil }
func (s *fakeDockerStorage) WriteDaemonJSON(b []byte, _ string) (func() error, error) {
	s.written = append([]byte(nil), b...)
	return func() error { return nil }, nil
}
func (s *fakeDockerStorage) Disk(string) (DockerDiskSummary, error) { return s.disk, nil }
func (s *fakeDockerStorage) DirSize(string) (uint64, error)         { return s.dirSize, nil }
func (s *fakeDockerStorage) EnsureTargetReady(string, bool) error   { return s.targetErr }

type fakeDockerRunner struct {
	runs []string
	err  error
}

func (r *fakeDockerRunner) LookPath(name string) (string, error) {
	if name == "rsync" && r.err != nil {
		return "", r.err
	}
	return "/usr/bin/" + name, nil
}
func (r *fakeDockerRunner) Run(_ context.Context, name string, args ...string) (string, error) {
	r.runs = append(r.runs, name+" "+stringsJoin(args))
	return "", r.err
}

func stringsJoin(parts []string) string {
	out := ""
	for i, part := range parts {
		if i > 0 {
			out += " "
		}
		out += part
	}
	return out
}

func newDockerTestManager(t *testing.T, handler http.Handler, host dockerServiceHost, storage dockerStorage) *DockerManager {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return newDockerManagerWithDeps(
		&dockerEngine{baseURL: server.URL, client: server.Client()}, host, storage, &fakeDockerRunner{},
	)
}

func TestDockerOverviewUsesEngineFacts(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/info":
			_ = json.NewEncoder(w).Encode(engineInfo{ServerVersion: "29.6.1", Containers: 4, ContainersRunning: 2, DockerRootDir: "/docker-data"})
		case "/containers/json":
			_ = json.NewEncoder(w).Encode([]engineContainer{
				{ID: "1", State: "running", Labels: map[string]string{"com.docker.compose.project": "alpha"}},
				{ID: "2", State: "exited", Labels: map[string]string{"com.docker.compose.project": "alpha"}},
				{ID: "3", State: "exited", Labels: map[string]string{"com.docker.compose.project": "beta"}},
				{ID: "4", State: "running", Labels: map[string]string{}},
			})
		default:
			http.NotFound(w, r)
		}
	})
	enabled := true
	host := &fakeDockerHost{status: DockerServiceSummary{State: DockerServiceStopped, Installed: true, AutostartEnabled: &enabled}}
	storage := &fakeDockerStorage{daemon: []byte(`{"data-root":"/configured"}`), disk: DockerDiskSummary{TotalBytes: 1000, AvailableBytes: 400}}
	m := newDockerTestManager(t, handler, host, storage)
	m.now = func() time.Time { return time.Unix(100, 0) }

	o, err := m.Overview(context.Background())
	require.NoError(t, err)
	assert.Equal(t, DockerServiceRunning, o.Service.State, "daemon fact must override service-manager snapshot")
	assert.Equal(t, DockerCountSummary{Running: 2, Total: 4}, o.Containers)
	assert.Equal(t, DockerCountSummary{Running: 1, Total: 2}, o.ComposeProjects)
	assert.Equal(t, "/docker-data", o.Storage.Path)
	assert.Equal(t, "daemon", o.Storage.Source)
	assert.True(t, o.Storage.Valid)
	assert.Equal(t, "2 个容器运行中", o.IdleSummary)
}

func TestDockerOverviewReturnsEmptyStateWhenDaemonUnavailable(t *testing.T) {
	host := &fakeDockerHost{status: DockerServiceSummary{
		State:      DockerServiceRunning,
		Installed:  true,
		Diagnostic: "systemd reports active",
	}}
	storage := &fakeDockerStorage{
		daemon: []byte(`{"data-root":"/docker-data"}`),
		disk:   DockerDiskSummary{TotalBytes: 1000, AvailableBytes: 400},
	}
	m := newDockerTestManager(t, http.NotFoundHandler(), host, storage)

	o, err := m.Overview(context.Background())
	require.NoError(t, err)
	assert.Equal(t, DockerServiceStopped, o.Service.State)
	assert.Equal(t, DockerCountSummary{}, o.Containers)
	assert.Equal(t, DockerCountSummary{}, o.ComposeProjects)
	assert.Equal(t, "空闲", o.IdleSummary)
	assert.Contains(t, o.Service.Diagnostic, "systemd reports active")
	assert.True(t, o.Storage.Valid)
}

func TestDockerStatsAggregatesRunningContainers(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/info":
			_ = json.NewEncoder(w).Encode(engineInfo{MemTotal: 32000})
		case "/containers/json":
			assert.Equal(t, "0", r.URL.Query().Get("all"))
			_ = json.NewEncoder(w).Encode([]engineContainer{{ID: "one"}, {ID: "two"}})
		case "/containers/one/stats", "/containers/two/stats":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"cpu_stats":    map[string]any{"cpu_usage": map[string]any{"total_usage": 30}, "system_cpu_usage": 200, "online_cpus": 2},
				"precpu_stats": map[string]any{"cpu_usage": map[string]any{"total_usage": 10}, "system_cpu_usage": 100},
				"memory_stats": map[string]any{"usage": 1000, "limit": 8000},
				"networks":     map[string]any{"eth0": map[string]any{"rx_bytes": 100, "tx_bytes": 50}},
			})
		default:
			http.NotFound(w, r)
		}
	})
	m := newDockerTestManager(t, handler, &fakeDockerHost{}, &fakeDockerStorage{})

	stats, err := m.Stats(context.Background())
	require.NoError(t, err)
	assert.True(t, stats.Available)
	assert.InDelta(t, 80, stats.CPUPercent, 0.001)
	assert.Equal(t, uint64(2000), stats.MemoryUsageBytes)
	assert.Equal(t, uint64(32000), stats.MemoryLimitBytes)
	assert.Equal(t, uint64(200), stats.NetworkRxBytes)
	assert.Equal(t, uint64(100), stats.NetworkTxBytes)
}

func TestDockerStatsReturnsEmptyStateWhenAllSamplesTimeout(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/info":
			_ = json.NewEncoder(w).Encode(engineInfo{MemTotal: 32000})
		case "/containers/json":
			_ = json.NewEncoder(w).Encode([]engineContainer{{ID: "slow"}})
		case "/containers/slow/stats":
			<-r.Context().Done()
		default:
			http.NotFound(w, r)
		}
	})
	m := newDockerTestManager(t, handler, &fakeDockerHost{}, &fakeDockerStorage{})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	stats, err := m.Stats(ctx)
	require.NoError(t, err)
	assert.False(t, stats.Available)
	assert.Equal(t, 1, stats.FailedContainers)
	assert.Contains(t, stats.Diagnostic, "1 个容器")
}

func TestDockerMigrationPlanPreservesDaemonConfig(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/info":
			_ = json.NewEncoder(w).Encode(engineInfo{DockerRootDir: "/old", Containers: 1})
		case "/containers/json":
			_ = json.NewEncoder(w).Encode([]engineContainer{})
		default:
			http.NotFound(w, r)
		}
	})
	storage := &fakeDockerStorage{
		daemon: []byte(`{"data-root":"/old","registry-mirrors":["https://registry.example"]}`),
		disk:   DockerDiskSummary{TotalBytes: 10000, AvailableBytes: 8000}, dirSize: 3000,
	}
	m := newDockerTestManager(t, handler, &fakeDockerHost{status: DockerServiceSummary{State: DockerServiceRunning}}, storage)

	plan, err := m.MigrationPlan(context.Background(), DockerMigrationRequest{TargetPath: "/new"})
	require.NoError(t, err)
	assert.NotEmpty(t, plan.ID)
	assert.Equal(t, "/old", plan.SourcePath)
	assert.Equal(t, "/new", plan.TargetPath)
	assert.Equal(t, uint64(3000), plan.RequiredBytes)
	assert.Len(t, plan.Steps, 4)
	assert.JSONEq(t, `{"data-root":"/new","registry-mirrors":["https://registry.example"]}`, plan.ProposedDaemonJSON)

	_, err = m.ExecuteMigration(context.Background(), DockerMigrationExecuteRequest{TargetPath: "/new", PlanID: plan.ID})
	assert.Error(t, err)
	ae, ok := AsError(err)
	require.True(t, ok)
	assert.Equal(t, ErrKindValidation, ae.Kind)
}

func TestDockerMigrationPlanRejectsInsufficientCapacity(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/info" {
			_ = json.NewEncoder(w).Encode(engineInfo{DockerRootDir: "/old"})
			return
		}
		if r.URL.Path == "/containers/json" {
			_ = json.NewEncoder(w).Encode([]engineContainer{})
			return
		}
		http.NotFound(w, r)
	})
	storage := &fakeDockerStorage{daemon: []byte(`{}`), disk: DockerDiskSummary{AvailableBytes: 99}, dirSize: 100}
	m := newDockerTestManager(t, handler, &fakeDockerHost{}, storage)

	_, err := m.MigrationPlan(context.Background(), DockerMigrationRequest{TargetPath: "/new"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "空间不足")
}

func TestDockerServiceActionReturnsPermissionDiagnostic(t *testing.T) {
	host := &fakeDockerHost{controlErr: CapabilityDetailErr("permission_denied", "Docker 服务操作失败", "polkit denied", errors.New("exit 1"))}
	m := newDockerTestManager(t, http.NotFoundHandler(), host, &fakeDockerStorage{daemon: []byte(`{}`), disk: DockerDiskSummary{AvailableBytes: 100}})

	_, err := m.ServiceAction(context.Background(), DockerServiceActionRequest{Action: "stop"})
	require.Error(t, err)
	ae, ok := AsError(err)
	require.True(t, ok)
	assert.Equal(t, "permission_denied", ae.Reason)
	assert.Equal(t, "polkit denied", ae.Detail)
}

func TestDockerServiceActionRechecksDaemonState(t *testing.T) {
	host := &fakeDockerHost{status: DockerServiceSummary{State: DockerServiceStopped, Installed: true, ControlSupported: true}}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if host.Status(r.Context()).State != DockerServiceRunning {
			http.Error(w, "daemon down", http.StatusServiceUnavailable)
			return
		}
		switch r.URL.Path {
		case "/info":
			_ = json.NewEncoder(w).Encode(engineInfo{DockerRootDir: "/docker", Containers: 1, ContainersRunning: 1})
		case "/containers/json":
			_ = json.NewEncoder(w).Encode([]engineContainer{{ID: "one", State: "running"}})
		default:
			http.NotFound(w, r)
		}
	})
	storage := &fakeDockerStorage{daemon: []byte(`{"data-root":"/docker"}`), disk: DockerDiskSummary{AvailableBytes: 100}}
	m := newDockerTestManager(t, handler, host, storage)

	overview, err := m.ServiceAction(context.Background(), DockerServiceActionRequest{Action: "start"})
	require.NoError(t, err)
	assert.Equal(t, DockerServiceRunning, overview.Service.State)
	assert.Equal(t, 1, overview.Containers.Running)
	host.mu.Lock()
	assert.Equal(t, []string{"start"}, host.actions)
	host.mu.Unlock()
}
