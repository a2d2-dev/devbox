package shares

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

type SMBShare struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	ReadOnly bool   `json:"readOnly"`
	Guest    bool   `json:"guest"`
}

type SMBProbe struct {
	Installed         bool   `json:"installed"`
	Active            bool   `json:"active"`
	Binary            string `json:"binary,omitempty"`
	TestparmInstalled bool   `json:"testparmInstalled"`
	Message           string `json:"message"`
}

type CommandRunner interface {
	Run(context.Context, string, ...string) ([]byte, error)
	LookPath(string) (string, error)
}

type OSCommandRunner struct{}

func (OSCommandRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}
func (OSCommandRunner) LookPath(name string) (string, error) { return exec.LookPath(name) }

func ProbeSMB(ctx context.Context, runner CommandRunner) SMBProbe {
	if runner == nil {
		runner = OSCommandRunner{}
	}
	probe := SMBProbe{Message: "未安装 Samba；请使用系统包管理器安装 samba，DevBox 不会自动安装系统服务"}
	path, err := runner.LookPath("smbd")
	if err != nil {
		return probe
	}
	probe.Installed = true
	probe.Binary = path
	if _, err := runner.LookPath("testparm"); err == nil {
		probe.TestparmInstalled = true
	}
	out, err := runner.Run(ctx, "systemctl", "is-active", "smbd.service")
	probe.Active = err == nil && strings.TrimSpace(string(out)) == "active"
	if probe.Active {
		probe.Message = "Samba 服务运行中"
	} else {
		probe.Message = "Samba 已安装但服务未运行"
	}
	return probe
}

var smbNamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

func RenderSMB(dataRoot string, entries []SMBShare) (string, error) {
	seen := make(map[string]struct{}, len(entries))
	var b strings.Builder
	b.WriteString("# Managed by DevBox. Manual changes will be overwritten.\n")
	for i, entry := range entries {
		entry.Name = strings.TrimSpace(entry.Name)
		if !smbNamePattern.MatchString(entry.Name) || strings.EqualFold(entry.Name, "global") {
			return "", fmt.Errorf("share %d has an invalid name", i+1)
		}
		key := strings.ToLower(entry.Name)
		if _, ok := seen[key]; ok {
			return "", fmt.Errorf("duplicate SMB share name %q", entry.Name)
		}
		seen[key] = struct{}{}
		path, err := ResolveWithinRoot(dataRoot, entry.Path)
		if err != nil {
			return "", fmt.Errorf("share %q: %w", entry.Name, err)
		}
		fmt.Fprintf(&b, "\n[%s]\n", entry.Name)
		fmt.Fprintf(&b, "    path = %s\n", path)
		b.WriteString("    browseable = yes\n")
		fmt.Fprintf(&b, "    read only = %s\n", yesNo(entry.ReadOnly))
		fmt.Fprintf(&b, "    guest ok = %s\n", yesNo(entry.Guest))
		b.WriteString("    create mask = 0660\n    directory mask = 0770\n")
	}
	return b.String(), nil
}

func yesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

type SMBApplyResult struct {
	Path     string `json:"path"`
	Reloaded bool   `json:"reloaded"`
	Preview  string `json:"preview"`
}

// ApplySMB validates a temporary candidate before atomically replacing the
// managed include. A running daemon is reloaded only when smbstatus proves that
// it has no active share sessions.
func ApplySMB(ctx context.Context, runner CommandRunner, dataRoot, target string, entries []SMBShare) (SMBApplyResult, error) {
	if runner == nil {
		runner = OSCommandRunner{}
	}
	probe := ProbeSMB(ctx, runner)
	if !probe.Installed {
		return SMBApplyResult{}, errors.New(probe.Message)
	}
	if !probe.TestparmInstalled {
		return SMBApplyResult{}, errors.New("testparm is required before SMB configuration can be applied")
	}
	preview, err := RenderSMB(dataRoot, entries)
	if err != nil {
		return SMBApplyResult{}, err
	}
	if target == "" {
		target = "/etc/samba/devbox-shares.conf"
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
		return SMBApplyResult{}, fmt.Errorf("prepare SMB configuration directory: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), ".devbox-smb-*.conf")
	if err != nil {
		return SMBApplyResult{}, fmt.Errorf("create SMB validation file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o640); err != nil {
		tmp.Close()
		return SMBApplyResult{}, err
	}
	if _, err := tmp.WriteString(preview); err != nil {
		tmp.Close()
		return SMBApplyResult{}, fmt.Errorf("write SMB validation file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return SMBApplyResult{}, err
	}
	if out, err := runner.Run(ctx, "testparm", "-s", tmpName); err != nil {
		return SMBApplyResult{}, fmt.Errorf("testparm rejected SMB configuration: %s", strings.TrimSpace(string(out)))
	}

	if probe.Active {
		if _, err := runner.LookPath("smbstatus"); err != nil {
			return SMBApplyResult{}, errors.New("cannot verify active SMB connections because smbstatus is unavailable")
		}
		out, err := runner.Run(ctx, "smbstatus", "-S", "--no-processes")
		if err != nil {
			return SMBApplyResult{}, fmt.Errorf("cannot verify active SMB connections: %s", strings.TrimSpace(string(out)))
		}
		if hasActiveSMBSessions(string(out)) {
			return SMBApplyResult{}, errors.New("SMB has active connections; configuration was not written or reloaded")
		}
	}

	if err := os.Rename(tmpName, target); err != nil {
		return SMBApplyResult{}, fmt.Errorf("install managed SMB configuration: %w", err)
	}
	result := SMBApplyResult{Path: target, Preview: preview}
	if probe.Active {
		if out, err := runner.Run(ctx, "systemctl", "reload", "smbd.service"); err != nil {
			return result, fmt.Errorf("configuration written but smbd reload failed: %s", strings.TrimSpace(string(out)))
		}
		result.Reloaded = true
	}
	return result, nil
}

func hasActiveSMBSessions(output string) bool {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "Service") || strings.HasPrefix(line, "-") ||
			strings.HasPrefix(line, "Samba version") || strings.HasPrefix(line, "No locked files") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			return true
		}
	}
	return false
}
