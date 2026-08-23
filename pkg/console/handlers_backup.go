package console

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/a2d2-dev/devbox/pkg/backup"
)

func (s *Server) registerBackupRoutes() {
	s.mux.HandleFunc("/api/v1/backups", s.handleBackups)
	s.mux.HandleFunc("/api/v1/backups/", s.handleBackup)
}

func (s *Server) handleBackups(w http.ResponseWriter, r *http.Request) {
	if s.backup == nil {
		writeJSONErrStatus(w, http.StatusServiceUnavailable, map[string]any{"error": "backup manager not initialized"})
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.jsonOK(w, s.backup.List())
	case http.MethodPost:
		var task backup.Task
		if !decodeBackupJSON(w, r, &task) {
			return
		}
		created, result, err := s.backup.Create(r.Context(), task)
		if err != nil {
			writeJSONErrStatus(w, http.StatusUnprocessableEntity, map[string]any{"error": err.Error(), "preflight": result})
			return
		}
		s.jsonStatus(w, http.StatusCreated, created)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleBackup(w http.ResponseWriter, r *http.Request) {
	if s.backup == nil {
		writeJSONErrStatus(w, http.StatusServiceUnavailable, map[string]any{"error": "backup manager not initialized"})
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/backups/")
	segments := strings.Split(strings.Trim(path, "/"), "/")
	if len(segments) == 1 && segments[0] == "preflight" {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var task backup.Task
		if !decodeBackupJSON(w, r, &task) {
			return
		}
		result := s.backup.Preflight(r.Context(), task)
		status := http.StatusOK
		if !result.OK {
			status = http.StatusUnprocessableEntity
		}
		s.jsonStatus(w, status, result)
		return
	}
	if len(segments) == 0 || segments[0] == "" {
		http.Error(w, "backup task id required", http.StatusBadRequest)
		return
	}
	taskID := segments[0]

	var err error
	switch {
	case len(segments) == 1 && r.Method == http.MethodGet:
		var task backup.Task
		task, err = s.backup.Get(taskID)
		if err == nil {
			s.jsonOK(w, task)
		}
	case len(segments) == 2 && segments[1] == "run" && r.Method == http.MethodPost:
		var history backup.History
		history, err = s.backup.RunNow(taskID)
		if err == nil {
			s.jsonStatus(w, http.StatusAccepted, history)
		}
	case len(segments) == 2 && segments[1] == "pause" && r.Method == http.MethodPost:
		var request struct {
			Paused bool `json:"paused"`
		}
		if !decodeBackupJSON(w, r, &request) {
			return
		}
		var task backup.Task
		task, err = s.backup.SetPaused(taskID, request.Paused)
		if err == nil {
			s.jsonOK(w, task)
		}
	case len(segments) == 2 && segments[1] == "history" && r.Method == http.MethodGet:
		var histories []backup.History
		histories, err = s.backup.Histories(taskID)
		if err == nil {
			for i := range histories {
				histories[i].Log = ""
			}
			s.jsonOK(w, histories)
		}
	case len(segments) == 4 && segments[1] == "history" && segments[3] == "log" && r.Method == http.MethodGet:
		var history backup.History
		history, err = s.backup.History(taskID, segments[2])
		if err == nil {
			s.jsonOK(w, map[string]any{"id": history.ID, "phase": history.Phase, "error": history.Error, "log": history.Log})
		}
	case len(segments) == 2 && segments[1] == "versions" && r.Method == http.MethodGet:
		var versions []string
		versions, err = s.backup.Versions(r.Context(), taskID)
		if err == nil {
			s.jsonOK(w, versions)
		}
	case len(segments) == 3 && segments[1] == "restore" && segments[2] == "preview" && r.Method == http.MethodPost:
		var request backup.RestoreRequest
		if !decodeBackupJSON(w, r, &request) {
			return
		}
		var preview backup.RestorePreview
		preview, err = s.backup.PreviewRestore(r.Context(), taskID, request)
		if err == nil {
			s.jsonOK(w, preview)
		}
	case len(segments) == 2 && segments[1] == "restore" && r.Method == http.MethodPost:
		var request backup.RestoreRequest
		if !decodeBackupJSON(w, r, &request) {
			return
		}
		var history backup.History
		history, err = s.backup.Restore(r.Context(), taskID, request)
		if err == nil {
			s.jsonStatus(w, http.StatusAccepted, history)
		}
	default:
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		writeBackupError(w, err)
	}
}

func decodeBackupJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeJSONErrStatus(w, http.StatusBadRequest, map[string]any{"error": "invalid request body: " + err.Error()})
		return false
	}
	return true
}

func writeBackupError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, backup.ErrNotFound):
		writeJSONErrStatus(w, http.StatusNotFound, map[string]any{"error": err.Error()})
	case errors.Is(err, backup.ErrConflict):
		writeJSONErrStatus(w, http.StatusConflict, map[string]any{"error": err.Error()})
	default:
		writeJSONErrStatus(w, http.StatusUnprocessableEntity, map[string]any{"error": err.Error()})
	}
}
