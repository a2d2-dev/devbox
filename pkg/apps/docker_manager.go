package apps

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	defaultDockerDataRoot = "/var/lib/docker"
	defaultDaemonJSON     = "/etc/docker/daemon.json"
)

// DockerController 是 Controller 的可选 Docker 主机能力。它和 Compose 管理共用
// 同一个 dockerEngine、领域错误和鉴权边界，但不扩大 Issue #2 的稳定 Controller 接口。
type DockerController interface {
	DockerOverview(context.Context) (DockerOverview, error)
	DockerStats(context.Context) (DockerStats, error)
	DockerServiceAction(context.Context, DockerServiceActionRequest) (DockerOverview, error)
	SetDockerAutostart(context.Context, DockerAutostartRequest) (DockerOverview, error)
	PlanDockerMigration(context.Context, DockerMigrationRequest) (DockerMigrationPlan, error)
	ExecuteDockerMigration(context.Context, DockerMigrationExecuteRequest) (DockerMigrationResult, error)
}

type DockerServiceState string

const (
	DockerServiceRunning      DockerServiceState = "running"
	DockerServiceStopped      DockerServiceState = "stopped"
	DockerServiceNotInstalled DockerServiceState = "not_installed"
)

type DockerServiceSummary struct {
	State              DockerServiceState `json:"state"`
	Installed          bool               `json:"installed"`
	ControlSupported   bool               `json:"controlSupported"`
	AutostartSupported bool               `json:"autostartSupported"`
	AutostartEnabled   *bool              `json:"autostartEnabled"`
	Manager            string             `json:"manager,omitempty"`
	Diagnostic         string             `json:"diagnostic,omitempty"`
}

type DockerCountSummary struct {
	Running int `json:"running"`
	Total   int `json:"total"`
}

type DockerDiskSummary struct {
	TotalBytes     uint64 `json:"totalBytes"`
	AvailableBytes uint64 `json:"availableBytes"`
}

type DockerStorageSummary struct {
	Path       string            `json:"path"`
	Source     string            `json:"source"`
	Configured bool              `json:"configured"`
	Valid      bool              `json:"valid"`
	Error      string            `json:"error,omitempty"`
	Disk       DockerDiskSummary `json:"disk"`
}

type DockerOverview struct {
	Service         DockerServiceSummary `json:"service"`
	Version         string               `json:"version,omitempty"`
	ComposeProjects DockerCountSummary   `json:"composeProjects"`
	Containers      DockerCountSummary   `json:"containers"`
	Storage         DockerStorageSummary `json:"storage"`
	IdleSummary     string               `json:"idleSummary"`
	CheckedAt       time.Time            `json:"checkedAt"`
}

type DockerStats struct {
	Available        bool      `json:"available"`
	CPUPercent       float64   `json:"cpuPercent"`
	MemoryUsageBytes uint64    `json:"memoryUsageBytes"`
	MemoryLimitBytes uint64    `json:"memoryLimitBytes"`
	NetworkRxBytes   uint64    `json:"networkRxBytes"`
	NetworkTxBytes   uint64    `json:"networkTxBytes"`
	Containers       int       `json:"containers"`
	FailedContainers int       `json:"failedContainers"`
	Diagnostic       string    `json:"diagnostic,omitempty"`
	SampledAt        time.Time `json:"sampledAt"`
}

type DockerServiceActionRequest struct {
	Action string `json:"action"`
}

type DockerAutostartRequest struct {
	Enabled bool `json:"enabled"`
}

type DockerMigrationRequest struct {
	TargetPath string `json:"targetPath"`
}

type DockerMigrationStep struct {
	Order       int    `json:"order"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

type DockerMigrationPlan struct {
	ID                 string                `json:"id"`
	SourcePath         string                `json:"sourcePath"`
	TargetPath         string                `json:"targetPath"`
	DaemonConfigPath   string                `json:"daemonConfigPath"`
	RequiredBytes      uint64                `json:"requiredBytes"`
	AvailableBytes     uint64                `json:"availableBytes"`
	ProposedDaemonJSON string                `json:"proposedDaemonJson"`
	Steps              []DockerMigrationStep `json:"steps"`
	Warnings           []string              `json:"warnings"`
}

type DockerMigrationExecuteRequest struct {
	TargetPath string `json:"targetPath"`
	PlanID     string `json:"planId"`
	Confirm    bool   `json:"confirm"`
}

type DockerMigrationResult struct {
	Plan      DockerMigrationPlan `json:"plan"`
	Completed bool                `json:"completed"`
	Message   string              `json:"message"`
}

type dockerCommandRunner interface {
	LookPath(string) (string, error)
	Run(context.Context, string, ...string) (string, error)
}

type realDockerCommandRunner struct{}

func (realDockerCommandRunner) LookPath(name string) (string, error) { return exec.LookPath(name) }
func (realDockerCommandRunner) Run(ctx context.Context, name string, args ...string) (string, error) {
	b, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	return strings.TrimSpace(string(b)), err
}

type dockerServiceHost interface {
	Status(context.Context) DockerServiceSummary
	Control(context.Context, string) error
	SetAutostart(context.Context, bool) error
	Diagnostic(context.Context) string
}

type systemDockerServiceHost struct{ runner dockerCommandRunner }

func (h *systemDockerServiceHost) Status(ctx context.Context) DockerServiceSummary {
	if _, err := h.runner.LookPath("systemctl"); err == nil {
		load, loadErr := h.runner.Run(ctx, "systemctl", "show", "docker.service", "--property=LoadState", "--value")
		if loadErr == nil && strings.TrimSpace(load) != "not-found" && strings.TrimSpace(load) != "" {
			s := DockerServiceSummary{State: DockerServiceStopped, Installed: true, ControlSupported: true, AutostartSupported: true, Manager: "systemd"}
			active, _ := h.runner.Run(ctx, "systemctl", "is-active", "docker.service")
			if strings.TrimSpace(active) == "active" {
				s.State = DockerServiceRunning
			}
			enabled, _ := h.runner.Run(ctx, "systemctl", "is-enabled", "docker.service")
			v := strings.TrimSpace(enabled) == "enabled"
			s.AutostartEnabled = &v
			return s
		}
	}
	if _, err := h.runner.LookPath("service"); err == nil {
		out, err := h.runner.Run(ctx, "service", "docker", "status")
		lower := strings.ToLower(out)
		if !strings.Contains(lower, "unrecognized service") && !strings.Contains(lower, "not found") {
			s := DockerServiceSummary{State: DockerServiceStopped, Installed: true, ControlSupported: true, Manager: "service"}
			if err == nil {
				s.State = DockerServiceRunning
			}
			return s
		}
	}
	if _, err := h.runner.LookPath("dockerd"); err == nil {
		return DockerServiceSummary{State: DockerServiceStopped, Installed: true, Diagnostic: "已找到 dockerd，但当前环境不支持 systemd/service 控制"}
	}
	return DockerServiceSummary{State: DockerServiceNotInstalled, Diagnostic: "未找到 Docker 服务单元或 dockerd"}
}

func (h *systemDockerServiceHost) Control(ctx context.Context, action string) error {
	s := h.Status(ctx)
	if !s.ControlSupported {
		return CapabilityDetailErr("service_control_unsupported", "当前环境不支持 Docker 服务控制", s.Diagnostic, nil)
	}
	var name string
	var args []string
	if s.Manager == "systemd" {
		name, args = "systemctl", []string{action, "docker.service"}
	} else {
		name, args = "service", []string{"docker", action}
	}
	out, err := h.runner.Run(ctx, name, args...)
	if err == nil {
		return nil
	}
	reason := "service_control_failed"
	lower := strings.ToLower(out + " " + err.Error())
	if strings.Contains(lower, "permission denied") || strings.Contains(lower, "authentication is required") || strings.Contains(lower, "access denied") {
		reason = "permission_denied"
	}
	return CapabilityDetailErr(reason, "Docker 服务操作失败", limitDiagnostic(out), err)
}

func (h *systemDockerServiceHost) SetAutostart(ctx context.Context, enabled bool) error {
	s := h.Status(ctx)
	if !s.AutostartSupported || s.Manager != "systemd" {
		return CapabilityDetailErr("autostart_unsupported", "当前环境不支持设置 Docker 开机自启", s.Diagnostic, nil)
	}
	action := "disable"
	if enabled {
		action = "enable"
	}
	out, err := h.runner.Run(ctx, "systemctl", action, "docker.service")
	if err == nil {
		return nil
	}
	reason := "autostart_failed"
	lower := strings.ToLower(out + " " + err.Error())
	if strings.Contains(lower, "permission denied") || strings.Contains(lower, "authentication is required") || strings.Contains(lower, "access denied") {
		reason = "permission_denied"
	}
	return CapabilityDetailErr(reason, "Docker 开机自启设置失败", limitDiagnostic(out), err)
}

func (h *systemDockerServiceHost) Diagnostic(ctx context.Context) string {
	if _, err := h.runner.LookPath("journalctl"); err != nil {
		return "journalctl 不可用"
	}
	out, err := h.runner.Run(ctx, "journalctl", "-u", "docker.service", "--no-pager", "-n", "30")
	if err != nil && out == "" {
		return "无法读取 docker.service 日志: " + err.Error()
	}
	return limitDiagnostic(out)
}

func limitDiagnostic(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 4000 {
		return s[len(s)-4000:]
	}
	return s
}

type dockerStorage interface {
	ReadDaemonJSON() ([]byte, error)
	WriteDaemonJSON([]byte, string) (func() error, error)
	Disk(string) (DockerDiskSummary, error)
	DirSize(string) (uint64, error)
	EnsureTargetReady(string, bool) error
}

type osDockerStorage struct{ daemonPath string }

func (s *osDockerStorage) ReadDaemonJSON() ([]byte, error) {
	b, err := os.ReadFile(s.daemonPath)
	if errors.Is(err, fs.ErrNotExist) {
		return []byte("{}\n"), nil
	}
	return b, err
}

func (s *osDockerStorage) WriteDaemonJSON(data []byte, planID string) (func() error, error) {
	dir := filepath.Dir(s.daemonPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	original, readErr := os.ReadFile(s.daemonPath)
	originalMode := fs.FileMode(0o644)
	if info, err := os.Stat(s.daemonPath); err == nil {
		originalMode = info.Mode().Perm()
	}
	tmp, err := os.CreateTemp(dir, ".daemon.json.devbox-*")
	if err != nil {
		return nil, err
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if _, err = tmp.Write(data); err == nil {
		err = tmp.Chmod(originalMode)
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		cleanup()
		return nil, err
	}
	if err = os.Rename(tmpName, s.daemonPath); err != nil {
		cleanup()
		return nil, err
	}
	rollback := func() error {
		if errors.Is(readErr, fs.ErrNotExist) {
			return os.Remove(s.daemonPath)
		}
		if readErr != nil {
			return readErr
		}
		return os.WriteFile(s.daemonPath, original, originalMode)
	}
	_ = planID // planID is kept in the signature for alternative audited storage implementations.
	return rollback, nil
}

func (s *osDockerStorage) Disk(path string) (DockerDiskSummary, error) {
	p := filepath.Clean(path)
	for {
		var st syscall.Statfs_t
		if err := syscall.Statfs(p, &st); err == nil {
			return DockerDiskSummary{TotalBytes: st.Blocks * uint64(st.Bsize), AvailableBytes: st.Bavail * uint64(st.Bsize)}, nil
		} else if !errors.Is(err, fs.ErrNotExist) {
			return DockerDiskSummary{}, err
		}
		parent := filepath.Dir(p)
		if parent == p {
			return DockerDiskSummary{}, fmt.Errorf("找不到可用的父目录")
		}
		p = parent
	}
}

func (s *osDockerStorage) DirSize(root string) (uint64, error) {
	var total uint64
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			info, err := d.Info()
			if err != nil {
				return err
			}
			total += uint64(info.Size())
		}
		return nil
	})
	return total, err
}

func (s *osDockerStorage) EnsureTargetReady(target string, create bool) error {
	entries, err := os.ReadDir(target)
	if err == nil {
		if len(entries) != 0 {
			return ValidationErr("目标目录必须为空，避免覆盖已有数据")
		}
		return nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if create {
		return os.MkdirAll(target, 0o710)
	}
	return nil
}

// DockerManager 聚合 Docker daemon 读路径与主机服务控制。
type DockerManager struct {
	engine  *dockerEngine
	host    dockerServiceHost
	storage dockerStorage
	runner  dockerCommandRunner
	now     func() time.Time
	mu      sync.Mutex
}

func NewDockerManager(endpoint string) *DockerManager {
	runner := realDockerCommandRunner{}
	return &DockerManager{
		engine: newDockerEngine(endpoint), host: &systemDockerServiceHost{runner: runner},
		storage: &osDockerStorage{daemonPath: defaultDaemonJSON}, runner: runner, now: time.Now,
	}
}

func newDockerManagerWithDeps(engine *dockerEngine, host dockerServiceHost, storage dockerStorage, runner dockerCommandRunner) *DockerManager {
	return &DockerManager{engine: engine, host: host, storage: storage, runner: runner, now: time.Now}
}

func (m *DockerManager) Overview(ctx context.Context) (DockerOverview, error) {
	now := m.now()
	o := DockerOverview{Service: m.host.Status(ctx), CheckedAt: now}
	info, infoErr := m.engine.info(ctx)
	if infoErr == nil {
		o.Service.State = DockerServiceRunning
		o.Service.Installed = true
		o.Version = info.ServerVersion
		o.Containers = DockerCountSummary{Running: info.ContainersRunning, Total: info.Containers}
	}
	containers, containersErr := m.engine.listContainers(ctx, nil)
	if containersErr == nil {
		projects := map[string]bool{}
		for _, c := range containers {
			name := c.Labels["com.docker.compose.project"]
			if name == "" {
				continue
			}
			if _, ok := projects[name]; !ok {
				projects[name] = false
			}
			if c.State == "running" || c.State == "restarting" {
				projects[name] = true
			}
		}
		o.ComposeProjects.Total = len(projects)
		for _, running := range projects {
			if running {
				o.ComposeProjects.Running++
			}
		}
	}
	root, source, configErr := m.configuredDataRoot()
	if infoErr == nil && strings.TrimSpace(info.DockerRootDir) != "" {
		root, source = filepath.Clean(info.DockerRootDir), "daemon"
	}
	o.Storage = DockerStorageSummary{Path: root, Source: source, Configured: root != ""}
	if configErr != nil {
		o.Storage.Error = configErr.Error()
	} else if root == "" || !filepath.IsAbs(root) {
		o.Storage.Error = "Docker data-root 必须是绝对路径"
	} else if disk, err := m.storage.Disk(root); err != nil {
		o.Storage.Error = "无法读取存储容量: " + err.Error()
	} else {
		o.Storage.Valid = true
		o.Storage.Disk = disk
	}
	if infoErr != nil {
		if o.Service.State == DockerServiceRunning {
			o.Service.State = DockerServiceStopped
		}
		detail := infoErr.Error()
		if hostDetail := o.Service.Diagnostic; hostDetail != "" {
			detail += "; " + hostDetail
		}
		o.Service.Diagnostic = limitDiagnostic(detail)
	}
	if o.Service.State != DockerServiceRunning || o.Containers.Running == 0 {
		o.IdleSummary = "空闲"
	} else {
		o.IdleSummary = fmt.Sprintf("%d 个容器运行中", o.Containers.Running)
	}
	return o, nil
}

func (m *DockerManager) Stats(ctx context.Context) (DockerStats, error) {
	out := DockerStats{SampledAt: m.now()}
	containers, err := m.engine.listContainersAll(ctx, false, nil)
	if err != nil {
		out.Diagnostic = "Docker daemon 不可用: " + err.Error()
		return out, nil
	}
	out.Available = true
	out.Containers = len(containers)
	if info, infoErr := m.engine.info(ctx); infoErr == nil && info.MemTotal > 0 {
		out.MemoryLimitBytes = uint64(info.MemTotal)
	}
	sampleCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	type sample struct {
		stats engineContainerStats
		err   error
	}
	results := make(chan sample, len(containers))
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for _, container := range containers {
		id := container.ID
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-sampleCtx.Done():
				results <- sample{err: sampleCtx.Err()}
				return
			}
			stats, err := m.engine.containerStats(sampleCtx, id)
			<-sem
			results <- sample{stats: stats, err: err}
		}()
	}
	wg.Wait()
	close(results)
	for result := range results {
		if result.err != nil {
			out.FailedContainers++
			continue
		}
		s := result.stats
		var cpuDelta, systemDelta uint64
		if s.CPUStats.CPUUsage.TotalUsage >= s.PreCPUStats.CPUUsage.TotalUsage {
			cpuDelta = s.CPUStats.CPUUsage.TotalUsage - s.PreCPUStats.CPUUsage.TotalUsage
		}
		if s.CPUStats.SystemCPUUsage >= s.PreCPUStats.SystemCPUUsage {
			systemDelta = s.CPUStats.SystemCPUUsage - s.PreCPUStats.SystemCPUUsage
		}
		cpus := s.CPUStats.OnlineCPUs
		if cpus == 0 {
			cpus = 1
		}
		if systemDelta > 0 && cpuDelta > 0 {
			out.CPUPercent += (float64(cpuDelta) / float64(systemDelta)) * float64(cpus) * 100
		}
		out.MemoryUsageBytes += s.MemoryStats.Usage
		for _, network := range s.Networks {
			out.NetworkRxBytes += network.RxBytes
			out.NetworkTxBytes += network.TxBytes
		}
	}
	if out.FailedContainers > 0 {
		out.Diagnostic = fmt.Sprintf("%d 个容器统计采样失败", out.FailedContainers)
	}
	if len(containers) > 0 && out.FailedContainers == len(containers) {
		out.Available = false
	}
	return out, nil
}

func (m *DockerManager) ServiceAction(ctx context.Context, req DockerServiceActionRequest) (DockerOverview, error) {
	action := strings.TrimSpace(req.Action)
	if action != "start" && action != "stop" && action != "restart" {
		return DockerOverview{}, ValidationErr("Docker 服务操作仅支持 start、stop、restart")
	}
	if action == "start" || action == "restart" {
		o, _ := m.Overview(ctx)
		if !o.Storage.Valid {
			return DockerOverview{}, CapabilityDetailErr("storage_invalid", "Docker 存储位置未配置或异常，请先完成存储设置", o.Storage.Error, nil)
		}
	}
	if err := m.host.Control(ctx, action); err != nil {
		return DockerOverview{}, m.withHostDiagnostic(ctx, err)
	}
	wantRunning := action != "stop"
	o, reached := m.waitForDaemonState(ctx, wantRunning, 5*time.Second)
	if !reached {
		return DockerOverview{}, CapabilityDetailErr("service_state_mismatch", "Docker 服务操作未达到预期状态", m.host.Diagnostic(ctx), nil)
	}
	return o, nil
}

func (m *DockerManager) waitForDaemonState(ctx context.Context, running bool, timeout time.Duration) (DockerOverview, bool) {
	deadline := time.Now().Add(timeout)
	for {
		o, _ := m.Overview(ctx)
		if (o.Service.State == DockerServiceRunning) == running {
			return o, true
		}
		if time.Now().After(deadline) {
			return o, false
		}
		select {
		case <-ctx.Done():
			return o, false
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func (m *DockerManager) SetAutostart(ctx context.Context, req DockerAutostartRequest) (DockerOverview, error) {
	if err := m.host.SetAutostart(ctx, req.Enabled); err != nil {
		return DockerOverview{}, m.withHostDiagnostic(ctx, err)
	}
	o, _ := m.Overview(ctx)
	if o.Service.AutostartEnabled == nil || *o.Service.AutostartEnabled != req.Enabled {
		return DockerOverview{}, CapabilityDetailErr("autostart_state_mismatch", "Docker 开机自启设置未达到预期状态", o.Service.Diagnostic, nil)
	}
	return o, nil
}

func (m *DockerManager) withHostDiagnostic(ctx context.Context, err error) error {
	ae, ok := AsError(err)
	if !ok || ae.Detail != "" {
		return err
	}
	ae.Detail = m.host.Diagnostic(ctx)
	return ae
}

func (m *DockerManager) configuredDataRoot() (string, string, error) {
	b, err := m.storage.ReadDaemonJSON()
	if err != nil {
		return "", "config", fmt.Errorf("读取 daemon.json 失败: %w", err)
	}
	var config map[string]json.RawMessage
	if err := json.Unmarshal(b, &config); err != nil {
		return "", "config", fmt.Errorf("daemon.json 格式无效: %w", err)
	}
	if raw, ok := config["data-root"]; ok {
		var root string
		if err := json.Unmarshal(raw, &root); err != nil {
			return "", "config", fmt.Errorf("daemon.json 的 data-root 不是字符串")
		}
		return filepath.Clean(strings.TrimSpace(root)), "config", nil
	}
	return defaultDockerDataRoot, "default", nil
}

func (m *DockerManager) MigrationPlan(ctx context.Context, req DockerMigrationRequest) (DockerMigrationPlan, error) {
	target := filepath.Clean(strings.TrimSpace(req.TargetPath))
	if target == "." || !filepath.IsAbs(target) {
		return DockerMigrationPlan{}, ValidationErr("Docker data-root 目标必须是绝对路径")
	}
	o, _ := m.Overview(ctx)
	if !o.Storage.Valid {
		return DockerMigrationPlan{}, CapabilityDetailErr("storage_invalid", "当前 Docker 存储位置异常，无法生成迁移计划", o.Storage.Error, nil)
	}
	source := filepath.Clean(o.Storage.Path)
	if target == source || strings.HasPrefix(target+string(os.PathSeparator), source+string(os.PathSeparator)) || strings.HasPrefix(source+string(os.PathSeparator), target+string(os.PathSeparator)) {
		return DockerMigrationPlan{}, ValidationErr("目标目录不能与当前 data-root 相同或互相包含")
	}
	if err := m.storage.EnsureTargetReady(target, false); err != nil {
		return DockerMigrationPlan{}, err
	}
	required, err := m.storage.DirSize(source)
	if err != nil {
		return DockerMigrationPlan{}, CapabilityDetailErr("storage_scan_failed", "无法计算当前 Docker 数据量", err.Error(), err)
	}
	disk, err := m.storage.Disk(target)
	if err != nil {
		return DockerMigrationPlan{}, ValidationErr("无法读取目标磁盘容量: " + err.Error())
	}
	if disk.AvailableBytes < required {
		return DockerMigrationPlan{}, ValidationErr(fmt.Sprintf("目标磁盘空间不足：需要 %d 字节，可用 %d 字节", required, disk.AvailableBytes))
	}
	currentJSON, err := m.storage.ReadDaemonJSON()
	if err != nil {
		return DockerMigrationPlan{}, CapabilityDetailErr("daemon_config_unreadable", "无法读取 Docker daemon 配置", err.Error(), err)
	}
	proposed, err := daemonJSONWithDataRoot(currentJSON, target)
	if err != nil {
		return DockerMigrationPlan{}, ValidationErr("daemon.json 格式无效: " + err.Error())
	}
	hash := sha256.Sum256([]byte(source + "\x00" + target + "\x00" + string(proposed)))
	plan := DockerMigrationPlan{
		ID: hex.EncodeToString(hash[:16]), SourcePath: source, TargetPath: target,
		DaemonConfigPath: defaultDaemonJSON, RequiredBytes: required, AvailableBytes: disk.AvailableBytes,
		ProposedDaemonJSON: string(proposed),
		Warnings:           []string{"迁移期间 Docker 与所有容器将停止", "成功后旧 data-root 会保留，确认运行正常后再人工清理"},
		Steps: []DockerMigrationStep{
			{Order: 1, Title: "停止 Docker", Description: "停止 docker.service 并以 daemon 实际状态复核"},
			{Order: 2, Title: "复制数据", Description: fmt.Sprintf("使用 rsync 保留权限与硬链接，将 %s 复制到 %s", source, target)},
			{Order: 3, Title: "更新配置", Description: fmt.Sprintf("原子写入 %s 的 data-root，保留其它配置项", defaultDaemonJSON)},
			{Order: 4, Title: "启动并验证", Description: "重新启动 Docker，并确认 daemon 返回的新 DockerRootDir"},
		},
	}
	return plan, nil
}

func daemonJSONWithDataRoot(current []byte, target string) ([]byte, error) {
	config := map[string]json.RawMessage{}
	if len(strings.TrimSpace(string(current))) > 0 {
		if err := json.Unmarshal(current, &config); err != nil {
			return nil, err
		}
	}
	rootJSON, _ := json.Marshal(target)
	config["data-root"] = rootJSON
	keys := make([]string, 0, len(config))
	for key := range config {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	ordered := make(map[string]json.RawMessage, len(config))
	for _, key := range keys {
		ordered[key] = config[key]
	}
	b, err := json.MarshalIndent(ordered, "", "  ")
	return append(b, '\n'), err
}

func (m *DockerManager) ExecuteMigration(ctx context.Context, req DockerMigrationExecuteRequest) (DockerMigrationResult, error) {
	if !req.Confirm || strings.TrimSpace(req.PlanID) == "" {
		return DockerMigrationResult{}, ValidationErr("执行迁移需要二次确认和有效的 planId")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	plan, err := m.MigrationPlan(ctx, DockerMigrationRequest{TargetPath: req.TargetPath})
	if err != nil {
		return DockerMigrationResult{}, err
	}
	if plan.ID != req.PlanID {
		return DockerMigrationResult{}, ConflictErr("migration_plan_changed", "迁移环境已变化，请重新生成计划并确认")
	}
	if _, err := m.runner.LookPath("rsync"); err != nil {
		return DockerMigrationResult{}, CapabilityDetailErr("rsync_unavailable", "缺少 rsync，无法安全迁移 Docker 数据", err.Error(), err)
	}
	if err := m.storage.EnsureTargetReady(plan.TargetPath, true); err != nil {
		return DockerMigrationResult{}, err
	}
	if err := m.host.Control(ctx, "stop"); err != nil {
		return DockerMigrationResult{}, m.withHostDiagnostic(ctx, err)
	}
	restartOld := func() { _ = m.host.Control(context.Background(), "start") }
	if _, stopped := m.waitForDaemonState(ctx, false, 10*time.Second); !stopped {
		restartOld()
		return DockerMigrationResult{}, CapabilityDetailErr("migration_stop_failed", "Docker 未完全停止，迁移未开始", m.host.Diagnostic(ctx), nil)
	}
	copyOut, err := m.runner.Run(ctx, "rsync", "-aHAX", "--numeric-ids", plan.SourcePath+string(os.PathSeparator), plan.TargetPath+string(os.PathSeparator))
	if err != nil {
		restartOld()
		return DockerMigrationResult{}, CapabilityDetailErr("migration_copy_failed", "Docker 数据复制失败，原配置未修改", limitDiagnostic(copyOut), err)
	}
	rollback, err := m.storage.WriteDaemonJSON([]byte(plan.ProposedDaemonJSON), plan.ID)
	if err != nil {
		restartOld()
		return DockerMigrationResult{}, CapabilityDetailErr("daemon_config_write_failed", "写入 daemon.json 失败，原数据未删除", err.Error(), err)
	}
	if err := m.host.Control(ctx, "start"); err != nil {
		rollbackErr := rollback()
		restartOld()
		detail := m.host.Diagnostic(ctx)
		if rollbackErr != nil {
			detail += "\n回滚 daemon.json 失败: " + rollbackErr.Error()
		}
		return DockerMigrationResult{}, CapabilityDetailErr("migration_start_failed", "Docker 使用新 data-root 启动失败，已尝试恢复原配置", limitDiagnostic(detail), err)
	}
	var info engineInfo
	verified := false
	deadline := time.Now().Add(10 * time.Second)
	for !verified {
		info, err = m.engine.info(ctx)
		verified = err == nil && filepath.Clean(info.DockerRootDir) == plan.TargetPath
		if verified || time.Now().After(deadline) {
			break
		}
		select {
		case <-ctx.Done():
			err = ctx.Err()
			deadline = time.Now()
		case <-time.After(250 * time.Millisecond):
		}
	}
	if !verified {
		rollbackErr := rollback()
		_ = m.host.Control(context.Background(), "restart")
		detail := "daemon 未报告预期的 DockerRootDir"
		if err != nil {
			detail = err.Error()
		}
		if rollbackErr != nil {
			detail += "; 回滚 daemon.json 失败: " + rollbackErr.Error()
		}
		return DockerMigrationResult{}, CapabilityDetailErr("migration_verify_failed", "Docker 存储迁移验证失败，已尝试恢复原配置", detail, err)
	}
	return DockerMigrationResult{Plan: plan, Completed: true, Message: "Docker 数据迁移完成；旧 data-root 已保留"}, nil
}
