package console

import (
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
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
		s.jsonOK(w, map[string]interface{}{
			"authenticated": true,
			"token":         "",
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
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"authenticated": false,
			"message":       "密码错误",
		})
		return
	}
	s.loginLimiter.clear(key)

	s.jsonOK(w, map[string]interface{}{
		"authenticated": true,
		"token":         token,
		"user":          principal,
		"message":       "验证成功",
	})
}

func (s *Server) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
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
