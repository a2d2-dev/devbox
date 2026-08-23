// Package syslog provides the persistent structured event log used by the
// DevBox console. It intentionally keeps one store for both audit and system
// events so filtering, retention and redaction cannot diverge.
package syslog

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

const DefaultPath = "/var/lib/devbox/system-events.jsonl"

type Event struct {
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
	UserAgent    string         `json:"user_agent,omitempty"`
	Payload      map[string]any `json:"payload,omitempty"`
}

type Input struct {
	Level        string
	Module       string
	Username     string
	Event        string
	EventType    string
	Outcome      string
	ResourceKind string
	ResourceID   string
	SourceIP     string
	UserAgent    string
	Payload      map[string]any
}

type Query struct {
	Levels   []string
	Modules  []string
	Username string
	Since    time.Time
	Until    time.Time
	Limit    int
	Offset   int
}

type Page struct {
	Events []Event `json:"events"`
	Total  int     `json:"total"`
	Limit  int     `json:"limit"`
	Offset int     `json:"offset"`
}

type Store struct {
	mu     sync.RWMutex
	path   string
	events []Event
	nextID uint64
}

func New(path string) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("system log path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return nil, fmt.Errorf("create system log directory: %w", err)
	}
	s := &Store{path: path, nextID: 1}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) load() error {
	f, err := os.Open(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open system log: %w", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
	for scanner.Scan() {
		var event Event
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return fmt.Errorf("decode system log: %w", err)
		}
		s.events = append(s.events, event)
		if event.ID >= s.nextID {
			s.nextID = event.ID + 1
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read system log: %w", err)
	}
	return nil
}

func (s *Store) Append(input Input) (Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	event := s.makeEvent(input)
	if err := appendEvent(s.path, event); err != nil {
		return Event{}, err
	}
	s.events = append(s.events, event)
	return event, nil
}

func (s *Store) Query(query Query) Page {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query.Limit = clamp(query.Limit, 1, 200, 50)
	if query.Offset < 0 {
		query.Offset = 0
	}
	levels := stringSet(query.Levels)
	modules := stringSet(query.Modules)
	matched := make([]Event, 0, len(s.events))
	for i := len(s.events) - 1; i >= 0; i-- {
		event := s.events[i]
		if len(levels) > 0 && !levels[strings.ToLower(event.Level)] {
			continue
		}
		if len(modules) > 0 && !modules[strings.ToLower(event.Module)] {
			continue
		}
		if query.Username != "" && !strings.Contains(strings.ToLower(event.Username), strings.ToLower(query.Username)) {
			continue
		}
		if !query.Since.IsZero() && event.TS.Before(query.Since) {
			continue
		}
		if !query.Until.IsZero() && event.TS.After(query.Until) {
			continue
		}
		matched = append(matched, event)
	}

	page := Page{Events: []Event{}, Total: len(matched), Limit: query.Limit, Offset: query.Offset}
	if query.Offset >= len(matched) {
		return page
	}
	end := query.Offset + query.Limit
	if end > len(matched) {
		end = len(matched)
	}
	page.Events = append(page.Events, matched[query.Offset:end]...)
	return page
}

// Clear removes all events that predate a durable intent and atomically keeps
// both that intent and the successful result.
func (s *Store) Clear(intent Event, actor, sourceIP, userAgent string) (int, Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	foundIntent := false
	for _, event := range s.events {
		if event.ID == intent.ID && event.EventType == "LOG_CLEAR" && event.Outcome == "intent" {
			foundIntent = true
			break
		}
	}
	if !foundIntent {
		return 0, Event{}, errors.New("durable log clear intent is required")
	}
	cleared := len(s.events) - 1
	event := s.makeEvent(Input{
		Level: "warning", Module: "audit", Username: actor,
		Event: "清空系统日志", EventType: "LOG_CLEAR", Outcome: "success",
		ResourceKind: "system_log", ResourceID: "all", SourceIP: sourceIP, UserAgent: userAgent,
		Payload: map[string]any{"cleared_count": cleared},
	})
	if err := rewriteEvents(s.path, []Event{intent, event}); err != nil {
		return 0, Event{}, err
	}
	s.events = []Event{intent, event}
	return cleared, event, nil
}

func (s *Store) makeEvent(input Input) Event {
	event := Event{
		ID: s.nextID, Level: normalize(input.Level, "info"), Module: normalize(input.Module, "system"),
		TS: time.Now().UTC(), Username: clean(input.Username, "system"), Event: clean(input.Event, "系统事件"),
		EventType: clean(input.EventType, "SYSTEM_EVENT"), Outcome: normalize(input.Outcome, "success"),
		ResourceKind: clean(input.ResourceKind, ""), ResourceID: clean(input.ResourceID, ""),
		SourceIP: clean(input.SourceIP, ""), UserAgent: clean(input.UserAgent, ""), Payload: redactMap(input.Payload),
	}
	s.nextID++
	return event
}

func appendEvent(path string, event Event) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("open system log for append: %w", err)
	}
	defer f.Close()
	if err := f.Chmod(0600); err != nil {
		return fmt.Errorf("protect system log: %w", err)
	}
	if err := json.NewEncoder(f).Encode(event); err != nil {
		return fmt.Errorf("append system log: %w", err)
	}
	return f.Sync()
}

func rewriteEvents(path string, events []Event) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".system-events-*")
	if err != nil {
		return fmt.Errorf("create system log replacement: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return err
	}
	enc := json.NewEncoder(tmp)
	for _, event := range events {
		if err := enc.Encode(event); err != nil {
			tmp.Close()
			return err
		}
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace system log: %w", err)
	}
	return nil
}

var secretKey = regexp.MustCompile(`(?i)(password|passwd|secret|token|authorization|cookie|api[_-]?key|credential)`)
var inlineSecret = regexp.MustCompile(`(?i)(password|passwd|secret|token|authorization|cookie|api[_-]?key)=([^\s&]+)`)
var headerSecret = regexp.MustCompile(`(?im)\b(authorization|cookie|set-cookie)\s*:\s*[^\r\n]+`)

func redactMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]any, len(input))
	keys := make([]string, 0, len(input))
	for key := range input {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if secretKey.MatchString(key) {
			out[key] = "[REDACTED]"
			continue
		}
		out[key] = redactValue(input[key])
	}
	return out
}

func redactValue(value any) any {
	return redactReflect(reflect.ValueOf(value), 0)
}

func redactReflect(value reflect.Value, depth int) any {
	if !value.IsValid() || depth > 64 {
		return nil
	}
	for value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return nil
		}
		value = value.Elem()
	}
	switch value.Kind() {
	case reflect.Map:
		if value.IsNil() {
			return nil
		}
		out := make(map[string]any, value.Len())
		iter := value.MapRange()
		for iter.Next() {
			key := fmt.Sprint(iter.Key().Interface())
			if secretKey.MatchString(key) {
				out[key] = "[REDACTED]"
			} else {
				out[key] = redactReflect(iter.Value(), depth+1)
			}
		}
		return out
	case reflect.Struct:
		out := make(map[string]any, value.NumField())
		typeInfo := value.Type()
		for i := 0; i < value.NumField(); i++ {
			field := typeInfo.Field(i)
			if field.PkgPath != "" {
				continue
			}
			name := field.Name
			if tag := strings.Split(field.Tag.Get("json"), ",")[0]; tag == "-" {
				continue
			} else if tag != "" {
				name = tag
			}
			if secretKey.MatchString(name) {
				out[name] = "[REDACTED]"
			} else {
				out[name] = redactReflect(value.Field(i), depth+1)
			}
		}
		return out
	case reflect.Slice, reflect.Array:
		if value.Kind() == reflect.Slice && value.IsNil() {
			return nil
		}
		out := make([]any, value.Len())
		for i := 0; i < value.Len(); i++ {
			out[i] = redactReflect(value.Index(i), depth+1)
		}
		return out
	case reflect.String:
		redacted := inlineSecret.ReplaceAllString(value.String(), "$1=[REDACTED]")
		return headerSecret.ReplaceAllString(redacted, "$1: [REDACTED]")
	default:
		return value.Interface()
	}
}

func stringSet(values []string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, value := range values {
		if value = strings.ToLower(strings.TrimSpace(value)); value != "" {
			out[value] = true
		}
	}
	return out
}

func normalize(value, fallback string) string { return strings.ToLower(clean(value, fallback)) }

func clean(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	if len(value) > 2048 {
		return value[:2048]
	}
	return value
}

func clamp(value, min, max, fallback int) int {
	if value == 0 {
		return fallback
	}
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}
