package console

import (
	"encoding/json"
	"net/http"

	"github.com/a2d2-dev/devbox/pkg/auth"
)

// registerAccountPrefsRoutes wires the per-user preference endpoints. Both live
// under /api/v1/account/ and require an authenticated session: the enclosing
// authGate runs auth.Middleware for every /api/v1/* path, so by the time these
// handlers run a principal is in context whenever auth is enabled.
func (s *Server) registerAccountPrefsRoutes() {
	s.mux.HandleFunc("/api/v1/account/preferences", s.handleAccountPreferences)
}

// handleAccountPreferences reads or replaces the calling user's stored
// preferences. It only ever touches the authenticated principal's own row; the
// request body never carries a user_id.
func (s *Server) handleAccountPreferences(w http.ResponseWriter, r *http.Request) {
	if !s.requireUsers(w) {
		return
	}
	userID, ok := s.currentUserID(r)
	if !ok {
		writeAccountUnauthorized(w)
		return
	}
	switch r.Method {
	case http.MethodGet:
		raw, err := s.users.GetPrefs(r.Context(), userID)
		if err != nil {
			s.userError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(raw)
	case http.MethodPut:
		var body map[string]any
		if !decodeAccountPrefs(w, r, &body) {
			return
		}
		saved, err := s.users.PutPrefs(r.Context(), userID, body)
		if err != nil {
			s.userError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(saved)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// currentUserID resolves the authenticated user's id. It returns false for
// anonymous or legacy principals that have no backing users row, which keeps
// preference storage strictly per-account.
func (s *Server) currentUserID(r *http.Request) (string, bool) {
	p, ok := auth.PrincipalFromContext(r.Context())
	if !ok || p.UserID == "" {
		return "", false
	}
	return p.UserID, true
}

func writeAccountUnauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
}

// decodeAccountPrefs decodes an arbitrary JSON object. Unlike decodeJSON it does
// not reject unknown fields: the body is intentionally free-form and gets
// whitelisted downstream by users.PutPrefs.
func decodeAccountPrefs(w http.ResponseWriter, r *http.Request, v *map[string]any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		http.Error(w, "invalid request: "+err.Error(), http.StatusBadRequest)
		return false
	}
	if *v == nil {
		*v = map[string]any{}
	}
	return true
}
