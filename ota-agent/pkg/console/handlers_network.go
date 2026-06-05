package console

import (
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
)

type networkInfo struct {
	Hostname   string      `json:"hostname"`
	IP         string      `json:"ip"`
	Gateway    string      `json:"gateway"`
	DNS        []string    `json:"dns"`
	Interfaces []ifaceInfo `json:"interfaces"`
}

type ifaceInfo struct {
	Name    string `json:"name"`
	State   string `json:"state"`
	IP      string `json:"ip"`
	MAC     string `json:"mac"`
	MTU     int    `json:"mtu"`
	Type    string `json:"type"`
}

func (s *Server) handleNetwork(w http.ResponseWriter, r *http.Request) {
	info := networkInfo{
		DNS: readDNS(),
	}
	info.Hostname, _ = os.Hostname()
	info.Gateway = readDefaultGateway()

	ifaces, _ := net.Interfaces()
	for _, iface := range ifaces {
		ii := ifaceInfo{
			Name: iface.Name,
			MAC:  iface.HardwareAddr.String(),
			MTU:  iface.MTU,
		}

		if iface.Flags&net.FlagUp != 0 {
			ii.State = "up"
		} else {
			ii.State = "down"
		}

		// 类型推断
		switch {
		case iface.Flags&net.FlagLoopback != 0:
			ii.Type = "本地"
		case strings.HasPrefix(iface.Name, "docker") || strings.HasPrefix(iface.Name, "br-") || strings.HasPrefix(iface.Name, "veth"):
			ii.Type = "虚拟桥"
		case strings.HasPrefix(iface.Name, "wl"):
			ii.Type = "无线"
		default:
			ii.Type = "以太网"
		}

		addrs, _ := iface.Addrs()
		var ips []string
		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok && ipnet.IP.To4() != nil {
				ips = append(ips, addr.String())
			}
		}
		if len(ips) > 0 {
			ii.IP = ips[0]
			if info.IP == "" && iface.Flags&net.FlagLoopback == 0 {
				info.IP = strings.Split(ips[0], "/")[0]
			}
		}

		info.Interfaces = append(info.Interfaces, ii)
	}

	s.jsonOK(w, info)
}

func readDNS() []string {
	data, err := os.ReadFile("/etc/resolv.conf")
	if err != nil {
		return nil
	}
	var dns []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "nameserver ") {
			dns = append(dns, strings.TrimPrefix(line, "nameserver "))
		}
	}
	return dns
}

func readDefaultGateway() string {
	out, err := exec.Command("ip", "route", "show", "default").Output()
	if err != nil {
		return ""
	}
	// "default via 192.168.1.1 dev eth0"
	fields := strings.Fields(string(out))
	for i, f := range fields {
		if f == "via" && i+1 < len(fields) {
			return fields[i+1]
		}
	}
	return ""
}
