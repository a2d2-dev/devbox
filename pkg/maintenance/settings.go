package maintenance

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/a2d2-dev/devbox/pkg/shares"
)

const currentSchema = 1

type SMTPConfig struct {
	Enabled  bool   `json:"enabled"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	TLS      string `json:"tls"`
	Username string `json:"username"`
	Password string `json:"-"`
	From     string `json:"from"`
	To       string `json:"to"`
}

type UpdateConfig struct {
	CheckEnabled bool   `json:"checkEnabled"`
	AutoUpdate   bool   `json:"autoUpdate"`
	Repository   string `json:"repository"`
}

type Settings struct {
	SchemaVersion int                 `json:"schemaVersion"`
	WebDAV        shares.WebDAVConfig `json:"webdav"`
	SMB           []shares.SMBShare   `json:"smb"`
	SMTP          SMTPConfig          `json:"smtp"`
	Updates       UpdateConfig        `json:"updates"`
	DefaultApps   map[string]string   `json:"defaultApps"`
}

type PublicSettings struct {
	Settings
	SMTPPasswordSet bool `json:"smtpPasswordSet"`
}

type diskSettings struct {
	SchemaVersion      int                 `json:"schemaVersion"`
	WebDAV             shares.WebDAVConfig `json:"webdav"`
	SMB                []shares.SMBShare   `json:"smb"`
	SMTP               SMTPConfig          `json:"smtp"`
	SMTPPasswordCipher string              `json:"smtpPasswordCipher,omitempty"`
	Updates            UpdateConfig        `json:"updates"`
	DefaultApps        map[string]string   `json:"defaultApps"`
}

type Store struct {
	mu       sync.RWMutex
	dir      string
	dataRoot string
	state    Settings
}

func NewStore(dir, dataRoot string) (*Store, error) {
	if strings.TrimSpace(dir) == "" {
		dir = os.Getenv("DEVBOX_MAINTENANCE_DIR")
	}
	if strings.TrimSpace(dir) == "" {
		dir = "/var/lib/devbox/maintenance"
	}
	if strings.TrimSpace(dataRoot) == "" {
		dataRoot = "/data"
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create maintenance data directory: %w", err)
	}
	s := &Store{dir: dir, dataRoot: dataRoot, state: defaultSettings(dataRoot)}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func defaultSettings(dataRoot string) Settings {
	return Settings{
		SchemaVersion: currentSchema,
		WebDAV:        shares.WebDAVConfig{Port: 19000, Path: dataRoot},
		Updates:       UpdateConfig{CheckEnabled: true, Repository: "a2d2-dev/devbox"},
		DefaultApps:   map[string]string{"text/plain": "browser", "application/json": "browser"},
	}
}

func (s *Store) Get() Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneSettings(s.state)
}

func (s *Store) Public() PublicSettings {
	state := s.Get()
	set := state.SMTP.Password != ""
	state.SMTP.Password = ""
	return PublicSettings{Settings: state, SMTPPasswordSet: set}
}

func (s *Store) Save(next Settings) error {
	next = normalizeSettings(next, s.dataRoot)
	if err := ValidateSettings(next, s.dataRoot); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked(next)
}

func ValidateSettings(state Settings, dataRoot string) error {
	if _, err := shares.ResolveWithinRoot(dataRoot, state.WebDAV.Path); err != nil {
		return fmt.Errorf("WebDAV path: %w", err)
	}
	if _, err := shares.RenderSMB(dataRoot, state.SMB); err != nil {
		return err
	}
	if state.SMTP.Enabled {
		if err := ValidateSMTP(state.SMTP); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) load() error {
	data, err := os.ReadFile(s.statePath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read maintenance settings: %w", err)
	}
	var disk diskSettings
	if err := json.Unmarshal(data, &disk); err != nil {
		return fmt.Errorf("decode maintenance settings: %w", err)
	}
	state := Settings{
		SchemaVersion: disk.SchemaVersion, WebDAV: disk.WebDAV, SMB: disk.SMB,
		SMTP: disk.SMTP, Updates: disk.Updates, DefaultApps: disk.DefaultApps,
	}
	if disk.SMTPPasswordCipher != "" {
		password, err := s.decrypt(disk.SMTPPasswordCipher)
		if err != nil {
			return fmt.Errorf("decrypt SMTP password: %w", err)
		}
		state.SMTP.Password = password
	}
	s.state = normalizeSettings(state, s.dataRoot)
	return nil
}

func (s *Store) saveLocked(next Settings) error {
	disk := diskSettings{
		SchemaVersion: next.SchemaVersion, WebDAV: next.WebDAV, SMB: next.SMB,
		SMTP: next.SMTP, Updates: next.Updates, DefaultApps: next.DefaultApps,
	}
	if next.SMTP.Password != "" {
		ciphertext, err := s.encrypt(next.SMTP.Password)
		if err != nil {
			return fmt.Errorf("encrypt SMTP password: %w", err)
		}
		disk.SMTPPasswordCipher = ciphertext
	}
	data, err := json.MarshalIndent(disk, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.dir, ".settings-*.json")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, s.statePath()); err != nil {
		return err
	}
	s.state = cloneSettings(next)
	return nil
}

func normalizeSettings(state Settings, dataRoot string) Settings {
	state.SchemaVersion = currentSchema
	if state.WebDAV.Port == 0 {
		state.WebDAV.Port = 19000
	}
	if state.WebDAV.Path == "" {
		state.WebDAV.Path = dataRoot
	}
	if state.Updates.Repository == "" {
		state.Updates.Repository = "a2d2-dev/devbox"
	}
	if state.DefaultApps == nil {
		state.DefaultApps = map[string]string{}
	}
	return state
}

func cloneSettings(in Settings) Settings {
	out := in
	out.SMB = append([]shares.SMBShare(nil), in.SMB...)
	out.DefaultApps = make(map[string]string, len(in.DefaultApps))
	for k, v := range in.DefaultApps {
		out.DefaultApps[k] = v
	}
	return out
}

func (s *Store) encrypt(plain string) (string, error) {
	key, err := s.loadOrCreateKey()
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
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
	sealed := gcm.Seal(nonce, nonce, []byte(plain), nil)
	return base64.RawStdEncoding.EncodeToString(sealed), nil
}

func (s *Store) decrypt(encoded string) (string, error) {
	key, err := os.ReadFile(s.keyPath())
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	sealed, err := base64.RawStdEncoding.DecodeString(encoded)
	if err != nil || len(sealed) < gcm.NonceSize() {
		return "", errors.New("invalid encrypted password")
	}
	plain, err := gcm.Open(nil, sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():], nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func (s *Store) loadOrCreateKey() ([]byte, error) {
	key, err := os.ReadFile(s.keyPath())
	if err == nil {
		if len(key) != 32 {
			return nil, errors.New("invalid maintenance encryption key length")
		}
		return key, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	key = make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	if err := os.WriteFile(s.keyPath(), key, 0o600); err != nil {
		return nil, err
	}
	return key, nil
}

func (s *Store) statePath() string { return filepath.Join(s.dir, "settings.json") }
func (s *Store) keyPath() string   { return filepath.Join(s.dir, "secret.key") }

type exportSMTP struct {
	Enabled  bool   `json:"enabled"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	TLS      string `json:"tls"`
	Username string `json:"username"`
	Password string `json:"password,omitempty"`
	From     string `json:"from"`
	To       string `json:"to"`
}

type exportSettings struct {
	SchemaVersion int                 `json:"schemaVersion"`
	WebDAV        shares.WebDAVConfig `json:"webdav"`
	SMB           []shares.SMBShare   `json:"smb"`
	SMTP          exportSMTP          `json:"smtp"`
	Updates       UpdateConfig        `json:"updates"`
	DefaultApps   map[string]string   `json:"defaultApps"`
}

func (s *Store) Export(includeSecrets bool) ([]byte, error) {
	state := s.Get()
	exp := toExport(state, includeSecrets)
	configJSON, err := json.MarshalIndent(exp, "", "  ")
	if err != nil {
		return nil, err
	}
	manifest, _ := json.MarshalIndent(map[string]any{
		"format": "devbox-config", "schemaVersion": currentSchema,
		"createdAt": time.Now().UTC().Format(time.RFC3339), "includesSecrets": includeSecrets,
	}, "", "  ")
	var result bytes.Buffer
	gz := gzip.NewWriter(&result)
	tw := tar.NewWriter(gz)
	for _, file := range []struct {
		name string
		data []byte
		mode int64
	}{{"manifest.json", manifest, 0o644}, {"config.json", configJSON, 0o600}} {
		if err := tw.WriteHeader(&tar.Header{Name: file.name, Mode: file.mode, Size: int64(len(file.data)), ModTime: time.Now()}); err != nil {
			return nil, err
		}
		if _, err := tw.Write(file.data); err != nil {
			return nil, err
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	return result.Bytes(), nil
}

func toExport(state Settings, includeSecrets bool) exportSettings {
	password := ""
	if includeSecrets {
		password = state.SMTP.Password
	}
	return exportSettings{
		SchemaVersion: state.SchemaVersion, WebDAV: state.WebDAV, SMB: state.SMB,
		SMTP: exportSMTP{Enabled: state.SMTP.Enabled, Host: state.SMTP.Host, Port: state.SMTP.Port,
			TLS: state.SMTP.TLS, Username: state.SMTP.Username, Password: password, From: state.SMTP.From, To: state.SMTP.To},
		Updates: state.Updates, DefaultApps: state.DefaultApps,
	}
}

type RestorePreview struct {
	Candidate Settings `json:"-"`
	Changes   []string `json:"changes"`
}

func (s *Store) PreviewRestore(archive []byte) (RestorePreview, error) {
	if len(archive) > 8<<20 {
		return RestorePreview{}, errors.New("backup archive exceeds 8 MiB")
	}
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return RestorePreview{}, errors.New("uploaded file is not a valid tar.gz backup")
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	var configData []byte
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return RestorePreview{}, fmt.Errorf("read backup archive: %w", err)
		}
		if hdr.Name != filepath.Base(hdr.Name) {
			return RestorePreview{}, errors.New("backup contains an unsafe path")
		}
		if hdr.Name == "config.json" {
			if hdr.Size > 4<<20 {
				return RestorePreview{}, errors.New("backup config is too large")
			}
			configData, err = io.ReadAll(io.LimitReader(tr, 4<<20))
			if err != nil {
				return RestorePreview{}, err
			}
		}
	}
	if len(configData) == 0 {
		return RestorePreview{}, errors.New("backup does not contain config.json")
	}
	var exp exportSettings
	if err := json.Unmarshal(configData, &exp); err != nil {
		return RestorePreview{}, fmt.Errorf("decode backup config: %w", err)
	}
	if exp.SchemaVersion != currentSchema {
		return RestorePreview{}, fmt.Errorf("unsupported backup schema version %d", exp.SchemaVersion)
	}
	candidate := Settings{
		SchemaVersion: exp.SchemaVersion, WebDAV: exp.WebDAV, SMB: exp.SMB,
		SMTP: SMTPConfig{Enabled: exp.SMTP.Enabled, Host: exp.SMTP.Host, Port: exp.SMTP.Port, TLS: exp.SMTP.TLS,
			Username: exp.SMTP.Username, Password: exp.SMTP.Password, From: exp.SMTP.From, To: exp.SMTP.To},
		Updates: exp.Updates, DefaultApps: exp.DefaultApps,
	}
	// A redacted backup preserves the currently stored secret during restore.
	if candidate.SMTP.Password == "" {
		candidate.SMTP.Password = s.Get().SMTP.Password
	}
	candidate = normalizeSettings(candidate, s.dataRoot)
	if err := ValidateSettings(candidate, s.dataRoot); err != nil {
		return RestorePreview{}, fmt.Errorf("backup settings are invalid: %w", err)
	}
	return RestorePreview{Candidate: candidate, Changes: diffSettings(s.Get(), candidate)}, nil
}

func diffSettings(current, candidate Settings) []string {
	var changes []string
	checks := []struct {
		name string
		a    any
		b    any
	}{
		{"WebDAV 配置", current.WebDAV, candidate.WebDAV},
		{"SMB 共享", current.SMB, candidate.SMB},
		{"SMTP 通知配置", publicSMTP(current.SMTP), publicSMTP(candidate.SMTP)},
		{"更新配置", current.Updates, candidate.Updates},
		{"默认应用", current.DefaultApps, candidate.DefaultApps},
	}
	for _, check := range checks {
		if !reflect.DeepEqual(check.a, check.b) {
			changes = append(changes, check.name+"将被替换")
		}
	}
	if len(changes) == 0 {
		changes = append(changes, "配置无变化")
	}
	return changes
}

func publicSMTP(in SMTPConfig) SMTPConfig { in.Password = ""; return in }

func (s *Store) Restore(candidate Settings) (string, error) {
	backup, err := s.Export(true)
	if err != nil {
		return "", fmt.Errorf("create automatic pre-restore backup: %w", err)
	}
	backupDir := filepath.Join(s.dir, "backups")
	if err := os.MkdirAll(backupDir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(backupDir, "pre-restore-"+time.Now().UTC().Format("20060102T150405.000000000Z")+".tar.gz")
	if err := os.WriteFile(path, backup, 0o600); err != nil {
		return "", err
	}
	if err := s.Save(candidate); err != nil {
		return "", err
	}
	return path, nil
}

func (s *Store) Reset() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, name := range []string{s.statePath(), s.keyPath(), filepath.Join(s.dir, "backups")} {
		if err := os.RemoveAll(name); err != nil {
			return err
		}
	}
	s.state = defaultSettings(s.dataRoot)
	return nil
}
