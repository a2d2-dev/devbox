package supervisor

import (
	"bufio"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"go.uber.org/zap"
)

// Manager communicates with supervisord via XML-RPC over a Unix socket.
type Manager struct {
	socketPath string
	confDir    string
	logger     *zap.Logger
	portRegex  *regexp.Regexp
}

// NewManager creates a new supervisor manager.
// Returns nil if the socket does not exist.
func NewManager(socketPath, confDir string, logger *zap.Logger) *Manager {
	if socketPath == "" {
		socketPath = "/var/run/supervisor.sock"
	}
	if confDir == "" {
		confDir = "/etc/supervisor/conf.d"
	}
	if _, err := os.Stat(socketPath); err != nil {
		logger.Warn("Supervisor socket not found, supervisor features disabled",
			zap.String("socket", socketPath), zap.Error(err))
		return nil
	}
	return &Manager{
		socketPath: socketPath,
		confDir:    confDir,
		logger:     logger,
		portRegex:  regexp.MustCompile(`(?:--port[= ]|[ ]-p[ ])(\d{4,5})`),
	}
}

// xmlRPCCall performs an XML-RPC call over the Unix socket
func (m *Manager) xmlRPCCall(method string, params ...string) ([]byte, error) {
	var paramXML string
	for _, p := range params {
		paramXML += "<param><value><string>" + xmlEscape(p) + "</string></value></param>"
	}

	body := fmt.Sprintf(`<?xml version="1.0"?>
<methodCall>
<methodName>%s</methodName>
<params>%s</params>
</methodCall>`, method, paramXML)

	// Create HTTP request
	req, err := http.NewRequest("POST", "/RPC2", strings.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "text/xml")

	// Dial Unix socket
	conn, err := net.DialTimeout("unix", m.socketPath, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("dial supervisor socket: %w", err)
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(10 * time.Second))

	// Write HTTP request
	err = req.Write(conn)
	if err != nil {
		return nil, fmt.Errorf("write request: %w", err)
	}

	// Read HTTP response
	resp, err := http.ReadResponse(bufio.NewReader(conn), req)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	return respBody, nil
}

// GetStatus returns the full supervisor status including hostname, IP, and all processes
func (m *Manager) GetStatus() (*StatusResponse, error) {
	procs, err := m.getAllProcessInfo()
	if err != nil {
		return nil, err
	}

	hostname, _ := os.Hostname()
	ip := getLocalIP()

	result := make([]ProcessInfo, 0, len(procs))
	for _, p := range procs {
		port := m.extractPort(p.Name)
		groupLabel, groupOrder := ClassifyGroup(p.Name)

		result = append(result, ProcessInfo{
			Name:        p.Name,
			Group:       groupLabel,
			GroupOrder:  groupOrder,
			StateName:   p.StateName,
			PID:         p.PID,
			Start:       p.Start,
			Now:         p.Now,
			Description: p.Description,
			Port:        port,
		})
	}

	return &StatusResponse{
		Hostname:  hostname,
		IP:        ip,
		Processes: result,
	}, nil
}

// GetServiceLogs returns the tail of stdout log for a service
func (m *Manager) GetServiceLogs(name string) (*LogResponse, error) {
	respBody, err := m.xmlRPCCall("supervisor.tailProcessStdoutLog", name, "0", "32768")
	if err != nil {
		return &LogResponse{Name: name, Log: err.Error()}, nil
	}

	logText := extractStringFromResponse(respBody)
	return &LogResponse{Name: name, Log: logText}, nil
}

// ControlService performs start/stop/restart on a service
func (m *Manager) ControlService(name, action string) error {
	var method string
	switch action {
	case "start":
		method = "supervisor.startProcess"
	case "stop":
		method = "supervisor.stopProcess"
	case "restart":
		// Restart = stop + start
		m.xmlRPCCall("supervisor.stopProcess", name, "true")
		time.Sleep(500 * time.Millisecond)
		method = "supervisor.startProcess"
	default:
		return fmt.Errorf("unknown action: %s (expected start/stop/restart)", action)
	}

	_, err := m.xmlRPCCall(method, name, "true")
	return err
}

// --- internal types for XML-RPC parsing ---

type rawProcessInfo struct {
	Name        string
	StateName   string
	PID         int
	Start       int64
	Now         int64
	Description string
}

func (m *Manager) getAllProcessInfo() ([]rawProcessInfo, error) {
	respBody, err := m.xmlRPCCall("supervisor.getAllProcessInfo")
	if err != nil {
		return nil, err
	}
	return parseProcessInfoArray(respBody)
}

func (m *Manager) extractPort(name string) string {
	confPath := fmt.Sprintf("%s/%s.conf", m.confDir, name)
	data, err := os.ReadFile(confPath)
	if err != nil {
		return ""
	}
	match := m.portRegex.FindSubmatch(data)
	if match != nil {
		return string(match[1])
	}
	return ""
}

// --- XML-RPC response parsing ---

// parseProcessInfoArray parses the XML-RPC response from supervisor.getAllProcessInfo()
func parseProcessInfoArray(data []byte) ([]rawProcessInfo, error) {
	var result []rawProcessInfo

	decoder := xml.NewDecoder(bytes.NewReader(data))
	// Navigate to the array of structs
	// Response format: <methodResponse><params><param><value><array><data><value><struct>...

	type xmlMember struct {
		Name  string `xml:"name"`
		Value struct {
			Inner []byte `xml:",innerxml"`
		} `xml:"value"`
	}

	inArray := false
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "array" {
				inArray = true
			}
			if inArray && t.Name.Local == "struct" {
				// Parse struct members
				proc := rawProcessInfo{}
				for {
					innerTok, err := decoder.Token()
					if err != nil {
						break
					}
					switch it := innerTok.(type) {
					case xml.StartElement:
						if it.Name.Local == "member" {
							var mem xmlMember
							decoder.DecodeElement(&mem, &it)
							val := extractValue(mem.Value.Inner)
							switch mem.Name {
							case "name":
								proc.Name = val
							case "statename":
								proc.StateName = val
							case "pid":
								fmt.Sscanf(val, "%d", &proc.PID)
							case "start":
								fmt.Sscanf(val, "%d", &proc.Start)
							case "now":
								fmt.Sscanf(val, "%d", &proc.Now)
							case "description":
								proc.Description = val
							}
						}
					case xml.EndElement:
						if it.Name.Local == "struct" {
							goto doneStruct
						}
					}
				}
			doneStruct:
				if proc.Name != "" {
					result = append(result, proc)
				}
			}
		}
	}
	return result, nil
}

// extractValue extracts the text content from XML value inner XML
func extractValue(inner []byte) string {
	// inner looks like: <string>value</string> or <int>123</int> or <i4>123</i4>
	decoder := xml.NewDecoder(bytes.NewReader(inner))
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		if _, ok := tok.(xml.StartElement); ok {
			// Read the char data
			tok2, err := decoder.Token()
			if err != nil {
				break
			}
			if cd, ok := tok2.(xml.CharData); ok {
				return string(cd)
			}
		}
	}
	// Fallback: try raw content
	s := strings.TrimSpace(string(inner))
	return s
}

// extractStringFromResponse extracts the first string value from an XML-RPC response
func extractStringFromResponse(data []byte) string {
	// Look for <string>...</string> in the response
	decoder := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}
		if se, ok := tok.(xml.StartElement); ok && se.Name.Local == "string" {
			tok2, err := decoder.Token()
			if err != nil {
				break
			}
			if cd, ok := tok2.(xml.CharData); ok {
				return string(cd)
			}
		}
	}
	return ""
}

func xmlEscape(s string) string {
	var buf bytes.Buffer
	xml.EscapeText(&buf, []byte(s))
	return buf.String()
}

func getLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "unknown"
	}
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() && ipnet.IP.To4() != nil {
			return ipnet.IP.String()
		}
	}
	return "unknown"
}

