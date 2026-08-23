package security

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"io"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
	if ok, err := reloaded.VerifySecondFactor(recovery[0], time.Now()); err != nil || !ok {
		t.Fatal("recovery code should work")
	}
	if ok, _ := reloaded.VerifySecondFactor(recovery[0], time.Now()); ok {
		t.Fatal("recovery code must be one-time")
	}
	code, err = totp.GenerateCode(enroll.Secret, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := reloaded.VerifySecondFactor(code, time.Now()); err != nil || !ok {
		t.Fatal("TOTP code should work after reload")
	}
	if err := reloaded.DisableTOTP(); err != nil {
		t.Fatal(err)
	}
	if reloaded.Settings().TOTPEnabled {
		t.Fatal("disabling TOTP must clear TOTP state")
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
	crtPath, _, err := m.Paths("devbox")
	if err != nil {
		t.Fatal(err)
	}
	crt, err := os.ReadFile(crtPath)
	if err != nil {
		t.Fatal(err)
	}
	other := NewCertificateManager(t.TempDir())
	_, err = other.SelfSigned("other", []string{"other.local"}, 30)
	if err != nil {
		t.Fatal(err)
	}
	_, keyPath, err := other.Paths("other")
	if err != nil {
		t.Fatal(err)
	}
	key, err := os.ReadFile(keyPath)
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

func TestTOTPCodeCannotBeReplayedInTheSameTimeStep(t *testing.T) {
	store, _ := NewStore("", "")
	enroll, err := store.BeginTOTP("DevBox", "admin")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_800_000_000, 0)
	code, err := totp.GenerateCode(enroll.Secret, now)
	if err != nil {
		t.Fatal(err)
	}
	confirmCode, _ := totp.GenerateCode(enroll.Secret, time.Now())
	if _, err := store.ConfirmTOTP(confirmCode); err != nil {
		t.Fatal(err)
	}
	if ok, err := store.VerifySecondFactor(code, now); err != nil || !ok {
		t.Fatalf("first TOTP use failed: ok=%v err=%v", ok, err)
	}
	if ok, err := store.VerifySecondFactor(code, now); err != nil || ok {
		t.Fatalf("replayed TOTP accepted: ok=%v err=%v", ok, err)
	}
}

func TestSettingsUpdateDoesNotDriftWhenPersistenceFails(t *testing.T) {
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")
	store, err := NewStore(filepath.Join(dataDir, "settings.json"), filepath.Join(dataDir, "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	before := SettingsUpdate{HTTPPort: 9090, HTTPSPort: 9443, ShareDomain: "https://before.example"}
	if err := store.Update(before); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(dataDir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dataDir, []byte("blocks directory creation"), 0600); err != nil {
		t.Fatal(err)
	}
	after := before
	after.ShareDomain = "https://after.example"
	if err := store.Update(after); err == nil {
		t.Fatal("expected persistence failure")
	}
	if got := store.Settings().ShareDomain; got != before.ShareDomain {
		t.Fatalf("in-memory settings drifted to %q", got)
	}
}

func TestStoreRejectsBareDDNSCredentialBeforePersistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	store, err := NewStore(path, filepath.Join(dir, "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	update := SettingsUpdate{HTTPPort: 9090, HTTPSPort: 9443, DDNSProvider: "cloudflare", DDNSDomain: "example.com", DDNSCredentialRef: "actual-secret-value"}
	if err := store.Update(update); err == nil {
		t.Fatal("store accepted a bare DDNS credential")
	}
	b, err := os.ReadFile(path)
	if err == nil && strings.Contains(string(b), "actual-secret-value") {
		t.Fatal("bare DDNS credential reached persistent storage")
	}
}

func TestRecoveryCodeIsNotConsumedWhenPersistenceFails(t *testing.T) {
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")
	store, err := NewStore(filepath.Join(dataDir, "settings.json"), filepath.Join(dir, "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	enroll, _ := store.BeginTOTP("DevBox", "admin")
	code, _ := totp.GenerateCode(enroll.Secret, time.Now())
	recovery, err := store.ConfirmTOTP(code)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(dataDir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dataDir, []byte("blocks directory creation"), 0600); err != nil {
		t.Fatal(err)
	}
	if ok, err := store.VerifySecondFactor(recovery[0], time.Now()); err == nil || ok {
		t.Fatalf("recovery login succeeded despite save failure: ok=%v err=%v", ok, err)
	}
	if err := os.Remove(dataDir); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		t.Fatal(err)
	}
	if ok, err := store.VerifySecondFactor(recovery[0], time.Now()); err != nil || !ok {
		t.Fatalf("recovery code was consumed by failed save: ok=%v err=%v", ok, err)
	}
}

func TestAccessAndRecoveryCodesUseRequiredStrengthAndBcrypt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	store, err := NewStore(path, filepath.Join(dir, "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	base := SettingsUpdate{HTTPPort: 9090, HTTPSPort: 9443, AccessCodeEnabled: true}
	base.AccessCode = "short7"
	if err := store.Update(base); err == nil {
		t.Fatal("accepted access code shorter than 8 characters")
	}
	base.AccessCode = "long-enough"
	if err := store.Update(base); err != nil {
		t.Fatal(err)
	}
	enroll, _ := store.BeginTOTP("DevBox", "admin")
	code, _ := totp.GenerateCode(enroll.Secret, time.Now())
	recovery, err := store.ConfirmTOTP(code)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(strings.ReplaceAll(recovery[0], "-", "")); got != 16 {
		t.Fatalf("recovery code carries %d base32 characters, want 16 (80 bits)", got)
	}
	var saved map[string]any
	b, _ := os.ReadFile(path)
	if err := json.Unmarshal(b, &saved); err != nil {
		t.Fatal(err)
	}
	if hash, _ := saved["accessCodeHash"].(string); !strings.HasPrefix(hash, "$2") {
		t.Fatalf("access code is not bcrypt hashed: %q", hash)
	}
	hashes, _ := saved["recoveryHashes"].([]any)
	if len(hashes) == 0 || !strings.HasPrefix(hashes[0].(string), "$2") {
		t.Fatalf("recovery codes are not bcrypt hashed: %#v", hashes)
	}
}

func TestBanManagerDoesNotBanProtectedAddresses(t *testing.T) {
	b := NewBanManager(BanRule{Threshold: 1, Window: time.Minute, BanFor: time.Minute})
	warnings := 0
	b.SetProtectedFailureHook(func(_, _ string) { warnings++ })
	if b.RecordFailure("10.126.126.42", "login") {
		t.Fatal("default tun0 subnet was banned")
	}
	b.SetProtectedIP("203.0.113.7")
	if b.RecordFailure("203.0.113.7", "login") {
		t.Fatal("current session IP was banned")
	}
	if err := b.SetProtectedNetworks([]string{"192.0.2.0/24"}); err != nil {
		t.Fatal(err)
	}
	if b.RecordFailure("192.0.2.9", "login") {
		t.Fatal("configured management subnet was banned")
	}
	if warnings != 3 {
		t.Fatalf("protected failure warnings=%d, want 3", warnings)
	}
	if got := b.Rule().ProtectedCIDRs; len(got) != 1 || got[0] != "192.0.2.0/24" {
		t.Fatalf("configured protected networks not reflected in rule: %#v", got)
	}
}

func TestCertificateValidationRejectsExpiredAndNotYetValid(t *testing.T) {
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	m := NewCertificateManager(t.TempDir())
	m.Now = func() time.Time { return now }
	for _, tc := range []struct {
		name      string
		notBefore time.Time
		notAfter  time.Time
	}{
		{name: "expired", notBefore: now.Add(-48 * time.Hour), notAfter: now.Add(-time.Hour)},
		{name: "future", notBefore: now.Add(time.Hour), notAfter: now.Add(48 * time.Hour)},
	} {
		certPEM, keyPEM := certificatePair(t, tc.notBefore, tc.notAfter, tc.name)
		if _, err := m.Validate(tc.name, certPEM, keyPEM); err == nil {
			t.Fatalf("accepted %s certificate", tc.name)
		}
	}
}

func TestConcurrentCertificateUploadsCommitWholePairs(t *testing.T) {
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	m := NewCertificateManager(t.TempDir())
	m.Now = func() time.Time { return now }
	aCert, aKey := certificatePair(t, now.Add(-time.Hour), now.Add(24*time.Hour), "a")
	bCert, bKey := certificatePair(t, now.Add(-time.Hour), now.Add(24*time.Hour), "b")
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, pair := range [][2][]byte{{aCert, aKey}, {bCert, bKey}} {
		wg.Add(1)
		go func(certPEM, keyPEM []byte) {
			defer wg.Done()
			_, err := m.Upload("shared", certPEM, keyPEM)
			errs <- err
		}(pair[0], pair[1])
	}
	wg.Wait()
	close(errs)
	successes := 0
	for err := range errs {
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("successful uploads=%d, want exactly one atomic commit", successes)
	}
	certPath, keyPath, err := m.Paths("shared")
	if err != nil {
		t.Fatal(err)
	}
	certPEM, _ := os.ReadFile(certPath)
	keyPEM, _ := os.ReadFile(keyPath)
	if _, err := m.Validate("shared", certPEM, keyPEM); err != nil {
		t.Fatalf("committed certificate pair is inconsistent: %v", err)
	}
}

func certificatePair(t *testing.T, notBefore, notAfter time.Time, commonName string) ([]byte, []byte) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{SerialNumber: big.NewInt(notAfter.UnixNano()), Subject: pkix.Name{CommonName: commonName}, Issuer: pkix.Name{CommonName: commonName}, NotBefore: notBefore, NotAfter: notAfter, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
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
