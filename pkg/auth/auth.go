package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/a2d2-dev/devbox/pkg/users"
)

type Config struct {
	Password        string `mapstructure:"password"`
	SessionTTL      int    `mapstructure:"session_ttl"`
	Users           *users.Store
	UsersConfigured bool
}

type Principal struct {
	UserID      string     `json:"userId,omitempty"`
	Username    string     `json:"username"`
	DisplayName string     `json:"displayName"`
	Role        users.Role `json:"role"`
	Legacy      bool       `json:"legacy,omitempty"`
}

func (p Principal) IsAdmin() bool { return p.Role == users.RoleAdmin }

type session struct {
	expires   time.Time
	principal Principal
}

type Auth struct {
	password        string
	sessionTTL      time.Duration
	users           *users.Store
	usersConfigured bool
	mu              sync.RWMutex
	sessions        map[string]session
}

func New(cfg Config) *Auth {
	ttl := time.Duration(cfg.SessionTTL) * time.Second
	if ttl == 0 {
		ttl = time.Hour
	}
	return &Auth{password: strings.TrimSpace(cfg.Password), sessionTTL: ttl, users: cfg.Users, usersConfigured: cfg.UsersConfigured || cfg.Users != nil, sessions: make(map[string]session)}
}

func (a *Auth) Enabled() bool {
	enabled, _ := a.state()
	return enabled
}

func (a *Auth) Available() bool {
	_, available := a.state()
	return available
}

func (a *Auth) state() (enabled, available bool) {
	if a.users == nil {
		if a.usersConfigured {
			return true, false
		}
		return a.password != "", true
	}
	n, err := a.users.Count(context.Background())
	if err != nil {
		return true, false
	}
	return a.password != "" || n > 0, true
}

// Verify preserves the legacy password-only API.
func (a *Auth) Verify(password string) (string, bool) {
	token, _, ok := a.VerifyCredentials("", password)
	return token, ok
}

func (a *Auth) VerifyCredentials(username, password string) (string, Principal, bool) {
	enabled, available := a.state()
	if !available {
		return "", Principal{}, false
	}
	username = strings.TrimSpace(username)
	if a.users != nil && username != "" {
		if u, ok := a.users.Authenticate(context.Background(), username, password); ok {
			p := Principal{UserID: u.ID, Username: u.Username, DisplayName: u.DisplayName, Role: u.Role}
			return a.issue(p), p, true
		}
	}
	// Before the first database user exists, keep the configured single-password
	// administrator behavior. Once users exist, only the explicit legacy admin name
	// may use it, preventing a mistyped user password from escalating privileges.
	allowLegacy := a.password != "" && strings.TrimSpace(password) == a.password
	if allowLegacy && a.users != nil {
		n, err := a.users.Count(context.Background())
		allowLegacy = err == nil && (n == 0 || username == "" || strings.EqualFold(username, "admin"))
	}
	if allowLegacy {
		if username == "" {
			username = "admin"
		}
		p := Principal{Username: username, DisplayName: username, Role: users.RoleAdmin, Legacy: true}
		return a.issue(p), p, true
	}
	if !enabled {
		p := Principal{Username: "local", DisplayName: "Local user", Role: users.RoleAdmin, Legacy: true}
		return "", p, true
	}
	return "", Principal{}, false
}

func (a *Auth) issue(p Principal) string {
	token := generateToken()
	a.mu.Lock()
	a.sessions[token] = session{expires: time.Now().Add(a.sessionTTL), principal: p}
	a.mu.Unlock()
	return token
}

func normalizeToken(token string) string {
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(token), "Bearer "))
}

func (a *Auth) Principal(token string) (Principal, bool) {
	enabled, available := a.state()
	if !available {
		return Principal{}, false
	}
	if !enabled {
		return Principal{Username: "local", DisplayName: "Local user", Role: users.RoleAdmin, Legacy: true}, true
	}
	token = normalizeToken(token)
	if token == "" {
		return Principal{}, false
	}
	a.mu.RLock()
	sess, ok := a.sessions[token]
	a.mu.RUnlock()
	if !ok {
		return Principal{}, false
	}
	if time.Now().After(sess.expires) {
		a.mu.Lock()
		delete(a.sessions, token)
		a.mu.Unlock()
		return Principal{}, false
	}
	return sess.principal, true
}

func (a *Auth) ValidateToken(token string) bool { _, ok := a.Principal(token); return ok }

func (a *Auth) RevokeUser(userID string) {
	if userID == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for token, sess := range a.sessions {
		if sess.principal.UserID == userID {
			delete(a.sessions, token)
		}
	}
}

type principalKey struct{}

func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(Principal)
	return p, ok
}

func tokenFromRequest(r *http.Request) string {
	token := r.Header.Get("Authorization")
	if token == "" {
		token = r.URL.Query().Get("token")
	}
	return token
}

func (a *Auth) Middleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a.Available() {
			writeDenied(w, http.StatusServiceUnavailable, "user_database_unavailable", "用户数据库不可用，认证服务已关闭访问")
			return
		}
		p, ok := a.Principal(tokenFromRequest(r))
		if !ok {
			writeDenied(w, http.StatusUnauthorized, "unauthorized", "身份验证失败")
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), principalKey{}, p)))
	}
}

func (a *Auth) RequireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a.Available() {
			writeDenied(w, http.StatusServiceUnavailable, "user_database_unavailable", "用户数据库不可用，认证服务已关闭访问")
			return
		}
		p, ok := PrincipalFromContext(r.Context())
		if !ok {
			p, ok = a.Principal(tokenFromRequest(r))
		}
		if !ok {
			writeDenied(w, http.StatusUnauthorized, "unauthorized", "身份验证失败")
			return
		}
		if !p.IsAdmin() {
			writeDenied(w, http.StatusForbidden, "forbidden", "需要管理员权限")
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), principalKey{}, p)))
	}
}

func writeDenied(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`{"error":"` + code + `","message":"` + message + `"}`))
}

func generateToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
