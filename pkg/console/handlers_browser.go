package console

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"go.uber.org/zap"
)

// newBrowserClient 构造浏览器代理复用的 HTTP client。
//   - 显式禁用环境代理（HTTP_PROXY/HTTPS_PROXY），内网直连；
//   - insecureTLS=true 时跳过远端证书校验（内网自签证书场景）；
//   - CheckRedirect 限 5 跳，并对每一跳重新走 SSRF 校验。
func newBrowserClient(insecureTLS bool) *http.Client {
	return &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			Proxy:                 nil, // 禁用环境代理
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
			TLSClientConfig:       &tls.Config{InsecureSkipVerify: insecureTLS},
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			// 重定向目标必须同样通过 scheme / SSRF 校验
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return errors.New("redirect scheme not allowed")
			}
			if isBlockedHost(req.URL.Hostname()) {
				return errors.New("redirect to blocked host")
			}
			return nil
		},
	}
}

// registerBrowserRoutes 注册「浏览器」应用相关路由。
//   GET    /api/v1/browser/proxy?url=<encoded>       代理目标 URL，剥离嵌套限制头 + 注入 <base>
//   GET    /api/v1/browser/probe?url=<encoded>       探测目标能否被 iframe 直连（检测 X-Frame-Options/CSP）
//   GET    /api/v1/browser/bookmarks                 列书签
//   POST   /api/v1/browser/bookmarks                 新建书签 {title,url}
//   DELETE /api/v1/browser/bookmarks/{id}            删除书签
//   GET    /api/v1/browser/history                   列历史（最新在前）
//   POST   /api/v1/browser/history                   记录一次访问 {url,title}（去重置顶）
//   DELETE /api/v1/browser/history                   清空历史
//
// 所有路由自动被 authGate 鉴权（/api/v1/* 前缀，见 server.go authGate）。
func (s *Server) registerBrowserRoutes() {
	s.mux.HandleFunc("/api/v1/browser/proxy", s.handleBrowserProxy)
	s.mux.HandleFunc("/api/v1/browser/probe", s.handleBrowserProbe)
	s.mux.HandleFunc("/api/v1/browser/bookmarks", s.handleBookmarks)
	s.mux.HandleFunc("/api/v1/browser/bookmarks/", s.handleBookmarkByID) // 结尾斜杠匹配 {id}
	s.mux.HandleFunc("/api/v1/browser/history", s.handleHistory)
}

// proxyAllowedRespHeaders 响应头白名单：只转发这些，其余一律丢弃。
// 这是剥离 X-Frame-Options / CSP 等嵌套限制头的核心手段——未知限制头默认不转，更安全。
var proxyAllowedRespHeaders = map[string]struct{}{
	"Content-Type":  {},
	"Cache-Control": {},
	"ETag":          {},
	"Last-Modified": {},
	"Accept-Ranges": {},
	"Content-Range": {},
	"Vary":          {},
}

// handleBrowserProxy 抓回目标 URL，剥离嵌套限制头，text/html 注入 <base>，回送给前端 iframe。
func (s *Server) handleBrowserProxy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	raw := r.URL.Query().Get("url")
	if raw == "" {
		http.Error(w, "missing url parameter", http.StatusBadRequest)
		return
	}
	target, err := validateTarget(raw)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if isBlockedHost(target.Hostname()) {
		if s.logger != nil {
			s.logger.Warn("browser proxy blocked host (ssrf guard)",
				zap.String("host", target.Hostname()))
		}
		http.Error(w, "host blocked by ssrf guard", http.StatusForbidden)
		return
	}
	if s.logger != nil && isPrivateHost(target.Hostname()) {
		s.logger.Info("browser proxy private target",
			zap.String("url", target.String()))
	}

	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, target.String(), nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// 用通用 UA + 转发 Accept，避免被反爬直接拒
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; devbox-browser/1.0)")
	req.Header.Set("Accept", r.Header.Get("Accept"))
	if al := r.Header.Get("Accept-Language"); al != "" {
		req.Header.Set("Accept-Language", al)
	}

	resp, err := s.browserClient.Do(req)
	if err != nil {
		http.Error(w, "upstream fetch failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// 白名单复制响应头（X-Frame-Options / CSP / COOP / COEP / CORP / Content-Disposition 等天然被丢弃）
	for h := range proxyAllowedRespHeaders {
		for _, v := range resp.Header.Values(h) {
			w.Header().Add(h, v)
		}
	}
	w.Header().Set("X-Devbox-Proxied", "1")

	ctype := resp.Header.Get("Content-Type")
	// 50MB 上限，防止下载大文件把内存撑爆
	body := io.LimitReader(resp.Body, 50*1024*1024)

	// text/html：注入 <base> 让相对/绝对资源解析回原站；改写后 Content-Length 失准，丢弃让 Go 用 chunked
	if strings.HasPrefix(strings.ToLower(ctype), "text/html") {
		buf, err := io.ReadAll(body)
		if err != nil {
			http.Error(w, "read body failed: "+err.Error(), http.StatusBadGateway)
			return
		}
		// base 用最终（重定向后）URL：resp.Request.URL 指向最后一跳
		baseURL := target.String()
		if resp.Request != nil && resp.Request.URL != nil {
			baseURL = resp.Request.URL.String()
		}
		out := injectBaseAndShim(buf, baseURL)
		w.Header().Del("Content-Length")
		w.Header().Del("Content-Encoding")
		w.WriteHeader(resp.StatusCode)
		w.Write(out)
		return
	}

	// 非 HTML（图片/CSS/JS/字体/JSON…）：直接流式转发
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, body)
}

// handleBrowserProbe 探测目标 URL 能否被 iframe 直接嵌套（不走代理）。
// 前端导航前先调这个：能直连就 iframe 直连（快、无副作用），检测到拦截头才走代理。
// 等价于"先直连，不通走代理"——只是把"检测是否通"放后端做（前端被同源策略限制做不到）。
//
// 判定规则（保守，覆盖绝大多数情况）：
//   - 有 X-Frame-Options（DENY/SAMEORIGIN/ALLOW-FROM）→ 需代理（SAMEORIGIN 几乎不可能含 console origin）
//   - CSP 含 frame-ancestors 且不含通配 * → 需代理
//   - 其余 → 可直连
//
// 任何网络错误/超时 → 返回 direct=true（让 iframe 直连，由浏览器自然报错，和输错地址一样）。
func (s *Server) handleBrowserProbe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	raw := r.URL.Query().Get("url")
	if raw == "" {
		http.Error(w, "missing url parameter", http.StatusBadRequest)
		return
	}
	target, err := validateTarget(raw)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if isBlockedHost(target.Hostname()) {
		// 元数据地址：既不直连也不代理
		s.jsonOK(w, map[string]any{"direct": false, "reason": "blocked"})
		return
	}

	direct, reason := s.probeDirectEmbed(r.Context(), target)
	s.jsonOK(w, map[string]any{"direct": direct, "reason": reason})
}

// probeDirectEmbed 发 HEAD（失败回退 GET，均不读 body）检查响应头是否禁止 iframe 嵌套。
func (s *Server) probeDirectEmbed(parent context.Context, target *url.URL) (bool, string) {
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()

	check := func(method string) (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, method, target.String(), nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; devbox-browser-probe/1.0)")
		return s.browserClient.Do(req)
	}

	resp, err := check(http.MethodHead)
	if err != nil || resp.StatusCode >= 400 {
		// HEAD 被拒/不支持（常见 405）→ 回退 GET 只为拿 header
		if resp != nil {
			resp.Body.Close()
		}
		resp, err = check(http.MethodGet)
	}
	if err != nil {
		// 目标不可达/超时：交回前端直连，由浏览器自然报错
		return true, "unreachable"
	}
	defer resp.Body.Close()
	// 不读 body，立即丢弃（GET 回退时避免拉取整个页面）
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1))

	hdr := resp.Header
	if xo := hdr.Get("X-Frame-Options"); xo != "" {
		return false, "x-frame-options: " + xo
	}
	for _, csp := range hdr.Values("Content-Security-Policy") {
		if fa := extractFrameAncestors(csp); fa != "" && !strings.Contains(fa, "*") {
			return false, "frame-ancestors: " + fa
		}
	}
	return true, "ok"
}

// extractFrameAncestors 从一条 CSP 字符串里提取 frame-ancestors 指令的值串（含 'none'）。
// 找不到返回空串。
func extractFrameAncestors(csp string) string {
	// 按分号拆指令
	for _, directive := range strings.Split(csp, ";") {
		d := strings.TrimSpace(directive)
		fields := strings.Fields(d)
		if len(fields) >= 1 && fields[0] == "frame-ancestors" {
			return strings.Join(fields[1:], " ")
		}
	}
	return ""
}

// validateTarget 校验目标 URL：只允许 http/https scheme（禁 file:// ftp:// gopher://），要求 host 非空。
func validateTarget(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, errors.New("invalid url")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, errors.New("scheme not allowed (only http/https)")
	}
	if u.Host == "" {
		return nil, errors.New("host required")
	}
	return u, nil
}

// isBlockedHost 拒绝云元数据 link-local 地址，防 SSRF 盗云凭据。
// 注意：私网 IP（10/172.16-31/192.168）和内网域名不拦——需求要支持内网服务。
// 不做 DNS 解析（否则内网 hostname 会被解析失败），只在 hostname 字面量层面拦 link-local。
func isBlockedHost(host string) bool {
	h := strings.TrimSpace(host)
	// 括号包裹的 IPv6 link-local
	trimmed := strings.TrimPrefix(strings.TrimSuffix(h, "]"), "[")
	switch {
	case strings.HasPrefix(h, "169.254."):
		return true
	case strings.HasPrefix(trimmed, "fe80:"):
		return true
	case h == "169.254.169.254" || h == "169.254.170.2":
		return true
	case trimmed == "fd00:ec2::254":
		return true
	}
	return false
}

// isPrivateHost 判断 hostname 是否私网/回环 IP 字面量（仅用于审计日志，不拦截）。
func isPrivateHost(host string) bool {
	ip := net.ParseIP(strings.Trim(host, "[]"))
	if ip == nil {
		return false // 域名，无法判断，按非私网处理
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()
}

// browserShim 注入到被代理 HTML 的脚本：patch fetch / XHR.open，把跨源请求改写到代理，
// 改善 SPA 的动态 fetch（同源请求直连保留）。
const browserShim = `<script>(function(){
var P="/api/v1/browser/proxy?url=";
function wrap(u){try{var a=new URL(u,document.baseURI);if(a.origin!==location.origin)return P+encodeURIComponent(a.href);}catch(e){}return u;}
var of=window.fetch;if(of)window.fetch=function(i,o){if(typeof i==="string")i=wrap(i);else if(i&&i.url)i=new Request(wrap(i.url),i);return of.call(this,i,o);};
var oo=XMLHttpRequest.prototype.open;XMLHttpRequest.prototype.open=function(m,u){return oo.call(this,m,wrap(u));};
})();</script>`

// injectBaseAndShim 在 HTML 的 <head> 后注入 <base href> + shim；
// 找不到 <head> 则插到 <body> 后，都没有就前置到开头。
func injectBaseAndShim(buf []byte, baseURL string) []byte {
	baseTag := []byte(`<base href="` + attrEscape(baseURL) + `">`)
	insert := append(baseTag, []byte(browserShim)...)
	insertAt := func(marker string) (int, bool) {
		idx := bytes.Index(buf, []byte(marker))
		if idx < 0 {
			return 0, false
		}
		rel := bytes.IndexByte(buf[idx:], '>')
		if rel < 0 {
			return 0, false
		}
		return idx + rel + 1, true
	}
	for _, m := range []string{"<head", "<body", "<html"} {
		if at, ok := insertAt(m); ok {
			out := make([]byte, 0, len(buf)+len(insert))
			out = append(out, buf[:at]...)
			out = append(out, insert...)
			out = append(out, buf[at:]...)
			return out
		}
	}
	return append(insert, buf...)
}

// attrEscape 转义 URL 以安全嵌入 HTML 属性值（仅转义 & 和引号）。
func attrEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, `"`, "&#34;")
	return s
}

// ─── Bookmarks CRUD ──────────────────────────────────────────────

func (s *Server) handleBookmarks(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.jsonOK(w, s.browser.ListBookmarks())
	case http.MethodPost:
		var req struct {
			Title string `json:"title"`
			URL   string `json:"url"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if req.URL == "" {
			http.Error(w, "url required", http.StatusBadRequest)
			return
		}
		bm, err := s.browser.AddBookmark(req.Title, req.URL)
		if err != nil {
			http.Error(w, "save bookmark failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		s.jsonOK(w, bm)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleBookmarkByID 处理 /api/v1/browser/bookmarks/{id}（目前仅 DELETE）。
func (s *Server) handleBookmarkByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/browser/bookmarks/")
	id = strings.Trim(id, "/")
	if id == "" {
		http.Error(w, "bookmark id required", http.StatusBadRequest)
		return
	}
	switch r.Method {
	case http.MethodDelete:
		if err := s.browser.RemoveBookmark(id); err != nil {
			http.Error(w, "remove bookmark failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		s.jsonOK(w, map[string]string{"status": "ok"})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleHistory 处理历史的 GET / POST / DELETE。
func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.jsonOK(w, s.browser.ListHistory())
	case http.MethodPost:
		var req struct {
			URL   string `json:"url"`
			Title string `json:"title"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if req.URL == "" {
			http.Error(w, "url required", http.StatusBadRequest)
			return
		}
		if err := s.browser.AddHistory(req.URL, req.Title); err != nil {
			http.Error(w, "save history failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		s.jsonOK(w, map[string]string{"status": "ok"})
	case http.MethodDelete:
		if err := s.browser.ClearHistory(); err != nil {
			http.Error(w, "clear history failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		s.jsonOK(w, map[string]string{"status": "ok"})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
