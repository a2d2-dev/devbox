package console

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/a2d2-dev/devbox/pkg/apps"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// composeCatalogJSON 一个带 compose 模板 + valuesSchema 的 approved 版本（tag 文本必填，pw 密码必填）。
const composeCatalogJSON = `{"items":[{"spec":{"version":"1.0.0",` +
	`"composeTemplate":"services:\n  web:\n    image: nginx:{{ .tag }}\n    environment:\n      PW: ${pw}\n",` +
	`"valuesSchema":{"version":"v1","fields":[` +
	`{"key":"tag","type":"text","required":true,"label":{"zh":"标签"}},` +
	`{"key":"pw","type":"password","required":true,"label":{"zh":"密码"}}]},` +
	`"values":{"tag":{"raw":"latest"}}},` +
	`"status":{"reviewPhase":"Approved"}}]}`

func newStoreTestServer(t *testing.T, ctrl apps.Controller, catalog http.HandlerFunc) *Server {
	t.Helper()
	srv := httptest.NewServer(catalog)
	t.Cleanup(srv.Close)
	storeMgr := apps.NewStoreManagerWithClient(srv.URL, "", "",
		&http.Client{Timeout: 5 * time.Second}, zap.NewNop())
	s := &Server{controller: ctrl, storeManager: storeMgr, logger: zap.NewNop(), mux: http.NewServeMux()}
	s.mux.HandleFunc("/api/v1/store/version", s.handleStoreVersion)
	s.mux.HandleFunc("/api/v1/store/install", s.handleStoreInstall)
	return s
}

func TestHandleStoreVersion_Success(t *testing.T) {
	ctrl := &stubController{}
	s := newStoreTestServer(t, ctrl, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, composeCatalogJSON)
	})

	w := do(s, http.MethodGet, "/api/v1/store/version?appId=ghost&v=1.0.0", nil)
	assert.Equal(t, http.StatusOK, w.Code)

	var v apps.StoreAppVersion
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &v))
	assert.Equal(t, apps.RuntimeCompose, v.Runtime)
	assert.True(t, v.Installable)
	assert.NotEmpty(t, v.ValuesSchema)

	// compose 模板不得泄露到 HTTP 响应。
	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &raw))
	_, hasCompose := raw["composeTemplate"]
	_, hasComposeAlias := raw["compose"]
	assert.False(t, hasCompose || hasComposeAlias, "compose template must not leak")
}

func TestHandleStoreVersion_CatalogError(t *testing.T) {
	ctrl := &stubController{}
	s := newStoreTestServer(t, ctrl, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	})
	w := do(s, http.MethodGet, "/api/v1/store/version?appId=ghost", nil)
	assert.Equal(t, http.StatusBadGateway, w.Code)
}

func TestHandleStoreVersion_StoreNotConfigured(t *testing.T) {
	s := &Server{controller: &stubController{}, logger: zap.NewNop(), mux: http.NewServeMux()}
	s.mux.HandleFunc("/api/v1/store/version", s.handleStoreVersion)
	w := do(s, http.MethodGet, "/api/v1/store/version?appId=ghost", nil)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestHandleStoreInstall_Success(t *testing.T) {
	ctrl := &stubController{applyTask: apps.Task{ID: "task-1", Revision: 7}}
	s := newStoreTestServer(t, ctrl, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, composeCatalogJSON)
	})

	body := map[string]any{
		"appId": "ghost", "version": "1.0.0",
		"values": map[string]any{"tag": "1.25", "pw": "secret"},
	}
	w := do(s, http.MethodPost, "/api/v1/store/install", body)
	assert.Equal(t, http.StatusAccepted, w.Code)
	assert.Equal(t, 1, ctrl.lastApplyCt)

	d := ctrl.lastDesired
	assert.Equal(t, apps.SourceStore, d.Source.Kind)
	assert.Equal(t, "ghost", d.Source.StoreID)
	assert.Equal(t, "1.0.0", d.Source.Version)
	// 非敏感参数已插值；secret 未进 compose 正文。
	assert.Contains(t, d.ComposeContent, "nginx:1.25")
	assert.NotContains(t, d.ComposeContent, "secret")
	assert.Contains(t, d.ComposeContent, "${pw}") // secret 保留为 env 引用
	// secret 分离：进 Secrets，不进 Parameters。
	assert.Equal(t, "secret", d.Secrets["pw"])
	assert.NotContains(t, d.Parameters, "pw")

	var res apps.StoreInstallResult
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &res))
	assert.Equal(t, "task-1", res.TaskID)
	assert.Equal(t, int64(7), res.Revision)
}

func TestHandleStoreInstall_PassesRiskConfirmation(t *testing.T) {
	ctrl := &stubController{applyTask: apps.Task{ID: "task-risk"}}
	s := newStoreTestServer(t, ctrl, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, composeCatalogJSON)
	})
	w := do(s, http.MethodPost, "/api/v1/store/install", map[string]any{
		"appId": "ghost", "version": "1.0.0", "confirmRisky": true,
		"values": map[string]any{"tag": "1.25", "pw": "secret"},
	})
	require.Equal(t, http.StatusAccepted, w.Code)
	assert.True(t, ctrl.lastApplyOpts.AllowRiskyConfirmation)
}

func TestHandleStoreInstall_KubernetesRejected(t *testing.T) {
	ctrl := &stubController{}
	s := newStoreTestServer(t, ctrl, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"items":[{"spec":{"version":"1.0.0"},"status":{"reviewPhase":"Approved"}}]}`)
	})
	w := do(s, http.MethodPost, "/api/v1/store/install",
		map[string]any{"appId": "nginx", "version": "1.0.0"})
	assert.Equal(t, http.StatusUnprocessableEntity, w.Code)
	assert.Equal(t, 0, ctrl.lastApplyCt) // 不应触达 Apply
}

func TestHandleStoreInstall_ValidationFails(t *testing.T) {
	ctrl := &stubController{}
	s := newStoreTestServer(t, ctrl, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, composeCatalogJSON)
	})
	// 缺必填 pw。
	w := do(s, http.MethodPost, "/api/v1/store/install",
		map[string]any{"appId": "ghost", "version": "1.0.0", "values": map[string]any{"tag": "1.25"}})
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, 0, ctrl.lastApplyCt)
}

func TestHandleStoreInstall_ReuseExisting(t *testing.T) {
	ctrl := &stubController{
		listResult: []apps.Application{{
			ID: "ghost-installed", Revision: 3, Runtime: apps.RuntimeCompose,
			Source: apps.ApplicationSource{Kind: apps.SourceStore, StoreID: "ghost", Version: "0.9.0"},
		}},
		applyTask: apps.Task{ID: "task-2"},
	}
	s := newStoreTestServer(t, ctrl, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, composeCatalogJSON)
	})
	w := do(s, http.MethodPost, "/api/v1/store/install",
		map[string]any{"appId": "ghost", "version": "1.0.0", "values": map[string]any{"tag": "1.25", "pw": "x"}})
	require.Equal(t, http.StatusAccepted, w.Code)

	d := ctrl.lastDesired
	assert.Equal(t, "ghost-installed", d.ID)      // 复用已装 app ID
	assert.Equal(t, int64(3), d.ExpectedRevision) // 乐观并发
	assert.Equal(t, "1.0.0", d.Source.Version)    // 版本切换
}

func TestHandleStoreInstall_ApplyError(t *testing.T) {
	ctrl := &stubController{applyErr: apps.NotFoundErr("x")}
	s := newStoreTestServer(t, ctrl, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, composeCatalogJSON)
	})
	w := do(s, http.MethodPost, "/api/v1/store/install",
		map[string]any{"appId": "ghost", "values": map[string]any{"tag": "1.25", "pw": "x"}})
	assert.Equal(t, http.StatusNotFound, w.Code) // 领域错误映射
}
