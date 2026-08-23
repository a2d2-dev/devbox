package backup

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func localTask(source, target string) Task {
	return Task{
		Name:        "test backup",
		Source:      Endpoint{Type: EndpointLocal, Path: source},
		Target:      Endpoint{Type: EndpointLocal, Path: target},
		Schedule:    Schedule{Kind: "daily", Hour: 3, Minute: 15},
		Retention:   RetentionPolicy{KeepLast: 2},
		Mode:        ModeVersioned,
		Incremental: true,
	}
}

func waitForHistory(t *testing.T, manager *Manager, taskID string, count int) []History {
	t.Helper()
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		histories, err := manager.Histories(taskID)
		if err == nil && len(histories) >= count && histories[0].FinishedAt != nil {
			return histories
		}
		time.Sleep(20 * time.Millisecond)
	}
	histories, _ := manager.Histories(taskID)
	t.Fatalf("history did not finish: %+v", histories)
	return nil
}

func TestVersionedIncrementalBackupAndRetention(t *testing.T) {
	requireRsync(t)
	root := t.TempDir()
	source := filepath.Join(root, "source")
	target := filepath.Join(root, "target")
	data := filepath.Join(root, "state")
	mustMkdir(t, source)
	mustMkdir(t, target)
	mustWrite(t, filepath.Join(source, "stable.txt"), "stable")

	manager, err := NewManager(data, 1, nil, WithWorkDir(root))
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	manager.engine.now = func() time.Time { return base }
	created, result, err := manager.Create(context.Background(), localTask(source, target))
	if err != nil || !result.OK {
		t.Fatalf("create: result=%+v err=%v", result, err)
	}

	if _, err := manager.RunNow(created.ID); err != nil {
		t.Fatal(err)
	}
	firstHistory := waitForHistory(t, manager, created.ID, 1)
	if firstHistory[0].Status != StatusSuccess {
		t.Fatalf("first backup failed: %+v", firstHistory[0])
	}
	if firstHistory[0].TransferredBytes == 0 {
		t.Fatalf("first backup did not record transferred bytes: %+v", firstHistory[0])
	}
	firstVersion := firstHistory[0].Version
	firstInfo, err := os.Stat(filepath.Join(target, created.ID, firstVersion, "stable.txt"))
	if err != nil {
		t.Fatal(err)
	}

	mustWrite(t, filepath.Join(source, "second.txt"), "second")
	if _, err := manager.RunNow(created.ID); err != nil {
		t.Fatal(err)
	}
	secondHistory := waitForHistory(t, manager, created.ID, 2)
	secondInfo, err := os.Stat(filepath.Join(target, created.ID, secondHistory[0].Version, "stable.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(firstInfo, secondInfo) {
		t.Fatal("unchanged file was not hard-linked to the previous version")
	}

	mustWrite(t, filepath.Join(source, "third.txt"), "third")
	if _, err := manager.RunNow(created.ID); err != nil {
		t.Fatal(err)
	}
	thirdHistory := waitForHistory(t, manager, created.ID, 3)
	if thirdHistory[0].Status != StatusSuccess {
		t.Fatalf("third backup failed: %+v", thirdHistory[0])
	}
	versions, err := manager.Versions(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 2 {
		t.Fatalf("retention kept %d versions, want 2: %v", len(versions), versions)
	}
	if _, err := os.Stat(filepath.Join(target, created.ID, firstVersion)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("oldest version was not removed: %v", err)
	}
}

func TestVersionsBeforeFirstRunIsEmptyInTaskNamespace(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	target := filepath.Join(root, "target")
	mustMkdir(t, source)
	mustMkdir(t, target)
	manager, err := NewManager(filepath.Join(root, "state"), 1, nil, WithWorkDir(root))
	if err != nil {
		t.Fatal(err)
	}
	created, _, err := manager.Create(context.Background(), localTask(source, target))
	if err != nil {
		t.Fatal(err)
	}

	versions, err := manager.Versions(context.Background(), created.ID)
	if err != nil || len(versions) != 0 {
		t.Fatalf("versions before first run=%v err=%v", versions, err)
	}
}

func TestRestoreRequiresPreviewAndRestoresConflict(t *testing.T) {
	requireRsync(t)
	root := t.TempDir()
	source := filepath.Join(root, "source")
	target := filepath.Join(root, "target")
	restore := filepath.Join(root, "restore")
	for _, path := range []string{source, target, restore} {
		mustMkdir(t, path)
	}
	mustWrite(t, filepath.Join(source, "config.txt"), "from-backup")
	mustWrite(t, filepath.Join(restore, "config.txt"), "conflicting")

	manager, err := NewManager(filepath.Join(root, "state"), 1, nil, WithWorkDir(root))
	if err != nil {
		t.Fatal(err)
	}
	created, _, err := manager.Create(context.Background(), localTask(source, target))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.RunNow(created.ID); err != nil {
		t.Fatal(err)
	}
	histories := waitForHistory(t, manager, created.ID, 1)
	version := histories[0].Version
	preview, err := manager.PreviewRestore(context.Background(), created.ID, RestoreRequest{Version: version, Destination: restore})
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Conflicts) != 1 || preview.Conflicts[0] != "config.txt" {
		t.Fatalf("unexpected conflicts: %+v", preview)
	}
	if _, err := manager.Restore(context.Background(), created.ID, RestoreRequest{Version: version, Destination: restore}); err == nil {
		t.Fatal("restore without confirmation succeeded")
	}
	if _, err := manager.Restore(context.Background(), created.ID, RestoreRequest{
		Version: version, Destination: restore, Confirm: true, PreviewToken: preview.Token,
	}); err != nil {
		t.Fatal(err)
	}
	restoreHistory := waitForHistory(t, manager, created.ID, 2)
	if restoreHistory[0].Kind != RunRestore || restoreHistory[0].Status != StatusSuccess {
		t.Fatalf("restore failed: %+v", restoreHistory[0])
	}
	if restoreHistory[0].TransferredBytes == 0 {
		t.Fatalf("restore did not record transferred bytes: %+v", restoreHistory[0])
	}
	content, err := os.ReadFile(filepath.Join(restore, "config.txt"))
	if err != nil || string(content) != "from-backup" {
		t.Fatalf("restored content=%q err=%v", content, err)
	}
}

func TestRestorePathPolicyAllowsOriginalSourceButRejectsOtherOutsidePath(t *testing.T) {
	requireRsync(t)
	root := t.TempDir()
	externalRoot, err := os.MkdirTemp("/var/tmp", "devbox-backup-restore-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(externalRoot) })
	workDir := filepath.Join(root, "work")
	source := filepath.Join(externalRoot, "source-outside-work")
	target := filepath.Join(workDir, "target")
	outside := filepath.Join(externalRoot, "other-outside-work")
	for _, path := range []string{workDir, source, target, outside} {
		mustMkdir(t, path)
	}
	manager, err := NewManager(filepath.Join(root, "state"), 1, nil, WithWorkDir(workDir))
	if err != nil {
		t.Fatal(err)
	}
	created, _, err := manager.Create(context.Background(), localTask(source, target))
	if err != nil {
		t.Fatal(err)
	}
	version := "20260823T120000Z"
	versionDir := filepath.Join(target, created.ID, version)
	mustMkdir(t, versionDir)
	mustWrite(t, filepath.Join(versionDir, "config.txt"), "backup")

	if _, err := manager.PreviewRestore(context.Background(), created.ID, RestoreRequest{
		Version: version, Destination: source,
	}); err != nil {
		t.Fatalf("explicit restore to original source should be allowed: %v", err)
	}
	if _, err := manager.PreviewRestore(context.Background(), created.ID, RestoreRequest{
		Version: version, Destination: outside,
	}); err == nil || !strings.Contains(err.Error(), "目标路径") {
		t.Fatalf("outside restore destination should be rejected by path policy: %v", err)
	}
}

func TestPreflightRejectsUnmountedMountTarget(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	target := filepath.Join(root, "looks-mounted")
	mustMkdir(t, source)
	mustMkdir(t, target)
	task := localTask(source, target)
	task.Target.Type = EndpointMount
	result := preflight(context.Background(), task, testPolicy(t, root))
	assertFailedCheck(t, result, "目标可达性与写入权限")
}

func TestPathPolicyRejectsProtectedTargetAndAllowedRootAncestor(t *testing.T) {
	root := t.TempDir()
	workDir := filepath.Join(root, "work")
	source := filepath.Join(workDir, "source")
	target := filepath.Join(workDir, "target")
	for _, path := range []string{source, target} {
		mustMkdir(t, path)
	}
	manager, err := NewManager(filepath.Join(root, "state"), 1, nil, WithWorkDir(workDir))
	if err != nil {
		t.Fatal(err)
	}

	t.Run("protected target", func(t *testing.T) {
		result := manager.Preflight(context.Background(), localTask(source, "/etc"))
		assertFailedCheck(t, result, "配置")
		if len(result.Checks) != 1 {
			t.Fatalf("unsafe target was probed after policy rejection: %+v", result.Checks)
		}
	})

	t.Run("allowed root ancestor", func(t *testing.T) {
		policy := pathPolicy{workDir: workDir, targetRoots: []string{workDir}}
		if err := policy.validateLocalTarget(root); err == nil || !strings.Contains(err.Error(), "允许根") {
			t.Fatalf("allowed root ancestor should be rejected by path policy: %v", err)
		}
	})

	t.Run("symlink escape", func(t *testing.T) {
		link := filepath.Join(workDir, "etc-link")
		if err := os.Symlink("/etc", link); err != nil {
			t.Fatal(err)
		}
		result := manager.Preflight(context.Background(), localTask(source, link))
		assertFailedCheck(t, result, "配置")
	})
}

func TestPathPolicyProtectsVarWhenWorkDirIsFilesystemRoot(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	mustMkdir(t, source)
	manager, err := NewManager(filepath.Join(root, "state"), 1, nil, WithWorkDir("/"))
	if err != nil {
		t.Fatal(err)
	}

	result := manager.Preflight(context.Background(), localTask(source, "/var/tmp"))
	assertFailedCheck(t, result, "配置")
}

func TestIdentityFileOutsideBackupKeysDirectoryIsRejected(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	mustMkdir(t, target)
	manager, err := NewManager(filepath.Join(root, "state"), 1, nil, WithWorkDir(root))
	if err != nil {
		t.Fatal(err)
	}
	task := localTask(filepath.Join(root, "unused-source"), target)
	task.Source = Endpoint{
		Type: EndpointSSH, Host: "backup@example.test", Path: "/backup",
		IdentityFile: "/root/.ssh/id_rsa",
	}
	result := manager.Preflight(context.Background(), task)
	message := failedCheckMessage(result, "配置")
	if !strings.Contains(message, manager.policy.keysDir) ||
		(!strings.Contains(message, "上传") && !strings.Contains(message, "放入")) {
		t.Fatalf("host identity file rejection lacks placement guidance: %q", message)
	}
	if len(result.Checks) != 1 {
		t.Fatalf("SSH was probed after identity policy rejection: %+v", result.Checks)
	}
}

func TestAdditionalAllowedTargetRootCanBeConfigured(t *testing.T) {
	root := t.TempDir()
	workDir := filepath.Join(root, "work")
	extraRoot := filepath.Join(root, "backup-volume")
	source := filepath.Join(root, "source")
	target := filepath.Join(extraRoot, "target")
	for _, path := range []string{workDir, extraRoot, source, target} {
		mustMkdir(t, path)
	}
	manager, err := NewManager(filepath.Join(root, "state"), 1, nil,
		WithWorkDir(workDir), WithAllowedTargetRoots(extraRoot))
	if err != nil {
		t.Fatal(err)
	}

	result := manager.Preflight(context.Background(), localTask(source, target))
	if !result.OK {
		t.Fatalf("configured target root was not accepted: %+v", result)
	}
}

func TestCreatePersistsResolvedLocalPaths(t *testing.T) {
	root := t.TempDir()
	realSource := filepath.Join(root, "real-source")
	realTarget := filepath.Join(root, "real-target")
	sourceAlias := filepath.Join(root, "source-alias")
	targetAlias := filepath.Join(root, "target-alias")
	mustMkdir(t, realSource)
	mustMkdir(t, realTarget)
	if err := os.Symlink(realSource, sourceAlias); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realTarget, targetAlias); err != nil {
		t.Fatal(err)
	}
	manager, err := NewManager(filepath.Join(root, "state"), 1, nil, WithWorkDir(root))
	if err != nil {
		t.Fatal(err)
	}

	created, _, err := manager.Create(context.Background(), localTask(sourceAlias, targetAlias))
	if err != nil {
		t.Fatal(err)
	}
	if created.Source.Path != realSource || created.Target.Path != realTarget {
		t.Fatalf("task retained unresolved paths: source=%q target=%q", created.Source.Path, created.Target.Path)
	}
}

func TestPreflightRejectsLoopUnwritableAndUnreachableSSH(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	targetInside := filepath.Join(source, "backup")
	unwritable := filepath.Join(root, "unwritable")
	for _, path := range []string{source, targetInside, unwritable} {
		mustMkdir(t, path)
	}

	policy := testPolicy(t, root)
	loop := preflight(context.Background(), localTask(source, targetInside), policy)
	assertFailedCheck(t, loop, "路径循环")

	if err := os.Chmod(unwritable, 0o500); err != nil {
		t.Fatal(err)
	}
	permissions := preflight(context.Background(), localTask(source, unwritable), policy)
	assertFailedCheck(t, permissions, "目标可达性与写入权限")

	remote := localTask(source, targetInside)
	remote.Target = Endpoint{Type: EndpointSSH, Host: "backup@does-not-exist.invalid", Path: "/backup", Port: 22}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	unreachable := preflight(ctx, remote, policy)
	assertFailedCheck(t, unreachable, "目标可达性与写入权限")
}

func TestPreflightRejectsTargetThatIsSourceAncestor(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	source := filepath.Join(target, "source")
	mustMkdir(t, source)
	manager, err := NewManager(filepath.Join(root, "state"), 1, nil, WithWorkDir(root))
	if err != nil {
		t.Fatal(err)
	}

	result := manager.Preflight(context.Background(), localTask(source, target))
	assertFailedCheck(t, result, "路径循环")
}

func TestPreflightWarnsWhenSSHPathLoopCannotBeChecked(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	bin := filepath.Join(root, "bin")
	mustMkdir(t, source)
	mustMkdir(t, bin)
	ssh := filepath.Join(bin, "ssh")
	sshScript := `#!/bin/sh
case "$*" in
  *"df -Pk"*) echo 'fs 100 1 99 1% /backup' ;;
esac
`
	if err := os.WriteFile(ssh, []byte(sshScript), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	manager, err := NewManager(filepath.Join(root, "state"), 1, nil, WithWorkDir(root))
	if err != nil {
		t.Fatal(err)
	}
	task := localTask(source, filepath.Join(root, "unused"))
	task.Target = Endpoint{Type: EndpointSSH, Host: "backup@example.test", Path: "/backup"}

	result := manager.Preflight(context.Background(), task)
	if !result.OK {
		t.Fatalf("preflight failed unexpectedly: %+v", result)
	}
	if !strings.Contains(strings.Join(result.Warnings, "\n"), "未能检查路径循环") {
		t.Fatalf("SSH path-loop warning missing: %+v", result)
	}
}

func TestSSHPreflightCreatesAndDeletesTemporaryFile(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	bin := filepath.Join(root, "bin")
	capture := filepath.Join(root, "ssh-commands")
	mustMkdir(t, source)
	mustMkdir(t, bin)
	t.Setenv("BACKUP_TEST_SSH_CAPTURE", capture)
	ssh := filepath.Join(bin, "ssh")
	sshScript := `#!/bin/sh
printf '%s\n' "$*" >> "$BACKUP_TEST_SSH_CAPTURE"
case "$*" in
  *"df -Pk"*) echo 'fs 100 1 99 1% /backup' ;;
esac
`
	if err := os.WriteFile(ssh, []byte(sshScript), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	manager, err := NewManager(filepath.Join(root, "state"), 1, nil, WithWorkDir(root))
	if err != nil {
		t.Fatal(err)
	}
	task := localTask(source, filepath.Join(root, "unused"))
	task.Target = Endpoint{Type: EndpointSSH, Host: "backup@example.test", Path: "/backup path"}

	result := manager.Preflight(context.Background(), task)
	if !result.OK {
		t.Fatalf("preflight failed unexpectedly: %+v", result)
	}
	commands, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	text := string(commands)
	if !strings.Contains(text, "mktemp") || !strings.Contains(text, "rm -f") {
		t.Fatalf("SSH target write check did not create and remove a temporary file:\n%s", text)
	}
}

func TestFailedRsyncPersistsTransferPhaseAndOutput(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	target := filepath.Join(root, "target")
	bin := filepath.Join(root, "bin")
	for _, path := range []string{source, target, bin} {
		mustMkdir(t, path)
	}
	mustWrite(t, filepath.Join(source, "file"), "data")
	script := filepath.Join(bin, "rsync")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho 'simulated rsync failure' >&2\nexit 23\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	manager, err := NewManager(filepath.Join(root, "state"), 1, nil, WithWorkDir(root))
	if err != nil {
		t.Fatal(err)
	}
	created, _, err := manager.Create(context.Background(), localTask(source, target))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.RunNow(created.ID); err != nil {
		t.Fatal(err)
	}
	histories := waitForHistory(t, manager, created.ID, 1)
	if histories[0].Status != StatusFailed || histories[0].Phase != "transfer" {
		t.Fatalf("failure was not attributed to transfer: %+v", histories[0])
	}
	if !strings.Contains(histories[0].Log, "simulated rsync failure") || !strings.Contains(histories[0].Error, "exit status 23") {
		t.Fatalf("rsync evidence missing: %+v", histories[0])
	}
	versions, err := manager.Versions(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 0 {
		t.Fatalf("failed run exposed an incomplete version: %v", versions)
	}
}

func TestScheduleAndPersistence(t *testing.T) {
	after := time.Date(2026, 8, 23, 12, 34, 0, 0, time.UTC)
	next, err := nextSchedule(Schedule{Kind: "cron", Cron: "*/15 9-17 * * 1-5"}, after)
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2026, 8, 24, 9, 0, 0, 0, time.UTC); !next.Equal(want) {
		t.Fatalf("next=%s want=%s", next, want)
	}

	root := t.TempDir()
	source := filepath.Join(root, "source")
	target := filepath.Join(root, "target")
	mustMkdir(t, source)
	mustMkdir(t, target)
	data := filepath.Join(root, "state")
	first, err := NewManager(data, 1, nil, WithWorkDir(root))
	if err != nil {
		t.Fatal(err)
	}
	created, _, err := first.Create(context.Background(), localTask(source, target))
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewManager(data, 1, nil, WithWorkDir(root))
	if err != nil {
		t.Fatal(err)
	}
	reloaded, err := second.Get(created.ID)
	if err != nil || reloaded.NextRunAt == nil || reloaded.Name != created.Name {
		t.Fatalf("reloaded=%+v err=%v", reloaded, err)
	}
}

func TestSchedulerRunsDueTask(t *testing.T) {
	requireRsync(t)
	root := t.TempDir()
	source := filepath.Join(root, "source")
	target := filepath.Join(root, "target")
	mustMkdir(t, source)
	mustMkdir(t, target)
	mustWrite(t, filepath.Join(source, "scheduled.txt"), "scheduled")
	manager, err := NewManager(filepath.Join(root, "state"), 1, nil, WithWorkDir(root))
	if err != nil {
		t.Fatal(err)
	}
	created, _, err := manager.Create(context.Background(), localTask(source, target))
	if err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Minute)
	if _, err := manager.store.updateTask(created.ID, func(task *Task) { task.NextRunAt = &past }); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager.Start(ctx)
	histories := waitForHistory(t, manager, created.ID, 1)
	if histories[0].Status != StatusSuccess {
		t.Fatalf("scheduled run failed: %+v", histories[0])
	}
	updated, err := manager.Get(created.ID)
	if err != nil || updated.NextRunAt == nil || !updated.NextRunAt.After(time.Now()) {
		t.Fatalf("next schedule was not advanced: task=%+v err=%v", updated, err)
	}
}

func TestConcurrencyLimitQueuesSecondRun(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	mustMkdir(t, bin)
	release := filepath.Join(root, "release-rsync")
	t.Setenv("BACKUP_TEST_RELEASE", release)
	defer func() { _ = os.WriteFile(release, []byte("release"), 0o600) }()
	script := filepath.Join(bin, "rsync")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nwhile [ ! -e \"$BACKUP_TEST_RELEASE\" ]; do sleep 0.05; done\necho 'Total transferred file size: 0 bytes'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	manager, err := NewManager(filepath.Join(root, "state"), 1, nil, WithWorkDir(root))
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, 2)
	for i := 0; i < 2; i++ {
		source := filepath.Join(root, "source-"+strconv.Itoa(i))
		target := filepath.Join(root, "target-"+strconv.Itoa(i))
		mustMkdir(t, source)
		mustMkdir(t, target)
		created, _, err := manager.Create(context.Background(), localTask(source, target))
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, created.ID)
	}
	for _, id := range ids {
		if _, err := manager.RunNow(id); err != nil {
			t.Fatal(err)
		}
	}
	deadline := time.Now().Add(2 * time.Minute)
	observedLimit := false
	for time.Now().Before(deadline) {
		statuses := []TaskStatus{}
		for _, id := range ids {
			task, _ := manager.Get(id)
			statuses = append(statuses, task.Status)
		}
		if (statuses[0] == StatusRunning && statuses[1] == StatusQueued) ||
			(statuses[1] == StatusRunning && statuses[0] == StatusQueued) {
			observedLimit = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !observedLimit {
		t.Fatal("did not observe one running task and one queued task at concurrency 1")
	}
	mustWrite(t, release, "release")
	for _, id := range ids {
		histories := waitForHistory(t, manager, id, 1)
		if histories[0].Status != StatusSuccess {
			t.Fatalf("queued task failed: %+v", histories[0])
		}
	}
}

func TestSameTargetTasksAreSerializedAndVersionNamespaced(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	target := filepath.Join(root, "target")
	targetAlias := filepath.Join(root, "target-alias")
	mustMkdir(t, bin)
	mustMkdir(t, target)
	if err := os.Symlink(target, targetAlias); err != nil {
		t.Fatal(err)
	}
	release := filepath.Join(root, "release-rsync")
	active := filepath.Join(root, "active-rsync")
	t.Setenv("BACKUP_TEST_RELEASE", release)
	t.Setenv("BACKUP_TEST_ACTIVE", active)
	defer func() { _ = os.WriteFile(release, []byte("release"), 0o600) }()
	script := filepath.Join(bin, "rsync")
	scriptBody := `#!/bin/sh
if ! mkdir "$BACKUP_TEST_ACTIVE" 2>/dev/null; then
  echo 'same target rsync overlapped' >&2
  exit 70
fi
trap 'rmdir "$BACKUP_TEST_ACTIVE"' EXIT
while [ ! -e "$BACKUP_TEST_RELEASE" ]; do sleep 0.02; done
echo 'Total transferred file size: 0 bytes'
`
	if err := os.WriteFile(script, []byte(scriptBody), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	manager, err := NewManager(filepath.Join(root, "state"), 2, nil, WithWorkDir(root))
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, 2)
	for i := 0; i < 2; i++ {
		source := filepath.Join(root, "source-"+strconv.Itoa(i))
		mustMkdir(t, source)
		taskTarget := target
		if i == 1 {
			taskTarget = targetAlias
		}
		created, _, err := manager.Create(context.Background(), localTask(source, taskTarget))
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, created.ID)
	}
	for _, id := range ids {
		if _, err := manager.RunNow(id); err != nil {
			t.Fatal(err)
		}
	}

	deadline := time.Now().Add(5 * time.Second)
	serialized := false
	for time.Now().Before(deadline) {
		first, _ := manager.Get(ids[0])
		second, _ := manager.Get(ids[1])
		if (first.Status == StatusRunning && second.Status == StatusQueued) ||
			(first.Status == StatusQueued && second.Status == StatusRunning) {
			serialized = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !serialized {
		t.Fatal("same-target tasks were not serialized")
	}
	mustWrite(t, release, "release")
	for _, id := range ids {
		histories := waitForHistory(t, manager, id, 1)
		if histories[0].Status != StatusSuccess {
			t.Fatalf("same-target task failed: %+v", histories[0])
		}
		versionPath := filepath.Join(target, id, histories[0].Version)
		if info, err := os.Stat(versionPath); err != nil || !info.IsDir() {
			t.Fatalf("version namespace missing at %s: info=%v err=%v", versionPath, info, err)
		}
	}
}

func TestApplyRetentionLeavesUnrelatedDirectories(t *testing.T) {
	target := t.TempDir()
	for _, name := range []string{"20260820T100000Z", "20260821T100000Z", "20260822T100000Z", "notes"} {
		mustMkdir(t, filepath.Join(target, name))
	}
	if err := applyRetention(context.Background(), Endpoint{Type: EndpointLocal, Path: target}, 2); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(target, "20260820T100000Z")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("old version still exists: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "notes")); err != nil {
		t.Fatalf("unrelated directory removed: %v", err)
	}
}

func TestParseRestoreChangesPreservesFilenameSpaces(t *testing.T) {
	output := ">f+++++++++| leading and trailing \n"
	changes := parseChanges(output)
	if len(changes) != 1 || changes[0] != " leading and trailing " {
		t.Fatalf("changes=%q, want filename spaces preserved", changes)
	}
}

func TestQueuedRestorePreviewChangePersistsRestorePreviewPhase(t *testing.T) {
	requireRsync(t)
	root := t.TempDir()
	source := filepath.Join(root, "source")
	target := filepath.Join(root, "target")
	destination := filepath.Join(root, "restore")
	for _, path := range []string{source, target, destination} {
		mustMkdir(t, path)
	}
	manager, err := NewManager(filepath.Join(root, "state"), 1, nil, WithWorkDir(root))
	if err != nil {
		t.Fatal(err)
	}
	created, _, err := manager.Create(context.Background(), localTask(source, target))
	if err != nil {
		t.Fatal(err)
	}
	version := "20260823T120000Z"
	versionDir := filepath.Join(target, created.ID, version)
	mustMkdir(t, versionDir)
	mustWrite(t, filepath.Join(versionDir, "config.txt"), "backup")
	request := RestoreRequest{Version: version, Destination: destination}
	preview, err := manager.PreviewRestore(context.Background(), created.ID, request)
	if err != nil {
		t.Fatal(err)
	}

	manager.slots <- struct{}{}
	restore, err := manager.Restore(context.Background(), created.ID, RestoreRequest{
		Version: version, Destination: destination, Confirm: true, PreviewToken: preview.Token,
	})
	if err != nil {
		<-manager.slots
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(destination, "config.txt"), "backup")
	<-manager.slots
	histories := waitForHistory(t, manager, created.ID, 1)
	if histories[0].ID != restore.ID || histories[0].Status != StatusFailed || histories[0].Phase != "restore-preview" {
		t.Fatalf("queued preview change attributed to wrong phase: %+v", histories[0])
	}
}

func TestCorruptStateIsBackedUpAndManagerStartsEmptyWithWarning(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "state")
	mustMkdir(t, dataDir)
	mustWrite(t, filepath.Join(dataDir, "state.json"), "{not-json")
	core, observed := observer.New(zap.WarnLevel)

	manager, err := NewManager(dataDir, 1, zap.New(core), WithWorkDir(root))
	if err != nil {
		t.Fatalf("manager should recover corrupt state: %v", err)
	}
	if tasks := manager.List(); len(tasks) != 0 {
		t.Fatalf("recovered manager should start empty: %+v", tasks)
	}
	backups, err := filepath.Glob(filepath.Join(dataDir, "state.json.corrupt-*"))
	if err != nil || len(backups) != 1 {
		t.Fatalf("corrupt state backup=%v err=%v", backups, err)
	}
	if observed.Len() == 0 || !strings.Contains(observed.All()[0].Message, "corrupt") {
		t.Fatalf("corrupt state warning missing: %+v", observed.All())
	}
}

func TestRecoverInterruptedHistorySetsFinishedAt(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "state")
	first, err := NewManager(dataDir, 1, nil, WithWorkDir(root))
	if err != nil {
		t.Fatal(err)
	}
	task := Task{ID: "task-1", Name: "interrupted", Status: StatusRunning}
	history := History{
		ID: "history-1", TaskID: task.ID, Kind: RunBackup, Status: StatusQueued,
		Phase: "queued", StartedAt: time.Now().Add(-time.Minute),
	}
	if err := first.store.putTask(task); err != nil {
		t.Fatal(err)
	}
	if err := first.store.putHistory(history); err != nil {
		t.Fatal(err)
	}

	before := time.Now()
	second, err := NewManager(dataDir, 1, nil, WithWorkDir(root))
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := second.History(task.ID, history.ID)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Status != StatusFailed || recovered.Phase != "interrupted" || recovered.FinishedAt == nil {
		t.Fatalf("interrupted history not completed: %+v", recovered)
	}
	if recovered.FinishedAt.Before(before) || recovered.FinishedAt.After(time.Now()) {
		t.Fatalf("unexpected FinishedAt: %s", recovered.FinishedAt)
	}
}

func assertFailedCheck(t *testing.T, result PreflightResult, name string) {
	t.Helper()
	if result.OK {
		t.Fatalf("preflight unexpectedly passed: %+v", result)
	}
	for _, check := range result.Checks {
		if check.Name == name && !check.OK {
			return
		}
	}
	t.Fatalf("failed check %q missing: %+v", name, result)
}

func failedCheckMessage(result PreflightResult, name string) string {
	for _, check := range result.Checks {
		if check.Name == name && !check.OK {
			return check.Message
		}
	}
	return ""
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o750); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o640); err != nil {
		t.Fatal(err)
	}
}

func testPolicy(t *testing.T, workDir string) pathPolicy {
	t.Helper()
	policy, err := newPathPolicy(workDir, filepath.Join(workDir, ".backup-state"), nil)
	if err != nil {
		t.Fatal(err)
	}
	return policy
}

func requireRsync(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("rsync"); err != nil {
		t.Fatalf("rsync is required for backup integration tests: %v", err)
	}
}
