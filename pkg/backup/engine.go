package backup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var (
	versionPattern = regexp.MustCompile(`^\d{8}T\d{6}Z(?:-\d+)?$`)
	bytesPattern   = regexp.MustCompile(`(?m)^Total transferred file size: ([0-9,]+) bytes`)
)

type runFailure struct {
	phase string
	log   string
	err   error
}

func (e *runFailure) Error() string { return fmt.Sprintf("%s: %v", e.phase, e.err) }
func (e *runFailure) Unwrap() error { return e.err }

type engine struct {
	now func() time.Time
}

func newEngine() *engine { return &engine{now: time.Now} }

func (e *engine) runBackup(ctx context.Context, task Task) (version string, transferred int64, log string, err error) {
	check := preflight(ctx, task)
	if !check.OK {
		return "", 0, "", &runFailure{phase: "preflight", err: fmt.Errorf("%s", failedChecks(check))}
	}

	var destination Endpoint
	var finalDestination Endpoint
	var previous string
	if task.Mode == ModeVersioned {
		if cleanupErr := cleanupIncomplete(ctx, task.Target, task.ID); cleanupErr != nil {
			return "", 0, "", &runFailure{phase: "prepare", err: cleanupErr}
		}
		versions, listErr := listVersions(ctx, task.Target)
		if listErr != nil {
			return "", 0, "", &runFailure{phase: "prepare", err: listErr}
		}
		if len(versions) > 0 {
			previous = versions[len(versions)-1]
		}
		version = e.now().UTC().Format("20060102T150405Z")
		for suffix := 1; containsString(versions, version); suffix++ {
			version = e.now().UTC().Format("20060102T150405Z") + "-" + strconv.Itoa(suffix)
		}
		finalDestination = childEndpoint(task.Target, version)
		destination = childEndpoint(task.Target, ".devbox-incomplete-"+task.ID+"-"+version)
		if prepErr := makeEndpointDir(ctx, destination); prepErr != nil {
			return "", 0, "", &runFailure{phase: "prepare", err: prepErr}
		}
	} else {
		version = "mirror"
		destination = task.Target
	}

	args := []string{"--archive", "--stats", "--itemize-changes"}
	if task.Delete {
		args = append(args, "--delete")
	}
	if task.Mode == ModeVersioned && task.Incremental && previous != "" {
		args = append(args, "--link-dest=../"+previous)
	}
	for _, pattern := range task.Excludes {
		args = append(args, "--exclude="+pattern)
	}
	args = addRemoteShell(args, task.Source, destination)
	args = append(args, endpointSpec(task.Source, true), endpointSpec(destination, true))
	out, rsyncErr := command(ctx, "rsync", args...)
	log = out
	if rsyncErr != nil {
		if task.Mode == ModeVersioned {
			_ = removeEndpointDir(ctx, destination)
		}
		return version, 0, log, &runFailure{phase: "transfer", log: log, err: rsyncErr}
	}
	transferred = parseTransferredBytes(log)
	if task.Mode == ModeVersioned {
		if finalizeErr := renameEndpointDir(ctx, destination, finalDestination); finalizeErr != nil {
			_ = removeEndpointDir(ctx, destination)
			return version, transferred, log, &runFailure{phase: "finalize", log: log, err: finalizeErr}
		}
		if retentionErr := applyRetention(ctx, task.Target, task.Retention.KeepLast); retentionErr != nil {
			return version, transferred, log, &runFailure{phase: "retention", log: log, err: retentionErr}
		}
	}
	return version, transferred, log, nil
}

func cleanupIncomplete(ctx context.Context, target Endpoint, taskID string) error {
	prefix := ".devbox-incomplete-" + taskID + "-"
	if target.Type == EndpointSSH {
		command := "find " + shellQuote(target.Path) + " -mindepth 1 -maxdepth 1 -type d -name " + shellQuote(prefix+"*") + " -exec rm -rf -- {} +"
		return remoteTest(ctx, target, command)
	}
	entries, err := os.ReadDir(target.Path)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), prefix) {
			if err := os.RemoveAll(filepath.Join(target.Path, entry.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}

func renameEndpointDir(ctx context.Context, source, target Endpoint) error {
	if source.Type == EndpointSSH {
		return remoteTest(ctx, source, "mv -- "+shellQuote(source.Path)+" "+shellQuote(target.Path))
	}
	return os.Rename(source.Path, target.Path)
}

func removeEndpointDir(ctx context.Context, endpoint Endpoint) error {
	if endpoint.Type == EndpointSSH {
		return remoteTest(ctx, endpoint, "rm -rf -- "+shellQuote(endpoint.Path))
	}
	return os.RemoveAll(endpoint.Path)
}

func failedChecks(result PreflightResult) string {
	parts := []string{}
	for _, check := range result.Checks {
		if !check.OK {
			parts = append(parts, check.Name+": "+check.Message)
		}
	}
	return strings.Join(parts, "; ")
}

func endpointSpec(endpoint Endpoint, trailingSlash bool) string {
	path := endpoint.Path
	if trailingSlash && !strings.HasSuffix(path, "/") {
		path += "/"
	}
	if endpoint.Type == EndpointSSH {
		return endpoint.Host + ":" + path
	}
	return path
}

func childEndpoint(parent Endpoint, child string) Endpoint {
	parent.Path = filepath.Join(parent.Path, child)
	return parent
}

func addRemoteShell(args []string, endpoints ...Endpoint) []string {
	for _, endpoint := range endpoints {
		if endpoint.Type != EndpointSSH {
			continue
		}
		parts := []string{"ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=5"}
		if endpoint.Port > 0 {
			parts = append(parts, "-p", strconv.Itoa(endpoint.Port))
		}
		if endpoint.IdentityFile != "" {
			parts = append(parts, "-i", shellQuote(endpoint.IdentityFile))
		}
		return append(args, "--protect-args", "-e", strings.Join(parts, " "))
	}
	return args
}

func command(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		if text == "" {
			return text, err
		}
		return text, fmt.Errorf("%w: %s", err, text)
	}
	return text, nil
}

func parseTransferredBytes(log string) int64 {
	match := bytesPattern.FindStringSubmatch(log)
	if len(match) != 2 {
		return 0
	}
	value, _ := strconv.ParseInt(strings.ReplaceAll(match[1], ",", ""), 10, 64)
	return value
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func listVersions(ctx context.Context, target Endpoint) ([]string, error) {
	var names []string
	if target.Type == EndpointSSH {
		args := append(sshArgs(target), target.Host, "find "+shellQuote(target.Path)+" -mindepth 1 -maxdepth 1 -type d -printf '%f\\n'")
		out, err := command(ctx, "ssh", args...)
		if err != nil {
			return nil, err
		}
		names = strings.Fields(out)
	} else {
		entries, err := os.ReadDir(target.Path)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if entry.IsDir() {
				names = append(names, entry.Name())
			}
		}
	}
	versions := names[:0]
	for _, name := range names {
		if versionPattern.MatchString(name) {
			versions = append(versions, name)
		}
	}
	sort.Strings(versions)
	return versions, nil
}

func makeEndpointDir(ctx context.Context, endpoint Endpoint) error {
	if endpoint.Type == EndpointSSH {
		return remoteTest(ctx, endpoint, "mkdir -- "+shellQuote(endpoint.Path))
	}
	return os.Mkdir(endpoint.Path, 0o750)
}

func applyRetention(ctx context.Context, target Endpoint, keep int) error {
	versions, err := listVersions(ctx, target)
	if err != nil {
		return err
	}
	if keep < 1 || len(versions) <= keep {
		return nil
	}
	for _, version := range versions[:len(versions)-keep] {
		if !versionPattern.MatchString(version) {
			return fmt.Errorf("refusing to remove invalid version %q", version)
		}
		path := filepath.Join(target.Path, version)
		if target.Type == EndpointSSH {
			if err := remoteTest(ctx, target, "rm -rf -- "+shellQuote(path)); err != nil {
				return err
			}
		} else if err := os.RemoveAll(path); err != nil {
			return err
		}
	}
	return nil
}

func (e *engine) previewRestore(ctx context.Context, task Task, request RestoreRequest) (RestorePreview, error) {
	source, destination, err := restoreEndpoints(ctx, task, request)
	if err != nil {
		return RestorePreview{}, err
	}
	args := []string{"--archive", "--checksum", "--dry-run", "--itemize-changes", "--out-format=%i|%n"}
	args = addRemoteShell(args, source, destination)
	args = append(args, endpointSpec(source, true), endpointSpec(destination, true))
	out, err := command(ctx, "rsync", args...)
	if err != nil {
		return RestorePreview{}, &runFailure{phase: "restore-preview", log: out, err: err}
	}
	changes := parseChanges(out)
	conflicts := []string{}
	if destination.Type != EndpointSSH {
		for _, change := range changes {
			if _, statErr := os.Lstat(filepath.Join(destination.Path, filepath.FromSlash(change))); statErr == nil {
				conflicts = append(conflicts, change)
			}
		}
	} else {
		conflicts = append(conflicts, changes...)
	}
	preview := RestorePreview{
		TaskID: task.ID, Version: request.Version, Destination: endpointSpec(destination, false),
		Conflicts: conflicts, Changes: changes,
	}
	preview.Token = restoreToken(preview)
	return preview, nil
}

func restoreEndpoints(ctx context.Context, task Task, request RestoreRequest) (Endpoint, Endpoint, error) {
	version := request.Version
	if task.Mode == ModeMirror {
		if version != "" && version != "mirror" {
			return Endpoint{}, Endpoint{}, fmt.Errorf("mirror task only has the mirror version")
		}
		version = "mirror"
	} else if !versionPattern.MatchString(version) {
		return Endpoint{}, Endpoint{}, fmt.Errorf("invalid backup version")
	}
	source := task.Target
	if task.Mode == ModeVersioned {
		source = childEndpoint(source, version)
	}
	destination := task.Source
	if request.Destination != "" {
		destination = Endpoint{Type: EndpointLocal, Path: request.Destination}
		if err := validateEndpoint(destination); err != nil {
			return Endpoint{}, Endpoint{}, err
		}
	}
	if source.Type == EndpointSSH && destination.Type == EndpointSSH {
		return Endpoint{}, Endpoint{}, fmt.Errorf("remote-to-remote restore is not supported")
	}
	if source.Type != EndpointSSH {
		if info, err := os.Stat(source.Path); err != nil || !info.IsDir() {
			return Endpoint{}, Endpoint{}, fmt.Errorf("backup version is not available")
		}
	}
	if destination.Type != EndpointSSH {
		if err := checkLocalTarget(destination.Path); err != nil {
			return Endpoint{}, Endpoint{}, fmt.Errorf("restore destination: %w", err)
		}
	} else if err := remoteTest(ctx, destination, "test -d "+shellQuote(destination.Path)+" && test -w "+shellQuote(destination.Path)); err != nil {
		return Endpoint{}, Endpoint{}, err
	}
	return source, destination, nil
}

func parseChanges(output string) []string {
	changes := []string{}
	for _, line := range strings.Split(output, "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), "|", 2)
		if len(parts) != 2 || len(parts[0]) < 2 || parts[0][1] == 'd' {
			continue
		}
		name := strings.TrimSuffix(strings.TrimSpace(parts[1]), "/")
		clean := filepath.Clean(filepath.FromSlash(name))
		if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			continue
		}
		changes = append(changes, filepath.ToSlash(clean))
	}
	return changes
}

func restoreToken(preview RestorePreview) string {
	payload, _ := json.Marshal(struct {
		TaskID      string
		Version     string
		Destination string
		Conflicts   []string
		Changes     []string
	}{preview.TaskID, preview.Version, preview.Destination, preview.Conflicts, preview.Changes})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func (e *engine) runRestore(ctx context.Context, task Task, request RestoreRequest) (int64, string, error) {
	if !request.Confirm || request.PreviewToken == "" {
		return 0, "", fmt.Errorf("restore requires confirmation from a current preview")
	}
	preview, err := e.previewRestore(ctx, task, request)
	if err != nil {
		return 0, "", err
	}
	if preview.Token != request.PreviewToken {
		return 0, "", fmt.Errorf("restore preview changed; preview again before confirming")
	}
	source, destination, err := restoreEndpoints(ctx, task, request)
	if err != nil {
		return 0, "", err
	}
	args := []string{"--archive", "--checksum", "--stats", "--itemize-changes"}
	args = addRemoteShell(args, source, destination)
	args = append(args, endpointSpec(source, true), endpointSpec(destination, true))
	out, err := command(ctx, "rsync", args...)
	if err != nil {
		return 0, out, &runFailure{phase: "restore-transfer", log: out, err: err}
	}
	return parseTransferredBytes(out), out, nil
}
