package console

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/a2d2-dev/devbox/pkg/apps"
)

// Docker 首屏与 Issue #2 共用同一个 controller 实例。controller 未装配或未实现
// DockerController 时，读接口返回明确空态，写接口返回统一 capability 错误。
func (s *Server) registerDockerRoutes() {
	s.mux.HandleFunc("/api/v1/docker/overview", s.handleDockerOverview)
	s.mux.HandleFunc("/api/v1/docker/stats", s.handleDockerStats)
	s.mux.HandleFunc("/api/v1/docker/service", s.requireAdmin(s.handleDockerService))
	s.mux.HandleFunc("/api/v1/docker/autostart", s.requireAdmin(s.handleDockerAutostart))
	s.mux.HandleFunc("/api/v1/docker/storage/plan", s.requireAdmin(s.handleDockerStoragePlan))
	s.mux.HandleFunc("/api/v1/docker/storage/execute", s.requireAdmin(s.handleDockerStorageExecute))
}

func (s *Server) dockerController() (apps.DockerController, bool) {
	controller, ok := s.controller.(apps.DockerController)
	return controller, ok && controller != nil
}

func (s *Server) handleDockerOverview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	controller, ok := s.dockerController()
	if !ok {
		s.jsonOK(w, apps.DockerOverview{
			Service:     apps.DockerServiceSummary{State: apps.DockerServiceNotInstalled, Diagnostic: "Docker 管理能力未装配"},
			IdleSummary: "不可用", CheckedAt: time.Now(),
		})
		return
	}
	overview, err := controller.DockerOverview(r.Context())
	if err != nil {
		writeAppErr(w, err)
		return
	}
	s.jsonOK(w, overview)
}

func (s *Server) handleDockerStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	controller, ok := s.dockerController()
	if !ok {
		s.jsonOK(w, apps.DockerStats{Diagnostic: "Docker 管理能力未装配", SampledAt: time.Now()})
		return
	}
	stats, err := controller.DockerStats(r.Context())
	if err != nil {
		writeAppErr(w, err)
		return
	}
	s.jsonOK(w, stats)
}

func (s *Server) handleDockerService(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	controller, ok := s.dockerController()
	if !ok {
		writeAppErr(w, apps.CapabilityErr("Docker 管理能力未装配"))
		return
	}
	var req apps.DockerServiceActionRequest
	if err := decodeDockerRequest(r, &req); err != nil {
		writeAppErr(w, err)
		return
	}
	overview, err := controller.DockerServiceAction(r.Context(), req)
	if err != nil {
		writeAppErr(w, err)
		return
	}
	s.jsonOK(w, overview)
}

func (s *Server) handleDockerAutostart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	controller, ok := s.dockerController()
	if !ok {
		writeAppErr(w, apps.CapabilityErr("Docker 管理能力未装配"))
		return
	}
	var req apps.DockerAutostartRequest
	if err := decodeDockerRequest(r, &req); err != nil {
		writeAppErr(w, err)
		return
	}
	overview, err := controller.SetDockerAutostart(r.Context(), req)
	if err != nil {
		writeAppErr(w, err)
		return
	}
	s.jsonOK(w, overview)
}

func (s *Server) handleDockerStoragePlan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	controller, ok := s.dockerController()
	if !ok {
		writeAppErr(w, apps.CapabilityErr("Docker 管理能力未装配"))
		return
	}
	var req apps.DockerMigrationRequest
	if err := decodeDockerRequest(r, &req); err != nil {
		writeAppErr(w, err)
		return
	}
	plan, err := controller.PlanDockerMigration(r.Context(), req)
	if err != nil {
		writeAppErr(w, err)
		return
	}
	s.jsonOK(w, plan)
}

func (s *Server) handleDockerStorageExecute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	controller, ok := s.dockerController()
	if !ok {
		writeAppErr(w, apps.CapabilityErr("Docker 管理能力未装配"))
		return
	}
	var req apps.DockerMigrationExecuteRequest
	if err := decodeDockerRequest(r, &req); err != nil {
		writeAppErr(w, err)
		return
	}
	result, err := controller.ExecuteDockerMigration(r.Context(), req)
	if err != nil {
		writeAppErr(w, err)
		return
	}
	s.jsonOK(w, result)
}

func decodeDockerRequest(r *http.Request, out any) error {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return apps.ValidationErr("请求内容无效: " + err.Error())
	}
	return nil
}
