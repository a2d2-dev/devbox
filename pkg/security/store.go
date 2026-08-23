package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

type Settings struct {
	HTTPPort             int    `json:"httpPort"`
	HTTPSPort            int    `json:"httpsPort"`
	ShareDomain          string `json:"shareDomain"`
	MaxUploadBytesSec    int64  `json:"maxUploadBytesSec"`
	MaxDownloadBytesSec  int64  `json:"maxDownloadBytesSec"`
	AccessCodeEnabled    bool   `json:"accessCodeEnabled"`
	ForceTwoFactor       bool   `json:"forceTwoFactor"`
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
	mu      sync.RWMutex
	path    string
	keyPath string
	key     []byte
	data    persisted
	loaded  bool
}

func NewStore(path, keyPath string) (*Store, error) {
	s := &Store{path: path, keyPath: keyPath}
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
	return s.data.Settings
}

type SettingsUpdate struct {
	HTTPPort            int    `json:"httpPort"`
	HTTPSPort           int    `json:"httpsPort"`
	ShareDomain         string `json:"shareDomain"`
	MaxUploadBytesSec   int64  `json:"maxUploadBytesSec"`
	MaxDownloadBytesSec int64  `json:"maxDownloadBytesSec"`
	AccessCodeEnabled   bool   `json:"accessCodeEnabled"`
	AccessCode          string `json:"accessCode,omitempty"`
	ForceTwoFactor      bool   `json:"forceTwoFactor"`
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
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.HTTPPort = update.HTTPPort
	s.data.HTTPSPort = update.HTTPSPort
	s.data.ShareDomain = strings.TrimSpace(update.ShareDomain)
	s.data.MaxUploadBytesSec = update.MaxUploadBytesSec
	s.data.MaxDownloadBytesSec = update.MaxDownloadBytesSec
	s.data.AccessCodeEnabled = update.AccessCodeEnabled
	s.data.ForceTwoFactor = update.ForceTwoFactor
	if update.HTTPSCertificate == "" {
		s.data.HTTPSCertificate = ""
	} else {
		s.data.HTTPSCertificate = filepath.Base(update.HTTPSCertificate)
	}
	s.data.DDNSProvider = update.DDNSProvider
	s.data.DDNSDomain = strings.TrimSpace(update.DDNSDomain)
	s.data.DDNSCredentialRef = strings.TrimSpace(update.DDNSCredentialRef)
	s.data.DDNSWebhookURL = strings.TrimSpace(update.DDNSWebhookURL)
	if update.AccessCode != "" {
		s.data.AccessCodeHash = hash(update.AccessCode)
	}
	s.syncPublicFlags()
	return s.saveLocked()
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
	s.mu.RLock()
	configured, totpEnabled := s.data.AccessCodeHash != "", s.data.TOTPEnabled
	s.mu.RUnlock()
	if update.AccessCodeEnabled && strings.TrimSpace(update.AccessCode) == "" && !configured {
		return errors.New("access code is required when enabling access-code protection")
	}
	if update.ForceTwoFactor && !totpEnabled {
		return errors.New("enroll TOTP before forcing two-factor authentication")
	}
	return nil
}

func (s *Store) VerifyAccessCode(code string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.data.AccessCodeEnabled {
		return true
	}
	return secureEqual(s.data.AccessCodeHash, hash(code))
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
	s.data.PendingTOTP = ciphertext
	if err := s.saveLocked(); err != nil {
		return Enrollment{}, err
	}
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
	s.data.TOTPSecret = s.data.PendingTOTP
	s.data.PendingTOTP = ""
	s.data.RecoveryHashes = hashes
	s.syncPublicFlags()
	if err := s.saveLocked(); err != nil {
		return nil, err
	}
	return codes, nil
}

func (s *Store) VerifySecondFactor(value string, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.data.TOTPEnabled {
		return !s.data.ForceTwoFactor
	}
	secret, err := s.decrypt(s.data.TOTPSecret)
	valid := false
	if err == nil {
		valid, _ = totp.ValidateCustom(strings.TrimSpace(value), secret, now, totp.ValidateOpts{Period: 30, Skew: 1, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1})
	}
	if valid {
		return true
	}
	wanted := hash(strings.ToUpper(strings.TrimSpace(value)))
	for i, candidate := range s.data.RecoveryHashes {
		if secureEqual(candidate, wanted) {
			s.data.RecoveryHashes = append(s.data.RecoveryHashes[:i], s.data.RecoveryHashes[i+1:]...)
			_ = s.saveLocked()
			return true
		}
	}
	return false
}

func (s *Store) DisableTOTP() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.TOTPSecret, s.data.PendingTOTP, s.data.RecoveryHashes = "", "", nil
	s.syncPublicFlags()
	return s.saveLocked()
}

func (s *Store) syncPublicFlags() {
	s.data.AccessCodeConfigured = s.data.AccessCodeHash != ""
	s.data.TOTPEnabled = s.data.TOTPSecret != ""
}

func (s *Store) saveLocked() error {
	if s.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
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
		b := make([]byte, 5)
		if _, err := rand.Read(b); err != nil {
			return nil, nil, err
		}
		raw := strings.ToUpper(hex.EncodeToString(b))
		codes[i] = raw[:5] + "-" + raw[5:]
		hashes[i] = hash(codes[i])
	}
	return codes, hashes, nil
}
