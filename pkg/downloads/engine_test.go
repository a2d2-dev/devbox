package downloads

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
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
		w.Header().Set("ETag", `"stable-payload"`)
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
	engine, err := New(Config{RootDir: root, MaxConcurrent: 2, AllowPrivateNetworks: true})
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

func TestDownloadRejectsParentSymlinkReplacedAfterAdd(t *testing.T) {
	payload := []byte("must stay inside the download root")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	root := t.TempDir()
	outside := t.TempDir()
	engine, err := New(Config{
		RootDir:   root,
		Protocols: []Protocol{&HTTPProtocol{Client: server.Client()}},
	})
	if err != nil {
		t.Fatal(err)
	}
	engine.Start(context.Background())
	task, err := engine.Add(AddRequest{URL: server.URL + "/payload.bin", TargetDirectory: "downloads"})
	if err != nil {
		t.Fatal(err)
	}
	originalDirectory := filepath.Join(root, "downloads")
	if err := os.Rename(originalDirectory, originalDirectory+"-original"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, originalDirectory); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Enqueue(task.ID); err != nil {
		t.Fatal(err)
	}
	waitTaskStatus(t, engine, task.ID, StatusError)
	if _, err := os.Lstat(filepath.Join(outside, "payload.bin.part")); !os.IsNotExist(err) {
		t.Fatalf("download escaped through replaced parent symlink: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(outside, "payload.bin")); !os.IsNotExist(err) {
		t.Fatalf("final file escaped through replaced parent symlink: %v", err)
	}
}

func TestDownloadRejectsPrecreatedPartialSymlink(t *testing.T) {
	payload := []byte("replacement content")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	root := t.TempDir()
	engine, err := New(Config{
		RootDir:   root,
		Protocols: []Protocol{&HTTPProtocol{Client: server.Client()}},
	})
	if err != nil {
		t.Fatal(err)
	}
	engine.Start(context.Background())
	task, err := engine.Add(AddRequest{URL: server.URL + "/payload.bin", TargetDirectory: "downloads"})
	if err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(root, "victim.txt")
	original := []byte("do not overwrite")
	if err := os.WriteFile(victim, original, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, task.Destination+".part"); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Enqueue(task.ID); err != nil {
		t.Fatal(err)
	}
	waitTaskStatus(t, engine, task.ID, StatusError)
	got, err := os.ReadFile(victim)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, original) {
		t.Fatalf("partial symlink target was overwritten: got %q want %q", got, original)
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

func TestPauseThenImmediateStartWaitsForPreviousWorker(t *testing.T) {
	protocol := &overlapDetectProtocol{
		firstStarted:  make(chan struct{}),
		secondStarted: make(chan struct{}),
		releaseFirst:  make(chan struct{}),
	}
	engine, err := New(Config{RootDir: t.TempDir(), MaxConcurrent: 2, Protocols: []Protocol{protocol}})
	if err != nil {
		t.Fatal(err)
	}
	engine.Start(context.Background())
	task, err := engine.Add(AddRequest{URL: "https://example.com/concurrent.bin"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Enqueue(task.ID); err != nil {
		t.Fatal(err)
	}
	<-protocol.firstStarted
	if _, err := engine.Pause(task.ID); err != nil {
		t.Fatal(err)
	}
	startResult := make(chan error, 1)
	go func() {
		_, startErr := engine.StartTask(task.ID)
		startResult <- startErr
	}()
	select {
	case <-protocol.secondStarted:
		t.Error("replacement worker started before the canceled worker exited")
	case <-time.After(100 * time.Millisecond):
	}
	close(protocol.releaseFirst)
	if err := <-startResult; err != nil {
		t.Fatal(err)
	}
	completed := waitTask(t, engine, task.ID, func(task Task) bool { return task.Status == StatusCompleted })
	if protocol.overlap.Load() {
		t.Fatal("two workers wrote the partial file concurrently")
	}
	contents, err := os.ReadFile(completed.Destination)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(contents), "old-end") {
		t.Fatalf("canceled generation wrote after pause: %q", contents)
	}
}

func TestPausePersistenceFailureMarksTaskError(t *testing.T) {
	protocol := &canceledProgressProtocol{started: make(chan struct{})}
	engine, err := New(Config{RootDir: t.TempDir(), Protocols: []Protocol{protocol}})
	if err != nil {
		t.Fatal(err)
	}
	engine.Start(context.Background())
	task, err := engine.Add(AddRequest{URL: "https://example.com/persist-pause.bin"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Enqueue(task.ID); err != nil {
		t.Fatal(err)
	}
	<-protocol.started
	engine.statePath = t.TempDir()
	done := engine.workerDone[task.ID]
	if _, err := engine.Pause(task.ID); err == nil {
		t.Fatal("pause unexpectedly hid persistence failure")
	}
	<-done
	failed, err := engine.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if failed.Status != StatusError || !strings.Contains(failed.Error, "persist") {
		t.Fatalf("pause persistence failure task = %+v", failed)
	}
}

func TestCompletionPersistenceFailureMarksTaskError(t *testing.T) {
	protocol := &releaseProtocol{started: make(chan struct{}), release: make(chan struct{})}
	engine, err := New(Config{RootDir: t.TempDir(), Protocols: []Protocol{protocol}})
	if err != nil {
		t.Fatal(err)
	}
	engine.Start(context.Background())
	task, err := engine.Add(AddRequest{URL: "https://example.com/persist-complete.bin"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Enqueue(task.ID); err != nil {
		t.Fatal(err)
	}
	<-protocol.started
	engine.statePath = t.TempDir()
	close(protocol.release)
	failed := waitTaskStatus(t, engine, task.ID, StatusError)
	if !strings.Contains(failed.Error, "persist") {
		t.Fatalf("completion persistence failure = %q", failed.Error)
	}
}

func TestHTTPResumeRejectsMismatchedContentRange(t *testing.T) {
	payload := []byte("complete remote payload")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") == "" {
			t.Fatal("resume request did not include Range")
		}
		w.Header().Set("ETag", `"same-version"`)
		w.Header().Set("Content-Range", fmt.Sprintf("bytes 0-%d/%d", len(payload)-1, len(payload)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	partialDir := t.TempDir()
	partial := filepath.Join(partialDir, "payload.part")
	original := []byte("partial")
	if err := os.WriteFile(partial, original, 0o640); err != nil {
		t.Fatal(err)
	}
	protocol := &HTTPProtocol{Client: server.Client()}
	root, err := os.OpenRoot(partialDir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	_, err = protocol.Download(context.Background(), TransferRequest{
		URL: server.URL, Root: root, PartialPath: "payload.part", Offset: int64(len(original)), ETag: `"same-version"`,
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

func TestHTTPResumeRestartsWhenETagChanges(t *testing.T) {
	oldPrefix := []byte("old-")
	newPayload := []byte("new resource contents")
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestNumber := requests.Add(1)
		w.Header().Set("ETag", `"new-version"`)
		if requestNumber == 1 {
			if got := r.Header.Get("Range"); got != "bytes=4-" {
				t.Errorf("first request Range = %q", got)
			}
			if got := r.Header.Get("If-Range"); got != `"old-version"` {
				t.Errorf("first request If-Range = %q", got)
			}
			w.Header().Set("Content-Range", fmt.Sprintf("bytes 4-%d/%d", len(newPayload)-1, len(newPayload)))
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(newPayload[4:])
			return
		}
		if r.Header.Get("Range") != "" || r.Header.Get("If-Range") != "" {
			t.Errorf("restart request retained resume headers: Range=%q If-Range=%q", r.Header.Get("Range"), r.Header.Get("If-Range"))
		}
		_, _ = w.Write(newPayload)
	}))
	defer server.Close()

	partialDir := t.TempDir()
	partial := filepath.Join(partialDir, "payload.part")
	if err := os.WriteFile(partial, oldPrefix, 0o640); err != nil {
		t.Fatal(err)
	}
	protocol := &HTTPProtocol{Client: server.Client()}
	root, err := os.OpenRoot(partialDir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	result, err := protocol.Download(context.Background(), TransferRequest{
		URL: server.URL, Root: root, PartialPath: "payload.part", Offset: int64(len(oldPrefix)), ETag: `"old-version"`,
	}, func(TransferProgress) {})
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(partial)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, newPayload) {
		t.Fatalf("partial was not replaced with new resource: got %q want %q", got, newPayload)
	}
	if requests.Load() != 2 || result.ETag != `"new-version"` {
		t.Fatalf("restart result = requests:%d etag:%q", requests.Load(), result.ETag)
	}
}

func TestHTTPResumeWithoutValidatorStartsFromZero(t *testing.T) {
	payload := []byte("complete new payload")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") != "" || r.Header.Get("If-Range") != "" {
			t.Errorf("request without validator attempted resume: Range=%q If-Range=%q", r.Header.Get("Range"), r.Header.Get("If-Range"))
		}
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	partialDir := t.TempDir()
	partial := filepath.Join(partialDir, "payload.part")
	if err := os.WriteFile(partial, []byte("stale"), 0o640); err != nil {
		t.Fatal(err)
	}
	protocol := &HTTPProtocol{Client: server.Client()}
	root, err := os.OpenRoot(partialDir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if _, err := protocol.Download(context.Background(), TransferRequest{
		URL: server.URL, Root: root, PartialPath: "payload.part", Offset: 5,
	}, func(TransferProgress) {}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(partial)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("download without validator did not restart: got %q want %q", got, payload)
	}
}

func TestHTTPRangeNotSatisfiableRequiresMatchingValidator(t *testing.T) {
	payload := []byte("fresh payload")
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			w.Header().Set("Content-Range", "bytes */7")
			w.Header().Set("ETag", `"new-version"`)
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}
		if r.Header.Get("Range") != "" {
			t.Errorf("retry after unsafe 416 retained Range: %q", r.Header.Get("Range"))
		}
		w.Header().Set("ETag", `"new-version"`)
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	partialDir := t.TempDir()
	partial := filepath.Join(partialDir, "payload.part")
	if err := os.WriteFile(partial, []byte("1234567"), 0o640); err != nil {
		t.Fatal(err)
	}
	protocol := &HTTPProtocol{Client: server.Client()}
	root, err := os.OpenRoot(partialDir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if _, err := protocol.Download(context.Background(), TransferRequest{
		URL: server.URL, Root: root, PartialPath: "payload.part", Offset: 7, ETag: `"old-version"`,
	}, func(TransferProgress) {}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(partial)
	if err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 2 || !bytes.Equal(got, payload) {
		t.Fatalf("unsafe 416 was accepted: requests=%d payload=%q", requests.Load(), got)
	}
}

func TestHTTPRedirectToLoopbackIsRejected(t *testing.T) {
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://127.0.0.1:1/internal", http.StatusFound)
	}))
	defer redirector.Close()

	engine, err := New(Config{
		RootDir:    t.TempDir(),
		HTTPClient: mappedHTTPClient(redirector.Listener.Addr().String(), false),
	})
	if err != nil {
		t.Fatal(err)
	}
	engine.Start(context.Background())
	task, err := engine.Add(AddRequest{URL: "http://93.184.216.34/public.bin"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Enqueue(task.ID); err != nil {
		t.Fatal(err)
	}
	failed := waitTaskStatus(t, engine, task.ID, StatusError)
	if !strings.Contains(failed.Error, "private network") {
		t.Fatalf("loopback redirect error = %q", failed.Error)
	}
}

func TestHTTPRedirectRejectsHTTPSDowngrade(t *testing.T) {
	redirector := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://93.184.216.35/plain.bin", http.StatusFound)
	}))
	defer redirector.Close()

	engine, err := New(Config{
		RootDir:    t.TempDir(),
		HTTPClient: mappedHTTPClient(redirector.Listener.Addr().String(), true),
	})
	if err != nil {
		t.Fatal(err)
	}
	engine.Start(context.Background())
	task, err := engine.Add(AddRequest{URL: "https://93.184.216.34/secure.bin"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Enqueue(task.ID); err != nil {
		t.Fatal(err)
	}
	failed := waitTaskStatus(t, engine, task.ID, StatusError)
	if !strings.Contains(failed.Error, "HTTPS redirect to HTTP") {
		t.Fatalf("downgrade redirect error = %q", failed.Error)
	}
}

func TestAllowPrivateNetworksPermitsInternalDownload(t *testing.T) {
	payload := []byte("internal artifact")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	engine, err := New(Config{RootDir: t.TempDir(), AllowPrivateNetworks: true})
	if err != nil {
		t.Fatal(err)
	}
	engine.Start(context.Background())
	task, err := engine.Add(AddRequest{URL: server.URL + "/internal.bin"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Enqueue(task.ID); err != nil {
		t.Fatal(err)
	}
	completed := waitTask(t, engine, task.ID, func(task Task) bool { return task.Status == StatusCompleted })
	got, err := os.ReadFile(completed.Destination)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("private download payload = %q", got)
	}
}

func mappedHTTPClient(target string, insecureTLS bool) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: insecureTLS} // Test server certificate.
	transport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, target)
	}
	return &http.Client{Transport: transport}
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

func TestDeleteFileFailureKeepsTask(t *testing.T) {
	engine, err := New(Config{RootDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	task, err := engine.Add(AddRequest{URL: "https://example.com/not-empty.bin"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(task.Destination, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(task.Destination, "child"), []byte("keep"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := engine.Delete(task.ID, true); err == nil {
		t.Fatal("delete unexpectedly succeeded for a non-empty destination directory")
	}
	retained, err := engine.Get(task.ID)
	if err != nil {
		t.Fatalf("task disappeared after file deletion failed: %v", err)
	}
	if retained.Status != StatusError || !strings.Contains(retained.Error, "delete") {
		t.Fatalf("retained task does not expose deletion failure: %+v", retained)
	}
}

func TestDeleteRejectsParentSymlinkReplacedAfterAdd(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	engine, err := New(Config{RootDir: root})
	if err != nil {
		t.Fatal(err)
	}
	task, err := engine.Add(AddRequest{URL: "https://example.com/artifact.bin", TargetDirectory: "downloads"})
	if err != nil {
		t.Fatal(err)
	}
	originalDirectory := filepath.Join(root, "downloads")
	if err := os.Rename(originalDirectory, originalDirectory+"-original"); err != nil {
		t.Fatal(err)
	}
	outsideFile := filepath.Join(outside, "artifact.bin")
	if err := os.WriteFile(outsideFile, []byte("outside"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, originalDirectory); err != nil {
		t.Fatal(err)
	}
	if err := engine.Delete(task.ID, true); err == nil {
		t.Fatal("delete followed a replaced parent symlink")
	}
	if got, err := os.ReadFile(outsideFile); err != nil || string(got) != "outside" {
		t.Fatalf("outside file changed: content=%q err=%v", got, err)
	}
	if _, err := engine.Get(task.ID); err != nil {
		t.Fatalf("task disappeared after unsafe deletion was rejected: %v", err)
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

type overlapDetectProtocol struct {
	calls         atomic.Int32
	activeWriters atomic.Int32
	overlap       atomic.Bool
	firstStarted  chan struct{}
	secondStarted chan struct{}
	releaseFirst  chan struct{}
}

type releaseProtocol struct {
	started chan struct{}
	release chan struct{}
}

func (p *releaseProtocol) Schemes() []string { return []string{"http", "https"} }

func (p *releaseProtocol) Download(_ context.Context, request TransferRequest, _ func(TransferProgress)) (TransferResult, error) {
	file, err := request.Root.OpenFile(request.PartialPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o640)
	if err != nil {
		return TransferResult{}, err
	}
	if _, err := file.WriteString("complete"); err != nil {
		_ = file.Close()
		return TransferResult{}, err
	}
	if err := file.Close(); err != nil {
		return TransferResult{}, err
	}
	close(p.started)
	<-p.release
	return TransferResult{Downloaded: 8, Total: 8}, nil
}

func (p *overlapDetectProtocol) Schemes() []string { return []string{"http", "https"} }

func (p *overlapDetectProtocol) Download(ctx context.Context, request TransferRequest, _ func(TransferProgress)) (TransferResult, error) {
	call := p.calls.Add(1)
	if p.activeWriters.Add(1) > 1 {
		p.overlap.Store(true)
	}
	defer p.activeWriters.Add(-1)
	flags := os.O_CREATE | os.O_WRONLY
	if call == 1 {
		flags |= os.O_TRUNC
	} else {
		flags |= os.O_APPEND
	}
	file, err := request.Root.OpenFile(request.PartialPath, flags, 0o640)
	if err != nil {
		return TransferResult{}, err
	}
	defer file.Close()
	if call == 1 {
		_, _ = file.WriteString("old-start")
		close(p.firstStarted)
		<-ctx.Done()
		<-p.releaseFirst
		if request.CanWrite == nil || request.CanWrite() {
			_, _ = file.WriteString("old-end")
		}
		return TransferResult{}, ctx.Err()
	}
	close(p.secondStarted)
	_, _ = file.WriteString("new")
	return TransferResult{Downloaded: request.Offset + 3, Total: request.Offset + 3}, nil
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

func waitTaskStatus(t *testing.T, engine *Engine, id string, status Status) Task {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		task, err := engine.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		if task.Status == status {
			return task
		}
		time.Sleep(10 * time.Millisecond)
	}
	task, _ := engine.Get(id)
	t.Fatalf("timed out waiting for status %s: %+v", status, task)
	return Task{}
}
