package console

import (
	"encoding/json"
	"net/http"

	eventlog "github.com/a2d2-dev/devbox/pkg/syslog"
)

// registerAuthRoutes 注册认证路由
func (s *Server) registerAuthRoutes() {
	s.mux.HandleFunc("/api/v1/auth/verify", s.handleAuthVerify)
	s.mux.HandleFunc("/api/v1/auth/status", s.handleAuthStatus)
	s.mux.HandleFunc("/api/v1/auth/logout", s.handleAuthLogout)
}

func (s *Server) handleAuthLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.auth == nil || !s.auth.RevokeToken(r.Header.Get("Authorization")) {
		writeJSONErrStatus(w, http.StatusUnauthorized, map[string]any{"error": "身份验证失败", "reason": "unauthorized"})
		return
	}
	s.recordEvent(r, eventlog.Input{
		Level: "info", Module: "auth", Username: "admin", Event: "退出本地会话", EventType: "LOGOUT", Outcome: "success",
	})
	s.jsonOK(w, map[string]any{"authenticated": false})
}

func (s *Server) installAuthSessionCleanup() {
	if s.auth == nil {
		return
	}
	s.auth.SetSessionRemovedHook(func(token string) {
		s.sessionUsersMu.Lock()
		delete(s.sessionUsers, token)
		s.sessionUsersMu.Unlock()
	})
}

func (s *Server) handleAuthVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Password string `json:"password"`
		Username string `json:"username"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	if s.auth == nil || !s.auth.Enabled() {
		s.recordEvent(r, eventlog.Input{
			Level: "info", Module: "auth", Username: "admin",
			Event: "本地登录成功", EventType: "LOGIN_SUCCESS", Outcome: "success",
		})
		s.jsonOK(w, map[string]interface{}{
			"authenticated": true,
			"token":         "",
			"username":      "admin",
			"message":       "认证未启用",
		})
		return
	}

	token, ok := s.auth.Verify(req.Password)
	if !ok {
		s.recordEvent(r, eventlog.Input{
			Level: "warning", Module: "auth", Username: "anonymous",
			Event: "本地登录失败", EventType: "LOGIN_FAILED", Outcome: "failure",
		})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"authenticated": false,
			"message":       "密码错误",
		})
		return
	}
	s.sessionUsersMu.Lock()
	s.sessionUsers[token] = "admin"
	s.sessionUsersMu.Unlock()
	s.recordEvent(r, eventlog.Input{
		Level: "info", Module: "auth", Username: "admin",
		Event: "本地登录成功", EventType: "LOGIN_SUCCESS", Outcome: "success",
	})

	s.jsonOK(w, map[string]interface{}{
		"authenticated": true,
		"token":         token,
		"username":      "admin",
		"message":       "验证成功",
	})
}

func (s *Server) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
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
