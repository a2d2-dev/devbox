package console

import (
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/a2d2-dev/devbox/pkg/auth"
	filemanager "github.com/a2d2-dev/devbox/pkg/files"
)

func (s *Server) registerFileRoutes() {
	s.mux.HandleFunc("/api/v1/files", s.handleFiles)
	s.mux.HandleFunc("/api/v1/files/sources", s.handleFileSources)
	s.mux.HandleFunc("/api/v1/files/search", s.handleFileSearch)
	s.mux.HandleFunc("/api/v1/files/upload", s.handleFileUpload)
	s.mux.HandleFunc("/api/v1/files/content", s.handleFileContent)
	s.mux.HandleFunc("/api/v1/files/download", s.handleFileDownload)
	s.mux.HandleFunc("/api/v1/files/mkdir", s.handleFileMkdir)
	s.mux.HandleFunc("/api/v1/files/rename", s.handleFileRename)
	s.mux.HandleFunc("/api/v1/files/transfer", s.handleFileTransfer)
	s.mux.HandleFunc("/api/v1/files/delete", s.requireAdmin(s.handleFileDelete))
	s.mux.HandleFunc("/api/v1/files/trash", s.requireAdmin(s.handleTrash))
	s.mux.HandleFunc("/api/v1/files/trash/restore", s.requireAdmin(s.handleTrashRestore))
	s.mux.HandleFunc("/api/v1/files/trash/purge", s.requireAdmin(s.handleTrashPurge))
	s.mux.HandleFunc("/api/v1/files/trash/empty", s.requireAdmin(s.handleTrashEmpty))
	s.mux.HandleFunc("/api/v1/files/favorites", s.requireAdmin(s.handleFavorites))
	s.mux.HandleFunc("/api/v1/files/recent", s.requireAdmin(s.handleRecent))
	s.mux.HandleFunc("/api/v1/files/shares", s.requireAdmin(s.handleShares))
	s.mux.HandleFunc("/api/v1/files/shares/", s.requireAdmin(s.handleShareByID))
	s.mux.HandleFunc("/api/v1/files/public/", s.handlePublicShare)
}

func (s *Server) fileReady(w http.ResponseWriter) bool {
	if s.fileBrowser == nil {
		writeFileError(w, filemanagerError("SOURCE_UNAVAILABLE", "file manager is unavailable"))
		return false
	}
	return true
}

type consoleFileError struct{ code, message string }

func (e consoleFileError) Error() string { return e.message }
func filemanagerError(code, message string) error {
	return consoleFileError{code: code, message: message}
}

func writeFileError(w http.ResponseWriter, err error) {
	code := filemanager.ErrorCode(err)
	if own, ok := err.(consoleFileError); ok {
		code = own.code
	}
	status := http.StatusInternalServerError
	switch code {
	case "PATH_FORBIDDEN", "OPERATION_UNSUPPORTED", "SHARE_EXPIRED":
		status = http.StatusForbidden
	case "PATH_NOT_FOUND", "SOURCE_NOT_FOUND", "SHARE_NOT_FOUND", "TRASH_NOT_FOUND":
		status = http.StatusNotFound
	case "SOURCE_UNAVAILABLE":
		status = http.StatusServiceUnavailable
	case "CONFLICT":
		status = http.StatusConflict
	case "INVALID_NAME", "INVALID_REQUEST", "CONFIRMATION_REQUIRED", "NOT_DIRECTORY", "NOT_FILE", "LIMIT_EXCEEDED":
		status = http.StatusBadRequest
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"code": code, "message": err.Error()})
}

func requireMethod(w http.ResponseWriter, r *http.Request, method string) bool {
	if r.Method == method {
		return true
	}
	w.Header().Set("Allow", method)
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	return false
}

func decodeFileJSON(w http.ResponseWriter, r *http.Request, value any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		writeFileError(w, filemanagerError("INVALID_REQUEST", "invalid request body"))
		return false
	}
	return true
}

func (s *Server) handleFileSources(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) || !s.fileReady(w) {
		return
	}
	sources := s.fileBrowser.Sources()
	if s.isRestrictedFileUser(r) {
		filtered := sources[:0]
		for _, source := range sources {
			if source.ID == "my" {
				filtered = append(filtered, source)
			}
		}
		sources = filtered
	}
	s.jsonOK(w, sources)
}

func (s *Server) handleFiles(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) || !s.fileReady(w) {
		return
	}
	sourceID, path := r.URL.Query().Get("source"), r.URL.Query().Get("path")
	allowed, err := s.authorizedFilePaths(r, sourceID, path, true)
	if err != nil {
		writeFileError(w, err)
		return
	}
	entries, err := s.fileBrowser.ListSource(sourceID, path, r.URL.Query().Get("sort"), r.URL.Query().Get("order"))
	if err != nil {
		writeFileError(w, err)
		return
	}
	if allowed != nil {
		filtered := entries[:0]
		for _, entry := range entries {
			if pathWithinAny(entry.AbsPath, allowed, true) {
				filtered = append(filtered, entry)
			}
		}
		entries = filtered
	}
	s.jsonOK(w, entries)
}

func (s *Server) handleFileSearch(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) || !s.fileReady(w) {
		return
	}
	sourceID, path := r.URL.Query().Get("source"), r.URL.Query().Get("path")
	allowed, err := s.authorizedFilePaths(r, sourceID, path, true)
	if err != nil {
		writeFileError(w, err)
		return
	}
	entries, err := s.fileBrowser.Search(sourceID, path, r.URL.Query().Get("q"))
	if err != nil {
		writeFileError(w, err)
		return
	}
	if allowed != nil {
		filtered := entries[:0]
		for _, entry := range entries {
			if pathWithinAny(entry.AbsPath, allowed, false) {
				filtered = append(filtered, entry)
			}
		}
		entries = filtered
	}
	s.jsonOK(w, entries)
}

func (s *Server) handleFileUpload(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) || !s.fileReady(w) {
		return
	}
	const maxUpload = 20 << 20
	r.Body = http.MaxBytesReader(w, r.Body, maxUpload)
	if err := r.ParseMultipartForm(maxUpload); err != nil {
		http.Error(w, "upload exceeds 20 MiB", http.StatusRequestEntityTooLarge)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeFileError(w, filemanagerError("INVALID_REQUEST", "missing file field"))
		return
	}
	defer file.Close()
	name := r.FormValue("name")
	if name == "" {
		name = header.Filename
	}
	if _, err := s.authorizedFilePaths(r, r.FormValue("source"), r.FormValue("path"), false); err != nil {
		writeFileError(w, err)
		return
	}
	data, err := io.ReadAll(file)
	if err != nil {
		writeFileError(w, err)
		return
	}
	saved, err := s.fileBrowser.SaveSource(r.FormValue("source"), r.FormValue("path"), name, data)
	if err != nil {
		writeFileError(w, err)
		return
	}
	s.jsonOK(w, map[string]string{"name": saved})
}

func requestedFilePath(r *http.Request) string {
	path := r.URL.Query().Get("path")
	if name := r.URL.Query().Get("name"); name != "" {
		path = filepath.ToSlash(filepath.Join(path, name))
	}
	return path
}

func (s *Server) serveManagedFile(w http.ResponseWriter, r *http.Request, attachment bool) {
	if !requireMethod(w, r, http.MethodGet) || !s.fileReady(w) {
		return
	}
	if _, err := s.authorizedFilePaths(r, r.URL.Query().Get("source"), requestedFilePath(r), false); err != nil {
		writeFileError(w, err)
		return
	}
	file, info, err := s.fileBrowser.OpenDownload(r.URL.Query().Get("source"), requestedFilePath(r))
	if err != nil {
		writeFileError(w, err)
		return
	}
	defer file.Close()
	if attachment {
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": info.Name()}))
	}
	http.ServeContent(w, r, info.Name(), info.ModTime(), file)
}

func (s *Server) handleFileContent(w http.ResponseWriter, r *http.Request) {
	s.serveManagedFile(w, r, false)
}
func (s *Server) handleFileDownload(w http.ResponseWriter, r *http.Request) {
	s.serveManagedFile(w, r, true)
}

func (s *Server) handleFileMkdir(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) || !s.fileReady(w) {
		return
	}
	var req struct {
		Source string `json:"source"`
		Path   string `json:"path"`
		Name   string `json:"name"`
	}
	if !decodeFileJSON(w, r, &req) {
		return
	}
	if _, err := s.authorizedFilePaths(r, req.Source, req.Path, false); err != nil {
		writeFileError(w, err)
		return
	}
	if err := s.fileBrowser.Mkdir(req.Source, req.Path, req.Name); err != nil {
		writeFileError(w, err)
		return
	}
	s.jsonOK(w, map[string]string{"status": "ok"})
}

func (s *Server) handleFileRename(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) || !s.fileReady(w) {
		return
	}
	var req struct {
		Source string `json:"source"`
		Path   string `json:"path"`
		Name   string `json:"name"`
	}
	if !decodeFileJSON(w, r, &req) {
		return
	}
	if _, err := s.authorizedFilePaths(r, req.Source, req.Path, false); err != nil {
		writeFileError(w, err)
		return
	}
	if err := s.fileBrowser.Rename(req.Source, req.Path, req.Name); err != nil {
		writeFileError(w, err)
		return
	}
	s.jsonOK(w, map[string]string{"status": "ok"})
}

func (s *Server) handleFileTransfer(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) || !s.fileReady(w) {
		return
	}
	var req struct {
		Source      string `json:"source"`
		Path        string `json:"path"`
		Destination string `json:"destination"`
		Copy        bool   `json:"copy"`
	}
	if !decodeFileJSON(w, r, &req) {
		return
	}
	if _, err := s.authorizedFilePaths(r, req.Source, req.Path, false); err != nil {
		writeFileError(w, err)
		return
	}
	if _, err := s.authorizedFilePaths(r, req.Source, req.Destination, false); err != nil {
		writeFileError(w, err)
		return
	}
	if err := s.fileBrowser.Transfer(req.Source, req.Path, req.Destination, req.Copy); err != nil {
		writeFileError(w, err)
		return
	}
	s.jsonOK(w, map[string]string{"status": "ok"})
}

func (s *Server) handleFileDelete(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) || !s.fileReady(w) {
		return
	}
	var req struct {
		Source    string `json:"source"`
		Path      string `json:"path"`
		Permanent bool   `json:"permanent"`
		Confirm   bool   `json:"confirm"`
	}
	if !decodeFileJSON(w, r, &req) {
		return
	}
	source, err := s.fileBrowser.Source(req.Source)
	if err != nil {
		writeFileError(w, err)
		return
	}
	willPermanentlyDelete := req.Permanent || !source.Capabilities.Trash
	if willPermanentlyDelete && !req.Confirm {
		writeFileError(w, filemanagerError("CONFIRMATION_REQUIRED", "permanent deletion requires confirmation"))
		return
	}
	if err := s.fileBrowser.Delete(req.Source, req.Path, req.Permanent); err != nil {
		writeFileError(w, err)
		return
	}
	s.jsonOK(w, map[string]any{"status": "ok", "permanent": willPermanentlyDelete})
}

func (s *Server) handleTrash(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) || !s.fileReady(w) {
		return
	}
	entries, err := s.fileBrowser.Trash(r.URL.Query().Get("q"))
	if err != nil {
		writeFileError(w, err)
		return
	}
	s.jsonOK(w, entries)
}

func (s *Server) handleTrashRestore(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) || !s.fileReady(w) {
		return
	}
	var req struct {
		ID string `json:"id"`
	}
	if !decodeFileJSON(w, r, &req) {
		return
	}
	if err := s.fileBrowser.RestoreTrash(req.ID); err != nil {
		writeFileError(w, err)
		return
	}
	s.jsonOK(w, map[string]string{"status": "ok"})
}

func (s *Server) handleTrashPurge(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) || !s.fileReady(w) {
		return
	}
	var req struct {
		ID      string `json:"id"`
		Confirm bool   `json:"confirm"`
	}
	if !decodeFileJSON(w, r, &req) {
		return
	}
	if !req.Confirm {
		writeFileError(w, filemanagerError("CONFIRMATION_REQUIRED", "permanent deletion requires confirmation"))
		return
	}
	if err := s.fileBrowser.PurgeTrash(req.ID); err != nil {
		writeFileError(w, err)
		return
	}
	s.jsonOK(w, map[string]string{"status": "ok"})
}

func (s *Server) handleTrashEmpty(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) || !s.fileReady(w) {
		return
	}
	var req struct {
		Confirm bool `json:"confirm"`
	}
	if !decodeFileJSON(w, r, &req) {
		return
	}
	if !req.Confirm {
		writeFileError(w, filemanagerError("CONFIRMATION_REQUIRED", "empty trash requires confirmation"))
		return
	}
	if err := s.fileBrowser.EmptyTrash(); err != nil {
		writeFileError(w, err)
		return
	}
	s.jsonOK(w, map[string]string{"status": "ok"})
}

func (s *Server) handleFavorites(w http.ResponseWriter, r *http.Request) {
	if !s.fileReady(w) {
		return
	}
	if r.Method == http.MethodGet {
		items, err := s.fileBrowser.Favorites()
		if err != nil {
			writeFileError(w, err)
			return
		}
		s.jsonOK(w, items)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Source  string `json:"source"`
		Path    string `json:"path"`
		Enabled bool   `json:"enabled"`
	}
	if !decodeFileJSON(w, r, &req) {
		return
	}
	if err := s.fileBrowser.SetFavorite(req.Source, req.Path, req.Enabled); err != nil {
		writeFileError(w, err)
		return
	}
	s.jsonOK(w, map[string]string{"status": "ok"})
}

func (s *Server) handleRecent(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) || !s.fileReady(w) {
		return
	}
	items, err := s.fileBrowser.Recent()
	if err != nil {
		writeFileError(w, err)
		return
	}
	s.jsonOK(w, items)
}

func (s *Server) handleShares(w http.ResponseWriter, r *http.Request) {
	if !s.fileReady(w) {
		return
	}
	if r.Method == http.MethodGet {
		items, err := s.fileBrowser.Shares()
		if err != nil {
			writeFileError(w, err)
			return
		}
		s.jsonOK(w, items)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Source    string `json:"source"`
		Path      string `json:"path"`
		ExpiresIn int64  `json:"expiresIn"`
	}
	if !decodeFileJSON(w, r, &req) {
		return
	}
	if req.ExpiresIn < 0 || req.ExpiresIn > int64((365*24*time.Hour)/time.Second) {
		writeFileError(w, filemanagerError("INVALID_REQUEST", "expiry must be between 0 and 365 days"))
		return
	}
	share, err := s.fileBrowser.CreateShare(req.Source, req.Path, time.Duration(req.ExpiresIn)*time.Second)
	if err != nil {
		writeFileError(w, err)
		return
	}
	s.jsonOK(w, share)
}

func (s *Server) handleShareByID(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodDelete) || !s.fileReady(w) {
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/files/shares/")
	if id == "" || strings.Contains(id, "/") {
		writeFileError(w, filemanagerError("INVALID_REQUEST", "invalid share id"))
		return
	}
	if err := s.fileBrowser.RevokeShare(id); err != nil {
		writeFileError(w, err)
		return
	}
	s.jsonOK(w, map[string]string{"status": "ok"})
}

func (s *Server) handlePublicShare(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) || !s.fileReady(w) {
		return
	}
	token := strings.TrimPrefix(r.URL.Path, "/api/v1/files/public/")
	if token == "" || strings.Contains(token, "/") {
		writeFileError(w, filemanagerError("SHARE_NOT_FOUND", "share not found"))
		return
	}
	file, name, info, err := s.fileBrowser.OpenShare(token)
	if err != nil {
		writeFileError(w, err)
		return
	}
	defer file.Close()
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	w.Header().Set("Cache-Control", "private, no-store")
	http.ServeContent(w, r, name, info.ModTime(), file)
}

func (s *Server) isRestrictedFileUser(r *http.Request) bool {
	if s.users == nil {
		return false
	}
	principal, ok := auth.PrincipalFromContext(r.Context())
	return ok && !principal.IsAdmin() && !principal.Legacy
}

func (s *Server) authorizedFilePaths(r *http.Request, sourceID, requested string, allowAncestor bool) ([]string, error) {
	if !s.isRestrictedFileUser(r) {
		return nil, nil
	}
	if sourceID != "" && sourceID != "my" {
		return nil, filemanagerError("PATH_FORBIDDEN", "source is not assigned to this account")
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	paths, err := s.users.AllowedPaths(r.Context(), principal.UserID)
	if err != nil {
		return nil, err
	}
	work := s.config.WorkDir
	if work == "" {
		work = "/data"
	}
	target := filepath.Join(work, requested)
	realTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		return nil, filemanagerError("PATH_FORBIDDEN", "path cannot be resolved")
	}
	realPaths := make([]string, 0, len(paths))
	for _, path := range paths {
		real, err := filepath.EvalSymlinks(path)
		if err != nil {
			return nil, filemanagerError("PATH_FORBIDDEN", "assigned path cannot be resolved")
		}
		realPaths = append(realPaths, real)
	}
	if !pathWithinAny(realTarget, realPaths, allowAncestor) {
		return nil, filemanagerError("PATH_FORBIDDEN", "path is not assigned to this account")
	}
	return realPaths, nil
}

func pathWithinAny(path string, roots []string, allowAncestor bool) bool {
	path = filepath.Clean(path)
	for _, root := range roots {
		root = filepath.Clean(root)
		if path == root || strings.HasPrefix(path, root+string(filepath.Separator)) {
			return true
		}
		if allowAncestor && strings.HasPrefix(root, path+string(filepath.Separator)) {
			return true
		}
	}
	return false
}
