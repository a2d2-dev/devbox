package apps

import (
	"errors"
	"fmt"
	"time"
)

// 本文件定义 devbox 应用管理的稳定领域模型。
//
// 设计目标（Issue #2）：
//   - Application 是产品概念，不泄漏 K8s/Compose 实现细节；Namespace/PodName/ContainerID
//     等运行时诊断下沉到 runtime diagnostics，不进对外 schema 的核心字段。
//   - JSON 向前兼容：GET /api/v1/apps 返回 []Application，旧前端 useApps() 读取的
//     state/image/ports/replicas 等字段保持不变，新增字段（runtime/phase/services/...）
//     旧代码自然忽略。
//   - 一个应用 = 一个 Compose project（compose 运行时）或一个 Deployment（k8s 运行时）。

// RuntimeKind 运行时类型。
type RuntimeKind string

const (
	RuntimeCompose    RuntimeKind = "compose"
	RuntimeKubernetes RuntimeKind = "kubernetes"
)

// Phase 应用聚合状态（后端聚合，前端不自行推断）。
type Phase string

const (
	PhasePending   Phase = "pending"
	PhaseDeploying Phase = "deploying"
	PhaseRunning   Phase = "running"
	PhaseDegraded  Phase = "degraded"
	PhaseStopped   Phase = "stopped"
	PhaseFailed    Phase = "failed"
	PhaseRemoving  Phase = "removing"
	PhaseUnknown   Phase = "unknown"
)

// LegacyState 把新 Phase 映射回旧前端 useApps() 期望的 state 字段
// （running/stopped/error/pending），保证零改前端。
func (p Phase) LegacyState() string {
	switch p {
	case PhaseRunning:
		return "running"
	case PhaseStopped:
		return "stopped"
	case PhaseFailed:
		return "error"
	case PhasePending, PhaseDeploying:
		return "pending"
	default: // degraded/removing/unknown
		return "pending"
	}
}

// Action 生命周期动作。
type Action string

const (
	ActionStart    Action = "start"
	ActionStop     Action = "stop"
	ActionRestart  Action = "restart"
	ActionRedeploy Action = "redeploy"
)

// SourceKind 应用来源（来源是一等概念，不能压成单个 composeYaml string）。
type SourceKind string

const (
	SourceInline  SourceKind = "inline"  // 粘贴/上传 Compose YAML
	SourceStore   SourceKind = "store"   // 应用商店（edge-apiserver）
	SourceCatalog SourceKind = "catalog" // 第三方 HTTP/Git catalog source（阶段4 扩展）
	SourceLocal   SourceKind = "local"   // 导入本地已有项目（后续阶段）
	SourceGit     SourceKind = "git"     // 后续阶段
)

// ApplicationSource 描述应用的来源与可追溯版本。
type ApplicationSource struct {
	Kind    SourceKind `json:"kind"`
	StoreID string     `json:"storeId,omitempty"` // 商店/catalog 包 ID
	Version string     `json:"version,omitempty"` // 商店/catalog 包版本 / 来源版本
	// CatalogID 第三方 catalog source 标识（仅 SourceCatalog；用于来源筛选与升级路由）。
	CatalogID string `json:"catalogId,omitempty"`
}

// ServiceStatus 单个 service（→ container）的运行态。
type ServiceStatus struct {
	Name        string        `json:"name"`
	Image       string        `json:"image,omitempty"`
	State       string        `json:"state,omitempty"`       // created/running/restarting/...（容器状态）
	Health      string        `json:"health,omitempty"`      // healthy/unhealthy/starting/none
	Replicas    int32         `json:"replicas,omitempty"`    // k8s 副本意图
	Ready       int32         `json:"ready,omitempty"`       // k8s 就绪副本
	ContainerID string        `json:"containerId,omitempty"` // 诊断字段（compose）
	Ports       []PortMapping `json:"ports,omitempty"`
}

// Endpoint 对外入口。
type Endpoint struct {
	Name     string `json:"name,omitempty"`
	URL      string `json:"url"`
	Protocol string `json:"protocol,omitempty"` // http/https/tcp
	Port     int32  `json:"port,omitempty"`
}

// ObservedState 运行时观测到的状态（由 adapter Observe 填充）。
type ObservedState struct {
	Revision  int64           `json:"observedRevision"` // 已成功部署到的 revision
	Phase     Phase           `json:"phase"`
	Services  []ServiceStatus `json:"services,omitempty"`
	Endpoints []Endpoint      `json:"endpoints,omitempty"`
	Message   string          `json:"message,omitempty"` // 诊断/错误摘要
	// 旧前端兼容字段（K8s adapter 填充，compose 留空）：
	ContainerID   string     `json:"containerID,omitempty"`
	RestartCount  int32      `json:"restartCount,omitempty"`
	RestartPolicy string     `json:"restartPolicy,omitempty"`
	VolumeMounts  []string   `json:"volumeMounts,omitempty"`
	PodName       string     `json:"podName,omitempty"`
	NodeName      string     `json:"nodeName,omitempty"`
	StartedAt     *time.Time `json:"startedAt,omitempty"`
}

// Application 是对外稳定的应用对象（HTTP 返回它）。
//
// 旧字段（ID/Name/State/Image/Ports/...）保持兼容；新字段（Runtime/Source/Revision/
// Phase/Services/Endpoints/LastTask）供新 UI 使用。旧字段中 State 由 Observed.Phase
// 派生，在序列化前由 adapter/controller 填好。
type Application struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"` // "app" | "system"

	// 兼容旧 useApps/useAppDetail 读路径 —— 必须保留这些 JSON key。
	State      string        `json:"state"`
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
	Namespace  string        `json:"namespace,omitempty"`

	// 新字段（runtime 无关语义）。
	Runtime  RuntimeKind       `json:"runtime"`
	Source   ApplicationSource `json:"source"`
	Revision int64             `json:"revision"` // desired generation
	Observed ObservedState     `json:"observed"`
	LastTask *Task             `json:"lastTask,omitempty"`
}

// DesiredApplication 创建/更新请求体（PUT/POST /apps）。
type DesiredApplication struct {
	ID               string            `json:"id,omitempty"` // 更新时由路径提供；创建时可省略（自动生成）
	Name             string            `json:"name"`
	Source           ApplicationSource `json:"source"`
	ComposeContent   string            `json:"compose"`                    // inline Compose YAML（source=inline）
	Parameters       map[string]string `json:"parameters,omitempty"`       // 非敏感参数快照（进 revision）
	Secrets          map[string]string `json:"secrets,omitempty"`          // 仅写，永不回传/入 revision/audit
	ExpectedRevision int64             `json:"expectedRevision,omitempty"` // 乐观并发；更新时校验
}

// --- Task ---

// TaskStatus 任务生命周期。
type TaskStatus string

const (
	TaskQueued     TaskStatus = "queued"
	TaskRunning    TaskStatus = "running"
	TaskSucceeded  TaskStatus = "succeeded"
	TaskFailed     TaskStatus = "failed"
	TaskCanceled   TaskStatus = "canceled"
	TaskSuperseded TaskStatus = "superseded"
)

// IsTerminal 终态判定（崩溃恢复/清理用）。
func (s TaskStatus) IsTerminal() bool {
	switch s {
	case TaskSucceeded, TaskFailed, TaskCanceled, TaskSuperseded:
		return true
	}
	return false
}

// TaskPhase 任务执行阶段。
type TaskPhase string

const (
	PhaseTaskValidating    TaskPhase = "validating"
	PhaseTaskResolving     TaskPhase = "resolving"
	PhaseTaskPulling       TaskPhase = "pulling"
	PhaseTaskApplying      TaskPhase = "applying"
	PhaseTaskWaitingHealth TaskPhase = "waiting-health"
	PhaseTaskVerifying     TaskPhase = "verifying"
	PhaseTaskCleaningUp    TaskPhase = "cleaning-up"
)

// TaskType 任务类型。
type TaskType string

const (
	TaskApply   TaskType = "apply"   // 创建/更新
	TaskOperate TaskType = "operate" // start/stop/restart/redeploy
	TaskRemove  TaskType = "remove"  // 卸载
	TaskRestore TaskType = "restore" // 回滚到某 revision
)

// Task 异步变更任务。HTTP 写操作返回 202 + Task，前端轮询 GET /tasks/{id}。
type Task struct {
	ID             string     `json:"id"`
	AppID          string     `json:"appId"`
	Type           TaskType   `json:"type"`
	Action         Action     `json:"action,omitempty"` // operate 时的动作
	Status         TaskStatus `json:"status"`
	Phase          TaskPhase  `json:"phase,omitempty"`
	Revision       int64      `json:"revision,omitempty"` // 目标 revision（apply/restore）
	Purge          bool       `json:"purge,omitempty"`    // remove 是否删除受管数据
	IdempotencyKey string     `json:"idempotencyKey,omitempty"`
	// RequestSummary 是请求的脱敏摘要（供审计/前端展示），不含 secret。
	RequestSummary string     `json:"requestSummary,omitempty"`
	Message        string     `json:"message,omitempty"` // 进度/错误信息（已脱敏）
	CreatedAt      time.Time  `json:"createdAt"`
	StartedAt      *time.Time `json:"startedAt,omitempty"`
	FinishedAt     *time.Time `json:"finishedAt,omitempty"`
}

// --- Revision ---

// Revision 记录每次被接受的配置变更。secret 永不入 revision。
type Revision struct {
	Number      int64             `json:"number"`
	AppID       string            `json:"appId"`
	ComposeHash string            `json:"composeHash"` // Compose 内容 hash
	Source      ApplicationSource `json:"source"`
	Parameters  map[string]string `json:"parameters,omitempty"` // 非敏感参数快照
	CreatedAt   time.Time         `json:"createdAt"`
	CreatedBy   string            `json:"createdBy,omitempty"`
	Note        string            `json:"note,omitempty"`
}

// --- Options ---

// ApplyOptions Apply（创建/更新）的可选项。
type ApplyOptions struct {
	IdempotencyKey string
	// AllowRiskyConfirmation 表示调用方已对 confirmation 级风险显式确认（审计留痕）。
	AllowRiskyConfirmation bool
	Actor                  string // 审计：操作者
}

// OperationOptions Operate（start/stop/...）的可选项。
type OperationOptions struct {
	IdempotencyKey string
	Actor          string
}

// RemoveOptions Remove（卸载）的可选项。数据删除策略必须显式。
type RemoveOptions struct {
	IdempotencyKey string
	// Purge=true 时删除受管（managed）volume 与受管 bind 目录；
	// external volume 与非受管 bind 永远不删。
	Purge bool
	Actor string
}

// LogOptions 日志查询。
type LogOptions struct {
	Tail   int64 // 行数；0 表示不限（实际有上限）
	Since  time.Duration
	Follow bool // 本 MVP 不实现 follow 流，保留字段
}

// LogPage 日志结果。内容必须脱敏。
type LogPage struct {
	AppID     string `json:"appId"`
	Logs      string `json:"logs"`
	Truncated bool   `json:"truncated,omitempty"`
}

// Filter List 过滤。
type Filter struct {
	Runtime RuntimeKind // 空表示全部
}

// ComposeContent GET /apps/{id}/compose 返回。
type ComposeContent struct {
	AppID  string            `json:"appId"`
	Source ApplicationSource `json:"source"`
	// Compose 文本（事实源）。仅含非敏感渲染结果；secret 以引用形式存在。
	Compose  string `json:"compose"`
	Revision int64  `json:"revision"`
}

// --- Storage / Env / Remove 详情（Issue #2 要求 6/7）---
//
// 这些是「读事实源（compose.yaml + .env）静态推导」的稳定详情，供详情页展示与删除预览。
// 不查 docker daemon：事实源是 compose.yaml（可被 git/CLI 管理，卸载 devbox 后仍可用）。

// VolumeKind 卷/挂载分类。
type VolumeKind string

const (
	VolumeManaged  VolumeKind = "managed"  // 受管命名卷（compose project 创建；purge 可删）
	VolumeExternal VolumeKind = "external" // external:true（外部数据；永不删）
	VolumeBind     VolumeKind = "bind"     // 宿主路径挂载（生命周期由宿主管；不删）
	VolumeSocket   VolumeKind = "socket"   // 特权 socket（docker.sock 等；安装期已阻断，防御性标识）
)

// VolumeInfo 单个卷/挂载的详情。
type VolumeInfo struct {
	Kind      VolumeKind `json:"kind"`
	Source    string     `json:"source,omitempty"` // 命名卷名 / bind 宿主路径 / socket 路径
	Target    string     `json:"target,omitempty"` // 容器内挂载点
	External  bool       `json:"external"`         // external:true
	Managed   bool       `json:"managed"`          // devbox 受管（purge 可删）
	Deletable bool       `json:"deletable"`        // purge 时会被删除（仅 managed 命名卷）
}

// StorageInventory 应用存储清单（详情）。
type StorageInventory struct {
	AppID   string       `json:"appId"`
	Volumes []VolumeInfo `json:"volumes"`
	// ManagedDataDir 受管数据目录（compose.yaml/.env/revisions 所在），purge 时删除。
	ManagedDataDir string `json:"managedDataDir,omitempty"`
	// Note 生命周期说明（external 永不删等）。
	Note string `json:"note,omitempty"`
}

// EnvVarInfo 环境变量元信息（仅 key/configured/type，绝不回值）。
type EnvVarInfo struct {
	Key        string `json:"key"`
	Configured bool   `json:"configured"` // 是否已在 .env 提供
	Type       string `json:"type"`       // password | text（启发式：secrety key → password）
	Required   bool   `json:"required"`   // compose 引用但 .env 未提供
}

// EnvInventory 应用环境变量清单（详情，不含任何值）。
type EnvInventory struct {
	AppID string       `json:"appId"`
	Vars  []EnvVarInfo `json:"vars"`
}

// RemovePreview 删除预览：明确列出会被删除 / 保留的资源。
type RemovePreview struct {
	AppID      string   `json:"appId"`
	Purge      bool     `json:"purge"`
	WillDelete []string `json:"willDelete"` // purge=true：受管命名卷 + 受管目录 + 容器/网络；false：仅容器/网络
	WillKeep   []string `json:"willKeep"`   // external 卷 / 非受管 bind（purge=false 时含全部卷与数据目录）
	Note       string   `json:"note,omitempty"`
}

// --- Capability ---

// CapabilityReport 运行时能力探测结果。Docker 不可用时必须清晰且不影响 K8s。
type CapabilityReport struct {
	Compose    RuntimeCapability `json:"compose"`
	Kubernetes RuntimeCapability `json:"kubernetes"`
}

// RuntimeCapability 单个运行时的可用性。
type RuntimeCapability struct {
	Available bool     `json:"available"`
	Reason    string   `json:"reason,omitempty"`   // 不可用原因
	Version   string   `json:"version,omitempty"`  // docker / compose / k8s 版本
	Features  []string `json:"features,omitempty"` // 可用特性标签
}

// --- Validate（阶段3 预检，不落盘）---

// ValidateRequest 预检请求。
type ValidateRequest struct {
	ComposeContent string            `json:"compose"`
	Name           string            `json:"name,omitempty"`
	Parameters     map[string]string `json:"parameters,omitempty"`
	Source         ApplicationSource `json:"source,omitempty"`
}

// ValidateResult 预检结果。
type ValidateResult struct {
	OK       bool             `json:"ok"`
	Services []ServicePreview `json:"services,omitempty"`
	Risks    []RiskFinding    `json:"risks,omitempty"`
	Warnings []string         `json:"warnings,omitempty"`
	Errors   []string         `json:"errors,omitempty"` // 致命（如非法 YAML）
}

// ServicePreview 预检解析出的服务概览。
type ServicePreview struct {
	Name    string   `json:"name"`
	Image   string   `json:"image"`
	Ports   []string `json:"ports,omitempty"`
	Volumes []string `json:"volumes,omitempty"`
}

// --- 错误类型 ---
//
// HTTP 层按 ErrorKind 映射状态码。使用 errors.As 解包。

type ErrorKind string

const (
	ErrKindNotFound    ErrorKind = "not_found"
	ErrKindConflict    ErrorKind = "conflict" // revision mismatch / idempotency 冲突
	ErrKindValidation  ErrorKind = "validation"
	ErrKindRiskBlocked ErrorKind = "risk_blocked" // 风险策略阻断
	ErrKindCapability  ErrorKind = "capability"   // 运行时不可用
	ErrKindInternal    ErrorKind = "internal"
)

// Error 领域错误。Reason 为机器可读原因码，Message 为人读描述。
type Error struct {
	Kind    ErrorKind
	Reason  string // 机器可读原因码（如 "revision_mismatch" / "idempotency_conflict"）
	Message string
	Cause   error
	// Findings 仅 ErrKindRiskBlocked 携带：阻断/需确认的具体风险项。内容仅含
	// 字段名与脱敏描述（不含 compose 正文 / secret 值），可安全回传调用方。
	Findings []RiskFinding
}

func (e *Error) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Kind, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Kind, e.Message)
}

func (e *Error) Unwrap() error { return e.Cause }

func newErr(kind ErrorKind, reason, msg string, cause error) *Error {
	return &Error{Kind: kind, Reason: reason, Message: msg, Cause: cause}
}

// NotFoundErr 资源不存在（→ HTTP 404）。
func NotFoundErr(id string) *Error {
	return newErr(ErrKindNotFound, "not_found", fmt.Sprintf("application %q not found", id), nil)
}

// ConflictErr 冲突（→ HTTP 409）。reason 区分 revision_mismatch / idempotency_conflict。
func ConflictErr(reason, msg string) *Error {
	return newErr(ErrKindConflict, reason, msg, nil)
}

// ValidationErr 校验失败（→ HTTP 400）。
func ValidationErr(msg string) *Error {
	return newErr(ErrKindValidation, "validation_failed", msg, nil)
}

// RiskBlockedErr 风险策略阻断（→ HTTP 422）。findings 随错误携带，供调用方/HTTP
// 取得具体风险项；findings 仅含字段名与脱敏描述，不含 secret。
func RiskBlockedErr(msg string, findings []RiskFinding) *Error {
	return &Error{Kind: ErrKindRiskBlocked, Reason: "risk_blocked", Message: msg, Findings: findings}
}

// CapabilityErr 运行时不可用（→ HTTP 503）。
func CapabilityErr(msg string) *Error {
	return newErr(ErrKindCapability, "unavailable", msg, nil)
}

// AsError 解包到 *Error；非领域错误返回 nil kind。
func AsError(err error) (*Error, bool) {
	var e *Error
	if errors.As(err, &e) {
		return e, true
	}
	return nil, false
}
