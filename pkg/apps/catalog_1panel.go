package apps

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// 原生 1Panel 应用商店适配（Issue #2 / PR #3）。
//
// 1Panel 官方仓库 1Panel-dev/appstore 是「源即运行时」格式（无构建步骤）：
//
//	appstore/
//	  data.yaml                  # 商店名 + 分类标签（i18n）
//	  apps/<app-key>/
//	    data.yml                 # 应用元数据（name/key/tags/desc/type/links/arch）
//	    logo.png, README*.md
//	    <version>/
//	      data.yml               # additionalProperties.formFields[] 表单 schema
//	      docker-compose.yml     # 引用 ${formField} 与 ${CONTAINER_NAME}，加入 1panel-network
//	      scripts/init.sh        # 宿主初始化脚本（devbox 绝不执行）
//
// 本文件实现「1Panel 元数据 → devbox StoreApp/StoreAppVersion」的纯转换；clone/walk
// 与 Catalog 接口实现在同文件后半段。转换遵循官方 spec：
// https://github.com/1Panel-dev/1Panel-appstore-skills/blob/main/references/appstore-format.md
//
// 安全（用户裁决 + Issue secret 规则）：
//   - password 字段仍在 schema（前端展示密码框，required 由 splitValues 强制），
//     但其上游 default 一律丢弃——不进 DefaultValues / 响应 / revision / audit。
//   - 1panel-network(external) 收敛为 project-managed（去 external/name，保留 key 与
//     服务引用），不创建全局共享网络；其它 external network → 视为依赖未知外部服务，报错。
//   - container_name/${CONTAINER_NAME} 移除（devbox 按 devbox-<id> project 管理）。
//   - scripts/init.sh 永不执行；存在即保守标记该 version 不可安装（不解析 shell）。

// --- 1Panel YAML 类型 ---

type onePanelAppData struct {
	Name                 string           `yaml:"name"`
	Tags                 []string         `yaml:"tags"`
	Title                string           `yaml:"title"`
	Description          string           `yaml:"description"`
	AdditionalProperties onePanelAppProps `yaml:"additionalProperties"`
}

type onePanelAppProps struct {
	Key           string            `yaml:"key"`
	Name          string            `yaml:"name"`
	Tags          []string          `yaml:"tags"`
	ShortDescZh   string            `yaml:"shortDescZh"`
	ShortDescEn   string            `yaml:"shortDescEn"`
	Description   map[string]string `yaml:"description"`
	Type          string            `yaml:"type"`
	Website       string            `yaml:"website"`
	GitHub        string            `yaml:"github"`
	Document      string            `yaml:"document"`
	Architectures []string          `yaml:"architectures"`
}

type onePanelVersionData struct {
	AdditionalProperties onePanelVersionProps `yaml:"additionalProperties"`
}

type onePanelVersionProps struct {
	FormFields []onePanelFormField `yaml:"formFields"`
}

type onePanelFormField struct {
	EnvKey   string                 `yaml:"envKey"`
	Type     string                 `yaml:"type"`
	Required bool                   `yaml:"required"`
	Default  any                    `yaml:"default"`
	LabelZh  string                 `yaml:"labelZh"`
	LabelEn  string                 `yaml:"labelEn"`
	Label    map[string]string      `yaml:"label"`
	Rule     string                 `yaml:"rule"`
	Values   []onePanelSelectOption `yaml:"values"` // select 选项
}

type onePanelSelectOption struct {
	Label string `yaml:"label"`
	Value string `yaml:"value"`
}

// --- formFields → devbox valuesSchema + defaults ---

// onePanelFieldsToSchema 把 1Panel 版本级 formFields 转为 devbox valuesSchema JSON
// 与默认值 map。返回：
//   - schemaJSON：{"version":"v1","fields":[{key,type,label,required,options}]}，
//     类型映射 text/number/password/select(须带 options)/boolean；未知类型 → error。
//   - defaults：非 password 字段的上游 default（JSON 编码）；password default 一律丢弃。
func onePanelFieldsToSchema(fields []onePanelFormField) (json.RawMessage, map[string]json.RawMessage, error) {
	outFields := make([]storeValueField, 0, len(fields))
	defaults := map[string]json.RawMessage{}
	declared := map[string]string{} // envKey → type，供 compose 变量转换用
	for _, f := range fields {
		envKey := strings.TrimSpace(f.EnvKey)
		if envKey == "" {
			continue
		}
		typ, err := mapOnePanelFieldType(f.Type)
		if err != nil {
			return nil, nil, fmt.Errorf("字段 %s: %w", envKey, err)
		}
		if typ == "select" && len(f.Values) == 0 {
			return nil, nil, fmt.Errorf("字段 %s: select 缺少 values 选项", envKey)
		}
		if _, dup := declared[envKey]; dup {
			return nil, nil, fmt.Errorf("字段 %s: 重复声明", envKey)
		}
		declared[envKey] = typ

		sf := storeValueField{
			Key:      envKey,
			Type:     typ,
			Label:    onePanelLabelMap(f),
			Required: f.Required,
		}
		if typ == "select" {
			for _, v := range f.Values {
				sf.Options = append(sf.Options, storeSelectOption{Label: v.Label, Value: v.Value})
			}
		}
		if typ == "password" {
			// 上游非空默认值已被丢弃 → 强制 required，要求用户重新填写；
			// 否则上游 required=false 会默默传空导致运行失败。无默认值且上游 optional 时保留 false。
			if _, hadDefault := onePanelDefaultJSON(f.Default); hadDefault {
				sf.Required = true
			}
		}
		outFields = append(outFields, sf)

		// password default 永不回传（Issue secret 不回显 + 明文敏感默认值阻断）。
		if typ != "password" {
			if d, ok := onePanelDefaultJSON(f.Default); ok {
				defaults[envKey] = d
			}
		}
	}
	schema := storeValuesSchema{Version: "v1", Fields: outFields}
	b, err := json.Marshal(schema)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal valuesSchema: %w", err)
	}
	return b, defaults, nil
}

// mapOnePanelFieldType 把 1Panel 字段类型映射为 devbox schema 类型；未知 → error（不猜）。
func mapOnePanelFieldType(t string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "", "text":
		return "text", nil
	case "number":
		return "number", nil
	case "password":
		return "password", nil
	case "select":
		return "select", nil
	case "bool", "boolean":
		return "boolean", nil
	default:
		return "", fmt.Errorf("不支持的字段类型 %q", t)
	}
}

func onePanelLabelMap(f onePanelFormField) map[string]string {
	zh, en := f.LabelZh, f.LabelEn
	if zh == "" && f.Label != nil {
		zh = f.Label["zh"]
	}
	if en == "" && f.Label != nil {
		en = f.Label["en"]
	}
	if strings.TrimSpace(zh) == "" && strings.TrimSpace(en) == "" {
		return nil
	}
	return map[string]string{"zh": zh, "en": en}
}

// onePanelDefaultJSON 把上游 default 编码为 JSON RawMessage；空值/无效 → ok=false。
func onePanelDefaultJSON(def any) (json.RawMessage, bool) {
	if def == nil {
		return nil, false
	}
	if s, ok := def.(string); ok && strings.TrimSpace(s) == "" {
		return nil, false
	}
	b, err := json.Marshal(def)
	if err != nil {
		return nil, false
	}
	return b, true
}

// onePanelFieldTypeIndex 从 formFields 构建 envKey → type 映射（compose 变量转换用）。
func onePanelFieldTypeIndex(fields []onePanelFormField) map[string]string {
	out := map[string]string{}
	for _, f := range fields {
		if k := strings.TrimSpace(f.EnvKey); k != "" {
			if t, err := mapOnePanelFieldType(f.Type); err == nil {
				out[k] = t
			}
		}
	}
	return out
}

// --- docker-compose.yml 安全收敛 ---

// onePanelVarRefRe 匹配 compose 中的 ${VAR} 变量引用。
var onePanelVarRefRe = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// sanitizeOnePanelCompose 把 1Panel docker-compose.yml 转为 devbox 可渲染模板：
//   - services[*].container_name 移除（含 ${CONTAINER_NAME} 与派生 ${CONTAINER_NAME}-svc）。
//   - networks.1panel-network(external) 收敛为 project-managed：去 external/name，保留 key
//     与所有服务的 networks 引用，让 Compose 创建 <project>_1panel-network（多 service 互通）。
//     存在 1panel-network 以外的 external network → 报错（依赖未知外部服务，不猜）。
//   - 声明的非秘密字段 ${VAR} → {{ .VAR }}（devbox Go 模板渲染入 compose body）；
//     password 字段保留 ${VAR}（由 docker compose 从 .env 展开，值不进渲染/revision）。
//
// fieldTypes 来自 formFields（envKey → devbox 类型）。
func sanitizeOnePanelCompose(raw string, fieldTypes map[string]string) (string, error) {
	var root map[string]any
	if err := yaml.Unmarshal([]byte(raw), &root); err != nil {
		return "", fmt.Errorf("解析 docker-compose.yml: %w", err)
	}
	if root == nil {
		return "", fmt.Errorf("docker-compose.yml 为空或非映射")
	}

	// 1. 剥离 container_name（所有服务）。
	if services, ok := root["services"].(map[string]any); ok {
		for _, svc := range services {
			if m, ok := svc.(map[string]any); ok {
				delete(m, "container_name")
			}
		}
	}

	// 2. 网络收敛。
	if nets, ok := root["networks"].(map[string]any); ok {
		for name, def := range nets {
			m, ok := def.(map[string]any)
			if !ok {
				continue
			}
			if ext, _ := m["external"].(bool); ext {
				if name == onePanelSharedNetwork {
					delete(m, "external")
					delete(m, "name") // → Compose 建 <project>_1panel-network
				} else {
					return "", fmt.Errorf("compose 声明了 1panel-network 以外的外部网络 %q：devbox 单机无法提供其依赖的外部服务", name)
				}
			}
		}
	}

	// 3. 变量引用转换（非秘密字段 → Go 模板）。
	convertOnePanelVarRefs(root, fieldTypes)

	out, err := yaml.Marshal(root)
	if err != nil {
		return "", fmt.Errorf("重新序列化 docker-compose.yml: %w", err)
	}
	return string(out), nil
}

// onePanelSharedNetwork 是 1Panel compose 中所有服务加入的外部网络 key。
const onePanelSharedNetwork = "1panel-network"

// convertOnePanelVarRefs 递归把字符串标量中声明的非秘密 ${VAR} 替换为 {{ .VAR }}。
// password 与未声明变量保持原样。
func convertOnePanelVarRefs(node any, fieldTypes map[string]string) {
	switch v := node.(type) {
	case map[string]any:
		for k, val := range v {
			if s, ok := val.(string); ok {
				v[k] = convertOnePanelVarString(s, fieldTypes)
			} else {
				convertOnePanelVarRefs(val, fieldTypes)
			}
		}
	case []any:
		for i, val := range v {
			if s, ok := val.(string); ok {
				v[i] = convertOnePanelVarString(s, fieldTypes)
			} else {
				convertOnePanelVarRefs(val, fieldTypes)
			}
		}
	}
}

func convertOnePanelVarString(s string, fieldTypes map[string]string) string {
	if !strings.Contains(s, "${") {
		return s
	}
	return onePanelVarRefRe.ReplaceAllStringFunc(s, func(m string) string {
		name := m[2 : len(m)-1] // 去掉 ${ }
		typ, known := fieldTypes[name]
		if !known || typ == "password" {
			return m // 未声明（如 CONTAINER_NAME，随 container_name 剥离）或密码：原样
		}
		return "{{ ." + name + " }}"
	})
}

// --- sanitize 后残留变量校验 ---

var (
	// onePanelBraceVarRe 匹配 ${...}（含 modifier 形式 ${VAR:-x}/${VAR:?x}/${VAR:=x} 等）。
	onePanelBraceVarRe = regexp.MustCompile(`\$\{([^}]*)\}`)
	// onePanelBareVarRe 匹配 bare $IDENT（docker compose 也按环境变量插值）。
	onePanelBareVarRe = regexp.MustCompile(`\$([A-Za-z_][A-Za-z0-9_]*)`)
	// onePanelNameRe 从 ${...} 内提取前导变量名。
	onePanelNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*`)
)

// passwordFieldSet 返回 formFields 中 password 类型字段的集合（这些 ${VAR} 由 .env 提供，允许残留）。
func passwordFieldSet(fields []onePanelFormField) map[string]bool {
	out := map[string]bool{}
	for _, f := range fields {
		if t, err := mapOnePanelFieldType(f.Type); err == nil && t == "password" {
			if k := strings.TrimSpace(f.EnvKey); k != "" {
				out[k] = true
			}
		}
	}
	return out
}

// residualOnePanelVars 返回 sanitize 后仍残留、且非声明密码字段的 compose 变量名（排序、去重）。
//   - 声明密码字段 ${PW} 允许（由受管 .env 提供）。
//   - modifier 形式 ${VAR:-x}/${VAR:?x}/... 一律标记为不安全（名后缀 '!'），即使 VAR 是声明
//     密码——可能内嵌默认值绕过 .env。
//   - bare $IDENT 同样按变量处理；$$ 先中和（compose 转义美元）。
func residualOnePanelVars(compose string, passwordFields map[string]bool) []string {
	scan := strings.ReplaceAll(compose, "$$", "\x00") // 中和转义美元，避免 $$VAR 误判
	seen := map[string]bool{}
	var out []string
	add := func(name string) {
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		out = append(out, name)
	}
	for _, m := range onePanelBraceVarRe.FindAllStringSubmatch(scan, -1) {
		inner := strings.TrimSpace(m[1])
		name := onePanelNameRe.FindString(inner)
		if name == "" {
			add("{" + inner + "}!")
			continue
		}
		if len(name) != len(inner) {
			add(name + "!") // modifier 形式
			continue
		}
		if !passwordFields[name] {
			add(name)
		}
	}
	for _, m := range onePanelBareVarRe.FindAllStringSubmatch(scan, -1) {
		if !passwordFields[m[1]] {
			add(m[1])
		}
	}
	sort.Strings(out)
	return out
}

// onePanelComposeMutableImages 从原始 compose 提取 services.*.image，返回使用可变标签
// (latest/main/master/edge/nightly，含无 tag 默认 latest) 的镜像（排序去重）。
// focused 提取于原始 YAML，不走 AnalyzeCompose（Go 模板占位符会使 YAML 解析歧义）；
// Controller.Apply 仍是最终风险裁决，此处为适配层兼容性预检。
func onePanelComposeMutableImages(raw string) []string {
	var root map[string]any
	if err := yaml.Unmarshal([]byte(raw), &root); err != nil {
		return nil
	}
	services, ok := root["services"].(map[string]any)
	if !ok {
		return nil
	}
	seen := map[string]bool{}
	var bad []string
	for _, svc := range services {
		m, ok := svc.(map[string]any)
		if !ok {
			continue
		}
		img, _ := m["image"].(string)
		if strings.TrimSpace(img) == "" {
			continue
		}
		if isLatestOrMainTag(img) && !seen[img] {
			seen[img] = true
			bad = append(bad, img)
		}
	}
	sort.Strings(bad)
	return bad
}

// --- 解析 1Panel 仓库目录 ---

// onepanelParsedApp 一个 1Panel 应用的解析结果（多版本）。
type onepanelParsedApp struct {
	key      string
	meta     onePanelAppData
	versions []onepanelParsedVersion
}

// onepanelParsedVersion 单个版本的解析结果。compose 为原始 docker-compose.yml（未收敛）。
type onepanelParsedVersion struct {
	version   string
	fields    []onePanelFormField
	compose   string // 原始 docker-compose.yml
	hasScript bool   // scripts/init.sh 存在（来自 ls-tree，未拉 blob）
}

// parseOnePanelRepo 解析已 clone 的 1Panel 仓库根目录。
// pathSet 来自 `git ls-tree -r --name-only HEAD`（用于 scripts 存在性探测，不依赖工作树）。
// 返回 apps map 与可选的商店名（root data.yaml.name）。
func parseOnePanelRepo(root string, pathSet []string) (map[string]*onepanelParsedApp, string, error) {
	storeName := ""
	if b, err := safeReadCatalogFile(root, "data.yaml", maxCatalogFileBytes); err == nil {
		var cm struct {
			Name string `yaml:"name"`
		}
		if yaml.Unmarshal(b, &cm) == nil {
			storeName = strings.TrimSpace(cm.Name)
		}
	}
	pathLookup := make(map[string]bool, len(pathSet))
	for _, p := range pathSet {
		pathLookup[strings.TrimSpace(p)] = true
	}

	appsDir := filepath.Join(root, "apps")
	entries, err := os.ReadDir(appsDir)
	if err != nil {
		return nil, storeName, fmt.Errorf("读 apps 目录失败: %w", err)
	}
	apps := map[string]*onepanelParsedApp{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		key := e.Name()
		appDir := filepath.Join(appsDir, key)
		metaBody, err := safeReadCatalogFile(appDir, "data.yml", maxCatalogFileBytes)
		if err != nil {
			continue // 无 app 元数据 → 跳过（非受支持 app 目录）
		}
		var meta onePanelAppData
		if err := yaml.Unmarshal(metaBody, &meta); err != nil {
			continue
		}
		pa := &onepanelParsedApp{key: key, meta: meta}
		vers, err := os.ReadDir(appDir)
		if err != nil {
			continue
		}
		for _, v := range vers {
			if !v.IsDir() {
				continue
			}
			verName := v.Name()
			verDir := filepath.Join(appDir, verName)
			fields, err := parseOnePanelVersionFields(verDir)
			if err != nil {
				continue
			}
			composeBody, err := safeReadCatalogFile(verDir, "docker-compose.yml", maxCatalogFileBytes)
			if err != nil || strings.TrimSpace(string(composeBody)) == "" {
				continue
			}
			relScript := "apps/" + key + "/" + verName + "/scripts/init.sh"
			pa.versions = append(pa.versions, onepanelParsedVersion{
				version:   verName,
				fields:    fields,
				compose:   string(composeBody),
				hasScript: pathLookup[relScript],
			})
		}
		if len(pa.versions) > 0 {
			apps[key] = pa
		}
	}
	return apps, storeName, nil
}

// parseOnePanelVersionFields 读版本目录 data.yml 的 formFields；缺失/解析失败 → nil。
func parseOnePanelVersionFields(verDir string) ([]onePanelFormField, error) {
	body, err := safeReadCatalogFile(verDir, "data.yml", maxCatalogFileBytes)
	if err != nil {
		return nil, err
	}
	var vd onePanelVersionData
	if err := yaml.Unmarshal(body, &vd); err != nil {
		return nil, err
	}
	return vd.AdditionalProperties.FormFields, nil
}

// detectOnePanel 按 1Panel 官方特征判定仓库是否为受支持格式：
// 根 data.yaml 存在 且 apps/ 下至少一个 <key>/data.yml。
// pooneyy 类无 root data.yaml 的 fork → 判定不支持（不为它放宽猜测）。
func detectOnePanel(root string, pathSet []string) bool {
	if _, err := safeReadCatalogFile(root, "data.yaml", maxCatalogFileBytes); err != nil {
		return false
	}
	for _, p := range pathSet {
		// apps/<key>/data.yml
		if strings.HasPrefix(p, "apps/") && strings.HasSuffix(p, "/data.yml") &&
			strings.Count(p, "/") == 2 {
			return true
		}
	}
	return false
}

// latestOnePanelVersion 返回语义最大版本（复用 compareVersionStrings）。
func latestOnePanelVersion(versions []onepanelParsedVersion) (onepanelParsedVersion, bool) {
	var best onepanelParsedVersion
	found := false
	for _, v := range versions {
		if !found || compareVersionStrings(v.version, best.version) > 0 {
			best = v
			found = true
		}
	}
	return best, found
}

// findOnePanelVersion 精确匹配 version；为空则取最大版本。
func findOnePanelVersion(versions []onepanelParsedVersion, version string) (onepanelParsedVersion, bool) {
	want := strings.TrimSpace(version)
	if want == "" {
		return latestOnePanelVersion(versions)
	}
	for _, v := range versions {
		if v.version == want {
			return v, true
		}
	}
	return onepanelParsedVersion{}, false
}

// onePanelVersionInstallable 返回该版本是否可安装及不可安装原因。
func onePanelVersionInstallable(v onepanelParsedVersion) (bool, string) {
	if strings.TrimSpace(v.compose) == "" {
		return false, "该版本未提供 docker-compose.yml"
	}
	if v.hasScript {
		return false, "上游提供宿主初始化脚本 scripts/init.sh，devbox 不执行上游脚本；数据目录权限可能不足"
	}
	return true, ""
}

// resolveOnePanelVersion 对单版本执行完整转换校验（compose/script → sanitize → 残留变量 → schema）。
// 返回（收敛后 compose, schema, defaults, 可安装, 原因）。不可安装时 compose/schema 为零。
// Snapshot 与 GetVersion 共用此函数，保证列表与详情的可安装性判定一致（不先假阳性 installable）。
// compose/schema 仅供后端持有，Snapshot 调用方丢弃它们（不进 StoreApp）。
func resolveOnePanelVersion(v onepanelParsedVersion) (string, json.RawMessage, map[string]json.RawMessage, bool, string) {
	if ok, reason := onePanelVersionInstallable(v); !ok {
		return "", nil, nil, false, reason
	}
	if bad := onePanelComposeMutableImages(v.compose); len(bad) > 0 {
		return "", nil, nil, false, "使用可变镜像标签（禁止 latest/main/master/edge/nightly）: " + strings.Join(bad, ", ")
	}
	compose, err := sanitizeOnePanelCompose(v.compose, onePanelFieldTypeIndex(v.fields))
	if err != nil {
		return "", nil, nil, false, "compose 转换失败: " + err.Error()
	}
	if residuals := residualOnePanelVars(compose, passwordFieldSet(v.fields)); len(residuals) > 0 {
		return "", nil, nil, false, "compose 引用未声明/不安全的变量（无法安全填充）: " + strings.Join(residuals, ", ")
	}
	schema, defaults, err := onePanelFieldsToSchema(v.fields)
	if err != nil {
		return "", nil, nil, false, "参数 schema 转换失败: " + err.Error()
	}
	return compose, schema, defaults, true, ""
}

// --- partial + sparse clone 引擎 ---

// onePanelSparsePatterns non-cone sparse-checkout：只取 metadata + compose（不拉 logo/截图/脚本）。
var onePanelSparsePatterns = []string{
	"/data.yaml",
	"/apps/*/data.yml",
	"/apps/*/*/data.yml",
	"/apps/*/*/docker-compose.yml",
}

const (
	maxLsTreeBytes = 2 << 20 // 2 MiB：ls-tree 输出上限
	maxLsTreePaths = 200000  // ls-tree 路径数上限（防恶意 repo）
)

// gitRunner 执行一条 git 命令（argv/stdin，返回脱敏输出），可注入便于测试 filter-ignored 场景。
type gitRunner func(ctx context.Context, gitBin string, args []string, token string, stdin []byte) (string, error)

// defaultSparseGitRunner 生产用真实 git 执行器。
var defaultSparseGitRunner gitRunner = runSparseGit

// filterIgnoredRe 检测服务端忽略 partial clone filter（git 可能 exit 0 但回退完整 clone）。
// 典型输出：warning: filtering not recognized by server, ignoring
var filterIgnoredRe = regexp.MustCompile(`(?i)filter(?:ing)? not recognized`)

// cloneFilterIgnored 判定 clone 输出是否表明服务端忽略了 --filter。
func cloneFilterIgnored(output string) bool { return filterIgnoredRe.MatchString(output) }

// gitSparseClone 用 partial(blobless)+sparse clone 仅取 1Panel metadata/compose。
// ref 为空 → 不带 --branch，clone remote HEAD（1Panel 官方默认 dev）。
// runner 为 nil 时用 defaultSparseGitRunner（真实 git）。
// 检测服务端忽略 --filter（exit 0 但回退完整 clone）→ 失败（不突破带宽限额，不回退完整 clone）。
func gitSparseClone(ctx context.Context, gitBin, repoURL, ref, destDir, token string, runner gitRunner) error {
	if gitBin == "" {
		gitBin = "git"
	}
	if runner == nil {
		runner = defaultSparseGitRunner
	}
	// 1) clone --depth 1 --filter=blob:none --no-checkout [--branch ref]
	args := []string{"-c", "credential.helper=", "-c", "core.hooksPath=/dev/null"}
	if parsed, err := url.Parse(repoURL); err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") {
		args = append(args,
			"-c", "protocol.file.allow=never",
			"-c", "protocol.ssh.allow=never",
			"-c", "protocol.git.allow=never",
			"-c", "protocol.ext.allow=never",
		)
	}
	args = append(args, "clone", "--depth", "1", "--filter", "blob:none", "--no-checkout")
	if ref != "" {
		args = append(args, "--branch", ref)
	}
	args = append(args, "--", repoURL, destDir)
	out, err := runner(ctx, gitBin, args, token, nil)
	if err != nil {
		return fmt.Errorf("partial clone 失败（确认 Git 服务端支持 --filter=blob:none）: %s: %w", out, err)
	}
	if cloneFilterIgnored(out) {
		return fmt.Errorf("Git 服务端不支持 partial clone（--filter 被忽略，将回退完整 clone）；devbox 不接受完整 clone 以控制带宽")
	}
	// 2) sparse-checkout set --no-cone --stdin（patterns 经 stdin，命令 argv，不经 shell）。
	patterns := strings.Join(onePanelSparsePatterns, "\n") + "\n"
	setArgs := []string{"-C", destDir, "-c", "core.hooksPath=/dev/null", "sparse-checkout", "set", "--no-cone", "--stdin"}
	if out, err := runner(ctx, gitBin, setArgs, token, []byte(patterns)); err != nil {
		return fmt.Errorf("sparse-checkout set 失败: %s: %w", out, err)
	}
	// 3) checkout（按 sparse 集合填充工作树，仅拉所需 blob）。
	coArgs := []string{"-C", destDir, "-c", "core.hooksPath=/dev/null", "checkout"}
	if out, err := runner(ctx, gitBin, coArgs, token, nil); err != nil {
		return fmt.Errorf("sparse checkout 失败: %s: %w", out, err)
	}
	return nil
}

// runSparseGit 执行 git 命令（argv，不经 shell），注入只读 token 并裁剪/脱敏输出。
func runSparseGit(ctx context.Context, gitBin string, args []string, token string, stdin []byte) (string, error) {
	cmd := exec.CommandContext(ctx, gitBin, args...)
	cmd.Env = sparseGitEnv(token)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	out := &capWriter{max: gitOutputCap}
	cmd.Stdout = out
	cmd.Stderr = out
	err := cmd.Run()
	s := scrubToken(out.String(), token)
	if out.truncated {
		s += "...(truncated)"
	}
	return s, err
}

// sparseGitEnv 构造隔离的 git 进程环境：剥离继承的 GIT_*、禁交互/系统配置、注入只读 token。
func sparseGitEnv(token string) []string {
	env := append(sanitizedGitEnv(os.Environ()),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_ATTR_NOSYSTEM=1",
	)
	if token != "" {
		env = append(env,
			"GIT_CONFIG_COUNT=1",
			"GIT_CONFIG_KEY_0=http.extraHeader",
			"GIT_CONFIG_VALUE_0=Authorization: Bearer "+token,
		)
	}
	return env
}

// gitListTreePaths 从 partial clone 已有 tree object 列出全部路径（不拉额外 blob），
// 用于 scripts/init.sh 存在性探测。输出限制字节/路径数防恶意 repo。
func gitListTreePaths(ctx context.Context, gitBin, destDir, token string) ([]string, error) {
	if gitBin == "" {
		gitBin = "git"
	}
	args := []string{"-C", destDir, "-c", "core.hooksPath=/dev/null", "ls-tree", "-r", "--name-only", "HEAD"}
	cmd := exec.CommandContext(ctx, gitBin, args...)
	cmd.Env = sparseGitEnv(token)
	out := &capWriter{max: maxLsTreeBytes}
	cmd.Stdout = out
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ls-tree: %w", err)
	}
	if out.truncated {
		return nil, fmt.Errorf("ls-tree 输出超过 %d 字节上限（疑似异常仓库）", maxLsTreeBytes)
	}
	body := strings.TrimRight(out.String(), "\n")
	if body == "" {
		return nil, nil
	}
	paths := strings.Split(body, "\n")
	if len(paths) > maxLsTreePaths {
		return nil, fmt.Errorf("ls-tree 路径数超过 %d 上限（疑似异常仓库）", maxLsTreePaths)
	}
	return paths, nil
}

// --- onepanelCatalog: Catalog 接口实现 ---

type onepanelCatalog struct {
	id       string
	name     string
	source   CatalogSource
	gitBin   string
	cacheDir string
	runner   gitRunner

	refreshMu sync.Mutex
	mu        sync.RWMutex
	apps      map[string]*onepanelParsedApp
	storeName string
	fetchedAt *time.Time
	lastErr   string
}

// newOnePanelCatalog 构造原生 1Panel catalog source（复用 git 校验 + last-good 缓存根）。
func newOnePanelCatalog(src CatalogSource, cacheRoot string) (*onepanelCatalog, error) {
	u := strings.TrimSpace(src.URL)
	if u == "" {
		return nil, fmt.Errorf("1panel catalog 缺 url（须完整 https 仓库地址）")
	}
	if err := validateGitURL(u); err != nil {
		return nil, err
	}
	sub, err := cleanCatalogSubdir(src.Path)
	if err != nil {
		return nil, err
	}
	if sub != "" {
		return nil, fmt.Errorf("1panel 暂不支持仓库子目录 path（请指向含 apps/ 的仓库根）")
	}
	id := sourceID(src, "1panel:"+u)
	var dir string
	if cacheRoot != "" {
		// URL/ref 纳入 cache namespace：同 ID 更新远端时，旧 generation 的后台
		// refresh 不会覆盖新来源的 last-good 事实源。
		dir = filepath.Join(cacheRoot, "1panel-"+shortHash(id+"\x00"+u+"\x00"+strings.TrimSpace(src.Ref)))
	} else {
		dir = filepath.Join(os.TempDir(), "devbox-catalog-1panel-"+shortHash(id+"\x00"+u+"\x00"+strings.TrimSpace(src.Ref)))
	}
	c := &onepanelCatalog{
		id:       id,
		name:     src.Name,
		source:   src,
		gitBin:   "git",
		cacheDir: dir,
		runner:   defaultSparseGitRunner,
	}
	_ = recoverCatalogBackup(c.cacheDir)
	_ = c.loadCached()
	return c, nil
}

func (c *onepanelCatalog) ID() string   { return c.id }
func (c *onepanelCatalog) Kind() string { return "1panel" }

func (c *onepanelCatalog) displayName() string {
	if v := c.name; v != "" {
		return v
	}
	if c.storeName != "" {
		return c.storeName
	}
	return c.id
}

func (c *onepanelCatalog) setErr(msg string) {
	c.mu.Lock()
	c.lastErr = truncateMsg(msg, 512)
	c.mu.Unlock()
}

// loadCached 从已有 cacheDir（上次 clone）重新解析，用于重启后离线 last-good。
func (c *onepanelCatalog) loadCached() error {
	root, err := catalogRootWithin(c.cacheDir, c.source.Path)
	if err != nil {
		return err
	}
	paths, err := gitListTreePaths(context.Background(), c.gitBin, c.cacheDir, c.source.Token)
	if err != nil {
		paths = nil
	}
	apps, storeName, err := parseOnePanelRepo(root, paths)
	if err != nil {
		return fmt.Errorf("load cached: %w", err)
	}
	if len(apps) == 0 {
		return fmt.Errorf("load cached: 缓存中没有可识别应用")
	}
	info, _ := os.Stat(filepath.Join(root, "data.yaml"))
	c.mu.Lock()
	c.apps = apps
	c.storeName = storeName
	if info != nil {
		t := info.ModTime().UTC()
		c.fetchedAt = &t
	}
	c.mu.Unlock()
	return nil
}

func (c *onepanelCatalog) Refresh(ctx context.Context) error {
	c.refreshMu.Lock()
	defer c.refreshMu.Unlock()
	_ = recoverCatalogBackup(c.cacheDir)
	c.mu.RLock()
	hasApps := len(c.apps) > 0
	c.mu.RUnlock()
	if !hasApps {
		_ = c.loadCached()
	}

	cloneCtx, cancel := context.WithTimeout(ctx, gitCloneTimeout)
	defer cancel()

	parent := filepath.Dir(c.cacheDir)
	if err := os.MkdirAll(parent, 0o750); err != nil {
		c.setErr("create cache root: " + err.Error())
		return err
	}
	tmp, err := os.MkdirTemp(parent, filepath.Base(c.cacheDir)+".tmp-*")
	if err != nil {
		c.setErr("create clone temp dir: " + err.Error())
		return err
	}
	cleanup := func() { _ = os.RemoveAll(tmp) }

	sub, err := cleanCatalogSubdir(c.source.Path)
	if err != nil {
		cleanup()
		c.setErr(err.Error())
		return err
	}
	ref := strings.TrimSpace(c.source.Ref)
	if err := gitSparseClone(cloneCtx, c.gitBin, c.source.URL, ref, tmp, c.source.Token, c.runner); err != nil {
		cleanup()
		msg := scrubToken(err.Error(), c.source.Token)
		c.setErr(msg)
		return fmt.Errorf("1panel clone %s: %w", redactURL(c.source.URL), err)
	}
	if err := boundCloneSize(tmp, maxCloneTotalBytes); err != nil {
		cleanup()
		c.setErr(err.Error())
		return err
	}
	newRoot, err := catalogRootWithin(tmp, sub)
	if err != nil {
		cleanup()
		c.setErr(err.Error())
		return err
	}
	paths, err := gitListTreePaths(ctx, c.gitBin, tmp, c.source.Token)
	if err != nil {
		cleanup()
		c.setErr(err.Error())
		return err
	}
	if !detectOnePanel(newRoot, paths) {
		cleanup()
		c.setErr("不是受支持的 1Panel 应用商店格式（未检出 root data.yaml + apps/<key>/data.yml）")
		return fmt.Errorf("1panel %s: 不是受支持的格式", redactURL(c.source.URL))
	}
	apps, storeName, err := parseOnePanelRepo(newRoot, paths)
	if err != nil {
		cleanup()
		c.setErr(err.Error())
		return err
	}
	if err := swapDir(tmp, c.cacheDir); err != nil {
		cleanup()
		c.setErr("swap clone dir: " + err.Error())
		return err
	}
	now := time.Now().UTC()
	c.mu.Lock()
	c.apps = apps
	c.storeName = storeName
	c.fetchedAt = &now
	c.lastErr = ""
	c.mu.Unlock()
	return nil
}

func (c *onepanelCatalog) Snapshot() CatalogSnapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()
	snap := CatalogSnapshot{SourceID: c.id, SourceName: c.displayName(), Kind: "1panel"}
	if len(c.apps) == 0 {
		snap.Status = CatalogStatus{State: CatalogStateError, Message: orStr(c.lastErr, "尚未同步")}
		return snap
	}
	appsList := make([]StoreApp, 0, len(c.apps))
	for _, pa := range c.apps {
		latest, ok := latestOnePanelVersion(pa.versions)
		if !ok {
			continue
		}
		sa := onePanelAppToStoreApp(c.id, c.displayName(), pa, latest)
		// 列表层即运行与 GetVersion 相同的完整转换校验，避免先假阳性 installable。
		if _, _, _, installable, reason := resolveOnePanelVersion(latest); !installable {
			sa.Installable = false
			sa.NotInstallableReason = reason
		}
		appsList = append(appsList, sa)
	}
	st := CatalogStatus{State: CatalogStateOK, AppCount: len(appsList), FetchedAt: c.fetchedAt}
	if c.lastErr != "" {
		st.State = CatalogStateError
		st.Message = c.lastErr
	}
	snap.Apps = appsList
	snap.Status = st
	return snap
}

func (c *onepanelCatalog) GetVersion(ctx context.Context, appID, version string) (StoreAppVersion, error) {
	// 在 RLock 内捕获 apps map 指针 + 来源展示名/id；apps/versions 在 Refresh 中以整体替换
	// （不就地改写）发布，故解锁后读取旧 map/版本切片是安全的；转换无共享状态。
	c.mu.RLock()
	apps := c.apps
	srcID := c.id
	srcName := c.displayName()
	c.mu.RUnlock()
	pa, ok := apps[appID]
	if !ok {
		return StoreAppVersion{}, fmt.Errorf("1panel catalog %q 中未找到应用 %q", srcID, appID)
	}
	v, ok := findOnePanelVersion(pa.versions, version)
	if !ok {
		return StoreAppVersion{}, fmt.Errorf("应用 %q 未找到版本 %q", appID, version)
	}
	ver := StoreAppVersion{
		AppID:       appID,
		Version:     v.version,
		Runtime:     RuntimeCompose,
		CatalogID:   srcID,
		CatalogName: srcName,
	}
	compose, schema, defaults, installable, reason := resolveOnePanelVersion(v)
	if !installable {
		ver.Installable = false
		ver.NotInstallableReason = reason
		return ver, nil
	}
	ver.Installable = true
	ver.ComposeTemplate = compose
	ver.ValuesSchema = schema
	ver.DefaultValues = defaults
	return ver, nil
}

// onePanelAppToStoreApp 把 1Panel 应用 + 指定版本映射为 StoreApp（列表用）。
func onePanelAppToStoreApp(srcID, srcName string, pa *onepanelParsedApp, v onepanelParsedVersion) StoreApp {
	meta := pa.meta
	ap := meta.AdditionalProperties
	name := orStr(strings.TrimSpace(ap.Name), orStr(strings.TrimSpace(meta.Name), pa.key))
	desc := orStr(strings.TrimSpace(ap.ShortDescZh), orStr(strings.TrimSpace(ap.ShortDescEn), meta.Description))
	category := ""
	if len(ap.Tags) > 0 {
		category = ap.Tags[0]
	} else if len(meta.Tags) > 0 {
		category = meta.Tags[0]
	}
	return StoreApp{
		ID:           pa.key,
		Name:         name,
		Category:     category,
		Version:      v.version,
		Description:  desc,
		Provider:     orStr(ap.GitHub, ap.Website),
		VersionCount: len(pa.versions),
		Runtime:      RuntimeCompose,
		Runtimes:     []string{string(RuntimeCompose)},
		Installable:  true,
		CatalogID:    srcID,
		CatalogName:  srcName,
		SourceType:   "community",
		TrustLevel:   "unverified",
	}
}
