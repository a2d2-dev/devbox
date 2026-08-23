package console

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/a2d2-dev/devbox/pkg/apps"
	eventlog "github.com/a2d2-dev/devbox/pkg/syslog"
	"go.uber.org/zap"
)

// 商店路由（阶段4 Compose 商店统一）。
//
//   - GET  /api/v1/store/apps           列表（含 runtime/installable）— handleStoreApps 在 handlers_extra.go
//   - GET  /api/v1/store/version?appId=&v=  版本详情（valuesSchema/defaultValues/runtime/installable；
//     compose 模板由 StoreAppVersion.ComposeTemplate 的 json:"-" 裁剪，永不回前端）
//   - POST /api/v1/store/install        后端从可信 catalog 重取版本 → 校验 → 渲染 → Controller.Apply → 202+Task
//
// 关键安全约束（CEO 裁决第5/7/10条）：
//   - install 不接受前端 compose 原文，一律后端从 catalog 重新取版本渲染。
//   - secret/password 只进 .env（0600），不进 revision/task/audit/HTTP 回读。
//   - catalog 响应大小、模板输出、错误体均有上限并脱敏。

// handleStoreVersion 返回某商店应用指定版本的参数 schema / 默认值 / runtime / 可安装性。
// 不返回 compose 模板原文（StoreAppVersion.ComposeTemplate 标记 json:"-"）。
func (s *Server) handleStoreVersion(w http.ResponseWriter, r *http.Request) {
	if s.storeManager == nil {
		writeJSONErrStatus(w, http.StatusServiceUnavailable, map[string]any{"error": "store not configured"})
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	appID := r.URL.Query().Get("appId")
	version := r.URL.Query().Get("v")
	if appID == "" {
		writeJSONErrStatus(w, http.StatusBadRequest, map[string]any{"error": "appId is required"})
		return
	}
	ver, err := s.storeManager.GetStoreAppVersion(r.Context(), appID, version)
	if err != nil {
		s.logger.Warn("store version fetch failed", zap.String("app", appID), zap.Error(err))
		writeJSONErrStatus(w, http.StatusBadGateway, map[string]any{
			"error":  "获取商店版本失败",
			"reason": "catalog_unreachable",
		})
		return
	}
	s.jsonOK(w, ver)
}

// handleStoreInstall 商店安装（Compose 包）：
//  1. 从可信 catalog 重新获取版本（不接受前端 compose 原文）
//  2. 校验 values 对照 schema（required/type/select），分离 secret/password
//  3. 安全渲染 compose 模板（text/template missingkey=error，无 FuncMap）
//  4. 查找同源已装 catalog app → 复用 ID + ExpectedRevision（版本切换产生新 revision）
//  5. Controller.Apply（幂等/预检/风险分析/secret 仅 .env/atomic 由 Controller 保证）→ 202+Task
func (s *Server) handleStoreInstall(w http.ResponseWriter, r *http.Request) {
	if s.controller == nil {
		http.Error(w, "app controller not initialized", http.StatusServiceUnavailable)
		return
	}
	if s.storeManager == nil {
		writeJSONErrStatus(w, http.StatusServiceUnavailable, map[string]any{"error": "store not configured"})
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// 限制请求体大小（values map 由用户填写，防御性上限）。
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req apps.StoreInstallRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.AppID == "" {
		writeJSONErrStatus(w, http.StatusBadRequest, map[string]any{"error": "appId is required"})
		return
	}

	// 1. 可信 catalog 取版本。
	ver, err := s.storeManager.GetStoreAppVersion(r.Context(), req.AppID, req.Version)
	if err != nil {
		s.logger.Warn("store install: version fetch failed", zap.String("app", req.AppID), zap.Error(err))
		writeJSONErrStatus(w, http.StatusBadGateway, map[string]any{
			"error":  "获取商店版本失败",
			"reason": "catalog_unreachable",
		})
		return
	}

	source := apps.ApplicationSource{Kind: apps.SourceStore, StoreID: req.AppID, Version: ver.Version}
	s.installResolvedVersion(w, r, req.AppID, ver, req.Values, req.IdempotencyKey, req.ConfirmRisky, source)
}

// installResolvedVersion store/catalog 安装的共享流程（要求 3/5）：
// 已从可信 source 取到版本 → 校验+安全渲染 → 复用同源已装 ID → Controller.Apply。
//   - compose 原文一律后端持有（ver），不来自前端。
//   - 可安装性 / 风险（含 store/catalog 可变标签阻断）/ secret 仅 .env / atomic 均由 Controller 保证。
//   - 幂等键：只使用调用方显式提供的 key。不能生成跨请求永久稳定 key，否则应用
//     卸载后重装同版本会命中卸载前的旧成功 Task，而不会重新创建应用。
func (s *Server) installResolvedVersion(w http.ResponseWriter, r *http.Request, appID string,
	ver apps.StoreAppVersion, values map[string]any, idemKey string, confirmRisky bool, source apps.ApplicationSource) {
	if idemKey == "" {
		idemKey = idempotencyKey(r)
	}
	if !ver.Installable || ver.Runtime != apps.RuntimeCompose {
		writeJSONErrStatus(w, http.StatusUnprocessableEntity, map[string]any{
			"error":  "该应用不可在本机安装",
			"reason": "not_installable",
			"detail": ver.NotInstallableReason,
		})
		return
	}

	compose, params, secrets, err := apps.RenderStoreCompose(ver, values)
	if err != nil {
		writeJSONErrStatus(w, http.StatusBadRequest, map[string]any{
			"error":  err.Error(),
			"reason": "validation_failed",
		})
		return
	}

	desired := apps.DesiredApplication{
		Name: appID, ComposeContent: compose, Parameters: params, Secrets: secrets, Source: source,
	}
	// 同源已装 → 复用 ID + ExpectedRevision；否则全新安装用来源隔离稳定 ID。
	// catalog 来源（含 1Panel）一律 namespaced：CatalogLocalAppID(appID, catalogID)，
	// 避免 upstream key 含下划线（act_runner）/超长/多来源同 key 撞 ID 或 fail ValidateAppID。
	// StoreApp.ID 与可信路由仍保留原始 upstreamKey；edge store（SourceStore）保留原 appID。
	if id, rev, found := s.findInstalledVersion(r.Context(), source, appID); found {
		desired.ID = id
		desired.ExpectedRevision = rev
	} else if source.Kind == apps.SourceCatalog {
		desired.ID = apps.CatalogLocalAppID(appID, source.CatalogID)
	} else {
		desired.ID = appID
	}

	task, err := s.controller.Apply(r.Context(), desired, apps.ApplyOptions{
		IdempotencyKey: idemKey, Actor: defaultActor, AllowRiskyConfirmation: confirmRisky,
	})
	if err != nil {
		writeAppErr(w, err)
		return
	}
	s.recordEvent(r, eventlog.Input{
		Level: "info", Module: "apps", Event: "从应用市场安装应用", EventType: "APP_INSTALL", Outcome: "accepted",
		ResourceKind: "application", ResourceID: desired.ID,
		Payload: map[string]any{"source": source.Kind, "store_id": source.StoreID, "catalog_id": source.CatalogID, "version": source.Version, "task_id": task.ID},
	})
	s.jsonStatus(w, http.StatusAccepted, apps.StoreInstallResult{
		TaskID:   task.ID,
		AppID:    desired.ID,
		Name:     desired.Name,
		Revision: task.Revision,
	})
}

// findInstalledVersion 查找已安装的同源应用（compose + source Kind/StoreID 匹配；
// catalog 来源额外匹配 CatalogID）。命中返回 ID 与当前 revision（复用 ID + 乐观并发）。
func (s *Server) findInstalledVersion(ctx context.Context, source apps.ApplicationSource, appID string) (string, int64, bool) {
	list, err := s.controller.List(ctx, apps.Filter{Runtime: apps.RuntimeCompose})
	if err != nil || list == nil {
		return "", 0, false
	}
	for _, a := range list {
		if a.Runtime != apps.RuntimeCompose || a.Source.Kind != source.Kind || a.Source.StoreID != appID {
			continue
		}
		if source.Kind == apps.SourceCatalog && a.Source.CatalogID != source.CatalogID {
			continue
		}
		return a.ID, a.Revision, true
	}
	return "", 0, false
}
