package console

import (
	"encoding/json"
	"net/http"
	"net/mail"
	"os"
	"strings"
)

type onboardingResponse struct {
	onboardingState
	Readiness onboardingReadiness `json:"readiness"`
}

type onboardingReadiness struct {
	StorageConfigured bool   `json:"storageConfigured"`
	StorageReason     string `json:"storageReason,omitempty"`
}

func (s *Server) registerOnboardingRoutes() {
	s.mux.HandleFunc("/api/v1/onboarding", s.requireAdminWrites(s.handleOnboarding))
}

func (s *Server) handleOnboarding(w http.ResponseWriter, r *http.Request) {
	if s.onboarding == nil {
		http.Error(w, "onboarding store not initialized", http.StatusServiceUnavailable)
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.jsonOK(w, s.onboardingResponse(s.onboarding.get()))
	case http.MethodPatch:
		s.patchOnboarding(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) patchOnboarding(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Step         string  `json:"step"`
		Status       string  `json:"status"`
		ContactEmail *string `json:"contactEmail"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if !validOnboardingStep(req.Step) {
		http.Error(w, "invalid onboarding step", http.StatusBadRequest)
		return
	}
	if req.Status != onboardingPending && req.Status != onboardingCompleted && req.Status != onboardingSkipped {
		http.Error(w, "invalid onboarding status", http.StatusBadRequest)
		return
	}
	if req.ContactEmail != nil {
		email := strings.TrimSpace(*req.ContactEmail)
		if email != "" {
			addr, err := mail.ParseAddress(email)
			if err != nil || addr.Address != email {
				http.Error(w, "invalid contact email", http.StatusBadRequest)
				return
			}
		}
		req.ContactEmail = &email
	}
	if req.Step == "securityContact" && req.Status == onboardingCompleted &&
		(req.ContactEmail == nil || *req.ContactEmail == "") {
		http.Error(w, "contact email is required", http.StatusBadRequest)
		return
	}
	state, err := s.onboarding.update(req.Step, req.Status, req.ContactEmail)
	if err != nil {
		http.Error(w, "failed to persist onboarding state", http.StatusInternalServerError)
		return
	}
	s.jsonOK(w, s.onboardingResponse(state))
}

func (s *Server) onboardingResponse(state onboardingState) onboardingResponse {
	workDir := s.config.WorkDir
	if workDir == "" {
		workDir = "/data"
	}
	ready := false
	reason := "工作区目录不存在"
	if info, err := os.Stat(workDir); err == nil && info.IsDir() {
		ready = true
		reason = ""
	} else if err == nil {
		reason = "工作区路径不是目录"
	}
	return onboardingResponse{
		onboardingState: state,
		Readiness:       onboardingReadiness{StorageConfigured: ready, StorageReason: reason},
	}
}
