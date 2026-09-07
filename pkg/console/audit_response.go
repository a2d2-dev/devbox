package console

import (
	"time"

	eventlog "github.com/a2d2-dev/devbox/pkg/syslog"
)

// auditEvent is the admin-facing audit-history response shape. It preserves
// event metadata but never exposes the full source IP or raw User-Agent.
type auditEvent struct {
	ID           uint64         `json:"id"`
	Level        string         `json:"level"`
	Module       string         `json:"module"`
	TS           time.Time      `json:"ts"`
	Username     string         `json:"username"`
	Event        string         `json:"event"`
	EventType    string         `json:"event_type"`
	Outcome      string         `json:"outcome"`
	ResourceKind string         `json:"resource_kind,omitempty"`
	ResourceID   string         `json:"resource_id,omitempty"`
	SourceIP     string         `json:"source_ip,omitempty"`
	DeviceLabel  string         `json:"deviceLabel"`
	DeviceType   string         `json:"deviceType"`
	Payload      map[string]any `json:"payload,omitempty"`
}

type auditPage struct {
	Events []auditEvent `json:"events"`
	Total  int          `json:"total"`
	Limit  int          `json:"limit"`
	Offset int          `json:"offset"`
}

func sanitizeAuditPage(page eventlog.Page) auditPage {
	events := make([]auditEvent, 0, len(page.Events))
	for _, event := range page.Events {
		deviceLabel, deviceType := parseUA(event.UserAgent)
		events = append(events, auditEvent{
			ID: event.ID, Level: event.Level, Module: event.Module, TS: event.TS,
			Username: event.Username, Event: event.Event, EventType: event.EventType, Outcome: event.Outcome,
			ResourceKind: event.ResourceKind, ResourceID: event.ResourceID, SourceIP: maskIP(event.SourceIP),
			DeviceLabel: deviceLabel, DeviceType: deviceType, Payload: event.Payload,
		})
	}
	return auditPage{Events: events, Total: page.Total, Limit: page.Limit, Offset: page.Offset}
}
