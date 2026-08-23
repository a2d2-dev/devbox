package console

import (
	"errors"
	"net"
	"net/http"
	"net/netip"
	"strings"

	"github.com/a2d2-dev/devbox/pkg/apps"
	eventlog "github.com/a2d2-dev/devbox/pkg/syslog"
	"go.uber.org/zap"
)

var errAuditUnavailable = errors.New("audit log unavailable")

func (s *Server) recordEvent(r *http.Request, input eventlog.Input) (eventlog.Event, error) {
	if s.systemLog == nil {
		return eventlog.Event{}, errAuditUnavailable
	}
	if input.Username == "" {
		input.Username = s.actorFromRequest(r)
	}
	if input.SourceIP == "" {
		input.SourceIP = s.requestIP(r)
	}
	if input.UserAgent == "" && r != nil {
		input.UserAgent = r.UserAgent()
	}
	event, err := s.systemLog.Append(input)
	if err != nil {
		if s.logger != nil {
			s.logger.Warn("Failed to persist system event", zap.String("event_type", input.EventType), zap.Error(err))
		}
		return eventlog.Event{}, err
	}
	return event, nil
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
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

func (s *Server) requestIP(r *http.Request) string {
	remote := requestIP(r)
	peer, err := netip.ParseAddr(remote)
	if err != nil || !matchesTrustedProxy(peer, s.config.TrustedProxies) {
		return remote
	}
	hops := make([]netip.Addr, 0)
	for _, raw := range strings.Split(r.Header.Get("X-Forwarded-For"), ",") {
		if hop, err := netip.ParseAddr(strings.TrimSpace(raw)); err == nil {
			hops = append(hops, hop)
		}
	}
	for i := len(hops) - 1; i >= 0; i-- {
		if !matchesTrustedProxy(hops[i], s.config.TrustedProxies) {
			return hops[i].String()
		}
	}
	if len(hops) > 0 {
		return hops[0].String()
	}
	return remote
}

func matchesTrustedProxy(ip netip.Addr, configured []string) bool {
	for _, raw := range configured {
		raw = strings.TrimSpace(raw)
		if prefix, err := netip.ParsePrefix(raw); err == nil && prefix.Contains(ip) {
			return true
		}
		if trusted, err := netip.ParseAddr(raw); err == nil && trusted == ip {
			return true
		}
	}
	return false
}

func (s *Server) installTaskAuditObserver() {
	registrar, ok := s.controller.(apps.TaskObserverRegistrar)
	if !ok || s.systemLog == nil {
		return
	}
	registrar.RegisterTaskObserver(func(task apps.Task) {
		outcome, level := "success", "info"
		if task.Status != apps.TaskSucceeded {
			outcome, level = "failure", "error"
		}
		eventType, event := taskAuditEvent(task)
		_, _ = s.recordEvent(nil, eventlog.Input{
			Level: level, Module: "apps", Username: "admin", Event: event, EventType: eventType, Outcome: outcome,
			ResourceKind: "application", ResourceID: task.AppID,
			Payload: map[string]any{"task_id": task.ID, "task_type": task.Type, "action": task.Action, "message": task.Message},
		})
	})
}

func taskAuditEvent(task apps.Task) (string, string) {
	switch task.Type {
	case apps.TaskApply:
		return "APP_INSTALL", "应用部署任务完成"
	case apps.TaskOperate:
		return "APP_" + strings.ToUpper(string(task.Action)), "应用操作任务完成"
	case apps.TaskRemove:
		return "APP_UNINSTALL", "应用卸载任务完成"
	case apps.TaskRestore:
		return "APP_RESTORE", "应用恢复任务完成"
	default:
		return "APP_TASK", "应用任务完成"
	}
}
