package console

import (
	"fmt"
	"net/http"
	"net/netip"
	"strconv"
	"strings"

	"github.com/a2d2-dev/devbox/pkg/auth"
	eventlog "github.com/a2d2-dev/devbox/pkg/syslog"
)

// accountSession is the sanitized view of a login-history row returned to the
// current user. It intentionally never carries a full IP, a raw User-Agent or
// any token: SourceIP is masked and the User-Agent is parsed into coarse
// device labels before it leaves the process.
type accountSession struct {
	ID           string `json:"id"`
	DeviceLabel  string `json:"deviceLabel"`
	DeviceType   string `json:"deviceType"`
	LoginAt      string `json:"loginAt"`
	LastActiveAt string `json:"lastActiveAt"`
	IPMasked     string `json:"ipMasked"`
	Current      bool   `json:"current"`
}

// registerAccountSessionRoutes wires the self-service login-history and
// logout-others endpoints. Both operate strictly on the caller's own identity.
func (s *Server) registerAccountSessionRoutes() {
	s.mux.HandleFunc("/api/v1/account/sessions", s.handleAccountSessions)
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
	if s.systemLog == nil {
		http.Error(w, "system log unavailable", http.StatusServiceUnavailable)
		return
	}
	principal, _, ok := s.currentPrincipal(r)
	if !ok {
		writeJSONErrStatus(w, http.StatusUnauthorized, map[string]any{"error": "身份验证失败", "reason": "unauthorized"})
		return
	}

	// Only this user's events, newest first. The store returns events in
	// reverse-chronological order already, so index 0 is the most recent.
	page := s.systemLog.Query(eventlog.Query{Username: principal.Username, Limit: 200})
	sessions := make([]accountSession, 0, len(page.Events))
	// current is an approximation: audit LOGIN_SUCCESS events are not bound to a
	// revocable token, so we cannot map a row to the exact live session. We flag
	// the single most-recent LOGIN_SUCCESS for this user as the current session.
	currentAssigned := false
	for _, event := range page.Events {
		if event.EventType != "LOGIN_SUCCESS" {
			continue
		}
		// Query does a substring username match; require an exact (case-folded)
		// owner match so no other user's rows can appear.
		if !strings.EqualFold(event.Username, principal.Username) {
			continue
		}
		label, deviceType := parseUA(event.UserAgent)
		ts := event.TS.UTC().Format("2006-01-02T15:04:05Z07:00")
		row := accountSession{
			ID:          strconv.FormatUint(event.ID, 10),
			DeviceLabel: label,
			DeviceType:  deviceType,
			LoginAt:     ts,
			// lastActiveAt approximates to loginAt for now: sessions are in-memory
			// and audit events are per-login, so there is no per-session activity
			// timestamp to surface yet.
			LastActiveAt: ts,
			IPMasked:     maskIP(event.SourceIP),
		}
		if !currentAssigned {
			row.Current = true
			currentAssigned = true
		}
		sessions = append(sessions, row)
	}
	s.jsonOK(w, sessions)
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
	revoked := s.auth.RevokeUserSessionsExcept(principal.Username, token)
	s.recordEvent(r, eventlog.Input{
		Level: "warning", Module: "auth", Username: principal.Username,
		Event: "退出其他全部设备", EventType: "LOGOUT_OTHERS", Outcome: "success",
		Payload: map[string]any{"revoked_count": revoked},
	})
	w.WriteHeader(http.StatusNoContent)
}

// maskIP redacts a source IP so the response never carries a full address.
// IPv4 keeps the first three octets and masks the last as "x"
// (203.0.113.7 -> 203.0.113.x). IPv6 is truncated to its /32 prefix
// (first two hextets) rendered as "prefix::/32". Unparseable or empty input
// yields an empty string rather than echoing raw text.
func maskIP(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	// Strip a bracketed/single-colon port form; leave bare IPv6 (many colons).
	if host, ok := hostWithoutPort(raw); ok {
		raw = host
	}
	addr, err := netip.ParseAddr(raw)
	if err != nil {
		return ""
	}
	addr = addr.WithZone("")
	if addr.Is4() || addr.Is4In6() {
		a := addr.As4()
		return fmt.Sprintf("%d.%d.%d.x", a[0], a[1], a[2])
	}
	a := addr.As16()
	// First two hextets form the /32 prefix; everything after is dropped.
	return fmt.Sprintf("%x:%x::/32", uint16(a[0])<<8|uint16(a[1]), uint16(a[2])<<8|uint16(a[3]))
}

// hostWithoutPort strips a trailing port only for unambiguous "host:port"
// forms (bracketed IPv6 or an IPv4/host with exactly one colon). Bare IPv6
// addresses (multiple colons, no brackets) are returned untouched.
func hostWithoutPort(raw string) (string, bool) {
	if strings.HasPrefix(raw, "[") {
		if i := strings.Index(raw, "]"); i > 0 {
			return raw[1:i], true
		}
		return raw, false
	}
	if strings.Count(raw, ":") == 1 {
		return raw[:strings.Index(raw, ":")], true
	}
	return raw, false
}

// parseUA turns a raw User-Agent string into a coarse ("Browser · OS") label
// and a device type ("desktop"/"mobile"/"tablet"/"unknown") using simple
// substring rules. It never returns the original UA. Order of checks matters:
// more specific tokens are tested before more general ones.
func parseUA(ua string) (label, deviceType string) {
	ua = strings.TrimSpace(ua)
	if ua == "" {
		return "Unknown device", "unknown"
	}
	lower := strings.ToLower(ua)

	browser := "Unknown"
	switch {
	case strings.Contains(lower, "edg/") || strings.Contains(lower, "edge"):
		browser = "Edge"
	case strings.Contains(lower, "opr/") || strings.Contains(lower, "opera"):
		browser = "Opera"
	case strings.Contains(lower, "firefox"):
		browser = "Firefox"
	case strings.Contains(lower, "chrome") || strings.Contains(lower, "crios"):
		browser = "Chrome"
	case strings.Contains(lower, "safari"):
		browser = "Safari"
	case strings.Contains(lower, "curl"):
		browser = "curl"
	case strings.Contains(lower, "wget"):
		browser = "wget"
	case strings.Contains(lower, "go-http-client"):
		browser = "Go client"
	}

	osName := "Unknown OS"
	switch {
	case strings.Contains(lower, "windows"):
		osName = "Windows"
	case strings.Contains(lower, "iphone"):
		osName = "iOS"
	case strings.Contains(lower, "ipad"):
		osName = "iPadOS"
	case strings.Contains(lower, "android"):
		osName = "Android"
	case strings.Contains(lower, "mac os") || strings.Contains(lower, "macos") || strings.Contains(lower, "macintosh"):
		osName = "macOS"
	case strings.Contains(lower, "cros"):
		osName = "ChromeOS"
	case strings.Contains(lower, "linux"):
		osName = "Linux"
	}

	deviceType = "desktop"
	switch {
	case strings.Contains(lower, "ipad") || strings.Contains(lower, "tablet"):
		deviceType = "tablet"
	case strings.Contains(lower, "mobile") || strings.Contains(lower, "iphone") || strings.Contains(lower, "android"):
		deviceType = "mobile"
	case browser == "curl" || browser == "wget" || browser == "Go client":
		deviceType = "unknown"
	}

	return browser + " · " + osName, deviceType
}
