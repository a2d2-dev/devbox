package backup

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type managerConfig struct {
	workDir            string
	allowedTargetRoots []string
}

// ManagerOption configures backup safety policy without changing persisted tasks.
type ManagerOption func(*managerConfig)

// WithWorkDir sets the console workspace root allowed for local backup targets.
func WithWorkDir(path string) ManagerOption {
	return func(config *managerConfig) { config.workDir = path }
}

// WithAllowedTargetRoots adds local target roots beyond work_dir and /data.
func WithAllowedTargetRoots(paths ...string) ManagerOption {
	return func(config *managerConfig) {
		config.allowedTargetRoots = append(config.allowedTargetRoots, paths...)
	}
}

type pathPolicy struct {
	workDir     string
	targetRoots []string
	keysDir     string
}

func newPathPolicy(workDir, dataDir string, extraRoots []string) (pathPolicy, error) {
	if workDir == "" {
		workDir = "/data"
	}
	resolvedWorkDir, err := resolveExistingPath(workDir)
	if err != nil {
		return pathPolicy{}, fmt.Errorf("resolve backup work_dir: %w", err)
	}
	roots := []string{resolvedWorkDir}
	if resolvedWorkDir != "/data" {
		dataRoot, err := resolveExistingPath("/data")
		if err != nil {
			return pathPolicy{}, fmt.Errorf("resolve default backup target root: %w", err)
		}
		roots = append(roots, dataRoot)
	}
	for _, root := range extraRoots {
		resolved, err := resolveExistingPath(root)
		if err != nil {
			return pathPolicy{}, fmt.Errorf("resolve allowed backup target root %q: %w", root, err)
		}
		if !containsString(roots, resolved) {
			roots = append(roots, resolved)
		}
	}
	keysDir := filepath.Join(dataDir, "keys")
	if err := os.MkdirAll(keysDir, 0o700); err != nil {
		return pathPolicy{}, fmt.Errorf("create backup keys directory: %w", err)
	}
	resolvedKeysDir, err := resolveExistingPath(keysDir)
	if err != nil {
		return pathPolicy{}, fmt.Errorf("resolve backup keys directory: %w", err)
	}
	return pathPolicy{workDir: resolvedWorkDir, targetRoots: roots, keysDir: resolvedKeysDir}, nil
}

func resolveExistingPath(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("path must be absolute")
	}
	resolved, err := filepath.EvalSymlinks(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	return filepath.Clean(resolved), nil
}

func pathWithin(root, path string, allowEqual bool) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	return allowEqual || rel != "."
}

func (p pathPolicy) validateLocalTarget(path string) error {
	resolved, err := resolveExistingPath(path)
	if err != nil {
		return fmt.Errorf("解析目标路径: %w", err)
	}
	protected := []string{"/etc", "/usr", "/boot", "/proc", "/sys", "/dev", "/run"}
	workDirIsUnderVar := pathWithin("/var", p.workDir, true)
	if !workDirIsUnderVar || !pathWithin(p.workDir, resolved, true) {
		protected = append(protected, "/var")
	}
	for _, denied := range protected {
		if pathWithin(denied, resolved, true) || pathWithin(resolved, denied, true) {
			return fmt.Errorf("目标路径 %q 受系统路径策略保护", resolved)
		}
	}
	for _, root := range p.targetRoots {
		if pathWithin(root, resolved, false) {
			return nil
		}
	}
	return fmt.Errorf("目标路径必须位于允许根的子目录内（允许根：%s）", strings.Join(p.targetRoots, ", "))
}

func (p pathPolicy) validateIdentityFile(path string) error {
	resolved, err := resolveExistingPath(path)
	if err != nil {
		return fmt.Errorf("identity file 不可用；请将私钥放入 %s 后再配置: %w", p.keysDir, err)
	}
	if !pathWithin(p.keysDir, resolved, false) {
		return fmt.Errorf("identity file 必须位于 %s；请先将私钥上传或放置到该目录", p.keysDir)
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o400 == 0 {
		return fmt.Errorf("identity file is not a readable regular file")
	}
	return nil
}

func normalizeTaskPaths(task Task) (Task, error) {
	for _, endpoint := range []*Endpoint{&task.Source, &task.Target} {
		if endpoint.Type != EndpointSSH {
			resolved, err := resolveExistingPath(endpoint.Path)
			if err != nil {
				return Task{}, err
			}
			endpoint.Path = resolved
		}
		if endpoint.IdentityFile != "" {
			resolved, err := resolveExistingPath(endpoint.IdentityFile)
			if err != nil {
				return Task{}, err
			}
			endpoint.IdentityFile = resolved
		}
	}
	return task, nil
}
