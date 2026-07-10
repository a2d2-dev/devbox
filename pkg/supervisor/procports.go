package supervisor

import (
	"bufio"
	"encoding/hex"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// listenInode -> externally-reachable listening port.
//
// Some services keep their port in their own config file (caddy's Caddyfile,
// shadow-cljs.edn, ...) rather than in the supervisor conf, so the conf-based
// extraction misses it. As a fallback we read the kernel's socket tables and
// map a process's listening sockets to the port it actually serves on.

const tcpStateListen = "0A"

// buildListenMap scans /proc/net/tcp{,6} and returns a map of socket inode ->
// externally-reachable listening port. Loopback-only sockets (127.0.0.1, ::1)
// are skipped because they are not reachable for jump-to-service.
func buildListenMap() map[string]int {
	res := make(map[string]int)
	for _, path := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		parseNetTCP(path, res)
	}
	return res
}

func parseNetTCP(path string, res map[string]int) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	first := true
	for scanner.Scan() {
		if first { // skip header row
			first = false
			continue
		}
		fields := strings.Fields(scanner.Text())
		if len(fields) < 10 || fields[3] != tcpStateListen {
			continue
		}
		ip, port := parseHexAddr(fields[1])
		if ip == nil || port == 0 {
			continue
		}
		if ip.IsLoopback() { // not reachable from outside, no jump value
			continue
		}
		res[fields[9]] = port // fields[9] is the socket inode
	}
}

// parseHexAddr parses a /proc/net/tcp local_address field like "0100007F:1F90"
// (IPv4) or a 32-hex-char IPv6 form into a net.IP and port. Addresses are
// stored little-endian per 4-byte word.
func parseHexAddr(s string) (net.IP, int) {
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 {
		return nil, 0
	}
	ipBytes, err := hex.DecodeString(parts[0])
	if err != nil || (len(ipBytes) != 4 && len(ipBytes) != 16) {
		return nil, 0
	}
	for i := 0; i < len(ipBytes); i += 4 {
		ipBytes[i], ipBytes[i+3] = ipBytes[i+3], ipBytes[i]
		ipBytes[i+1], ipBytes[i+2] = ipBytes[i+2], ipBytes[i+1]
	}
	port, err := strconv.ParseInt(parts[1], 16, 32)
	if err != nil {
		return nil, 0
	}
	return net.IP(ipBytes), int(port)
}

// socketInodes returns the socket inodes owned by the given PID, read from
// /proc/<pid>/fd symlinks ("socket:[12345]").
func socketInodes(pid int) []string {
	if pid <= 0 {
		return nil
	}
	dir := filepath.Join("/proc", strconv.Itoa(pid), "fd")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var inodes []string
	for _, e := range entries {
		link, err := os.Readlink(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		if strings.HasPrefix(link, "socket:[") {
			inodes = append(inodes, strings.TrimSuffix(strings.TrimPrefix(link, "socket:["), "]"))
		}
	}
	return inodes
}

// portFromListen returns the lowest externally-reachable port the given socket
// inodes are listening on, or "" if none. Lowest wins so the result is stable
// when a process listens on several ports (e.g. caddy's 5200 vs admin 2019,
// the latter already filtered out as loopback).
func portFromListen(inodes []string, listenMap map[string]int) string {
	best := 0
	for _, ino := range inodes {
		if p, ok := listenMap[ino]; ok && (best == 0 || p < best) {
			best = p
		}
	}
	if best == 0 {
		return ""
	}
	return strconv.Itoa(best)
}
