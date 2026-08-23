package console

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/a2d2-dev/devbox/pkg/auth"
	eventlog "github.com/a2d2-dev/devbox/pkg/syslog"
	"go.uber.org/zap"
)

func newObservabilityTestServer(t *testing.T, password string) *Server {
	t.Helper()
	store, err := eventlog.New(filepath.Join(t.TempDir(), "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	return &Server{
		logger: zap.NewNop(), auth: auth.New(auth.Config{Password: password}), systemLog: store,
		sessionUsers: make(map[string]string), processResources: newProcessResourceSampler(),
	}
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

func TestTerminateProcessReturnsNotFoundBeforeSignal(t *testing.T) {
	server := newObservabilityTestServer(t, "pw")
	token, _ := server.auth.Verify("pw")
	originalExists, originalTerminate := processExists, terminateProcess
	processExists = func(int) bool { return false }
	called := false
	terminateProcess = func(int) error { called = true; return nil }
	t.Cleanup(func() { processExists, terminateProcess = originalExists, originalTerminate })

	req := httptest.NewRequest(http.MethodPost, "/api/v1/processes/4242/terminate", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	server.handleProcessDetail(rec, req)
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "process_not_found") || called {
		t.Fatalf("status=%d called=%v body=%s", rec.Code, called, rec.Body.String())
	}
}

func TestTerminateProcessSignalsAndAudits(t *testing.T) {
	server := newObservabilityTestServer(t, "pw")
	token, _ := server.auth.Verify("pw")
	server.sessionUsers[token] = "alice"
	originalExists, originalTerminate := processExists, terminateProcess
	processExists = func(pid int) bool { return pid == 4242 }
	signaled := 0
	terminateProcess = func(pid int) error { signaled = pid; return nil }
	t.Cleanup(func() { processExists, terminateProcess = originalExists, originalTerminate })

	req := httptest.NewRequest(http.MethodPost, "/api/v1/processes/4242/terminate", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	server.handleProcessDetail(rec, req)
	if rec.Code != http.StatusAccepted || signaled != 4242 {
		t.Fatalf("status=%d signaled=%d body=%s", rec.Code, signaled, rec.Body.String())
	}
	page := server.systemLog.Query(eventlog.Query{Modules: []string{"process"}})
	if page.Total != 1 || page.Events[0].Username != "alice" || page.Events[0].EventType != "PROCESS_TERMINATE" {
		t.Fatalf("unexpected audit event: %#v", page)
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
	if page.Total != 1 || page.Events[0].EventType != "LOG_CLEAR" || page.Events[0].Username != "admin" {
		t.Fatalf("unexpected events after clear: %#v", page)
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
