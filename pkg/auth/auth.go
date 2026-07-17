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

	mu       sync.RWMutex
	sessions map[string]time.Time // token → 过期时间
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

// Verify 验证密码，成功返回 session token
func (a *Auth) Verify(password string) (string, bool) {
	if !a.Enabled() {
		return "", true
	}
	password = strings.TrimSpace(password)
	if password != a.password {
		return "", false
	}

	token := generateToken()
	a.mu.Lock()
	a.sessions[token] = time.Now().Add(a.sessionTTL)
	a.mu.Unlock()

	return token, true
}

// ValidateToken 验证 session token。支持裸 token 和 "Bearer xxx" 两种形式。
func (a *Auth) ValidateToken(token string) bool {
	if !a.Enabled() {
		return true
	}
	token = strings.TrimSpace(strings.TrimPrefix(token, "Bearer "))
	if token == "" {
		return false
	}
	a.mu.RLock()
	expiry, ok := a.sessions[token]
	a.mu.RUnlock()

	if !ok {
		return false
	}
	if time.Now().After(expiry) {
		a.mu.Lock()
		delete(a.sessions, token)
		a.mu.Unlock()
		return false
	}
	return true
}

// Middleware HTTP 中间件，检查 Authorization header
func (a *Auth) Middleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a.Enabled() {
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
