package console

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/a2d2-dev/devbox/pkg/auth"
	eventlog "github.com/a2d2-dev/devbox/pkg/syslog"
	"go.uber.org/zap"
)

func newAccountSessionsTestServer(t *testing.T) *Server {
	t.Helper()
	store, err := eventlog.New(filepath.Join(t.TempDir(), "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{
		logger:       zap.NewNop(),
		auth:         auth.New(auth.Config{Password: "pw"}),
		systemLog:    store,
		sessionUsers: make(map[string]string),
	}
	server.installAuthSessionCleanup()
	server.mux = http.NewServeMux()
	server.registerAccountSessionRoutes()
	return server
}

// issueLogin creates a live session for username and records a LOGIN_SUCCESS
// audit event that carries a full IP and raw User-Agent, mimicking production.
func issueLogin(t *testing.T, s *Server, username, ip, ua string) string {
	t.Helper()
	token := s.auth.IssueSession(auth.Principal{Username: username, DisplayName: username})
	s.sessionUsers[token] = username
	if _, err := s.systemLog.Append(eventlog.Input{
		Level: "info", Module: "auth", Username: username,
		Event: "本地登录成功", EventType: "LOGIN_SUCCESS", Outcome: "success",
		SourceIP: ip, UserAgent: ua,
	}); err != nil {
		t.Fatal(err)
	}
	return token
}

func TestAccountSessionsRequiresSession(t *testing.T) {
	s := newAccountSessionsTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/account/sessions", nil)
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestLogoutOthersRequiresSession(t *testing.T) {
	s := newAccountSessionsTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/account/logout-others", nil)
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// TestAccountSessionsLegacyAdminOK verifies the login-history endpoint is
// unaffected by the empty-UserID case that broke preferences. The sessions
// handler works purely by principal.Username (which the legacy single-password
// admin always has) and never inspects UserID, so a legacy admin session must
// return 200 — not 401 or 403 — even though its principal has no users row.
func TestAccountSessionsLegacyAdminOK(t *testing.T) {
	s := newAccountSessionsTestServer(t)
	// issueLogin mints a principal with no UserID, exactly the legacy shape.
	token := issueLogin(t, s, "admin", "203.0.113.9", "curl/8.0")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/account/sessions", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("legacy admin sessions: expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var sessions []accountSession
	if err := json.Unmarshal(rec.Body.Bytes(), &sessions); err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session row for legacy admin, got %d", len(sessions))
	}
}

func TestAccountSessionsMasksIPAndUA(t *testing.T) {
	s := newAccountSessionsTestServer(t)
	fullIP := "203.0.113.42"
	rawUA := "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
	token := issueLogin(t, s, "alice", fullIP, rawUA)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/account/sessions", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()

	// Adversarial redaction assertions: the response must not contain the full
	// IP, the raw UA, or the token.
	if strings.Contains(body, fullIP) {
		t.Fatalf("full IP leaked: %s", body)
	}
	if strings.Contains(body, "AppleWebKit") || strings.Contains(body, "Mozilla") {
		t.Fatalf("raw UA leaked: %s", body)
	}
	if strings.Contains(body, token) {
		t.Fatalf("token leaked: %s", body)
	}

	var sessions []accountSession
	if err := json.Unmarshal(rec.Body.Bytes(), &sessions); err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	got := sessions[0]
	if got.IPMasked != "203.0.113.x" {
		t.Fatalf("ipMasked=%q, want 203.0.113.x", got.IPMasked)
	}
	if got.DeviceLabel != "Chrome · macOS" {
		t.Fatalf("deviceLabel=%q, want Chrome · macOS", got.DeviceLabel)
	}
	if got.DeviceType != "desktop" {
		t.Fatalf("deviceType=%q, want desktop", got.DeviceType)
	}
	if !got.Current {
		t.Fatalf("single session should be current")
	}
}

func TestAccountSessionsCurrentExactlyOne(t *testing.T) {
	s := newAccountSessionsTestServer(t)
	// Two logins for the same user -> two history rows, exactly one current.
	issueLogin(t, s, "bob", "198.51.100.5", "curl/8.0")
	token := issueLogin(t, s, "bob", "198.51.100.9", "Mozilla/5.0 Firefox/121.0")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/account/sessions", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var sessions []accountSession
	if err := json.Unmarshal(rec.Body.Bytes(), &sessions); err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(sessions))
	}
	currents := 0
	for _, row := range sessions {
		if row.Current {
			currents++
		}
	}
	if currents != 1 {
		t.Fatalf("expected exactly one current, got %d", currents)
	}
	// Newest row is current (index 0, reverse-chronological).
	if !sessions[0].Current {
		t.Fatalf("most recent row should be current: %#v", sessions)
	}
}

func TestAccountSessionsOnlyOwnRows(t *testing.T) {
	s := newAccountSessionsTestServer(t)
	issueLogin(t, s, "carol", "192.0.2.1", "curl/8.0")
	token := issueLogin(t, s, "dave", "192.0.2.2", "curl/8.0")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/account/sessions", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	var sessions []accountSession
	if err := json.Unmarshal(rec.Body.Bytes(), &sessions); err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("dave should see only their own row, got %d: %#v", len(sessions), sessions)
	}
}

func TestLogoutOthersRevokesOtherTokensKeepsCurrent(t *testing.T) {
	s := newAccountSessionsTestServer(t)
	other1 := issueLogin(t, s, "erin", "192.0.2.10", "curl/8.0")
	other2 := issueLogin(t, s, "erin", "192.0.2.11", "curl/8.0")
	current := issueLogin(t, s, "erin", "192.0.2.12", "curl/8.0")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/account/logout-others", nil)
	req.Header.Set("Authorization", "Bearer "+current)
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d body=%s", rec.Code, rec.Body.String())
	}
	if s.auth.ValidateToken(other1) || s.auth.ValidateToken(other2) {
		t.Fatal("other sessions should be revoked")
	}
	if !s.auth.ValidateToken(current) {
		t.Fatal("current session must remain valid")
	}
	// Audit event written.
	page := s.systemLog.Query(eventlog.Query{Username: "erin"})
	found := false
	for _, ev := range page.Events {
		if ev.EventType == "LOGOUT_OTHERS" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected LOGOUT_OTHERS audit event")
	}
}

func TestLogoutOthersDoesNotTouchOtherUsers(t *testing.T) {
	s := newAccountSessionsTestServer(t)
	mine := issueLogin(t, s, "frank", "192.0.2.20", "curl/8.0")
	theirs := issueLogin(t, s, "grace", "192.0.2.21", "curl/8.0")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/account/logout-others", nil)
	req.Header.Set("Authorization", "Bearer "+mine)
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}
	if !s.auth.ValidateToken(theirs) {
		t.Fatal("another user's session must not be revoked")
	}
}

func TestMaskIP(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"203.0.113.7", "203.0.113.x"},
		{"203.0.113.7:5555", "203.0.113.x"},
		{"10.0.0.255", "10.0.0.x"},
		{"not-an-ip", ""},
		{"2001:db8:1234:5678::1", "2001:db8::/32"},
		{"[2001:db8:1234::1]:443", "2001:db8::/32"},
		{"::1", "0:0::/32"},
	}
	for _, c := range cases {
		if got := maskIP(c.in); got != c.want {
			t.Errorf("maskIP(%q)=%q, want %q", c.in, got, c.want)
		}
	}
}

func TestParseUA(t *testing.T) {
	cases := []struct {
		ua         string
		label      string
		deviceType string
	}{
		{"", "Unknown device", "unknown"},
		{
			"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) Chrome/120.0 Safari/537.36",
			"Chrome · macOS", "desktop",
		},
		{
			"Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) Mobile/15E148 Safari/604.1",
			"Safari · iOS", "mobile",
		},
		{
			"Mozilla/5.0 (Linux; Android 14) Chrome/120.0 Mobile Safari/537.36",
			"Chrome · Android", "mobile",
		},
		{
			"Mozilla/5.0 (Windows NT 10.0; Win64; x64) Firefox/121.0",
			"Firefox · Windows", "desktop",
		},
		{"curl/8.0.1", "curl · Unknown OS", "unknown"},
	}
	for _, c := range cases {
		label, deviceType := parseUA(c.ua)
		if label != c.label || deviceType != c.deviceType {
			t.Errorf("parseUA(%q)=(%q,%q), want (%q,%q)", c.ua, label, deviceType, c.label, c.deviceType)
		}
	}
}
