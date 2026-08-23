package shares

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveWithinRoot(t *testing.T) {
	root := t.TempDir()
	inside := filepath.Join(root, "projects")
	if err := os.Mkdir(inside, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveWithinRoot(root, "projects")
	if err != nil {
		t.Fatal(err)
	}
	if got != inside {
		t.Fatalf("got %q, want %q", got, inside)
	}
}

func TestResolveWithinRootRejectsTraversalAndSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if _, err := ResolveWithinRoot(root, filepath.Join(root, "..", filepath.Base(outside))); err == nil {
		t.Fatal("expected traversal to be rejected")
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveWithinRoot(root, "escape"); err == nil {
		t.Fatal("expected symlink escape to be rejected")
	}
}
