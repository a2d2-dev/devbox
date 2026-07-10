package aiactivity

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type Snapshot struct {
	GeneratedAt time.Time      `json:"generatedAt"`
	Summary     Summary        `json:"summary"`
	Board       AgentBoard     `json:"board"`
	AgentTypes  []AgentType    `json:"agentTypes"`
	Processes   []AIProcess    `json:"processes"`
	Sessions    []AISession    `json:"sessions"`
	Workers     []ClaudeWorker `json:"workers"`
	Issues      []Issue        `json:"issues"`
}

type AgentStatus string

const (
	StatusWorking AgentStatus = "working"
	StatusBlocked AgentStatus = "blocked"
	StatusIdle    AgentStatus = "idle"
	StatusDone    AgentStatus = "done"
	StatusUnknown AgentStatus = "unknown"
)

const activeRecentWindow = 2 * time.Minute

var ErrTranscriptForbidden = errors.New("transcript path forbidden")

var transcriptRoots = []string{
	"/root/.claude",
	"/data/_ssd/_claude",
	"/root/.codex",
	"/data/_ssd/_codex",
}

type AgentBoard struct {
	Counts BoardCounts `json:"counts"`
	Agents []AgentCard `json:"agents"`
}

type BoardCounts struct {
	Working int `json:"working"`
	Blocked int `json:"blocked"`
	Idle    int `json:"idle"`
	Done    int `json:"done"`
	Unknown int `json:"unknown"`
}

type AgentCard struct {
	ID             string      `json:"id"`
	Kind           string      `json:"kind"`
	Name           string      `json:"name"`
	Cwd            string      `json:"cwd,omitempty"`
	GitBranch      string      `json:"gitBranch,omitempty"`
	Model          string      `json:"model,omitempty"`
	Status         AgentStatus `json:"status"`
	LastText       string      `json:"lastText,omitempty"`
	LastAt         time.Time   `json:"lastAt,omitempty"`
	TranscriptPath string      `json:"transcriptPath,omitempty"`
	SourceID       string      `json:"sourceId,omitempty"`
	PID            int         `json:"pid,omitempty"`
}

type TranscriptTail struct {
	Path  string   `json:"path"`
	Tail  int      `json:"tail"`
	Lines []string `json:"lines"`
	Text  string   `json:"text"`
}

type Summary struct {
	ClaudeProcesses int `json:"claudeProcesses"`
	CodexProcesses  int `json:"codexProcesses"`
	ClaudeSessions  int `json:"claudeSessions"`
	Workers         int `json:"workers"`
	RiskyWorkers    int `json:"riskyWorkers"`
	RateLimitEvents int `json:"rateLimitEvents"`
	ConfigFiles     int `json:"configFiles"`
	AgentTypes      int `json:"agentTypes"`
}

type AgentType struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Processes   []AIProcess    `json:"processes"`
	Sessions    []AISession    `json:"sessions"`
	Workers     []ClaudeWorker `json:"workers,omitempty"`
	Configs     []ConfigFile   `json:"configs"`
	Issues      []Issue        `json:"issues"`
}

type ConfigFile struct {
	Kind      string    `json:"kind"`
	Path      string    `json:"path"`
	Exists    bool      `json:"exists"`
	UpdatedAt time.Time `json:"updatedAt,omitempty"`
	SizeBytes int64     `json:"sizeBytes,omitempty"`
	Preview   string    `json:"preview,omitempty"`
}

type AIProcess struct {
	PID       int         `json:"pid"`
	PPID      int         `json:"ppid"`
	Kind      string      `json:"kind"`
	Name      string      `json:"name"`
	State     string      `json:"state"`
	Status    AgentStatus `json:"status"`
	AgeSec    int64       `json:"ageSec"`
	Cwd       string      `json:"cwd,omitempty"`
	Cmdline   string      `json:"cmdline"`
	Model     string      `json:"model,omitempty"`
	SessionID string      `json:"sessionId,omitempty"`
}

type AISession struct {
	ID           string      `json:"id"`
	Kind         string      `json:"kind"`
	Path         string      `json:"path"`
	Cwd          string      `json:"cwd,omitempty"`
	Model        string      `json:"model,omitempty"`
	LastPrompt   string      `json:"lastPrompt,omitempty"`
	LastError    string      `json:"lastError,omitempty"`
	RateLimited  bool        `json:"rateLimited"`
	UpdatedAt    time.Time   `json:"updatedAt"`
	Status       AgentStatus `json:"status"`
	SizeBytes    int64       `json:"sizeBytes"`
	LinkedPID    int         `json:"linkedPid,omitempty"`
	LinkedWorker string      `json:"linkedWorker,omitempty"`
	SessionKind  string      `json:"sessionKind,omitempty"`
	Entrypoint   string      `json:"entrypoint,omitempty"`
	GitBranch    string      `json:"gitBranch,omitempty"`
	MessagesSeen int         `json:"messagesSeen"`
}

type ClaudeWorker struct {
	Short        string      `json:"short"`
	PID          int         `json:"pid"`
	SessionID    string      `json:"sessionId,omitempty"`
	Cwd          string      `json:"cwd,omitempty"`
	Model        string      `json:"model,omitempty"`
	Source       string      `json:"source,omitempty"`
	Permission   string      `json:"permission,omitempty"`
	Effort       string      `json:"effort,omitempty"`
	StartedAt    time.Time   `json:"startedAt,omitempty"`
	Transcript   string      `json:"transcript,omitempty"`
	Risky        bool        `json:"risky"`
	LastTimeline string      `json:"lastTimeline,omitempty"`
	LastState    string      `json:"lastState,omitempty"`
	LastAt       time.Time   `json:"lastAt,omitempty"`
	Status       AgentStatus `json:"status"`
}

type Issue struct {
	Severity string `json:"severity"`
	Kind     string `json:"kind"`
	Title    string `json:"title"`
	Detail   string `json:"detail"`
	Ref      string `json:"ref,omitempty"`
}

type CleanupResult struct {
	Matched []AIProcess `json:"matched"`
	Termed  []int       `json:"termed"`
	Killed  []int       `json:"killed"`
	Failed  []KillError `json:"failed"`
}

type KillError struct {
	PID   int    `json:"pid"`
	Error string `json:"error"`
}

type Config struct {
	ClaudeRoots []string
	CodexRoots  []string
	MaxSessions int
}

func Collect(cfg Config) (*Snapshot, error) {
	if len(cfg.ClaudeRoots) == 0 {
		cfg.ClaudeRoots = []string{"/root/.claude", "/data/_ssd/_claude"}
	}
	if len(cfg.CodexRoots) == 0 {
		cfg.CodexRoots = []string{"/root/.codex", "/data/_ssd/_codex"}
	}
	if cfg.MaxSessions <= 0 {
		cfg.MaxSessions = 80
	}

	procs := collectProcesses()
	workers := collectClaudeWorkers(cfg.ClaudeRoots)
	sessions := collectClaudeSessions(cfg.ClaudeRoots, cfg.MaxSessions)
	openclawSessions := collectOpenClawSessions(cfg.MaxSessions)
	hermesSessions := collectHermesSessions(cfg.MaxSessions)
	configs := collectConfigFiles()
	linkSessions(sessions, procs, workers)
	normalizeProcesses(procs)
	normalizeWorkers(workers)
	normalizeSessions(sessions, workers)
	normalizeSessions(openclawSessions, workers)
	normalizeSessions(hermesSessions, workers)

	allSessions := append(append([]AISession{}, sessions...), append(openclawSessions, hermesSessions...)...)
	snap := &Snapshot{
		GeneratedAt: time.Now(),
		Processes:   procs,
		Sessions:    allSessions,
		Workers:     workers,
	}
	snap.AgentTypes = buildAgentTypes(procs, sessions, openclawSessions, hermesSessions, workers, configs)
	snap.Board = buildBoard(procs, allSessions, workers)
	snap.Summary.AgentTypes = len(snap.AgentTypes)
	snap.Summary.ClaudeSessions = len(sessions)
	snap.Summary.Workers = len(workers)
	for _, c := range configs {
		if c.Exists {
			snap.Summary.ConfigFiles++
		}
	}
	for _, p := range procs {
		switch p.Kind {
		case "claudecode":
			snap.Summary.ClaudeProcesses++
		case "codex":
			snap.Summary.CodexProcesses++
		}
	}
	seenWorkerIssue := map[string]bool{}
	for _, w := range workers {
		if w.Risky {
			snap.Summary.RiskyWorkers++
			key := w.Short + "|" + w.SessionID + "|" + w.Model
			if !seenWorkerIssue[key] {
				seenWorkerIssue[key] = true
				snap.Issues = append(snap.Issues, Issue{
					Severity: "critical",
					Kind:     "risky_model",
					Title:    "后台 Claude worker 使用高风险模型",
					Detail:   fmt.Sprintf("%s 使用 %s，cwd=%s", w.Short, w.Model, w.Cwd),
					Ref:      w.SessionID,
				})
			}
		}
		if isRateLimitText(w.LastTimeline) {
			snap.Summary.RateLimitEvents++
		}
	}
	for _, s := range snap.Sessions {
		if s.RateLimited {
			snap.Summary.RateLimitEvents++
			snap.Issues = append(snap.Issues, Issue{
				Severity: "warn",
				Kind:     "rate_limit",
				Title:    "最近会话出现限流或账号不可用",
				Detail:   s.LastError,
				Ref:      s.ID,
			})
		}
	}
	sort.Slice(snap.Issues, func(i, j int) bool {
		return severityRank(snap.Issues[i].Severity) > severityRank(snap.Issues[j].Severity)
	})
	snap.Issues = dedupeIssues(snap.Issues)
	return snap, nil
}

func buildAgentTypes(procs []AIProcess, claudeSessions, openclawSessions, hermesSessions []AISession, workers []ClaudeWorker, configs []ConfigFile) []AgentType {
	types := []AgentType{
		{ID: "claudecode", Name: "Claude Code", Description: "Claude Code CLI、daemon worker、后台 fleet/spare 会话"},
		{ID: "codex", Name: "Codex", Description: "OpenAI Codex CLI、app-server broker、会话索引"},
		{ID: "openclaw", Name: "OpenClaw", Description: "OpenClaw 多入口 agent、Feishu/skills/session 配置"},
		{ID: "hermes", Name: "Hermes", Description: "Hermes gateway、skills、cron、会话与日志"},
		{ID: "agent-browser", Name: "Agent Browser", Description: "浏览器验证和网页操作 agent 进程"},
		{ID: "other-agent", Name: "Other Agents", Description: "命令行中带 agent 语义但未归类的进程"},
	}
	idx := map[string]int{}
	for i := range types {
		idx[types[i].ID] = i
	}
	for _, p := range procs {
		if i, ok := idx[p.Kind]; ok {
			types[i].Processes = append(types[i].Processes, p)
		}
	}
	types[idx["claudecode"]].Sessions = claudeSessions
	types[idx["claudecode"]].Workers = workers
	types[idx["openclaw"]].Sessions = openclawSessions
	types[idx["hermes"]].Sessions = hermesSessions
	for _, c := range configs {
		if i, ok := idx[c.Kind]; ok {
			types[i].Configs = append(types[i].Configs, c)
		}
	}
	for _, w := range workers {
		if w.Risky {
			types[idx["claudecode"]].Issues = append(types[idx["claudecode"]].Issues, Issue{
				Severity: "critical",
				Kind:     "risky_model",
				Title:    "后台 worker 使用 fable 系模型",
				Detail:   fmt.Sprintf("%s: %s", w.Short, w.Model),
				Ref:      w.SessionID,
			})
		}
	}
	for i := range types {
		types[i].Issues = dedupeIssues(types[i].Issues)
	}
	out := types[:0]
	for _, t := range types {
		if len(t.Processes)+len(t.Sessions)+len(t.Workers)+len(t.Configs)+len(t.Issues) > 0 {
			out = append(out, t)
		}
	}
	return out
}

func collectProcesses() []AIProcess {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	boot := readBootTime()
	now := time.Now().Unix()
	var out []AIProcess
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		p, ok := readAIProcess(pid, boot, now)
		if ok {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].AgeSec > out[j].AgeSec
	})
	return out
}

func CleanupStaleCodexDeletedCWD() CleanupResult {
	matched := staleCodexDeletedCWD()
	res := CleanupResult{Matched: matched}
	for _, p := range matched {
		if err := syscall.Kill(p.PID, syscall.SIGTERM); err != nil && err != syscall.ESRCH {
			res.Failed = append(res.Failed, KillError{PID: p.PID, Error: err.Error()})
			continue
		}
		res.Termed = append(res.Termed, p.PID)
	}
	time.Sleep(3 * time.Second)
	for _, p := range matched {
		if !pidAlive(p.PID) {
			continue
		}
		if err := syscall.Kill(p.PID, syscall.SIGKILL); err != nil && err != syscall.ESRCH {
			res.Failed = append(res.Failed, KillError{PID: p.PID, Error: err.Error()})
			continue
		}
		res.Killed = append(res.Killed, p.PID)
	}
	return res
}

func staleCodexDeletedCWD() []AIProcess {
	procs := collectProcesses()
	var out []AIProcess
	for _, p := range procs {
		if p.Kind != "codex" {
			continue
		}
		if !strings.Contains(p.Cwd, " (deleted)") {
			continue
		}
		if strings.Contains(p.Cmdline, "app-server") || strings.Contains(p.Cmdline, "app-server-broker.mjs") {
			out = append(out, p)
		}
	}
	return out
}

func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

func readAIProcess(pid int, boot, now int64) (AIProcess, bool) {
	var p AIProcess
	p.PID = pid
	statB, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return p, false
	}
	stat := string(statB)
	openIdx := strings.IndexByte(stat, '(')
	closeIdx := strings.LastIndexByte(stat, ')')
	if openIdx < 0 || closeIdx <= openIdx {
		return p, false
	}
	p.Name = stat[openIdx+1 : closeIdx]
	rest := strings.Fields(stat[closeIdx+2:])
	if len(rest) > 19 {
		p.State = rest[0]
		p.PPID, _ = strconv.Atoi(rest[1])
		if boot > 0 {
			startClk, _ := strconv.ParseInt(rest[19], 10, 64)
			startUnix := boot + startClk/100
			if startUnix > 0 {
				p.AgeSec = now - startUnix
			}
		}
	}
	if b, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid)); err == nil {
		p.Cmdline = strings.ReplaceAll(strings.TrimRight(string(b), "\x00"), "\x00", " ")
	}
	needle := strings.ToLower(p.Name + " " + p.Cmdline)
	switch {
	case strings.Contains(needle, "codex"):
		p.Kind = "codex"
	case strings.Contains(needle, "claude.exe") ||
		strings.Contains(needle, " claude ") ||
		strings.HasPrefix(needle, "claude ") ||
		p.Name == "claude":
		p.Kind = "claudecode"
	case strings.Contains(needle, "hermes"):
		p.Kind = "hermes"
	case isAgentBrowserRootProcess(p.Name, p.Cmdline):
		p.Kind = "agent-browser"
	case isOpenClawProcess(p.Name, p.Cmdline):
		p.Kind = "openclaw"
	case strings.Contains(needle, " agent") || strings.Contains(needle, "/agent "):
		p.Kind = "other-agent"
	default:
		return p, false
	}
	if cwd, err := os.Readlink(fmt.Sprintf("/proc/%d/cwd", pid)); err == nil {
		p.Cwd = cwd
	}
	p.Model = flagValue(p.Cmdline, "--model")
	p.SessionID = flagValue(p.Cmdline, "--session-id")
	if p.SessionID == "" {
		p.SessionID = resumeSessionID(p.Cmdline)
	}
	return p, true
}

func isAgentBrowserRootProcess(name, cmdline string) bool {
	needle := strings.ToLower(name + " " + cmdline)
	if strings.Contains(needle, "chrome") || strings.Contains(needle, "crashpad") {
		return false
	}
	return strings.Contains(needle, "agent-browser-linux") ||
		strings.Contains(needle, "/agent-browser/bin/agent-browser")
}

func isOpenClawProcess(name, cmdline string) bool {
	if isCommandObserverProcess(name) {
		return false
	}
	needle := strings.ToLower(name + " " + cmdline)
	return strings.Contains(needle, "/data/_openclaw/agents") ||
		strings.Contains(needle, "/root/.openclaw/agents") ||
		strings.Contains(needle, "/data/_openclaw/workspace") ||
		strings.Contains(needle, "/root/.openclaw/workspace") ||
		strings.Contains(needle, "/data/_openclaw/environment") ||
		strings.Contains(needle, "/root/.openclaw/")
}

func isCommandObserverProcess(name string) bool {
	switch strings.ToLower(name) {
	case "bash", "sh", "zsh", "fish", "tmux", "tmux: client",
		"jq", "curl", "rg", "grep", "sed", "awk", "ps", "xargs":
		return true
	default:
		return false
	}
}

func collectClaudeWorkers(roots []string) []ClaudeWorker {
	var out []ClaudeWorker
	for _, root := range roots {
		path := filepath.Join(root, "daemon", "roster.json")
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var doc struct {
			Workers map[string]json.RawMessage `json:"workers"`
		}
		if json.Unmarshal(b, &doc) != nil {
			continue
		}
		for short, raw := range doc.Workers {
			var m map[string]any
			if json.Unmarshal(raw, &m) != nil {
				continue
			}
			w := ClaudeWorker{
				Short:     short,
				PID:       intFromAny(m["pid"]),
				SessionID: stringFromAny(m["sessionId"]),
				Cwd:       stringFromAny(m["cwd"]),
			}
			if ms := int64FromAny(m["startedAt"]); ms > 0 {
				w.StartedAt = time.UnixMilli(ms)
			}
			if dispatch, ok := m["dispatch"].(map[string]any); ok {
				w.Source = stringFromAny(dispatch["source"])
				if launch, ok := dispatch["launch"].(map[string]any); ok {
					w.Transcript = stringFromAny(launch["transcriptPath"])
					w.Model = flagValueFromList(launch["flagArgs"], "--model")
					w.Permission = flagValueFromList(launch["flagArgs"], "--permission-mode")
					w.Effort = flagValueFromList(launch["flagArgs"], "--effort")
				}
				if w.Model == "" {
					w.Model = flagValueFromList(dispatch["respawnFlags"], "--model")
				}
			}
			if w.Transcript != "" {
				w.Transcript = resolveClaudePath(w.Transcript, root)
			}
			w.Risky = isRiskyModel(w.Model)
			if line, at, state := lastTimeline(root, short); line != "" {
				w.LastTimeline = line
				w.LastAt = at
				w.LastState = state
			}
			out = append(out, w)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Short < out[j].Short
	})
	return out
}

func collectOpenClawSessions(limit int) []AISession {
	return collectGenericJSONLSessions("openclaw", []string{
		"/data/_openclaw/agents/*/sessions/*.jsonl",
		"/root/.openclaw/agents/*/sessions/*.jsonl",
	}, limit)
}

func collectHermesSessions(limit int) []AISession {
	var out []AISession
	for _, path := range []string{"/root/.hermes/sessions/sessions.json"} {
		if info, err := os.Stat(path); err == nil {
			out = append(out, AISession{
				ID:        "hermes-sessions",
				Kind:      "hermes",
				Path:      path,
				UpdatedAt: info.ModTime(),
				SizeBytes: info.Size(),
			})
		}
	}
	return out
}

func collectGenericJSONLSessions(kind string, patterns []string, limit int) []AISession {
	seen := map[string]bool{}
	var paths []string
	for _, pattern := range patterns {
		matches, _ := filepath.Glob(pattern)
		for _, path := range matches {
			if !seen[path] {
				seen[path] = true
				paths = append(paths, path)
			}
		}
	}
	sort.Slice(paths, func(i, j int) bool {
		return fileMTime(paths[i]).After(fileMTime(paths[j]))
	})
	if len(paths) > limit {
		paths = paths[:limit]
	}
	var out []AISession
	seenSession := map[string]bool{}
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		s := AISession{
			ID:        strings.TrimSuffix(filepath.Base(path), ".jsonl"),
			Kind:      kind,
			Path:      path,
			UpdatedAt: info.ModTime(),
			SizeBytes: info.Size(),
		}
		if lines, err := tailLines(path, 80); err == nil {
			for _, line := range lines {
				var m map[string]any
				if json.Unmarshal([]byte(line), &m) != nil {
					continue
				}
				if sid := stringFromAny(m["sessionId"]); sid != "" {
					s.ID = sid
				}
				if cwd := stringFromAny(m["cwd"]); cwd != "" {
					s.Cwd = cwd
				}
				if msg, ok := m["message"].(map[string]any); ok {
					if model := stringFromAny(msg["model"]); model != "" {
						s.Model = model
					}
					if s.LastPrompt == "" {
						s.LastPrompt = truncate(contentText(msg["content"]), 220)
					}
				}
				if isRateLimitText(line) {
					s.RateLimited = true
					s.LastError = truncate(line, 260)
				}
			}
		}
		key := s.Kind + "|" + s.ID
		if seenSession[key] {
			continue
		}
		seenSession[key] = true
		out = append(out, s)
	}
	return out
}

func collectConfigFiles() []ConfigFile {
	specs := []struct {
		kind    string
		pattern string
	}{
		{"claudecode", "/data/_ssd/_claude/daemon/roster.json"},
		{"claudecode", "/root/.claude/daemon/roster.json"},
		{"claudecode", "/root/.claude/settings*.json"},
		{"codex", "/data/_ssd/_codex/session_index.jsonl"},
		{"codex", "/data/_ssd/_codex/history.jsonl"},
		{"codex", "/root/.codex/config*.toml"},
		{"openclaw", "/data/_openclaw/openclaw.json"},
		{"openclaw", "/root/.openclaw/openclaw.json"},
		{"openclaw", "/data/_openclaw/agents/*/agent/models.json"},
		{"openclaw", "/data/_openclaw/agents/*/agent/auth-profiles.json"},
		{"openclaw", "/data/_openclaw/agents/*/sessions/sessions.json"},
		{"hermes", "/root/.hermes/config.yaml"},
		{"hermes", "/root/.hermes/gateway_state.json"},
		{"hermes", "/root/.hermes/processes.json"},
		{"hermes", "/root/.hermes/cron/jobs.json"},
		{"hermes", "/root/.hermes/sessions/sessions.json"},
	}
	var out []ConfigFile
	seen := map[string]bool{}
	for _, spec := range specs {
		matches, _ := filepath.Glob(spec.pattern)
		if len(matches) == 0 && !strings.ContainsAny(spec.pattern, "*?[") {
			matches = []string{spec.pattern}
		}
		for _, path := range matches {
			if seen[spec.kind+"|"+path] {
				continue
			}
			seen[spec.kind+"|"+path] = true
			out = append(out, readConfigFile(spec.kind, path))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind == out[j].Kind {
			return out[i].Path < out[j].Path
		}
		return out[i].Kind < out[j].Kind
	})
	return out
}

func readConfigFile(kind, path string) ConfigFile {
	c := ConfigFile{Kind: kind, Path: path}
	info, err := os.Stat(path)
	if err != nil {
		return c
	}
	c.Exists = true
	c.UpdatedAt = info.ModTime()
	c.SizeBytes = info.Size()
	c.Preview = redactPreview(path, 80)
	return c
}

func redactPreview(path string, maxLines int) string {
	lines, err := tailLines(path, maxLines)
	if err != nil {
		return ""
	}
	for i, line := range lines {
		lines[i] = redactLine(line)
	}
	return strings.Join(lines, "\n")
}

func redactLine(line string) string {
	lower := strings.ToLower(line)
	keys := []string{"api_key", "apikey", "auth", "authorization", "bearer", "token", "password", "secret", "key"}
	for _, key := range keys {
		if strings.Contains(lower, key) {
			if idx := strings.IndexAny(line, ":="); idx >= 0 {
				return line[:idx+1] + " \"***REDACTED***\""
			}
			return "***REDACTED***"
		}
	}
	return line
}

func collectClaudeSessions(roots []string, limit int) []AISession {
	var paths []string
	seen := map[string]bool{}
	for _, root := range roots {
		base := filepath.Join(root, "projects")
		filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
			if err != nil || d == nil || d.IsDir() || !strings.HasSuffix(path, ".jsonl") {
				return nil
			}
			if !seen[path] {
				seen[path] = true
				paths = append(paths, path)
			}
			return nil
		})
	}
	sort.Slice(paths, func(i, j int) bool {
		return fileMTime(paths[i]).After(fileMTime(paths[j]))
	})
	if len(paths) > limit {
		paths = paths[:limit]
	}
	out := make([]AISession, 0, len(paths))
	seenSession := map[string]bool{}
	for _, path := range paths {
		if s, ok := parseSession(path); ok {
			key := s.Kind + "|" + s.ID + "|" + s.Cwd
			if seenSession[key] {
				continue
			}
			seenSession[key] = true
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	return out
}

func parseSession(path string) (AISession, bool) {
	info, err := os.Stat(path)
	if err != nil {
		return AISession{}, false
	}
	s := AISession{
		ID:        strings.TrimSuffix(filepath.Base(path), ".jsonl"),
		Kind:      "claude",
		Path:      path,
		UpdatedAt: info.ModTime(),
		SizeBytes: info.Size(),
	}
	lines, err := tailLines(path, 250)
	if err != nil {
		return s, true
	}
	for _, line := range lines {
		var m map[string]any
		if json.Unmarshal([]byte(line), &m) != nil {
			continue
		}
		s.MessagesSeen++
		if sid := stringFromAny(m["sessionId"]); sid != "" {
			s.ID = sid
		}
		if cwd := stringFromAny(m["cwd"]); cwd != "" {
			s.Cwd = cwd
		}
		if k := stringFromAny(m["sessionKind"]); k != "" {
			s.SessionKind = k
		}
		if e := stringFromAny(m["entrypoint"]); e != "" {
			s.Entrypoint = e
		}
		if b := stringFromAny(m["gitBranch"]); b != "" {
			s.GitBranch = b
		}
		if lp := stringFromAny(m["lastPrompt"]); lp != "" {
			s.LastPrompt = truncate(lp, 220)
		}
		if msg, ok := m["message"].(map[string]any); ok {
			if model := stringFromAny(msg["model"]); model != "" && model != "<synthetic>" {
				s.Model = model
			}
			if role := stringFromAny(msg["role"]); role == "user" && s.LastPrompt == "" {
				s.LastPrompt = truncate(contentText(msg["content"]), 220)
			}
			text := contentText(msg["content"])
			if isRateLimitText(text) || boolFromAny(m["isApiErrorMessage"]) {
				s.RateLimited = true
				if text != "" {
					s.LastError = truncate(text, 260)
				}
			}
		}
		if errText := stringFromAny(m["error"]); errText != "" && s.LastError == "" {
			s.LastError = errText
		}
	}
	if s.LastError != "" && isRateLimitText(s.LastError) {
		s.RateLimited = true
	}
	return s, true
}

func linkSessions(sessions []AISession, procs []AIProcess, workers []ClaudeWorker) {
	pidBySession := map[string]int{}
	for _, p := range procs {
		if p.SessionID != "" {
			pidBySession[p.SessionID] = p.PID
		}
	}
	workerBySession := map[string]string{}
	modelBySession := map[string]string{}
	for _, w := range workers {
		if w.SessionID != "" {
			workerBySession[w.SessionID] = w.Short
			modelBySession[w.SessionID] = w.Model
		}
	}
	for i := range sessions {
		if pid := pidBySession[sessions[i].ID]; pid != 0 {
			sessions[i].LinkedPID = pid
		}
		if short := workerBySession[sessions[i].ID]; short != "" {
			sessions[i].LinkedWorker = short
		}
		if sessions[i].Model == "" && modelBySession[sessions[i].ID] != "" {
			sessions[i].Model = modelBySession[sessions[i].ID]
		}
	}
}

func normalizeProcesses(procs []AIProcess) {
	for i := range procs {
		procs[i].Status = processStatus(procs[i])
	}
}

func normalizeWorkers(workers []ClaudeWorker) {
	now := time.Now()
	for i := range workers {
		workers[i].Status = workerStatus(workers[i], now)
	}
}

func normalizeSessions(sessions []AISession, workers []ClaudeWorker) {
	workerStatusByShort := map[string]AgentStatus{}
	for _, w := range workers {
		if w.Short != "" {
			workerStatusByShort[w.Short] = w.Status
		}
	}
	now := time.Now()
	for i := range sessions {
		sessions[i].Status = sessionStatus(sessions[i], workerStatusByShort, now)
	}
}

func buildBoard(procs []AIProcess, sessions []AISession, workers []ClaudeWorker) AgentBoard {
	sessionByID := map[string]AISession{}
	for _, s := range sessions {
		if s.ID != "" {
			if prev, ok := sessionByID[s.ID]; !ok || s.UpdatedAt.After(prev.UpdatedAt) {
				sessionByID[s.ID] = s
			}
		}
	}

	var board AgentBoard
	for _, w := range workers {
		card := AgentCard{
			ID:             stableAgentID("worker", w.Short, w.SessionID, w.Transcript),
			Kind:           "worker",
			Name:           displayWorkerName(w),
			Cwd:            w.Cwd,
			Model:          w.Model,
			Status:         statusOrUnknown(w.Status),
			LastText:       w.LastTimeline,
			LastAt:         w.LastAt,
			TranscriptPath: allowedCardTranscriptPath(w.Transcript),
			SourceID:       w.Short,
			PID:            w.PID,
		}
		if s, ok := sessionByID[w.SessionID]; ok {
			if card.Cwd == "" {
				card.Cwd = s.Cwd
			}
			card.GitBranch = s.GitBranch
			if card.Model == "" {
				card.Model = s.Model
			}
			if card.LastText == "" {
				card.LastText = firstNonEmpty(s.LastError, s.LastPrompt)
			}
			if card.LastAt.IsZero() {
				card.LastAt = s.UpdatedAt
			}
			if card.TranscriptPath == "" {
				card.TranscriptPath = allowedCardTranscriptPath(s.Path)
			}
		}
		board.add(card)
	}
	for _, p := range procs {
		s := sessionByID[p.SessionID]
		card := AgentCard{
			ID:             stableAgentID("process", strconv.Itoa(p.PID), p.SessionID, p.Cwd),
			Kind:           "process",
			Name:           displayProcessName(p),
			Cwd:            firstNonEmpty(p.Cwd, s.Cwd),
			GitBranch:      s.GitBranch,
			Model:          firstNonEmpty(p.Model, s.Model),
			Status:         statusOrUnknown(p.Status),
			LastText:       firstNonEmpty(s.LastError, s.LastPrompt, p.Cmdline),
			LastAt:         s.UpdatedAt,
			TranscriptPath: allowedCardTranscriptPath(s.Path),
			SourceID:       p.SessionID,
			PID:            p.PID,
		}
		board.add(card)
	}
	for _, s := range sessions {
		card := AgentCard{
			ID:             stableAgentID("session", s.Kind, s.ID, s.Path),
			Kind:           "session",
			Name:           displaySessionName(s),
			Cwd:            s.Cwd,
			GitBranch:      s.GitBranch,
			Model:          s.Model,
			Status:         statusOrUnknown(s.Status),
			LastText:       firstNonEmpty(s.LastError, s.LastPrompt),
			LastAt:         s.UpdatedAt,
			TranscriptPath: allowedCardTranscriptPath(s.Path),
			SourceID:       s.ID,
			PID:            s.LinkedPID,
		}
		board.add(card)
	}
	sort.SliceStable(board.Agents, func(i, j int) bool {
		if statusRank(board.Agents[i].Status) != statusRank(board.Agents[j].Status) {
			return statusRank(board.Agents[i].Status) < statusRank(board.Agents[j].Status)
		}
		return board.Agents[i].LastAt.After(board.Agents[j].LastAt)
	})
	return board
}

func (b *AgentBoard) add(card AgentCard) {
	card.Status = statusOrUnknown(card.Status)
	switch card.Status {
	case StatusWorking:
		b.Counts.Working++
	case StatusBlocked:
		b.Counts.Blocked++
	case StatusIdle:
		b.Counts.Idle++
	case StatusDone:
		b.Counts.Done++
	default:
		b.Counts.Unknown++
	}
	b.Agents = append(b.Agents, card)
}

// Status mapping intentionally uses only observable activity fields:
// RateLimited/LastState/timeline text for blocked signals, process State/PID for
// liveness, LastAt/UpdatedAt within activeRecentWindow for recent activity, and
// missing live linkage for done sessions. It does not infer by agent type or use
// mock fallback data.
func processStatus(p AIProcess) AgentStatus {
	switch strings.ToUpper(strings.TrimSpace(p.State)) {
	case "R", "D":
		return StatusWorking
	case "S", "I":
		return StatusIdle
	case "Z", "X":
		return StatusDone
	case "":
		return StatusUnknown
	default:
		if p.PID > 0 {
			return StatusIdle
		}
		return StatusUnknown
	}
}

func workerStatus(w ClaudeWorker, now time.Time) AgentStatus {
	text := strings.Join([]string{w.LastState, w.LastTimeline}, " ")
	if hasBlockedSignal(text) {
		return StatusBlocked
	}
	if hasDoneSignal(w.LastState) {
		return StatusDone
	}
	if hasWorkingSignal(w.LastState) || recentAt(w.LastAt, now) {
		return StatusWorking
	}
	if w.PID > 0 {
		if pidAlive(w.PID) {
			return StatusIdle
		}
		return StatusDone
	}
	if !w.LastAt.IsZero() {
		return StatusDone
	}
	return StatusUnknown
}

func sessionStatus(s AISession, workerStatusByShort map[string]AgentStatus, now time.Time) AgentStatus {
	if s.RateLimited || hasBlockedSignal(s.LastError) {
		return StatusBlocked
	}
	if s.LinkedWorker != "" {
		switch workerStatusByShort[s.LinkedWorker] {
		case StatusBlocked, StatusWorking, StatusIdle, StatusDone:
			return workerStatusByShort[s.LinkedWorker]
		}
	}
	if s.LinkedPID > 0 {
		if !pidAlive(s.LinkedPID) {
			return StatusDone
		}
		if recentAt(s.UpdatedAt, now) {
			return StatusWorking
		}
		return StatusIdle
	}
	if recentAt(s.UpdatedAt, now) {
		return StatusWorking
	}
	if !s.UpdatedAt.IsZero() {
		return StatusDone
	}
	return StatusUnknown
}

func hasBlockedSignal(text string) bool {
	t := strings.ToLower(text)
	return strings.Contains(t, "waiting") ||
		strings.Contains(t, "blocked") ||
		strings.Contains(t, "error") ||
		isRateLimitText(t)
}

func hasWorkingSignal(state string) bool {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "working", "running", "active", "busy", "in_progress", "in-progress", "started", "processing":
		return true
	default:
		return false
	}
}

func hasDoneSignal(state string) bool {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "done", "complete", "completed", "success", "succeeded", "finished", "exited", "cancelled", "canceled":
		return true
	default:
		return false
	}
}

func recentAt(at, now time.Time) bool {
	return !at.IsZero() && now.Sub(at) >= 0 && now.Sub(at) <= activeRecentWindow
}

func statusOrUnknown(status AgentStatus) AgentStatus {
	switch status {
	case StatusWorking, StatusBlocked, StatusIdle, StatusDone:
		return status
	default:
		return StatusUnknown
	}
}

func statusRank(status AgentStatus) int {
	switch status {
	case StatusWorking:
		return 0
	case StatusBlocked:
		return 1
	case StatusIdle:
		return 2
	case StatusDone:
		return 3
	default:
		return 4
	}
}

func stableAgentID(parts ...string) string {
	h := fnv.New64a()
	for _, part := range parts {
		_, _ = h.Write([]byte(part))
		_, _ = h.Write([]byte{0})
	}
	return fmt.Sprintf("%s:%x", parts[0], h.Sum64())
}

func displayWorkerName(w ClaudeWorker) string {
	if w.Short != "" {
		return "Claude worker " + w.Short
	}
	if w.SessionID != "" {
		return "Claude worker " + w.SessionID
	}
	return "Claude worker"
}

func displayProcessName(p AIProcess) string {
	name := p.Kind
	if name == "" {
		name = p.Name
	}
	if p.PID > 0 {
		return fmt.Sprintf("%s PID %d", name, p.PID)
	}
	return name
}

func displaySessionName(s AISession) string {
	if s.LastPrompt != "" {
		return truncate(s.LastPrompt, 54)
	}
	if s.LastError != "" {
		return truncate(s.LastError, 54)
	}
	if s.ID != "" {
		return s.Kind + " " + s.ID
	}
	return s.Kind + " session"
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func allowedCardTranscriptPath(path string) string {
	clean, ok := allowedTranscriptPath(path)
	if !ok {
		return ""
	}
	return clean
}

func ReadTranscriptTail(path string, maxLines int) (TranscriptTail, error) {
	clean, ok := allowedTranscriptPath(path)
	if !ok {
		return TranscriptTail{}, ErrTranscriptForbidden
	}
	if maxLines <= 0 {
		maxLines = 200
	}
	if maxLines > 1000 {
		maxLines = 1000
	}
	lines, err := tailLines(clean, maxLines)
	if err != nil {
		return TranscriptTail{}, err
	}
	return TranscriptTail{
		Path:  clean,
		Tail:  maxLines,
		Lines: lines,
		Text:  strings.Join(lines, "\n"),
	}, nil
}

func allowedTranscriptPath(path string) (string, bool) {
	if path == "" || !filepath.IsAbs(path) {
		return "", false
	}
	clean := filepath.Clean(path)
	if !isTranscriptFile(clean) {
		return "", false
	}
	if resolved, err := filepath.EvalSymlinks(clean); err == nil {
		clean = filepath.Clean(resolved)
		if !isTranscriptFile(clean) {
			return "", false
		}
	}
	for _, root := range transcriptAllowedRoots() {
		if pathWithinRoot(clean, root) {
			return clean, true
		}
	}
	return "", false
}

func transcriptAllowedRoots() []string {
	return transcriptRoots
}

func isTranscriptFile(path string) bool {
	return strings.HasSuffix(filepath.Base(path), ".jsonl")
}

func pathWithinRoot(path, root string) bool {
	root = filepath.Clean(root)
	return path == root || strings.HasPrefix(path, root+string(os.PathSeparator))
}

func tailLines(path string, maxLines int) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	const maxBytes int64 = 512 * 1024
	start := info.Size() - maxBytes
	if start < 0 {
		start = 0
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return nil, err
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)
	var lines []string
	for sc.Scan() {
		line := sc.Text()
		if start > 0 && len(lines) == 0 {
			lines = append(lines, line)
			continue
		}
		lines = append(lines, line)
		if len(lines) > maxLines {
			lines = lines[len(lines)-maxLines:]
		}
	}
	return lines, sc.Err()
}

func lastTimeline(root, short string) (string, time.Time, string) {
	path := filepath.Join(root, "jobs", short, "timeline.jsonl")
	lines, err := tailLines(path, 10)
	if err != nil || len(lines) == 0 {
		return "", time.Time{}, ""
	}
	line := lines[len(lines)-1]
	var m map[string]any
	if json.Unmarshal([]byte(line), &m) != nil {
		return truncate(line, 260), time.Time{}, ""
	}
	at, _ := time.Parse(time.RFC3339Nano, stringFromAny(m["at"]))
	text := stringFromAny(m["text"])
	if text == "" {
		text = stringFromAny(m["detail"])
	}
	return truncate(text, 260), at, stringFromAny(m["state"])
}

func contentText(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case []any:
		var parts []string
		for _, item := range x {
			if m, ok := item.(map[string]any); ok {
				if t := stringFromAny(m["text"]); t != "" {
					parts = append(parts, t)
				}
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

func flagValue(cmd, flag string) string {
	parts := strings.Fields(cmd)
	for i := 0; i < len(parts)-1; i++ {
		if parts[i] == flag {
			return strings.Trim(parts[i+1], `"'`)
		}
	}
	return ""
}

func flagValueFromList(v any, flag string) string {
	list, ok := v.([]any)
	if !ok {
		return ""
	}
	for i := 0; i < len(list)-1; i++ {
		if stringFromAny(list[i]) == flag {
			return stringFromAny(list[i+1])
		}
	}
	return ""
}

func resumeSessionID(cmd string) string {
	parts := strings.Fields(cmd)
	for i := 0; i < len(parts)-1; i++ {
		if parts[i] == "--resume" {
			base := filepath.Base(parts[i+1])
			return strings.TrimSuffix(base, ".jsonl")
		}
	}
	return ""
}

func readBootTime() int64 {
	b, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "btime ") {
			t, _ := strconv.ParseInt(strings.TrimPrefix(line, "btime "), 10, 64)
			return t
		}
	}
	return 0
}

func resolveClaudePath(path, root string) string {
	if _, err := os.Stat(path); err == nil {
		return path
	}
	if strings.HasPrefix(path, "/root/.claude/") && root != "/root/.claude" {
		alt := filepath.Join(root, strings.TrimPrefix(path, "/root/.claude/"))
		if _, err := os.Stat(alt); err == nil {
			return alt
		}
	}
	return path
}

func fileMTime(path string) time.Time {
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

func isRiskyModel(model string) bool {
	m := strings.ToLower(model)
	return strings.Contains(m, "fable")
}

func isRateLimitText(text string) bool {
	t := strings.ToLower(text)
	return strings.Contains(t, "rate limit") ||
		strings.Contains(t, "request rejected (429)") ||
		strings.Contains(t, "http 429") ||
		strings.Contains(t, "status 429") ||
		strings.Contains(t, "status\":429") ||
		strings.Contains(t, "no available accounts") ||
		strings.Contains(t, "upstream rate limit")
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\x00", " "))
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func severityRank(s string) int {
	switch s {
	case "critical":
		return 3
	case "warn":
		return 2
	default:
		return 1
	}
}

func dedupeIssues(in []Issue) []Issue {
	seen := map[string]bool{}
	out := in[:0]
	for _, it := range in {
		key := it.Severity + "|" + it.Kind + "|" + it.Title + "|" + it.Ref + "|" + it.Detail
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, it)
	}
	return out
}

func stringFromAny(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case fmt.Stringer:
		return x.String()
	default:
		return ""
	}
}

func boolFromAny(v any) bool {
	b, _ := v.(bool)
	return b
}

func intFromAny(v any) int {
	return int(int64FromAny(v))
}

func int64FromAny(v any) int64 {
	switch x := v.(type) {
	case float64:
		return int64(x)
	case int64:
		return x
	case int:
		return int64(x)
	case json.Number:
		n, _ := x.Int64()
		return n
	default:
		return 0
	}
}
