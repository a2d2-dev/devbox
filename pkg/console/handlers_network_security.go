package console

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	devnetwork "github.com/a2d2-dev/devbox/pkg/network"
	"github.com/a2d2-dev/devbox/pkg/security"
)

func (s *Server) registerNetworkSecurityRoutes() {
	s.mux.HandleFunc("/api/v1/network/remote-access", s.handleRemoteAccess)
	s.mux.HandleFunc("/api/v1/network/ddns/preview", s.handleDDNSPreview)
	s.mux.HandleFunc("/api/v1/network/ddns/update", s.handleDDNSUpdate)
	s.mux.HandleFunc("/api/v1/security/settings", s.handleSecuritySettings)
	s.mux.HandleFunc("/api/v1/security/settings/preview", s.handleSecuritySettingsPreview)
	s.mux.HandleFunc("/api/v1/security/ssh", s.handleSSHStatus)
	s.mux.HandleFunc("/api/v1/security/ssh/preview", s.handleSSHPreview)
	s.mux.HandleFunc("/api/v1/security/ssh/apply", s.handleSSHApply)
	s.mux.HandleFunc("/api/v1/security/firewall", s.handleFirewallStatus)
	s.mux.HandleFunc("/api/v1/security/firewall/preview", s.handleFirewallPreview)
	s.mux.HandleFunc("/api/v1/security/firewall/apply", s.handleFirewallApply)
	s.mux.HandleFunc("/api/v1/security/totp/enroll", s.handleTOTPEnroll)
	s.mux.HandleFunc("/api/v1/security/totp/confirm", s.handleTOTPConfirm)
	s.mux.HandleFunc("/api/v1/security/totp/disable", s.handleTOTPDisable)
	s.mux.HandleFunc("/api/v1/security/bans", s.handleBans)
	s.mux.HandleFunc("/api/v1/security/bans/", s.handleBan)
	s.mux.HandleFunc("/api/v1/security/ban-rule", s.handleBanRule)
	s.mux.HandleFunc("/api/v1/security/certificates", s.handleCertificates)
	s.mux.HandleFunc("/api/v1/security/certificates/preview", s.handleCertificatePreview)
	s.mux.HandleFunc("/api/v1/security/certificates/self-signed", s.handleSelfSignedCertificate)
}

func (s *Server) handleRemoteAccess(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	remote := s.network.RemoteAccess()
	https := remote.HTTPS
	settings := s.security.Settings()
	if settings.HTTPSCertificate != "" {
		for _, listener := range remote.Listeners {
			if listener.Port == settings.HTTPSPort {
				https = true
				break
			}
		}
	}
	s.jsonOK(w, map[string]any{"listeners": remote.Listeners, "tunnelIPs": remote.TunnelIPs, "https": https, "currentSessionIP": clientIP(r)})
}

func settingsDDNS(cfg security.Settings, echo string) devnetwork.DDNSConfig {
	return devnetwork.DDNSConfig{Provider: cfg.DDNSProvider, Domain: cfg.DDNSDomain, CredentialRef: cfg.DDNSCredentialRef, WebhookURL: cfg.DDNSWebhookURL, EchoCommand: echo}
}

func (s *Server) handleDDNSPreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var cfg devnetwork.DDNSConfig
	if !decodeJSON(w, r, &cfg) {
		return
	}
	preview, err := devnetwork.PreviewDDNS(cfg)
	if err != nil {
		jsonError(w, err, http.StatusBadRequest)
		return
	}
	s.jsonOK(w, map[string]any{"preview": preview, "credential": "configured (redacted)", "dryRun": true})
}

func (s *Server) handleDDNSUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		EchoCommand string `json:"echoCommand"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	result, err := devnetwork.RunDDNSDry(settingsDDNS(s.security.Settings(), req.EchoCommand))
	if err != nil {
		jsonError(w, err, http.StatusBadRequest)
		return
	}
	s.jsonOK(w, map[string]any{"status": "verified", "result": result, "dryRun": true, "updatedAt": time.Now()})
}

func (s *Server) handleSecuritySettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.jsonOK(w, s.security.Settings())
	case http.MethodPost:
		var req struct {
			security.SettingsUpdate
			Confirm bool `json:"confirm"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		if !req.Confirm {
			jsonError(w, fmt.Errorf("confirmation required"), http.StatusConflict)
			return
		}
		if err := s.validateSettings(req.SettingsUpdate); err != nil {
			jsonError(w, err, http.StatusBadRequest)
			return
		}
		if err := s.security.Update(req.SettingsUpdate); err != nil {
			jsonError(w, err, http.StatusBadRequest)
			return
		}
		s.jsonOK(w, map[string]any{"settings": s.security.Settings(), "restartRequired": true})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleSecuritySettingsPreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req security.SettingsUpdate
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := s.validateSettings(req); err != nil {
		jsonError(w, err, http.StatusBadRequest)
		return
	}
	current := s.security.Settings()
	redacted := req
	redacted.AccessCode = ""
	s.jsonOK(w, map[string]any{"current": current, "proposed": redacted, "restartRequired": current.HTTPPort != req.HTTPPort || current.HTTPSPort != req.HTTPSPort || current.HTTPSCertificate != req.HTTPSCertificate, "requiresConfirmation": true})
}

func (s *Server) validateSettings(req security.SettingsUpdate) error {
	current := s.security.Settings()
	if err := s.security.UpdatePreview(req); err != nil {
		return err
	}
	if req.HTTPSCertificate != "" {
		if _, _, err := s.certificates.Paths(req.HTTPSCertificate); err != nil {
			return err
		}
	}
	if req.DDNSProvider != "" {
		if err := devnetwork.ValidateDDNS(devnetwork.DDNSConfig{
			Provider: req.DDNSProvider, Domain: req.DDNSDomain,
			CredentialRef: req.DDNSCredentialRef, WebhookURL: req.DDNSWebhookURL,
		}); err != nil {
			return err
		}
	}
	for _, l := range s.network.RemoteAccess().Listeners {
		if (l.Port == req.HTTPPort && req.HTTPPort != current.HTTPPort) || (l.Port == req.HTTPSPort && req.HTTPSPort != current.HTTPSPort) {
			return fmt.Errorf("port %d is already in use by %s", l.Port, l.Process)
		}
	}
	return nil
}

func (s *Server) handleSSHStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", 405)
		return
	}
	s.jsonOK(w, s.network.SSHStatus())
}
func (s *Server) handleSSHPreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	var change devnetwork.SSHChange
	if !decodeJSON(w, r, &change) {
		return
	}
	remote := s.network.RemoteAccess()
	preview, err := devnetwork.PreviewSSH(s.network.SSHStatus(), change, remote.Listeners)
	if err != nil {
		jsonError(w, err, 400)
		return
	}
	s.jsonOK(w, map[string]any{"diff": preview, "requiresConfirmation": true, "dryRun": true})
}
func (s *Server) handleSSHApply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	var req struct {
		Change  devnetwork.SSHChange `json:"change"`
		Confirm bool                 `json:"confirm"`
		DryRun  bool                 `json:"dryRun"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if !req.Confirm {
		jsonError(w, fmt.Errorf("confirmation required"), 409)
		return
	}
	preview, err := devnetwork.PreviewSSH(s.network.SSHStatus(), req.Change, s.network.RemoteAccess().Listeners)
	if err != nil {
		jsonError(w, err, 400)
		return
	}
	s.jsonOK(w, map[string]any{"diff": preview, "status": "not-applied", "dryRun": true, "message": "本机安全策略禁止真实修改 sshd"})
}

func (s *Server) handleFirewallStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", 405)
		return
	}
	backend, rules, err := s.network.FirewallRules()
	if err != nil {
		s.jsonOK(w, map[string]any{"backend": backend, "ruleset": "", "error": err.Error(), "readOnly": true})
		return
	}
	s.jsonOK(w, map[string]any{"backend": backend, "ruleset": rules, "readOnly": true})
}

type firewallRequest struct {
	Rules     []devnetwork.FirewallRule `json:"rules"`
	SessionIP string                    `json:"sessionIP"`
	Confirm   bool                      `json:"confirm"`
}

func (s *Server) firewallPreview(r *http.Request, req firewallRequest) (devnetwork.FirewallPreview, error) {
	ip := clientIP(r)
	if req.SessionIP != "" && req.SessionIP != ip {
		return devnetwork.FirewallPreview{}, fmt.Errorf("sessionIP must match current request address")
	}
	return devnetwork.RenderFirewall(req.Rules, ip)
}
func (s *Server) handleFirewallPreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	var req firewallRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	preview, err := s.firewallPreview(r, req)
	if err != nil {
		jsonError(w, err, 400)
		return
	}
	s.jsonOK(w, preview)
}
func (s *Server) handleFirewallApply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	var req firewallRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if !req.Confirm {
		jsonError(w, fmt.Errorf("confirmation required"), 409)
		return
	}
	preview, err := s.firewallPreview(r, req)
	if err != nil {
		jsonError(w, err, 400)
		return
	}
	s.jsonOK(w, map[string]any{"preview": preview, "status": "not-applied", "dryRun": true, "message": "本机安全策略禁止真实应用防火墙规则"})
}

func (s *Server) handleTOTPEnroll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	enroll, err := s.security.BeginTOTP("DevBox", clientIP(r))
	if err != nil {
		jsonError(w, err, 500)
		return
	}
	s.jsonOK(w, enroll)
}
func (s *Server) handleTOTPConfirm(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	codes, err := s.security.ConfirmTOTP(req.Code)
	if err != nil {
		jsonError(w, err, 400)
		return
	}
	s.jsonOK(w, map[string]any{"enabled": true, "recoveryCodes": codes, "message": "恢复码仅显示一次"})
}
func (s *Server) handleTOTPDisable(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	if err := s.security.DisableTOTP(); err != nil {
		jsonError(w, err, 500)
		return
	}
	s.jsonOK(w, map[string]bool{"enabled": false})
}

func (s *Server) handleBans(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", 405)
		return
	}
	s.jsonOK(w, map[string]any{"items": s.bans.List(), "rule": s.bans.Rule(), "sshLogMonitoring": "display-only"})
}
func (s *Server) handleBan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", 405)
		return
	}
	ip := strings.TrimPrefix(r.URL.Path, "/api/v1/security/bans/")
	if net.ParseIP(ip) == nil {
		jsonError(w, fmt.Errorf("invalid IP"), 400)
		return
	}
	s.bans.Unban(ip)
	s.jsonOK(w, map[string]any{"unbanned": ip})
}
func (s *Server) handleBanRule(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodPut {
		http.Error(w, "method not allowed", 405)
		return
	}
	var req struct {
		security.BanRule
		Confirm bool `json:"confirm"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	validator := security.NewBanManager(security.BanRule{})
	if err := validator.SetRule(req.BanRule); err != nil {
		jsonError(w, err, 400)
		return
	}
	if r.Method == http.MethodPost {
		s.jsonOK(w, map[string]any{"current": s.bans.Rule(), "proposed": validator.Rule(), "requiresConfirmation": true})
		return
	}
	if !req.Confirm {
		jsonError(w, fmt.Errorf("confirmation required"), http.StatusConflict)
		return
	}
	if err := s.bans.SetRule(req.BanRule); err != nil {
		jsonError(w, err, 400)
		return
	}
	s.jsonOK(w, s.bans.Rule())
}

func (s *Server) handleCertificates(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.jsonOK(w, map[string]any{"items": s.certificates.List(), "acme": "not implemented"})
	case http.MethodPost:
		var req struct {
			Name        string `json:"name"`
			Certificate string `json:"certificate"`
			PrivateKey  string `json:"privateKey"`
			Confirm     bool   `json:"confirm"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		if !req.Confirm {
			jsonError(w, fmt.Errorf("confirmation required"), http.StatusConflict)
			return
		}
		cert, err := s.certificates.Upload(req.Name, []byte(req.Certificate), []byte(req.PrivateKey))
		if err != nil {
			jsonError(w, err, 400)
			return
		}
		s.jsonOK(w, cert)
	default:
		http.Error(w, "method not allowed", 405)
	}
}

func (s *Server) handleCertificatePreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	var req struct {
		Name        string `json:"name"`
		Certificate string `json:"certificate"`
		PrivateKey  string `json:"privateKey"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	cert, err := s.certificates.Validate(req.Name, []byte(req.Certificate), []byte(req.PrivateKey))
	if err != nil {
		jsonError(w, err, 400)
		return
	}
	s.jsonOK(w, map[string]any{"certificate": cert, "privateKey": "configured (redacted)", "requiresConfirmation": true})
}
func (s *Server) handleSelfSignedCertificate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	var req struct {
		Name      string   `json:"name"`
		Hosts     []string `json:"hosts"`
		ValidDays int      `json:"validDays"`
		Confirm   bool     `json:"confirm"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if !req.Confirm {
		if err := s.certificates.ValidateSelfSigned(req.Name, req.Hosts, req.ValidDays); err != nil {
			jsonError(w, err, 400)
			return
		}
		s.jsonOK(w, map[string]any{"proposed": map[string]any{"name": req.Name, "hosts": req.Hosts, "validDays": req.ValidDays}, "requiresConfirmation": true})
		return
	}
	cert, err := s.certificates.SelfSigned(req.Name, req.Hosts, req.ValidDays)
	if err != nil {
		jsonError(w, err, 400)
		return
	}
	s.jsonOK(w, cert)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, out any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		jsonError(w, fmt.Errorf("invalid request: %w", err), 400)
		return false
	}
	return true
}
func jsonError(w http.ResponseWriter, err error, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return strings.TrimSpace(r.RemoteAddr)
}
