package console

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/a2d2-dev/devbox/pkg/apps"
	"github.com/a2d2-dev/devbox/pkg/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// stubController 隔离 HTTP 逻辑（业务正确性已在 pkg/apps 覆盖）。
type stubController struct {
	listResult []apps.Application
	listErr    error

	applyTask      apps.Task
	applyErr       error
	lastApplyCt    int
	lastValidateCt int
	lastDesired    apps.DesiredApplication
	lastApplyOpts  apps.ApplyOptions

	operateTask apps.Task
	operateErr  error

	removeTask apps.Task
	removeErr  error

	validateResult apps.ValidateResult
	capability     apps.CapabilityReport

	task    apps.Task
	taskErr error

	logs           apps.LogPage
	logsErr        error
	lastLogOptions apps.LogOptions

	compose    apps.ComposeContent
	composeErr error

	revisions    []apps.Revision
	revisionsErr error

	operations    []apps.Task
	operationsErr error

	restoreTask apps.Task
	restoreErr  error

	takeoverErr error
}

func (s *stubController) Capability(context.Context) (apps.CapabilityReport, error) {
	return s.capability, nil
}
func (s *stubController) List(context.Context, apps.Filter) ([]apps.Application, error) {
	return s.listResult, s.listErr
}
func (s *stubController) Get(context.Context, string) (apps.Application, error) {
	return apps.Application{}, nil
}
func (s *stubController) Logs(_ context.Context, _ string, opts apps.LogOptions) (apps.LogPage, error) {
	s.lastLogOptions = opts
	return s.logs, s.logsErr
}
func (s *stubController) Validate(context.Context, apps.ValidateRequest) (apps.ValidateResult, error) {
	return s.validateResult, nil
}
func (s *stubController) Apply(_ context.Context, d apps.DesiredApplication, opts apps.ApplyOptions) (apps.Task, error) {
	s.lastApplyCt++
	s.lastDesired = d
	s.lastApplyOpts = opts
	return s.applyTask, s.applyErr
}
func (s *stubController) Operate(context.Context, string, apps.Action, apps.OperationOptions) (apps.Task, error) {
	return s.operateTask, s.operateErr
}
func (s *stubController) Remove(context.Context, string, apps.RemoveOptions) (apps.Task, error) {
	return s.removeTask, s.removeErr
}
func (s *stubController) RestoreRevision(context.Context, string, int64, apps.ApplyOptions) (apps.Task, error) {
	return s.restoreTask, s.restoreErr
}
func (s *stubController) Takeover(_ context.Context, req apps.TakeoverRequest, _ apps.ApplyOptions) (apps.Application, error) {
	if s.takeoverErr != nil {
		return apps.Application{}, s.takeoverErr
	}
	return apps.Application{ID: req.ID, Name: "stub", Runtime: apps.RuntimeCompose, Ownership: apps.OwnershipManaged}, nil
}
func (s *stubController) GetTask(_ context.Context, id string) (apps.Task, error) {
	if s.taskErr != nil {
		return apps.Task{}, s.taskErr
	}
	t := s.task
	if t.ID == "" {
		t.ID = id
	}
	return t, nil
}
func (s *stubController) ListOperations(context.Context, string) ([]apps.Task, error) {
	return s.operations, s.operationsErr
}
func (s *stubController) GetCompose(context.Context, string) (apps.ComposeContent, error) {
	return s.compose, s.composeErr
}
func (s *stubController) ListRevisions(context.Context, string) ([]apps.Revision, error) {
	return s.revisions, s.revisionsErr
}
func (s *stubController) StorageInventory(context.Context, string) (apps.StorageInventory, error) {
	return apps.StorageInventory{}, nil
}
func (s *stubController) EnvInventory(context.Context, string) (apps.EnvInventory, error) {
	return apps.EnvInventory{}, nil
}
func (s *stubController) RemovePreview(_ context.Context, id string, purge bool) (apps.RemovePreview, error) {
	return apps.RemovePreview{AppID: id, Purge: purge}, nil
}

func newTestServer(ctrl apps.Controller) *Server {
	s := &Server{controller: ctrl, logger: zap.NewNop(), mux: http.NewServeMux(), auth: auth.New(auth.Config{})}
	s.registerAppRoutes()
	return s
}

func do(s *Server, method, target string, body any) *httptest.ResponseRecorder {
	var r *http.Request
	if body != nil {
		b, _ := json.Marshal(body)
		r = httptest.NewRequest(method, target, bytes.NewReader(b))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(method, target, nil)
	}
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, r)
	return w
}

func TestHTTPListEmptyWhenNilController(t *testing.T) {
	s := &Server{controller: nil, logger: zap.NewNop(), mux: http.NewServeMux()}
	s.registerAppRoutes()
	w := do(s, http.MethodGet, "/api/v1/apps", nil)
	assert.Equal(t, http.StatusOK, w.Code)
	var arr []any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &arr))
	assert.Empty(t, arr)
}

func TestHTTPCreateReturns202(t *testing.T) {
	stub := &stubController{applyTask: apps.Task{ID: "t1", Status: apps.TaskQueued}}
	s := newTestServer(stub)
	w := do(s, http.MethodPost, "/api/v1/apps", apps.DesiredApplication{Name: "x", ComposeContent: "services:\n  a:\n    image: nginx:1.27"})
	assert.Equal(t, http.StatusAccepted, w.Code)
	var got apps.Task
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Equal(t, "t1", got.ID)
	assert.Equal(t, 1, stub.lastApplyCt)
}

func TestHTTPCreateConflict409(t *testing.T) {
	stub := &stubController{applyErr: apps.ConflictErr("revision_mismatch", "x")}
	s := newTestServer(stub)
	w := do(s, http.MethodPost, "/api/v1/apps", apps.DesiredApplication{Name: "x"})
	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestHTTPCreateBlocked422(t *testing.T) {
	stub := &stubController{applyErr: apps.RiskBlockedErr("privileged", nil)}
	s := newTestServer(stub)
	w := do(s, http.MethodPost, "/api/v1/apps", apps.DesiredApplication{Name: "x"})
	assert.Equal(t, http.StatusUnprocessableEntity, w.Code)
}

// LOW#12 + HIGH#1：错误统一 JSON 信封；risk_blocked 回传脱敏 findings。
func TestHTTPErrorEnvelopeJSONAndFindings(t *testing.T) {
	findings := []apps.RiskFinding{
		{Level: apps.RiskBlocked, Service: "web", Field: "privileged", Message: "privileged:true"},
	}
	stub := &stubController{applyErr: apps.RiskBlockedErr("存在阻断级风险", findings)}
	s := newTestServer(stub)
	w := do(s, http.MethodPost, "/api/v1/apps", apps.DesiredApplication{Name: "x"})
	assert.Equal(t, http.StatusUnprocessableEntity, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "application/json")
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Contains(t, body, "error")
	fl, ok := body["findings"].([]any)
	require.True(t, ok, "应回传 findings")
	assert.Len(t, fl, 1)
	// findings 内不应出现 secret/compose 正文（这里本就只有脱敏字段）。
	raw := w.Body.String()
	assert.NotContains(t, raw, "secret")
}

// LOW#12：非 risk_blocked 错误为统一 JSON，且不含 findings 字段。
func TestHTTPErrorEnvelopeGenericValidation(t *testing.T) {
	stub := &stubController{applyErr: apps.ValidationErr("compose 配置无效")}
	s := newTestServer(stub)
	w := do(s, http.MethodPost, "/api/v1/apps", apps.DesiredApplication{Name: "x"})
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "application/json")
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "compose 配置无效", body["error"])
	_, hasFindings := body["findings"]
	assert.False(t, hasFindings, "非 risk_blocked 不应含 findings")
}

func TestHTTPValidate(t *testing.T) {
	stub := &stubController{validateResult: apps.ValidateResult{OK: true}}
	s := newTestServer(stub)
	w := do(s, http.MethodPost, "/api/v1/apps/validate", apps.ValidateRequest{ComposeContent: "services:\n  a:\n    image: nginx:1.27"})
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHTTPCapability(t *testing.T) {
	stub := &stubController{capability: apps.CapabilityReport{Compose: apps.RuntimeCapability{Available: true}}}
	s := newTestServer(stub)
	w := do(s, http.MethodGet, "/api/v1/apps/capability", nil)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHTTPTaskGetAnd404(t *testing.T) {
	stub := &stubController{task: apps.Task{ID: "t9", Status: apps.TaskSucceeded}}
	s := newTestServer(stub)
	w := do(s, http.MethodGet, "/api/v1/tasks/t9", nil)
	assert.Equal(t, http.StatusOK, w.Code)

	stub.taskErr = apps.NotFoundErr("t9")
	w = do(s, http.MethodGet, "/api/v1/tasks/t9", nil)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHTTPOperateAsync202(t *testing.T) {
	stub := &stubController{operateTask: apps.Task{ID: "ta", Status: apps.TaskQueued}}
	s := newTestServer(stub)
	w := do(s, http.MethodPost, "/api/v1/apps/myapp/actions/redeploy", nil)
	assert.Equal(t, http.StatusAccepted, w.Code)
}

func TestHTTPDeleteCompat(t *testing.T) {
	stub := &stubController{
		removeTask: apps.Task{ID: "tr", Type: apps.TaskRemove, Purge: true},
		task:       apps.Task{ID: "tr", Status: apps.TaskSucceeded},
	}
	s := newTestServer(stub)
	w := do(s, http.MethodDelete, "/api/v1/apps/myapp", nil)
	assert.Equal(t, http.StatusOK, w.Code)
	var body map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "ok", body["status"])
}

func TestHTTPLogs(t *testing.T) {
	stub := &stubController{logs: apps.LogPage{AppID: "a", Logs: "line1\nline2"}}
	s := newTestServer(stub)
	w := do(s, http.MethodGet, "/api/v1/apps/a/logs?tail=50&service=worker", nil)
	assert.Equal(t, http.StatusOK, w.Code)
	var body map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "line1\nline2", body["logs"])
	assert.Equal(t, int64(50), stub.lastLogOptions.Tail)
	assert.Equal(t, "worker", stub.lastLogOptions.Service)
}

func TestHTTPComposeRevisionsOperations(t *testing.T) {
	stub := &stubController{
		compose:    apps.ComposeContent{AppID: "a", Compose: "services: {}"},
		revisions:  []apps.Revision{{Number: 1}},
		operations: []apps.Task{{ID: "t1"}},
	}
	s := newTestServer(stub)
	assert.Equal(t, http.StatusOK, do(s, http.MethodGet, "/api/v1/apps/a/compose", nil).Code)
	assert.Equal(t, http.StatusOK, do(s, http.MethodGet, "/api/v1/apps/a/revisions", nil).Code)
	assert.Equal(t, http.StatusOK, do(s, http.MethodGet, "/api/v1/apps/a/operations", nil).Code)
}

func TestHTTPRestore202(t *testing.T) {
	stub := &stubController{restoreTask: apps.Task{ID: "tr", Status: apps.TaskQueued}}
	s := newTestServer(stub)
	w := do(s, http.MethodPost, "/api/v1/apps/a/revisions/2/restore", nil)
	assert.Equal(t, http.StatusAccepted, w.Code)
}

func TestHTMPTakeoverReturns200(t *testing.T) {
	stub := &stubController{}
	s := newTestServer(stub)
	w := do(s, http.MethodPost, "/api/v1/apps/ext-foo-abcd/takeover", apps.TakeoverRequest{ConfirmRisky: true})
	assert.Equal(t, http.StatusOK, w.Code)
	var got apps.Application
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Equal(t, apps.OwnershipManaged, got.Ownership)
}

func TestHTMPTakeoverBlockedMaps422(t *testing.T) {
	stub := &stubController{}
	stub.takeoverErr = apps.RiskBlockedErr("blocked", nil)
	s := newTestServer(stub)
	w := do(s, http.MethodPost, "/api/v1/apps/ext-foo-abcd/takeover", nil)
	assert.Equal(t, http.StatusUnprocessableEntity, w.Code)
}

func TestHTMPTakeoverDecodeHardening(t *testing.T) {
	cases := []struct {
		name       string
		body       string
		noLen      bool // 模拟 chunked（ContentLength=-1）
		wantStatus int
	}{
		{"empty body ok", "", false, http.StatusOK},
		{"valid confirmRisky", `{"confirmRisky":true}`, false, http.StatusOK},
		{"chunked valid", `{"confirmRisky":false}`, true, http.StatusOK},
		{"invalid json", `{not json`, false, http.StatusBadRequest},
		{"unknown field", `{"foo":"bar"}`, false, http.StatusBadRequest},
		{"trailing json", `{"confirmRisky":true}{}`, false, http.StatusBadRequest},
		{"trailing garbage", `{"confirmRisky":true}garbage`, false, http.StatusBadRequest},
		{"oversize", `{"x":"` + strings.Repeat("a", 5000) + `"}`, false, http.StatusRequestEntityTooLarge},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			stub := &stubController{}
			s := newTestServer(stub)
			var r *http.Request
			if c.body == "" {
				r = httptest.NewRequest(http.MethodPost, "/api/v1/apps/ext-x/takeover", nil)
			} else {
				r = httptest.NewRequest(http.MethodPost, "/api/v1/apps/ext-x/takeover", strings.NewReader(c.body))
				r.Header.Set("Content-Type", "application/json")
			}
			if c.noLen {
				r.ContentLength = -1
			}
			w := httptest.NewRecorder()
			s.mux.ServeHTTP(w, r)
			assert.Equal(t, c.wantStatus, w.Code, "body=%q", c.body)
		})
	}
}
