package downloads

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestPauseResumeWithHTTPRange(t *testing.T) {
	payload := bytes.Repeat([]byte("devbox-download-range-test\n"), 64*1024)
	var rangeRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := int64(0)
		if header := r.Header.Get("Range"); header != "" {
			rangeRequests.Add(1)
			if _, err := fmt.Sscanf(header, "bytes=%d-", &start); err != nil {
				http.Error(w, "bad range", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(payload)-1, len(payload)))
		}
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)-int(start)))
		if start > 0 {
			w.WriteHeader(http.StatusPartialContent)
		}
		for pos := int(start); pos < len(payload); pos += 16 * 1024 {
			end := pos + 16*1024
			if end > len(payload) {
				end = len(payload)
			}
			if _, err := w.Write(payload[pos:end]); err != nil {
				return
			}
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
			time.Sleep(2 * time.Millisecond)
		}
	}))
	defer server.Close()

	root := t.TempDir()
	engine, err := New(Config{RootDir: root, MaxConcurrent: 2})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	engine.Start(ctx)
	task, err := engine.Add(AddRequest{URL: server.URL + "/payload.bin", TargetDirectory: "downloads"})
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != StatusWaiting {
		t.Fatalf("new task status = %s", task.Status)
	}
	if _, err := engine.Enqueue(task.ID); err != nil {
		t.Fatal(err)
	}
	waitTask(t, engine, task.ID, func(task Task) bool { return task.DownloadedBytes > 128*1024 })
	paused, err := engine.Pause(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if paused.Status != StatusPaused {
		t.Fatalf("paused task status = %s", paused.Status)
	}
	partialInfo, err := os.Stat(paused.Destination + ".part")
	if err != nil || partialInfo.Size() == 0 {
		t.Fatalf("partial file not preserved: info=%v err=%v", partialInfo, err)
	}
	if _, err := engine.StartTask(task.ID); err != nil {
		t.Fatal(err)
	}
	completed := waitTask(t, engine, task.ID, func(task Task) bool { return task.Status == StatusCompleted })
	if !completed.ResumeSupported || rangeRequests.Load() == 0 {
		t.Fatalf("resume was not observed: supported=%v range requests=%d", completed.ResumeSupported, rangeRequests.Load())
	}
	got, err := os.ReadFile(completed.Destination)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("downloaded payload differs: got=%d want=%d", len(got), len(payload))
	}
	if completed.DownloadedBytes != int64(len(payload)) || completed.TotalBytes != int64(len(payload)) {
		t.Fatalf("completed progress = %d/%d", completed.DownloadedBytes, completed.TotalBytes)
	}
	if completed.StartedAt == nil || completed.CompletedAt == nil || !completed.StartedAt.Before(*completed.CompletedAt) {
		t.Fatalf("invalid task timestamps: started=%v completed=%v", completed.StartedAt, completed.CompletedAt)
	}
	if _, err := os.Stat(completed.Destination + ".part"); !os.IsNotExist(err) {
		t.Fatalf("partial file should be renamed, got %v", err)
	}
}

func TestCanceledTransferProgressStillCountsTraffic(t *testing.T) {
	protocol := &canceledProgressProtocol{started: make(chan struct{})}
	engine, err := New(Config{RootDir: t.TempDir(), Protocols: []Protocol{protocol}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	engine.Start(ctx)
	task, err := engine.Add(AddRequest{URL: "https://example.com/late-progress.bin"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Enqueue(task.ID); err != nil {
		t.Fatal(err)
	}
	<-protocol.started
	if _, err := engine.Pause(task.ID); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if engine.Snapshot().Statistics.TotalDownloadedBytes == 64 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("late transfer bytes were not counted: %+v", engine.Snapshot().Statistics)
}

func TestHTTPResumeRejectsMismatchedContentRange(t *testing.T) {
	payload := []byte("complete remote payload")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") == "" {
			t.Fatal("resume request did not include Range")
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes 0-%d/%d", len(payload)-1, len(payload)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	partial := filepath.Join(t.TempDir(), "payload.part")
	original := []byte("partial")
	if err := os.WriteFile(partial, original, 0o640); err != nil {
		t.Fatal(err)
	}
	protocol := &HTTPProtocol{Client: server.Client()}
	_, err := protocol.Download(context.Background(), TransferRequest{
		URL: server.URL, PartialPath: partial, Offset: int64(len(original)),
	}, func(TransferProgress) {})
	if err == nil || !strings.Contains(err.Error(), "invalid Content-Range") {
		t.Fatalf("expected invalid Content-Range error, got %v", err)
	}
	got, readErr := os.ReadFile(partial)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !bytes.Equal(got, original) {
		t.Fatalf("partial file changed after rejected resume: got %q want %q", got, original)
	}
}

func TestRestartRestoresIncompleteTaskAsPaused(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "state.json")
	now := time.Now().UTC()
	state := persistedState{Version: 1, Tasks: []*Task{{
		ID: "running-task", URL: "https://example.com/file.bin", Name: "file.bin",
		TargetDirectory: "downloads", Destination: filepath.Join(root, "downloads", "file.bin"),
		Status: StatusDownloading, DownloadedBytes: 42, SpeedBytesPerSec: 100,
		CreatedAt: now, UpdatedAt: now,
	}}}
	if err := saveState(statePath, state); err != nil {
		t.Fatal(err)
	}
	engine, err := New(Config{RootDir: root, StatePath: statePath})
	if err != nil {
		t.Fatal(err)
	}
	task, err := engine.Get("running-task")
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != StatusPaused || task.DownloadedBytes != 42 || task.SpeedBytesPerSec != 0 {
		t.Fatalf("unexpected restored task: %+v", task)
	}
}

func TestDeleteKeepsOrRemovesFile(t *testing.T) {
	root := t.TempDir()
	engine, err := New(Config{RootDir: root})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	engine.Start(ctx)

	for _, removeFile := range []bool{false, true} {
		name := fmt.Sprintf("file-%v.bin", removeFile)
		task, err := engine.Add(AddRequest{URL: "https://example.com/" + name, TargetDirectory: "downloads"})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(task.Destination+".part", []byte("partial"), 0o640); err != nil {
			t.Fatal(err)
		}
		if err := engine.Delete(task.ID, removeFile); err != nil {
			t.Fatal(err)
		}
		_, err = os.Stat(task.Destination + ".part")
		if removeFile && !os.IsNotExist(err) {
			t.Fatalf("deleteFile=true should remove partial file: %v", err)
		}
		if !removeFile && err != nil {
			t.Fatalf("deleteFile=false should preserve partial file: %v", err)
		}
		if !removeFile {
			_, err := engine.Add(AddRequest{URL: "https://example.com/" + name, TargetDirectory: "downloads"})
			if !errors.Is(err, ErrTaskConflict) {
				t.Fatalf("orphan partial file must block a same-name task, got %v", err)
			}
		}
	}
}

func TestStateMachineRejectsInvalidTransitions(t *testing.T) {
	engine, err := New(Config{RootDir: t.TempDir(), Protocols: []Protocol{blockingProtocol{}}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	engine.Start(ctx)
	task, err := engine.Add(AddRequest{URL: "https://example.com/state.bin", TargetDirectory: "downloads"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Pause(task.ID); err != nil {
		t.Fatalf("waiting -> paused: %v", err)
	}
	if _, err := engine.Pause(task.ID); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("paused -> paused should fail, got %v", err)
	}
	if _, err := engine.StartTask(task.ID); err != nil {
		t.Fatalf("paused -> waiting: %v", err)
	}
	if _, err := engine.StartTask(task.ID); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("duplicate start should fail, got %v", err)
	}
	waitTask(t, engine, task.ID, func(task Task) bool { return task.Status == StatusDownloading })
	if _, err := engine.StartTask(task.ID); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("downloading -> start should fail, got %v", err)
	}
	if _, err := engine.Pause(task.ID); err != nil {
		t.Fatalf("downloading -> paused: %v", err)
	}
}

type blockingProtocol struct{}

func (blockingProtocol) Schemes() []string { return []string{"http", "https"} }

func (blockingProtocol) Download(ctx context.Context, _ TransferRequest, _ func(TransferProgress)) (TransferResult, error) {
	<-ctx.Done()
	return TransferResult{}, ctx.Err()
}

type canceledProgressProtocol struct {
	started chan struct{}
}

func (p *canceledProgressProtocol) Schemes() []string { return []string{"http", "https"} }

func (p *canceledProgressProtocol) Download(ctx context.Context, _ TransferRequest, progress func(TransferProgress)) (TransferResult, error) {
	close(p.started)
	<-ctx.Done()
	progress(TransferProgress{BytesReceived: 64})
	return TransferResult{}, ctx.Err()
}

func waitTask(t *testing.T, engine *Engine, id string, predicate func(Task) bool) Task {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		task, err := engine.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		if task.Status == StatusError {
			t.Fatalf("task failed: %s", task.Error)
		}
		if predicate(task) {
			return task
		}
		time.Sleep(10 * time.Millisecond)
	}
	task, _ := engine.Get(id)
	t.Fatalf("timed out waiting for task: %+v", task)
	return Task{}
}
