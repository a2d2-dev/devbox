package network

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCollectorSnapshotAndRate(t *testing.T) {
	root := t.TempDir()
	iface := filepath.Join(root, "eth0")
	if err := os.MkdirAll(filepath.Join(iface, "statistics"), 0755); err != nil {
		t.Fatal(err)
	}
	write := func(name, value string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(iface, name), []byte(value), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("duplex", "full\n")
	write("speed", "1000\n")
	write("statistics/rx_bytes", "1000\n")
	write("statistics/tx_bytes", "2000\n")
	resolv := filepath.Join(root, "resolv.conf")
	if err := os.WriteFile(resolv, []byte("nameserver 1.1.1.1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(100, 0)
	c := NewCollector()
	c.SysClassNet = root
	c.ResolvConf = resolv
	c.Now = func() time.Time { return now }
	c.Run = func(_ string, args ...string) ([]byte, error) {
		joined := strings.Join(args, " ")
		if joined == "-j addr show" {
			return []byte(`[{"ifname":"eth0","operstate":"UP","address":"00:11:22:33:44:55","mtu":1500,"addr_info":[{"family":"inet","local":"10.0.0.2","prefixlen":24,"dynamic":true},{"family":"inet6","local":"fd00::2","prefixlen":64}]}]`), nil
		}
		if joined == "-j route show default" {
			return []byte(`[{"gateway":"10.0.0.1","dev":"eth0"}]`), nil
		}
		return nil, errors.New("unexpected command")
	}
	first, err := c.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if first.Gateway != "10.0.0.1" || first.Interfaces[0].Mode != "dhcp" || first.Interfaces[0].Duplex != "full" {
		t.Fatalf("unexpected snapshot: %#v", first)
	}
	write("statistics/rx_bytes", "3000\n")
	write("statistics/tx_bytes", "5000\n")
	now = now.Add(2 * time.Second)
	second, err := c.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if second.Interfaces[0].RxBytesSec != 1000 || second.Interfaces[0].TxBytesSec != 1500 {
		t.Fatalf("unexpected rates: %#v", second.Interfaces[0])
	}
	write("speed", "-1\n")
	third, err := c.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if third.Interfaces[0].LinkMbps != 0 {
		t.Fatalf("invalid sysfs speed should be reported as unknown: %#v", third.Interfaces[0])
	}
}

func TestRenderFirewallLockoutProtection(t *testing.T) {
	session := "10.126.126.2"
	tunnelProtection := FirewallRule{Direction: "in", Action: "allow", Protocol: "any", Source: "any", Interface: "tun0"}
	sessionProtection := FirewallRule{Direction: "in", Action: "allow", Protocol: "any", Source: session}
	interfaces := []string{"tun0", "eth0"}
	if _, err := RenderFirewall([]FirewallRule{tunnelProtection}, session, interfaces); err == nil || !strings.Contains(err.Error(), "session IP") {
		t.Fatalf("expected session protection rejection, got %v", err)
	}
	if _, err := RenderFirewall([]FirewallRule{sessionProtection}, session, interfaces); err == nil || !strings.Contains(err.Error(), "tun0") {
		t.Fatalf("expected tun0 protection rejection, got %v", err)
	}
	tooNarrow := []FirewallRule{
		{Direction: "in", Action: "allow", Protocol: "tcp", Port: 22, Source: "any", Interface: "tun0"},
		sessionProtection,
	}
	if _, err := RenderFirewall(tooNarrow, session, interfaces); err == nil || !strings.Contains(err.Error(), "tun0") {
		t.Fatalf("expected narrow tun0 rule rejection, got %v", err)
	}
	protected := []FirewallRule{tunnelProtection, sessionProtection}
	preview, err := RenderFirewall(protected, session, interfaces)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(preview.Ruleset, `iifname "tun0" accept`) || !strings.Contains(preview.Ruleset, "ip saddr 10.126.126.2 accept") {
		t.Fatalf("missing safety rules:\n%s", preview.Ruleset)
	}
	if _, err := RenderFirewall(protected, "not-an-ip", interfaces); err == nil {
		t.Fatal("expected invalid session IP rejection")
	}
}

func TestRenderFirewallRejectsPortWithoutTransportProtocol(t *testing.T) {
	rules := []FirewallRule{
		{Direction: "in", Action: "allow", Protocol: "any", Source: "any", Interface: "tun0"},
		{Direction: "in", Action: "allow", Protocol: "any", Source: "10.126.126.2"},
		{Direction: "in", Action: "allow", Protocol: "any", Port: 443, Source: "any"},
	}
	if _, err := RenderFirewall(rules, "10.126.126.2", []string{"tun0", "eth0"}); err == nil || !strings.Contains(err.Error(), "tcp or udp") {
		t.Fatalf("expected protocol/port validation error, got %v", err)
	}
}

func TestRenderFirewallRejectsProtectionRulesShadowedByDeny(t *testing.T) {
	rules := []FirewallRule{
		{Direction: "in", Action: "deny", Protocol: "any", Source: "any"},
		{Direction: "in", Action: "allow", Protocol: "any", Source: "any", Interface: "tun0"},
		{Direction: "in", Action: "allow", Protocol: "any", Source: "10.126.126.2"},
	}
	if _, err := RenderFirewall(rules, "10.126.126.2", []string{"tun0", "eth0"}); err == nil || !strings.Contains(err.Error(), "shadow") {
		t.Fatalf("expected shadowed protection rejection, got %v", err)
	}
}

func TestRenderFirewallRejectsNarrowedTunnelProtection(t *testing.T) {
	rules := []FirewallRule{
		{Direction: "in", Action: "allow", Protocol: "any", Source: "10.126.126.0/24", Interface: "tun0"},
		{Direction: "in", Action: "allow", Protocol: "any", Source: "10.126.126.2"},
	}
	if _, err := RenderFirewall(rules, "10.126.126.2", []string{"tun0", "eth0"}); err == nil || !strings.Contains(err.Error(), "tun0") {
		t.Fatalf("expected narrowed tun0 protection rejection, got %v", err)
	}
}

func TestRenderFirewallRejectsSessionProtectionOnUnknownInterface(t *testing.T) {
	rules := []FirewallRule{
		{Direction: "in", Action: "allow", Protocol: "any", Source: "any", Interface: "tun0"},
		{Direction: "in", Action: "allow", Protocol: "any", Source: "10.126.126.2", Interface: "fake0"},
	}
	if _, err := RenderFirewall(rules, "10.126.126.2", []string{"tun0", "eth0"}); err == nil || !strings.Contains(err.Error(), "interface") {
		t.Fatalf("expected unknown session interface rejection, got %v", err)
	}
}

func TestDDNSCredentialMustBeAReferenceAndPreviewIsRedacted(t *testing.T) {
	for _, value := range []string{"plain-secret-token", "0123456789abcdef0123456789abcdef", "hunter2"} {
		cfg := DDNSConfig{Provider: "cloudflare", Domain: "example.com", CredentialRef: value}
		if err := ValidateDDNS(cfg); err == nil {
			t.Fatalf("accepted bare credential %q", value)
		}
	}
	for _, value := range []string{"env:CLOUDFLARE_TOKEN", "file:/run/secrets/cloudflare-token"} {
		cfg := DDNSConfig{Provider: "cloudflare", Domain: "example.com", CredentialRef: value}
		preview, err := PreviewDDNS(cfg)
		if err != nil {
			t.Fatalf("valid reference %q rejected: %v", value, err)
		}
		if strings.Contains(preview, value) || !strings.Contains(preview, "redacted") {
			t.Fatalf("credential reference leaked in preview: %q", preview)
		}
	}
}

func TestClassifyVirtualInterfaces(t *testing.T) {
	for _, name := range []string{"vnet0", "cali123", "flannel.1"} {
		if got := classify(name); got != "virtual" {
			t.Fatalf("classify(%q)=%q, want virtual", name, got)
		}
	}
}

func TestPreviewSSHRejectsPortConflict(t *testing.T) {
	current := SSHStatus{Port: 22, PermitRootLogin: "no", PasswordAuthentication: "no"}
	change := SSHChange{Port: 9092, PermitRootLogin: "prohibit-password", PasswordAuthentication: "no"}
	_, err := PreviewSSH(current, change, []Listener{{Port: 9092, Process: "devbox"}})
	if err == nil || !strings.Contains(err.Error(), "already in use") {
		t.Fatalf("expected conflict, got %v", err)
	}
	change.Port = 2222
	diff, err := PreviewSSH(current, change, nil)
	if err != nil || !strings.Contains(diff, "+Port 2222") {
		t.Fatalf("unexpected diff/error: %q %v", diff, err)
	}
	change.PermitRootLogin = "without-password"
	if _, err := PreviewSSH(current, change, nil); err != nil {
		t.Fatalf("OpenSSH compatibility value rejected: %v", err)
	}
}

func TestReadPathsParseSystemCommandOutput(t *testing.T) {
	c := NewCollector()
	c.Run = func(name string, args ...string) ([]byte, error) {
		command := strings.TrimSpace(name + " " + strings.Join(args, " "))
		switch command {
		case "systemctl is-active sshd":
			return []byte("active\n"), nil
		case "sshd -T":
			return []byte("port 2222\npermitrootlogin prohibit-password\npasswordauthentication no\npubkeyauthentication yes\n"), nil
		case "ss -H -lntp":
			return []byte("LISTEN 0 4096 0.0.0.0:9133 0.0.0.0:* users:((\"devbox\",pid=1,fd=3))\n"), nil
		case "ip -j addr show":
			return []byte(`[{"ifname":"tun0","operstate":"UP","address":"","mtu":1400,"addr_info":[{"family":"inet","local":"10.126.126.12","prefixlen":24}]}]`), nil
		case "ip -j route show default":
			return []byte(`[]`), nil
		case "nft list ruleset":
			return nil, errors.New("nft unavailable")
		case "iptables-save":
			return []byte("*filter\nCOMMIT\n"), nil
		default:
			return nil, errors.New("unexpected command: " + command)
		}
	}

	ssh := c.SSHStatus()
	if !ssh.Running || ssh.Port != 2222 || ssh.PermitRootLogin != "prohibit-password" || ssh.PasswordAuthentication != "no" || ssh.PubkeyAuthentication != "yes" {
		t.Fatalf("unexpected SSH status: %#v", ssh)
	}
	remote := c.RemoteAccess()
	if len(remote.Listeners) != 1 || remote.Listeners[0].Port != 9133 || len(remote.TunnelIPs) != 1 || remote.TunnelIPs[0] != "10.126.126.12/24" {
		t.Fatalf("unexpected remote access status: %#v", remote)
	}
	backend, ruleset, err := c.FirewallRules()
	if err != nil || backend != "iptables" || ruleset != "*filter\nCOMMIT\n" {
		t.Fatalf("unexpected firewall fallback: backend=%q rules=%q err=%v", backend, ruleset, err)
	}
}
