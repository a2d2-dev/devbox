package console

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/a2d2-dev/devbox/pkg/apps"
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

// writeAppErr 把领域错误映射到 HTTP 状态码。
func writeAppErr(w http.ResponseWriter, err error) {
	if ae, ok := apps.AsError(err); ok {
		switch ae.Kind {
		case apps.ErrKindNotFound:
			http.Error(w, ae.Message, http.StatusNotFound)
		case apps.ErrKindConflict:
			http.Error(w, ae.Message, http.StatusConflict)
		case apps.ErrKindValidation:
			http.Error(w, ae.Message, http.StatusBadRequest)
		case apps.ErrKindRiskBlocked:
			http.Error(w, ae.Message, http.StatusUnprocessableEntity)
		case apps.ErrKindCapability:
			http.Error(w, ae.Message, http.StatusServiceUnavailable)
		default:
			http.Error(w, ae.Message, http.StatusInternalServerError)
		}
		return
	}
	http.Error(w, err.Error(), http.StatusInternalServerError)
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
		IdempotencyKey: idempotencyKey(r), Actor: defaultActor,
	})
	if err != nil {
		writeAppErr(w, err)
		return
	}
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
	case len(segments) == 2 && segments[1] == "operations" && r.Method == http.MethodGet:
		s.listOperations(w, r, id)
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
	page, err := s.controller.Logs(r.Context(), id, apps.LogOptions{Tail: tail})
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
		IdempotencyKey: idempotencyKey(r), Actor: defaultActor,
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
		http.Error(w, "operation failed: "+task.Message, http.StatusInternalServerError)
		return
	}
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
		http.Error(w, "delete failed: "+task.Message, http.StatusInternalServerError)
		return
	}
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

// awaitTask 轮询任务到终态或超时；用于兼容同步接口。
func (s *Server) awaitTask(r *http.Request, taskID string, timeout time.Duration) apps.Task {
	deadline := time.Now().Add(timeout)
	for {
		t, err := s.controller.GetTask(r.Context(), taskID)
		if err != nil || t.Status.IsTerminal() || time.Now().After(deadline) {
			return t
		}
		time.Sleep(200 * time.Millisecond)
	}
}
