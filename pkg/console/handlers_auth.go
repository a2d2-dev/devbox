package console

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
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
		Password   string `json:"password"`
		AccessCode string `json:"accessCode"`
		OTP        string `json:"otp"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	ip := clientIP(r)
	if ban, banned := s.bans.IsBanned(ip); banned {
		w.Header().Set("Retry-After", fmt.Sprintf("%d", max(1, int(time.Until(ban.Until).Seconds()))))
		jsonError(w, fmt.Errorf("IP temporarily banned until %s", ban.Until.Format(time.RFC3339)), http.StatusTooManyRequests)
		return
	}
	if !s.security.VerifyAccessCode(req.AccessCode) {
		s.bans.RecordFailure(ip, "devbox-login")
		jsonError(w, fmt.Errorf("访问码错误"), http.StatusUnauthorized)
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
		s.bans.RecordFailure(ip, "devbox-login")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"authenticated": false,
			"message":       "密码错误",
		})
		return
	}
	settings := s.security.Settings()
	if settings.TOTPEnabled && req.OTP == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusPreconditionRequired)
		json.NewEncoder(w).Encode(map[string]interface{}{"authenticated": false, "twoFactorRequired": true, "message": "请输入动态验证码或恢复码"})
		return
	}
	if settings.TOTPEnabled && !s.security.VerifySecondFactor(req.OTP, time.Now()) {
		s.bans.RecordFailure(ip, "devbox-login")
		jsonError(w, fmt.Errorf("动态验证码或恢复码错误"), http.StatusUnauthorized)
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
		"enabled":            enabled,
		"authenticated":      authenticated,
		"accessCodeRequired": s.security.Settings().AccessCodeEnabled,
		"twoFactorRequired":  s.security.Settings().TOTPEnabled,
	})
}
