package downloads

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveTargetDirectory(t *testing.T) {
	root := t.TempDir()

	got, rel, err := ResolveTargetDirectory(root, "downloads/images")
	if err != nil {
		t.Fatalf("valid target: %v", err)
	}
	if rel != "downloads/images" || got != filepath.Join(root, "downloads", "images") {
		t.Fatalf("unexpected resolution: path=%q rel=%q", got, rel)
	}

	for _, target := range []string{"../outside", "downloads/../other", filepath.Join(filepath.Dir(root), "outside")} {
		_, _, err := ResolveTargetDirectory(root, target)
		if !errors.Is(err, ErrInvalidPath) && !errors.Is(err, ErrPathOutsideRoot) {
			t.Errorf("target %q should be rejected, got %v", target, err)
		}
	}
}

func TestResolveTargetDirectoryRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	_, _, err := ResolveTargetDirectory(root, "escape/files")
	if !errors.Is(err, ErrPathOutsideRoot) {
		t.Fatalf("expected root escape error, got %v", err)
	}
}

func TestValidateURL(t *testing.T) {
	for _, valid := range []string{"http://example.com/file", "https://example.com/a"} {
		if _, err := validateURL(valid); err != nil {
			t.Errorf("valid URL %q rejected: %v", valid, err)
		}
	}
	for _, invalid := range []string{"ftp://example.com/file", "file:///tmp/a", "../file", "https://user:pass@example.com/a"} {
		if _, err := validateURL(invalid); !errors.Is(err, ErrInvalidURL) {
			t.Errorf("invalid URL %q returned %v", invalid, err)
		}
	}
}
