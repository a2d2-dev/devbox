package auth

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Config 认证配置
type Config struct {
	Password   string `mapstructure:"password"`
	SessionTTL int    `mapstructure:"session_ttl"` // 秒
}

// Auth 认证管理器
type Auth struct {
	password   string
	sessionTTL time.Duration

	mu               sync.RWMutex
	sessions         map[string]time.Time // token → 过期时间
	onSessionRemoved func(string)
}

// New 创建认证管理器
func New(cfg Config) *Auth {
	ttl := time.Duration(cfg.SessionTTL) * time.Second
	if ttl == 0 {
		ttl = time.Hour
	}
	return &Auth{
		password:   strings.TrimSpace(cfg.Password),
		sessionTTL: ttl,
		sessions:   make(map[string]time.Time),
	}
}

// Enabled 是否启用认证（密码为空则不需要）
func (a *Auth) Enabled() bool {
	return a.password != ""
}

// VerifyPassword checks only the configured password and never creates a session.
func (a *Auth) VerifyPassword(password string) bool {
	if !a.Enabled() {
		return true
	}
	password = strings.TrimSpace(password)
	return password == a.password
}

// NewSession creates a token after every configured authentication factor succeeds.
func (a *Auth) NewSession() string {
	a.PruneExpired()
	token := generateToken()
	a.mu.Lock()
	a.sessions[token] = time.Now().Add(a.sessionTTL)
	a.mu.Unlock()
	return token
}

// Verify is retained for callers that only use password authentication.
func (a *Auth) Verify(password string) (string, bool) {
	if !a.VerifyPassword(password) {
		return "", false
	}
	return a.NewSession(), true
}

// ValidateToken 验证 session token。支持裸 token 和 "Bearer xxx" 两种形式。
func (a *Auth) ValidateToken(token string) bool {
	token = strings.TrimSpace(strings.TrimPrefix(token, "Bearer "))
	if token == "" {
		return false
	}
	a.PruneExpired()
	a.mu.RLock()
	expiry, ok := a.sessions[token]
	a.mu.RUnlock()

	if !ok {
		return false
	}
	if time.Now().After(expiry) {
		a.removeSession(token)
		return false
	}
	return true
}

// SetSessionRemovedHook aligns session cleanup with the hook introduced by #18.
func (a *Auth) SetSessionRemovedHook(hook func(string)) {
	a.mu.Lock()
	a.onSessionRemoved = hook
	a.mu.Unlock()
}

// RevokeToken removes a session token. It accepts bare and Bearer forms.
func (a *Auth) RevokeToken(token string) bool {
	token = strings.TrimSpace(strings.TrimPrefix(token, "Bearer "))
	if token == "" {
		return false
	}
	return a.removeSession(token)
}

// PruneExpired removes all expired sessions, including tokens that are never reused.
func (a *Auth) PruneExpired() {
	now := time.Now()
	a.mu.Lock()
	removed := make([]string, 0)
	for token, expiry := range a.sessions {
		if !now.Before(expiry) {
			delete(a.sessions, token)
			removed = append(removed, token)
		}
	}
	hook := a.onSessionRemoved
	a.mu.Unlock()
	if hook != nil {
		for _, token := range removed {
			hook(token)
		}
	}
}

func (a *Auth) removeSession(token string) bool {
	a.mu.Lock()
	_, existed := a.sessions[token]
	delete(a.sessions, token)
	hook := a.onSessionRemoved
	a.mu.Unlock()
	if existed && hook != nil {
		hook(token)
	}
	return existed
}

// Middleware HTTP 中间件，检查 Authorization header
func (a *Auth) Middleware(next http.HandlerFunc, additionalRequired ...func() bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		required := a.Enabled()
		for _, check := range additionalRequired {
			required = required || (check != nil && check())
		}
		if !required {
			next(w, r)
			return
		}

		token := r.Header.Get("Authorization")
		if token == "" {
			token = r.URL.Query().Get("token")
		}

		if !a.ValidateToken(token) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":"unauthorized","message":"身份验证失败"}`))
			return
		}

		next(w, r)
	}
}

func generateToken() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}
