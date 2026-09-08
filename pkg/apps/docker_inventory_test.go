package apps

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 网络/卷只读清单（T3）：正常清单 + 字段映射 + 容器关联数聚合。
func TestDockerNetworksListsAndCountsContainers(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/networks":
			_ = json.NewEncoder(w).Encode([]engineNetwork{
				{Name: "bridge", ID: "n1", Driver: "bridge", Scope: "local"},
				{Name: "alpha_default", ID: "n2", Driver: "bridge", Scope: "local"},
				{Name: "backend", ID: "n3", Driver: "overlay", Scope: "swarm", Internal: true},
			})
		case "/containers/json":
			_, _ = w.Write(mustJSON(t, []map[string]any{
				{"Id": "1", "State": "running", "NetworkSettings": map[string]any{"Networks": map[string]any{"alpha_default": map[string]any{}}}},
				{"Id": "2", "State": "exited", "NetworkSettings": map[string]any{"Networks": map[string]any{"alpha_default": map[string]any{}, "bridge": map[string]any{}}}},
			}))
		default:
			http.NotFound(w, r)
		}
	})
	m := newDockerTestManager(t, handler, &fakeDockerHost{}, &fakeDockerStorage{})
	m.now = func() time.Time { return time.Unix(100, 0) }

	list, err := m.Networks(context.Background())
	require.NoError(t, err)
	assert.True(t, list.Available)
	require.Len(t, list.Networks, 3)
	// 按名称排序
	assert.Equal(t, DockerNetworkSummary{Name: "alpha_default", Driver: "bridge", Scope: "local", Containers: 2}, list.Networks[0])
	assert.Equal(t, DockerNetworkSummary{Name: "backend", Driver: "overlay", Scope: "swarm", Internal: true, Containers: 0}, list.Networks[1])
	assert.Equal(t, DockerNetworkSummary{Name: "bridge", Driver: "bridge", Scope: "local", Containers: 1}, list.Networks[2])
	assert.Equal(t, time.Unix(100, 0), list.CheckedAt)
}

func TestDockerVolumesListsWithComposeProjectAndCounts(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/volumes":
			_, _ = w.Write(mustJSON(t, map[string]any{"Volumes": []map[string]any{
				{"Name": "alpha_db", "Driver": "local", "Mountpoint": "/var/lib/docker/volumes/alpha_db/_data",
					"Labels": map[string]string{"com.docker.compose.project": "alpha"}},
				{"Name": "scratch", "Driver": "local", "Mountpoint": "/var/lib/docker/volumes/scratch/_data"},
			}}))
		case "/containers/json":
			_, _ = w.Write(mustJSON(t, []map[string]any{
				{"Id": "1", "State": "running", "Mounts": []map[string]any{{"Type": "volume", "Name": "alpha_db"}}},
				{"Id": "2", "State": "exited", "Mounts": []map[string]any{
					{"Type": "volume", "Name": "alpha_db"},
					{"Type": "bind", "Name": ""},
				}},
			}))
		default:
			http.NotFound(w, r)
		}
	})
	m := newDockerTestManager(t, handler, &fakeDockerHost{}, &fakeDockerStorage{})

	list, err := m.Volumes(context.Background())
	require.NoError(t, err)
	assert.True(t, list.Available)
	require.Len(t, list.Volumes, 2)
	assert.Equal(t, DockerVolumeSummary{
		Name: "alpha_db", Driver: "local", Mountpoint: "/var/lib/docker/volumes/alpha_db/_data",
		Containers: 2, ComposeProject: "alpha",
	}, list.Volumes[0])
	assert.Equal(t, DockerVolumeSummary{
		Name: "scratch", Driver: "local", Mountpoint: "/var/lib/docker/volumes/scratch/_data",
	}, list.Volumes[1])
}

// daemon 不可用：返回 available:false + 诊断，不返回 error（对齐 overview/stats 空态约定）。
func TestDockerNetworksVolumesUnavailableEmptyState(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "cannot connect to daemon", http.StatusInternalServerError)
	})
	m := newDockerTestManager(t, handler, &fakeDockerHost{}, &fakeDockerStorage{})

	networks, err := m.Networks(context.Background())
	require.NoError(t, err)
	assert.False(t, networks.Available)
	assert.Contains(t, networks.Diagnostic, "Docker daemon 不可用")
	assert.Empty(t, networks.Networks)

	volumes, err := m.Volumes(context.Background())
	require.NoError(t, err)
	assert.False(t, volumes.Available)
	assert.Contains(t, volumes.Diagnostic, "Docker daemon 不可用")
	assert.Empty(t, volumes.Volumes)
}

// 清单可得但容器列表失败：清单仍返回（容器数 0），带降级诊断。
func TestDockerNetworksContainerCountDegradesGracefully(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/networks":
			_ = json.NewEncoder(w).Encode([]engineNetwork{{Name: "bridge", Driver: "bridge", Scope: "local"}})
		default:
			http.Error(w, "boom", http.StatusInternalServerError)
		}
	})
	m := newDockerTestManager(t, handler, &fakeDockerHost{}, &fakeDockerStorage{})

	list, err := m.Networks(context.Background())
	require.NoError(t, err)
	assert.True(t, list.Available)
	require.Len(t, list.Networks, 1)
	assert.Equal(t, 0, list.Networks[0].Containers)
	assert.Contains(t, list.Diagnostic, "容器关联数不可用")
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return b
}
