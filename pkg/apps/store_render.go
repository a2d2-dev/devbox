package apps

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"text/template"
)

// 商店 Compose 模板的安全渲染层（CEO 裁决第3/7/10条）。
//
// 设计原则：
//  1. 确定性 & 安全：text/template + Option(missingkey=error)，且【不注册任何
//     FuncMap】——没有 call/printf/html 等内置之外的函数可调用，天然禁止任意
//     函数执行；text/template 本身无 shell/file/exec 能力。
//  2. secret 不进 compose 正文：渲染数据只含非敏感 params；secret 字段仅写入
//     .env（0600，不入 revision/audit）。catalog 模板须以 ${KEY} 引用 secret，
//     由 docker compose 在渲染/运行期从 .env 展开。若模板误用 {{ .secret }}，
//     missingkey=error 直接报错拒绝，强制正确约定。
//  3. 校验：对照 valuesSchema 校验 required/type/select 选项，拒绝未声明 key。
//  4. 上限：渲染输出超 maxRenderOutput 视为异常拒绝。

const maxRenderOutput = 1 << 20 // 1 MiB

// storeValuesSchema 本地解析 edge-apiserver valuesSchema（仅取校验所需字段）。
type storeValuesSchema struct {
	Version string            `json:"version"`
	Fields  []storeValueField `json:"fields"`
}

type storeValueField struct {
	Key      string              `json:"key"`
	Type     string              `json:"type"` // text|number|select|boolean|password
	Label    map[string]string   `json:"label"`
	Required bool                `json:"required"`
	Options  []storeSelectOption `json:"options"`
}

type storeSelectOption struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// RenderStoreCompose 是 store install 的「校验 + 渲染」入口：
// 解析 schema → 校验/分离 values → 渲染 compose 模板。
// 返回：渲染后 compose 正文、非敏感 params（进 revision）、secret（仅写 .env）。
func RenderStoreCompose(ver StoreAppVersion, input map[string]any) (compose string, params, secrets map[string]string, err error) {
	if !ver.Installable || ver.Runtime != RuntimeCompose {
		return "", nil, nil, fmt.Errorf("该版本不可本地安装: %s", ver.NotInstallableReason)
	}
	schema, err := parseValuesSchema(ver.ValuesSchema)
	if err != nil {
		return "", nil, nil, err
	}
	params, secrets, err = splitValues(schema, input)
	if err != nil {
		return "", nil, nil, err
	}
	compose, err = renderComposeTemplate(ver.ComposeTemplate, params)
	if err != nil {
		return "", nil, nil, err
	}
	return compose, params, secrets, nil
}

// parseValuesSchema 解析透传的 valuesSchema RawMessage；空返回零值（无字段）。
// 同时做防御性校验：
//   - 每个 field.Key 须形如 ^[A-Za-z_][A-Za-z0-9_]*$（防止 .env key 注入 / 模板逃逸）；
//   - select 字段必须声明 options（否则退化为自由文本，失去约束）。
func parseValuesSchema(raw json.RawMessage) (storeValuesSchema, error) {
	var s storeValuesSchema
	if len(bytes.TrimSpace(raw)) == 0 {
		return s, nil
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		return s, fmt.Errorf("invalid valuesSchema: %w", err)
	}
	for _, f := range s.Fields {
		if !validValueKey(f.Key) {
			return s, fmt.Errorf("valuesSchema 字段 key %q 非法（须 ^[A-Za-z_][A-Za-z0-9_]*$）", f.Key)
		}
		if f.Type == "select" && len(f.Options) == 0 {
			return s, fmt.Errorf("valuesSchema select 字段 %q 缺少 options", f.Key)
		}
	}
	return s, nil
}

// validValueKey 校验参数 key 形如 ^[A-Za-z_][A-Za-z0-9_]*$（与 edge-apiserver ValuesField 一致）。
func validValueKey(k string) bool {
	if k == "" {
		return false
	}
	for i, r := range k {
		isAlpha := (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z')
		isDigit := r >= '0' && r <= '9'
		if i == 0 {
			if !(isAlpha || r == '_') {
				return false
			}
			continue
		}
		if !(isAlpha || isDigit || r == '_') {
			return false
		}
	}
	return true
}

// splitValues 校验用户输入并按字段类型分离：password→secrets，其余→params。
// 拒绝 schema 未声明的 key（防止注入未校验值）。
func splitValues(schema storeValuesSchema, input map[string]any) (params, secrets map[string]string, err error) {
	params = map[string]string{}
	secrets = map[string]string{}
	declared := map[string]bool{}
	for _, f := range schema.Fields {
		declared[f.Key] = true
		raw, present := input[f.Key]
		if !present || isEmptyValue(raw) {
			if f.Required {
				return nil, nil, fmt.Errorf("缺少必填参数: %s", fieldLabel(f))
			}
			continue
		}
		val, verr := coerceValue(f, raw)
		if verr != nil {
			return nil, nil, verr
		}
		if f.Type == "password" {
			secrets[f.Key] = val
		} else {
			params[f.Key] = val
		}
	}
	// 拒绝未声明 key：用户不得提交 schema 之外的参数。
	for k := range input {
		if !declared[k] {
			return nil, nil, fmt.Errorf("未声明的参数: %s", k)
		}
	}
	return params, secrets, nil
}

// coerceValue 按字段类型把用户输入转换/校验为字符串值。
func coerceValue(f storeValueField, raw any) (string, error) {
	switch f.Type {
	case "number":
		switch v := raw.(type) {
		case float64:
			return formatNumber(v), nil
		case int:
			return strconv.Itoa(v), nil
		case string:
			t := strings.TrimSpace(v)
			if n, perr := strconv.ParseFloat(t, 64); perr == nil {
				return formatNumber(n), nil
			}
			return "", fmt.Errorf("%s 要求数字", fieldLabel(f))
		default:
			return "", fmt.Errorf("%s 要求数字", fieldLabel(f))
		}
	case "boolean":
		var b bool
		switch v := raw.(type) {
		case bool:
			b = v
		case string:
			switch strings.ToLower(strings.TrimSpace(v)) {
			case "true", "1":
				b = true
			case "false", "0":
				b = false
			default:
				return "", fmt.Errorf("%s 要求布尔值", fieldLabel(f))
			}
		default:
			return "", fmt.Errorf("%s 要求布尔值", fieldLabel(f))
		}
		if b {
			return "true", nil
		}
		return "false", nil
	case "select":
		s, ok := raw.(string)
		if !ok {
			s = fmt.Sprint(raw)
		}
		if len(f.Options) > 0 {
			valid := false
			for _, o := range f.Options {
				if o.Value == s {
					valid = true
					break
				}
			}
			if !valid {
				return "", fmt.Errorf("%s 的值不在可选项内", fieldLabel(f))
			}
		}
		return s, nil
	case "text", "password", "":
		return toString(raw), nil
	default:
		return "", fmt.Errorf("%s 类型 %q 不支持", fieldLabel(f), f.Type)
	}
}

// renderComposeTemplate 用 text/template 渲染 Compose 模板（仅注入非敏感 params）。
func renderComposeTemplate(tmpl string, params map[string]string) (string, error) {
	if strings.TrimSpace(tmpl) == "" {
		return "", fmt.Errorf("compose 模板为空")
	}
	t, err := template.New("compose").Option("missingkey=error").Parse(tmpl)
	if err != nil {
		return "", fmt.Errorf("模板语法错误: %w", err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, params); err != nil {
		return "", fmt.Errorf("模板渲染失败: %w", err)
	}
	if buf.Len() > maxRenderOutput {
		return "", fmt.Errorf("渲染输出超过 %d 字节上限", maxRenderOutput)
	}
	return buf.String(), nil
}

// --- 辅助 ---

func isEmptyValue(v any) bool {
	if v == nil {
		return true
	}
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s) == ""
	}
	return false
}

func toString(v any) string {
	return strings.TrimSpace(fmt.Sprint(v))
}

func fieldLabel(f storeValueField) string {
	if f.Label != nil {
		if l := strings.TrimSpace(f.Label["zh"]); l != "" {
			return l
		}
		if l := strings.TrimSpace(f.Label["en"]); l != "" {
			return l
		}
	}
	return f.Key
}

// formatNumber 把 JSON number(float64) 规整为最短字符串（8080.0 → "8080"）。
func formatNumber(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}
