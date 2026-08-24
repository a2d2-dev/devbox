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
	password         string
	sessionTTL       time.Duration
	users            *users.Store
	usersConfigured  bool
	mu               sync.RWMutex
	sessions         map[string]session
	onSessionRemoved func(string)
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

// AuthenticateCredentials validates the configured user or legacy password
// without creating a session. Callers can complete additional factors before
// issuing a token with IssueSession.
func (a *Auth) AuthenticateCredentials(username, password string) (Principal, bool) {
	enabled, available := a.state()
	if !available {
		return Principal{}, false
	}
	username = strings.TrimSpace(username)
	if a.users != nil && username != "" {
		if u, ok := a.users.Authenticate(context.Background(), username, password); ok {
			p := Principal{UserID: u.ID, Username: u.Username, DisplayName: u.DisplayName, Role: u.Role}
			return p, true
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
		p := Principal{Username: "admin", DisplayName: "admin", Role: users.RoleAdmin, Legacy: true}
		return p, true
	}
	if !enabled {
		p := Principal{Username: "local", DisplayName: "Local user", Role: users.RoleAdmin, Legacy: true}
		return p, true
	}
	return Principal{}, false
}

func (a *Auth) VerifyCredentials(username, password string) (string, Principal, bool) {
	p, ok := a.AuthenticateCredentials(username, password)
	if !ok {
		return "", Principal{}, false
	}
	if !a.Enabled() {
		return "", p, true
	}
	return a.IssueSession(p), p, true
}

// IssueSession creates a session after all configured authentication factors
// have succeeded.
func (a *Auth) IssueSession(p Principal) string {
	a.PruneExpired()
	token := generateToken()
	a.mu.Lock()
	a.sessions[token] = session{expires: time.Now().Add(a.sessionTTL), principal: p}
	a.mu.Unlock()
	return token
}

// NewSession preserves the original single-password session helper.
func (a *Auth) NewSession() string {
	return a.IssueSession(Principal{Username: "admin", DisplayName: "admin", Role: users.RoleAdmin, Legacy: true})
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
	return a.SessionPrincipal(token)
}

// SessionPrincipal requires a real, unexpired session even when password auth
// itself is disabled. Security factors use this to protect the API.
func (a *Auth) SessionPrincipal(token string) (Principal, bool) {
	if !a.Available() {
		return Principal{}, false
	}
	token = normalizeToken(token)
	if token == "" {
		return Principal{}, false
	}
	a.PruneExpired()
	a.mu.RLock()
	sess, ok := a.sessions[token]
	a.mu.RUnlock()
	if !ok {
		return Principal{}, false
	}
	return sess.principal, true
}

func (a *Auth) ValidateToken(token string) bool { _, ok := a.Principal(token); return ok }

func (a *Auth) RevokeUser(userID string) {
	if userID == "" {
		return
	}
	a.mu.RLock()
	tokens := make([]string, 0)
	for token, sess := range a.sessions {
		if sess.principal.UserID == userID {
			tokens = append(tokens, token)
		}
	}
	a.mu.RUnlock()
	for _, token := range tokens {
		a.removeSession(token)
	}
}

// RevokeUserSessionsExcept revokes every session belonging to username except
// the session identified by keepToken. Matching is by principal username so it
// works for both database users and the legacy single-password admin (whose
// UserID is empty). It returns the number of sessions revoked. keepToken is
// normalized so bare and Bearer forms both match the caller's current session.
func (a *Auth) RevokeUserSessionsExcept(username, keepToken string) int {
	username = strings.TrimSpace(username)
	if username == "" {
		return 0
	}
	keepToken = normalizeToken(keepToken)
	a.mu.RLock()
	tokens := make([]string, 0)
	for token, sess := range a.sessions {
		if token == keepToken {
			continue
		}
		if strings.EqualFold(sess.principal.Username, username) {
			tokens = append(tokens, token)
		}
	}
	a.mu.RUnlock()
	revoked := 0
	for _, token := range tokens {
		if a.removeSession(token) {
			revoked++
		}
	}
	return revoked
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

// SetSessionRemovedHook registers cleanup invoked after expiry or explicit logout.
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

// PruneExpired removes all expired sessions, including tokens that are never
// reused, and runs the cleanup hook for each removed token.
func (a *Auth) PruneExpired() {
	now := time.Now()
	a.mu.Lock()
	removed := make([]string, 0)
	for token, sess := range a.sessions {
		if !now.Before(sess.expires) {
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

// Middleware HTTP 中间件，检查 Authorization header
func (a *Auth) Middleware(next http.HandlerFunc, additionalRequired ...func() bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a.Available() {
			writeDenied(w, http.StatusServiceUnavailable, "user_database_unavailable", "用户数据库不可用，认证服务已关闭访问")
			return
		}
		required := a.Enabled()
		for _, check := range additionalRequired {
			required = required || (check != nil && check())
		}
		if !required {
			p, _ := a.Principal("")
			next(w, r.WithContext(context.WithValue(r.Context(), principalKey{}, p)))
			return
		}
		p, ok := a.SessionPrincipal(tokenFromRequest(r))
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
