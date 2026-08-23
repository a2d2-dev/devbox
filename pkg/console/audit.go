package console

import (
	"net"
	"net/http"
	"strings"

	eventlog "github.com/a2d2-dev/devbox/pkg/syslog"
	"go.uber.org/zap"
)

func (s *Server) recordEvent(r *http.Request, input eventlog.Input) {
	if s.systemLog == nil {
		return
	}
	if input.Username == "" {
		input.Username = s.actorFromRequest(r)
	}
	if input.SourceIP == "" {
		input.SourceIP = requestIP(r)
	}
	if input.UserAgent == "" && r != nil {
		input.UserAgent = r.UserAgent()
	}
	if _, err := s.systemLog.Append(input); err != nil {
		s.logger.Warn("Failed to persist system event", zap.String("event_type", input.EventType), zap.Error(err))
	}
}

func (s *Server) actorFromRequest(r *http.Request) string {
	if r == nil {
		return "system"
	}
	token := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if token != "" {
		s.sessionUsersMu.RLock()
		username := s.sessionUsers[token]
		s.sessionUsersMu.RUnlock()
		if username != "" {
			return username
		}
	}
	return "console"
}

func requestIP(r *http.Request) string {
	if r == nil {
		return ""
	}
	if forwarded := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-For"), ",")[0]); forwarded != "" {
		return forwarded
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}
