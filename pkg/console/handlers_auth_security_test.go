package console

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/a2d2-dev/devbox/pkg/auth"
	devnetwork "github.com/a2d2-dev/devbox/pkg/network"
	"github.com/a2d2-dev/devbox/pkg/security"
	"github.com/pquerna/otp/totp"
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
	if err := store.Update(security.SettingsUpdate{HTTPPort: 9090, HTTPSPort: 9443, AccessCodeEnabled: true, AccessCode: "shared-code"}); err != nil {
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
	if got := login("shared-code"); got != http.StatusOK {
		t.Fatalf("correct access code=%d", got)
	}
}

func TestPasswordlessTOTPRequiresOTPAndProtectsAPIsWithRealToken(t *testing.T) {
	store, _ := security.NewStore("", "")
	enrollment, err := store.BeginTOTP("DevBox", "admin")
	if err != nil {
		t.Fatal(err)
	}
	confirm, _ := totp.GenerateCode(enrollment.Secret, time.Now())
	if _, err := store.ConfirmTOTP(confirm); err != nil {
		t.Fatal(err)
	}
	s := &Server{auth: auth.New(auth.Config{}), security: store, bans: security.NewBanManager(security.BanRule{}), mux: http.NewServeMux()}
	s.mux.HandleFunc("/api/v1/private", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })

	anonymous := httptest.NewRecorder()
	s.authGate(s.mux).ServeHTTP(anonymous, httptest.NewRequest(http.MethodGet, "/api/v1/private", nil))
	if anonymous.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous API status=%d", anonymous.Code)
	}

	missing := loginRequest(t, s, "10.0.0.20", map[string]string{})
	if missing.Code != http.StatusUnauthorized {
		t.Fatalf("missing OTP login status=%d", missing.Code)
	}

	code, _ := totp.GenerateCode(enrollment.Secret, time.Now().Add(30*time.Second))
	loggedIn := loginRequest(t, s, "10.0.0.20", map[string]string{"otp": code})
	if loggedIn.Code != http.StatusOK {
		t.Fatalf("valid passwordless TOTP login status=%d body=%s", loggedIn.Code, loggedIn.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(loggedIn.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	token, _ := body["token"].(string)
	if token == "" {
		t.Fatal("passwordless security-factor login returned an empty token")
	}
	authorized := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/private", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	s.authGate(s.mux).ServeHTTP(authorized, req)
	if authorized.Code != http.StatusNoContent {
		t.Fatalf("token-protected API status=%d", authorized.Code)
	}
}

func TestPasswordlessAccessCodeLoginReturnsRealToken(t *testing.T) {
	store, _ := security.NewStore("", "")
	if err := store.Update(security.SettingsUpdate{HTTPPort: 9090, HTTPSPort: 9443, AccessCodeEnabled: true, AccessCode: "shared-code"}); err != nil {
		t.Fatal(err)
	}
	s := &Server{auth: auth.New(auth.Config{}), security: store, bans: security.NewBanManager(security.BanRule{})}
	missing := loginRequest(t, s, "10.0.0.21", map[string]string{})
	if missing.Code != http.StatusUnauthorized {
		t.Fatalf("missing access code status=%d", missing.Code)
	}
	loggedIn := loginRequest(t, s, "10.0.0.21", map[string]string{"accessCode": "shared-code"})
	if loggedIn.Code != http.StatusOK {
		t.Fatalf("valid access code status=%d body=%s", loggedIn.Code, loggedIn.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(loggedIn.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if token, _ := body["token"].(string); token == "" {
		t.Fatal("passwordless access-code login returned an empty token")
	}
}

func TestMissingOrInvalidOTPCountsAsFailureBeforeSessionCreation(t *testing.T) {
	store, _ := security.NewStore("", "")
	enrollment, _ := store.BeginTOTP("DevBox", "admin")
	confirm, _ := totp.GenerateCode(enrollment.Secret, time.Now())
	if _, err := store.ConfirmTOTP(confirm); err != nil {
		t.Fatal(err)
	}
	a := auth.New(auth.Config{Password: "correct", SessionTTL: -1})
	removed := 0
	a.SetSessionRemovedHook(func(string) { removed++ })
	bans := security.NewBanManager(security.BanRule{Threshold: 2, Window: time.Minute, BanFor: time.Minute})
	s := &Server{auth: a, security: store, bans: bans}
	if got := loginRequest(t, s, "10.0.0.30", map[string]string{"password": "correct"}).Code; got != http.StatusUnauthorized {
		t.Fatalf("missing OTP status=%d", got)
	}
	if got := loginRequest(t, s, "10.0.0.30", map[string]string{"password": "correct", "otp": "000000"}).Code; got != http.StatusUnauthorized {
		t.Fatalf("invalid OTP status=%d", got)
	}
	a.PruneExpired()
	if removed != 0 {
		t.Fatalf("failed OTP created %d sessions", removed)
	}
	code, _ := totp.GenerateCode(enrollment.Secret, time.Now().Add(30*time.Second))
	if got := loginRequest(t, s, "10.0.0.30", map[string]string{"password": "correct", "otp": code}).Code; got != http.StatusTooManyRequests {
		t.Fatalf("OTP failures did not trigger ban: status=%d", got)
	}
}

func TestSecuritySettingsAndPreviewRedactCredentialReference(t *testing.T) {
	store, _ := security.NewStore("", "")
	update := security.SettingsUpdate{HTTPPort: 9090, HTTPSPort: 9443, DDNSProvider: "cloudflare", DDNSDomain: "example.com", DDNSCredentialRef: "env:HIGH_VALUE_TOKEN"}
	if err := store.Update(update); err != nil {
		t.Fatal(err)
	}
	s := &Server{security: store, certificates: security.NewCertificateManager(t.TempDir()), network: devNetworkCollectorForSettingsTest()}
	get := httptest.NewRecorder()
	s.handleSecuritySettings(get, httptest.NewRequest(http.MethodGet, "/api/v1/security/settings", nil))
	if strings.Contains(get.Body.String(), "HIGH_VALUE_TOKEN") || !strings.Contains(get.Body.String(), "REDACTED") {
		t.Fatalf("settings response was not redacted: %s", get.Body.String())
	}
	payload, _ := json.Marshal(update)
	preview := httptest.NewRecorder()
	s.handleSecuritySettingsPreview(preview, httptest.NewRequest(http.MethodPost, "/api/v1/security/settings/preview", bytes.NewReader(payload)))
	if preview.Code != http.StatusOK {
		t.Fatalf("preview status=%d body=%s", preview.Code, preview.Body.String())
	}
	if strings.Contains(preview.Body.String(), "HIGH_VALUE_TOKEN") || !strings.Contains(preview.Body.String(), "REDACTED") {
		t.Fatalf("preview response was not redacted: %s", preview.Body.String())
	}
}

func TestCertificatePreviewRejectsExpiredUploadWithBadRequest(t *testing.T) {
	generator := security.NewCertificateManager(t.TempDir())
	generator.Now = func() time.Time { return time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC) }
	if _, err := generator.SelfSigned("expired", []string{"expired.example"}, 1); err != nil {
		t.Fatal(err)
	}
	certPath, keyPath, err := generator.Paths("expired")
	if err != nil {
		t.Fatal(err)
	}
	certPEM, _ := os.ReadFile(certPath)
	keyPEM, _ := os.ReadFile(keyPath)
	manager := security.NewCertificateManager(t.TempDir())
	manager.Now = func() time.Time { return time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC) }
	s := &Server{certificates: manager}
	payload, _ := json.Marshal(map[string]string{"name": "expired", "certificate": string(certPEM), "privateKey": string(keyPEM)})
	w := httptest.NewRecorder()
	s.handleCertificatePreview(w, httptest.NewRequest(http.MethodPost, "/api/v1/security/certificates/preview", bytes.NewReader(payload)))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expired certificate preview status=%d body=%s", w.Code, w.Body.String())
	}
}

func loginRequest(t *testing.T, s *Server, ip string, values map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(values)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/verify", bytes.NewReader(body))
	req.RemoteAddr = ip + ":42100"
	w := httptest.NewRecorder()
	s.handleAuthVerify(w, req)
	return w
}

func devNetworkCollectorForSettingsTest() *devnetwork.Collector {
	collector := devnetwork.NewCollector()
	collector.Run = func(_ string, args ...string) ([]byte, error) {
		if strings.Join(args, " ") == "-j addr show" || strings.Join(args, " ") == "-j route show default" {
			return []byte("[]"), nil
		}
		return []byte(""), nil
	}
	return collector
}

func TestLoginAcceptsTOTPAndOneTimeRecoveryCode(t *testing.T) {
	store, err := security.NewStore("", "")
	if err != nil {
		t.Fatal(err)
	}
	enrollment, err := store.BeginTOTP("DevBox", "admin")
	if err != nil {
		t.Fatal(err)
	}
	code, err := totp.GenerateCode(enrollment.Secret, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	recoveryCodes, err := store.ConfirmTOTP(code)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{auth: auth.New(auth.Config{Password: "correct"}), security: store, bans: security.NewBanManager(security.BanRule{})}
	login := func(otp string) int {
		body, _ := json.Marshal(map[string]string{"password": "correct", "otp": otp})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/verify", bytes.NewReader(body))
		req.RemoteAddr = "10.0.0.10:42"
		w := httptest.NewRecorder()
		s.handleAuthVerify(w, req)
		return w.Code
	}
	if got := login(""); got != http.StatusUnauthorized {
		t.Fatalf("missing TOTP=%d", got)
	}
	code, err = totp.GenerateCode(enrollment.Secret, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if got := login(code); got != http.StatusOK {
		t.Fatalf("valid TOTP=%d", got)
	}
	if got := login(recoveryCodes[0]); got != http.StatusOK {
		t.Fatalf("valid recovery code=%d", got)
	}
	if got := login(recoveryCodes[0]); got != http.StatusUnauthorized {
		t.Fatalf("reused recovery code=%d", got)
	}
}
