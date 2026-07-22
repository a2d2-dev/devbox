package console

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/a2d2-dev/devbox/pkg/apps"
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
	if !ver.Installable || ver.Runtime != apps.RuntimeCompose {
		writeJSONErrStatus(w, http.StatusUnprocessableEntity, map[string]any{
			"error":  "该应用不可在本机安装",
			"reason": "not_installable",
			"detail": ver.NotInstallableReason,
		})
		return
	}

	// 2/3. 校验 + 安全渲染。
	compose, params, secrets, err := apps.RenderStoreCompose(ver, req.Values)
	if err != nil {
		writeJSONErrStatus(w, http.StatusBadRequest, map[string]any{
			"error":  err.Error(),
			"reason": "validation_failed",
		})
		return
	}

	// 4. 同源已装 → 复用 ID + ExpectedRevision；否则用 catalog appID 作 devbox app ID。
	desired := apps.DesiredApplication{
		Name:           req.AppID,
		ComposeContent: compose,
		Parameters:     params,
		Secrets:        secrets,
		Source:         apps.ApplicationSource{Kind: apps.SourceStore, StoreID: req.AppID, Version: ver.Version},
	}
	if id, rev, found := s.findInstalledStoreApp(r.Context(), req.AppID); found {
		desired.ID = id
		desired.ExpectedRevision = rev
	} else {
		desired.ID = req.AppID
	}

	// 5. 幂等键：前端可传；为空则按 app+version+params+secrets 指纹生成稳定键。
	// 指纹纳入 secrets 是关键——否则同 app+version 换密码会被旧 task 短路（secret 静默
	// 不更新），改 params 会被判 idempotency_conflict（reconfigure 被阻断）。
	idemKey := req.IdempotencyKey
	if idemKey == "" {
		idemKey = "store:" + req.AppID + ":" + ver.Version + ":" + apps.StoreInstallFingerprint(params, secrets)
	}

	task, err := s.controller.Apply(r.Context(), desired, apps.ApplyOptions{
		IdempotencyKey: idemKey, Actor: defaultActor,
	})
	if err != nil {
		writeAppErr(w, err)
		return
	}
	s.jsonStatus(w, http.StatusAccepted, apps.StoreInstallResult{
		TaskID:   task.ID,
		AppID:    desired.ID,
		Name:     desired.Name,
		Revision: task.Revision,
	})
}

// findInstalledStoreApp 查找已安装的同源 catalog app（compose 运行时 + SourceStore + StoreID 匹配）。
// 命中返回其 ID 与当前 revision（用于复用 ID + 乐观并发）；用于安装/升级同一 catalog app。
func (s *Server) findInstalledStoreApp(ctx context.Context, appID string) (string, int64, bool) {
	list, err := s.controller.List(ctx, apps.Filter{})
	if err != nil || list == nil {
		return "", 0, false
	}
	for _, a := range list {
		if a.Runtime == apps.RuntimeCompose &&
			a.Source.Kind == apps.SourceStore && a.Source.StoreID == appID {
			return a.ID, a.Revision, true
		}
	}
	return "", 0, false
}
