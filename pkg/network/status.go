package network

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Runner func(name string, args ...string) ([]byte, error)

type Collector struct {
	SysClassNet string
	ResolvConf  string
	Run         Runner
	Now         func() time.Time
	mu          sync.Mutex
	previous    map[string]sample
}

type sample struct {
	rx, tx uint64
	at     time.Time
}

type Status struct {
	Hostname   string      `json:"hostname"`
	PrimaryIP  string      `json:"ip"`
	Gateway    string      `json:"gateway"`
	DNS        []string    `json:"dns"`
	Interfaces []Interface `json:"interfaces"`
}

type Interface struct {
	Name       string   `json:"name"`
	State      string   `json:"state"`
	Type       string   `json:"type"`
	MAC        string   `json:"mac"`
	MTU        int      `json:"mtu"`
	IPv4       []string `json:"ipv4"`
	IPv6       []string `json:"ipv6"`
	Gateway    string   `json:"gateway,omitempty"`
	DNS        []string `json:"dns,omitempty"`
	Mode       string   `json:"mode"`
	Duplex     string   `json:"duplex,omitempty"`
	LinkMbps   int      `json:"linkMbps,omitempty"`
	RxBytes    uint64   `json:"rxBytes"`
	TxBytes    uint64   `json:"txBytes"`
	RxBytesSec uint64   `json:"rxBytesSec"`
	TxBytesSec uint64   `json:"txBytesSec"`
}

type ipAddress struct {
	IfName    string `json:"ifname"`
	OperState string `json:"operstate"`
	Address   string `json:"address"`
	MTU       int    `json:"mtu"`
	AddrInfo  []struct {
		Family       string `json:"family"`
		Local        string `json:"local"`
		PrefixLen    int    `json:"prefixlen"`
		Dynamic      bool   `json:"dynamic"`
		ValidLifeSec any    `json:"valid_life_time"`
	} `json:"addr_info"`
}

type ipRoute struct {
	Gateway string `json:"gateway"`
	Dev     string `json:"dev"`
}

func NewCollector() *Collector {
	return &Collector{
		SysClassNet: "/sys/class/net",
		ResolvConf:  "/etc/resolv.conf",
		Run: func(name string, args ...string) ([]byte, error) {
			return exec.Command(name, args...).Output()
		},
		Now:      time.Now,
		previous: make(map[string]sample),
	}
}

func (c *Collector) Snapshot() (Status, error) {
	var result Status
	result.Hostname, _ = os.Hostname()
	result.DNS = readDNS(c.ResolvConf)

	out, err := c.Run("ip", "-j", "addr", "show")
	if err != nil {
		return result, fmt.Errorf("read interfaces: %w", err)
	}
	var addresses []ipAddress
	if err := json.Unmarshal(out, &addresses); err != nil {
		return result, fmt.Errorf("parse ip address output: %w", err)
	}

	routes := c.routes()
	for _, addr := range addresses {
		item := Interface{
			Name: addr.IfName, State: strings.ToLower(addr.OperState), Type: classify(addr.IfName),
			MAC: addr.Address, MTU: addr.MTU, Mode: "static", DNS: result.DNS,
		}
		for _, a := range addr.AddrInfo {
			cidr := fmt.Sprintf("%s/%d", a.Local, a.PrefixLen)
			switch a.Family {
			case "inet":
				item.IPv4 = append(item.IPv4, cidr)
				if result.PrimaryIP == "" && item.Type != "loopback" && item.Type != "virtual" {
					result.PrimaryIP = a.Local
				}
			case "inet6":
				item.IPv6 = append(item.IPv6, cidr)
			}
			if a.Dynamic {
				item.Mode = "dhcp"
			}
		}
		if r, ok := routes[item.Name]; ok {
			item.Gateway = r
			if result.Gateway == "" {
				result.Gateway = r
			}
		}
		item.Duplex = strings.ToLower(readText(filepath.Join(c.SysClassNet, item.Name, "duplex")))
		item.LinkMbps, _ = strconv.Atoi(readText(filepath.Join(c.SysClassNet, item.Name, "speed")))
		item.RxBytes, _ = strconv.ParseUint(readText(filepath.Join(c.SysClassNet, item.Name, "statistics/rx_bytes")), 10, 64)
		item.TxBytes, _ = strconv.ParseUint(readText(filepath.Join(c.SysClassNet, item.Name, "statistics/tx_bytes")), 10, 64)
		item.RxBytesSec, item.TxBytesSec = c.rate(item.Name, item.RxBytes, item.TxBytes)
		result.Interfaces = append(result.Interfaces, item)
	}
	return result, nil
}

func (c *Collector) routes() map[string]string {
	result := make(map[string]string)
	out, err := c.Run("ip", "-j", "route", "show", "default")
	if err != nil {
		return result
	}
	var routes []ipRoute
	if json.Unmarshal(out, &routes) != nil {
		return result
	}
	for _, r := range routes {
		if r.Dev != "" && r.Gateway != "" {
			result[r.Dev] = r.Gateway
		}
	}
	return result
}

func (c *Collector) rate(name string, rx, tx uint64) (uint64, uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.Now()
	old, ok := c.previous[name]
	c.previous[name] = sample{rx: rx, tx: tx, at: now}
	if !ok || !now.After(old.at) || rx < old.rx || tx < old.tx {
		return 0, 0
	}
	seconds := now.Sub(old.at).Seconds()
	return uint64(float64(rx-old.rx) / seconds), uint64(float64(tx-old.tx) / seconds)
}

func readDNS(path string) []string {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var result []string
	for _, line := range strings.Split(string(b), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "nameserver" && net.ParseIP(fields[1]) != nil {
			result = append(result, fields[1])
		}
	}
	return result
}

func readText(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func classify(name string) string {
	switch {
	case name == "lo":
		return "loopback"
	case name == "tun0" || strings.HasPrefix(name, "tun") || strings.HasPrefix(name, "tap") || strings.HasPrefix(name, "wg"):
		return "tunnel"
	case strings.HasPrefix(name, "wl"):
		return "wireless"
	case strings.HasPrefix(name, "docker") || strings.HasPrefix(name, "br-") || strings.HasPrefix(name, "veth") || strings.HasPrefix(name, "virbr") || strings.HasPrefix(name, "cni"):
		return "virtual"
	default:
		return "ethernet"
	}
}

func ValidateIPOrCIDR(value string) error {
	if net.ParseIP(value) != nil {
		return nil
	}
	if _, _, err := net.ParseCIDR(value); err == nil {
		return nil
	}
	return errors.New("must be a valid IP address or CIDR")
}

func ValidatePort(port int) error {
	if port < 1 || port > 65535 {
		return errors.New("port must be between 1 and 65535")
	}
	return nil
}
