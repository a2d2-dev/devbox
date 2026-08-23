package apps

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// 存储清单与环境变量元信息（Issue #2 要求 6）。
//
// 设计：纯静态分析——事实源是 compose.yaml（+ 可选 .env）。不查 docker daemon：
//   - 卸载 devbox 后 compose.yaml 仍可 `compose up`，状态应自洽；
//   - daemon 不可用时不影响详情（与 Observe 的降级语义一致）。
//
// 安全：环境变量只返回 key/configured/type/required，**绝不返回值**（.env 含 secret）。
// 卷清单按 managed/external/bind/socket 分类，用于详情与 remove 预览（要求 7）。

// invRoot 仅提取 storage/env 分析所需字段（与 risk.go 的 parseCompose 解耦，各管各的字段集）。
type invRoot struct {
	Services map[string]invService `yaml:"services"`
	Volumes  map[string]yaml.Node  `yaml:"volumes"` // 顶层命名卷声明
}

type invService struct {
	Volumes     []yaml.Node `yaml:"volumes"`
	Environment yaml.Node   `yaml:"environment"`
}

// composeNetworkInventory 返回预检可展示的网络名。显式顶层定义和 service 引用
// 都会保留；完全未声明网络时 Compose 使用隐式 default 网络。
func composeNetworkInventory(composeYAML string) ([]string, error) {
	var root struct {
		Services map[string]struct {
			Networks    yaml.Node `yaml:"networks"`
			NetworkMode string    `yaml:"network_mode"`
		} `yaml:"services"`
		Networks map[string]yaml.Node `yaml:"networks"`
	}
	if err := yaml.Unmarshal([]byte(composeYAML), &root); err != nil {
		return nil, fmt.Errorf("invalid compose yaml: %w", err)
	}
	seen := map[string]bool{}
	for name := range root.Networks {
		if strings.TrimSpace(name) != "" {
			seen[name] = true
		}
	}
	for _, service := range root.Services {
		if mode := strings.TrimSpace(service.NetworkMode); mode != "" {
			seen["mode:"+mode] = true
		}
		collectNetworkNames(service.Networks, seen)
	}
	if len(seen) == 0 {
		return []string{"default"}, nil
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out, nil
}

func collectNetworkNames(node yaml.Node, seen map[string]bool) {
	switch node.Kind {
	case yaml.ScalarNode:
		if name := strings.TrimSpace(node.Value); name != "" {
			seen[name] = true
		}
	case yaml.SequenceNode:
		for _, child := range node.Content {
			collectNetworkNames(*child, seen)
		}
	case yaml.MappingNode:
		for i := 0; i+1 < len(node.Content); i += 2 {
			if name := strings.TrimSpace(node.Content[i].Value); name != "" {
				seen[name] = true
			}
		}
	}
}

func preflightSecretKeys(composeYAML, envFile string, submitted map[string]string) []string {
	seen := map[string]bool{}
	var root struct {
		Secrets  map[string]yaml.Node `yaml:"secrets"`
		Services map[string]struct {
			Secrets yaml.Node `yaml:"secrets"`
		} `yaml:"services"`
	}
	if yaml.Unmarshal([]byte(composeYAML), &root) == nil {
		for key := range root.Secrets {
			if strings.TrimSpace(key) != "" {
				seen[key] = true
			}
		}
		for _, service := range root.Services {
			collectSecretNames(service.Secrets, seen)
		}
	}
	for key := range submitted {
		if strings.TrimSpace(key) != "" {
			seen[key] = true
		}
	}
	if _, vars, err := analyzeStorage(composeYAML, envFile); err == nil {
		for _, variable := range vars {
			if variable.Type == "password" {
				seen[variable.Key] = true
			}
		}
	}
	out := make([]string, 0, len(seen))
	for key := range seen {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func collectSecretNames(node yaml.Node, seen map[string]bool) {
	switch node.Kind {
	case yaml.ScalarNode:
		if key := strings.TrimSpace(node.Value); key != "" {
			seen[key] = true
		}
	case yaml.SequenceNode:
		for _, child := range node.Content {
			if child.Kind == yaml.MappingNode {
				if source := mappingValue(child, "source"); source != nil && strings.TrimSpace(source.Value) != "" {
					seen[strings.TrimSpace(source.Value)] = true
				}
			} else {
				collectSecretNames(*child, seen)
			}
		}
	}
}

type invTopVolume struct {
	External bool   `yaml:"external"`
	Name     string `yaml:"name"`
}

// analyzeStorage 解析 compose.yaml + .env，推导卷清单与环境变量元信息（不含值）。
func analyzeStorage(composeYAML, envFile string) ([]VolumeInfo, []EnvVarInfo, error) {
	var root invRoot
	if err := yaml.Unmarshal([]byte(composeYAML), &root); err != nil {
		return nil, nil, fmt.Errorf("invalid compose yaml: %w", err)
	}

	// 顶层命名卷：标记 external。
	topExternal := map[string]bool{}
	topDeclared := map[string]bool{}
	for name, node := range root.Volumes {
		topDeclared[name] = true
		var v invTopVolume
		_ = node.Decode(&v)
		if v.External || (node.Kind == yaml.ScalarNode && strings.EqualFold(node.Value, "external")) {
			topExternal[name] = true
		}
	}

	// 卷清单：遍历各 service.volumes，按 (kind,source,target) 去重。
	seen := map[string]bool{}
	var vols []VolumeInfo
	addVol := func(vi VolumeInfo) {
		key := string(vi.Kind) + "|" + vi.Source + "|" + vi.Target
		if seen[key] {
			return
		}
		seen[key] = true
		vols = append(vols, vi)
	}
	for _, svc := range root.Services {
		for i := range svc.Volumes {
			for _, vi := range classifyVolumeNode(svc.Volumes[i], topExternal) {
				addVol(vi)
			}
		}
	}

	configured := parseEnvKeys(envFile)
	used := envReferences(composeYAML, root)
	envVars := buildEnvInventory(configured, used)
	return vols, envVars, nil
}

// classifyVolumeNode 把单个 service.volumes 节点（短/长/数组语法）分类为 VolumeInfo。
func classifyVolumeNode(node yaml.Node, topExternal map[string]bool) []VolumeInfo {
	switch node.Kind {
	case yaml.ScalarNode:
		return []VolumeInfo{classifyVolumeSpec(node.Value, topExternal)}
	case yaml.MappingNode:
		var vol struct {
			Type   string `yaml:"type"`
			Source string `yaml:"source"`
			Target string `yaml:"target"`
		}
		_ = node.Decode(&vol)
		return []VolumeInfo{classifyVolumeLong(vol.Type, vol.Source, vol.Target, topExternal)}
	case yaml.SequenceNode:
		var out []VolumeInfo
		for i := 0; i < len(node.Content); i++ {
			out = append(out, classifyVolumeNode(*node.Content[i], topExternal)...)
		}
		return out
	}
	return nil
}

// classifyVolumeSpec 短语法 "[source:]target[:mode]" 或命名/匿名卷。
func classifyVolumeSpec(spec string, topExternal map[string]bool) VolumeInfo {
	spec = strings.TrimSpace(spec)
	parts := strings.SplitN(spec, ":", 3)
	var source, target string
	switch len(parts) {
	case 1:
		target = parts[0] // 匿名卷（仅容器路径）
	default:
		source, target = parts[0], parts[1]
	}
	return classifyVolumeLong("", source, target, topExternal)
}

// classifyVolumeLong 按 type/source 判定卷类别。
func classifyVolumeLong(vtype, source, target string, topExternal map[string]bool) VolumeInfo {
	vi := VolumeInfo{Target: target}
	if strings.HasSuffix(strings.ToLower(source), "docker.sock") {
		vi.Kind = VolumeSocket
		vi.Source = source
		return vi // 特权 socket（安装期应已阻断；防御性标识）
	}
	if vtype == "bind" || (vtype == "" && looksLikeAbsPath(source)) {
		vi.Kind = VolumeBind
		vi.Source = source
		return vi
	}
	if source == "" {
		vi.Kind = VolumeManaged
		vi.Managed = true
		vi.Deletable = true
		return vi // 匿名卷（受管）
	}
	if topExternal[source] {
		vi.Kind = VolumeExternal
		vi.Source = source
		vi.External = true
		return vi
	}
	vi.Kind = VolumeManaged
	vi.Source = source
	vi.Managed = true
	vi.Deletable = true
	return vi // 命名卷（受管）
}

// parseEnvKeys 从 .env 文本提取 key 集合（不保留值）。
func parseEnvKeys(envFile string) map[string]bool {
	keys := map[string]bool{}
	for _, line := range strings.Split(envFile, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.IndexByte(line, '=')
		if idx <= 0 {
			continue
		}
		if k := strings.TrimSpace(line[:idx]); k != "" {
			keys[k] = true
		}
	}
	return keys
}

// envRefRe 匹配 compose 中的 ${VAR} / ${VAR:-default} / ${VAR:?required} 插值引用。
var envRefRe = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(?::[-?][^}]*)?\}`)

// envReferences 收集应用「使用」的环境变量：${VAR} 插值 + service.environment 的 key。
func envReferences(composeYAML string, root invRoot) map[string]bool {
	refs := map[string]bool{}
	for _, m := range envRefRe.FindAllStringSubmatch(composeYAML, -1) {
		if len(m) > 1 {
			refs[m[1]] = true
		}
	}
	for _, svc := range root.Services {
		switch svc.Environment.Kind {
		case yaml.MappingNode:
			for i := 0; i+1 < len(svc.Environment.Content); i += 2 {
				if k := svc.Environment.Content[i].Value; k != "" {
					refs[k] = true
				}
			}
		case yaml.SequenceNode:
			for i := 0; i < len(svc.Environment.Content); i++ {
				s := svc.Environment.Content[i].Value
				if k := strings.TrimSpace(strings.SplitN(s, "=", 2)[0]); k != "" {
					refs[k] = true
				}
			}
		}
	}
	return refs
}

// buildEnvInventory 合并 configured ∪ used，标注 configured/type/required（无值）。
func buildEnvInventory(configured, used map[string]bool) []EnvVarInfo {
	all := map[string]bool{}
	for k := range configured {
		all[k] = true
	}
	for k := range used {
		all[k] = true
	}
	keys := make([]string, 0, len(all))
	for k := range all {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]EnvVarInfo, 0, len(keys))
	for _, k := range keys {
		typ := "text"
		if isSecretyKey(k) {
			typ = "password"
		}
		out = append(out, EnvVarInfo{
			Key:        k,
			Configured: configured[k],
			Type:       typ,
			Required:   used[k] && !configured[k],
		})
	}
	return out
}
