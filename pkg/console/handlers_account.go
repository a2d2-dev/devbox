package console

import (
	"errors"
	"net/http"
	"strings"

	"github.com/a2d2-dev/devbox/pkg/auth"
	eventlog "github.com/a2d2-dev/devbox/pkg/syslog"
	"github.com/a2d2-dev/devbox/pkg/users"
)

// avatarKindInitials is a constant placeholder for the account avatar rendering
// mode. Avatar storage is intentionally out of scope for this endpoint; the
// self-service API only advertises that clients should render initials.
const avatarKindInitials = "initials"

// registerAccountRoutes wires the self-service ("me") account endpoints. Every
// handler resolves the acting user from the session principal only and never
// trusts a userId/username supplied in the request, so it cannot be used to
// read or mutate another account. Authorization is enforced by authGate's
// session middleware; the handlers additionally re-check the principal so they
// stay safe if mounted without that gate.
func (s *Server) registerAccountRoutes() {
	s.mux.HandleFunc("/api/v1/account", s.handleAccount)
	s.mux.HandleFunc("/api/v1/account/password", s.handleAccountPassword)
}

// accountPrincipal returns the authenticated principal and its bare session
// token. It prefers the principal injected by the auth middleware and falls
// back to resolving the token directly, mirroring RequireAdmin. It reports
// false (and writes 401) when there is no valid session.
func (s *Server) accountPrincipal(w http.ResponseWriter, r *http.Request) (auth.Principal, string, bool) {
	if s.auth == nil {
		writeJSONErrStatus(w, http.StatusServiceUnavailable, map[string]any{"error": "authentication unavailable", "reason": "auth_unavailable"})
		return auth.Principal{}, "", false
	}
	token := bearerToken(r)
	p, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		p, ok = s.auth.SessionPrincipal(token)
	}
	if !ok {
		writeJSONErrStatus(w, http.StatusUnauthorized, map[string]any{"error": "身份验证失败", "reason": "unauthorized"})
		return auth.Principal{}, "", false
	}
	return p, token, true
}

func bearerToken(r *http.Request) string {
	token := r.Header.Get("Authorization")
	if token == "" {
		token = r.URL.Query().Get("token")
	}
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(token), "Bearer "))
}

func (s *Server) handleAccount(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleAccountGet(w, r)
	case http.MethodPatch:
		s.handleAccountPatch(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleAccountGet returns the current user's own profile. It never includes a
// session token or password hash.
func (s *Server) handleAccountGet(w http.ResponseWriter, r *http.Request) {
	p, _, ok := s.accountPrincipal(w, r)
	if !ok {
		return
	}
	view := map[string]any{
		"id":          p.UserID,
		"username":    p.Username,
		"displayName": p.DisplayName,
		"role":        p.Role,
		"avatarKind":  avatarKindInitials,
	}
	// createdAt is only known for real, store-backed accounts. Load it directly
	// by the session's own id so no request-supplied identifier is trusted.
	if s.users != nil && p.UserID != "" {
		if u, err := s.findUserByID(r, p.UserID); err == nil {
			view["createdAt"] = u.CreatedAt
		}
	}
	s.jsonOK(w, view)
}

// handleAccountPatch updates only the current user's display name. role and
// enabled are not user-editable: any such fields are rejected by the strict
// JSON decoder, and the session id is the only account ever touched.
func (s *Server) handleAccountPatch(w http.ResponseWriter, r *http.Request) {
	p, _, ok := s.accountPrincipal(w, r)
	if !ok {
		return
	}
	if !s.requireUsers(w) {
		return
	}
	if p.UserID == "" {
		writeJSONErrStatus(w, http.StatusBadRequest, map[string]any{"error": "当前会话不对应可修改的账号", "reason": "not_a_managed_account"})
		return
	}
	var req struct {
		DisplayName *string `json:"displayName"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.DisplayName == nil {
		writeJSONErrStatus(w, http.StatusBadRequest, map[string]any{"error": "displayName is required", "reason": "no_change"})
		return
	}
	u, err := s.users.UpdateUser(r.Context(), p.UserID, users.UpdateUser{DisplayName: req.DisplayName})
	if err != nil {
		s.userError(w, err)
		return
	}
	s.recordEvent(r, eventlog.Input{
		Level: "info", Module: "account", Username: p.Username,
		Event: "更新本人资料", EventType: "ACCOUNT_UPDATE", Outcome: "success",
		ResourceKind: "user", ResourceID: p.UserID,
	})
	s.jsonOK(w, map[string]any{
		"id":          u.ID,
		"username":    u.Username,
		"displayName": u.DisplayName,
		"role":        u.Role,
		"avatarKind":  avatarKindInitials,
		"createdAt":   u.CreatedAt,
	})
}

// handleAccountPassword changes the current user's password. It verifies the
// current password against the store, enforces the shared password policy, then
// revokes the user's other sessions while preserving the caller's current
// token. It never logs password material or the reason a check failed beyond a
// generic outcome.
func (s *Server) handleAccountPassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	p, token, ok := s.accountPrincipal(w, r)
	if !ok {
		return
	}
	if !s.requireUsers(w) {
		return
	}
	if p.UserID == "" {
		writeJSONErrStatus(w, http.StatusBadRequest, map[string]any{"error": "当前会话不对应可修改的账号", "reason": "not_a_managed_account"})
		return
	}
	var req struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	// Verify the current password against the store using the session's own
	// username. A failure must not change anything and must not leak whether the
	// account exists versus the password being wrong.
	if _, ok := s.users.Authenticate(r.Context(), p.Username, req.CurrentPassword); !ok {
		s.recordEvent(r, eventlog.Input{
			Level: "warning", Module: "account", Username: p.Username,
			Event: "修改本人密码失败", EventType: "PASSWORD_CHANGE", Outcome: "failure",
			ResourceKind: "user", ResourceID: p.UserID,
		})
		writeJSONErrStatus(w, http.StatusUnauthorized, map[string]any{"error": "当前密码错误", "reason": "invalid_current_password"})
		return
	}
	if err := users.ValidatePassword(req.NewPassword); err != nil {
		writeJSONErrStatus(w, http.StatusBadRequest, map[string]any{"error": err.Error(), "reason": "weak_password"})
		return
	}
	newPassword := req.NewPassword
	if _, err := s.users.UpdateUser(r.Context(), p.UserID, users.UpdateUser{Password: &newPassword}); err != nil {
		s.userError(w, err)
		return
	}
	// Invalidate every other session for this user; keep the caller's current
	// token valid so a password change does not log the caller out.
	s.auth.RevokeUserExcept(p.UserID, token)
	s.recordEvent(r, eventlog.Input{
		Level: "info", Module: "account", Username: p.Username,
		Event: "修改本人密码", EventType: "PASSWORD_CHANGE", Outcome: "success",
		ResourceKind: "user", ResourceID: p.UserID,
	})
	w.WriteHeader(http.StatusNoContent)
}

// findUserByID resolves a store user by id without exposing a request-supplied
// identifier path. It is used only with the caller's own session id.
func (s *Server) findUserByID(r *http.Request, id string) (users.User, error) {
	list, err := s.users.ListUsers(r.Context(), "")
	if err != nil {
		return users.User{}, err
	}
	for _, u := range list {
		if u.ID == id {
			return u, nil
		}
	}
	return users.User{}, errors.New("not found")
}
