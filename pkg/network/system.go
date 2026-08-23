package network

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
)

type Listener struct {
	Protocol string `json:"protocol"`
	Address  string `json:"address"`
	Port     int    `json:"port"`
	Process  string `json:"process,omitempty"`
}

type RemoteAccess struct {
	Listeners []Listener `json:"listeners"`
	TunnelIPs []string   `json:"tunnelIPs"`
	HTTPS     bool       `json:"https"`
}

func (c *Collector) RemoteAccess() RemoteAccess {
	var result RemoteAccess
	out, err := c.Run("ss", "-H", "-lntp")
	if err == nil {
		result.Listeners = parseListeners(string(out))
	}
	status, err := c.Snapshot()
	if err == nil {
		for _, iface := range status.Interfaces {
			if iface.Name == "tun0" {
				result.TunnelIPs = append(result.TunnelIPs, iface.IPv4...)
				result.TunnelIPs = append(result.TunnelIPs, iface.IPv6...)
			}
		}
	}
	for _, l := range result.Listeners {
		if l.Port == 443 || l.Port == 8443 || strings.Contains(strings.ToLower(l.Process), "https") {
			result.HTTPS = true
		}
	}
	return result
}

func parseListeners(raw string) []Listener {
	var result []Listener
	for _, line := range strings.Split(strings.TrimSpace(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		local := fields[3]
		host, portText, err := net.SplitHostPort(local)
		if err != nil {
			idx := strings.LastIndex(local, ":")
			if idx < 0 {
				continue
			}
			host, portText = local[:idx], local[idx+1:]
		}
		port, err := strconv.Atoi(portText)
		if err != nil {
			continue
		}
		process := ""
		if len(fields) > 5 {
			process = strings.Join(fields[5:], " ")
		}
		result = append(result, Listener{Protocol: "tcp", Address: strings.Trim(host, "[]"), Port: port, Process: process})
	}
	return result
}

type SSHStatus struct {
	Running                bool   `json:"running"`
	Port                   int    `json:"port"`
	PermitRootLogin        string `json:"permitRootLogin"`
	PasswordAuthentication string `json:"passwordAuthentication"`
	PubkeyAuthentication   string `json:"pubkeyAuthentication"`
	Error                  string `json:"error,omitempty"`
}

func (c *Collector) SSHStatus() SSHStatus {
	status := SSHStatus{Port: 22, PermitRootLogin: "unknown", PasswordAuthentication: "unknown", PubkeyAuthentication: "unknown"}
	if out, err := c.Run("systemctl", "is-active", "sshd"); err == nil {
		status.Running = strings.TrimSpace(string(out)) == "active"
	} else if out, err = c.Run("systemctl", "is-active", "ssh"); err == nil {
		status.Running = strings.TrimSpace(string(out)) == "active"
	}
	out, err := c.Run("sshd", "-T")
	if err != nil {
		status.Error = err.Error()
		return status
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		switch strings.ToLower(fields[0]) {
		case "port":
			status.Port, _ = strconv.Atoi(fields[1])
		case "permitrootlogin":
			status.PermitRootLogin = fields[1]
		case "passwordauthentication":
			status.PasswordAuthentication = fields[1]
		case "pubkeyauthentication":
			status.PubkeyAuthentication = fields[1]
		}
	}
	return status
}

type SSHChange struct {
	Port                   int    `json:"port"`
	PermitRootLogin        string `json:"permitRootLogin"`
	PasswordAuthentication string `json:"passwordAuthentication"`
}

func PreviewSSH(current SSHStatus, change SSHChange, occupied []Listener) (string, error) {
	if err := ValidatePort(change.Port); err != nil {
		return "", err
	}
	for _, l := range occupied {
		if l.Port == change.Port && change.Port != current.Port {
			return "", fmt.Errorf("port %d is already in use by %s", change.Port, l.Process)
		}
	}
	allowedRoot := map[string]bool{"no": true, "prohibit-password": true, "yes": true}
	if !allowedRoot[change.PermitRootLogin] {
		return "", errors.New("invalid PermitRootLogin value")
	}
	if change.PasswordAuthentication != "yes" && change.PasswordAuthentication != "no" {
		return "", errors.New("invalid PasswordAuthentication value")
	}
	return fmt.Sprintf("--- sshd_config.current\n+++ sshd_config.preview\n@@ managed by DevBox @@\n-Port %d\n+Port %d\n-PermitRootLogin %s\n+PermitRootLogin %s\n-PasswordAuthentication %s\n+PasswordAuthentication %s\n", current.Port, change.Port, current.PermitRootLogin, change.PermitRootLogin, current.PasswordAuthentication, change.PasswordAuthentication), nil
}

type DDNSConfig struct {
	Provider      string `json:"provider"`
	Domain        string `json:"domain"`
	CredentialRef string `json:"credentialRef"`
	WebhookURL    string `json:"webhookURL,omitempty"`
	EchoCommand   string `json:"echoCommand,omitempty"`
}

func ValidateDDNS(cfg DDNSConfig) error {
	if cfg.Provider != "cloudflare" && cfg.Provider != "webhook" {
		return errors.New("provider must be cloudflare or webhook")
	}
	if strings.TrimSpace(cfg.Domain) == "" || strings.ContainsAny(cfg.Domain, " /\\") {
		return errors.New("invalid domain")
	}
	if strings.TrimSpace(cfg.CredentialRef) == "" {
		return errors.New("credentialRef is required")
	}
	if cfg.Provider == "webhook" {
		u, err := url.Parse(cfg.WebhookURL)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			return errors.New("invalid webhook URL")
		}
	}
	return nil
}

func PreviewDDNS(cfg DDNSConfig) (string, error) {
	if err := ValidateDDNS(cfg); err != nil {
		return "", err
	}
	if cfg.Provider == "cloudflare" {
		return fmt.Sprintf("Cloudflare DNS update: domain=%s credential=%s value=<public-ip>", cfg.Domain, cfg.CredentialRef), nil
	}
	return fmt.Sprintf("Webhook POST: url=%s domain=%s credential=%s value=<public-ip>", cfg.WebhookURL, cfg.Domain, cfg.CredentialRef), nil
}

func RunDDNSDry(cfg DDNSConfig) (string, error) {
	preview, err := PreviewDDNS(cfg)
	if err != nil {
		return "", err
	}
	if cfg.EchoCommand != "" {
		fields := strings.Fields(cfg.EchoCommand)
		if len(fields) == 0 || (fields[0] != "echo" && fields[0] != "/bin/echo") {
			return "", errors.New("only echo is permitted for DDNS command verification")
		}
		return strings.Join(fields[1:], " "), nil
	}
	return "DRY-RUN: " + preview, nil
}

type FirewallRule struct {
	Direction string `json:"direction"`
	Action    string `json:"action"`
	Protocol  string `json:"protocol"`
	Port      int    `json:"port,omitempty"`
	Source    string `json:"source,omitempty"`
	Interface string `json:"interface,omitempty"`
	Comment   string `json:"comment,omitempty"`
}

type FirewallPreview struct {
	Backend         string `json:"backend"`
	Ruleset         string `json:"ruleset"`
	RollbackCommand string `json:"rollbackCommand"`
	RollbackSeconds int    `json:"rollbackSeconds"`
	DryRun          bool   `json:"dryRun"`
}

func (c *Collector) FirewallRules() (string, string, error) {
	if out, err := c.Run("nft", "list", "ruleset"); err == nil {
		return "nftables", string(out), nil
	}
	if out, err := c.Run("iptables-save"); err == nil {
		return "iptables", string(out), nil
	}
	return "unavailable", "", errors.New("neither nftables nor iptables rules could be read")
}

func RenderFirewall(rules []FirewallRule, sessionIP string) (FirewallPreview, error) {
	if net.ParseIP(sessionIP) == nil {
		return FirewallPreview{}, errors.New("current session IP is required for lockout protection")
	}
	hasTunnel, hasSession := false, false
	input := []string{"    ct state established,related accept", "    iifname \"lo\" accept"}
	output := []string{"    ct state established,related accept", "    oifname \"lo\" accept"}
	for _, rule := range rules {
		if rule.Direction != "in" && rule.Direction != "out" {
			return FirewallPreview{}, errors.New("direction must be in or out")
		}
		if rule.Action != "allow" && rule.Action != "deny" {
			return FirewallPreview{}, errors.New("action must be allow or deny")
		}
		if rule.Protocol != "tcp" && rule.Protocol != "udp" && rule.Protocol != "any" {
			return FirewallPreview{}, errors.New("protocol must be tcp, udp or any")
		}
		if rule.Port != 0 {
			if err := ValidatePort(rule.Port); err != nil {
				return FirewallPreview{}, err
			}
		}
		if rule.Source != "" && rule.Source != "any" {
			if err := ValidateIPOrCIDR(rule.Source); err != nil {
				return FirewallPreview{}, err
			}
		}
		if rule.Interface == "tun0" && rule.Action == "allow" && rule.Direction == "in" {
			hasTunnel = true
		}
		if rule.Source == sessionIP && rule.Action == "allow" && rule.Direction == "in" {
			hasSession = true
		}
		chain := "input"
		endpoint := "ip saddr"
		if rule.Direction == "out" {
			chain, endpoint = "output", "ip daddr"
		}
		if rule.Source != "" && rule.Source != "any" {
			parsed := net.ParseIP(strings.Split(rule.Source, "/")[0])
			if parsed != nil && parsed.To4() == nil {
				if rule.Direction == "out" {
					endpoint = "ip6 daddr"
				} else {
					endpoint = "ip6 saddr"
				}
			}
		}
		parts := []string{}
		if rule.Interface != "" {
			key := "iifname"
			if rule.Direction == "out" {
				key = "oifname"
			}
			parts = append(parts, key, strconv.Quote(rule.Interface))
		}
		if rule.Source != "" && rule.Source != "any" {
			parts = append(parts, endpoint, rule.Source)
		}
		if rule.Protocol != "any" {
			parts = append(parts, rule.Protocol)
		}
		if rule.Port != 0 {
			parts = append(parts, "dport", strconv.Itoa(rule.Port))
		}
		verdict := "accept"
		if rule.Action == "deny" {
			verdict = "drop"
		}
		parts = append(parts, verdict)
		if rule.Comment != "" {
			parts = append(parts, "comment", strconv.Quote(rule.Comment))
		}
		line := "    " + strings.Join(parts, " ")
		if chain == "output" {
			output = append(output, line)
		} else {
			input = append(input, line)
		}
	}
	if !hasTunnel {
		return FirewallPreview{}, errors.New("ruleset rejected: missing inbound allow protection for tun0")
	}
	if !hasSession {
		return FirewallPreview{}, errors.New("ruleset rejected: missing inbound allow protection for current session IP")
	}
	lines := []string{"table inet devbox_preview {", "  chain input {", "    type filter hook input priority 0; policy drop;"}
	lines = append(lines, input...)
	lines = append(lines, "  }", "  chain output {", "    type filter hook output priority 0; policy accept;")
	lines = append(lines, output...)
	lines = append(lines, "  }", "}")
	return FirewallPreview{Backend: "nftables", Ruleset: strings.Join(lines, "\n") + "\n", RollbackCommand: "nft -f <previous-ruleset>", RollbackSeconds: 60, DryRun: true}, nil
}

func ParseSSHDConfig(path string) map[string]string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	result := make(map[string]string)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			result[strings.ToLower(fields[0])] = fields[1]
		}
	}
	return result
}

func SortListeners(items []Listener) {
	sort.Slice(items, func(i, j int) bool { return items[i].Port < items[j].Port })
}
