package apps

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// 风险策略（Issue #2 安全模型）：Docker daemon ≈ 宿主 root，是本功能最大风险。
//
// 分级：
//   - blocked：默认硬阻断（privileged / docker.sock 挂载 / 根目录 bind mount /
//     host network / pid host）。Apply 直接拒绝。
//   - confirmation：需调用方显式确认（AllowRiskyConfirmation），否则 Apply 拒绝。
//   - warning：暴露给用户但不阻断。
//   - safe：无风险，不进结果。
//
// 仅做单文件内容分析；跨应用的端口/路径冲突检测见 validator.go。

// RiskLevel 风险等级。
type RiskLevel string

const (
	RiskBlocked      RiskLevel = "blocked"
	RiskConfirmation RiskLevel = "confirmation"
	RiskWarning      RiskLevel = "warning"
)

// RiskFinding 单条风险发现。
type RiskFinding struct {
	Level   RiskLevel `json:"level"`
	Service string    `json:"service,omitempty"`
	Field   string    `json:"field,omitempty"`
	Message string    `json:"message"`
}

// HasBlocked 是否含阻断级风险。
func HasBlocked(findings []RiskFinding) bool {
	for _, f := range findings {
		if f.Level == RiskBlocked {
			return true
		}
	}
	return false
}

// NeedsConfirmation 是否含 confirmation 级且未获授权。
func NeedsConfirmation(findings []RiskFinding, confirmed bool) bool {
	if confirmed {
		return false
	}
	for _, f := range findings {
		if f.Level == RiskConfirmation {
			return true
		}
	}
	return false
}

// HasMutableImageRisk 返回 latest/main 这类可变镜像标签的风险项。
// 商店/catalog 来源（StrictMutableTag）将其升格为阻断：可变标签无法锁定版本，
// 违反项目红线（禁 latest/main）。inline 导入仅 warning 暴露，不阻断。
func HasMutableImageRisk(findings []RiskFinding) []RiskFinding {
	var out []RiskFinding
	for _, f := range findings {
		// analyzeService 对 latest/main 标签产出 Field=="image" 的 RiskWarning。
		if f.Field == "image" && f.Level == RiskWarning {
			out = append(out, f)
		}
	}
	return out
}

// 系统关键目录：bind 这些 ≈ 篡改/逃逸宿主，硬阻断。
var systemCriticalPaths = []string{
	"/", "/etc", "/usr", "/bin", "/sbin", "/boot", "/lib", "/lib64",
	"/proc", "/sys", "/dev", "/run", "/var", "/var/run", "/var/lib/docker",
}

// 危险 Linux capability：需 confirmation。
var dangerousCaps = map[string]bool{
	"SYS_ADMIN": true, "NET_ADMIN": true, "SYS_PTRACE": true, "SYS_MODULE": true,
	"DAC_READ_SEARCH": true, "DAC_OVERRIDE": true, "SETUID": true, "SETGID": true,
	"NET_RAW": true, "CHOWN": true, "FOWNER": true,
}

// composeRoot 仅提取风险相关字段（精简 Compose spec 子集）。
type composeRoot struct {
	Services map[string]composeService `yaml:"services"`
	Networks map[string]yaml.Node      `yaml:"networks"`
	Volumes  map[string]yaml.Node      `yaml:"volumes"`
	Configs  map[string]composeFileRef `yaml:"configs"`
	Secrets  map[string]composeFileRef `yaml:"secrets"`
	Include  yaml.Node                 `yaml:"include"`
}

type composeFileRef struct {
	File string `yaml:"file"`
}

type composeExtends struct {
	File string `yaml:"file"`
}

type composeService struct {
	Image       string         `yaml:"image"`
	Privileged  *bool          `yaml:"privileged"`
	NetworkMode string         `yaml:"network_mode"`
	PID         string         `yaml:"pid"`
	IPC         string         `yaml:"ipc"`
	UTS         string         `yaml:"uts"`
	UserNS      string         `yaml:"userns_mode"`
	User        string         `yaml:"user"`
	CapAdd      []string       `yaml:"cap_add"`
	Devices     []yaml.Node    `yaml:"devices"`
	Volumes     []yaml.Node    `yaml:"volumes"`
	Ports       []yaml.Node    `yaml:"ports"`
	Build       yaml.Node      `yaml:"build"`
	SecurityOpt []string       `yaml:"security_opt"`
	Environment yaml.Node      `yaml:"environment"`
	EnvFile     yaml.Node      `yaml:"env_file"`
	Extends     composeExtends `yaml:"extends"`
}

// parseCompose 解析 Compose YAML 文本。返回精简根 + 解析错误。
// parseComposeLenient 解析 Compose，但不要求 services（供 AnalyzeComposeFileAccess 分析
// 只含 networks/volumes 的 override 文件：仍能检测顶层 include/secrets/configs）。
func parseComposeLenient(raw string) (*composeRoot, error) {
	var root composeRoot
	if err := yaml.Unmarshal([]byte(raw), &root); err != nil {
		return nil, fmt.Errorf("invalid compose yaml: %w", err)
	}
	return &root, nil
}

func parseCompose(raw string) (*composeRoot, error) {
	root, err := parseComposeLenient(raw)
	if err != nil {
		return nil, err
	}
	if root.Services == nil {
		return nil, fmt.Errorf("compose has no services")
	}
	return root, nil
}

// AnalyzeCompose 分析 Compose 文本的风险。yaml 非法时返回 error。
func AnalyzeCompose(raw string) ([]RiskFinding, error) {
	root, err := parseCompose(raw)
	if err != nil {
		return nil, err
	}
	var findings []RiskFinding
	for name, svc := range root.Services {
		findings = append(findings, analyzeService(name, svc)...)
	}
	findings = append(findings, analyzeNetworks(root.Networks)...)
	return findings, nil
}

type composeNetwork struct {
	Driver string `yaml:"driver"`
	Name   string `yaml:"name"`
}

// analyzeNetworks 阻断通过顶层 network 定义接入 Docker 预置 host network。
// 这与 service.network_mode=host 具有相同的隔离绕过效果。
func analyzeNetworks(networks map[string]yaml.Node) []RiskFinding {
	var findings []RiskFinding
	for name, node := range networks {
		var network composeNetwork
		if err := node.Decode(&network); err != nil {
			continue
		}
		if strings.EqualFold(network.Driver, "host") || strings.EqualFold(network.Name, "host") {
			findings = append(findings, RiskFinding{
				Level: RiskBlocked, Field: "networks." + name,
				Message: "host network 绕过网络命名空间隔离，已阻断",
			})
		}
	}
	return findings
}

func analyzeService(name string, svc composeService) []RiskFinding {
	var f []RiskFinding
	add := func(level RiskLevel, field, msg string) {
		f = append(f, RiskFinding{Level: level, Service: name, Field: field, Message: msg})
	}

	// 镜像版本：latest/main 强警告（本地创建不能静默接受 → 至少 warning 暴露）。
	if img := svc.Image; img != "" {
		if isLatestOrMainTag(img) {
			add(RiskWarning, "image", "镜像使用 latest/main 标签，无法锁定版本，建议固定具体版本")
		}
	} else if svc.Build.IsZero() == false {
		// build 而无 image：可接受，但提示。
		add(RiskWarning, "build", "服务使用 build 而非固定 image，部署结果取决于构建环境")
	}

	// privileged：硬阻断。
	if svc.Privileged != nil && *svc.Privileged {
		add(RiskBlocked, "privileged", "privileged:true 几乎等价于宿主 root，已阻断")
	}

	// host 或 container:<id> network：硬阻断。后者可借用另一个 host-network
	// 容器的网络命名空间，不能作为 host 模式的旁路。
	networkMode := strings.ToLower(strings.TrimSpace(svc.NetworkMode))
	if networkMode == "host" || strings.HasPrefix(networkMode, "container:") {
		add(RiskBlocked, "network_mode", "network_mode 绕过独立网络命名空间隔离，已阻断")
	}

	// pid host / ipc host。
	if strings.EqualFold(svc.PID, "host") {
		add(RiskBlocked, "pid", "pid:host 共享宿主进程命名空间，已阻断")
	}
	if strings.EqualFold(svc.IPC, "host") {
		add(RiskConfirmation, "ipc", "ipc:host 共享宿主 IPC 命名空间，需确认")
	}

	// 危险 capability：confirmation。
	for _, c := range svc.CapAdd {
		if strings.EqualFold(c, "ALL") {
			add(RiskBlocked, "cap_add", "cap_add: ALL 授予全部 Linux capabilities，已阻断")
		} else if dangerousCaps[strings.ToUpper(c)] {
			add(RiskConfirmation, "cap_add", fmt.Sprintf("授予敏感 capability %s，需确认", c))
		}
	}
	if strings.EqualFold(svc.UTS, "host") {
		add(RiskBlocked, "uts", "uts:host 共享宿主 UTS namespace，已阻断")
	}
	if strings.EqualFold(svc.UserNS, "host") {
		add(RiskBlocked, "userns_mode", "userns_mode:host 禁用用户命名空间隔离，已阻断")
	}
	if len(svc.Devices) > 0 {
		add(RiskConfirmation, "devices", "服务直接访问宿主设备，需确认设备权限边界")
	}

	// security_opt: apparmor=unconfined / seccomp:unconfined → confirmation。
	for _, s := range svc.SecurityOpt {
		if strings.Contains(s, "apparmor=unconfined") || strings.Contains(s, "seccomp=unconfined") {
			add(RiskConfirmation, "security_opt", "禁用 apparmor/seccomp 限制，需确认")
		}
	}

	// volumes：docker.sock 挂载（硬阻断）+ 根目录 bind（硬阻断）。
	for i := range svc.Volumes {
		analyzeVolumeNode(name, &svc.Volumes[i], &f)
	}

	// user: root：warning。
	if svc.User == "root" || svc.User == "0" || svc.User == "0:0" {
		add(RiskWarning, "user", "以 root 运行容器，建议使用非 root 用户")
	}

	return f
}

// AnalyzeLiteralSecrets 必须对未插值的事实源调用；渲染后 ${KEY} 已变成值，无法区分。
func AnalyzeLiteralSecrets(raw string) ([]RiskFinding, error) {
	return analyzeLiteralSecrets(raw, false)
}

// AnalyzeLiteralSecretsLenient 同 AnalyzeLiteralSecrets，但用 lenient 解析（容忍无 services 的
// override 文件：无 services 时返回空 findings）。Takeover 对多文件逐个 raw body 预检用此版本；
// Apply 对完整文档仍用严格的 AnalyzeLiteralSecrets。
func AnalyzeLiteralSecretsLenient(raw string) ([]RiskFinding, error) {
	return analyzeLiteralSecrets(raw, true)
}

func analyzeLiteralSecrets(raw string, lenient bool) ([]RiskFinding, error) {
	var (
		root *composeRoot
		err  error
	)
	if lenient {
		root, err = parseComposeLenient(raw)
	} else {
		root, err = parseCompose(raw)
	}
	if err != nil {
		return nil, err
	}
	var findings []RiskFinding
	for service, svc := range root.Services {
		for _, key := range literalSecretEnvKeys(svc.Environment) {
			findings = append(findings, RiskFinding{
				Level: RiskBlocked, Service: service, Field: "environment",
				Message: fmt.Sprintf("敏感变量 %s 含明文值；请改用 ${%s} 引用并在 Secret 输入框配置", key, key),
			})
		}
	}
	return findings, nil
}

// AnalyzeComposeFileAccess 在调用 Compose CLI 前检查会读取宿主文件的字段。MVP 只
// 托管 compose.yaml/.env，不接受额外文件；否则第三方包可借后端权限读取宿主内容。
func AnalyzeComposeFileAccess(raw string) ([]RiskFinding, error) {
	root, err := parseComposeLenient(raw) // 容忍无 services 的 override 文件
	if err != nil {
		return nil, err
	}
	var findings []RiskFinding
	add := func(service, field, message string) {
		findings = append(findings, RiskFinding{Level: RiskBlocked, Service: service, Field: field, Message: message})
	}
	if !root.Include.IsZero() {
		add("", "include", "include 会读取额外 Compose 文件，当前安全策略不支持")
	}
	for name, ref := range root.Configs {
		if strings.TrimSpace(ref.File) != "" {
			add("", "configs."+name+".file", "configs.file 会读取宿主文件，已阻断")
		}
	}
	for name, ref := range root.Secrets {
		if strings.TrimSpace(ref.File) != "" {
			add("", "secrets."+name+".file", "secrets.file 会读取宿主文件，已阻断；请使用受管 .env Secret")
		}
	}
	for name, svc := range root.Services {
		if !svc.EnvFile.IsZero() {
			add(name, "env_file", "env_file 会读取额外宿主文件，已阻断；请使用受管 .env")
		}
		if strings.TrimSpace(svc.Extends.File) != "" {
			add(name, "extends.file", "extends.file 会读取额外 Compose 文件，已阻断")
		}
		if !svc.Build.IsZero() {
			add(name, "build", "build context 可读取并发送宿主文件，当前安全策略只允许固定镜像")
		}
		for i := range svc.Volumes {
			if source := volumeSource(&svc.Volumes[i]); isManagedControlPath(source) {
				add(name, "volumes", fmt.Sprintf("禁止把受管控制文件 %q 挂载进容器", source))
			}
		}
	}
	return findings, nil
}

func volumeSource(node *yaml.Node) string {
	switch node.Kind {
	case yaml.ScalarNode:
		parts := strings.SplitN(node.Value, ":", 2)
		if len(parts) == 2 {
			return strings.TrimSpace(parts[0])
		}
	case yaml.MappingNode:
		var vol struct {
			Source string `yaml:"source"`
		}
		_ = node.Decode(&vol)
		return strings.TrimSpace(vol.Source)
	}
	return ""
}

func isManagedControlPath(source string) bool {
	if source == "" || filepath.IsAbs(source) {
		return false
	}
	clean := filepath.ToSlash(filepath.Clean(source))
	return clean == "." || clean == ".env" || clean == "compose.yaml" || clean == "compose.yml" ||
		strings.HasPrefix(clean, "revisions/") || strings.HasPrefix(clean, "secrets/")
}

func literalSecretEnvKeys(node yaml.Node) []string {
	var out []string
	switch node.Kind {
	case yaml.MappingNode:
		for i := 0; i+1 < len(node.Content); i += 2 {
			key := strings.TrimSpace(node.Content[i].Value)
			value := strings.TrimSpace(node.Content[i+1].Value)
			if isSecretyKey(key) && value != "" && !safeSecretReference(value, key) {
				out = append(out, key)
			}
		}
	case yaml.SequenceNode:
		for _, item := range node.Content {
			line := strings.TrimSpace(item.Value)
			idx := strings.IndexByte(line, '=')
			if idx <= 0 {
				continue
			}
			key, value := strings.TrimSpace(line[:idx]), strings.TrimSpace(line[idx+1:])
			if isSecretyKey(key) && value != "" && !safeSecretReference(value, key) {
				out = append(out, key)
			}
		}
	}
	return out
}

func safeSecretReference(value, key string) bool {
	pattern := `^\$\{` + regexp.QuoteMeta(key) + `(?::\?[^}]*)?\}$`
	return regexp.MustCompile(pattern).MatchString(strings.TrimSpace(value))
}

func analyzeVolumeNode(service string, node *yaml.Node, f *[]RiskFinding) {
	switch node.Kind {
	case yaml.ScalarNode:
		// 短语法："host:container[:mode]" 或 "container:path" 或命名 volume。
		analyzeVolumeString(service, node.Value, f)
	case yaml.MappingNode:
		// 长语法：{type, source, target, ...}
		var vol struct {
			Type   string `yaml:"type"`
			Source string `yaml:"source"`
			Target string `yaml:"target"`
		}
		_ = node.Decode(&vol)
		if vol.Type == "bind" || (vol.Type == "" && looksLikeAbsPath(vol.Source)) {
			src := vol.Source
			if src != "" {
				analyzeBindSource(service, src, f)
			}
		}
	case yaml.SequenceNode:
		for i := 0; i < len(node.Content); i++ {
			analyzeVolumeNode(service, node.Content[i], f)
		}
	}
}

func analyzeVolumeString(service, spec string, f *[]RiskFinding) {
	// "host:container[:mode]" — 只取第一段判断 host 源。
	parts := strings.SplitN(spec, ":", 2)
	src := strings.TrimSpace(parts[0])
	if src == "" {
		return
	}
	// 命名 volume（非绝对路径）忽略；bind（绝对路径）检查。
	if !looksLikeAbsPath(src) {
		return
	}
	analyzeBindSource(service, src, f)
}

func analyzeBindSource(service, src string, f *[]RiskFinding) {
	add := func(level RiskLevel, field, msg string) {
		*f = append(*f, RiskFinding{Level: level, Service: service, Field: "volumes", Message: msg})
	}
	// docker.sock 挂载（任意写法，含 ${SOCK} 已被渲染展开的情形）：硬阻断。
	lower := strings.ToLower(src)
	if strings.HasSuffix(lower, "docker.sock") {
		add(RiskBlocked, "volumes", "挂载 docker.sock 等价于授予宿主 root，已阻断")
		return
	}
	clean := filepath.Clean(src)
	// 相对路径穿越：Clean 后仍含 ".." 表示试图逃出项目目录（绝对路径的 ".." 会被
	// Clean 折叠，故仅相对 bind 会残留）。渲染路径下 compose config 会把相对 bind
	// 解析为绝对，配合下面的系统目录检查；此处覆盖静态/未渲染场景。
	if strings.Contains(clean, "..") {
		add(RiskConfirmation, "volumes", "相对 bind 含路径穿越（..），可能逃出项目目录，需确认")
		return
	}
	for _, p := range systemCriticalPaths {
		if clean == p {
			add(RiskBlocked, "volumes", fmt.Sprintf("bind 挂载系统关键目录 %s，已阻断", p))
			return
		}
	}
	for _, p := range []string{"/proc", "/sys", "/dev", "/var/lib/docker"} {
		if strings.HasPrefix(clean, p+string(filepath.Separator)) {
			add(RiskBlocked, "volumes", fmt.Sprintf("bind 挂载系统关键目录子路径 %s，已阻断", clean))
			return
		}
	}
	if filepath.IsAbs(clean) {
		add(RiskConfirmation, "volumes", fmt.Sprintf("bind 挂载宿主绝对路径 %s，数据生命周期不归应用管理，需确认", clean))
	}
}

func looksLikeAbsPath(p string) bool {
	return strings.HasPrefix(p, "/") || strings.HasPrefix(p, "./") || strings.HasPrefix(p, "../")
}

// isLatestOrMainTag 判断 image 是否使用 latest/main 这类可变标签。
func isLatestOrMainTag(image string) bool {
	idx := strings.LastIndex(image, ":")
	// 无 tag 默认 latest。
	tag := "latest"
	if idx >= 0 {
		// 排除 registry:port（含 / 在 : 之后）。
		if !strings.Contains(image[idx+1:], "/") {
			tag = image[idx+1:]
		}
	}
	switch strings.ToLower(strings.TrimSpace(tag)) {
	case "", "latest", "main", "master", "edge", "nightly":
		return true
	}
	return false
}

// ExtractServicePreviews 解析 Compose 提取服务预览（image/ports/volumes），供预检展示。
func ExtractServicePreviews(raw string) ([]ServicePreview, error) {
	root, err := parseCompose(raw)
	if err != nil {
		return nil, err
	}
	out := make([]ServicePreview, 0, len(root.Services))
	for name, svc := range root.Services {
		p := ServicePreview{Name: name, Image: svc.Image}
		for i := range svc.Ports {
			if s := previewPort(svc.Ports[i]); s != "" {
				p.Ports = append(p.Ports, s)
			}
		}
		for i := range svc.Volumes {
			if s := previewVolume(svc.Volumes[i]); s != "" {
				p.Volumes = append(p.Volumes, s)
			}
		}
		out = append(out, p)
	}
	return out, nil
}

func previewPort(n yaml.Node) string {
	if n.Kind == yaml.ScalarNode {
		return n.Value
	}
	if n.Kind != yaml.MappingNode {
		return ""
	}
	target := mappingScalar(n, "target")
	published := mappingScalar(n, "published")
	if target == "" {
		return published
	}
	port := target
	if published != "" {
		hostIP := mappingScalar(n, "host_ip")
		if strings.Contains(hostIP, ":") && !strings.HasPrefix(hostIP, "[") {
			hostIP = "[" + hostIP + "]"
		}
		if hostIP != "" {
			port = hostIP + ":" + published + ":" + target
		} else {
			port = published + ":" + target
		}
	}
	if protocol := mappingScalar(n, "protocol"); protocol != "" && protocol != "tcp" {
		port += "/" + protocol
	}
	return port
}

func previewVolume(n yaml.Node) string {
	if n.Kind == yaml.ScalarNode {
		return n.Value
	}
	if n.Kind != yaml.MappingNode {
		return ""
	}
	source := mappingScalar(n, "source")
	target := mappingScalar(n, "target")
	volume := target
	if source != "" && target != "" {
		volume = source + ":" + target
	} else if source != "" {
		volume = source
	}
	if volume != "" && mappingScalar(n, "read_only") == "true" {
		volume += ":ro"
	}
	return volume
}

func mappingScalar(n yaml.Node, key string) string {
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key && n.Content[i+1].Kind == yaml.ScalarNode {
			return n.Content[i+1].Value
		}
	}
	return ""
}
