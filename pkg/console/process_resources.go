package console

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/a2d2-dev/devbox/pkg/system"
)

const linuxClockTicks = 100.0

type processResourceInfo struct {
	system.ProcessBasic
	StartTicks     uint64   `json:"startTicks"`
	CPUPercent     *float64 `json:"cpuPercent"`
	CPUTimeSeconds float64  `json:"cpuTimeSeconds"`
	RuntimeSeconds int64    `json:"runtimeSeconds"`
	ReadBps        *float64 `json:"readBps"`
	WriteBps       *float64 `json:"writeBps"`
	IOStatus       string   `json:"ioStatus"`
	Ports          []int    `json:"ports"`
	PortsStatus    string   `json:"portsStatus"`
}

type processCounter struct {
	name       string
	startTicks uint64
	cpuTicks   uint64
	readBytes  uint64
	writeBytes uint64
	ioOK       bool
	at         time.Time
}

type processIdentity struct {
	ppid       int
	flags      uint64
	startTicks uint64
}

type processResourceSampler struct {
	mu        sync.Mutex
	procRoot  string
	prevTotal uint64
	previous  map[int]processCounter
}

func newProcessResourceSampler() *processResourceSampler {
	return &processResourceSampler{procRoot: "/proc", previous: make(map[int]processCounter)}
}

func (s *processResourceSampler) sample(ctx context.Context, basics []system.ProcessBasic) []processResourceInfo {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	total, _ := readGlobalCPUTicks(s.procRoot)
	ports, portsOK := processListeningPorts(ctx)
	out := make([]processResourceInfo, 0, len(basics))
	next := make(map[int]processCounter, len(basics))
	for _, basic := range basics {
		cpuTicks, startTicks, err := readProcessStat(s.procRoot, basic.PID)
		if err != nil {
			continue
		}
		readBytes, writeBytes, ioOK := readProcessIO(s.procRoot, basic.PID)
		current := processCounter{
			name: basic.Name, startTicks: startTicks, cpuTicks: cpuTicks, readBytes: readBytes,
			writeBytes: writeBytes, ioOK: ioOK, at: now,
		}
		info := processResourceInfo{
			ProcessBasic: basic, StartTicks: startTicks, CPUTimeSeconds: float64(cpuTicks) / linuxClockTicks,
			IOStatus: "unavailable", Ports: ports[basic.PID], PortsStatus: "available",
		}
		if basic.StartTime != "" {
			if started, err := time.Parse(time.RFC3339, basic.StartTime); err == nil {
				info.RuntimeSeconds = maxInt64(0, int64(now.Sub(started).Seconds()))
			}
		}
		if !portsOK {
			info.PortsStatus = "unavailable"
		}
		if ioOK {
			info.IOStatus = "available"
		}
		if prev, ok := s.previous[basic.PID]; ok && prev.name == basic.Name && prev.startTicks == startTicks {
			if total > s.prevTotal && cpuTicks >= prev.cpuTicks {
				value := float64(cpuTicks-prev.cpuTicks) / float64(total-s.prevTotal) * float64(runtime.NumCPU()) * 100
				info.CPUPercent = &value
			}
			dt := now.Sub(prev.at).Seconds()
			if ioOK && prev.ioOK && dt > 0 && readBytes >= prev.readBytes && writeBytes >= prev.writeBytes {
				readRate := float64(readBytes-prev.readBytes) / dt
				writeRate := float64(writeBytes-prev.writeBytes) / dt
				info.ReadBps, info.WriteBps = &readRate, &writeRate
			}
		}
		next[basic.PID] = current
		out = append(out, info)
	}
	s.prevTotal, s.previous = total, next
	return out
}

func readGlobalCPUTicks(procRoot string) (uint64, error) {
	data, err := os.ReadFile(procRoot + "/stat")
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != "cpu" {
			continue
		}
		var total uint64
		for _, field := range fields[1:] {
			value, err := strconv.ParseUint(field, 10, 64)
			if err != nil {
				return 0, err
			}
			total += value
		}
		return total, nil
	}
	return 0, fmt.Errorf("aggregate cpu line not found")
}

func readProcessStat(procRoot string, pid int) (cpuTicks, startTicks uint64, err error) {
	cpuTicks, identity, err := readProcessIdentity(procRoot, pid)
	return cpuTicks, identity.startTicks, err
}

func readProcessIdentity(procRoot string, pid int) (cpuTicks uint64, identity processIdentity, err error) {
	data, err := os.ReadFile(fmt.Sprintf("%s/%d/stat", procRoot, pid))
	if err != nil {
		return 0, processIdentity{}, err
	}
	text := string(data)
	closeIdx := strings.LastIndexByte(text, ')')
	if closeIdx < 0 || closeIdx+2 >= len(text) {
		return 0, processIdentity{}, fmt.Errorf("malformed process stat")
	}
	fields := strings.Fields(text[closeIdx+2:])
	if len(fields) < 20 {
		return 0, processIdentity{}, fmt.Errorf("short process stat")
	}
	identity.ppid, err = strconv.Atoi(fields[1])
	if err != nil {
		return 0, processIdentity{}, err
	}
	identity.flags, err = strconv.ParseUint(fields[6], 10, 64)
	if err != nil {
		return 0, processIdentity{}, err
	}
	userTicks, err := strconv.ParseUint(fields[11], 10, 64)
	if err != nil {
		return 0, processIdentity{}, err
	}
	systemTicks, err := strconv.ParseUint(fields[12], 10, 64)
	if err != nil {
		return 0, processIdentity{}, err
	}
	identity.startTicks, err = strconv.ParseUint(fields[19], 10, 64)
	return userTicks + systemTicks, identity, err
}

func readProcessIO(procRoot string, pid int) (readBytes, writeBytes uint64, ok bool) {
	data, err := os.ReadFile(fmt.Sprintf("%s/%d/io", procRoot, pid))
	if err != nil {
		return 0, 0, false
	}
	readFound, writeFound := false, false
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		switch fields[0] {
		case "read_bytes:":
			readBytes, err = strconv.ParseUint(fields[1], 10, 64)
			readFound = err == nil
		case "write_bytes:":
			writeBytes, err = strconv.ParseUint(fields[1], 10, 64)
			writeFound = err == nil
		}
		if err != nil {
			return 0, 0, false
		}
	}
	return readBytes, writeBytes, readFound && writeFound
}

func processListeningPorts(ctx context.Context) (map[int][]int, bool) {
	connections, err := system.ListNetworkConnections(ctx, 5000)
	if err != nil {
		return map[int][]int{}, false
	}
	sets := make(map[int]map[int]bool)
	for _, conn := range connections {
		if conn.PID <= 0 || !strings.EqualFold(conn.State, "LISTEN") {
			continue
		}
		port, ok := parseAddressPort(conn.Local)
		if !ok {
			continue
		}
		if sets[conn.PID] == nil {
			sets[conn.PID] = make(map[int]bool)
		}
		sets[conn.PID][port] = true
	}
	out := make(map[int][]int, len(sets))
	for pid, set := range sets {
		for port := range set {
			out[pid] = append(out[pid], port)
		}
		sort.Ints(out[pid])
	}
	return out, true
}

func parseAddressPort(address string) (int, bool) {
	idx := strings.LastIndexByte(address, ':')
	if idx < 0 || idx == len(address)-1 {
		return 0, false
	}
	port, err := strconv.Atoi(address[idx+1:])
	return port, err == nil && port > 0 && port <= 65535
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
