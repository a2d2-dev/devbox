package console

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/a2d2-dev/devbox/pkg/auth"
	"github.com/a2d2-dev/devbox/pkg/maintenance"
	"github.com/a2d2-dev/devbox/pkg/security"
	eventlog "github.com/a2d2-dev/devbox/pkg/syslog"
	"go.uber.org/zap"
)

const (
	maxLoginFailures   = 5
	loginFailureWindow = time.Minute
)

type loginAttempt struct {
	failures int
	resetAt  time.Time
}

type loginRateLimiter struct {
	mu       sync.Mutex
	attempts map[string]loginAttempt
}

func (l *loginRateLimiter) blocked(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.attempts == nil {
		l.attempts = make(map[string]loginAttempt)
	}
	a := l.attempts[key]
	if !a.resetAt.IsZero() && !now.Before(a.resetAt) {
		delete(l.attempts, key)
		return false
	}
	return a.failures >= maxLoginFailures
}

func (l *loginRateLimiter) fail(key string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.attempts == nil {
		l.attempts = make(map[string]loginAttempt)
	}
	a := l.attempts[key]
	if a.resetAt.IsZero() || !now.Before(a.resetAt) {
		a = loginAttempt{resetAt: now.Add(loginFailureWindow)}
	}
	a.failures++
	l.attempts[key] = a
}

func (l *loginRateLimiter) clear(key string) {
	l.mu.Lock()
	delete(l.attempts, key)
	l.mu.Unlock()
}

func loginRateKey(r *http.Request, username string) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return host + "\x00" + strings.ToLower(strings.TrimSpace(username))
}

func securitySettings(s *Server) security.Settings {
	if s.security == nil {
		return security.Settings{}
	}
	return s.security.Settings()
}

// registerAuthRoutes 注册认证路由
func (s *Server) registerAuthRoutes() {
	s.mux.HandleFunc("/api/v1/auth/verify", s.handleAuthVerify)
	s.mux.HandleFunc("/api/v1/auth/status", s.handleAuthStatus)
	s.mux.HandleFunc("/api/v1/auth/logout", s.handleAuthLogout)
}

func (s *Server) handleAuthLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	username := "unknown"
	if s.auth != nil {
		if principal, ok := s.auth.SessionPrincipal(r.Header.Get("Authorization")); ok {
			username = principal.Username
		}
	}
	if s.auth == nil || !s.auth.RevokeToken(r.Header.Get("Authorization")) {
		writeJSONErrStatus(w, http.StatusUnauthorized, map[string]any{"error": "身份验证失败", "reason": "unauthorized"})
		return
	}
	s.recordEvent(r, eventlog.Input{
		Level: "info", Module: "auth", Username: username, Event: "退出本地会话", EventType: "LOGOUT", Outcome: "success",
	})
	s.jsonOK(w, map[string]any{"authenticated": false})
}

func (s *Server) installAuthSessionCleanup() {
	if s.auth == nil {
		return
	}
	s.auth.SetSessionRemovedHook(func(token string) {
		s.sessionUsersMu.Lock()
		delete(s.sessionUsers, token)
		s.sessionUsersMu.Unlock()
	})
}

func (s *Server) handleAuthVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Username   string `json:"username"`
		Password   string `json:"password"`
		AccessCode string `json:"accessCode"`
		OTP        string `json:"otp"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	if s.auth == nil || !s.auth.Available() {
		writeJSONErrStatus(w, http.StatusServiceUnavailable, map[string]any{
			"authenticated": false,
			"message":       "认证服务不可用，已关闭访问",
		})
		return
	}
	key := loginRateKey(r, req.Username)
	now := time.Now()
	ip := clientIP(r)
	if s.bans != nil {
		if ban, banned := s.bans.IsBanned(ip); banned {
			w.Header().Set("Retry-After", fmt.Sprintf("%d", max(1, int(time.Until(ban.Until).Seconds()))))
			writeJSONErrStatus(w, http.StatusTooManyRequests, map[string]any{
				"authenticated": false,
				"message":       "来源地址已被临时封禁",
			})
			return
		}
	}
	if s.loginLimiter.blocked(key, now) {
		w.Header().Set("Retry-After", "60")
		writeJSONErrStatus(w, http.StatusTooManyRequests, map[string]any{
			"authenticated": false,
			"message":       "登录失败次数过多，请稍后重试",
		})
		return
	}

	principal, ok := s.auth.AuthenticateCredentials(req.Username, req.Password)
	if !ok {
		s.recordLoginFailure(r, key, ip, now)
		writeJSONErrStatus(w, http.StatusUnauthorized, map[string]any{"authenticated": false, "message": "用户名或密码错误"})
		return
	}

	if s.security != nil && !s.security.VerifyAccessCode(req.AccessCode) {
		s.recordLoginFailure(r, key, ip, now)
		writeJSONErrStatus(w, http.StatusUnauthorized, map[string]any{"authenticated": false, "message": "访问码错误"})
		return
	}
	settings := securitySettings(s)
	if settings.TOTPEnabled {
		if strings.TrimSpace(req.OTP) == "" {
			s.recordLoginFailure(r, key, ip, now)
			writeJSONErrStatus(w, http.StatusUnauthorized, map[string]any{
				"authenticated":     false,
				"twoFactorRequired": true,
				"message":           "请输入动态验证码或恢复码",
			})
			return
		}
		valid, err := s.security.VerifySecondFactor(req.OTP, now)
		if err != nil {
			writeJSONErrStatus(w, http.StatusInternalServerError, map[string]any{"authenticated": false, "message": "保存恢复码消费状态失败"})
			return
		}
		if !valid {
			s.recordLoginFailure(r, key, ip, now)
			writeJSONErrStatus(w, http.StatusUnauthorized, map[string]any{"authenticated": false, "message": "动态验证码或恢复码错误"})
			return
		}
	}

	token := ""
	if s.auth.Enabled() || (s.security != nil && s.security.ProtectionEnabled()) {
		token = s.auth.IssueSession(principal)
	}
	s.loginLimiter.clear(key)
	if s.bans != nil {
		s.bans.SetProtectedIP(ip)
	}
	if token != "" {
		s.sessionUsersMu.Lock()
		if s.sessionUsers == nil {
			s.sessionUsers = make(map[string]string)
		}
		s.sessionUsers[token] = principal.Username
		s.sessionUsersMu.Unlock()
	}
	s.recordEvent(r, eventlog.Input{
		Level: "info", Module: "auth", Username: principal.Username,
		Event: "本地登录成功", EventType: "LOGIN_SUCCESS", Outcome: "success",
	})

	s.jsonOK(w, map[string]interface{}{
		"authenticated": true,
		"token":         token,
		"user":          principal,
		"username":      principal.Username,
		"message":       "验证成功",
	})
}

func (s *Server) recordLoginFailure(r *http.Request, key, ip string, now time.Time) {
	s.loginLimiter.fail(key, now)
	if s.bans != nil {
		s.bans.RecordFailure(ip, "devbox-login")
	}
	s.recordEvent(r, eventlog.Input{
		Level: "warning", Module: "auth", Username: "anonymous",
		Event: "本地登录失败", EventType: "LOGIN_FAILED", Outcome: "failure",
	})
	if s.notifier == nil {
		return
	}
	remote := r.RemoteAddr
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := s.notifier.Notify(ctx, maintenance.Notification{
			Subject: "DevBox 登录失败告警",
			Body:    "控制台检测到一次登录失败。来源：" + remote + "，时间：" + time.Now().Format(time.RFC3339),
		}); err != nil && s.logger != nil {
			s.logger.Warn("Login failure notification failed", zap.Error(err))
		}
	}()
}

func (s *Server) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	settings := securitySettings(s)
	enabled := (s.auth != nil && s.auth.Enabled()) || (s.security != nil && s.security.ProtectionEnabled())
	authenticated := false
	var principal interface{}

	if s.auth != nil {
		token := r.Header.Get("Authorization")
		if token == "" {
			token = r.URL.Query().Get("token")
		}
		var p auth.Principal
		var ok bool
		if enabled {
			p, ok = s.auth.SessionPrincipal(token)
		} else {
			p, ok = s.auth.Principal(token)
		}
		if ok {
			authenticated = true
			principal = p
		}
	}

	s.jsonOK(w, map[string]interface{}{
		"enabled":            enabled,
		"authenticated":      authenticated,
		"passwordRequired":   s.auth != nil && s.auth.Enabled(),
		"accessCodeRequired": settings.AccessCodeEnabled,
		"twoFactorRequired":  settings.TOTPEnabled,
		"user":               principal,
	})
}
