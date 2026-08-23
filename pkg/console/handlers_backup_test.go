package console

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/a2d2-dev/devbox/pkg/backup"
)

func TestBackupRoutesCreateRunHistoryAndLog(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	target := filepath.Join(root, "target")
	if err := os.MkdirAll(source, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(target, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "file.txt"), []byte("backup route"), 0o640); err != nil {
		t.Fatal(err)
	}
	manager, err := backup.NewManager(filepath.Join(root, "state"), 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	manager.Start(context.Background())
	s := &Server{backup: manager, mux: http.NewServeMux()}
	s.registerBackupRoutes()

	task := backup.Task{
		Name:      "route test",
		Source:    backup.Endpoint{Type: backup.EndpointLocal, Path: source},
		Target:    backup.Endpoint{Type: backup.EndpointLocal, Path: target},
		Schedule:  backup.Schedule{Kind: "daily", Hour: 2},
		Retention: backup.RetentionPolicy{KeepLast: 2},
		Mode:      backup.ModeVersioned, Incremental: true,
	}
	response := backupRequest(t, s.mux, http.MethodPost, "/api/v1/backups", task)
	if response.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", response.Code, response.Body.String())
	}
	var created backup.Task
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}

	response = backupRequest(t, s.mux, http.MethodPost, "/api/v1/backups/"+created.ID+"/run", nil)
	if response.Code != http.StatusAccepted {
		t.Fatalf("run status=%d body=%s", response.Code, response.Body.String())
	}
	deadline := time.Now().Add(2 * time.Minute)
	var histories []backup.History
	for time.Now().Before(deadline) {
		histories, err = manager.Histories(created.ID)
		if err == nil && len(histories) == 1 && histories[0].FinishedAt != nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(histories) != 1 || histories[0].Status != backup.StatusSuccess {
		t.Fatalf("history=%+v err=%v", histories, err)
	}

	response = backupRequest(t, s.mux, http.MethodGet, "/api/v1/backups/"+created.ID+"/history", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("history status=%d body=%s", response.Code, response.Body.String())
	}
	var listed []backup.History
	if err := json.Unmarshal(response.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].Log != "" {
		t.Fatalf("history list should omit log: %+v", listed)
	}

	response = backupRequest(t, s.mux, http.MethodGet, "/api/v1/backups/"+created.ID+"/history/"+histories[0].ID+"/log", nil)
	if response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte("Total transferred file size")) {
		t.Fatalf("log status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestBackupPreflightRouteRejectsPathLoop(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	target := filepath.Join(source, "backup")
	if err := os.MkdirAll(target, 0o750); err != nil {
		t.Fatal(err)
	}
	manager, err := backup.NewManager(filepath.Join(root, "state"), 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{backup: manager, mux: http.NewServeMux()}
	s.registerBackupRoutes()
	task := backup.Task{
		Name: "loop", Source: backup.Endpoint{Type: backup.EndpointLocal, Path: source},
		Target:    backup.Endpoint{Type: backup.EndpointLocal, Path: target},
		Schedule:  backup.Schedule{Kind: "daily", Hour: 2},
		Retention: backup.RetentionPolicy{KeepLast: 1}, Mode: backup.ModeVersioned,
	}
	response := backupRequest(t, s.mux, http.MethodPost, "/api/v1/backups/preflight", task)
	if response.Code != http.StatusUnprocessableEntity || !bytes.Contains(response.Body.Bytes(), []byte("路径循环")) {
		t.Fatalf("preflight status=%d body=%s", response.Code, response.Body.String())
	}
}

func backupRequest(t *testing.T, handler http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var encoded []byte
	if body != nil {
		var err error
		encoded, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	request := httptest.NewRequest(method, path, bytes.NewReader(encoded))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
