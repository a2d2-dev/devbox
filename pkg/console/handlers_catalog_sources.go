package console

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/a2d2-dev/devbox/pkg/apps"
)

const catalogSourceRequestTimeout = 90 * time.Second

// handleCatalogSources 管理动态原生市场来源。token 只写，不出现在任何响应。
func (s *Server) handleCatalogSources(w http.ResponseWriter, r *http.Request) {
	if s.catalogSourceManager == nil {
		writeJSONErrStatus(w, http.StatusServiceUnavailable, map[string]any{"error": "catalog source management unavailable"})
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/catalogs/sources")
	if path == "/test" {
		s.handleCatalogSourceTest(w, r)
		return
	}
	if r.Method == http.MethodPost && strings.HasSuffix(path, "/refresh") {
		id := strings.Trim(strings.TrimSuffix(strings.TrimPrefix(path, "/"), "/refresh"), "/")
		if id == "" || strings.Contains(id, "/") {
			http.NotFound(w, r)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), catalogSourceRequestTimeout)
		defer cancel()
		snap, err := s.catalogSourceManager.Refresh(ctx, id)
		if err != nil {
			writeCatalogSourceErr(w, err)
			return
		}
		s.jsonOK(w, snap)
		return
	}
	id := strings.Trim(strings.TrimPrefix(path, "/"), "/")
	if id != "" && strings.Contains(id, "/") {
		http.NotFound(w, r)
		return
	}

	switch {
	case r.Method == http.MethodGet && id == "":
		views, err := s.catalogSourceManager.List(r.Context())
		if err != nil {
			writeJSONErrStatus(w, http.StatusInternalServerError, map[string]any{"error": "读取来源失败"})
			return
		}
		s.jsonOK(w, views)
	case r.Method == http.MethodPost && id == "":
		in, ok := decodeCatalogSourceInput(w, r)
		if !ok {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), catalogSourceRequestTimeout)
		defer cancel()
		view, err := s.catalogSourceManager.Create(ctx, in, defaultActor)
		if err != nil {
			writeCatalogSourceErr(w, err)
			return
		}
		s.jsonStatus(w, http.StatusCreated, view)
	case r.Method == http.MethodPut && id != "":
		in, ok := decodeCatalogSourceInput(w, r)
		if !ok {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), catalogSourceRequestTimeout)
		defer cancel()
		view, err := s.catalogSourceManager.Update(ctx, id, in, defaultActor)
		if err != nil {
			writeCatalogSourceErr(w, err)
			return
		}
		s.jsonOK(w, view)
	case r.Method == http.MethodDelete && id != "":
		if err := s.catalogSourceManager.Delete(r.Context(), id, defaultActor); err != nil {
			writeCatalogSourceErr(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleCatalogSourceTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	in, ok := decodeCatalogSourceInput(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), catalogSourceRequestTimeout)
	defer cancel()
	snap, err := s.catalogSourceManager.Test(ctx, in)
	if err != nil {
		writeCatalogSourceErr(w, err)
		return
	}
	s.jsonOK(w, map[string]any{"ok": true, "kind": snap.Kind, "sourceName": snap.SourceName, "appCount": snap.Status.AppCount})
}

func decodeCatalogSourceInput(w http.ResponseWriter, r *http.Request) (apps.CatalogSourceInput, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var in apps.CatalogSourceInput
	if err := dec.Decode(&in); err != nil {
		writeJSONErrStatus(w, http.StatusBadRequest, map[string]any{"error": "invalid request body"})
		return in, false
	}
	return in, true
}

func writeCatalogSourceErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, apps.ErrCatalogSourceConflict):
		writeJSONErrStatus(w, http.StatusConflict, map[string]any{"error": "来源 ID 已存在或由配置文件管理", "reason": "source_conflict"})
	case errors.Is(err, apps.ErrCatalogSourceNotFound):
		writeJSONErrStatus(w, http.StatusNotFound, map[string]any{"error": "来源不存在", "reason": "source_not_found"})
	case errors.Is(err, apps.ErrCatalogSourceUnreachable):
		writeJSONErrStatus(w, http.StatusBadGateway, map[string]any{"error": "无法连接或解析应用市场", "reason": "source_unreachable"})
	default:
		if ae, ok := apps.AsError(err); ok && ae.Kind == apps.ErrKindValidation {
			writeAppErr(w, err)
			return
		}
		// Git/网络错误可能含上游细节；只回通用消息，完整脱敏摘要留来源状态。
		writeJSONErrStatus(w, http.StatusInternalServerError, map[string]any{"error": "应用市场来源操作失败"})
	}
}
