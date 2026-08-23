package apps

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
	daemon     []byte
	disk       DockerDiskSummary
	dirSize    uint64
	targetErr  error
	written    []byte
	openTarget string
}

func (s *fakeDockerStorage) ReadDaemonJSON() ([]byte, error) { return s.daemon, nil }
func (s *fakeDockerStorage) WriteDaemonJSON(b []byte, _ string) (func() error, error) {
	s.written = append([]byte(nil), b...)
	return func() error { return nil }, nil
}
func (s *fakeDockerStorage) Disk(string) (DockerDiskSummary, error) { return s.disk, nil }
func (s *fakeDockerStorage) DirSize(string) (uint64, error)         { return s.dirSize, nil }
func (s *fakeDockerStorage) EnsureTargetReady(string, bool) error   { return s.targetErr }
func (s *fakeDockerStorage) OpenTarget(string) (*os.File, error) {
	if s.openTarget == "" {
		dir, err := os.MkdirTemp("/dev/shm", "devbox-docker-target-*")
		if err != nil {
			return nil, err
		}
		s.openTarget = dir
	}
	return os.Open(s.openTarget)
}

type fakeDockerRunner struct {
	runs       []string
	err        error
	rsyncStart chan struct{}
	rsyncDone  chan struct{}
}

type fdCheckingDockerRunner struct {
	t            *testing.T
	target       string
	movedTarget  string
	destination  string
	openedTarget string
}

func (r *fdCheckingDockerRunner) LookPath(name string) (string, error) {
	return "/usr/bin/" + name, nil
}
func (r *fdCheckingDockerRunner) Run(context.Context, string, ...string) (string, error) {
	return "", errors.New("rsync target fd was not passed to command runner")
}
func (r *fdCheckingDockerRunner) RunWithFiles(_ context.Context, name string, files []*os.File, args ...string) (string, error) {
	require.Equal(r.t, "rsync", name)
	require.Len(r.t, files, 1)
	require.NotEmpty(r.t, args)
	r.destination = args[len(args)-1]
	require.NoError(r.t, os.Rename(r.target, r.movedTarget))
	require.NoError(r.t, os.Symlink("/etc", r.target))
	opened, err := os.Readlink(fmt.Sprintf("/proc/self/fd/%d", files[0].Fd()))
	require.NoError(r.t, err)
	r.openedTarget = opened
	return "", nil
}

func (r *fakeDockerRunner) LookPath(name string) (string, error) {
	if name == "rsync" && r.err != nil {
		return "", r.err
	}
	return "/usr/bin/" + name, nil
}
func (r *fakeDockerRunner) Run(_ context.Context, name string, args ...string) (string, error) {
	r.runs = append(r.runs, name+" "+stringsJoin(args))
	if name == "rsync" && r.rsyncStart != nil {
		close(r.rsyncStart)
		<-r.rsyncDone
	}
	return "", r.err
}
func (r *fakeDockerRunner) RunWithFiles(ctx context.Context, name string, _ []*os.File, args ...string) (string, error) {
	return r.Run(ctx, name, args...)
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
	if fake, ok := storage.(*fakeDockerStorage); ok {
		t.Cleanup(func() {
			if fake.openTarget != "" {
				_ = os.RemoveAll(fake.openTarget)
			}
		})
	}
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
	assert.Equal(t, DockerServiceUnreachable, o.Service.State)
	assert.Equal(t, DockerCountSummary{}, o.Containers)
	assert.Equal(t, DockerCountSummary{}, o.ComposeProjects)
	assert.Equal(t, "空闲", o.IdleSummary)
	assert.Contains(t, o.Service.Diagnostic, "systemd reports active")
	assert.True(t, o.Storage.Valid)
}

func TestDockerOverviewMarksDaemonDefaultDataRootUnconfiguredWhileRunning(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/info":
			_ = json.NewEncoder(w).Encode(engineInfo{DockerRootDir: defaultDockerDataRoot})
		case "/containers/json":
			_ = json.NewEncoder(w).Encode([]engineContainer{})
		default:
			http.NotFound(w, r)
		}
	})
	m := newDockerTestManager(t, handler,
		&fakeDockerHost{status: DockerServiceSummary{State: DockerServiceRunning, Installed: true}},
		&fakeDockerStorage{daemon: []byte(`{}`), disk: DockerDiskSummary{AvailableBytes: 100}},
	)

	overview, err := m.Overview(context.Background())
	require.NoError(t, err)
	assert.Equal(t, DockerServiceRunning, overview.Service.State)
	assert.Equal(t, defaultDockerDataRoot, overview.Storage.Path)
	assert.False(t, overview.Storage.Configured)
	assert.True(t, overview.Storage.Valid, "running daemon storage remains observable")
}

func TestDockerOverviewClassifiesDaemonErrors(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		hostState  DockerServiceState
		wantState  DockerServiceState
	}{
		{name: "permission denied", statusCode: http.StatusForbidden, hostState: DockerServiceRunning, wantState: DockerServicePermissionDenied},
		{name: "unreachable", statusCode: http.StatusServiceUnavailable, hostState: DockerServiceRunning, wantState: DockerServiceUnreachable},
		{name: "systemd inactive", statusCode: http.StatusServiceUnavailable, hostState: DockerServiceStopped, wantState: DockerServiceStopped},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, http.StatusText(tt.statusCode), tt.statusCode)
			})
			m := newDockerTestManager(t, handler,
				&fakeDockerHost{status: DockerServiceSummary{State: tt.hostState, Installed: true}},
				&fakeDockerStorage{daemon: []byte(`{"data-root":"/data/docker"}`), disk: DockerDiskSummary{AvailableBytes: 100}},
			)

			overview, err := m.Overview(context.Background())
			require.NoError(t, err)
			assert.Equal(t, tt.wantState, overview.Service.State)
		})
	}
}

func TestDockerOverviewAppliesWholeRequestTimeout(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})
	m := newDockerTestManager(t, handler,
		&fakeDockerHost{status: DockerServiceSummary{State: DockerServiceRunning, Installed: true}},
		&fakeDockerStorage{daemon: []byte(`{"data-root":"/data/docker"}`), disk: DockerDiskSummary{AvailableBytes: 100}},
	)
	m.overviewTimeout = 25 * time.Millisecond

	started := time.Now()
	overview, err := m.Overview(context.Background())
	require.NoError(t, err)
	assert.Less(t, time.Since(started), 250*time.Millisecond)
	assert.Equal(t, DockerServiceTimeout, overview.Service.State)
}

func TestDockerServiceStartRequiresExplicitDataRoot(t *testing.T) {
	host := &fakeDockerHost{status: DockerServiceSummary{State: DockerServiceStopped, Installed: true, ControlSupported: true}}
	m := newDockerTestManager(t, http.NotFoundHandler(), host,
		&fakeDockerStorage{daemon: []byte(`{}`), disk: DockerDiskSummary{AvailableBytes: 100}},
	)

	_, err := m.ServiceAction(context.Background(), DockerServiceActionRequest{Action: "start"})
	require.Error(t, err)
	ae, ok := AsError(err)
	require.True(t, ok)
	assert.Equal(t, "storage_unconfigured", ae.Reason)
	assert.Contains(t, ae.Message, "存储设置")
	host.mu.Lock()
	assert.Empty(t, host.actions)
	host.mu.Unlock()
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

func TestDockerStatsBudgetStartsBeforeContainerListing(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/containers/json" {
			select {
			case <-time.After(150 * time.Millisecond):
				_ = json.NewEncoder(w).Encode([]engineContainer{})
			case <-r.Context().Done():
			}
			return
		}
		http.NotFound(w, r)
	})
	m := newDockerTestManager(t, handler, &fakeDockerHost{}, &fakeDockerStorage{})
	m.statsTimeout = 25 * time.Millisecond

	started := time.Now()
	stats, err := m.Stats(context.Background())
	require.NoError(t, err)
	assert.Less(t, time.Since(started), 100*time.Millisecond)
	assert.False(t, stats.Available)
	assert.Contains(t, stats.Diagnostic, "deadline")
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

	plan, err := m.MigrationPlan(context.Background(), DockerMigrationRequest{TargetPath: "/data/new"})
	require.NoError(t, err)
	assert.NotEmpty(t, plan.ID)
	assert.Equal(t, "/old", plan.SourcePath)
	assert.Equal(t, "/data/new", plan.TargetPath)
	assert.Equal(t, uint64(3000), plan.RequiredBytes)
	assert.Len(t, plan.Steps, 4)
	assert.JSONEq(t, `{"data-root":"/data/new","registry-mirrors":["https://registry.example"]}`, plan.ProposedDaemonJSON)

	_, err = m.ExecuteMigration(context.Background(), DockerMigrationExecuteRequest{TargetPath: "/data/new", PlanID: plan.ID})
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

	_, err := m.MigrationPlan(context.Background(), DockerMigrationRequest{TargetPath: "/data/new"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "空间不足")
}

func TestDockerMigrationPlanRejectsTargetOutsideAllowedRoots(t *testing.T) {
	allowedRoot := t.TempDir()
	outsideRoot := t.TempDir()
	require.NoError(t, os.Symlink(outsideRoot, filepath.Join(allowedRoot, "escape")))
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/info":
			_ = json.NewEncoder(w).Encode(engineInfo{DockerRootDir: "/old"})
		case "/containers/json":
			_ = json.NewEncoder(w).Encode([]engineContainer{})
		default:
			http.NotFound(w, r)
		}
	})
	m := newDockerTestManager(t, handler, &fakeDockerHost{}, &fakeDockerStorage{
		daemon: []byte(`{"data-root":"/old"}`), disk: DockerDiskSummary{AvailableBytes: 1000}, dirSize: 1,
	})
	m.allowedRoots = []string{allowedRoot}

	_, err := m.MigrationPlan(context.Background(), DockerMigrationRequest{TargetPath: filepath.Join(allowedRoot, "escape", "docker")})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "允许的存储根目录")
}

func TestDockerMigrationPlanRejectsSensitiveSystemPrefixes(t *testing.T) {
	m := newDockerTestManager(t, http.NotFoundHandler(), &fakeDockerHost{}, &fakeDockerStorage{})
	m.allowedRoots = []string{"/"}
	for _, target := range []string{"/run/docker-new", "/etc/docker-new", "/proc/docker-new", "/sys/docker-new", "/dev/docker-new"} {
		_, err := m.MigrationPlan(context.Background(), DockerMigrationRequest{TargetPath: target})
		require.Error(t, err, target)
		assert.Contains(t, err.Error(), "系统敏感目录", target)
	}
}

func TestDockerMigrationPinsOpenedTargetDirectoryAgainstSymlinkReplacement(t *testing.T) {
	allowedRoot := t.TempDir()
	source := filepath.Join(allowedRoot, "source")
	target := filepath.Join(allowedRoot, "target")
	movedTarget := filepath.Join(allowedRoot, "target-opened")
	require.NoError(t, os.Mkdir(source, 0o710))
	require.NoError(t, os.WriteFile(filepath.Join(source, "layer"), []byte("data"), 0o600))
	require.NoError(t, os.Mkdir(target, 0o710))
	daemonPath := filepath.Join(allowedRoot, "daemon.json")
	require.NoError(t, os.WriteFile(daemonPath, []byte(`{"data-root":"`+source+`"}`), 0o600))

	host := &fakeDockerHost{status: DockerServiceSummary{State: DockerServiceRunning, Installed: true, ControlSupported: true}}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host.mu.Lock()
		state := host.status.State
		actionCount := len(host.actions)
		host.mu.Unlock()
		if state != DockerServiceRunning {
			http.Error(w, "daemon down", http.StatusServiceUnavailable)
			return
		}
		root := source
		if actionCount > 1 {
			root = target
		}
		switch r.URL.Path {
		case "/info":
			_ = json.NewEncoder(w).Encode(engineInfo{DockerRootDir: root})
		case "/containers/json":
			_ = json.NewEncoder(w).Encode([]engineContainer{})
		default:
			http.NotFound(w, r)
		}
	})
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	runner := &fdCheckingDockerRunner{t: t, target: target, movedTarget: movedTarget}
	m := newDockerManagerWithDeps(
		&dockerEngine{baseURL: server.URL, client: server.Client()}, host,
		&osDockerStorage{daemonPath: daemonPath}, runner,
	)
	m.allowedRoots = []string{allowedRoot}

	plan, err := m.MigrationPlan(context.Background(), DockerMigrationRequest{TargetPath: target})
	require.NoError(t, err)
	_, err = m.ExecuteMigration(context.Background(), DockerMigrationExecuteRequest{
		TargetPath: plan.TargetPath, PlanID: plan.ID, Confirm: true,
	})
	require.NoError(t, err)
	assert.Equal(t, "/proc/self/fd/3/", runner.destination)
	assert.Equal(t, movedTarget, runner.openedTarget)
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

func TestDockerServiceActionRejectedWhileMigrationInProgress(t *testing.T) {
	host := &fakeDockerHost{status: DockerServiceSummary{State: DockerServiceRunning, Installed: true, ControlSupported: true}}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host.mu.Lock()
		state := host.status.State
		actions := append([]string(nil), host.actions...)
		host.mu.Unlock()
		if state != DockerServiceRunning {
			http.Error(w, "daemon down", http.StatusServiceUnavailable)
			return
		}
		root := "/old"
		if len(actions) > 1 {
			root = "/tmp/devbox-docker-new"
		}
		switch r.URL.Path {
		case "/info":
			_ = json.NewEncoder(w).Encode(engineInfo{DockerRootDir: root})
		case "/containers/json":
			_ = json.NewEncoder(w).Encode([]engineContainer{})
		default:
			http.NotFound(w, r)
		}
	})
	storage := &fakeDockerStorage{
		daemon:  []byte(`{"data-root":"/old"}`),
		disk:    DockerDiskSummary{AvailableBytes: 1000},
		dirSize: 100,
	}
	m := newDockerTestManager(t, handler, host, storage)
	m.allowedRoots = []string{"/tmp"}
	runner := &fakeDockerRunner{rsyncStart: make(chan struct{}), rsyncDone: make(chan struct{})}
	m.runner = runner

	plan, err := m.MigrationPlan(context.Background(), DockerMigrationRequest{TargetPath: "/tmp/devbox-docker-new"})
	require.NoError(t, err)
	migrationDone := make(chan error, 1)
	go func() {
		_, executeErr := m.ExecuteMigration(context.Background(), DockerMigrationExecuteRequest{
			TargetPath: plan.TargetPath,
			PlanID:     plan.ID,
			Confirm:    true,
		})
		migrationDone <- executeErr
	}()
	select {
	case <-runner.rsyncStart:
	case executeErr := <-migrationDone:
		t.Fatalf("migration returned before mocked rsync: %v", executeErr)
	case <-time.After(5 * time.Second):
		t.Fatal("migration did not reach mocked rsync")
	}

	_, err = m.ServiceAction(context.Background(), DockerServiceActionRequest{Action: "stop"})
	require.Error(t, err)
	ae, ok := AsError(err)
	require.True(t, ok)
	assert.Equal(t, "migration_in_progress", ae.Reason)

	close(runner.rsyncDone)
	require.NoError(t, <-migrationDone)
}
