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

// PendingEnvFile 是 Apply revision 对应的临时期望环境。它位于 revision 目录外、
// 权限 0600，worker 提升为 .env 后删除；revision/audit 中永不保存 secret。
func (p *Paths) PendingEnvFile(appID string, rev int64) string {
	return filepath.Join(p.AppDir(appID), fmt.Sprintf(".env.pending-%d", rev))
}

// ProjectName Compose project 名（固定 devbox-<app-id>）。
//
// 仅用于 devbox 自己创建的应用。接管的外部 compose project 必须保留原 project name
// 原地管理，应使用 ComposeProjectName(meta)，不要直接调用本函数。
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

// ComposeProjectName 返回 app 真实 compose project name（系统 Compose project 自动发现与接管）。
//
// 这是 runtime adapter 解析真实 project 的唯一领域入口（单一事实源）：
//   - 接管的外部 project 保留其原始 project name 原地管理——容器名、网络名与 named volume
//     都按 compose project name 键控，保留原名 = 不重建 = 数据不变；
//   - devbox 自己创建的应用用 devbox-<app-id>。
//
// controller 在构造交给 runtime 的 Application 时把结果写入 app.RuntimeProject（内部字段，
// 不序列化），compose runtime 的 Apply/Operate/Remove/Logs/projectEmpty 与 worker 健康检查
// 统一从 composeProject(app)=app.RuntimeProject 读取，不复用 K8s 专属的 Namespace。
func ComposeProjectName(meta AppRecord) string {
	if meta.OriginalProject != "" {
		return meta.OriginalProject
	}
	return ProjectName(meta.ID)
}

// ExternalIDPrefix / DiscoveredAltPrefix discovered（未接管）compose project 稳定 ID 的前缀。
// 主候选用 ext-，与受管历史 meta 冲突时回退 discovered-。二者均含 shortHash(project)，
// 不全局禁止任何前缀——历史合法受管 app id（含 ext- 开头）照常使用，冲突由 resolveDiscoveredID 消解。
const (
	ExternalIDPrefix    = "ext-"
	DiscoveredAltPrefix = "discovered-"
)

// IsExternalID 判断 id 是否为 discovered compose project 的稳定 ID（主/副候选前缀）。
func IsExternalID(id string) bool {
	return strings.HasPrefix(id, ExternalIDPrefix) || strings.HasPrefix(id, DiscoveredAltPrefix)
}

// discoveredIDWithPrefix 在固定前缀下为 project 生成稳定、合法、≤63 的 ID：
//   - base = Slugify(project)，截断预留 "-"+hash（共 13 字符），裁首尾 '-'；
//   - hash = shortHash(project)，含原始 project name → "a_b"/"a-b" slug 相同也区分；
//   - base 为空兜底 "discovered"；最终过 isValidAppID（3..63）。
func discoveredIDWithPrefix(prefix, project string) string {
	if project == "" {
		return ""
	}
	hash := shortHash(project)
	maxBase := 63 - len(prefix) - len("-"+hash)
	if maxBase < 1 {
		maxBase = 1
	}
	base := Slugify(project)
	if len(base) > maxBase {
		base = strings.Trim(base[:maxBase], "-")
	}
	if base == "" {
		base = "discovered"
	}
	id := prefix + base + "-" + hash
	if !isValidAppID(id) {
		id = prefix + "discovered-" + hash
	}
	return id
}

// ExternalID discovered compose project 的主候选稳定 ID（ext-<slug>-<hash>）。
func ExternalID(project string) string { return discoveredIDWithPrefix(ExternalIDPrefix, project) }

// DiscoveredAltID discovered compose project 的第二稳定候选 ID（discovered-<slug>-<hash>）。
// 主候选与某受管 meta 冲突时使用；与主候选仅前缀不同，hash 仍含原始 project name，
// 故与其它 project 的主/副 ID 不碰撞，且对同一 project 稳定。
func DiscoveredAltID(project string) string {
	return discoveredIDWithPrefix(DiscoveredAltPrefix, project)
}

// resolveDiscoveredID 在 claimed（已占用的 app ID 集合）约束下，返回 project 稳定、不冲突
// 的 discovered ID。list/get/takeover 必须用同一函数 + 同一 claimed 集，保证 ID 一致、
// 冲突时双方都展示且 discovered ID 稳定。
//
// 候选顺序（每个都含 shortHash(project)，对同一 project 稳定）：
//  1. 主候选 ExternalID（ext-<slug>-<hash>）；
//  2. 副候选 DiscoveredAltID（discovered-<slug>-<hash>）；
//  3. 有界 salt 候选（固定 salt 表，改输入→改 hash，仍稳定且与 project 绑定）。
//
// 任一候选被 claimed 占用即跳过；全部被占返回空串，调用方按冲突处理（list 跳过该
// discovered 项，takeover 返回显式冲突错误）——不无检查返回可能冲突的 ID。
func resolveDiscoveredID(project string, claimed map[string]bool) string {
	for _, cand := range []string{ExternalID(project), DiscoveredAltID(project)} {
		if !claimed[cand] {
			return cand
		}
	}
	for _, salt := range []string{"x", "y", "z", "w"} {
		cand := discoveredIDWithPrefix(ExternalIDPrefix, salt+"\x00"+project)
		if !claimed[cand] {
			return cand
		}
	}
	return ""
}

// appIDRegexp 合法 app ID：小写字母/数字/连字符，3..63 字符，不以连字符开头结尾。
var appIDRegexp = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{1,61}[a-z0-9])?$`)

// composeProjectNameRegexp Compose project name 官方约束：小写字母/数字开头，后接小写字母/
// 数字/下划线/连字符，长度 1..64。用于接管前严格校验（拒控制字符/换行，防 task/audit/marker 注入）。
var composeProjectNameRegexp = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// ValidComposeProjectName 严格校验 Compose project name（接管前）。非法时 discovered 列表
// 仍展示，但 takeoverAvailable=false、Takeover 拒绝。
func ValidComposeProjectName(project string) bool {
	return composeProjectNameRegexp.MatchString(project)
}

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
	// 规范化后必须仍在 appDir 内（显式括号：err 或 逃逸 都拒绝）。
	clean, err := filepath.Abs(filepath.Clean(full))
	escaped := !strings.HasPrefix(clean, appDir+string(filepath.Separator)) && clean != appDir
	if err != nil || escaped {
		return ValidationErr("path escapes app directory")
	}
	if err := os.MkdirAll(filepath.Dir(clean), 0o755); err != nil {
		return err
	}
	return os.WriteFile(clean, data, mode)
}

// AtomicWriteFile 在受管 app 目录内以同目录 rename 原子替换文件。
func (p *Paths) AtomicWriteFile(appID, rel string, data []byte, mode os.FileMode) error {
	if err := ValidateAppID(appID); err != nil {
		return err
	}
	final := filepath.Join(p.AppDir(appID), rel)
	clean, err := filepath.Abs(filepath.Clean(final))
	escaped := !strings.HasPrefix(clean, p.AppDir(appID)+string(filepath.Separator)) && clean != p.AppDir(appID)
	if err != nil || escaped {
		return ValidationErr("path escapes app directory")
	}
	if err := os.MkdirAll(filepath.Dir(clean), 0o755); err != nil {
		return err
	}
	stage, err := os.CreateTemp(filepath.Dir(clean), "."+filepath.Base(clean)+".stage-*")
	if err != nil {
		return err
	}
	stageName := stage.Name()
	defer os.Remove(stageName)
	if err := stage.Chmod(mode); err != nil {
		stage.Close()
		return err
	}
	if _, err := stage.Write(data); err != nil {
		stage.Close()
		return err
	}
	if err := stage.Close(); err != nil {
		return err
	}
	if err := os.Rename(stageName, clean); err != nil {
		return err
	}
	return nil
}
