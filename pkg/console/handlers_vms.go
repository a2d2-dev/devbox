package console

import (
	"encoding/json"
	"net/http"
	"strings"
)

func (s *Server) registerVMRoutes() {
	s.mux.HandleFunc("/api/v1/vms", s.handleVMs)
	s.mux.HandleFunc("/api/v1/vms/", s.handleVM)
}

func (s *Server) handleVMs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.vmManager == nil {
		http.Error(w, "VM manager not available", http.StatusServiceUnavailable)
		return
	}
	list, err := s.vmManager.List(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.jsonOK(w, list)
}

func (s *Server) handleVM(w http.ResponseWriter, r *http.Request) {
	if s.vmManager == nil {
		http.Error(w, "VM manager not available", http.StatusServiceUnavailable)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/vms/")
	parts := strings.SplitN(path, "/", 2)
	if parts[0] == "" {
		http.Error(w, "missing VM name", http.StatusBadRequest)
		return
	}
	name := parts[0]
	if len(parts) == 1 {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		vm, err := s.vmManager.Get(r.Context(), name)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.jsonOK(w, vm)
		return
	}

	if parts[1] != "control" {
		http.Error(w, "unknown VM action path", http.StatusNotFound)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Action string `json:"action"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := s.vmManager.Control(r.Context(), name, req.Action); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.jsonOK(w, map[string]string{"status": "ok"})
}
