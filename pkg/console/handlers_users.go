package console

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/a2d2-dev/devbox/pkg/users"
)

func (s *Server) registerUserRoutes() {
	s.mux.HandleFunc("/api/v1/users", s.admin(s.handleUsers))
	s.mux.HandleFunc("/api/v1/users/", s.admin(s.handleUser))
	s.mux.HandleFunc("/api/v1/user-groups", s.admin(s.handleGroups))
	s.mux.HandleFunc("/api/v1/user-groups/", s.admin(s.handleGroup))
	s.mux.HandleFunc("/api/v1/file-roots", s.admin(s.handleRoots))
	s.mux.HandleFunc("/api/v1/file-roots/", s.admin(s.handleRoot))
}

func (s *Server) admin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.auth == nil {
			http.Error(w, "authentication unavailable", http.StatusServiceUnavailable)
			return
		}
		s.auth.RequireAdmin(next)(w, r)
	}
}

func (s *Server) requireUsers(w http.ResponseWriter) bool {
	if s.users == nil {
		http.Error(w, "user database unavailable", http.StatusServiceUnavailable)
		return false
	}
	return true
}

func (s *Server) handleUsers(w http.ResponseWriter, r *http.Request) {
	if !s.requireUsers(w) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		items, err := s.users.ListUsers(r.Context(), r.URL.Query().Get("search"))
		if err != nil {
			s.userError(w, err)
			return
		}
		type item struct {
			users.User
			RootIDs []string `json:"rootIds"`
		}
		out := make([]item, 0, len(items))
		for _, u := range items {
			ids, err := s.users.UserRootIDs(r.Context(), u.ID)
			if err != nil {
				s.userError(w, err)
				return
			}
			out = append(out, item{User: u, RootIDs: ids})
		}
		s.jsonOK(w, out)
	case http.MethodPost:
		var req struct {
			Username, DisplayName, Password string
			Role                            users.Role
			Enabled                         *bool
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		enabled := true
		if req.Enabled != nil {
			enabled = *req.Enabled
		}
		u, err := s.users.CreateUser(r.Context(), users.CreateUser{Username: req.Username, DisplayName: req.DisplayName, Password: req.Password, Role: req.Role, Enabled: enabled})
		if err != nil {
			s.userError(w, err)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(u)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleUser(w http.ResponseWriter, r *http.Request) {
	if !s.requireUsers(w) {
		return
	}
	parts := pathParts(r.URL.Path, "/api/v1/users/")
	if len(parts) == 0 {
		http.NotFound(w, r)
		return
	}
	id := parts[0]
	if len(parts) == 2 && parts[1] == "access-roots" {
		s.handleUserRoots(w, r, id)
		return
	}
	if len(parts) != 1 {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodPut:
		var req struct {
			DisplayName *string
			Password    *string
			Role        *users.Role
			Enabled     *bool
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		u, err := s.users.UpdateUser(r.Context(), id, users.UpdateUser{DisplayName: req.DisplayName, Password: req.Password, Role: req.Role, Enabled: req.Enabled})
		if err != nil {
			s.userError(w, err)
			return
		}
		s.auth.RevokeUser(id)
		s.jsonOK(w, u)
	case http.MethodDelete:
		if err := s.users.DeleteUser(r.Context(), id); err != nil {
			s.userError(w, err)
			return
		}
		s.auth.RevokeUser(id)
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleUserRoots(w http.ResponseWriter, r *http.Request, id string) {
	switch r.Method {
	case http.MethodGet:
		ids, err := s.users.UserRootIDs(r.Context(), id)
		if err != nil {
			s.userError(w, err)
			return
		}
		s.jsonOK(w, ids)
	case http.MethodPut:
		var req struct {
			RootIDs []string `json:"rootIds"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		if err := s.users.SetUserRoots(r.Context(), id, req.RootIDs); err != nil {
			s.userError(w, err)
			return
		}
		s.jsonOK(w, req.RootIDs)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleGroups(w http.ResponseWriter, r *http.Request) {
	if !s.requireUsers(w) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		groups, err := s.users.ListGroups(r.Context(), r.URL.Query().Get("search"))
		if err != nil {
			s.userError(w, err)
			return
		}
		type item struct {
			users.Group
			RootIDs []string `json:"rootIds"`
		}
		out := make([]item, 0, len(groups))
		for _, g := range groups {
			ids, err := s.users.GroupRootIDs(r.Context(), g.ID)
			if err != nil {
				s.userError(w, err)
				return
			}
			out = append(out, item{Group: g, RootIDs: ids})
		}
		s.jsonOK(w, out)
	case http.MethodPost:
		var req struct {
			Name, Description string
			MemberIDs         []string `json:"memberIds"`
			RootIDs           []string `json:"rootIds"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		g, err := s.users.CreateGroup(r.Context(), req.Name, req.Description, req.MemberIDs)
		if err == nil && len(req.RootIDs) > 0 {
			err = s.users.SetGroupRoots(r.Context(), g.ID, req.RootIDs)
		}
		if err != nil {
			s.userError(w, err)
			return
		}
		g.MemberIDs = req.MemberIDs
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(g)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleGroup(w http.ResponseWriter, r *http.Request) {
	if !s.requireUsers(w) {
		return
	}
	parts := pathParts(r.URL.Path, "/api/v1/user-groups/")
	if len(parts) == 0 {
		http.NotFound(w, r)
		return
	}
	id := parts[0]
	if len(parts) == 2 && parts[1] == "access-roots" {
		switch r.Method {
		case http.MethodGet:
			ids, err := s.users.GroupRootIDs(r.Context(), id)
			if err != nil {
				s.userError(w, err)
				return
			}
			s.jsonOK(w, ids)
		case http.MethodPut:
			var req struct {
				RootIDs []string `json:"rootIds"`
			}
			if !decodeJSON(w, r, &req) {
				return
			}
			if err := s.users.SetGroupRoots(r.Context(), id, req.RootIDs); err != nil {
				s.userError(w, err)
				return
			}
			s.jsonOK(w, req.RootIDs)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}
	if len(parts) != 1 {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodPut:
		var req struct {
			Name, Description string
			MemberIDs         []string `json:"memberIds"`
			RootIDs           []string `json:"rootIds"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		g, err := s.users.UpdateGroup(r.Context(), id, req.Name, req.Description, req.MemberIDs)
		if err == nil {
			err = s.users.SetGroupRoots(r.Context(), id, req.RootIDs)
		}
		if err != nil {
			s.userError(w, err)
			return
		}
		s.jsonOK(w, g)
	case http.MethodDelete:
		if err := s.users.DeleteGroup(r.Context(), id); err != nil {
			s.userError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleRoots(w http.ResponseWriter, r *http.Request) {
	if !s.requireUsers(w) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		roots, err := s.users.ListRoots(r.Context())
		if err != nil {
			s.userError(w, err)
			return
		}
		s.jsonOK(w, roots)
	case http.MethodPost:
		var req struct{ Name, Path string }
		if !decodeJSON(w, r, &req) {
			return
		}
		path, err := s.validateRootPath(req.Path)
		if err != nil {
			s.userError(w, err)
			return
		}
		root, err := s.users.CreateRoot(r.Context(), req.Name, path)
		if err != nil {
			s.userError(w, err)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(root)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if !s.requireUsers(w) {
		return
	}
	parts := pathParts(r.URL.Path, "/api/v1/file-roots/")
	if len(parts) != 1 {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := s.users.DeleteRoot(r.Context(), parts[0]); err != nil {
		s.userError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) validateRootPath(path string) (string, error) {
	path = filepath.Clean(strings.TrimSpace(path))
	if !filepath.IsAbs(path) {
		return "", errors.New("file root must be an absolute path")
	}
	work := s.config.WorkDir
	if work == "" {
		work = "/data"
	}
	work = filepath.Clean(work)
	if path != work && !strings.HasPrefix(path, work+string(filepath.Separator)) {
		return "", errors.New("file root must be inside console.work_dir")
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", errors.New("file root does not exist")
	}
	if !info.IsDir() {
		return "", errors.New("file root must be a directory")
	}
	return path, nil
}

func (s *Server) userError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, users.ErrConflict):
		status = http.StatusConflict
	case errors.Is(err, users.ErrNotFound):
		status = http.StatusNotFound
	case errors.Is(err, users.ErrLastAdmin):
		status = http.StatusConflict
	case errors.Is(err, users.ErrInvalidUsername), errors.Is(err, users.ErrWeakPassword), errors.Is(err, users.ErrInvalidRole), strings.Contains(err.Error(), "required"), strings.Contains(err.Error(), "root"):
		status = http.StatusBadRequest
	case strings.Contains(strings.ToLower(err.Error()), "foreign key"):
		status = http.StatusBadRequest
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		http.Error(w, "invalid request: "+err.Error(), http.StatusBadRequest)
		return false
	}
	return true
}
func pathParts(path, prefix string) []string {
	rest := strings.Trim(strings.TrimPrefix(path, prefix), "/")
	if rest == "" {
		return nil
	}
	return strings.Split(rest, "/")
}
