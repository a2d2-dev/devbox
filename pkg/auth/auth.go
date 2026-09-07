package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"sort"
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
	SessionStore    *SessionStore
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
	id        string
	token     string
	expires   time.Time
	loginAt   time.Time
	lastSeen  time.Time
	sourceIP  string
	userAgent string
	principal Principal
}

type Auth struct {
	password         string
	sessionTTL       time.Duration
	users            *users.Store
	usersConfigured  bool
	sessionStore     *SessionStore
	mu               sync.RWMutex
	sessions         map[string]session
	onSessionRemoved func(string)
}

type SessionMetadata struct {
	SourceIP  string
	UserAgent string
}

type Session struct {
	ID           string
	Token        string
	Principal    Principal
	SourceIP     string
	UserAgent    string
	LoginAt      time.Time
	LastActiveAt time.Time
	ExpiresAt    time.Time
}

func New(cfg Config) *Auth {
	ttl := time.Duration(cfg.SessionTTL) * time.Second
	if ttl == 0 {
		ttl = time.Hour
	}
	return &Auth{password: strings.TrimSpace(cfg.Password), sessionTTL: ttl, users: cfg.Users, usersConfigured: cfg.UsersConfigured || cfg.Users != nil, sessionStore: cfg.SessionStore, sessions: make(map[string]session)}
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
	return a.IssueSessionWithMetadata(p, SessionMetadata{})
}

// IssueSessionWithMetadata creates a session after all configured authentication
// factors have succeeded and records request metadata for account device views.
func (a *Auth) IssueSessionWithMetadata(p Principal, meta SessionMetadata) string {
	a.PruneExpired()
	now := time.Now().UTC()
	token := generateToken()
	id := generateToken()
	sess := session{
		id: id, token: token, expires: now.Add(a.sessionTTL), loginAt: now, lastSeen: now,
		sourceIP: strings.TrimSpace(meta.SourceIP), userAgent: strings.TrimSpace(meta.UserAgent),
		principal: p,
	}
	a.mu.Lock()
	a.sessions[token] = sess
	a.mu.Unlock()
	if a.sessionStore != nil {
		_ = a.sessionStore.Put(context.Background(), exportSession(sess))
	}
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
		if a.sessionStore == nil {
			return Principal{}, false
		}
		stored, found, err := a.sessionStore.ByToken(context.Background(), token)
		if err != nil || !found {
			return Principal{}, false
		}
		sess = importSession(stored)
		if !time.Now().Before(sess.expires) {
			a.removeSession(token)
			return Principal{}, false
		}
		a.mu.Lock()
		a.sessions[token] = sess
		a.mu.Unlock()
	}
	a.touchSession(token)
	return sess.principal, true
}

func (a *Auth) ValidateToken(token string) bool { _, ok := a.Principal(token); return ok }

func (a *Auth) RevokeUser(userID string) {
	a.revokeUser(userID, "")
}

// RevokeUserExcept revokes every session belonging to userID except the one
// identified by keepToken. It reuses the existing session store so callers such
// as self-service password changes can invalidate other devices while keeping
// the caller's current session alive. keepToken accepts bare and Bearer forms.
func (a *Auth) RevokeUserExcept(userID, keepToken string) {
	a.revokeUser(userID, normalizeToken(keepToken))
}

func (a *Auth) revokeUser(userID, keepToken string) {
	if userID == "" {
		return
	}
	a.mu.RLock()
	tokens := make([]string, 0)
	for token, sess := range a.sessions {
		if sess.principal.UserID == userID && token != keepToken {
			tokens = append(tokens, token)
		}
	}
	a.mu.RUnlock()
	if a.sessionStore != nil {
		if removed, err := a.sessionStore.DeleteUserExcept(context.Background(), userID, keepToken); err == nil && removed > 0 {
			a.removeCachedUserExcept(userID, keepToken)
			return
		}
	}
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
	if a.sessionStore != nil {
		revoked, err := a.sessionStore.DeleteUsernameExcept(context.Background(), username, keepToken)
		if err == nil {
			a.removeCachedUsernameExcept(username, keepToken)
			return revoked
		}
	}
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
	if a.sessionStore != nil {
		if removed, err := a.sessionStore.DeleteToken(context.Background(), token); err == nil && removed {
			existed = true
		}
	}
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
	if a.sessionStore != nil {
		if tokens, err := a.sessionStore.PruneExpired(context.Background(), now); err == nil {
			for _, token := range tokens {
				found := false
				for _, existing := range removed {
					if existing == token {
						found = true
						break
					}
				}
				if !found {
					removed = append(removed, token)
				}
			}
		}
	}
	if hook != nil {
		for _, token := range removed {
			hook(token)
		}
	}
}

func (a *Auth) ListUserSessions(userID string) ([]Session, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil, nil
	}
	a.PruneExpired()
	if a.sessionStore != nil {
		return a.sessionStore.ListByUserID(context.Background(), userID, time.Now().UTC())
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := []Session{}
	now := time.Now()
	for _, sess := range a.sessions {
		if sess.principal.UserID == userID && now.Before(sess.expires) {
			out = append(out, exportSession(sess))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].LastActiveAt.Equal(out[j].LastActiveAt) {
			return out[i].LoginAt.After(out[j].LoginAt)
		}
		return out[i].LastActiveAt.After(out[j].LastActiveAt)
	})
	return out, nil
}

func (a *Auth) RevokeUserSession(userID, sessionID, currentToken string) (bool, bool) {
	userID = strings.TrimSpace(userID)
	sessionID = strings.TrimSpace(sessionID)
	currentToken = normalizeToken(currentToken)
	if userID == "" || sessionID == "" {
		return false, false
	}
	if current, ok := a.sessionByToken(currentToken); ok && current.id == sessionID {
		return false, true
	}
	if a.sessionStore != nil {
		removed, err := a.sessionStore.DeleteUserSession(context.Background(), userID, sessionID)
		if err == nil && removed {
			a.removeCachedSessionID(sessionID)
			return true, false
		}
	}
	if a.removeCachedUserSession(userID, sessionID) {
		return true, false
	}
	return false, false
}

func (a *Auth) sessionByToken(token string) (session, bool) {
	token = normalizeToken(token)
	if token == "" {
		return session{}, false
	}
	a.mu.RLock()
	sess, ok := a.sessions[token]
	a.mu.RUnlock()
	if ok {
		return sess, true
	}
	if a.sessionStore == nil {
		return session{}, false
	}
	stored, found, err := a.sessionStore.ByToken(context.Background(), token)
	if err != nil || !found {
		return session{}, false
	}
	return importSession(stored), true
}

func (a *Auth) touchSession(token string) {
	now := time.Now().UTC()
	a.mu.Lock()
	if sess, ok := a.sessions[token]; ok {
		sess.lastSeen = now
		a.sessions[token] = sess
	}
	a.mu.Unlock()
	if a.sessionStore != nil {
		_ = a.sessionStore.Touch(context.Background(), token, now)
	}
}

func (a *Auth) removeCachedUserExcept(userID, keepToken string) {
	a.mu.Lock()
	for token, sess := range a.sessions {
		if sess.principal.UserID == userID && token != keepToken {
			delete(a.sessions, token)
		}
	}
	a.mu.Unlock()
}

func (a *Auth) removeCachedUsernameExcept(username, keepToken string) {
	a.mu.Lock()
	for token, sess := range a.sessions {
		if token != keepToken && strings.EqualFold(sess.principal.Username, username) {
			delete(a.sessions, token)
		}
	}
	a.mu.Unlock()
}

func (a *Auth) removeCachedSessionID(sessionID string) {
	a.mu.Lock()
	for token, sess := range a.sessions {
		if sess.id == sessionID {
			delete(a.sessions, token)
		}
	}
	a.mu.Unlock()
}

func (a *Auth) removeCachedUserSession(userID, sessionID string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	for token, sess := range a.sessions {
		if sess.principal.UserID == userID && sess.id == sessionID {
			delete(a.sessions, token)
			return true
		}
	}
	return false
}

func exportSession(sess session) Session {
	return Session{
		ID: sess.id, Token: sess.token, Principal: sess.principal, SourceIP: sess.sourceIP,
		UserAgent: sess.userAgent, LoginAt: sess.loginAt, LastActiveAt: sess.lastSeen,
		ExpiresAt: sess.expires,
	}
}

func importSession(sess Session) session {
	return session{
		id: sess.ID, token: sess.Token, principal: sess.Principal, sourceIP: sess.SourceIP,
		userAgent: sess.UserAgent, loginAt: sess.LoginAt, lastSeen: sess.LastActiveAt,
		expires: sess.ExpiresAt,
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
