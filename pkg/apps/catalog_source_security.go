package apps

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strings"
)

// SSRF 防护：动态（DB/UI）catalog source 端点校验（Issue #2 / PR #3）。
//
// 启动 YAML 配置来源由运维显式管理，可保留内网/localhost 行为（既有 validateGitURL）。
// 但经认证 UI 创建的动态来源 + /sources/test 必须拒绝指向内网/元数据的端点，
// 否则认证用户可借 devbox 探测/克隆内网服务（SSRF）。
//
// 规则：仅 HTTPS；host 非本地字面量（localhost/.localhost/.internal/.local）；
// 解析后任一 IP 不得为 loopback/private(RFC1918+RFC4193)/link-local(含云元数据
// 169.254.169.254)/unspecified/multicast。动态端点不可设 Insecure/bypass。
//
// 残留风险（记录于 docs/research）：Git CLI 不支持 DNS pinning，校验与 clone 之间
// 理论上存在 DNS-rebinding 窗口；本实现至少在 create/test 时阻断字面量与已解析的
// 非公网地址，clone 仍由既有大小/协议/token 防护兜底。

// hostResolver 解析主机名为 IP 列表（可注入便于测试）。
type hostResolver func(ctx context.Context, host string) ([]net.IP, error)

var defaultHostResolver hostResolver = func(ctx context.Context, host string) ([]net.IP, error) {
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	out := make([]net.IP, 0, len(addrs))
	for _, a := range addrs {
		out = append(out, a.IP)
	}
	return out, nil
}

// validateDynamicCatalogURL 校验动态来源 URL（DB/UI 创建 + /sources/test 用）。
// 返回 nil 表示通过；否则返回脱敏错误（不含 URL 全文，避免日志回显 token/path）。
func validateDynamicCatalogURL(ctx context.Context, rawURL string, resolve hostResolver) error {
	if resolve == nil {
		resolve = defaultHostResolver
	}
	p, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return fmt.Errorf("invalid url")
	}
	if p.Scheme != "https" {
		return fmt.Errorf("动态来源仅允许 https（禁止 http/insecure）")
	}
	host := p.Hostname()
	if host == "" {
		return fmt.Errorf("url 缺少 host")
	}
	if p.User != nil || p.RawQuery != "" || p.Fragment != "" {
		return fmt.Errorf("动态来源 url 禁止 userinfo/query/fragment；请使用 token 配置")
	}
	if isForbiddenHostLiteral(host) {
		return fmt.Errorf("host 为禁止的本地/内部地址")
	}
	ips, err := resolve(ctx, host)
	if err != nil {
		return fmt.Errorf("解析 host 失败")
	}
	if len(ips) == 0 {
		return fmt.Errorf("解析 host 无结果（无法确认公网）")
	}
	for _, ip := range ips {
		if isDisallowedIP(ip) {
			return fmt.Errorf("host 解析到非公网地址")
		}
	}
	return nil
}

// isForbiddenHostLiteral 拒绝 localhost/.localhost/.internal/.local 字面量及 IP 字面量（按 IP 规则）。
func isForbiddenHostLiteral(host string) bool {
	h := strings.ToLower(strings.TrimSuffix(host, "."))
	switch {
	case h == "localhost" || strings.HasSuffix(h, ".localhost"):
		return true
	case h == "internal" || strings.HasSuffix(h, ".internal"):
		return true
	case h == "local" || strings.HasSuffix(h, ".local"):
		return true
	}
	if ip := net.ParseIP(h); ip != nil {
		return isDisallowedIP(ip)
	}
	return false
}

// isDisallowedIP 判定 IP 是否为非公网：
// loopback/private(RFC1918+RFC4193)/link-local(含 169.254 云元数据)/
// link-local-multicast/unspecified/multicast。
func isDisallowedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast()
}
