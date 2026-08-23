package shares

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ResolveWithinRoot resolves an existing share directory and enforces chroot-like
// semantics, including for symlinks inside the data root.
func ResolveWithinRoot(root, requested string) (string, error) {
	if strings.TrimSpace(root) == "" {
		root = "/data"
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve data root: %w", err)
	}
	rootReal, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return "", fmt.Errorf("data root is unavailable: %w", err)
	}

	requested = strings.TrimSpace(requested)
	if requested == "" {
		requested = rootReal
	} else if !filepath.IsAbs(requested) {
		requested = filepath.Join(rootReal, requested)
	}
	candidate, err := filepath.Abs(requested)
	if err != nil {
		return "", fmt.Errorf("resolve share path: %w", err)
	}
	if err := ensureDescendant(rootReal, candidate); err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", fmt.Errorf("share path must exist: %w", err)
	}
	if err := ensureDescendant(rootReal, real); err != nil {
		return "", err
	}
	info, err := os.Stat(real)
	if err != nil {
		return "", fmt.Errorf("inspect share path: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("share path must be a directory")
	}
	return real, nil
}

func ensureDescendant(root, candidate string) error {
	rel, err := filepath.Rel(root, candidate)
	if err != nil {
		return fmt.Errorf("compare share path with data root: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("share path must stay within data root %q", root)
	}
	return nil
}
