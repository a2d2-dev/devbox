package console

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/a2d2-dev/devbox/pkg/collector"
	"github.com/a2d2-dev/devbox/pkg/supervisor"
	"go.uber.org/zap"
)

// Module 是一个自包含的控制台功能单元:自注册 HTTP 路由并自管后台生命周期。
// 新增功能只需实现本接口并加入 NewServer 的 modules 列表,无需改动 Server 本身。
//
// 借鉴 DeepSeek Harness(Cordis)的"一切皆插件":RegisterRoutes 对应插件向共享
// 上下文的贡献,Start/Stop 对应 effect/disposer——Start 建立的后台工作在 Stop 时对称回收。
type Module interface {
	// Name 返回稳定的短标识,用于日志与诊断。
	Name() string
	// RegisterRoutes 把该 module 的路由挂到共享 mux 上。
	// 鉴权由 Server 外层的 authGate 统一兜底,module 不重复实现。
	RegisterRoutes(mux *http.ServeMux)
	// Start 启动后台工作(采集、watch 等)。无后台工作的 module 返回 nil。
	// ctx 取消时应停止工作,与 Stop 二者取先到者。
	Start(ctx context.Context) error
	// Stop 同步回收 Start 建立的资源。Start 为 no-op 时返回 nil。
	Stop() error
}

// Deps 是 module 构造时共享的依赖容器,取代把每个 manager 塞进 Server 字段的做法。
// 只放确有多方消费或跨 module 共享的依赖;module 私有的 manager 由 module 自己构造持有。
type Deps struct {
	Logger     *zap.Logger
	Config     Config
	Collector  *collector.Collector
	Supervisor *supervisor.Manager
}

// writeJSON 是 module 使用的 JSON 响应 helper,与 Server.jsonOK 行为一致。
func writeJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(data)
}
