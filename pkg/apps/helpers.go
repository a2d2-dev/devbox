package apps

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// 渲染 .env（含 secret 与插值参数）。文件以 0600 落盘，不进 revision/audit/返回值。
func renderEnvFile(secrets, params map[string]string) string {
	keys := make([]string, 0, len(secrets)+len(params))
	for k := range secrets {
		keys = append(keys, k)
	}
	for k := range params {
		if _, dup := secrets[k]; !dup {
			keys = append(keys, k)
		}
	}
	if len(keys) == 0 {
		return ""
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		v := secrets[k]
		if v == "" {
			v = params[k]
		}
		// 转义：值含换行或特殊字符时用双引号包裹。KEY=VALUE。
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(quoteEnvValue(v))
		b.WriteByte('\n')
	}
	return b.String()
}

// mergeEnvFile 在后端合并要轮换的键；浏览器无需也不能读取原有 secret。
func mergeEnvFile(existing string, secrets, params map[string]string) string {
	merged := parseEnvFile(existing)
	for k, v := range params {
		merged[k] = v
	}
	for k, v := range secrets {
		merged[k] = v
	}
	return renderEnvFile(merged, nil)
}

func parseEnvFile(raw string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.IndexByte(line, '=')
		if idx <= 0 {
			continue
		}
		key, value := strings.TrimSpace(line[:idx]), strings.TrimSpace(line[idx+1:])
		if key == "" {
			continue
		}
		if unquoted, err := strconv.Unquote(value); err == nil {
			value = unquoted
		}
		out[key] = value
	}
	return out
}

func quoteEnvValue(v string) string {
	if strings.ContainsAny(v, " \n\t\"'") {
		return strconv.Quote(v)
	}
	return v
}

// sanitizeSummary 生成脱敏请求摘要（不含 compose 正文、不含 secret）。
func sanitizeSummary(d DesiredApplication) string {
	previews, err := ExtractServicePreviews(d.ComposeContent)
	svcCount := 0
	if err == nil {
		svcCount = len(previews)
	}
	secretCount := len(d.Secrets)
	return fmt.Sprintf("name=%s source=%s services=%d secrets=%d",
		d.Name, d.Source.Kind, svcCount, secretCount)
}

// detectDuplicateHostPorts 检测同一 compose 内重复声明的宿主端口。
// ServicePreview.Ports 元素为短/长语法字符串（如 "8080:80"）。
func detectDuplicateHostPorts(previews []ServicePreview) []string {
	seen := map[string]bool{}
	var dups []string
	for _, p := range previews {
		for _, spec := range p.Ports {
			hp := extractHostPort(spec)
			if hp == "" {
				continue
			}
			if seen[hp] {
				dups = appendIfMissing(dups, hp)
			}
			seen[hp] = true
		}
	}
	return dups
}

func extractHostPort(spec string) string {
	spec = strings.TrimSpace(spec)
	// 短语法 [host:]container[/proto] 或长语法被 nodeString 转成 target。
	if i := strings.IndexByte(spec, ':'); i >= 0 {
		host := strings.TrimSpace(spec[:i])
		// host 可能是 ip:port（两个冒号）。
		if j := strings.LastIndex(host, ":"); j >= 0 {
			host = host[j+1:]
		}
		if _, err := strconv.Atoi(host); err == nil {
			return host
		}
		return ""
	}
	// 无冒号：单端口（仅容器端口，无宿主绑定）→ 不算冲突。
	return ""
}

func appendIfMissing(s []string, v string) []string {
	for _, x := range s {
		if x == v {
			return s
		}
	}
	return append(s, v)
}

func ternary(b bool, t, f string) string {
	if b {
		return t
	}
	return f
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
