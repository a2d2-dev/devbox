package apps

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// 文件布局与 ID 安全（Issue #2 持久化与文件布局 + 安全模型）。
//
// 目录布局（dataDir 默认 /var/lib/devbox）：
//
//	<dataDir>/apps.db                   SQLite（元数据/任务/revision/幂等/审计）
//	<dataDir>/apps/<app-id>/
//	  compose.yaml                      事实源（可被 git/CLI 管理，卸载 devbox 后仍可 compose up）
//	  .env
//	  app.json                          元数据缓存（SQLite 为权威）
//	  revisions/<rev>.yaml              历史快照（内容 hash 命名）
//
// project name 固定为 devbox-<app-id>，受管 Compose 容器据此 label 发现。

// ProjectPrefix Compose project 名前缀。受管 project = ProjectPrefix + appID。
const ProjectPrefix = "devbox-"

// 受管自定义 label（额外打在容器上，便于精确识别，即使 SQLite 短暂不一致）。
const (
	LabelManaged = "devbox.managed" // "true"
	LabelAppID   = "devbox.app.id"  // appID
)

// Paths 计算应用相关路径。集中在此避免散落的路径拼接（防穿越）。
type Paths struct {
	DataDir string // 数据根目录
}

func NewPaths(dataDir string) *Paths {
	if dataDir == "" {
		dataDir = "/var/lib/devbox"
	}
	return &Paths{DataDir: dataDir}
}

// DBPath SQLite 文件路径。
func (p *Paths) DBPath() string { return filepath.Join(p.DataDir, "apps.db") }

// AppDir 应用目录（绝对）。
func (p *Paths) AppDir(appID string) string {
	return filepath.Join(p.DataDir, "apps", appID)
}

// ComposeFile 事实源 compose.yaml。
func (p *Paths) ComposeFile(appID string) string {
	return filepath.Join(p.AppDir(appID), "compose.yaml")
}

// EnvFile .env 路径。
func (p *Paths) EnvFile(appID string) string {
	return filepath.Join(p.AppDir(appID), ".env")
}

// AppMetaFile 元数据缓存。
func (p *Paths) AppMetaFile(appID string) string {
	return filepath.Join(p.AppDir(appID), "app.json")
}

// RevisionsDir revision 历史目录。
func (p *Paths) RevisionsDir(appID string) string {
	return filepath.Join(p.AppDir(appID), "revisions")
}

// RevisionFile 单个 revision 的 compose 快照。
func (p *Paths) RevisionFile(appID string, rev int64) string {
	return filepath.Join(p.RevisionsDir(appID), fmt.Sprintf("%d.yaml", rev))
}

// ProjectName Compose project 名（固定 devbox-<app-id>）。
func ProjectName(appID string) string { return ProjectPrefix + appID }

// AppIDFromProject 从 compose project 名还原 appID；非受管前缀返回空。
func AppIDFromProject(project string) string {
	if !strings.HasPrefix(project, ProjectPrefix) {
		return ""
	}
	id := strings.TrimPrefix(project, ProjectPrefix)
	if !isValidAppID(id) {
		return ""
	}
	return id
}

// appIDRegexp 合法 app ID：小写字母/数字/连字符，3..63 字符，不以连字符开头结尾。
var appIDRegexp = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{1,61}[a-z0-9])?$`)

// isValidAppID 严格校验 app ID（防路径穿越）。
func isValidAppID(id string) bool {
	if id == "" || len(id) > 63 {
		return false
	}
	if strings.Contains(id, "..") {
		return false
	}
	if strings.ContainsAny(id, "/\\") {
		return false
	}
	return appIDRegexp.MatchString(id)
}

// ValidateAppID 校验 app ID；不合法返回领域错误。
func ValidateAppID(id string) error {
	if !isValidAppID(id) {
		return ValidationErr(fmt.Sprintf("invalid app id %q: must be 3-63 chars of [a-z0-9-], not start/end with '-'", id))
	}
	return nil
}

// Slugify 把应用名转成合法 app ID（小写、非字母数字→连字符、去前后连字符）。
// 若结果为空或不合法，调用方应让用户改用显式 ID。
func Slugify(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	var b strings.Builder
	prevDash := true // 抑制开头连字符
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash {
				b.WriteRune('-')
				prevDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > 63 {
		out = strings.TrimRight(out[:63], "-")
	}
	return out
}

// EnsureAppDir 创建应用目录（含 revisions 子目录），权限 0755。
// appID 必须先过 ValidateAppID，否则拒绝创建。
func (p *Paths) EnsureAppDir(appID string) error {
	if err := ValidateAppID(appID); err != nil {
		return err
	}
	for _, dir := range []string{p.AppDir(appID), p.RevisionsDir(appID)} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create dir %s: %w", dir, err)
		}
	}
	return nil
}

// SafeWriteFile 写文件前确保父目录在应用目录内（防穿越到应用目录外）。
func (p *Paths) SafeWriteFile(appID, rel string, data []byte, mode os.FileMode) error {
	if err := ValidateAppID(appID); err != nil {
		return err
	}
	appDir := p.AppDir(appID)
	full := filepath.Join(appDir, rel)
	// 规范化后必须仍在 appDir 内。
	clean, err := filepath.Abs(filepath.Clean(full))
	if err != nil || !strings.HasPrefix(clean, appDir+string(filepath.Separator)) && clean != appDir {
		return ValidationErr("path escapes app directory")
	}
	if err := os.MkdirAll(filepath.Dir(clean), 0o755); err != nil {
		return err
	}
	return os.WriteFile(clean, data, mode)
}
