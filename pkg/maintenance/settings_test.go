package maintenance

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/a2d2-dev/devbox/pkg/shares"
)

func TestDefaultSMTPValuesMatchDisplayedFormValues(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(t.TempDir(), root)
	if err != nil {
		t.Fatal(err)
	}
	got := store.Get().SMTP
	if got.Port != 587 || got.TLS != "starttls" {
		t.Fatalf("default SMTP config = %+v, want port 587 with STARTTLS", got)
	}
}

func TestSMTPPasswordEncryptedAndRedactedExport(t *testing.T) {
	dir := t.TempDir()
	root := t.TempDir()
	store, err := NewStore(dir, root)
	if err != nil {
		t.Fatal(err)
	}
	state := store.Get()
	state.SMTP = SMTPConfig{Host: "smtp.example.com", Port: 587, TLS: "starttls", Username: "devbox", Password: "plain-secret", From: "devbox@example.com", To: "ops@example.com"}
	if err := store.Save(state); err != nil {
		t.Fatal(err)
	}

	disk, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(disk, []byte("plain-secret")) || !bytes.Contains(disk, []byte("smtpPasswordCipher")) {
		t.Fatalf("password was not encrypted on disk: %s", disk)
	}
	info, err := os.Stat(filepath.Join(dir, "secret.key"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("key mode = %o, want 600", info.Mode().Perm())
	}

	reloaded, err := NewStore(dir, root)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Get().SMTP.Password != "plain-secret" {
		t.Fatal("encrypted password did not round-trip")
	}
	redacted, err := reloaded.Export(false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(readArchiveConfig(t, redacted), "plain-secret") {
		t.Fatal("redacted backup contains SMTP password")
	}
	withSecrets, err := reloaded.Export(true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(readArchiveConfig(t, withSecrets), "plain-secret") {
		t.Fatal("explicit secrets backup did not contain SMTP password")
	}
}

func TestBackupRestoreRoundTripAndAutomaticBackup(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(t.TempDir(), root)
	if err != nil {
		t.Fatal(err)
	}
	wanted := store.Get()
	wanted.WebDAV.Enabled = true
	wanted.WebDAV.Path = root
	wanted.SMB = []shares.SMBShare{{Name: "work", Path: root, ReadOnly: true}}
	wanted.DefaultApps["text/markdown"] = "vscode"
	wanted.SMTP.Password = "roundtrip-secret"
	if err := store.Save(wanted); err != nil {
		t.Fatal(err)
	}
	backup, err := store.Export(true)
	if err != nil {
		t.Fatal(err)
	}

	changed := store.Get()
	changed.WebDAV.Enabled = false
	changed.SMB = nil
	changed.DefaultApps = map[string]string{}
	changed.SMTP.Password = "other"
	if err := store.Save(changed); err != nil {
		t.Fatal(err)
	}
	preview, err := store.PreviewRestore(backup)
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Changes) < 3 {
		t.Fatalf("expected meaningful diff, got %v", preview.Changes)
	}
	autoBackup, err := store.Restore(preview.Candidate)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(autoBackup); err != nil {
		t.Fatalf("automatic pre-restore backup missing: %v", err)
	}
	got := store.Get()
	if !got.WebDAV.Enabled || len(got.SMB) != 1 || got.DefaultApps["text/markdown"] != "vscode" || got.SMTP.Password != "roundtrip-secret" {
		t.Fatalf("restored settings mismatch: %+v", got)
	}
}

func readArchiveConfig(t *testing.T, data []byte) string {
	t.Helper()
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if hdr.Name == "config.json" {
			body, err := io.ReadAll(tr)
			if err != nil {
				t.Fatal(err)
			}
			return string(body)
		}
	}
	t.Fatal("config.json not found")
	return ""
}
