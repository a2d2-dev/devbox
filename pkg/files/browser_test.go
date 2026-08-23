package files

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestBrowser(t *testing.T) (*Browser, string) {
	t.Helper()
	base := t.TempDir()
	root := filepath.Join(base, "root")
	apps := filepath.Join(base, "apps")
	require.NoError(t, os.MkdirAll(root, 0o755))
	require.NoError(t, os.MkdirAll(apps, 0o755))
	browser := NewBrowser(Config{RootDir: root, AppsDir: apps, StateDir: filepath.Join(base, "state"), MountsFile: filepath.Join(base, "mounts")})
	return browser, root
}

func requireCode(t *testing.T, expected string, err error) {
	t.Helper()
	require.Error(t, err)
	assert.Equal(t, expected, ErrorCode(err), err.Error())
}

func TestPathValidationRejectsTraversalAbsoluteAndSymlinkEscape(t *testing.T) {
	browser, root := newTestBrowser(t)
	outside := filepath.Join(filepath.Dir(root), "outside")
	require.NoError(t, os.MkdirAll(outside, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o644))
	require.NoError(t, os.Symlink(outside, filepath.Join(root, "escape")))
	require.NoError(t, os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(root, "secret-link")))

	for name, path := range map[string]string{
		"parent traversal":   "../outside",
		"nested traversal":   "folder/../../outside",
		"absolute injection": filepath.Join(outside, "secret.txt"),
		"windows separator":  `..\outside`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := browser.ListSource("my", path, "name", "asc")
			requireCode(t, "PATH_FORBIDDEN", err)
		})
	}

	_, err := browser.ListSource("my", "escape", "name", "asc")
	requireCode(t, "PATH_FORBIDDEN", err)
	_, err = browser.ResolveDownload("my", "secret-link")
	requireCode(t, "PATH_FORBIDDEN", err)
	_, err = browser.SaveSource("my", "escape", "written.txt", []byte("blocked"))
	requireCode(t, "PATH_FORBIDDEN", err)
	assert.NoFileExists(t, filepath.Join(outside, "written.txt"))

	for _, reserved := range []string{".trash", ".trash/index.json", ".devbox-files/state.json"} {
		_, err := browser.ListSource("my", reserved, "name", "asc")
		requireCode(t, "PATH_FORBIDDEN", err)
	}
}

func TestListSearchSortAndLimits(t *testing.T) {
	browser, root := newTestBrowser(t)
	require.NoError(t, os.MkdirAll(filepath.Join(root, "nested", "deep"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "zeta.log"), []byte("123456"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "alpha.txt"), []byte("1"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "nested", "deep", "alpha-note.md"), []byte("x"), 0o644))

	entries, err := browser.ListSource("my", "", "size", "desc")
	require.NoError(t, err)
	require.Len(t, entries, 3)
	assert.True(t, entries[0].IsDir, "directories remain grouped before files")
	assert.Equal(t, "zeta.log", entries[1].Name)

	results, err := browser.Search("my", "", "alpha")
	require.NoError(t, err)
	require.Len(t, results, 2)
	assert.ElementsMatch(t, []string{"alpha.txt", "nested/deep/alpha-note.md"}, []string{results[0].Path, results[1].Path})
}

func TestSourcesExposeConsistentCapabilities(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "root")
	apps := filepath.Join(base, "apps")
	remote := filepath.Join(root, "remote")
	for _, dir := range []string{root, apps, remote} {
		require.NoError(t, os.MkdirAll(dir, 0o755))
	}
	mounts := filepath.Join(base, "mounts")
	require.NoError(t, os.WriteFile(mounts, []byte("server:/share "+remote+" nfs rw 0 0\n"), 0o600))
	browser := NewBrowser(Config{RootDir: root, AppsDir: apps, StateDir: filepath.Join(base, "state"), MountsFile: mounts})

	sources := browser.Sources()
	byKind := map[string]Source{}
	for _, source := range sources {
		byKind[source.Kind] = source
	}
	require.True(t, byKind["personal"].Capabilities.Trash)
	require.True(t, byKind["applications"].Capabilities.Delete)
	require.False(t, byKind["applications"].Capabilities.Trash)
	require.True(t, byKind["network"].Capabilities.Download)
	require.False(t, byKind["network"].Capabilities.Upload)
	require.False(t, byKind["network"].Capabilities.Delete)
	rootEntries, err := browser.ListSource("my", "", "name", "asc")
	require.NoError(t, err)
	assert.NotContains(t, namesOf(rootEntries), "remote", "nested mounts must not inherit parent source capabilities")
	_, err = browser.ListSource("my", "remote", "name", "asc")
	requireCode(t, "PATH_FORBIDDEN", err)

	require.NoError(t, os.Mkdir(filepath.Join(apps, "demo"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(apps, "demo", ".env"), []byte("PASSWORD=secret"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(apps, "demo", "compose.yaml"), []byte("services: {}"), 0o644))
	entries, err := browser.ListSource("apps", "demo", "name", "asc")
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "compose.yaml", entries[0].Name)
	_, err = browser.ResolveDownload("apps", "demo/.env")
	requireCode(t, "PATH_FORBIDDEN", err)
}

func namesOf(entries []FileEntry) []string {
	result := make([]string, 0, len(entries))
	for _, entry := range entries {
		result = append(result, entry.Name)
	}
	return result
}

func TestTrashDeleteRestoreAndDangerousAudit(t *testing.T) {
	browser, root := newTestBrowser(t)
	original := filepath.Join(root, "project", "notes.txt")
	require.NoError(t, os.MkdirAll(filepath.Dir(original), 0o755))
	require.NoError(t, os.WriteFile(original, []byte("recover me"), 0o644))

	require.NoError(t, browser.Delete("my", "project/notes.txt", false))
	assert.NoFileExists(t, original)
	trash, err := browser.Trash("")
	require.NoError(t, err)
	require.Len(t, trash, 1)
	assert.Equal(t, "project/notes.txt", trash[0].OriginalPath)
	assert.FileExists(t, filepath.Join(root, ".trash", "files", trash[0].ID))

	require.NoError(t, os.Remove(filepath.Join(root, "project")))
	require.NoError(t, browser.RestoreTrash(trash[0].ID))
	data, err := os.ReadFile(original)
	require.NoError(t, err)
	assert.Equal(t, "recover me", string(data))
	trash, err = browser.Trash("")
	require.NoError(t, err)
	assert.Empty(t, trash)

	require.NoError(t, browser.Delete("my", "project/notes.txt", true))
	assert.NoFileExists(t, original)
	audit, err := os.ReadFile(filepath.Join(browser.stateDir, "audit.jsonl"))
	require.NoError(t, err)
	assert.Contains(t, string(audit), `"action":"files.permanent_delete"`)
	assert.Contains(t, string(audit), `"path":"project/notes.txt"`)
}

func TestTrashRestoreRefusesOccupiedOriginalPath(t *testing.T) {
	browser, root := newTestBrowser(t)
	path := filepath.Join(root, "same.txt")
	require.NoError(t, os.WriteFile(path, []byte("old"), 0o644))
	require.NoError(t, browser.Delete("my", "same.txt", false))
	trash, err := browser.Trash("")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, []byte("new"), 0o644))
	requireCode(t, "CONFLICT", browser.RestoreTrash(trash[0].ID))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "new", string(data))
}

func TestShareTokenLifecycleAndPersistence(t *testing.T) {
	browser, root := newTestBrowser(t)
	require.NoError(t, os.WriteFile(filepath.Join(root, "artifact.zip"), []byte("payload"), 0o644))
	clock := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	browser.now = func() time.Time { return clock }

	created, err := browser.CreateShare("my", "artifact.zip", time.Hour)
	require.NoError(t, err)
	assert.Len(t, created.Token, 43)
	assert.NotContains(t, created.Token, "/")
	full, name, err := browser.ResolveShare(created.Token)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(root, "artifact.zip"), full)
	assert.Equal(t, "artifact.zip", name)

	stateBytes, err := os.ReadFile(filepath.Join(browser.stateDir, "state.json"))
	require.NoError(t, err)
	assert.NotContains(t, string(stateBytes), created.Token, "plaintext share token must never be persisted")
	assert.Contains(t, string(stateBytes), `"tokenHash"`)

	reloaded := NewBrowser(Config{RootDir: root, AppsDir: browser.appsDir, StateDir: browser.stateDir, MountsFile: browser.mountsFile})
	reloaded.now = func() time.Time { return clock }
	_, _, err = reloaded.ResolveShare(created.Token)
	require.NoError(t, err)

	clock = clock.Add(2 * time.Hour)
	_, _, err = browser.ResolveShare(created.Token)
	requireCode(t, "SHARE_EXPIRED", err)
	require.NoError(t, browser.RevokeShare(created.ID))
	_, _, err = browser.ResolveShare(created.Token)
	requireCode(t, "SHARE_NOT_FOUND", err)
}

func TestFavoriteIsIdempotentAndPersistent(t *testing.T) {
	browser, root := newTestBrowser(t)
	require.NoError(t, os.Mkdir(filepath.Join(root, "docs"), 0o755))
	require.NoError(t, browser.SetFavorite("my", "docs", true))
	require.NoError(t, browser.SetFavorite("my", "docs", true))
	favorites, err := browser.Favorites()
	require.NoError(t, err)
	require.Len(t, favorites, 1)
	require.NoError(t, browser.SetFavorite("my", "docs", false))
	favorites, err = browser.Favorites()
	require.NoError(t, err)
	assert.Empty(t, favorites)
}

func TestShareCreationRejectsDirectoryAndTraversal(t *testing.T) {
	browser, _ := newTestBrowser(t)
	_, err := browser.CreateShare("my", "", time.Hour)
	requireCode(t, "NOT_FILE", err)
	_, err = browser.CreateShare("my", "../secret", time.Hour)
	requireCode(t, "PATH_FORBIDDEN", err)
}

func TestTransferRejectsDirectoryIntoOwnSubtree(t *testing.T) {
	browser, root := newTestBrowser(t)
	require.NoError(t, os.MkdirAll(filepath.Join(root, "tree", "child"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "tree", "value.txt"), []byte("x"), 0o644))
	requireCode(t, "PATH_FORBIDDEN", browser.Transfer("my", "tree", "tree/child", true))
	assert.NoDirExists(t, filepath.Join(root, "tree", "child", "tree"))
}

func TestStateNeverContainsRawTokenLikeMaterial(t *testing.T) {
	browser, root := newTestBrowser(t)
	require.NoError(t, os.WriteFile(filepath.Join(root, "x.txt"), []byte("x"), 0o644))
	share, err := browser.CreateShare("my", "x.txt", 0)
	require.NoError(t, err)
	state, err := os.ReadFile(filepath.Join(browser.stateDir, "state.json"))
	require.NoError(t, err)
	assert.False(t, strings.Contains(string(state), share.Token))
}
