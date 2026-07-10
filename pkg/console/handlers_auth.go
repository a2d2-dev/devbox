package console

import (
	"encoding/json"
	"net/http"
)

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

	token, ok := s.auth.Verify(req.Password)
	if !ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"authenticated": false,
			"message":       "密码错误",
		})
		return
	}

	s.jsonOK(w, map[string]interface{}{
		"authenticated": true,
		"token":         token,
		"message":       "验证成功",
	})
}

func (s *Server) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	enabled := s.auth != nil && s.auth.Enabled()
	authenticated := false

	if s.auth != nil {
		token := r.Header.Get("Authorization")
		if token == "" {
			token = r.URL.Query().Get("token")
		}
		authenticated = s.auth.ValidateToken(token)
	}

	s.jsonOK(w, map[string]interface{}{
		"enabled":       enabled,
		"authenticated": authenticated,
	})
}
