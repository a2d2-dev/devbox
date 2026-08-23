package console

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/a2d2-dev/devbox/pkg/auth"
	"github.com/a2d2-dev/devbox/pkg/security"
)

func TestLoginFailuresTriggerBanAndManualUnban(t *testing.T) {
	store, err := security.NewStore("", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Update(security.SettingsUpdate{HTTPPort: 9090, HTTPSPort: 9443}); err != nil {
		t.Fatal(err)
	}
	bans := security.NewBanManager(security.BanRule{Threshold: 2, Window: time.Minute, BanFor: time.Minute})
	s := &Server{auth: auth.New(auth.Config{Password: "correct", SessionTTL: 60}), security: store, bans: bans}
	call := func(password string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]string{"password": password})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/verify", bytes.NewReader(body))
		req.RemoteAddr = "10.0.0.8:42100"
		w := httptest.NewRecorder()
		s.handleAuthVerify(w, req)
		return w
	}
	if got := call("wrong").Code; got != http.StatusUnauthorized {
		t.Fatalf("first failure=%d", got)
	}
	if got := call("wrong").Code; got != http.StatusUnauthorized {
		t.Fatalf("second failure=%d", got)
	}
	if got := call("correct").Code; got != http.StatusTooManyRequests {
		t.Fatalf("banned login=%d", got)
	}
	bans.Unban("10.0.0.8")
	if got := call("correct").Code; got != http.StatusOK {
		t.Fatalf("login after unban=%d", got)
	}
}

func TestLoginRequiresConfiguredAccessCode(t *testing.T) {
	store, _ := security.NewStore("", "")
	if err := store.Update(security.SettingsUpdate{HTTPPort: 9090, HTTPSPort: 9443, AccessCodeEnabled: true, AccessCode: "shared"}); err != nil {
		t.Fatal(err)
	}
	s := &Server{auth: auth.New(auth.Config{Password: "correct"}), security: store, bans: security.NewBanManager(security.BanRule{})}
	login := func(access string) int {
		body, _ := json.Marshal(map[string]string{"password": "correct", "accessCode": access})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/verify", bytes.NewReader(body))
		req.RemoteAddr = "10.0.0.9:42"
		w := httptest.NewRecorder()
		s.handleAuthVerify(w, req)
		return w.Code
	}
	if got := login(""); got != http.StatusUnauthorized {
		t.Fatalf("missing access code=%d", got)
	}
	if got := login("shared"); got != http.StatusOK {
		t.Fatalf("correct access code=%d", got)
	}
}
