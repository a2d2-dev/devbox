package console

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"

	eventlog "github.com/a2d2-dev/devbox/pkg/syslog"
	"github.com/a2d2-dev/devbox/pkg/system"
)

// registerSupervisorRoutes registers supervisor-related API routes
func (s *Server) registerSupervisorRoutes() {
	s.mux.HandleFunc("/api/v1/supervisor/status", s.handleSupervisorStatus)
	s.mux.HandleFunc("/api/v1/supervisor/resources", s.handleSupervisorResources)
	s.mux.HandleFunc("/api/v1/supervisor/services/", s.requireAdminWrites(s.handleSupervisorService))
}

type supervisorResource struct {
	Name           string   `json:"name"`
	Group          string   `json:"group"`
	StateName      string   `json:"statename"`
	PID            int      `json:"pid"`
	Start          int64    `json:"start"`
	Now            int64    `json:"now"`
	Description    string   `json:"description"`
	Directory      string   `json:"directory,omitempty"`
	CPUPercent     *float64 `json:"cpuPercent"`
	CPUTimeSeconds float64  `json:"cpuTimeSeconds"`
	RuntimeSeconds int64    `json:"runtimeSeconds"`
	MemBytes       uint64   `json:"memBytes"`
	ReadBps        *float64 `json:"readBps"`
	WriteBps       *float64 `json:"writeBps"`
	DiskIOStatus   string   `json:"diskIOStatus"`
	Ports          []int    `json:"ports"`
	PortsStatus    string   `json:"portsStatus"`
	NetworkStatus  string   `json:"networkStatus"`
}

func (s *Server) handleSupervisorResources(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.supervisorMgr == nil {
		http.Error(w, "supervisor not available", http.StatusServiceUnavailable)
		return
	}
	status, err := s.supervisorMgr.GetStatus()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	basics, err := system.ListProcesses()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if s.processResources == nil {
		s.processResources = newProcessResourceSampler()
	}
	resourceByPID := make(map[int]processResourceInfo)
	for _, resource := range s.processResources.sample(r.Context(), basics) {
		resourceByPID[resource.PID] = resource
	}
	services := make([]supervisorResource, 0, len(status.Processes))
	for _, proc := range status.Processes {
		resource := resourceByPID[proc.PID]
		if resource.IOStatus == "" {
			resource.IOStatus = "unavailable"
		}
		if resource.PortsStatus == "" {
			resource.PortsStatus = "unavailable"
		}
		ports := append([]int(nil), resource.Ports...)
		if len(ports) == 0 && proc.Port != "" {
			if port, err := strconv.Atoi(proc.Port); err == nil && port > 0 {
				ports = []int{port}
			}
		}
		sort.Ints(ports)
		services = append(services, supervisorResource{
			Name: proc.Name, Group: proc.Group, StateName: proc.StateName, PID: proc.PID,
			Start: proc.Start, Now: proc.Now, Description: proc.Description, Directory: proc.Directory,
			CPUPercent: resource.CPUPercent, CPUTimeSeconds: resource.CPUTimeSeconds,
			RuntimeSeconds: resource.RuntimeSeconds, MemBytes: resource.MemBytes,
			ReadBps: resource.ReadBps, WriteBps: resource.WriteBps, DiskIOStatus: resource.IOStatus,
			Ports: ports, PortsStatus: resource.PortsStatus, NetworkStatus: "unsupported",
		})
	}
	s.jsonOK(w, map[string]any{"hostname": status.Hostname, "ip": status.IP, "services": services})
}

func (s *Server) handleSupervisorStatus(w http.ResponseWriter, r *http.Request) {
	if s.supervisorMgr == nil {
		http.Error(w, "supervisor not available", http.StatusServiceUnavailable)
		return
	}

	status, err := s.supervisorMgr.GetStatus()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.jsonOK(w, status)
}

// handleSupervisorService handles:
//
//	GET  /api/v1/supervisor/services/{name}/logs
//	POST /api/v1/supervisor/services/{name}/control
func (s *Server) handleSupervisorService(w http.ResponseWriter, r *http.Request) {
	if s.supervisorMgr == nil {
		http.Error(w, "supervisor not available", http.StatusServiceUnavailable)
		return
	}

	// Parse path: /api/v1/supervisor/services/{name}/{action}
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/supervisor/services/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) != 2 || parts[0] == "" {
		http.Error(w, "invalid path, expected /api/v1/supervisor/services/{name}/{logs|control}", http.StatusBadRequest)
		return
	}

	name := parts[0]
	action := parts[1]

	switch action {
	case "logs":
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		logResp, err := s.supervisorMgr.GetServiceLogs(name)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.jsonOK(w, logResp)

	case "control":
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
		if req.Action != "start" && req.Action != "stop" && req.Action != "restart" {
			http.Error(w, "action must be start, stop, or restart", http.StatusBadRequest)
			return
		}
		if err := s.supervisorMgr.ControlService(name, req.Action); err != nil {
			s.recordEvent(r, eventlog.Input{
				Level: "error", Module: "supervisor", Event: "Supervisor 服务控制失败",
				EventType: "SERVICE_" + strings.ToUpper(req.Action), Outcome: "failure",
				ResourceKind: "service", ResourceID: name, Payload: map[string]any{"action": req.Action},
			})
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.recordEvent(r, eventlog.Input{
			Level: "warning", Module: "supervisor", Event: "Supervisor 服务" + req.Action,
			EventType: "SERVICE_" + strings.ToUpper(req.Action), Outcome: "success",
			ResourceKind: "service", ResourceID: name, Payload: map[string]any{"action": req.Action},
		})
		s.jsonOK(w, map[string]string{"status": "ok"})

	default:
		http.Error(w, "unknown action: "+action, http.StatusNotFound)
	}
}
