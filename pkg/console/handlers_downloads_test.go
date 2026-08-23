package console

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/a2d2-dev/devbox/pkg/auth"
	"github.com/a2d2-dev/devbox/pkg/downloads"
)

func TestDownloadHandlersCreateListAndDelete(t *testing.T) {
	engine, err := downloads.New(downloads.Config{RootDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	engine.Start(context.Background())
	s := &Server{downloadEngine: engine, mux: http.NewServeMux(), auth: auth.New(auth.Config{})}
	s.registerDownloadRoutes()

	create := httptest.NewRequest(http.MethodPost, "/api/v1/downloads", strings.NewReader(`{"url":"https://example.com/archive.bin","targetDirectory":"downloads","start":false}`))
	created := httptest.NewRecorder()
	s.mux.ServeHTTP(created, create)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}
	var task downloads.Task
	if err := json.NewDecoder(created.Body).Decode(&task); err != nil {
		t.Fatal(err)
	}

	list := httptest.NewRecorder()
	s.mux.ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/api/v1/downloads", nil))
	var snapshot downloads.Snapshot
	if err := json.NewDecoder(list.Body).Decode(&snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.Counts.All != 1 || snapshot.Counts.Waiting != 1 || len(snapshot.Tasks) != 1 {
		t.Fatalf("inconsistent list snapshot: %+v", snapshot)
	}

	deleted := httptest.NewRecorder()
	s.mux.ServeHTTP(deleted, httptest.NewRequest(http.MethodDelete, "/api/v1/downloads/"+task.ID+"?deleteFile=false", nil))
	if deleted.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", deleted.Code, deleted.Body.String())
	}
}

func TestDownloadHandlersRejectInvalidInputAndUnavailableEngine(t *testing.T) {
	engine, err := downloads.New(downloads.Config{RootDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{downloadEngine: engine, mux: http.NewServeMux(), auth: auth.New(auth.Config{})}
	s.registerDownloadRoutes()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/downloads", strings.NewReader(`{"url":"file:///etc/passwd","targetDirectory":"../escape","start":false}`))
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid create status=%d body=%s", w.Code, w.Body.String())
	}

	unavailable := &Server{downloadEngineError: "permission denied", mux: http.NewServeMux(), auth: auth.New(auth.Config{})}
	unavailable.registerDownloadRoutes()
	w = httptest.NewRecorder()
	unavailable.mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/downloads", nil))
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), "permission denied") {
		t.Fatalf("unavailable status=%d body=%s", w.Code, w.Body.String())
	}
}
