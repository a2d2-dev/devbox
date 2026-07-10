package aiactivity

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestReadTranscriptTailRejectsForbiddenPaths(t *testing.T) {
	for _, path := range []string{
		"/tmp/devbox-secret.jsonl",
		"/root/.codex/config.toml",
		"/root/.claude/settings.json",
		"/root/.claude/../etc/passwd",
		"/root/.claude-evil/x.jsonl",
		"/root/.codex/session.txt",
	} {
		t.Run(path, func(t *testing.T) {
			if clean, ok := allowedTranscriptPath(path); ok {
				t.Fatalf("allowedTranscriptPath(%q) = %q, true; want forbidden", path, clean)
			}
			_, err := ReadTranscriptTail(path, 10)
			if !errors.Is(err, ErrTranscriptForbidden) {
				t.Fatalf("ReadTranscriptTail(%q) error = %v, want ErrTranscriptForbidden", path, err)
			}
		})
	}
}

func TestReadTranscriptTailAllowsOnlyJsonlInsideRoot(t *testing.T) {
	root := t.TempDir()
	withTranscriptRoots(t, root)

	transcript := filepath.Join(root, "sessions", "ok.jsonl")
	if err := os.MkdirAll(filepath.Dir(transcript), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(transcript, []byte("{\"type\":\"message\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	clean, ok := allowedTranscriptPath(transcript)
	if !ok {
		t.Fatalf("allowedTranscriptPath(%q) rejected valid transcript", transcript)
	}
	if clean != transcript {
		t.Fatalf("allowedTranscriptPath(%q) = %q, want %q", transcript, clean, transcript)
	}
	tail, err := ReadTranscriptTail(transcript, 10)
	if err != nil {
		t.Fatalf("ReadTranscriptTail(%q) error = %v", transcript, err)
	}
	if len(tail.Lines) != 1 || tail.Lines[0] != "{\"type\":\"message\"}" {
		t.Fatalf("tail lines = %#v, want one transcript line", tail.Lines)
	}

	nonTranscript := filepath.Join(root, "sessions", "settings.json")
	if clean, ok := allowedTranscriptPath(nonTranscript); ok {
		t.Fatalf("allowedTranscriptPath(%q) = %q, true; want forbidden", nonTranscript, clean)
	}
}

func TestReadTranscriptTailRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	withTranscriptRoots(t, root)

	outside := filepath.Join(t.TempDir(), "outside.jsonl")
	if err := os.WriteFile(outside, []byte("{\"secret\":true}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "linked.jsonl")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}

	if clean, ok := allowedTranscriptPath(link); ok {
		t.Fatalf("allowedTranscriptPath(%q) = %q, true; want symlink escape forbidden", link, clean)
	}
	_, err := ReadTranscriptTail(link, 10)
	if !errors.Is(err, ErrTranscriptForbidden) {
		t.Fatalf("ReadTranscriptTail(%q) error = %v, want ErrTranscriptForbidden", link, err)
	}
}

func TestStatusMapping(t *testing.T) {
	now := time.Now()
	livePID := os.Getpid()
	deadPID := exitedPID(t)

	workerCases := []struct {
		name string
		in   ClaudeWorker
		want AgentStatus
	}{
		{name: "waiting state blocked", in: ClaudeWorker{LastState: "waiting_for_account"}, want: StatusBlocked},
		{name: "blocked state blocked", in: ClaudeWorker{LastState: "blocked_on_tool"}, want: StatusBlocked},
		{name: "error state blocked", in: ClaudeWorker{LastState: "error: auth failed"}, want: StatusBlocked},
		{name: "working state working", in: ClaudeWorker{LastState: "working"}, want: StatusWorking},
		{name: "empty state live recent working", in: ClaudeWorker{PID: livePID, LastAt: now.Add(-activeRecentWindow)}, want: StatusWorking},
		{name: "empty state live stale idle", in: ClaudeWorker{PID: livePID, LastAt: now.Add(-activeRecentWindow - time.Nanosecond)}, want: StatusIdle},
		{name: "dead pid done", in: ClaudeWorker{PID: deadPID}, want: StatusDone},
	}
	for _, tc := range workerCases {
		t.Run("worker "+tc.name, func(t *testing.T) {
			if got := workerStatus(tc.in, now); got != tc.want {
				t.Fatalf("workerStatus() = %s, want %s", got, tc.want)
			}
		})
	}

	sessionCases := []struct {
		name string
		in   AISession
		want AgentStatus
	}{
		{name: "rate limited blocked", in: AISession{RateLimited: true}, want: StatusBlocked},
		{name: "last error waiting blocked", in: AISession{LastError: "waiting for account"}, want: StatusBlocked},
		{name: "live recent working", in: AISession{LinkedPID: livePID, UpdatedAt: now.Add(-activeRecentWindow)}, want: StatusWorking},
		{name: "live stale idle", in: AISession{LinkedPID: livePID, UpdatedAt: now.Add(-activeRecentWindow - time.Nanosecond)}, want: StatusIdle},
		{name: "dead pid done", in: AISession{LinkedPID: deadPID, UpdatedAt: now}, want: StatusDone},
		{name: "unlinked old done", in: AISession{UpdatedAt: now.Add(-time.Hour)}, want: StatusDone},
	}
	for _, tc := range sessionCases {
		t.Run("session "+tc.name, func(t *testing.T) {
			if got := sessionStatus(tc.in, nil, now); got != tc.want {
				t.Fatalf("sessionStatus() = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestBuildBoardOnlySetsAllowedTranscriptPath(t *testing.T) {
	root := t.TempDir()
	withTranscriptRoots(t, root)

	valid := filepath.Join(root, "valid.jsonl")
	if err := os.WriteFile(valid, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(root, "config.toml")
	outside := filepath.Join(t.TempDir(), "outside.jsonl")
	now := time.Now()

	board := buildBoard(nil, []AISession{
		{ID: "valid-session", Kind: "codex", Path: valid, Status: StatusDone, UpdatedAt: now},
		{ID: "config-session", Kind: "codex", Path: config, Status: StatusDone, UpdatedAt: now},
		{ID: "outside-session", Kind: "openclaw", Path: outside, Status: StatusDone, UpdatedAt: now},
	}, []ClaudeWorker{
		{Short: "valid-worker", Transcript: valid, Status: StatusIdle},
		{Short: "invalid-worker", Transcript: config, Status: StatusIdle},
	})

	want := map[string]string{
		"valid-session":   valid,
		"config-session":  "",
		"outside-session": "",
		"valid-worker":    valid,
		"invalid-worker":  "",
	}
	for _, card := range board.Agents {
		wantPath, ok := want[card.SourceID]
		if !ok {
			continue
		}
		if card.TranscriptPath != wantPath {
			t.Fatalf("card %s transcriptPath = %q, want %q", card.SourceID, card.TranscriptPath, wantPath)
		}
		delete(want, card.SourceID)
	}
	if len(want) != 0 {
		t.Fatalf("missing expected cards: %#v", want)
	}
}

func withTranscriptRoots(t *testing.T, roots ...string) {
	t.Helper()
	old := transcriptRoots
	transcriptRoots = roots
	t.Cleanup(func() {
		transcriptRoots = old
	})
}

func exitedPID(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("sh", "-c", "exit 0")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pid := cmd.Process.Pid
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	if pidAlive(pid) {
		t.Fatalf("test helper process pid %d is still alive", pid)
	}
	return pid
}
