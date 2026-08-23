package console

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/a2d2-dev/devbox/pkg/apps"
	eventlog "github.com/a2d2-dev/devbox/pkg/syslog"
)

// 应用管理路由（Issue #2）。
//
// 保留兼容读路径与旧 action（GET /apps、GET /apps/{id}、GET /apps/{id}/logs、
// POST /apps/{id}/{start|stop|restart|op}、DELETE /apps/{id}），前端零改；
// 新增异步写 API 返回 202+Task（POST /apps、PUT /apps/{id}、POST /apps/{id}/actions/...、
// DELETE?purge、revisions/restore）、validate、capability、tasks、operations。
//
// handler 不再直接知道 Deployment/Pod/Container，只依赖 apps.Controller。

func (s *Server) registerAppRoutes() {
	s.mux.HandleFunc("/api/v1/apps", s.handleApps)
	s.mux.HandleFunc("/api/v1/apps/validate", s.handleValidate)
	s.mux.HandleFunc("/api/v1/apps/capability", s.handleCapability)
	s.mux.HandleFunc("/api/v1/tasks/", s.handleTask)
	s.mux.HandleFunc("/api/v1/apps/", s.handleAppByID)
}

const defaultActor = "console"

// idempotencyKey 从 Idempotency-Key 头读取。
func idempotencyKey(r *http.Request) string {
	return strings.TrimSpace(r.Header.Get("Idempotency-Key"))
}

// writeAppErr 把领域错误映射到统一的 JSON 错误信封 {"error","reason","findings"}。
// risk_blocked 附带脱敏 findings（仅字段名/描述，无 secret/compose 正文），
// 让调用方能取得具体阻断项；非领域错误回 generic 500，不回显 compose 输出。
func writeAppErr(w http.ResponseWriter, err error) {
	if ae, ok := apps.AsError(err); ok {
		body := map[string]any{"error": ae.Message}
		if ae.Reason != "" {
			body["reason"] = ae.Reason
		}
		if ae.Kind == apps.ErrKindRiskBlocked && len(ae.Findings) > 0 {
			body["findings"] = ae.Findings
		}
		writeJSONErrStatus(w, appErrHTTPStatus(ae.Kind), body)
		return
	}
	writeJSONErrStatus(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
}

// appErrHTTPStatus 领域错误 → HTTP 状态码。
func appErrHTTPStatus(k apps.ErrorKind) int {
	switch k {
	case apps.ErrKindNotFound:
		return http.StatusNotFound
	case apps.ErrKindConflict:
		return http.StatusConflict
	case apps.ErrKindValidation:
		return http.StatusBadRequest
	case apps.ErrKindRiskBlocked:
		return http.StatusUnprocessableEntity
	case apps.ErrKindCapability:
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}

// writeJSONErrStatus 写统一 JSON 错误信封（application/json）。
func writeJSONErrStatus(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}

func (s *Server) jsonStatus(w http.ResponseWriter, code int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(data)
}

// --- /apps ---

func (s *Server) handleApps(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.listApps(w, r)
	case http.MethodPost:
		s.createApp(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) listApps(w http.ResponseWriter, r *http.Request) {
	// controller 未装配（装配失败）时返回空 list + 200，避免全局轮询刷屏。
	if s.controller == nil {
		s.jsonOK(w, []any{})
		return
	}
	var filter apps.Filter
	if rt := r.URL.Query().Get("runtime"); rt != "" {
		filter.Runtime = apps.RuntimeKind(rt)
	}
	list, err := s.controller.List(r.Context(), filter)
	if err != nil {
		writeAppErr(w, err)
		return
	}
	if list == nil {
		list = []apps.Application{}
	}
	s.jsonOK(w, list)
}

func (s *Server) createApp(w http.ResponseWriter, r *http.Request) {
	if s.controller == nil {
		http.Error(w, "app controller not initialized", http.StatusServiceUnavailable)
		return
	}
	var desired apps.DesiredApplication
	if err := json.NewDecoder(r.Body).Decode(&desired); err != nil {
		http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if desired.Source.Kind == "" {
		desired.Source.Kind = apps.SourceInline
	}
	task, err := s.controller.Apply(r.Context(), desired, apps.ApplyOptions{
		IdempotencyKey: idempotencyKey(r), Actor: defaultActor, AllowRiskyConfirmation: desired.ConfirmRisky,
	})
	if err != nil {
		writeAppErr(w, err)
		return
	}
	s.recordEvent(r, eventlog.Input{
		Level: "info", Module: "apps", Event: "创建应用", EventType: "APP_INSTALL", Outcome: "accepted",
		ResourceKind: "application", ResourceID: desired.ID, Payload: map[string]any{"source": desired.Source.Kind, "task_id": task.ID},
	})
	s.jsonStatus(w, http.StatusAccepted, task)
}

// --- /apps/validate ---

func (s *Server) handleValidate(w http.ResponseWriter, r *http.Request) {
	if s.controller == nil {
		http.Error(w, "app controller not initialized", http.StatusServiceUnavailable)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req apps.ValidateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	res, err := s.controller.Validate(r.Context(), req)
	if err != nil {
		writeAppErr(w, err)
		return
	}
	s.jsonOK(w, res)
}

// --- /apps/capability ---

func (s *Server) handleCapability(w http.ResponseWriter, r *http.Request) {
	if s.controller == nil {
		s.jsonOK(w, apps.CapabilityReport{})
		return
	}
	rep, err := s.controller.Capability(r.Context())
	if err != nil {
		writeAppErr(w, err)
		return
	}
	s.jsonOK(w, rep)
}

// --- /tasks/{id} ---

func (s *Server) handleTask(w http.ResponseWriter, r *http.Request) {
	if s.controller == nil {
		http.Error(w, "app controller not initialized", http.StatusServiceUnavailable)
		return
	}
	taskID := strings.TrimPrefix(r.URL.Path, "/api/v1/tasks/")
	if taskID == "" {
		http.Error(w, "task id required", http.StatusBadRequest)
		return
	}
	task, err := s.controller.GetTask(r.Context(), taskID)
	if err != nil {
		writeAppErr(w, err)
		return
	}
	s.jsonOK(w, task)
}

// --- /apps/{id}... ---

func (s *Server) handleAppByID(w http.ResponseWriter, r *http.Request) {
	if s.controller == nil {
		// 读路径降级为空/503；写路径 503。
		if r.Method == http.MethodGet && (strings.HasSuffix(r.URL.Path, "/logs") || strings.TrimPrefix(r.URL.Path, "/api/v1/apps/") == "") {
			s.jsonOK(w, []any{})
			return
		}
		http.Error(w, "app controller not initialized", http.StatusServiceUnavailable)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api/v1/apps/")
	segments := strings.Split(path, "/")
	if len(segments) == 0 || segments[0] == "" {
		http.Error(w, "app id required", http.StatusBadRequest)
		return
	}
	id := segments[0]

	switch {
	case len(segments) == 1 && r.Method == http.MethodGet:
		s.getApp(w, r, id)
	case len(segments) == 1 && r.Method == http.MethodDelete:
		s.deleteAppCompat(w, r, id)
	case len(segments) == 1 && r.Method == http.MethodPut:
		s.updateApp(w, r, id)
	case len(segments) == 2 && segments[1] == "logs" && r.Method == http.MethodGet:
		s.appLogs(w, r, id)
	case len(segments) == 2 && segments[1] == "compose" && r.Method == http.MethodGet:
		s.getCompose(w, r, id)
	case len(segments) == 2 && segments[1] == "revisions" && r.Method == http.MethodGet:
		s.listRevisions(w, r, id)
	case len(segments) == 2 && segments[1] == "storage" && r.Method == http.MethodGet:
		s.storageInventory(w, r, id)
	case len(segments) == 2 && segments[1] == "env" && r.Method == http.MethodGet:
		s.envInventory(w, r, id)
	case len(segments) == 2 && segments[1] == "remove-preview" && r.Method == http.MethodGet:
		s.removePreview(w, r, id)
	case len(segments) == 2 && segments[1] == "operations" && r.Method == http.MethodGet:
		s.listOperations(w, r, id)
	case len(segments) == 2 && segments[1] == "takeover" && r.Method == http.MethodPost:
		s.takeoverApp(w, r, id)
	case len(segments) == 2 && isCompatAction(segments[1]) && r.Method == http.MethodPost:
		// 兼容旧 action：内部统一走 Controller，同步等待。
		s.operateCompat(w, r, id, compatAction(segments[1]))
	case len(segments) == 3 && segments[1] == "actions" && r.Method == http.MethodPost:
		// 新异步 action：202 + Task。
		s.operateAsync(w, r, id, segments[2])
	case len(segments) == 4 && segments[1] == "revisions" && segments[3] == "restore" && r.Method == http.MethodPost:
		s.restoreRevision(w, r, id, segments[2])
	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
}

func isCompatAction(s string) bool {
	return s == "start" || s == "stop" || s == "restart" || s == "op"
}

// compatAction 旧 action 名 → 领域 Action（op 解析 body.operation）。
func compatAction(seg string) string {
	if seg == "op" {
		return "" // 由 body 决定
	}
	return seg
}

func (s *Server) getApp(w http.ResponseWriter, r *http.Request, id string) {
	app, err := s.controller.Get(r.Context(), id)
	if err != nil {
		writeAppErr(w, err)
		return
	}
	s.jsonOK(w, app)
}

func (s *Server) appLogs(w http.ResponseWriter, r *http.Request, id string) {
	tail := int64(200)
	if v := r.URL.Query().Get("tail"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			tail = n
		}
	}
	page, err := s.controller.Logs(r.Context(), id, apps.LogOptions{Tail: tail, Service: r.URL.Query().Get("service")})
	if err != nil {
		writeAppErr(w, err)
		return
	}
	s.jsonOK(w, map[string]string{"app": id, "logs": page.Logs})
}

func (s *Server) getCompose(w http.ResponseWriter, r *http.Request, id string) {
	c, err := s.controller.GetCompose(r.Context(), id)
	if err != nil {
		writeAppErr(w, err)
		return
	}
	s.jsonOK(w, c)
}

func (s *Server) listRevisions(w http.ResponseWriter, r *http.Request, id string) {
	revs, err := s.controller.ListRevisions(r.Context(), id)
	if err != nil {
		writeAppErr(w, err)
		return
	}
	if revs == nil {
		revs = []apps.Revision{}
	}
	s.jsonOK(w, revs)
}

func (s *Server) listOperations(w http.ResponseWriter, r *http.Request, id string) {
	tasks, err := s.controller.ListOperations(r.Context(), id)
	if err != nil {
		writeAppErr(w, err)
		return
	}
	if tasks == nil {
		tasks = []apps.Task{}
	}
	s.jsonOK(w, tasks)
}

// storageInventory GET /apps/{id}/storage：卷清单（managed/external/bind/socket）+
// 受管数据目录。external 永不删；managed 仅 purge 删。
func (s *Server) storageInventory(w http.ResponseWriter, r *http.Request, id string) {
	inv, err := s.controller.StorageInventory(r.Context(), id)
	if err != nil {
		writeAppErr(w, err)
		return
	}
	s.jsonOK(w, inv)
}

// envInventory GET /apps/{id}/env：环境变量元信息（仅 key/configured/type，不回值）。
func (s *Server) envInventory(w http.ResponseWriter, r *http.Request, id string) {
	inv, err := s.controller.EnvInventory(r.Context(), id)
	if err != nil {
		writeAppErr(w, err)
		return
	}
	s.jsonOK(w, inv)
}

// removePreview GET /apps/{id}/remove-preview?purge=true：明确列出将被删除/保留的资源。
func (s *Server) removePreview(w http.ResponseWriter, r *http.Request, id string) {
	purge := r.URL.Query().Has("purge") && r.URL.Query().Get("purge") != "false"
	pre, err := s.controller.RemovePreview(r.Context(), id, purge)
	if err != nil {
		writeAppErr(w, err)
		return
	}
	s.jsonOK(w, pre)
}

// updateApp PUT /apps/{id}：更新期望状态（乐观并发 via expectedRevision）。
func (s *Server) updateApp(w http.ResponseWriter, r *http.Request, id string) {
	var desired apps.DesiredApplication
	if err := json.NewDecoder(r.Body).Decode(&desired); err != nil {
		http.Error(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}
	desired.ID = id
	if desired.Source.Kind == "" {
		desired.Source.Kind = apps.SourceInline
	}
	task, err := s.controller.Apply(r.Context(), desired, apps.ApplyOptions{
		IdempotencyKey: idempotencyKey(r), Actor: defaultActor, AllowRiskyConfirmation: desired.ConfirmRisky,
	})
	if err != nil {
		writeAppErr(w, err)
		return
	}
	s.jsonStatus(w, http.StatusAccepted, task)
}

// operateAsync POST /apps/{id}/actions/{action}：202 + Task（新 API）。
func (s *Server) operateAsync(w http.ResponseWriter, r *http.Request, id, action string) {
	task, err := s.controller.Operate(r.Context(), id, apps.Action(action), apps.OperationOptions{
		IdempotencyKey: idempotencyKey(r), Actor: defaultActor,
	})
	if err != nil {
		writeAppErr(w, err)
		return
	}
	s.recordEvent(r, eventlog.Input{
		Level: "info", Module: "apps", Event: "应用" + action,
		EventType: "APP_" + strings.ToUpper(action), Outcome: "accepted",
		ResourceKind: "application", ResourceID: id, Payload: map[string]any{"task_id": task.ID},
	})
	s.jsonStatus(w, http.StatusAccepted, task)
}

// operateCompat 旧 action：提交后同步等待（K8s 秒级；Compose 一般 <30s）。
// 超时返回当前 task 状态（仍 200，避免旧前端 appOp 误报失败）。
func (s *Server) operateCompat(w http.ResponseWriter, r *http.Request, id, actionSeg string) {
	action := actionSeg
	if actionSeg == "" { // 来自 /op
		var req apps.AppOperation
		_ = json.NewDecoder(r.Body).Decode(&req)
		action = req.Operation
	}
	if action != "start" && action != "stop" && action != "restart" {
		http.Error(w, "unknown operation: "+action, http.StatusBadRequest)
		return
	}
	task, err := s.controller.Operate(r.Context(), id, apps.Action(action), apps.OperationOptions{Actor: defaultActor})
	if err != nil {
		writeAppErr(w, err)
		return
	}
	task = s.awaitTask(r, task.ID, 30*time.Second)
	body := map[string]string{"status": "ok", "operation": action, "app": id}
	if !task.Status.IsTerminal() {
		body["status"] = "running"
		body["taskId"] = task.ID
	} else if task.Status == apps.TaskFailed {
		writeJSONErrStatus(w, http.StatusInternalServerError, map[string]any{"error": "operation failed: " + task.Message})
		return
	}
	s.recordEvent(r, eventlog.Input{
		Level: "info", Module: "apps", Event: "应用" + action,
		EventType: "APP_" + strings.ToUpper(action), Outcome: "success",
		ResourceKind: "application", ResourceID: id, Payload: map[string]any{"task_id": task.ID},
	})
	s.jsonOK(w, body)
}

// deleteAppCompat DELETE /apps/{id}?purge=true：兼容同步，默认保留数据。
func (s *Server) deleteAppCompat(w http.ResponseWriter, r *http.Request, id string) {
	purge := r.URL.Query().Has("purge") && r.URL.Query().Get("purge") != "false"
	task, err := s.controller.Remove(r.Context(), id, apps.RemoveOptions{
		Purge: purge, Actor: defaultActor,
	})
	if err != nil {
		writeAppErr(w, err)
		return
	}
	task = s.awaitTask(r, task.ID, 30*time.Second)
	body := map[string]string{"status": "ok", "operation": "delete", "app": id}
	if !task.Status.IsTerminal() {
		body["status"] = "running"
		body["taskId"] = task.ID
	} else if task.Status == apps.TaskFailed {
		writeJSONErrStatus(w, http.StatusInternalServerError, map[string]any{"error": "delete failed: " + task.Message})
		return
	}
	s.recordEvent(r, eventlog.Input{
		Level: "warning", Module: "apps", Event: "卸载应用", EventType: "APP_UNINSTALL", Outcome: "success",
		ResourceKind: "application", ResourceID: id, Payload: map[string]any{"purge": purge, "task_id": task.ID},
	})
	s.jsonOK(w, body)
}

func (s *Server) restoreRevision(w http.ResponseWriter, r *http.Request, id, revStr string) {
	rev, err := strconv.ParseInt(revStr, 10, 64)
	if err != nil || rev <= 0 {
		http.Error(w, "invalid revision", http.StatusBadRequest)
		return
	}
	task, err := s.controller.RestoreRevision(r.Context(), id, rev, apps.ApplyOptions{
		IdempotencyKey: idempotencyKey(r), Actor: defaultActor,
	})
	if err != nil {
		writeAppErr(w, err)
		return
	}
	s.jsonStatus(w, http.StatusAccepted, task)
}

// takeoverApp POST /apps/{id}/takeover：把 discovered compose project 接管为受管。
// body 可选（含 confirmRisky）；id 来自路径。成功返回接管后的 managed Application（200）。
//
// 解码硬约束（不依赖 ContentLength，兼容 chunked/HTTP2）：始终 decode；EOF=空 body（confirmRisky
// 默认 false）；其它解码错误 400；超 MaxBytes 上限 413；DisallowUnknownFields；拒绝 trailing JSON。
func (s *Server) takeoverApp(w http.ResponseWriter, r *http.Request, id string) {
	if s.controller == nil {
		http.Error(w, "app controller not initialized", http.StatusServiceUnavailable)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	var body apps.TakeoverRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		if errors.Is(err, io.EOF) {
			// 空 body：confirmRisky 默认 false，允许。
		} else {
			var mb *http.MaxBytesError
			code := http.StatusBadRequest
			msg := "invalid request body"
			if errors.As(err, &mb) {
				code = http.StatusRequestEntityTooLarge
				msg = "request body too large"
			}
			writeJSONErrStatus(w, code, map[string]any{"error": msg})
			return
		}
	} else {
		// 首对象解码成功后，必须恰好到 EOF：第二次 Decode 严格要求 io.EOF。
		// nil（第二对象）/ 任何其它错误（trailing garbage）都视为多余输入 → 400。
		if extra := dec.Decode(&struct{}{}); !errors.Is(extra, io.EOF) {
			writeJSONErrStatus(w, http.StatusBadRequest, map[string]any{"error": "request body must be a single JSON object"})
			return
		}
	}
	body.ID = id
	app, err := s.controller.Takeover(r.Context(), body, apps.ApplyOptions{
		IdempotencyKey: idempotencyKey(r), Actor: defaultActor, AllowRiskyConfirmation: body.ConfirmRisky,
	})
	if err != nil {
		writeAppErr(w, err)
		return
	}
	s.jsonStatus(w, http.StatusOK, app)
}

// awaitTask 轮询任务到终态或超时；监听 request ctx，客户端断开立即返回。
func (s *Server) awaitTask(r *http.Request, taskID string, timeout time.Duration) apps.Task {
	ctx := r.Context()
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	var last apps.Task
	for {
		t, err := s.controller.GetTask(ctx, taskID)
		if err != nil {
			return last
		}
		last = t
		if t.Status.IsTerminal() || time.Now().After(deadline) {
			return t
		}
		select {
		case <-ctx.Done():
			return t
		case <-ticker.C:
		}
	}
}
