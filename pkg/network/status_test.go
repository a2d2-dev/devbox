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
}

func TestRenderFirewallLockoutProtection(t *testing.T) {
	session := "10.126.126.2"
	base := []FirewallRule{{Direction: "in", Action: "allow", Protocol: "any", Interface: "tun0"}}
	if _, err := RenderFirewall(base, session); err == nil || !strings.Contains(err.Error(), "session IP") {
		t.Fatalf("expected session protection rejection, got %v", err)
	}
	withSession := append(base, FirewallRule{Direction: "in", Action: "allow", Protocol: "tcp", Port: 9092, Source: session})
	preview, err := RenderFirewall(withSession, session)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(preview.Ruleset, `iifname "tun0" accept`) || !strings.Contains(preview.Ruleset, "ip saddr 10.126.126.2 tcp dport 9092 accept") {
		t.Fatalf("missing safety rules:\n%s", preview.Ruleset)
	}
	if _, err := RenderFirewall(withSession, "not-an-ip"); err == nil {
		t.Fatal("expected invalid session IP rejection")
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
}
