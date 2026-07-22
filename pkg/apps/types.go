package apps

import "time"

// AppInfo 已部署应用信息（返回给前端）
type AppInfo struct {
	ID         string        `json:"id"`
	Name       string        `json:"name"`
	Namespace  string        `json:"namespace"`
	Kind       string        `json:"kind"`  // "app" | "system"
	State      string        `json:"state"` // "running" | "stopped" | "error" | "pending"
	Replicas   int32         `json:"replicas"`
	Ready      int32         `json:"ready"`
	Image      string        `json:"image"`
	Version    string        `json:"version"`
	CreatedAt  time.Time     `json:"createdAt"`
	Ports      []PortMapping `json:"ports,omitempty"`
	CPURequest string        `json:"cpuRequest,omitempty"`
	CPULimit   string        `json:"cpuLimit,omitempty"`
	MemRequest string        `json:"memRequest,omitempty"`
	MemLimit   string        `json:"memLimit,omitempty"`
}

// PortMapping 端口映射
type PortMapping struct {
	Name          string `json:"name"`
	ContainerPort int32  `json:"containerPort"`
	HostPort      int32  `json:"hostPort,omitempty"`
	Protocol      string `json:"protocol"`
	NodePort      int32  `json:"nodePort,omitempty"`
}

// AppOperation 应用操作请求
type AppOperation struct {
	Operation string `json:"operation"` // "start" | "stop" | "restart"
}

// StoreApp 应用商店条目（列表）。
//
// 向前兼容：旧字段（id/name/category/version/description/icon/provider/
// versionCount/installed）保持不变，旧 UI 自然忽略新增字段。
// 阶段4 新增 runtime/runtimes/installable/notInstallableReason/pinned：
//   - runtime   该包在本机首选运行时（compose | kubernetes）；列表层面为粗判，
//               权威值以 GET /store/version 为准（详情页拉取）。
//   - runtimes  该包在 catalog 声明支持的所有运行时。
//   - installable        本机当前是否可安装（仅 compose 包 + Docker 可用为 true）。
//   - notInstallableReason 不可安装的人读原因（如「仅 Kubernetes 环境支持」）。
type StoreApp struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Category     string `json:"category"`
	Version      string `json:"version"`
	Description  string `json:"description"`
	Icon         string `json:"icon"`
	Provider     string `json:"provider"`
	VersionCount int    `json:"versionCount"`
	Installed    bool   `json:"installed"`

	Runtime            RuntimeKind `json:"runtime,omitempty"`
	Runtimes           []string    `json:"runtimes,omitempty"`
	Installable        bool        `json:"installable"`
	NotInstallableReason string   `json:"notInstallableReason,omitempty"`
	Pinned             bool        `json:"pinned,omitempty"`
}

// DeployRequest 部署请求
type DeployRequest struct {
	AppID     string            `json:"appId"`
	Name      string            `json:"name"`
	Namespace string            `json:"namespace,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
}
