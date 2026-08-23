package backup

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
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

	manager, err := NewManager(data, 1, nil)
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
	firstInfo, err := os.Stat(filepath.Join(target, firstVersion, "stable.txt"))
	if err != nil {
		t.Fatal(err)
	}

	mustWrite(t, filepath.Join(source, "second.txt"), "second")
	if _, err := manager.RunNow(created.ID); err != nil {
		t.Fatal(err)
	}
	secondHistory := waitForHistory(t, manager, created.ID, 2)
	secondInfo, err := os.Stat(filepath.Join(target, secondHistory[0].Version, "stable.txt"))
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
	if _, err := os.Stat(filepath.Join(target, firstVersion)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("oldest version was not removed: %v", err)
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

	manager, err := NewManager(filepath.Join(root, "state"), 1, nil)
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

func TestPreflightRejectsUnmountedMountTarget(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	target := filepath.Join(root, "looks-mounted")
	mustMkdir(t, source)
	mustMkdir(t, target)
	task := localTask(source, target)
	task.Target.Type = EndpointMount
	result := preflight(context.Background(), task)
	assertFailedCheck(t, result, "目标可达性与写入权限")
}

func TestPreflightRejectsLoopUnwritableAndUnreachableSSH(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	targetInside := filepath.Join(source, "backup")
	unwritable := filepath.Join(root, "unwritable")
	for _, path := range []string{source, targetInside, unwritable} {
		mustMkdir(t, path)
	}

	loop := preflight(context.Background(), localTask(source, targetInside))
	assertFailedCheck(t, loop, "路径循环")

	if err := os.Chmod(unwritable, 0o500); err != nil {
		t.Fatal(err)
	}
	permissions := preflight(context.Background(), localTask(source, unwritable))
	assertFailedCheck(t, permissions, "目标可达性与写入权限")

	remote := localTask(source, targetInside)
	remote.Target = Endpoint{Type: EndpointSSH, Host: "backup@does-not-exist.invalid", Path: "/backup", Port: 22}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	unreachable := preflight(ctx, remote)
	assertFailedCheck(t, unreachable, "目标可达性与写入权限")
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

	manager, err := NewManager(filepath.Join(root, "state"), 1, nil)
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
	first, err := NewManager(data, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	created, _, err := first.Create(context.Background(), localTask(source, target))
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewManager(data, 1, nil)
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
	manager, err := NewManager(filepath.Join(root, "state"), 1, nil)
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
	manager, err := NewManager(filepath.Join(root, "state"), 1, nil)
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

func requireRsync(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("rsync integration test requires Unix")
	}
	if _, err := exec.LookPath("rsync"); err != nil {
		t.Skip("rsync is not installed")
	}
}
