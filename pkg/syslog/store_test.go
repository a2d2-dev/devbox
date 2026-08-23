package syslog

import (
	"os"
	"path/filepath"
	"testing"
)

type typedAuditPayload struct {
	Authorization string            `json:"authorization"`
	Metadata      map[string]string `json:"metadata"`
	Headers       []string          `json:"headers"`
}

func TestStoreQueryPaginationAndFilters(t *testing.T) {
	store, err := New(filepath.Join(t.TempDir(), "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []Input{
		{Level: "info", Module: "auth", Username: "alice", Event: "login", EventType: "LOGIN_SUCCESS"},
		{Level: "warning", Module: "supervisor", Username: "bob", Event: "stop", EventType: "SERVICE_STOP"},
		{Level: "error", Module: "auth", Username: "alice", Event: "failed", EventType: "LOGIN_FAILED"},
	} {
		if _, err := store.Append(input); err != nil {
			t.Fatal(err)
		}
	}

	page := store.Query(Query{Levels: []string{"error", "info"}, Modules: []string{"auth"}, Username: "ali", Limit: 1})
	if page.Total != 2 || len(page.Events) != 1 || page.Events[0].EventType != "LOGIN_FAILED" {
		t.Fatalf("unexpected first page: %#v", page)
	}
	page = store.Query(Query{Levels: []string{"error", "info"}, Modules: []string{"auth"}, Username: "ali", Limit: 1, Offset: 1})
	if len(page.Events) != 1 || page.Events[0].EventType != "LOGIN_SUCCESS" {
		t.Fatalf("unexpected second page: %#v", page)
	}
}

func TestStoreRedactsSensitivePayloadAndPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	store, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Append(Input{Payload: map[string]any{
		"password": "plain", "detail": "token=abc123 ok", "nested": map[string]any{"api_key": "key"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	reloaded, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	event := reloaded.Query(Query{}).Events[0]
	if event.Payload["password"] != "[REDACTED]" || event.Payload["detail"] != "token=[REDACTED] ok" {
		t.Fatalf("payload was not redacted: %#v", event.Payload)
	}
	nested := event.Payload["nested"].(map[string]any)
	if nested["api_key"] != "[REDACTED]" {
		t.Fatalf("nested payload was not redacted: %#v", nested)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("log mode = %v, err=%v", info.Mode().Perm(), err)
	}
}

func TestStoreRedactsTypedPayloadsAndHeaderValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	store, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Append(Input{Payload: map[string]any{
		"typed": typedAuditPayload{
			Authorization: "Bearer struct-secret",
			Metadata:      map[string]string{"api_key": "typed-secret", "safe": "visible"},
			Headers: []string{
				"Authorization: Bearer header-secret",
				"Cookie: session=header-secret",
				"Set-Cookie: session=header-secret; HttpOnly",
			},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}

	reloaded, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	typed := reloaded.Query(Query{}).Events[0].Payload["typed"].(map[string]any)
	if typed["authorization"] != "[REDACTED]" {
		t.Fatalf("struct authorization was not redacted: %#v", typed)
	}
	metadata := typed["metadata"].(map[string]any)
	if metadata["api_key"] != "[REDACTED]" || metadata["safe"] != "visible" {
		t.Fatalf("typed map was not recursively redacted: %#v", metadata)
	}
	for _, header := range typed["headers"].([]any) {
		if header != "Authorization: [REDACTED]" && header != "Cookie: [REDACTED]" && header != "Set-Cookie: [REDACTED]" {
			t.Fatalf("header was not redacted: %q", header)
		}
	}
}

func TestClearKeepsItsOwnAuditEvent(t *testing.T) {
	store, err := New(filepath.Join(t.TempDir(), "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	_, _ = store.Append(Input{Event: "one"})
	_, _ = store.Append(Input{Event: "two"})
	intent, err := store.Append(Input{
		Level: "warning", Module: "audit", Username: "admin", Event: "请求清空系统日志",
		EventType: "LOG_CLEAR", Outcome: "intent", ResourceKind: "system_log", ResourceID: "all",
	})
	if err != nil {
		t.Fatal(err)
	}
	cleared, event, err := store.Clear(intent, "admin", "10.0.0.1", "test")
	if err != nil {
		t.Fatal(err)
	}
	if cleared != 2 || event.EventType != "LOG_CLEAR" {
		t.Fatalf("cleared=%d event=%#v", cleared, event)
	}
	page := store.Query(Query{})
	if page.Total != 2 || page.Events[0].Outcome != "success" || page.Events[1].Outcome != "intent" || page.Events[0].Username != "admin" {
		t.Fatalf("unexpected events after clear: %#v", page)
	}
}
