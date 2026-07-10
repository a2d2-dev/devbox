package supervisor

// ProcessInfo represents a single supervisord process
type ProcessInfo struct {
	Name        string `json:"name"`
	Group       string `json:"group"`
	GroupOrder  int    `json:"groupOrder"`
	StateName   string `json:"statename"`
	PID         int    `json:"pid"`
	Start       int64  `json:"start"`
	Now         int64  `json:"now"`
	Description string `json:"description"`
	Port        string `json:"port,omitempty"`
	Directory   string `json:"directory,omitempty"`
}

// StatusResponse is the full status response for the API
type StatusResponse struct {
	Hostname  string        `json:"hostname"`
	IP        string        `json:"ip"`
	Processes []ProcessInfo `json:"processes"`
}

// LogResponse is the response for log tail requests
type LogResponse struct {
	Name string `json:"name"`
	Log  string `json:"log"`
}

// ControlRequest is the request body for service control
type ControlRequest struct {
	Action string `json:"action"` // start, stop, restart
}

// ServiceGroup maps a name prefix to a group label and sort order
type ServiceGroup struct {
	Prefix string
	Label  string
	Order  int
}

// DefaultServiceGroups maps service name prefixes to dashboard groups.
var DefaultServiceGroups = []ServiceGroup{
	{"rise-global", "Rise Global", 1},
	{"rise-cloud", "Rise Cloud", 2},
	{"edge-", "Edge Platform", 3},
	{"camp-", "CAMP", 4},
	{"frpc", "Network", 5},
	{"gost", "Network", 5},
	{"easytier", "Network", 5},
	{"sing-box", "Network", 5},
	{"kcn", "Network", 5},
	{"routa", "Network", 5},
	{"browsidian", "Dev Tools", 6},
	{"claudecodeui", "Dev Tools", 6},
	{"opencode", "Dev Tools", 6},
	{"opcode", "Dev Tools", 6},
	{"crush", "Dev Tools", 6},
	{"happy", "Dev Tools", 6},
	{"paperclip", "Dev Tools", 6},
	{"github-", "Infra", 7},
	{"supervisor-", "Infra", 7},
}

// ClassifyGroup returns the group label and order for a service name
func ClassifyGroup(name string) (string, int) {
	for _, g := range DefaultServiceGroups {
		if len(name) >= len(g.Prefix) && name[:len(g.Prefix)] == g.Prefix {
			return g.Label, g.Order
		}
	}
	return "Other", 99
}
