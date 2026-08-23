package console

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/a2d2-dev/devbox/pkg/apps"
	"github.com/a2d2-dev/devbox/pkg/auth"
	eventlog "github.com/a2d2-dev/devbox/pkg/syslog"
	"github.com/a2d2-dev/devbox/pkg/system"
	"go.uber.org/zap"
	"golang.org/x/sys/unix"
)

func newObservabilityTestServer(t *testing.T, password string) *Server {
	t.Helper()
	store, err := eventlog.New(filepath.Join(t.TempDir(), "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{
		logger: zap.NewNop(), auth: auth.New(auth.Config{Password: password}), systemLog: store,
		sessionUsers: make(map[string]string), processResources: newProcessResourceSampler(),
	}
	server.installAuthSessionCleanup()
	return server
}

func TestTerminateProcessRequiresEnabledAuthentication(t *testing.T) {
	server := newObservabilityTestServer(t, "")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/processes/42/terminate", nil)
	rec := httptest.NewRecorder()
	server.handleProcessDetail(rec, req)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "permission_required") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestSuccessfulLoginBindsServerSideAdminIdentity(t *testing.T) {
	server := newObservabilityTestServer(t, "pw")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/verify", strings.NewReader(`{"username":"alice","password":"pw"}`))
	rec := httptest.NewRecorder()
	server.handleAuthVerify(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var response struct {
		Token    string `json:"token"`
		Username string `json:"username"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	authed := httptest.NewRequest(http.MethodGet, "/", nil)
	authed.Header.Set("Authorization", "Bearer "+response.Token)
	if response.Username != "admin" || server.actorFromRequest(authed) != "admin" {
		t.Fatalf("response username=%q actor=%q", response.Username, server.actorFromRequest(authed))
	}
	page := server.systemLog.Query(eventlog.Query{Modules: []string{"auth"}})
	if page.Total != 1 || page.Events[0].Username != "admin" {
		t.Fatalf("untrusted username reached audit: %#v", page)
	}
}

func TestLogoutRevokesTokenAndRemovesSessionIdentity(t *testing.T) {
	server := newObservabilityTestServer(t, "pw")
	server.mux = http.NewServeMux()
	server.registerAuthRoutes()
	token, _ := server.auth.Verify("pw")
	server.sessionUsers[token] = "admin"

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	server.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if server.auth.ValidateToken(token) {
		t.Fatal("logged-out token still validates")
	}
	server.sessionUsersMu.RLock()
	_, exists := server.sessionUsers[token]
	server.sessionUsersMu.RUnlock()
	if exists {
		t.Fatal("session identity leaked after logout")
	}
}

func TestExpiredTokenRemovesSessionIdentity(t *testing.T) {
	server := &Server{auth: auth.New(auth.Config{Password: "pw", SessionTTL: -1}), sessionUsers: make(map[string]string)}
	server.installAuthSessionCleanup()
	token, _ := server.auth.Verify("pw")
	server.sessionUsers[token] = "admin"
	if server.auth.ValidateToken(token) {
		t.Fatal("expired token validated")
	}
	server.sessionUsersMu.RLock()
	_, exists := server.sessionUsers[token]
	server.sessionUsersMu.RUnlock()
	if exists {
		t.Fatal("expired session identity was not removed")
	}
}

func TestRequestIPIgnoresForwardedHeaderByDefault(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "198.51.100.20:4321"
	req.Header.Set("X-Forwarded-For", "203.0.113.7")
	if got := requestIP(req); got != "198.51.100.20" {
		t.Fatalf("requestIP=%q want remote peer", got)
	}
}

func TestRequestIPUsesForwardedChainOnlyForTrustedProxy(t *testing.T) {
	server := &Server{config: Config{TrustedProxies: []string{"10.0.0.0/8"}}}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.2.3.4:4321"
	req.Header.Set("X-Forwarded-For", "203.0.113.7, 10.9.8.7")
	if got := server.requestIP(req); got != "203.0.113.7" {
		t.Fatalf("requestIP=%q want first untrusted hop", got)
	}
}

func TestTerminateProcessReturnsNotFoundBeforeSignal(t *testing.T) {
	server := newObservabilityTestServer(t, "pw")
	token, _ := server.auth.Verify("pw")
	originalOpen := openProcessPIDFD
	called := false
	openProcessPIDFD = func(int, int) (int, error) { called = true; return -1, unix.ESRCH }
	t.Cleanup(func() { openProcessPIDFD = originalOpen })

	req := httptest.NewRequest(http.MethodPost, "/api/v1/processes/4242/terminate", strings.NewReader(`{"startTicks":1}`))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	server.handleProcessDetail(rec, req)
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "process_not_found") || !called {
		t.Fatalf("status=%d called=%v body=%s", rec.Code, called, rec.Body.String())
	}
	page := server.systemLog.Query(eventlog.Query{Modules: []string{"process"}})
	if page.Total != 2 || page.Events[0].Outcome != "failure" || page.Events[1].Outcome != "intent" {
		t.Fatalf("unexpected not-found audit events: %#v", page)
	}
}

func TestTerminateProcessSignalsAndAudits(t *testing.T) {
	server := newObservabilityTestServer(t, "pw")
	token, _ := server.auth.Verify("pw")
	server.sessionUsers[token] = "alice"
	server.processResources.procRoot = t.TempDir()
	procDir := filepath.Join(server.processResources.procRoot, "4242")
	if err := os.MkdirAll(procDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(procDir, "stat"), []byte("4242 (test) S 1 0 0 0 0 0 0 0 0 0 1 1 0 0 20 0 1 0 123 0 0\n"), 0644); err != nil {
		t.Fatal(err)
	}
	originalOpen, originalSignal, originalClose := openProcessPIDFD, signalProcessPIDFD, closeProcessPIDFD
	openProcessPIDFD = func(pid, flags int) (int, error) { return 99, nil }
	signaled := 0
	signalProcessPIDFD = func(fd int, signal unix.Signal, _ *unix.Siginfo, flags int) error { signaled = fd; return nil }
	closeProcessPIDFD = func(int) error { return nil }
	t.Cleanup(func() {
		openProcessPIDFD, signalProcessPIDFD, closeProcessPIDFD = originalOpen, originalSignal, originalClose
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/processes/4242/terminate", strings.NewReader(`{"startTicks":123}`))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	server.handleProcessDetail(rec, req)
	if rec.Code != http.StatusAccepted || signaled != 99 {
		t.Fatalf("status=%d signaled=%d body=%s", rec.Code, signaled, rec.Body.String())
	}
	page := server.systemLog.Query(eventlog.Query{Modules: []string{"process"}})
	if page.Total != 2 || page.Events[0].Username != "alice" || page.Events[0].Outcome != "success" || page.Events[1].Outcome != "intent" {
		t.Fatalf("unexpected audit event: %#v", page)
	}
}

func TestTerminateProcessRefusesExecutionWhenAuditIntentCannotPersist(t *testing.T) {
	root := filepath.Join(t.TempDir(), "audit")
	store, err := eventlog.New(filepath.Join(root, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{
		logger: zap.NewNop(), auth: auth.New(auth.Config{Password: "pw"}), systemLog: store,
		sessionUsers: make(map[string]string), processResources: newProcessResourceSampler(),
	}
	token, _ := server.auth.Verify("pw")
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(root, []byte("blocks log directory recreation"), 0600); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/processes/4242/terminate", strings.NewReader(`{"startTicks":1}`))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	server.handleProcessDetail(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestTerminateProcessRejectsReusedPIDIdentity(t *testing.T) {
	cmd := exec.Command("sleep", "60")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	server := newObservabilityTestServer(t, "pw")
	token, _ := server.auth.Verify("pw")
	fakeProc := t.TempDir()
	procDir := filepath.Join(fakeProc, strconv.Itoa(cmd.Process.Pid))
	if err := os.MkdirAll(procDir, 0755); err != nil {
		t.Fatal(err)
	}
	stat := strconv.Itoa(cmd.Process.Pid) + " (reused) S 1 0 0 0 0 0 0 0 0 0 1 1 0 0 20 0 1 0 222 0 0\n"
	if err := os.WriteFile(filepath.Join(procDir, "stat"), []byte(stat), 0644); err != nil {
		t.Fatal(err)
	}
	server.processResources.procRoot = fakeProc

	req := httptest.NewRequest(http.MethodPost, "/api/v1/processes/"+strconv.Itoa(cmd.Process.Pid)+"/terminate", strings.NewReader(`{"startTicks":111}`))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	server.handleProcessDetail(rec, req)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "process_identity_changed") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	page := server.systemLog.Query(eventlog.Query{Modules: []string{"process"}})
	if page.Total != 2 || page.Events[0].Outcome != "failure" || page.Events[1].Outcome != "intent" {
		t.Fatalf("unexpected PID reuse audit events: %#v", page)
	}
}

func TestTerminateProcessProtectsSystemAndKernelProcesses(t *testing.T) {
	server := newObservabilityTestServer(t, "pw")
	token, _ := server.auth.Verify("pw")
	for _, pid := range []int{1, os.Getpid(), os.Getppid()} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/processes/"+strconv.Itoa(pid)+"/terminate", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		server.handleProcessDetail(rec, req)
		if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "protected_process") {
			t.Fatalf("pid=%d status=%d body=%s", pid, rec.Code, rec.Body.String())
		}
	}

	server.processResources.procRoot = t.TempDir()
	procDir := filepath.Join(server.processResources.procRoot, "4242")
	if err := os.MkdirAll(procDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(procDir, "stat"), []byte("4242 (kworker) S 2 0 0 0 0 2097152 0 0 0 0 1 1 0 0 20 0 1 0 123 0 0\n"), 0644); err != nil {
		t.Fatal(err)
	}
	originalOpen, originalSignal, originalClose := openProcessPIDFD, signalProcessPIDFD, closeProcessPIDFD
	openProcessPIDFD = func(int, int) (int, error) { return 99, nil }
	signalCalled := false
	signalProcessPIDFD = func(int, unix.Signal, *unix.Siginfo, int) error { signalCalled = true; return nil }
	closeProcessPIDFD = func(int) error { return nil }
	t.Cleanup(func() {
		openProcessPIDFD, signalProcessPIDFD, closeProcessPIDFD = originalOpen, originalSignal, originalClose
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/processes/4242/terminate", strings.NewReader(`{"startTicks":123}`))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	server.handleProcessDetail(rec, req)
	if rec.Code != http.StatusForbidden || signalCalled || !strings.Contains(rec.Body.String(), "protected_process") {
		t.Fatalf("kernel thread status=%d signalCalled=%v body=%s", rec.Code, signalCalled, rec.Body.String())
	}
}

func TestTerminateProcessAuditsSignalFailure(t *testing.T) {
	server := newObservabilityTestServer(t, "pw")
	token, _ := server.auth.Verify("pw")
	server.processResources.procRoot = t.TempDir()
	procDir := filepath.Join(server.processResources.procRoot, "4242")
	if err := os.MkdirAll(procDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(procDir, "stat"), []byte("4242 (test) S 1 0 0 0 0 0 0 0 0 0 1 1 0 0 20 0 1 0 123 0 0\n"), 0644); err != nil {
		t.Fatal(err)
	}
	originalOpen, originalSignal, originalClose := openProcessPIDFD, signalProcessPIDFD, closeProcessPIDFD
	openProcessPIDFD = func(int, int) (int, error) { return 99, nil }
	signalProcessPIDFD = func(int, unix.Signal, *unix.Siginfo, int) error { return unix.EPERM }
	closeProcessPIDFD = func(int) error { return nil }
	t.Cleanup(func() {
		openProcessPIDFD, signalProcessPIDFD, closeProcessPIDFD = originalOpen, originalSignal, originalClose
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/processes/4242/terminate", strings.NewReader(`{"startTicks":123}`))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	server.handleProcessDetail(rec, req)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "permission_denied") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	page := server.systemLog.Query(eventlog.Query{Modules: []string{"process"}})
	if page.Total != 2 || page.Events[0].Outcome != "failure" || page.Events[1].Outcome != "intent" {
		t.Fatalf("unexpected signal failure audit events: %#v", page)
	}
}

func TestAuditHandlerFiltersPagesAndClearAuditsItself(t *testing.T) {
	server := newObservabilityTestServer(t, "pw")
	token, _ := server.auth.Verify("pw")
	server.sessionUsers[token] = "admin"
	_, _ = server.systemLog.Append(eventlog.Input{Level: "info", Module: "auth", Event: "one"})
	_, _ = server.systemLog.Append(eventlog.Input{Level: "error", Module: "process", Event: "two"})

	get := httptest.NewRequest(http.MethodGet, "/api/v1/audit/events?level=error&module=process&limit=1&offset=0", nil)
	rec := httptest.NewRecorder()
	server.handleAuditEvents(rec, get)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"total":1`) || !strings.Contains(rec.Body.String(), `"module":"process"`) {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	del := httptest.NewRequest(http.MethodDelete, "/api/v1/audit/events", nil)
	del.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	server.handleAuditEvents(rec, del)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	page := server.systemLog.Query(eventlog.Query{})
	if page.Total != 2 || page.Events[0].EventType != "LOG_CLEAR" || page.Events[0].Outcome != "success" || page.Events[1].Outcome != "intent" || page.Events[0].Username != "admin" {
		t.Fatalf("unexpected events after clear: %#v", page)
	}
}

func TestClearAuditLogRefusesExecutionWhenIntentCannotPersist(t *testing.T) {
	root := filepath.Join(t.TempDir(), "audit")
	store, err := eventlog.New(filepath.Join(root, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	_, _ = store.Append(eventlog.Input{Event: "must remain"})
	server := &Server{
		logger: zap.NewNop(), auth: auth.New(auth.Config{Password: "pw"}), systemLog: store,
		sessionUsers: make(map[string]string),
	}
	token, _ := server.auth.Verify("pw")
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(root, []byte("blocks log directory recreation"), 0600); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/audit/events", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	server.handleAuditEvents(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if page := store.Query(eventlog.Query{}); page.Total != 1 || page.Events[0].Event != "must remain" {
		t.Fatalf("clear ran despite failed intent: %#v", page)
	}
}

func TestFailedAsyncTaskWritesFailureAudit(t *testing.T) {
	controller := &stubController{}
	server := newObservabilityTestServer(t, "pw")
	server.controller = controller
	server.installTaskAuditObserver()
	if controller.taskObserver == nil {
		t.Fatal("task observer was not registered")
	}
	controller.taskObserver(apps.Task{
		ID: "task-failed", AppID: "demo", Type: apps.TaskApply, Status: apps.TaskFailed, Message: "pull failed",
	})
	page := server.systemLog.Query(eventlog.Query{Modules: []string{"apps"}})
	if page.Total != 1 || page.Events[0].Outcome != "failure" || page.Events[0].Payload["task_id"] != "task-failed" {
		t.Fatalf("unexpected terminal task audit: %#v", page)
	}
}

func TestAsyncTaskAcceptanceAuditUsesAcceptedOutcome(t *testing.T) {
	controller := &stubController{applyTask: apps.Task{ID: "task-accepted", Status: apps.TaskQueued}}
	server := newObservabilityTestServer(t, "pw")
	server.controller = controller
	req := httptest.NewRequest(http.MethodPost, "/api/v1/apps", strings.NewReader(`{"id":"demo","name":"demo","composeContent":"services:\n  demo:\n    image: nginx"}`))
	rec := httptest.NewRecorder()
	server.createApp(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	page := server.systemLog.Query(eventlog.Query{Modules: []string{"apps"}})
	if page.Total != 1 || page.Events[0].Outcome != "accepted" || page.Events[0].Payload["task_id"] != "task-accepted" {
		t.Fatalf("unexpected acceptance audit: %#v", page)
	}
}

func TestSystemLogInitializationFailureDisablesDangerousEndpoints(t *testing.T) {
	blocked := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocked, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	server := NewServer(zap.NewNop(), Config{
		AuthPassword: "pw", SystemLogPath: filepath.Join(blocked, "events.jsonl"), BrowserDataPath: filepath.Join(t.TempDir(), "browser.json"),
	}, nil, nil, nil)
	if server.systemLog != nil {
		t.Fatal("system log unexpectedly initialized")
	}
	token, _ := server.auth.Verify("pw")
	terminate := httptest.NewRequest(http.MethodPost, "/api/v1/processes/1/terminate", nil)
	terminate.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	server.handleProcessDetail(rec, terminate)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("terminate status=%d body=%s", rec.Code, rec.Body.String())
	}
	clear := httptest.NewRequest(http.MethodDelete, "/api/v1/audit/events", nil)
	clear.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	server.handleAuditEvents(rec, clear)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("clear status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestProcessStatAndAddressParsers(t *testing.T) {
	root := t.TempDir()
	procDir := filepath.Join(root, "42")
	if err := os.MkdirAll(procDir, 0755); err != nil {
		t.Fatal(err)
	}
	stat := "42 (name with spaces) S 1 0 0 0 0 0 0 0 0 0 120 30 0 0 20 0 1 0 98765 0 0\n"
	if err := os.WriteFile(filepath.Join(procDir, "stat"), []byte(stat), 0644); err != nil {
		t.Fatal(err)
	}
	cpu, start, err := readProcessStat(root, 42)
	if err != nil || cpu != 150 || start != 98765 {
		t.Fatalf("cpu=%d start=%d err=%v", cpu, start, err)
	}
	for input, want := range map[string]int{"0.0.0.0:9092": 9092, "[::]:443": 443, "*:22": 22} {
		got, ok := parseAddressPort(input)
		if !ok || got != want {
			t.Fatalf("parseAddressPort(%q)=%d,%v want=%d", input, got, ok, want)
		}
	}
}

func TestProcessSamplerDoesNotReuseHistoryAcrossPIDIdentity(t *testing.T) {
	root := t.TempDir()
	procDir := filepath.Join(root, "42")
	if err := os.MkdirAll(procDir, 0755); err != nil {
		t.Fatal(err)
	}
	writeSample := func(global, cpu, start uint64) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, "stat"), []byte("cpu  "+strconv.FormatUint(global, 10)+" 0 0 0\n"), 0644); err != nil {
			t.Fatal(err)
		}
		stat := "42 (same-name) S 1 0 0 0 0 0 0 0 0 0 " + strconv.FormatUint(cpu, 10) + " 0 0 0 20 0 1 0 " + strconv.FormatUint(start, 10) + " 0 0\n"
		if err := os.WriteFile(filepath.Join(procDir, "stat"), []byte(stat), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(procDir, "io"), []byte("read_bytes: 10\nwrite_bytes: 20\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	sampler := newProcessResourceSampler()
	sampler.procRoot = root
	basic := []system.ProcessBasic{{PID: 42, Name: "same-name"}}
	writeSample(100, 10, 111)
	_ = sampler.sample(t.Context(), basic)
	writeSample(200, 20, 222)
	got := sampler.sample(t.Context(), basic)
	if len(got) != 1 || got[0].CPUPercent != nil || got[0].ReadBps != nil || got[0].WriteBps != nil {
		t.Fatalf("reused PID inherited historical rates: %#v", got)
	}
}

func TestReadProcessIOReturnsUnavailableOnMalformedCounter(t *testing.T) {
	root := t.TempDir()
	procDir := filepath.Join(root, "42")
	if err := os.MkdirAll(procDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(procDir, "io"), []byte("read_bytes: invalid\nwrite_bytes: 20\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if read, write, ok := readProcessIO(root, 42); ok {
		t.Fatalf("malformed I/O reported available: read=%d write=%d", read, write)
	}
}
