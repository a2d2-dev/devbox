//go:build integration

// 真实 Docker E2E：系统 Compose project 自动发现 → 接管 → 编辑部署 → 进程重启恢复，
// named volume 数据不变；完全 down 且容器删除后不再自动发现。需 Docker daemon 可达：
//
//	go test -tags=integration -run TestTakeoverE2E ./pkg/apps/ -timeout 600s
package apps

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func takeoverTestEndpoint() string {
	endpoint := strings.TrimSpace(os.Getenv("DEVBOX_TEST_DOCKER"))
	if endpoint == "" {
		endpoint = strings.TrimSpace(os.Getenv("DOCKER_HOST"))
	}
	if endpoint == "" {
		endpoint = "unix://" + defaultDockerSocket
	} else if !strings.Contains(endpoint, "://") {
		endpoint = "unix://" + endpoint
	}
	return endpoint
}

func dockerComposeArgs(endpoint, projectDir, project string, files []string, sub ...string) []string {
	args := []string{"-H", endpoint, "compose", "--project-directory", projectDir, "-p", project}
	for _, f := range files {
		args = append(args, "-f", f)
	}
	return append(args, sub...)
}

func runDockerCompose(t *testing.T, endpoint, projectDir, project string, files, sub []string, timeout time.Duration) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", dockerComposeArgs(endpoint, projectDir, project, files, sub...)...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func writeNamedVolumeMarker(t *testing.T, endpoint, project, marker string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", "-H", endpoint, "run", "--rm",
		"-e", "DEVBOX_E2E_MARKER="+marker, "-v", project+"_data:/data",
		"alpine:3", "sh", "-c", `printf %s "$DEVBOX_E2E_MARKER" > /data/marker.txt`)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "write marker: %s", string(out))
}

func readNamedVolumeMarker(t *testing.T, endpoint, project string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	// 目标机的 Docker TCP 代理可能吞掉 `docker run` stdout；按项目运维准则使用
	// docker create + docker cp 读取，不依赖远端 stdout。
	container := project + "-marker-reader"
	_, _ = exec.CommandContext(ctx, "docker", "-H", endpoint, "rm", "-f", container).CombinedOutput()
	out, err := exec.CommandContext(ctx, "docker", "-H", endpoint, "create", "--name", container,
		"-v", project+"_data:/data", "alpine:3").CombinedOutput()
	require.NoError(t, err, "create marker reader: %s", string(out))
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		_, _ = exec.CommandContext(cleanupCtx, "docker", "-H", endpoint, "rm", "-f", container).CombinedOutput()
	})
	tmp := t.TempDir()
	markerFile := filepath.Join(tmp, "marker.txt")
	out, err = exec.CommandContext(ctx, "docker", "-H", endpoint, "cp",
		container+":/data/marker.txt", markerFile).CombinedOutput()
	require.NoError(t, err, "copy marker: %s", string(out))
	body, err := os.ReadFile(markerFile)
	require.NoError(t, err)
	return strings.TrimSpace(string(body))
}

func findDiscoveredProject(t *testing.T, ctrl Controller, project string) (Application, bool) {
	t.Helper()
	list, err := ctrl.List(context.Background(), Filter{})
	require.NoError(t, err)
	for _, app := range list {
		if app.Ownership == OwnershipDiscovered && app.Discovered != nil && app.Discovered.Project == project {
			return app, true
		}
	}
	return Application{}, false
}

func TestTakeoverE2E(t *testing.T) {
	endpoint := takeoverTestEndpoint()
	skipIfNoDocker(t, endpoint)

	// 1. 直接用 docker compose 起一个唯一外部 project（不触碰现有 project）。
	work := filepath.Join(t.TempDir(), "project")
	require.NoError(t, os.MkdirAll(work, 0o755))
	project := "extakeover" + strconv.FormatInt(time.Now().UnixNano(), 36)
	composeBody := "services:\n  web:\n    image: alpine:3\n    command: [\"sleep\", \"3600\"]\n    volumes:\n      - data:/data\nvolumes:\n  data: {}\n"
	composeFile := filepath.Join(work, "compose.yaml")
	require.NoError(t, os.WriteFile(composeFile, []byte(composeBody), 0o644))
	out, err := runDockerCompose(t, endpoint, work, project, []string{composeFile}, []string{"up", "-d"}, 180*time.Second)
	require.NoError(t, err, "external compose up: %s", out)
	t.Cleanup(func() {
		_, _ = runDockerCompose(t, endpoint, work, project, []string{composeFile}, []string{"down", "-v", "--remove-orphans"}, 120*time.Second)
	})
	marker := "preserve-" + project
	writeNamedVolumeMarker(t, endpoint, project, marker)

	// 2. 第一代 controller 发现并接管。
	dataDir := t.TempDir()
	dbPath := filepath.Join(dataDir, "apps.db")
	repo1, err := OpenRepository(context.Background(), dbPath)
	require.NoError(t, err)
	paths := NewPaths(dataDir)
	runtime1 := NewComposeRuntime(endpoint, paths, zap.NewNop())
	adapters1 := map[RuntimeKind]runtimeAdapter{RuntimeCompose: runtime1}
	workerCtx1, cancelWorker1 := context.WithCancel(context.Background())
	worker1 := NewWorker(repo1, adapters1, paths, zap.NewNop())
	worker1.Start(workerCtx1)
	ctrl1 := NewController(repo1, paths, adapters1, worker1, zap.NewNop(),
		WithPrechecker(runtime1), WithTakeoverPrechecker(runtime1))

	var discovered Application
	require.Eventually(t, func() bool {
		var ok bool
		discovered, ok = findDiscoveredProject(t, ctrl1, project)
		return ok && discovered.Discovered.TakeoverAvailable
	}, 30*time.Second, time.Second, "external project should be discoverable")
	app, err := ctrl1.Takeover(context.Background(), TakeoverRequest{ID: discovered.ID}, ApplyOptions{Actor: "test"})
	require.NoError(t, err)
	assert.Equal(t, OwnershipManaged, app.Ownership)
	meta, ok, err := repo1.GetAppMeta(context.Background(), app.ID)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, project, meta.OriginalProject)

	// 3. 真正编辑定义并 Apply；worker 必须仍使用原 project，且 named volume 数据保留。
	editedCompose := strings.Replace(composeBody, "command: [\"sleep\", \"3600\"]",
		"command: [\"sleep\", \"3600\"]\n    labels:\n      devbox.e2e: edited", 1)
	applyTask, err := ctrl1.Apply(context.Background(), DesiredApplication{
		ID: app.ID, Name: app.Name, Source: ApplicationSource{Kind: SourceLocal},
		ComposeContent: editedCompose, ExpectedRevision: app.Revision,
	}, ApplyOptions{Actor: "test"})
	require.NoError(t, err)
	require.Equal(t, TaskSucceeded, waitTaskTerminal(t, ctrl1, applyTask.ID, 180*time.Second).Status)
	assert.Equal(t, marker, readNamedVolumeMarker(t, endpoint, project))

	ops, err := ctrl1.ListOperations(context.Background(), app.ID)
	require.NoError(t, err)
	foundTakeover := false
	for _, op := range ops {
		if op.Type == TaskTakeover && op.Status == TaskSucceeded {
			foundTakeover = true
			break
		}
	}
	assert.True(t, foundTakeover, "operation history should include successful takeover")

	// 4. 模拟进程重启：停止旧 worker/关闭 DB，重建完整 controller 后仍能观测和操作原 project。
	cancelWorker1()
	require.NoError(t, repo1.Close())
	repo2, err := OpenRepository(context.Background(), dbPath)
	require.NoError(t, err)
	defer repo2.Close()
	runtime2 := NewComposeRuntime(endpoint, paths, zap.NewNop())
	adapters2 := map[RuntimeKind]runtimeAdapter{RuntimeCompose: runtime2}
	workerCtx2, cancelWorker2 := context.WithCancel(context.Background())
	defer cancelWorker2()
	worker2 := NewWorker(repo2, adapters2, paths, zap.NewNop())
	worker2.Start(workerCtx2)
	ctrl2 := NewController(repo2, paths, adapters2, worker2, zap.NewNop(),
		WithPrechecker(runtime2), WithTakeoverPrechecker(runtime2))
	recovered, err := ctrl2.Get(context.Background(), app.ID)
	require.NoError(t, err)
	assert.Equal(t, project, recovered.RuntimeProject)
	assert.Equal(t, OwnershipManaged, recovered.Ownership)
	restartTask, err := ctrl2.Operate(context.Background(), app.ID, ActionRestart, OperationOptions{Actor: "test"})
	require.NoError(t, err)
	require.Equal(t, TaskSucceeded, waitTaskTerminal(t, ctrl2, restartTask.ID, 180*time.Second).Status)
	assert.Equal(t, marker, readNamedVolumeMarker(t, endpoint, project))

	// 5. 完全 down 的独立无数据 project：容器记录删除后按定义不再自动发现。
	downWork := filepath.Join(t.TempDir(), "down-project")
	require.NoError(t, os.MkdirAll(downWork, 0o755))
	downProject := "extdown" + strconv.FormatInt(time.Now().UnixNano(), 36)
	downFile := filepath.Join(downWork, "compose.yaml")
	require.NoError(t, os.WriteFile(downFile, []byte("services:\n  app:\n    image: alpine:3\n    command: [\"sleep\", \"3600\"]\n"), 0o644))
	out, err = runDockerCompose(t, endpoint, downWork, downProject, []string{downFile}, []string{"up", "-d"}, 180*time.Second)
	require.NoError(t, err, "boundary compose up: %s", out)
	_, seenBeforeDown := findDiscoveredProject(t, ctrl2, downProject)
	assert.True(t, seenBeforeDown)
	out, err = runDockerCompose(t, endpoint, downWork, downProject, []string{downFile}, []string{"down", "--remove-orphans"}, 120*time.Second)
	require.NoError(t, err, "boundary compose down: %s", out)
	require.Eventually(t, func() bool {
		_, found := findDiscoveredProject(t, ctrl2, downProject)
		return !found
	}, 30*time.Second, time.Second, "fully down project should no longer be discoverable")
}
