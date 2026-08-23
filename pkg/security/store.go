package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
	"golang.org/x/crypto/bcrypt"
)

const RedactedCredentialRef = "env:REDACTED"

type Settings struct {
	HTTPPort             int    `json:"httpPort"`
	HTTPSPort            int    `json:"httpsPort"`
	ShareDomain          string `json:"shareDomain"`
	MaxUploadBytesSec    int64  `json:"maxUploadBytesSec"`
	MaxDownloadBytesSec  int64  `json:"maxDownloadBytesSec"`
	AccessCodeEnabled    bool   `json:"accessCodeEnabled"`
	HTTPSCertificate     string `json:"httpsCertificate"`
	AccessCodeConfigured bool   `json:"accessCodeConfigured"`
	TOTPEnabled          bool   `json:"totpEnabled"`
	DDNSProvider         string `json:"ddnsProvider"`
	DDNSDomain           string `json:"ddnsDomain"`
	DDNSCredentialRef    string `json:"ddnsCredentialRef"`
	DDNSWebhookURL       string `json:"ddnsWebhookURL"`
}

type persisted struct {
	Settings
	AccessCodeHash string   `json:"accessCodeHash,omitempty"`
	TOTPSecret     string   `json:"totpSecret,omitempty"`
	PendingTOTP    string   `json:"pendingTOTP,omitempty"`
	RecoveryHashes []string `json:"recoveryHashes,omitempty"`
}

type Store struct {
	mu           sync.RWMutex
	path         string
	keyPath      string
	key          []byte
	data         persisted
	loaded       bool
	consumedTOTP map[totpUse]struct{}
}

type totpUse struct {
	step int64
	code string
}

func NewStore(path, keyPath string) (*Store, error) {
	s := &Store{path: path, keyPath: keyPath, consumedTOTP: make(map[totpUse]struct{})}
	if err := s.loadKey(); err != nil {
		return nil, err
	}
	s.data.HTTPPort = 9090
	s.data.HTTPSPort = 9443
	if path != "" {
		if b, err := os.ReadFile(path); err == nil {
			if err := json.Unmarshal(b, &s.data); err != nil {
				return nil, fmt.Errorf("parse security settings: %w", err)
			}
			s.loaded = true
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	s.syncPublicFlags()
	return s, nil
}

func (s *Store) InitializeHTTPPort(port int) error {
	if port < 1 || port > 65535 {
		return errors.New("HTTP port must be between 1 and 65535")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loaded {
		return nil
	}
	s.data.HTTPPort = port
	return s.saveLocked()
}

func (s *Store) loadKey() error {
	if s.keyPath != "" {
		if key, err := os.ReadFile(s.keyPath); err == nil {
			if len(key) != 32 {
				return errors.New("security key must be 32 bytes")
			}
			s.key = key
			return nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	s.key = make([]byte, 32)
	if _, err := rand.Read(s.key); err != nil {
		return err
	}
	if s.keyPath == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.keyPath), 0700); err != nil {
		return err
	}
	return os.WriteFile(s.keyPath, s.key, 0600)
}

func (s *Store) Settings() Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	settings := s.data.Settings
	if settings.DDNSCredentialRef != "" {
		settings.DDNSCredentialRef = RedactedCredentialRef
	}
	return settings
}

func (s *Store) ProtectionEnabled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data.AccessCodeEnabled || s.data.TOTPEnabled
}

func (s *Store) DDNSCredentialRef() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data.DDNSCredentialRef
}

type SettingsUpdate struct {
	HTTPPort            int    `json:"httpPort"`
	HTTPSPort           int    `json:"httpsPort"`
	ShareDomain         string `json:"shareDomain"`
	MaxUploadBytesSec   int64  `json:"maxUploadBytesSec"`
	MaxDownloadBytesSec int64  `json:"maxDownloadBytesSec"`
	AccessCodeEnabled   bool   `json:"accessCodeEnabled"`
	AccessCode          string `json:"accessCode,omitempty"`
	HTTPSCertificate    string `json:"httpsCertificate"`
	DDNSProvider        string `json:"ddnsProvider"`
	DDNSDomain          string `json:"ddnsDomain"`
	DDNSCredentialRef   string `json:"ddnsCredentialRef"`
	DDNSWebhookURL      string `json:"ddnsWebhookURL"`
}

func (s *Store) Update(update SettingsUpdate) error {
	if err := s.UpdatePreview(update); err != nil {
		return err
	}
	var accessHash string
	if update.AccessCode != "" {
		var err error
		accessHash, err = bcryptHash(strings.TrimSpace(update.AccessCode))
		if err != nil {
			return err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	next := clonePersisted(s.data)
	next.HTTPPort = update.HTTPPort
	next.HTTPSPort = update.HTTPSPort
	next.ShareDomain = strings.TrimSpace(update.ShareDomain)
	next.MaxUploadBytesSec = update.MaxUploadBytesSec
	next.MaxDownloadBytesSec = update.MaxDownloadBytesSec
	next.AccessCodeEnabled = update.AccessCodeEnabled
	if update.HTTPSCertificate == "" {
		next.HTTPSCertificate = ""
	} else {
		next.HTTPSCertificate = filepath.Base(update.HTTPSCertificate)
	}
	next.DDNSProvider = update.DDNSProvider
	next.DDNSDomain = strings.TrimSpace(update.DDNSDomain)
	if strings.TrimSpace(update.DDNSCredentialRef) != RedactedCredentialRef {
		next.DDNSCredentialRef = strings.TrimSpace(update.DDNSCredentialRef)
	}
	next.DDNSWebhookURL = strings.TrimSpace(update.DDNSWebhookURL)
	if update.AccessCode != "" {
		next.AccessCodeHash = accessHash
	}
	syncPublicFlags(&next)
	if err := s.saveDataLocked(next); err != nil {
		return err
	}
	s.data = next
	s.loaded = true
	return nil
}

func (s *Store) UpdatePreview(update SettingsUpdate) error {
	if update.HTTPPort < 1 || update.HTTPPort > 65535 || update.HTTPSPort < 1 || update.HTTPSPort > 65535 {
		return errors.New("HTTP and HTTPS ports must be between 1 and 65535")
	}
	if update.HTTPPort == update.HTTPSPort {
		return errors.New("HTTP and HTTPS ports must differ")
	}
	if update.MaxUploadBytesSec < 0 || update.MaxDownloadBytesSec < 0 {
		return errors.New("speed limits cannot be negative")
	}
	if update.ShareDomain != "" {
		u, err := url.ParseRequestURI(strings.TrimSpace(update.ShareDomain))
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
			return errors.New("share domain must be an HTTP or HTTPS URL without credentials")
		}
	}
	if update.DDNSCredentialRef != "" && update.DDNSCredentialRef != RedactedCredentialRef {
		if err := validateCredentialReference(update.DDNSCredentialRef); err != nil {
			return err
		}
	}
	s.mu.RLock()
	configured := s.data.AccessCodeHash != ""
	s.mu.RUnlock()
	if update.AccessCodeEnabled && strings.TrimSpace(update.AccessCode) == "" && !configured {
		return errors.New("access code is required when enabling access-code protection")
	}
	if update.AccessCode != "" && len([]rune(strings.TrimSpace(update.AccessCode))) < 8 {
		return errors.New("access code must be at least 8 characters")
	}
	return nil
}

func (s *Store) VerifyAccessCode(code string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.data.AccessCodeEnabled {
		return true
	}
	return verifyHash(s.data.AccessCodeHash, strings.TrimSpace(code))
}

type Enrollment struct {
	Secret    string `json:"secret"`
	URI       string `json:"uri"`
	QRDataURL string `json:"qrDataURL"`
}

func (s *Store) BeginTOTP(issuer, account string) (Enrollment, error) {
	key, err := totp.Generate(totp.GenerateOpts{Issuer: issuer, AccountName: account})
	if err != nil {
		return Enrollment{}, err
	}
	image, err := key.Image(240, 240)
	if err != nil {
		return Enrollment{}, err
	}
	// Key.Image returns an image; encode through the small helper to keep the API JSON-only.
	encoded, err := encodePNG(image)
	if err != nil {
		return Enrollment{}, err
	}
	ciphertext, err := s.encrypt(key.Secret())
	if err != nil {
		return Enrollment{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	next := clonePersisted(s.data)
	next.PendingTOTP = ciphertext
	if err := s.saveDataLocked(next); err != nil {
		return Enrollment{}, err
	}
	s.data = next
	return Enrollment{Secret: key.Secret(), URI: key.URL(), QRDataURL: "data:image/png;base64," + encoded}, nil
}

func (s *Store) ConfirmTOTP(code string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	secret, err := s.decrypt(s.data.PendingTOTP)
	if err != nil || secret == "" {
		return nil, errors.New("no pending TOTP enrollment")
	}
	if !totp.Validate(strings.TrimSpace(code), secret) {
		return nil, errors.New("invalid TOTP code")
	}
	codes, hashes, err := makeRecoveryCodes(10)
	if err != nil {
		return nil, err
	}
	next := clonePersisted(s.data)
	next.TOTPSecret = next.PendingTOTP
	next.PendingTOTP = ""
	next.RecoveryHashes = hashes
	syncPublicFlags(&next)
	if err := s.saveDataLocked(next); err != nil {
		return nil, err
	}
	s.data = next
	return codes, nil
}

func (s *Store) VerifySecondFactor(value string, now time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.data.TOTPEnabled {
		return true, nil
	}
	secret, err := s.decrypt(s.data.TOTPSecret)
	if err != nil {
		return false, err
	}
	code := strings.TrimSpace(value)
	if step, valid := matchingTOTPStep(code, secret, now); valid {
		use := totpUse{step: step, code: code}
		if _, consumed := s.consumedTOTP[use]; consumed {
			return false, nil
		}
		for old := range s.consumedTOTP {
			if old.step < now.Unix()/30-1 {
				delete(s.consumedTOTP, old)
			}
		}
		s.consumedTOTP[use] = struct{}{}
		return true, nil
	}
	wanted := strings.ToUpper(code)
	for i, candidate := range s.data.RecoveryHashes {
		if verifyHash(candidate, wanted) {
			next := clonePersisted(s.data)
			next.RecoveryHashes = append(next.RecoveryHashes[:i], next.RecoveryHashes[i+1:]...)
			if err := s.saveDataLocked(next); err != nil {
				return false, err
			}
			s.data = next
			return true, nil
		}
	}
	return false, nil
}

func (s *Store) DisableTOTP() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := clonePersisted(s.data)
	next.TOTPSecret, next.PendingTOTP, next.RecoveryHashes = "", "", nil
	syncPublicFlags(&next)
	if err := s.saveDataLocked(next); err != nil {
		return err
	}
	s.data = next
	s.consumedTOTP = make(map[totpUse]struct{})
	return nil
}

func (s *Store) syncPublicFlags() {
	syncPublicFlags(&s.data)
}

func syncPublicFlags(data *persisted) {
	data.AccessCodeConfigured = data.AccessCodeHash != ""
	data.TOTPEnabled = data.TOTPSecret != ""
}

func (s *Store) saveLocked() error {
	return s.saveDataLocked(s.data)
}

func (s *Store) saveDataLocked(data persisted) error {
	if s.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func clonePersisted(data persisted) persisted {
	data.RecoveryHashes = append([]string(nil), data.RecoveryHashes...)
	return data
}

func (s *Store) encrypt(value string) (string, error) {
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(value), nil)
	return base64.RawStdEncoding.EncodeToString(sealed), nil
}

func (s *Store) decrypt(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	b, err := base64.RawStdEncoding.DecodeString(value)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(b) < gcm.NonceSize() {
		return "", errors.New("invalid encrypted value")
	}
	plain, err := gcm.Open(nil, b[:gcm.NonceSize()], b[gcm.NonceSize():], nil)
	return string(plain), err
}

func hash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
func secureEqual(a, b string) bool {
	return len(a) == len(b) && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func makeRecoveryCodes(count int) ([]string, []string, error) {
	codes := make([]string, count)
	hashes := make([]string, count)
	for i := range codes {
		b := make([]byte, 10)
		if _, err := rand.Read(b); err != nil {
			return nil, nil, err
		}
		raw := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b)
		codes[i] = raw[:4] + "-" + raw[4:8] + "-" + raw[8:12] + "-" + raw[12:]
		var err error
		hashes[i], err = bcryptHash(codes[i])
		if err != nil {
			return nil, nil, err
		}
	}
	return codes, hashes, nil
}

func bcryptHash(value string) (string, error) {
	encoded, err := bcrypt.GenerateFromPassword([]byte(value), bcrypt.MinCost)
	return string(encoded), err
}

func verifyHash(encoded, value string) bool {
	if strings.HasPrefix(encoded, "$2") {
		return bcrypt.CompareHashAndPassword([]byte(encoded), []byte(value)) == nil
	}
	return secureEqual(encoded, hash(value))
}

func matchingTOTPStep(code, secret string, now time.Time) (int64, bool) {
	for _, offset := range []int{-1, 0, 1} {
		candidateTime := now.Add(time.Duration(offset) * 30 * time.Second)
		candidate, err := totp.GenerateCodeCustom(secret, candidateTime, totp.ValidateOpts{Period: 30, Skew: 0, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1})
		if err == nil && secureEqual(candidate, code) {
			return candidateTime.Unix() / 30, true
		}
	}
	return 0, false
}

func validateCredentialReference(value string) error {
	value = strings.TrimSpace(value)
	scheme, target, ok := strings.Cut(value, ":")
	if !ok || target == "" {
		return errors.New("DDNS credential must use env:NAME or file:path")
	}
	if scheme == "env" {
		for i, r := range target {
			if !(r == '_' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || i > 0 && r >= '0' && r <= '9') {
				return errors.New("DDNS environment reference is invalid")
			}
		}
		return nil
	}
	if scheme == "file" && strings.TrimSpace(target) == target && !strings.ContainsAny(target, "\r\n\x00") {
		return nil
	}
	return errors.New("DDNS credential must use env:NAME or file:path")
}
