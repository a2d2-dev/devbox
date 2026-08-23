package backup

import (
	"bufio"
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func validateTask(task Task, policy pathPolicy) error {
	if strings.TrimSpace(task.Name) == "" {
		return fmt.Errorf("task name is required")
	}
	if err := validateEndpoint(task.Source, policy); err != nil {
		return fmt.Errorf("source: %w", err)
	}
	if err := validateEndpoint(task.Target, policy); err != nil {
		return fmt.Errorf("target: %w", err)
	}
	if task.Target.Type != EndpointSSH {
		if err := policy.validateLocalTarget(task.Target.Path); err != nil {
			return fmt.Errorf("target: %w", err)
		}
	}
	if task.Source.Type == EndpointSSH && task.Target.Type == EndpointSSH {
		return fmt.Errorf("rsync does not support a remote source and remote target in one task")
	}
	if task.Mode != ModeVersioned && task.Mode != ModeMirror {
		return fmt.Errorf("mode must be versioned or mirror")
	}
	if task.Retention.KeepLast < 1 {
		return fmt.Errorf("retention.keepLast must be at least 1")
	}
	if _, err := nextSchedule(task.Schedule, timeNow()); err != nil {
		return fmt.Errorf("schedule: %w", err)
	}
	for _, pattern := range task.Excludes {
		if strings.ContainsAny(pattern, "\x00\r\n") {
			return fmt.Errorf("exclude pattern contains an invalid character")
		}
	}
	return nil
}

var timeNow = func() time.Time { return time.Now() }

func validateEndpoint(endpoint Endpoint, policy pathPolicy) error {
	if endpoint.Type != EndpointLocal && endpoint.Type != EndpointMount && endpoint.Type != EndpointSSH {
		return fmt.Errorf("type must be local, mount, or ssh")
	}
	if endpoint.Path == "" || strings.ContainsAny(endpoint.Path, "\x00\r\n") {
		return fmt.Errorf("path is required and must be a single line")
	}
	if !filepath.IsAbs(endpoint.Path) {
		return fmt.Errorf("path must be absolute")
	}
	if endpoint.Type != EndpointSSH {
		if endpoint.Host != "" || endpoint.IdentityFile != "" || endpoint.Port != 0 {
			return fmt.Errorf("ssh fields are only valid for ssh endpoints")
		}
		return nil
	}
	if endpoint.Host == "" || strings.HasPrefix(endpoint.Host, "-") || strings.ContainsAny(endpoint.Host, " \t\r\n") {
		return fmt.Errorf("ssh host must be user@host without whitespace")
	}
	if endpoint.Port < 0 || endpoint.Port > 65535 {
		return fmt.Errorf("ssh port is invalid")
	}
	if endpoint.IdentityFile != "" {
		if !filepath.IsAbs(endpoint.IdentityFile) || strings.ContainsAny(endpoint.IdentityFile, "\x00\r\n") {
			return fmt.Errorf("identity file must be an absolute single-line path")
		}
		if err := policy.validateIdentityFile(endpoint.IdentityFile); err != nil {
			return err
		}
	}
	return nil
}

func preflight(ctx context.Context, task Task, policy pathPolicy) PreflightResult {
	result := PreflightResult{OK: true, Checks: []Check{}}
	add := func(name string, err error) {
		check := Check{Name: name, OK: err == nil, Message: "通过"}
		if err != nil {
			check.Message = err.Error()
			result.OK = false
		}
		result.Checks = append(result.Checks, check)
	}

	configErr := validateTask(task, policy)
	add("配置", configErr)
	if configErr != nil {
		return result
	}
	if task.Source.Type == EndpointSSH {
		add("源可达性与读取权限", remoteTest(ctx, task.Source, "test -d "+shellQuote(task.Source.Path)+" && test -r "+shellQuote(task.Source.Path)))
	} else {
		err := checkLocalSource(task.Source.Path)
		if err == nil && task.Source.Type == EndpointMount {
			err = checkMountpoint(task.Source.Path)
		}
		add("源可达性与读取权限", err)
	}
	if task.Target.Type == EndpointSSH {
		add("目标可达性与写入权限", checkRemoteTargetWritable(ctx, task.Target))
	} else {
		err := checkLocalTarget(task.Target.Path)
		if err == nil && task.Target.Type == EndpointMount {
			err = checkMountpoint(task.Target.Path)
		}
		add("目标可达性与写入权限", err)
	}
	if task.Source.Type == EndpointSSH || task.Target.Type == EndpointSSH {
		warning := "SSH 端点未能检查路径循环，请确认远端路径与本地路径不指向同一存储"
		result.Warnings = append(result.Warnings, warning)
		result.Checks = append(result.Checks, Check{Name: "路径循环", OK: true, Message: warning})
	} else {
		add("路径循环", checkPathLoop(task.Source, task.Target))
	}

	var estimateErr, availableErr error
	if task.Source.Type == EndpointSSH {
		result.EstimatedBytes, estimateErr = remoteDiskUsage(ctx, task.Source)
	} else {
		result.EstimatedBytes, estimateErr = localDiskUsage(task.Source.Path, task.Excludes)
	}
	if task.Target.Type == EndpointSSH {
		result.AvailableBytes, availableErr = remoteAvailable(ctx, task.Target)
	} else {
		result.AvailableBytes, availableErr = localAvailable(task.Target.Path)
	}
	if estimateErr != nil {
		add("容量", fmt.Errorf("估算源数据量: %w", estimateErr))
	} else if availableErr != nil {
		add("容量", fmt.Errorf("读取目标可用容量: %w", availableErr))
	} else if result.EstimatedBytes > 0 && result.AvailableBytes > 0 && result.EstimatedBytes > result.AvailableBytes {
		add("容量", fmt.Errorf("可用容量不足：需要约 %d 字节，可用 %d 字节", result.EstimatedBytes, result.AvailableBytes))
	} else {
		add("容量", nil)
	}
	return result
}

func checkRemoteTargetWritable(ctx context.Context, endpoint Endpoint) error {
	template := strings.TrimSuffix(endpoint.Path, "/") + "/.devbox-backup-preflight.XXXXXX"
	command := "test -d " + shellQuote(endpoint.Path) +
		" && tmp=$(mktemp " + shellQuote(template) + ")" +
		" && rm -f -- \"$tmp\""
	return remoteTest(ctx, endpoint, command)
}

func checkMountpoint(path string) error {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}
	file, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		return fmt.Errorf("读取挂载信息: %w", err)
	}
	defer file.Close()
	decode := strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) > 4 && filepath.Clean(decode.Replace(fields[4])) == filepath.Clean(resolved) {
			return nil
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("读取挂载信息: %w", err)
	}
	return fmt.Errorf("路径不是当前挂载点；请确认外接设备已挂载")
}

func checkLocalSource(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("源路径不是目录")
	}
	if info.Mode().Perm()&0o444 == 0 {
		return fmt.Errorf("源目录没有读取权限")
	}
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("打开源目录: %w", err)
	}
	return f.Close()
}

func checkLocalTarget(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("目标路径不是目录")
	}
	if info.Mode().Perm()&0o222 == 0 {
		return fmt.Errorf("目标目录没有写入权限")
	}
	f, err := os.CreateTemp(path, ".devbox-backup-preflight-*")
	if err != nil {
		return fmt.Errorf("目标写入测试: %w", err)
	}
	name := f.Name()
	if err := f.Close(); err != nil {
		return err
	}
	return os.Remove(name)
}

func checkPathLoop(source, target Endpoint) error {
	if source.Type == EndpointSSH || target.Type == EndpointSSH {
		return nil
	}
	src, err := filepath.EvalSymlinks(source.Path)
	if err != nil {
		return fmt.Errorf("解析源路径: %w", err)
	}
	dst, err := filepath.EvalSymlinks(target.Path)
	if err != nil {
		return fmt.Errorf("解析目标路径: %w", err)
	}
	if pathWithin(src, dst, true) {
		return fmt.Errorf("目标路径不得等于或嵌套在源路径内")
	}
	if pathWithin(dst, src, true) {
		return fmt.Errorf("目标路径不得是源路径的祖先")
	}
	return nil
}

func excluded(rel string, patterns []string) bool {
	rel = filepath.ToSlash(rel)
	for _, pattern := range patterns {
		pattern = strings.TrimPrefix(filepath.ToSlash(pattern), "/")
		if matched, _ := filepath.Match(pattern, rel); matched {
			return true
		}
		if matched, _ := filepath.Match(pattern, filepath.Base(rel)); matched {
			return true
		}
		if strings.HasSuffix(pattern, "/") && strings.HasPrefix(rel+"/", strings.TrimSuffix(pattern, "/")+"/") {
			return true
		}
	}
	return false
}

func localDiskUsage(root string, excludes []string) (int64, error) {
	var total int64
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel != "." && excluded(rel, excludes) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type().IsRegular() {
			info, err := entry.Info()
			if err != nil {
				return err
			}
			total += info.Size()
		}
		return nil
	})
	return total, err
}

func localAvailable(path string) (int64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, err
	}
	return int64(stat.Bavail) * int64(stat.Bsize), nil
}

func sshArgs(endpoint Endpoint) []string {
	args := []string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=5"}
	if endpoint.Port > 0 {
		args = append(args, "-p", strconv.Itoa(endpoint.Port))
	}
	if endpoint.IdentityFile != "" {
		args = append(args, "-i", endpoint.IdentityFile)
	}
	return args
}

func remoteTest(ctx context.Context, endpoint Endpoint, command string) error {
	args := append(sshArgs(endpoint), endpoint.Host, command)
	out, err := exec.CommandContext(ctx, "ssh", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("ssh %s: %w: %s", endpoint.Host, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func remoteDiskUsage(ctx context.Context, endpoint Endpoint) (int64, error) {
	args := append(sshArgs(endpoint), endpoint.Host, "du -sk -- "+shellQuote(endpoint.Path))
	out, err := exec.CommandContext(ctx, "ssh", args...).Output()
	if err != nil {
		return 0, err
	}
	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		return 0, fmt.Errorf("ssh du returned no size")
	}
	kb, err := strconv.ParseInt(fields[0], 10, 64)
	return kb * 1024, err
}

func remoteAvailable(ctx context.Context, endpoint Endpoint) (int64, error) {
	args := append(sshArgs(endpoint), endpoint.Host, "df -Pk -- "+shellQuote(endpoint.Path)+" | tail -1")
	out, err := exec.CommandContext(ctx, "ssh", args...).Output()
	if err != nil {
		return 0, err
	}
	fields := strings.Fields(string(out))
	if len(fields) < 4 {
		return 0, fmt.Errorf("ssh df returned an unexpected result")
	}
	kb, err := strconv.ParseInt(fields[3], 10, 64)
	return kb * 1024, err
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
