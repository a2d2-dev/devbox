package console

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/a2d2-dev/devbox/pkg/auth"
	"github.com/a2d2-dev/devbox/pkg/maintenance"
	"github.com/a2d2-dev/devbox/pkg/shares"
	"go.uber.org/zap"
)

func TestMaintenanceSettingsRedactsPasswordAndRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	store, err := maintenance.NewStore(t.TempDir(), root)
	if err != nil {
		t.Fatal(err)
	}
	state := store.Get()
	state.SMTP.Password = "do-not-leak"
	if err := store.Save(state); err != nil {
		t.Fatal(err)
	}
	s := &Server{maintenanceStore: store, webdav: shares.NewWebDAVService(), config: Config{WorkDir: root, AuthPassword: "console-secret"}}

	w := httptest.NewRecorder()
	s.handleMaintenanceSettings(w, httptest.NewRequest(http.MethodGet, "/api/v1/maintenance/settings", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET status = %d: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "do-not-leak") || !strings.Contains(w.Body.String(), `"smtpPasswordSet":true`) {
		t.Fatalf("password response was not redacted: %s", w.Body.String())
	}

	req := settingsRequest{
		WebDAV:      shares.WebDAVConfig{Port: 19000, Path: "/"},
		Updates:     maintenance.UpdateConfig{CheckEnabled: true, Repository: "a2d2-dev/devbox"},
		DefaultApps: map[string]string{},
	}
	body, _ := json.Marshal(req)
	w = httptest.NewRecorder()
	s.handleMaintenanceSettings(w, httptest.NewRequest(http.MethodPut, "/api/v1/maintenance/settings", bytes.NewReader(body)))
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "data root") {
		t.Fatalf("traversal status/body = %d %s", w.Code, w.Body.String())
	}
}

type recordingNotifier chan maintenance.Notification

func (n recordingNotifier) Notify(_ context.Context, event maintenance.Notification) error {
	n <- event
	return nil
}

func TestLoginFailureTriggersNotificationWithoutPassword(t *testing.T) {
	notifications := make(recordingNotifier, 1)
	s := &Server{
		auth:     auth.New(auth.Config{Password: "correct", SessionTTL: 60}),
		notifier: notifications,
		logger:   zap.NewNop(),
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/auth/verify", strings.NewReader(`{"password":"wrong-secret"}`))
	s.handleAuthVerify(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", w.Code)
	}
	select {
	case event := <-notifications:
		if strings.Contains(event.Body, "wrong-secret") || !strings.Contains(event.Subject, "登录失败") {
			t.Fatalf("unsafe notification: %+v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("login failure notification was not emitted")
	}
}

func TestResetRequiresBothConfirmations(t *testing.T) {
	root := t.TempDir()
	store, err := maintenance.NewStore(t.TempDir(), root)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{maintenanceStore: store, webdav: shares.NewWebDAVService(), pendingRestores: make(map[string]pendingRestore)}
	w := httptest.NewRecorder()
	s.handleDevBoxReset(w, httptest.NewRequest(http.MethodPost, "/api/v1/maintenance/reset", strings.NewReader(`{"confirm":true,"phrase":"wrong"}`)))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", w.Code)
	}
}
