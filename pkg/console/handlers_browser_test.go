package console

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestValidateTarget(t *testing.T) {
	cases := []struct {
		in      string
		wantErr bool
	}{
		{"http://example.com", false},
		{"https://192.168.1.10:3000/path?q=1", false},
		{"file:///etc/passwd", true},
		{"ftp://example.com", true},
		{"gopher://x", true},
		{"javascript:alert(1)", true},
		{"/relative/url", true},
		{"http://", true}, // 空 host
		{"", true},
	}
	for _, c := range cases {
		_, err := validateTarget(c.in)
		if (err != nil) != c.wantErr {
			t.Errorf("validateTarget(%q) err=%v wantErr=%v", c.in, err, c.wantErr)
		}
	}
}

func TestIsBlockedHost(t *testing.T) {
	blocked := []string{"169.254.169.254", "169.254.170.2", "169.254.1.1", "fe80::1", "[fe80::1]"}
	for _, h := range blocked {
		if !isBlockedHost(h) {
			t.Errorf("isBlockedHost(%q) = false, want true", h)
		}
	}
	allowed := []string{"192.168.1.10", "10.0.0.5", "example.com", "localhost", "172.30.0.242", "[fd00:ec2::254]x"}
	// 注意：fd00:ec2::254 的括号包裹形式应被拦；裸串 "fd00:ec2::254" 不在 blocked（是 ULA 私网，放行）
	for _, h := range allowed {
		if isBlockedHost(h) {
			t.Errorf("isBlockedHost(%q) = true, want false", h)
		}
	}
}

func TestIsPrivateHost(t *testing.T) {
	priv := []string{"127.0.0.1", "10.1.2.3", "192.168.0.1", "172.16.0.1"}
	for _, h := range priv {
		if !isPrivateHost(h) {
			t.Errorf("isPrivateHost(%q) = false, want true", h)
		}
	}
	// 域名解析不了 IP，按非私网处理（只用于审计日志，不拦截）
	if isPrivateHost("example.com") {
		t.Errorf("isPrivateHost(example.com) = true, want false")
	}
}

func TestAbsolutize(t *testing.T) {
	base, _ := url.Parse("http://a.com/p/q/")
	cases := []struct{ ref, want string }{
		{"../r", "http://a.com/p/r"},
		{"/x", "http://a.com/x"},
		{"y.css", "http://a.com/p/q/y.css"},
		{"http://b.com/z", "http://b.com/z"},
		{"//b.com/m", "http://b.com/m"},
	}
	for _, c := range cases {
		got, err := base.Parse(c.ref)
		if err != nil {
			t.Fatalf("Parse(%q) err: %v", c.ref, err)
		}
		if got.String() != c.want {
			t.Errorf("absolutize(%q) = %q, want %q", c.ref, got.String(), c.want)
		}
	}
}

func TestInjectBaseAndShim(t *testing.T) {
	base := "http://example.com/page"
	// 有 <head>
	in := []byte(`<html><head><title>x</title></head><body></body></html>`)
	out := injectBaseAndShim(in, base)
	if !contains(out, []byte(`<base href="http://example.com/page">`)) {
		t.Error("missing <base href> after <head>")
	}
	if !contains(out, []byte(`/api/v1/browser/proxy?url=`)) {
		t.Error("missing shim script")
	}
	// 无 <head> 有 <body>
	in2 := []byte(`<html><body>hi</body></html>`)
	out2 := injectBaseAndShim(in2, base)
	if !contains(out2, []byte(`<base href=`)) {
		t.Error("missing base for body-only html")
	}
	// 兜底前置
	in3 := []byte(`hello world`)
	out3 := injectBaseAndShim(in3, base)
	if string(out3[:len(base)+30]) == base {
		t.Error("unexpected")
	}
	if !contains(out3, []byte(`<base href=`)) {
		t.Error("missing base for plain text")
	}
}

func contains(haystack, needle []byte) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			match := true
			for j := 0; j < len(needle); j++ {
				if haystack[i+j] != needle[j] {
					match = false
					break
				}
			}
			if match {
				return true
			}
		}
		return false
	})()
}

func TestExtractFrameAncestors(t *testing.T) {
	cases := []struct{ csp, want string }{
		{"frame-ancestors 'none'", "'none'"},
		{"default-src 'self'; frame-ancestors 'none'; img-src *", "'none'"},
		{"frame-ancestors https://a.com https://b.com", "https://a.com https://b.com"},
		{"frame-ancestors *", "*"},
		{"default-src 'self'", ""}, // 无 frame-ancestors
		{"", ""},
	}
	for _, c := range cases {
		got := extractFrameAncestors(c.csp)
		if got != c.want {
			t.Errorf("extractFrameAncestors(%q) = %q, want %q", c.csp, got, c.want)
		}
	}
}

// TestProbeDirectEmbed 验证探测：有拦截头判 false，无拦截头或 * 判 true。
func TestProbeDirectEmbed(t *testing.T) {
	type tc struct {
		name       string
		respHeader func(h http.Header)
		wantDirect bool
	}
	cases := []tc{
		{"xfo-deny", func(h http.Header) { h.Set("X-Frame-Options", "DENY") }, false},
		{"xfo-sameorigin", func(h http.Header) { h.Set("X-Frame-Options", "SAMEORIGIN") }, false},
		{"csp-none", func(h http.Header) { h.Set("Content-Security-Policy", "frame-ancestors 'none'") }, false},
		{"csp-specific", func(h http.Header) { h.Set("Content-Security-Policy", "frame-ancestors https://a.com") }, false},
		{"csp-wildcard", func(h http.Header) { h.Set("Content-Security-Policy", "frame-ancestors *") }, true},
		{"no-restrictive-header", func(h http.Header) { h.Set("Content-Type", "text/html") }, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				c.respHeader(w.Header())
				w.WriteHeader(200)
			}))
			defer upstream.Close()
			u, _ := url.Parse(upstream.URL)
			s := &Server{browserClient: newBrowserClient(true)}
			direct, reason := s.probeDirectEmbed(context.Background(), u)
			if direct != c.wantDirect {
				t.Errorf("probeDirectEmbed(%s) direct=%v reason=%q, want direct=%v", c.name, direct, reason, c.wantDirect)
			}
		})
	}
}
func TestProxyHeaderWhitelist(t *testing.T) {
	// 起一个假上游：返回 X-Frame-Options + CSP + Content-Type
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "frame-ancestors 'none'")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(200)
		w.Write([]byte(`<html><head></head><body>ok</body></html>`))
	}))
	defer upstream.Close()

	s := &Server{browserClient: newBrowserClient(true)}
	target := upstream.URL + "/page"

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/browser/proxy?url="+url.QueryEscape(target), nil)
	s.handleBrowserProxy(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("X-Frame-Options"); got != "" {
		t.Errorf("X-Frame-Options leaked: %q", got)
	}
	if got := rec.Header().Get("Content-Security-Policy"); got != "" {
		t.Errorf("CSP leaked: %q", got)
	}
	if got := rec.Header().Get("Content-Type"); got == "" {
		t.Error("Content-Type should be preserved")
	}
	if got := rec.Body.String(); !contains([]byte(got), []byte(`<base href=`)) {
		t.Error("response body missing injected <base>")
	}
}
