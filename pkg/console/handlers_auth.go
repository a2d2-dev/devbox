package console

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/a2d2-dev/devbox/pkg/maintenance"
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
		if principal, ok := s.auth.Principal(r.Header.Get("Authorization")); ok {
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
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	if s.auth == nil || !s.auth.Enabled() {
		s.recordEvent(r, eventlog.Input{
			Level: "info", Module: "auth", Username: "admin",
			Event: "本地登录成功", EventType: "LOGIN_SUCCESS", Outcome: "success",
		})
		s.jsonOK(w, map[string]interface{}{
			"authenticated": true,
			"token":         "",
			"username":      "admin",
			"message":       "认证未启用",
		})
		return
	}
	if !s.auth.Available() {
		writeJSONErrStatus(w, http.StatusServiceUnavailable, map[string]any{
			"authenticated": false,
			"message":       "用户数据库不可用，认证服务已关闭访问",
		})
		return
	}
	key := loginRateKey(r, req.Username)
	now := time.Now()
	if s.loginLimiter.blocked(key, now) {
		w.Header().Set("Retry-After", "60")
		writeJSONErrStatus(w, http.StatusTooManyRequests, map[string]any{
			"authenticated": false,
			"message":       "登录失败次数过多，请稍后重试",
		})
		return
	}

	token, principal, ok := s.auth.VerifyCredentials(req.Username, req.Password)
	if !ok {
		s.loginLimiter.fail(key, now)
		s.recordEvent(r, eventlog.Input{
			Level: "warning", Module: "auth", Username: "anonymous",
			Event: "本地登录失败", EventType: "LOGIN_FAILED", Outcome: "failure",
		})
		if s.notifier != nil {
			remote := r.RemoteAddr
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				if err := s.notifier.Notify(ctx, maintenance.Notification{
					Subject: "DevBox 登录失败告警",
					Body:    "控制台检测到一次登录失败。来源：" + remote + "，时间：" + time.Now().Format(time.RFC3339),
				}); err != nil {
					s.logger.Warn("Login failure notification failed", zap.Error(err))
				}
			}()
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"authenticated": false,
			"message":       "密码错误",
		})
		return
	}
	s.loginLimiter.clear(key)
	s.sessionUsersMu.Lock()
	if s.sessionUsers == nil {
		s.sessionUsers = make(map[string]string)
	}
	s.sessionUsers[token] = principal.Username
	s.sessionUsersMu.Unlock()
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

func (s *Server) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	enabled := s.auth != nil && s.auth.Enabled()
	authenticated := false
	var principal interface{}

	if s.auth != nil {
		token := r.Header.Get("Authorization")
		if token == "" {
			token = r.URL.Query().Get("token")
		}
		if p, ok := s.auth.Principal(token); ok {
			authenticated = true
			principal = p
		}
	}

	s.jsonOK(w, map[string]interface{}{
		"enabled":       enabled,
		"authenticated": authenticated,
		"user":          principal,
	})
}
