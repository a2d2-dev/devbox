package console

import (
	"net/http"
	"strings"

	"github.com/a2d2-dev/devbox/pkg/auth"
	eventlog "github.com/a2d2-dev/devbox/pkg/syslog"
)

// accountSession is the sanitized view of a live session returned to the
// current user. It intentionally never carries a full IP, a raw User-Agent or
// any token: SourceIP is masked and the User-Agent is parsed into coarse device
// labels before it leaves the process.
type accountSession struct {
	ID           string `json:"id"`
	DeviceLabel  string `json:"deviceLabel"`
	DeviceType   string `json:"deviceType"`
	LoginAt      string `json:"loginAt"`
	LastActiveAt string `json:"lastActiveAt"`
	IPMasked     string `json:"ipMasked"`
	Current      bool   `json:"current"`
}

// registerAccountSessionRoutes wires self-service live-session listing,
// single-device revocation and logout-others endpoints. All operate strictly on
// the caller's own identity.
func (s *Server) registerAccountSessionRoutes() {
	s.mux.HandleFunc("/api/v1/account/sessions", s.handleAccountSessions)
	s.mux.HandleFunc("/api/v1/account/sessions/", s.handleAccountSessionByID)
	s.mux.HandleFunc("/api/v1/account/logout-others", s.handleAccountLogoutOthers)
}

// currentPrincipal resolves the caller's session principal from the bearer
// token. A missing or invalid session yields ok=false so handlers can answer
// 401 without leaking anything. It deliberately requires a real session
// (SessionPrincipal), never the unauthenticated local fallback.
func (s *Server) currentPrincipal(r *http.Request) (auth.Principal, string, bool) {
	if s.auth == nil {
		return auth.Principal{}, "", false
	}
	token := r.Header.Get("Authorization")
	if token == "" {
		token = r.URL.Query().Get("token")
	}
	p, ok := s.auth.SessionPrincipal(token)
	if !ok {
		return auth.Principal{}, "", false
	}
	return p, token, true
}

func (s *Server) handleAccountSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	principal, token, ok := s.currentPrincipal(r)
	if !ok {
		writeJSONErrStatus(w, http.StatusUnauthorized, map[string]any{"error": "身份验证失败", "reason": "unauthorized"})
		return
	}
	if principal.UserID == "" {
		writeAccountNotManaged(w)
		return
	}

	live, err := s.auth.ListUserSessions(principal.UserID)
	if err != nil {
		http.Error(w, "session store unavailable", http.StatusServiceUnavailable)
		return
	}
	currentTokenHash := auth.HashToken(bearerTokenValue(token))
	sessions := make([]accountSession, 0, len(live))
	for _, sess := range live {
		label, deviceType := parseUA(sess.UserAgent)
		sessions = append(sessions, accountSession{
			ID:           sess.ID,
			DeviceLabel:  label,
			DeviceType:   deviceType,
			LoginAt:      sess.LoginAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
			LastActiveAt: sess.LastActiveAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
			IPMasked:     maskIP(sess.SourceIP),
			Current:      sess.TokenHash == currentTokenHash,
		})
	}
	s.jsonOK(w, sessions)
}

func (s *Server) handleAccountSessionByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	principal, token, ok := s.currentPrincipal(r)
	if !ok {
		writeJSONErrStatus(w, http.StatusUnauthorized, map[string]any{"error": "身份验证失败", "reason": "unauthorized"})
		return
	}
	if principal.UserID == "" {
		writeAccountNotManaged(w)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/account/sessions/")
	if strings.TrimSpace(id) == "" || strings.Contains(id, "/") {
		http.NotFound(w, r)
		return
	}
	revoked, current := s.auth.RevokeUserSession(principal.UserID, id, token)
	if current {
		writeJSONErrStatus(w, http.StatusBadRequest, map[string]any{"error": "请使用退出登录结束当前会话", "reason": "current_session"})
		return
	}
	if !revoked {
		http.NotFound(w, r)
		return
	}
	s.recordEvent(r, eventlog.Input{
		Level: "warning", Module: "auth", Username: principal.Username,
		Event: "退出指定设备", EventType: "LOGOUT_SESSION", Outcome: "success",
		ResourceKind: "session", ResourceID: id,
	})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleAccountLogoutOthers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	principal, token, ok := s.currentPrincipal(r)
	if !ok {
		writeJSONErrStatus(w, http.StatusUnauthorized, map[string]any{"error": "身份验证失败", "reason": "unauthorized"})
		return
	}
	revoked := 0
	if principal.UserID != "" {
		revoked = s.auth.RevokeUserExcept(principal.UserID, token)
	} else {
		revoked = s.auth.RevokeUserSessionsExcept(principal.Username, token)
	}
	s.recordEvent(r, eventlog.Input{
		Level: "warning", Module: "auth", Username: principal.Username,
		Event: "退出其他全部设备", EventType: "LOGOUT_OTHERS", Outcome: "success",
		Payload: map[string]any{"revoked_count": revoked},
	})
	w.WriteHeader(http.StatusNoContent)
}
func bearerTokenValue(token string) string {
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(token), "Bearer "))
}
