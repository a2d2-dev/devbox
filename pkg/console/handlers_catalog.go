package console

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/a2d2-dev/devbox/pkg/apps"
	"go.uber.org/zap"
)

// 第三方 catalog 路由（Issue #2 阶段4 扩展：HTTP/Git 文件原生 catalog source）。
//
//   - GET  /api/v1/catalogs             各 source 健康状态（只读，UI 展示来源 health）
//   - POST /api/v1/catalogs             显式 refresh（已认证；不接受 URL 入参，仅刷新已配置 source）
//   - GET  /api/v1/catalogs/apps        合并所有 source 的 app（带 sourceId/sourceName/runtimes）
//   - GET  /api/v1/catalogs/version     ?sourceId=&appId=&v= 版本详情（compose 模板 json:"-" 裁剪）
//   - POST /api/v1/catalogs/install     后端按 sourceId+appId+version 从可信 source 重取 → 渲染 → Apply
//
// 关键约束（要求 1/3/4）：
//   - install 不接受前端 compose 原文，一律后端从可信 catalog 重取（禁止前端传模板）。
//   - catalog 不可用时返回上次可信缓存；已装应用不受影响。
//   - 原生 1Panel 来源由 /catalogs/sources 管理；动态 URL 走 HTTPS/SSRF 校验，token 只写。

// handleCatalogs GET=状态视图 / POST=显式 refresh。
func (s *Server) handleCatalogs(w http.ResponseWriter, r *http.Request) {
	if s.catalogs == nil {
		s.jsonOK(w, []any{})
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.jsonOK(w, s.catalogs.Statuses())
	case http.MethodPost:
		// 显式刷新所有已配置 source（失败隔离；不返回错误体细节，仅各 source 状态）。
		s.catalogs.RefreshAll(r.Context())
		s.jsonOK(w, s.catalogs.Statuses())
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleCatalogApps 合并所有第三方 catalog source 的 app 列表。
// 增加 sourceId/sourceName/runtimes；并用 Docker capability + 已装 map 增补
// installable / installed（已装应用照常由 /apps 返回，此处仅标记）。
func (s *Server) handleCatalogApps(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.catalogs == nil || !s.catalogs.Configured() {
		s.jsonOK(w, []any{})
		return
	}
	appsList := s.catalogs.ListApps()
	composeOK := s.composeAvailable(r.Context())
	installed := s.installedCatalogAppIDs()
	for i := range appsList {
		if !composeOK {
			appsList[i].Installable = false
			appsList[i].NotInstallableReason = "Docker/Compose 运行时不可用"
		}
		if installed[appsList[i].CatalogID+"\x00"+appsList[i].ID] {
			appsList[i].Installed = true
		}
	}
	s.jsonOK(w, appsList)
}

// handleCatalogVersion 返回某 catalog app 指定版本的参数 schema / 默认值 / runtime / 可安装性。
// compose 模板原文由 StoreAppVersion.ComposeTemplate 的 json:"-" 裁剪，永不回前端。
func (s *Server) handleCatalogVersion(w http.ResponseWriter, r *http.Request) {
	if s.catalogs == nil || !s.catalogs.Configured() {
		writeJSONErrStatus(w, http.StatusServiceUnavailable, map[string]any{"error": "catalog not configured"})
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sourceID := r.URL.Query().Get("sourceId")
	appID := r.URL.Query().Get("appId")
	version := r.URL.Query().Get("v")
	if sourceID == "" || appID == "" {
		writeJSONErrStatus(w, http.StatusBadRequest, map[string]any{"error": "sourceId and appId are required"})
		return
	}
	ver, err := s.catalogs.GetVersion(r.Context(), sourceID, appID, version)
	if err != nil {
		s.logger.Warn("catalog version fetch failed",
			zap.String("source", sourceID), zap.String("app", appID), zap.Error(err))
		writeJSONErrStatus(w, http.StatusBadGateway, map[string]any{
			"error":  "获取 catalog 版本失败",
			"reason": "catalog_unreachable",
		})
		return
	}
	s.jsonOK(w, ver)
}

// handleCatalogInstall 第三方 catalog 安装：
//  1. 从可信 catalog 按 sourceId+appId+version 重新取版本（不接受前端 compose 原文）
//  2. 校验 values + 安全渲染（RenderStoreCompose）
//  3. 复用同源已装应用 ID + ExpectedRevision；否则用 catalog appID 作 devbox app ID
//  4. Controller.Apply（预检/风险/可变标签阻断/secret 仅 .env/atomic 由 Controller 保证）
func (s *Server) handleCatalogInstall(w http.ResponseWriter, r *http.Request) {
	if s.controller == nil {
		http.Error(w, "app controller not initialized", http.StatusServiceUnavailable)
		return
	}
	if s.catalogs == nil || !s.catalogs.Configured() {
		writeJSONErrStatus(w, http.StatusServiceUnavailable, map[string]any{"error": "catalog not configured"})
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req apps.CatalogInstallRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.SourceID == "" || req.AppID == "" {
		writeJSONErrStatus(w, http.StatusBadRequest, map[string]any{"error": "sourceId and appId are required"})
		return
	}

	// 1. 可信 catalog 取版本（按 sourceId+appId+version）。
	ver, err := s.catalogs.GetVersion(r.Context(), req.SourceID, req.AppID, req.Version)
	if err != nil {
		s.logger.Warn("catalog install: version fetch failed",
			zap.String("source", req.SourceID), zap.String("app", req.AppID), zap.Error(err))
		writeJSONErrStatus(w, http.StatusBadGateway, map[string]any{
			"error":  "获取 catalog 版本失败",
			"reason": "catalog_unreachable",
		})
		return
	}

	source := apps.ApplicationSource{
		Kind: apps.SourceCatalog, StoreID: req.AppID, Version: ver.Version, CatalogID: req.SourceID,
	}
	s.installResolvedVersion(w, r, req.AppID, ver, req.Values, req.IdempotencyKey, req.ConfirmRisky, source)
}

// handleCatalogPreflight 与安装使用相同的 sourceId+appId+version 可信重取路径，
// 仅执行渲染和 Controller.Validate，不创建 revision/task。
func (s *Server) handleCatalogPreflight(w http.ResponseWriter, r *http.Request) {
	if s.controller == nil || s.catalogs == nil || !s.catalogs.Configured() {
		writeJSONErrStatus(w, http.StatusServiceUnavailable, map[string]any{"error": "catalog preflight unavailable"})
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req apps.CatalogInstallRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.SourceID == "" || req.AppID == "" {
		writeJSONErrStatus(w, http.StatusBadRequest, map[string]any{"error": "sourceId and appId are required"})
		return
	}
	ver, err := s.catalogs.GetVersion(r.Context(), req.SourceID, req.AppID, req.Version)
	if err != nil {
		writeJSONErrStatus(w, http.StatusBadGateway, map[string]any{"error": "获取 catalog 版本失败", "reason": "catalog_unreachable"})
		return
	}
	s.preflightResolvedVersion(w, r, req.AppID, ver, req.Values, apps.ApplicationSource{
		Kind: apps.SourceCatalog, StoreID: req.AppID, Version: ver.Version, CatalogID: req.SourceID,
	})
}

// --- catalog 辅助 ---

// composeAvailable 本机 Docker/Compose 运行时是否可用（catalog 列表标记 installable 用）。
func (s *Server) composeAvailable(ctx context.Context) bool {
	if s.controller == nil {
		return false
	}
	rep, err := s.controller.Capability(ctx)
	if err != nil {
		return false
	}
	return rep.Compose.Available
}

// installedCatalogAppIDs 已安装的 catalog 应用集合（key: catalogID \x00 appID）。
func (s *Server) installedCatalogAppIDs() map[string]bool {
	out := map[string]bool{}
	if s.controller == nil {
		return out
	}
	list, err := s.controller.List(context.Background(), apps.Filter{Runtime: apps.RuntimeCompose})
	if err != nil || list == nil {
		return out
	}
	for _, a := range list {
		if a.Source.Kind == apps.SourceCatalog && a.Source.CatalogID != "" {
			out[a.Source.CatalogID+"\x00"+a.Source.StoreID] = true
		}
	}
	return out
}
