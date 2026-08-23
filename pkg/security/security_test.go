package security

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
)

func TestUploadLimiterUsesConfiguredRate(t *testing.T) {
	now := time.Unix(100, 0)
	var slept time.Duration
	r := &byteLimiter{inner: strings.NewReader("0123456789"), bytesPerSecond: 5, now: func() time.Time { return now }, sleep: func(d time.Duration) { slept = d }, started: now}
	b := make([]byte, 10)
	n, err := r.Read(b)
	if err != nil && err != io.EOF {
		t.Fatal(err)
	}
	if n != 5 || slept != time.Second {
		t.Fatalf("n=%d sleep=%s", n, slept)
	}
}

func TestTOTPEnrollmentRecoveryAndEncryptedPersistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	keyPath := filepath.Join(dir, "master.key")
	store, err := NewStore(path, keyPath)
	if err != nil {
		t.Fatal(err)
	}
	enroll, err := store.BeginTOTP("DevBox", "admin")
	if err != nil {
		t.Fatal(err)
	}
	code, err := totp.GenerateCode(enroll.Secret, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	recovery, err := store.ConfirmTOTP(code)
	if err != nil {
		t.Fatal(err)
	}
	if len(recovery) != 10 || !store.Settings().TOTPEnabled {
		t.Fatalf("unexpected enrollment result: %#v", recovery)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), enroll.Secret) || strings.Contains(string(b), recovery[0]) {
		t.Fatal("sensitive TOTP material was stored in plaintext")
	}
	reloaded, err := NewStore(path, keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if !reloaded.VerifySecondFactor(recovery[0], time.Now()) {
		t.Fatal("recovery code should work")
	}
	if reloaded.VerifySecondFactor(recovery[0], time.Now()) {
		t.Fatal("recovery code must be one-time")
	}
	code, err = totp.GenerateCode(enroll.Secret, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !reloaded.VerifySecondFactor(code, time.Now()) {
		t.Fatal("TOTP code should work after reload")
	}
	if err := reloaded.Update(SettingsUpdate{HTTPPort: 9090, HTTPSPort: 9443, ForceTwoFactor: true}); err != nil {
		t.Fatal(err)
	}
	if err := reloaded.DisableTOTP(); err != nil {
		t.Fatal(err)
	}
	if reloaded.Settings().TOTPEnabled || reloaded.Settings().ForceTwoFactor {
		t.Fatal("disabling TOTP must also clear forced two-factor state")
	}
}

func TestBanManagerWindowExpiryAndManualUnban(t *testing.T) {
	now := time.Unix(100, 0)
	b := NewBanManager(BanRule{Threshold: 3, Window: 10 * time.Second, BanFor: 20 * time.Second})
	b.SetClock(func() time.Time { return now })
	for i := 0; i < 2; i++ {
		if b.RecordFailure("10.0.0.8", "login") {
			t.Fatal("banned too early")
		}
	}
	if !b.RecordFailure("10.0.0.8", "login") {
		t.Fatal("expected ban")
	}
	if _, ok := b.IsBanned("10.0.0.8"); !ok {
		t.Fatal("ban missing")
	}
	b.Unban("10.0.0.8")
	if _, ok := b.IsBanned("10.0.0.8"); ok {
		t.Fatal("manual unban failed")
	}
	for i := 0; i < 3; i++ {
		b.RecordFailure("10.0.0.9", "login")
	}
	now = now.Add(21 * time.Second)
	if _, ok := b.IsBanned("10.0.0.9"); ok {
		t.Fatal("ban should expire")
	}
}

func TestSelfSignedCertificateAndMismatch(t *testing.T) {
	m := NewCertificateManager(t.TempDir())
	m.Now = func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
	cert, err := m.SelfSigned("devbox", []string{"devbox.local", "10.0.0.2"}, 90)
	if err != nil {
		t.Fatal(err)
	}
	if !cert.SelfSigned || !cert.HasKey || cert.DaysLeft < 89 {
		t.Fatalf("unexpected cert: %#v", cert)
	}
	crt, err := os.ReadFile(filepath.Join(m.Dir, "devbox.crt"))
	if err != nil {
		t.Fatal(err)
	}
	other := NewCertificateManager(t.TempDir())
	_, err = other.SelfSigned("other", []string{"other.local"}, 30)
	if err != nil {
		t.Fatal(err)
	}
	key, err := os.ReadFile(filepath.Join(other.Dir, "other.key"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Upload("mismatch", crt, key); err == nil {
		t.Fatal("expected certificate/private key mismatch")
	}
}

func TestSettingsValidation(t *testing.T) {
	s, _ := NewStore("", "")
	base := SettingsUpdate{HTTPPort: 9090, HTTPSPort: 9443}
	if err := s.UpdatePreview(base); err != nil {
		t.Fatal(err)
	}
	bad := base
	bad.HTTPSPort = 9090
	if err := s.UpdatePreview(bad); err == nil {
		t.Fatal("expected duplicate port rejection")
	}
	bad = base
	bad.ForceTwoFactor = true
	if err := s.UpdatePreview(bad); err == nil {
		t.Fatal("expected force-2FA before enrollment rejection")
	}
	bad = base
	bad.ShareDomain = "javascript:alert(1)"
	if err := s.UpdatePreview(bad); err == nil {
		t.Fatal("expected invalid share URL rejection")
	}
	good := base
	good.ShareDomain = "https://share.example.com/files"
	if err := s.UpdatePreview(good); err != nil {
		t.Fatalf("valid share URL rejected: %v", err)
	}
}

func TestInitialHTTPPortPersistsWithoutOverwritingUserSetting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	key := filepath.Join(dir, "master.key")
	store, err := NewStore(path, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.InitializeHTTPPort(9133); err != nil {
		t.Fatal(err)
	}
	reloaded, err := NewStore(path, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := reloaded.InitializeHTTPPort(9999); err != nil {
		t.Fatal(err)
	}
	if got := reloaded.Settings().HTTPPort; got != 9133 {
		t.Fatalf("persisted HTTP port overwritten: %d", got)
	}
}
