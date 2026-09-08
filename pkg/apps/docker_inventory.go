package apps

import (
	"context"
	"sort"
	"time"
)

// Docker 网络 / 卷只读清单（容器域 IA 重组 T3）。
//
// 与 overview/stats 同分层：engine 只读端点（GET /networks、GET /volumes）
// → DockerManager 聚合 → controller 透传 → console handler。
// daemon 不可用是正常空态：返回 available:false + 诊断字符串（200，不 5xx），
// 对齐 docs/docker-overview.md 与 DockerStats 的现行为，避免前端轮询白屏。
//
// 关联容器数从 /containers/json?all=1 聚合：
//   - network：NetworkSettings.Networks 的键即容器接入的 network 名；
//   - volume：Mounts 中 Type=volume 的 Name。
// 一次容器列表调用同时服务两种聚合，比逐个 inspect 成本低且口径一致
//（计入停止容器，与 docker network/volume 的占用判定一致）。

type DockerNetworkSummary struct {
	Name       string `json:"name"`
	Driver     string `json:"driver"`
	Scope      string `json:"scope"`
	Internal   bool   `json:"internal"`
	Containers int    `json:"containers"`
}

type DockerNetworkList struct {
	Available  bool                   `json:"available"`
	Networks   []DockerNetworkSummary `json:"networks"`
	Diagnostic string                 `json:"diagnostic,omitempty"`
	CheckedAt  time.Time              `json:"checkedAt"`
}

type DockerVolumeSummary struct {
	Name           string `json:"name"`
	Driver         string `json:"driver"`
	Mountpoint     string `json:"mountpoint"`
	Containers     int    `json:"containers"`
	ComposeProject string `json:"composeProject,omitempty"`
}

type DockerVolumeList struct {
	Available  bool                  `json:"available"`
	Volumes    []DockerVolumeSummary `json:"volumes"`
	Diagnostic string                `json:"diagnostic,omitempty"`
	CheckedAt  time.Time             `json:"checkedAt"`
}

const composeProjectLabel = "com.docker.compose.project"

func (m *DockerManager) Networks(ctx context.Context) (DockerNetworkList, error) {
	ctx, cancel := context.WithTimeout(ctx, m.overviewTimeout)
	defer cancel()
	out := DockerNetworkList{CheckedAt: m.now()}
	networks, err := m.engine.listNetworks(ctx)
	if err != nil {
		out.Diagnostic = "Docker daemon 不可用: " + err.Error()
		return out, nil
	}
	out.Available = true
	counts := map[string]int{}
	if containers, err := m.engine.listContainers(ctx, nil); err == nil {
		for _, c := range containers {
			for name := range c.NetworkSettings.Networks {
				counts[name]++
			}
		}
	} else {
		out.Diagnostic = "容器关联数不可用: " + err.Error()
	}
	out.Networks = make([]DockerNetworkSummary, 0, len(networks))
	for _, n := range networks {
		out.Networks = append(out.Networks, DockerNetworkSummary{
			Name: n.Name, Driver: n.Driver, Scope: n.Scope, Internal: n.Internal,
			Containers: counts[n.Name],
		})
	}
	sort.Slice(out.Networks, func(i, j int) bool { return out.Networks[i].Name < out.Networks[j].Name })
	return out, nil
}

func (m *DockerManager) Volumes(ctx context.Context) (DockerVolumeList, error) {
	ctx, cancel := context.WithTimeout(ctx, m.overviewTimeout)
	defer cancel()
	out := DockerVolumeList{CheckedAt: m.now()}
	volumes, err := m.engine.listVolumes(ctx)
	if err != nil {
		out.Diagnostic = "Docker daemon 不可用: " + err.Error()
		return out, nil
	}
	out.Available = true
	counts := map[string]int{}
	if containers, err := m.engine.listContainers(ctx, nil); err == nil {
		for _, c := range containers {
			for _, mount := range c.Mounts {
				if mount.Type == "volume" && mount.Name != "" {
					counts[mount.Name]++
				}
			}
		}
	} else {
		out.Diagnostic = "容器关联数不可用: " + err.Error()
	}
	out.Volumes = make([]DockerVolumeSummary, 0, len(volumes))
	for _, v := range volumes {
		out.Volumes = append(out.Volumes, DockerVolumeSummary{
			Name: v.Name, Driver: v.Driver, Mountpoint: v.Mountpoint,
			Containers:     counts[v.Name],
			ComposeProject: v.Labels[composeProjectLabel],
		})
	}
	sort.Slice(out.Volumes, func(i, j int) bool { return out.Volumes[i].Name < out.Volumes[j].Name })
	return out, nil
}
