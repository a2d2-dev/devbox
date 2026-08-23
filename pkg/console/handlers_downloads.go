package console

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/a2d2-dev/devbox/pkg/downloads"
)

func (s *Server) registerDownloadRoutes() {
	s.mux.HandleFunc("/api/v1/downloads", s.handleDownloads)
	s.mux.HandleFunc("/api/v1/downloads/", s.handleDownload)
}

func (s *Server) handleDownloads(w http.ResponseWriter, r *http.Request) {
	if !s.requireDownloadEngine(w) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.jsonOK(w, s.downloadEngine.Snapshot())
	case http.MethodPost:
		var req struct {
			URL             string `json:"url"`
			TargetDirectory string `json:"targetDirectory"`
			Start           *bool  `json:"start"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&req); err != nil {
			s.downloadError(w, errors.New("invalid request body"))
			return
		}
		task, err := s.downloadEngine.Add(downloads.AddRequest{URL: req.URL, TargetDirectory: req.TargetDirectory})
		if err != nil {
			s.downloadError(w, err)
			return
		}
		autoStart := req.Start == nil || *req.Start
		if autoStart {
			if _, err := s.downloadEngine.Enqueue(task.ID); err != nil {
				_ = s.downloadEngine.Delete(task.ID, false)
				s.downloadError(w, err)
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(task)
	default:
		w.Header().Set("Allow", "GET, POST")
		s.downloadJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	if !s.requireDownloadEngine(w) {
		return
	}
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/downloads/"), "/")
	parts := strings.Split(path, "/")
	if len(parts) == 0 || parts[0] == "" {
		s.downloadJSONError(w, http.StatusBadRequest, "missing download task ID")
		return
	}
	id := parts[0]
	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			task, err := s.downloadEngine.Get(id)
			if err != nil {
				s.downloadError(w, err)
				return
			}
			s.jsonOK(w, task)
		case http.MethodDelete:
			deleteFile, err := strconv.ParseBool(defaultString(r.URL.Query().Get("deleteFile"), "false"))
			if err != nil {
				s.downloadJSONError(w, http.StatusBadRequest, "deleteFile must be true or false")
				return
			}
			if err := s.downloadEngine.Delete(id, deleteFile); err != nil {
				s.downloadError(w, err)
				return
			}
			s.jsonOK(w, map[string]interface{}{"id": id, "deleted": true, "fileDeleted": deleteFile})
		default:
			w.Header().Set("Allow", "GET, DELETE")
			s.downloadJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
		return
	}
	if len(parts) != 2 || r.Method != http.MethodPost {
		s.downloadJSONError(w, http.StatusNotFound, "unknown download task action")
		return
	}
	var (
		task downloads.Task
		err  error
	)
	switch parts[1] {
	case "start":
		task, err = s.downloadEngine.StartTask(id)
	case "pause":
		task, err = s.downloadEngine.Pause(id)
	default:
		s.downloadJSONError(w, http.StatusNotFound, "unknown download task action")
		return
	}
	if err != nil {
		s.downloadError(w, err)
		return
	}
	s.jsonOK(w, task)
}

func (s *Server) requireDownloadEngine(w http.ResponseWriter) bool {
	if s.downloadEngine != nil {
		return true
	}
	message := "download engine is unavailable"
	if s.downloadEngineError != "" {
		message += ": " + s.downloadEngineError
	}
	s.downloadJSONError(w, http.StatusServiceUnavailable, message)
	return false
}

func (s *Server) downloadError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, downloads.ErrInvalidURL), errors.Is(err, downloads.ErrInvalidPath):
		status = http.StatusBadRequest
	case errors.Is(err, downloads.ErrPathOutsideRoot), errors.Is(err, os.ErrPermission):
		status = http.StatusForbidden
	case errors.Is(err, downloads.ErrTaskNotFound):
		status = http.StatusNotFound
	case errors.Is(err, downloads.ErrTaskConflict), errors.Is(err, downloads.ErrInvalidTransition):
		status = http.StatusConflict
	}
	s.downloadJSONError(w, status, err.Error())
}

func (s *Server) downloadJSONError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
