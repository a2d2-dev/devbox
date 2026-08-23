package downloads

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func normalizeRoot(root string) (string, error) {
	if root == "" {
		root = "/data"
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve download root: %w", err)
	}
	if err := os.MkdirAll(abs, 0o750); err != nil {
		return "", fmt.Errorf("create download root: %w", err)
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("resolve download root symlinks: %w", err)
	}
	return filepath.Clean(real), nil
}

// ResolveTargetDirectory applies chroot-style semantics and also verifies the
// created directory after symlink evaluation. Raw ".." components are rejected
// even when filepath.Clean would place the result back under the root.
func ResolveTargetDirectory(root, target string) (string, string, error) {
	if target == "" {
		target = "downloads"
	}
	for _, part := range strings.FieldsFunc(filepath.ToSlash(target), func(r rune) bool { return r == '/' }) {
		if part == ".." {
			return "", "", ErrInvalidPath
		}
	}

	root = filepath.Clean(root)
	candidate := target
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(root, candidate)
	}
	candidate = filepath.Clean(candidate)
	if !pathWithin(root, candidate) {
		return "", "", ErrPathOutsideRoot
	}
	if err := verifyExistingParent(root, candidate); err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(candidate, 0o750); err != nil {
		return "", "", fmt.Errorf("create target directory: %w", err)
	}
	real, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", "", fmt.Errorf("resolve target directory: %w", err)
	}
	if !pathWithin(root, real) {
		return "", "", ErrPathOutsideRoot
	}
	rel, err := filepath.Rel(root, real)
	if err != nil {
		return "", "", fmt.Errorf("make target directory relative: %w", err)
	}
	if rel == "." {
		rel = ""
	}
	return filepath.Clean(real), filepath.ToSlash(rel), nil
}

func verifyExistingParent(root, candidate string) error {
	current := candidate
	for {
		_, err := os.Lstat(current)
		if err == nil {
			real, evalErr := filepath.EvalSymlinks(current)
			if evalErr != nil {
				return fmt.Errorf("resolve target parent: %w", evalErr)
			}
			if !pathWithin(root, real) {
				return ErrPathOutsideRoot
			}
			return nil
		}
		if !os.IsNotExist(err) {
			return fmt.Errorf("inspect target directory: %w", err)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return ErrPathOutsideRoot
		}
		current = parent
	}
}

func pathWithin(root, candidate string) bool {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(candidate))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func safeFilename(raw string) string {
	name := filepath.Base(strings.TrimSpace(raw))
	if name == "" || name == "." || name == string(filepath.Separator) {
		return "download"
	}
	name = strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', 0, '\r', '\n', '\t':
			return '_'
		default:
			if r < 32 {
				return '_'
			}
			return r
		}
	}, name)
	if name == "" || name == "." || name == ".." {
		return "download"
	}
	return name
}
