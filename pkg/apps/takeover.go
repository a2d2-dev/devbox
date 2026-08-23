package apps

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"
)

// 系统 Compose project 自动发现与安全接管（Issue #2 新增目标）。
//
// 设计要点（见 issue 验收 + 多轮 review）：
//   - 列表扫描不读 working_dir/config_files 指向的宿主文件；只有 Takeover（显式用户动作）
//     才读取并严格校验。
//   - 接管保留原 compose project name 原地管理（容器/网络/named volume 按 project name 键控，
//     不改名 = 数据不变）；持久化 OriginalProject，重启后 ComposeProjectName(meta) 恢复。
//   - 源目录只读：把 compose config --no-interpolate 归一化结果写成「托管副本」compose.yaml，
//     CLI 后续对托管副本用原 project name 操作；绝不修改源目录，绝不只复制首文件。
//   - 安全硬边界：working_dir 与 config file 用 openat2 原子打开并拒绝 symlink/magic-link；
//     config 必须在 working_dir 内且不得跨文件系统；working_dir 必须
//     绝对、非系统敏感根/祖先、非 devbox data dir / docker socket 目录（或其祖先/之下）；
//     普通 YAML、限大小。compose CLI 只接收 canonical 路径。错误只说策略拒绝，不回显内容。
//   - 插值门：托管目录无 .env，归一化副本中未提供安全默认值的 ${VAR}/$VAR 必须阻断（禁止
//     静默复制/读取宿主 .env secret），提示改用导入向导；${VAR:-非空} 放行。
//   - 原子持久化：staging dir 写 compose.yaml+revisions/1.yaml+marker，fsync 后 rename 到
//     AppDir（final 不存在），再提交 DB。无「DB 成功但事实源缺失」窗口。崩溃在 rename 后/
//     DB 前留下带 marker 的 orphan dir，重试按 marker(project+hash) 复用或冲突，绝不盲删。
//   - 接管前执行 compose config 与现有风险预检：blocked 不可 override，confirmation 需确认 + 审计。
//   - 审计 detail 不含宿主路径：仅 project/configCount/hash/confirmation。

const (
	maxTakeoverComposeFile  = 1 << 20 // 单个 compose 文件 1MiB
	maxTakeoverComposeTotal = 4 << 20 // 所有 config files 总计 4MiB
	takeoverMarker          = ".devbox-takeover"
)

// sensitiveSystemDirs working_dir 不得是这些目录或其祖先（伪造 label 指向系统根目录的防护）。
var sensitiveSystemDirs = []string{
	"/", "/etc", "/proc", "/sys", "/dev", "/run", "/var/run",
	"/boot", "/usr", "/bin", "/sbin", "/lib", "/lib64",
}

// Takeover 接管一个 discovered compose project（显式用户动作）。
func (s *service) Takeover(ctx context.Context, req TakeoverRequest, opts ApplyOptions) (Application, error) {
	// 锁必须在最前：序列化同 id 的并发接管。
	defer s.lockMutation(req.ID)()

	// 幂等（必须在 meta 早返回之前）：同 key + 同 (target,confirm) → 返回原 task.app；
	// 同 key + 异 target/confirm → 409。用 req.ID（deterministic discovery index 给出的稳定
	// target）+ ConfirmRisky 计算请求 hash，避免「目标已受管」早返回绕过全局 key 冲突。
	reqHash := hashTakeoverRequest(req.ID, opts.AllowRiskyConfirmation)
	if opts.IdempotencyKey != "" {
		rec, hit, err := s.repo.GetIdempotency(ctx, opts.IdempotencyKey)
		if err != nil {
			return Application{}, err
		}
		if hit {
			if rec.RequestHash == reqHash {
				if t, terr := s.repo.GetTask(ctx, rec.TaskID); terr == nil {
					return s.Get(ctx, t.AppID)
				}
			}
			return Application{}, ConflictErr("idempotency_conflict",
				"takeover idempotency key reused with a different target or confirmation")
		}
	}

	// 已受管（含本会话/并发前一次接管）→ 幂等返回（此时已确认无 key 冲突）。
	if meta, ok, err := s.repo.GetAppMeta(ctx, req.ID); err != nil {
		return Application{}, err
	} else if ok {
		return s.Get(ctx, meta.ID)
	}

	// 重新观测 daemon 取最新 working_dir/config_files labels（不信任客户端提交路径）。
	disc, ok := s.findDiscoveredByID(ctx, req.ID)
	if !ok {
		return Application{}, NotFoundErr(req.ID)
	}
	if disc.Discovered == nil {
		return Application{}, ValidationErr("目标不是可接管的 discovered project")
	}
	project := disc.RuntimeProject
	workingDir := disc.Discovered.WorkingDir
	configFiles := disc.Discovered.ConfigFiles
	if strings.TrimSpace(project) == "" {
		return Application{}, ValidationErr("缺少 compose project name")
	}
	// 可接管性：project name 合法 + 容器标签一致 + 有 working_dir/config_files。
	if !disc.Discovered.TakeoverAvailable {
		reason := disc.Discovered.Reason
		if reason == "" {
			reason = "当前不可接管"
		}
		return Application{}, ValidationErr("无法接管: " + reason + "；可改用粘贴/上传导入向导")
	}

	renderer := s.takeoverRenderer()
	if renderer == nil {
		return Application{}, CapabilityErr("compose 运行时不可用，无法接管")
	}

	// 1. 安全校验来源路径（canonical + 拒 symlink 链 + 敏感目录 + 读 body）。源目录只读，不修改。
	canonicalWork, sources, err := validateTakeoverPaths(workingDir, configFiles, s.paths.DataDir, socketDirOf(renderer))
	if err != nil {
		return Application{}, err
	}
	// 1b. 在任何 compose CLI 调用前，对每个原始 body 跑文件访问风险分析，阻断会读取额外
	//     宿主文件的指令（include/extends.file/env_file/secrets.file/configs.file/build），
	//     否则 CLI 自身会绕过顶层路径校验去读这些文件。
	for _, src := range sources {
		risks, rerr := AnalyzeComposeFileAccess(src.Body)
		if rerr != nil {
			return Application{}, ValidationErr("接管解析失败: " + rerr.Error())
		}
		if HasBlocked(risks) {
			return Application{}, RiskBlockedErr("接管目标含未受管的宿主文件读取（include/extends.file/env_file/secrets.file/configs.file/build）", risks)
		}
	}
	// 1c. 在任何临时落盘前，对每个原始 body 跑明文 secret 风险分析（lenient，容忍无 services 的
	//     override 文件）：blocked 立即拒绝——否则明文 PASSWORD/TOKEN 会被写到 devbox dataDir 的
	//     临时副本，失败/崩溃后残留。
	for _, src := range sources {
		risks, rerr := AnalyzeLiteralSecretsLenient(src.Body)
		if rerr != nil {
			return Application{}, ValidationErr("接管解析失败: " + rerr.Error())
		}
		if HasBlocked(risks) {
			return Application{}, RiskBlockedErr("接管目标含明文敏感值，已拒绝（请用 ${VAR} 引用 + 导入向导填写 secret）", risks)
		}
	}
	// 2. 归一化：先清理可能的崩溃残留（仅专用 takeover-render-*），再把已通过 secret 校验的
	//    bodies 写到 devbox 控制临时目录（0700 + 0600 副本 + 受控空 .env），再交给 CLI——消除
	//    原始路径 TOCTOU + 阻止 compose 自动读取 working_dir/.env。--project-directory=canonicalWork
	//    保留相对 bind 语义；多文件按原顺序无损合并，绝不只复制首文件。
	cleanupStaleTakeoverRender(s.paths.DataDir, staleTakeoverRenderAge)
	tempFiles, envFile, tempCleanup, err := stageTakeoverRenderSources(s.paths.DataDir, sources)
	if err != nil {
		return Application{}, err
	}
	defer tempCleanup()
	normalized, err := renderer.RenderProjectConfig(ctx, canonicalWork, project, tempFiles, envFile, true)
	if err != nil {
		return Application{}, err
	}
	if strings.TrimSpace(normalized) == "" {
		return Application{}, ValidationErr("compose 归一化结果为空")
	}

	// 3. 插值门：托管目录无 .env，未提供安全默认值的变量会导致首次 redeploy 空值/失败。
	//    禁止静默复制/读取宿主 .env secret → 阻断（仅变量名进错误，无值）。
	if unsafe := unsafeInterpolations(normalized); len(unsafe) > 0 {
		return Application{}, ValidationErr("接管目标含未提供值的 Compose 变量: " + strings.Join(unsafe, ", ") +
			"；请通过粘贴/上传导入向导重新填写参数/secret 后再接管")
	}
	// 在临时托管环境（空 env，不读宿主 .env）用现有 RenderConfig 把安全默认值展开后验证结构有效。
	rendered, _, err := s.renderForCheck(ctx, normalized, "", true)
	if err != nil {
		return Application{}, err
	}

	// 4. 风险预检（现有策略）：基于默认值展开后的内存内容（secret 不持久化）。
	if err := s.checkTakeoverRisks(rendered, opts.AllowRiskyConfirmation); err != nil {
		return Application{}, err
	}

	// 5. 受管 ID = 统一 deterministic discovery index 给出的 ID（即 req.ID，与 List/Get 一致）。
	//    不在此处用 claimed-only 重算 resolveDiscoveredID：它与 resolveDiscoveredIDs（含同轮
	//    其它 discovered 的 claimed）不等价，极端候选碰撞时会让接管 ID 漂移并占用另一个
	//    discovered。顶部 meta 重检已处理「已受管」；promoteTakeoverDir 处理目录冲突。
	appID := req.ID

	// 6. 原子落盘事实源（staging→rename），再提交 DB：无半接管窗口。
	now := s.now()
	hash := composeHash(normalized, nil)
	finalDir, created, err := s.promoteTakeoverDir(appID, project, hash, normalized)
	if err != nil {
		return Application{}, err
	}
	meta := AppRecord{
		ID: appID, Name: project, Runtime: RuntimeCompose,
		Source:          ApplicationSource{Kind: SourceLocal},
		OriginalProject: project,
		// 接管未执行 runtime Apply，不臆造已观测：Revision=1（托管副本即期望），ObservedRevision=0
		// 直到真实 redeploy/health 验证后才推进。
		Revision: 1, ObservedRevision: 0,
		CreatedAt: now, UpdatedAt: now,
	}
	rev := Revision{
		Number: 1, AppID: appID, ComposeHash: hash, Source: meta.Source,
		CreatedAt: now, CreatedBy: opts.Actor,
		Note: "takeover: normalized via `compose config --no-interpolate`; original project=" + project,
	}
	// TaskTypeTakeover：同步成功操作（不入队 worker），operation history 准确反映「接管」而非 Apply。
	// summary 含 confirmed=true/false，事务内留痕（即便审计 insert 失败亦可见确认事实）。
	task := Task{
		ID: uuid.NewString(), AppID: appID, Type: TaskTakeover, Status: TaskSucceeded,
		Phase: PhaseTaskVerifying, IdempotencyKey: opts.IdempotencyKey,
		RequestSummary: fmt.Sprintf("takeover project=%s confirmed=%v configFiles=%d", project, opts.AllowRiskyConfirmation, len(sources)),
		CreatedAt:      now, StartedAt: &now, FinishedAt: &now,
	}
	if err := s.repo.CommitApply(ctx, meta, rev, task, opts.IdempotencyKey, reqHash); err != nil {
		if created { // 仅清理本次新建目录，绝不盲删未知目录
			_ = os.RemoveAll(finalDir)
		}
		return Application{}, err
	}
	// DB 已提交：清理 in-progress marker（best-effort）+ 补 sidecar（非权威缓存）。
	_ = os.Remove(filepath.Join(finalDir, takeoverMarker))
	s.writeAppMetaSidecar(meta)
	// 审计 detail 不含宿主路径：仅 project/configCount/hash/confirmation。
	s.audit(ctx, opts.Actor, appID, "takeover:project="+project, task.ID,
		fmt.Sprintf("configFiles=%d hash=%s confirmed=%v", len(sources), truncHash(hash), opts.AllowRiskyConfirmation))
	return s.Get(ctx, appID)
}

// checkTakeoverRisks 复用现有风险策略：literal secret / 宿主文件读取 / 渲染后结构风险。
// blocked 不可 override；confirmation 需 allowConfirm（审计留痕）。
func (s *service) checkTakeoverRisks(rendered string, allowConfirm bool) error {
	if risks, err := AnalyzeLiteralSecrets(rendered); err != nil {
		return ValidationErr("接管渲染解析失败: " + err.Error())
	} else if HasBlocked(risks) {
		return RiskBlockedErr("接管目标含敏感环境变量明文", risks)
	}
	if risks, err := AnalyzeComposeFileAccess(rendered); err != nil {
		return ValidationErr("接管渲染解析失败: " + err.Error())
	} else if HasBlocked(risks) {
		return RiskBlockedErr("接管目标含未受管的宿主文件读取（env_file/extends/include 等）", risks)
	}
	findings, err := AnalyzeCompose(rendered)
	if err != nil {
		return ValidationErr("接管风险分析失败: " + err.Error())
	}
	if HasBlocked(findings) {
		return RiskBlockedErr("接管预检存在阻断级风险，已拒绝", findings)
	}
	if NeedsConfirmation(findings, allowConfirm) {
		return RiskBlockedErr("接管存在需确认的风险，请显式确认后重试", findings)
	}
	return nil
}

// takeoverRenderer 返回接管归一化器：优先注入的 fake（测试），否则从 compose adapter 断言。
func (s *service) takeoverRenderer() takeoverPrechecker {
	if s.takeover != nil {
		return s.takeover
	}
	if cr, ok := s.adapters[RuntimeCompose].(*composeRuntime); ok {
		return cr
	}
	return nil
}

// socketDirOf 取 renderer 的 socket 所在目录（接管路径校验拒绝 working_dir 落在其上）。
func socketDirOf(r takeoverPrechecker) string {
	if r == nil {
		return ""
	}
	if sp := r.SocketPath(); sp != "" {
		return filepath.Dir(sp)
	}
	return ""
}

// hashTakeoverRequest 接管请求指纹：target(req.ID, deterministic discovery index 给出的稳定
// id) + ConfirmRisky。同 target + 同确认 → 同 hash（幂等）；任一不同 → 409。
func hashTakeoverRequest(reqID string, confirmRisky bool) string {
	return sha256hex([]byte(fmt.Sprintf("takeover|%s|%v", reqID, confirmRisky)))
}

func truncHash(h string) string {
	if len(h) > 12 {
		return h[:12]
	}
	return h
}

// promoteTakeoverDir 原子地把托管事实源落盘到 AppDir（无半接管窗口）。
//
//	若 AppDir 已存在且其 takeover marker 与本次 (project,hash) 一致 → 复用（前次崩溃留下的
//	  完整 orphan dir，文件已正确），created=false。
//	若 AppDir 已存在但 marker 不一致/缺失 → 冲突（不盲删未知目录）。
//	否则：在 <dataDir>/apps/ 下建 staging dir，写 compose.yaml/revisions/1.yaml/marker，
//	  fsync 后 os.Rename(staging, AppDir)（final 不存在保证原子提升），created=true。
func (s *service) promoteTakeoverDir(appID, project, hash, normalized string) (finalDir string, created bool, err error) {
	finalDir = s.paths.AppDir(appID)
	if marker, merr := os.ReadFile(filepath.Join(finalDir, takeoverMarker)); merr == nil {
		mp, mh := parseTakeoverMarker(string(marker))
		if mp == project && mh == hash {
			return finalDir, false, nil // 前次崩溃的完整 orphan，文件已正确，仅待 DB 提交
		}
		return "", false, ConflictErr("takeover_dir_conflict",
			"目标目录已存在且不匹配本次接管（project/hash 不一致），请检查或手动清理后重试")
	} else if _, statErr := os.Stat(finalDir); statErr == nil {
		return "", false, ConflictErr("takeover_dir_conflict", "目标目录已存在（非接管 in-progress），请检查后重试")
	}
	appsRoot := filepath.Join(s.paths.DataDir, "apps")
	if err := os.MkdirAll(appsRoot, 0o755); err != nil {
		return "", false, err
	}
	stage, err := os.MkdirTemp(appsRoot, ".takeover-stage-*")
	if err != nil {
		return "", false, err
	}
	stageCleanup := func() { _ = os.RemoveAll(stage) }
	if err := writeSyncFile(filepath.Join(stage, "compose.yaml"), []byte(normalized), 0o644); err != nil {
		stageCleanup()
		return "", false, err
	}
	if err := os.MkdirAll(filepath.Join(stage, "revisions"), 0o755); err != nil {
		stageCleanup()
		return "", false, err
	}
	if err := writeSyncFile(filepath.Join(stage, "revisions", "1.yaml"), []byte(normalized), 0o644); err != nil {
		stageCleanup()
		return "", false, err
	}
	if err := writeSyncFile(filepath.Join(stage, takeoverMarker), []byte(formatTakeoverMarker(project, hash)), 0o644); err != nil {
		stageCleanup()
		return "", false, err
	}
	// 同步 staging 与 revisions 目录（保证新建条目持久；平台不支持 fsync 目录时 best-effort）。
	fsyncDir(filepath.Join(stage, "revisions"))
	fsyncDir(stage)
	// 原子提升：final 不存在（上面已确认），rename 同文件系统原子。
	if err := os.Rename(stage, finalDir); err != nil {
		stageCleanup()
		return "", false, err
	}
	// rename 后同步 appsRoot，保证目录条目（finalDir 的出现）持久（best-effort）。
	fsyncDir(appsRoot)
	return finalDir, true, nil
}

// fsyncDir best-effort 同步目录（持久化其下新建/改名条目）；平台不支持时忽略错误。
func fsyncDir(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	_ = f.Sync()
	_ = f.Close()
}

// writeSyncFile 写文件后 best-effort fsync 再 close（接管 staging 落盘用）。
func writeSyncFile(path string, data []byte, mode os.FileMode) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	_ = f.Sync()
	return f.Close()
}

func formatTakeoverMarker(project, hash string) string {
	return "project=" + project + "\nhash=" + hash + "\n"
}

func parseTakeoverMarker(s string) (project, hash string) {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "project="):
			project = strings.TrimSpace(strings.TrimPrefix(line, "project="))
		case strings.HasPrefix(line, "hash="):
			hash = strings.TrimSpace(strings.TrimPrefix(line, "hash="))
		}
	}
	return
}

// --- 插值门 ---

// interpolationTokenRe 匹配 ${...} 或 $VAR。
var interpolationTokenRe = regexp.MustCompile(`\$\{([^}]*)\}|\$([A-Za-z_][A-Za-z0-9_]*)`)

// unsafeInterpolations 返回归一化副本中「未提供安全默认值」的变量名（接管后托管目录无 .env，
// 这些变量会解析为空/报错）。$$ 为转义字面 $，先移除避免误判其后变量。仅返回变量名，不含值。
func unsafeInterpolations(normalized string) []string {
	scan := strings.ReplaceAll(normalized, "$$", "")
	unsafe := map[string]bool{}
	for _, m := range interpolationTokenRe.FindAllStringSubmatch(scan, -1) {
		brace, bare := m[1], m[2]
		name := bare
		mod := ""
		if brace != "" {
			i := 0
			for i < len(brace) && isInterpNameChar(brace[i], i == 0) {
				i++
			}
			name = brace[:i]
			mod = brace[i:]
		}
		if name == "" {
			continue
		}
		if !hasSafeInterpDefault(mod) {
			unsafe[name] = true
		}
	}
	out := make([]string, 0, len(unsafe))
	for k := range unsafe {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func isInterpNameChar(b byte, first bool) bool {
	switch {
	case b == '_':
		return true
	case b >= 'A' && b <= 'Z', b >= 'a' && b <= 'z':
		return true
	case !first && b >= '0' && b <= '9':
		return true
	}
	return false
}

// hasSafeInterpDefault 仅 ${VAR:-非空} / ${VAR-非空} 视为有安全默认值；其余（裸/${VAR}/
// ${VAR:?}/${VAR:+}/空默认）均需用户提供值 → 不安全。modifier 严格按首字符判定。
func hasSafeInterpDefault(mod string) bool {
	switch {
	case strings.HasPrefix(mod, ":-"):
		return strings.TrimSpace(mod[2:]) != ""
	case strings.HasPrefix(mod, "-"):
		return strings.TrimSpace(mod[1:]) != ""
	default:
		return false
	}
}

// --- 路径安全 ---

// takeoverSource 一个已安全读取的 config file：canonical 绝对路径 + 原始 body。
// body 供 AnalyzeComposeFileAccess 在任何 compose CLI 调用前阻断 include/extends/env_file
// 等会读取额外宿主文件的指令。
type takeoverSource struct {
	Path string
	Body string
}

// validateTakeoverPaths 校验接管来源路径的安全硬边界。返回 canonical working_dir 与每个
// config file 的 canonical 路径 + 已安全读取的 body。所有拒绝只说「路径策略拒绝」+ 简短原因，
// 绝不回显文件内容。
//
// 安全：文件打开走 openat2(RESOLVE_BENEATH|NO_SYMLINKS|NO_MAGICLINKS)（见 takeover_open_*.go），
// 安全决定与打开原子完成，消除「先 Lstat/EvalSymlinks/Stat 再 ReadFile」的可被替换（TOCTOU）窗口。
func validateTakeoverPaths(workingDir string, configFiles []string, dataDir, socketDir string) (string, []takeoverSource, error) {
	reject := func(reason string) (string, []takeoverSource, error) {
		return "", nil, ValidationErr("接管来源路径策略拒绝: " + reason)
	}
	if workingDir == "" {
		return reject("working_dir 为空")
	}
	canonicalWork := filepath.Clean(workingDir)
	if !filepath.IsAbs(canonicalWork) {
		return reject("working_dir 必须为绝对路径")
	}
	// 敏感目录策略（字符串判定，非文件读）：拒绝 working_dir == d、在 d 之下、或是 d 的祖先。
	for _, d := range sensitiveSystemDirs {
		if withinOrAncestor(canonicalWork, d) {
			return reject("working_dir 落在系统敏感目录（或其祖先/之下）")
		}
	}
	for _, d := range []string{dataDir, socketDir} {
		if d == "" {
			continue
		}
		if withinOrAncestor(canonicalWork, filepath.Clean(d)) {
			return reject("working_dir 落在 devbox 数据目录/docker socket 目录（或其祖先/之下）")
		}
	}
	// 安全打开 canonical working_dir（openat2 NO_SYMLINKS：路径链任何 symlink 原子拒绝）。
	workDirF, err := safeOpenWorkDir(canonicalWork)
	if err != nil {
		return reject("working_dir 不可安全打开（含 symlink / 不可访问 / 非 Linux）")
	}
	defer workDirF.Close()

	var sources []takeoverSource
	var total int64
	hasServices := false
	for _, cf := range configFiles {
		abs := cf
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(canonicalWork, abs)
		}
		abs = filepath.Clean(abs)
		// 相对 working_dir 的路径（供 openat2 BENEATH）；先做字符串逃逸校验。
		rel, err := filepath.Rel(canonicalWork, abs)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return reject("config file 逃逸 canonical working_dir")
		}
		// openat2 BENEATH|NO_SYMLINKS 原子安全打开（消除 check→open TOCTOU）。
		f, err := safeOpenConfigBeneath(workDirF, rel)
		if err != nil {
			return reject("config file 不可安全打开（逃逸/symlink/不可访问）")
		}
		regular, size, ferr := fdRegularSize(f)
		if ferr != nil || !regular {
			f.Close()
			return reject("config file 必须为普通文件")
		}
		if size > maxTakeoverComposeFile {
			f.Close()
			return reject("config file 超过单文件大小上限")
		}
		total += size
		if total > maxTakeoverComposeTotal {
			f.Close()
			return reject("config files 总计超过大小上限")
		}
		body, rerr := readAllBounded(f, size)
		f.Close()
		if rerr != nil {
			return reject("config file 不可读取")
		}
		if !looksLikeComposeYAML(body) {
			return reject("config file 非合法 Compose YAML（顶层须为非空 mapping）")
		}
		if composeBodyHasServices(body) {
			hasServices = true
		}
		sources = append(sources, takeoverSource{Path: abs, Body: string(body)})
	}
	if len(sources) == 0 {
		return reject("缺少 config file")
	}
	// 集合至少一个文件有非空 services（override 文件可只含 networks/volumes）。
	if !hasServices {
		return reject("config files 集合缺少非空 services（include-only 或无可接管服务）")
	}
	return canonicalWork, sources, nil
}

// looksLikeComposeYAML 校验顶层为非空 YAML mapping（compose 文档特征）。不要求单个文件都有
// services：override 文件可能只含 networks/volumes；整个 config_files 集合至少一个文件有非空
// services 由 validateTakeoverPaths 的集合检查把关（include-only 集合仍被 pre-scan 阻断）。
func looksLikeComposeYAML(body []byte) bool {
	var root map[string]any
	if err := yaml.Unmarshal(body, &root); err != nil {
		return false
	}
	return len(root) > 0
}

// composeBodyHasServices 该 body 顶层含非空 services mapping。
func composeBodyHasServices(body []byte) bool {
	var root map[string]any
	if err := yaml.Unmarshal(body, &root); err != nil {
		return false
	}
	svcs, ok := root["services"]
	if !ok {
		return false
	}
	services, ok := svcs.(map[string]any)
	return ok && len(services) > 0
}

// staleTakeoverRenderAge 接管渲染临时目录的「陈旧」阈值：超过该年龄的专用 takeover-render-*
// 目录视为崩溃残留并清理。取值远大于单次接管耗时（秒级），避免误删进行中的接管。
const staleTakeoverRenderAge = 1 * time.Hour

// cleanupStaleTakeoverRender 有界清理崩溃残留：仅扫描 dataDir 下名字以 takeover-render- 开头的
// 目录，且 ModTime 早于 now-staleTakeoverRenderAge 的才删除。绝不删除其它路径/非目录/新近目录。
func cleanupStaleTakeoverRender(dataDir string, maxAge time.Duration) {
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-maxAge)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, "takeover-render-") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(cutoff) {
			continue // 新近（可能进行中），不删
		}
		_ = os.RemoveAll(filepath.Join(dataDir, name))
	}
}

// stageTakeoverRenderSources 把已安全读取的 bodies 写到 devbox 控制临时目录（0700；副本 0600；
// 受控空 .env 0600），按原顺序返回副本路径 + envFile + cleanup。CLI 只接收这些副本，不打开
// 原始 config path（消除 TOCTOU），且 --env-file 阻止自动读取 working_dir/.env。
func stageTakeoverRenderSources(dataDir string, sources []takeoverSource) (files []string, envFile string, cleanup func(), err error) {
	tempDir, err := os.MkdirTemp(dataDir, "takeover-render-*")
	if err != nil {
		return nil, "", nil, err
	}
	cleanup = func() { _ = os.RemoveAll(tempDir) }
	if err := os.Chmod(tempDir, 0o700); err != nil {
		cleanup()
		return nil, "", nil, err
	}
	for i, src := range sources {
		p := filepath.Join(tempDir, fmt.Sprintf("file-%d.yaml", i))
		if err := os.WriteFile(p, []byte(src.Body), 0o600); err != nil {
			cleanup()
			return nil, "", nil, err
		}
		files = append(files, p)
	}
	envFile = filepath.Join(tempDir, ".env")
	if err := os.WriteFile(envFile, []byte{}, 0o600); err != nil {
		cleanup()
		return nil, "", nil, err
	}
	return files, envFile, cleanup, nil
}

// withinOrAncestor dir==target、dir 在 target 之下、或 dir 是 target 的祖先（双向）。
func withinOrAncestor(dir, target string) bool {
	dir = filepath.Clean(dir)
	target = filepath.Clean(target)
	if dir == target {
		return true
	}
	if strings.HasPrefix(target, dir+string(filepath.Separator)) {
		return true // dir 是 target 的祖先
	}
	if strings.HasPrefix(dir, target+string(filepath.Separator)) {
		return true // dir 在 target 之下
	}
	return false
}
