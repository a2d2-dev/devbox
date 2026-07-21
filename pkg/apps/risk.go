package apps

import (
	"fmt"
	"path/filepath"
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
}

type composeService struct {
	Image       string      `yaml:"image"`
	Privileged  *bool       `yaml:"privileged"`
	NetworkMode string      `yaml:"network_mode"`
	PID         string      `yaml:"pid"`
	IPC         string      `yaml:"ipc"`
	User        string      `yaml:"user"`
	CapAdd      []string    `yaml:"cap_add"`
	Volumes     []yaml.Node `yaml:"volumes"`
	Ports       []yaml.Node `yaml:"ports"`
	Build       yaml.Node   `yaml:"build"`
	SecurityOpt []string    `yaml:"security_opt"`
}

// parseCompose 解析 Compose YAML 文本。返回精简根 + 解析错误。
func parseCompose(raw string) (*composeRoot, error) {
	var root composeRoot
	if err := yaml.Unmarshal([]byte(raw), &root); err != nil {
		return nil, fmt.Errorf("invalid compose yaml: %w", err)
	}
	if root.Services == nil {
		return nil, fmt.Errorf("compose has no services")
	}
	return &root, nil
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

	// host network：硬阻断（绕过网络隔离）。
	if strings.EqualFold(svc.NetworkMode, "host") {
		add(RiskBlocked, "network_mode", "network_mode:host 绕过网络命名空间隔离，已阻断")
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
		if dangerousCaps[strings.ToUpper(c)] {
			add(RiskConfirmation, "cap_add", fmt.Sprintf("授予敏感 capability %s，需确认", c))
		}
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
		// 子目录允许（如 /etc/devbox），仅根/关键目录本身阻断。
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
			if s := nodeString(svc.Ports[i]); s != "" {
				p.Ports = append(p.Ports, s)
			}
		}
		for i := range svc.Volumes {
			if s := nodeString(svc.Volumes[i]); s != "" {
				p.Volumes = append(p.Volumes, s)
			}
		}
		out = append(out, p)
	}
	return out, nil
}

func nodeString(n yaml.Node) string {
	switch n.Kind {
	case yaml.ScalarNode:
		return n.Value
	case yaml.MappingNode:
		// 长语法取 target 或 source。
		var m struct {
			Target string `yaml:"target"`
			Source string `yaml:"source"`
		}
		_ = n.Decode(&m)
		if m.Target != "" {
			return m.Target
		}
		return m.Source
	}
	return ""
}
