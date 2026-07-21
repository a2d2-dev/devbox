//go:build integration

// 真实 Docker 集成测试（默认不跑）。需 Docker daemon 可达：
//
//	go test -tags=integration -run TestComposeE2E ./pkg/apps/ -timeout 300s
//
// 环境通过 DOCKER_HOST 指定 daemon（unix socket 或 tcp）。若不可达则 t.Skip。
package apps

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// e2eYAML 最小多 service Compose（alpine:3 通常已缓存，避免外网拉取阻塞）。
const e2eYAML = `services:
  one:
    image: alpine:3
    command: ["sleep", "3600"]
  two:
    image: alpine:3
    command: ["sleep", "3600"]
`

func skipIfNoDocker(t *testing.T, endpoint string) {
	t.Helper()
	e := newDockerEngine(endpoint)
	if err := e.ping(context.Background()); err != nil {
		t.Skipf("docker 不可达，跳过集成测试: %v", err)
	}
}

func waitTaskTerminal(t *testing.T, ctrl Controller, taskID string, timeout time.Duration) Task {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last Task
	for time.Now().Before(deadline) {
		tk, err := ctrl.GetTask(context.Background(), taskID)
		require.NoError(t, err)
		last = tk
		if tk.Status.IsTerminal() {
			return tk
		}
		time.Sleep(500 * time.Millisecond)
	}
	return last
}

func waitPhase(t *testing.T, ctrl Controller, appID string, want Phase, timeout time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		a, err := ctrl.Get(context.Background(), appID)
		if err == nil && a.Observed.Phase == want {
			return true
		}
		time.Sleep(500 * time.Millisecond)
	}
	return false
}

func TestComposeE2E(t *testing.T) {
	endpoint := os.Getenv("DEVBOX_TEST_DOCKER") // 可显式覆盖；否则读 DOCKER_HOST
	skipIfNoDocker(t, endpoint)

	dir := t.TempDir()
	repo, err := OpenRepository(context.Background(), filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	defer repo.Close()
	paths := NewPaths(dir)

	compose := NewComposeRuntime(endpoint, paths, zap.NewNop())
	adapters := map[RuntimeKind]runtimeAdapter{RuntimeCompose: compose}
	worker := NewWorker(repo, adapters, paths, zap.NewNop())
	worker.ctx = context.Background()
	ctrl := NewController(repo, paths, adapters, worker, zap.NewNop())

	appID := "e2e-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	t.Cleanup(func() {
		// 兜底清理（remove 幂等；测试中途失败也保证不残留容器）。
		if rmTask, err := ctrl.Remove(context.Background(), appID, RemoveOptions{Actor: "test"}); err == nil {
			waitTaskTerminal(t, ctrl, rmTask.ID, 60*time.Second)
		}
	})

	// 1. Apply（创建 + up）。
	task, err := ctrl.Apply(context.Background(), DesiredApplication{
		Name: appID, Source: ApplicationSource{Kind: SourceInline}, ComposeContent: e2eYAML,
	}, ApplyOptions{Actor: "test"})
	require.NoError(t, err)
	task = waitTaskTerminal(t, ctrl, task.ID, 180*time.Second)
	assert.Equal(t, TaskSucceeded, task.Status, "apply 应成功；message=%s", task.Message)

	// 2. 观测到 running（2 services）。
	require.True(t, waitPhase(t, ctrl, appID, PhaseRunning, 30*time.Second), "应进入 running")
	app, err := ctrl.Get(context.Background(), appID)
	require.NoError(t, err)
	assert.Equal(t, RuntimeCompose, app.Runtime)
	assert.Len(t, app.Observed.Services, 2)
	assert.Equal(t, int32(2), app.Replicas)

	// 3. compose 文件事实源 + revision。
	c, err := ctrl.GetCompose(context.Background(), appID)
	require.NoError(t, err)
	assert.Contains(t, c.Compose, "alpine:3")
	revs, err := ctrl.ListRevisions(context.Background(), appID)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(revs), 1)

	// 4. 日志接口不报错。
	page, err := ctrl.Logs(context.Background(), appID, LogOptions{Tail: 50})
	require.NoError(t, err)
	assert.Equal(t, appID, page.AppID)

	// 5. stop → stopped。
	stopTask, err := ctrl.Operate(context.Background(), appID, ActionStop, OperationOptions{Actor: "test"})
	require.NoError(t, err)
	require.Equal(t, TaskSucceeded, waitTaskTerminal(t, ctrl, stopTask.ID, 60*time.Second).Status)
	require.True(t, waitPhase(t, ctrl, appID, PhaseStopped, 30*time.Second), "stop 后应 stopped")

	// 6. start → running。
	startTask, err := ctrl.Operate(context.Background(), appID, ActionStart, OperationOptions{Actor: "test"})
	require.NoError(t, err)
	require.Equal(t, TaskSucceeded, waitTaskTerminal(t, ctrl, startTask.ID, 60*time.Second).Status)
	require.True(t, waitPhase(t, ctrl, appID, PhaseRunning, 30*time.Second), "start 后应 running")

	// 7. remove（默认保留数据）→ 容器消失 + 元数据清理。
	rmTask, err := ctrl.Remove(context.Background(), appID, RemoveOptions{Actor: "test"})
	require.NoError(t, err)
	require.Equal(t, TaskSucceeded, waitTaskTerminal(t, ctrl, rmTask.ID, 60*time.Second).Status)
	time.Sleep(time.Second) // 让 Observe 稳定
	_, err = ctrl.Get(context.Background(), appID)
	assert.Error(t, err, "remove 后 app 应不可见")
}
