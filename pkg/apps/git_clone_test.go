package apps

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// git 集成测试需要系统 git；缺失则跳过（CI 可能无 git）。
func requireGit(t *testing.T) string {
	t.Helper()
	bin, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not installed; skipping git clone integration test")
	}
	return bin
}

// makeTempRepo 建一个本地 git 仓库（默认分支 main），写入 catalog.json + compose 文件并提交。
func makeTempRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	requireGit(t)
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_TERMINAL_PROMPT=0", "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := cmd.CombinedOutput()
		require.NoErrorf(t, err, "git %s: %s", strings.Join(args, " "), out)
	}
	run("init", "-b", "main")
	for path, content := range files {
		full := filepath.Join(dir, path)
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(content), 0o644))
	}
	run("add", "-A")
	run("commit", "-m", "init")
	return dir
}

// 直接构造 gitCatalog（绕过 NewGitCatalog 的 URL 校验），用本地仓库路径测试 clone 引擎。
func newGitCatalogForTest(t *testing.T, url, ref, sub string) *gitCatalog {
	t.Helper()
	dir := t.TempDir()
	cacheDir := filepath.Join(dir, "cache")
	return &gitCatalog{
		id:       "test-git",
		name:     "Test Git",
		gitBin:   "git",
		cacheDir: cacheDir,
		source:   CatalogSource{URL: url, Ref: ref, Path: sub},
	}
}

func TestGitCatalog_RefreshAndGetVersion(t *testing.T) {
	repo := makeTempRepo(t, map[string]string{
		"catalog.json": `{"apiVersion":"devbox/v1","name":"GitCat","apps":[
			{"id":"app","name":"App","version":"1.2.0","compose":"deploy/compose.yaml"}]}`,
		"deploy/compose.yaml": "services:\n  web:\n    image: nginx:1.27\n",
	})
	gc := newGitCatalogForTest(t, repo, "main", "")

	require.NoError(t, gc.Refresh(context.Background()))

	snap := gc.Snapshot()
	assert.Equal(t, CatalogStateOK, snap.Status.State)
	require.Len(t, snap.Apps, 1)
	assert.Equal(t, "app", snap.Apps[0].ID)
	assert.Equal(t, "1.2.0", snap.Apps[0].Version)

	// GetVersion 从 clone 目录安全读取 compose。
	v, err := gc.GetVersion(context.Background(), "app", "1.2.0")
	require.NoError(t, err)
	assert.Contains(t, v.ComposeTemplate, "nginx:1.27")
}

// sub 目录场景：catalog.json 与 compose 均在 <repo>/catalogs/ 下。
func TestGitCatalog_Subdir(t *testing.T) {
	repo := makeTempRepo(t, map[string]string{
		"catalogs/catalog.json": `{"apiVersion":"devbox/v1","apps":[
			{"id":"app","version":"1.0.0","compose":"compose.yaml"}]}`,
		"catalogs/compose.yaml": "services:\n  web:\n    image: redis:7\n",
	})
	gc := newGitCatalogForTest(t, repo, "main", "catalogs")
	require.NoError(t, gc.Refresh(context.Background()))
	v, err := gc.GetVersion(context.Background(), "app", "1.0.0")
	require.NoError(t, err)
	assert.Contains(t, v.ComposeTemplate, "redis:7")
}

// clone 失败（仓库不存在）→ 保留上次可信缓存 + 状态 error。
func TestGitCatalog_FailureKeepsLastGoodCache(t *testing.T) {
	repo := makeTempRepo(t, map[string]string{
		"catalog.json": `{"apiVersion":"devbox/v1","apps":[{"id":"app","version":"1.0.0","composeTemplate":"services:\n  web:\n    image: nginx:1.27\n"}]}`,
	})
	gc := newGitCatalogForTest(t, repo, "main", "")
	require.NoError(t, gc.Refresh(context.Background()))
	require.Len(t, gc.Snapshot().Apps, 1)

	// 指向不存在的仓库 → 刷新失败。
	gc.source.URL = "/nonexistent/repo/path-xyz-123"
	err := gc.Refresh(context.Background())
	require.Error(t, err)

	// 上次缓存的 manifest 与 clone 目录仍在；ListApps 仍可用。
	snap := gc.Snapshot()
	assert.Equal(t, CatalogStateError, snap.Status.State)
	assert.Len(t, snap.Apps, 1, "last good cache preserved")
	// 内联 compose 的 GetVersion 仍可服务（不依赖 clone 目录重读）。
	v, err := gc.GetVersion(context.Background(), "app", "1.0.0")
	require.NoError(t, err)
	assert.Contains(t, v.ComposeTemplate, "nginx")
}

func TestGitCatalog_RestartOfflineLoadsLastGoodClone(t *testing.T) {
	repo := makeTempRepo(t, map[string]string{
		"catalog.json": "{\"apiVersion\":\"devbox/v1\",\"apps\":[{\"id\":\"demo-app\",\"version\":\"1.0.0\",\"compose\":\"compose.yaml\"}]}",
		"compose.yaml": "services:\n  web:\n    image: nginx:1.27\n",
	})
	root := t.TempDir()
	cacheDir := filepath.Join(root, "cache")
	first := &gitCatalog{id: "test-git", name: "Test", gitBin: "git", cacheDir: cacheDir, source: CatalogSource{URL: repo, Ref: "main"}}
	require.NoError(t, first.Refresh(context.Background()))

	restarted := &gitCatalog{id: "test-git", name: "Test", gitBin: "git", cacheDir: cacheDir, source: CatalogSource{URL: "/missing/repo", Ref: "main"}}
	require.NoError(t, restarted.loadCachedManifest())
	require.Error(t, restarted.Refresh(context.Background()))
	assert.Len(t, restarted.Snapshot().Apps, 1)
	ver, err := restarted.GetVersion(context.Background(), "demo-app", "1.0.0")
	require.NoError(t, err)
	assert.Contains(t, ver.ComposeTemplate, "nginx:1.27")
}

// token 抹除：clone 输出含 token 时被 scrub（这里验证 token 不出现在 lastErr）。
func TestGitCatalog_TokenNotInError(t *testing.T) {
	token := "supersecret-token-xyz"
	gc := newGitCatalogForTest(t, "/nonexistent/repo/path-abc-999", "main", "")
	gc.source.Token = token
	err := gc.Refresh(context.Background())
	require.Error(t, err)
	// Snapshot 的 message（来自 lastErr）不得含 token 明文。
	snap := gc.Snapshot()
	assert.NotContains(t, snap.Status.Message, token)
}

// 仓内 compose 为逃逸 symlink → GetVersion 拒绝读取。
func TestGitCatalog_SymlinkComposeRejected(t *testing.T) {
	// 先在仓外放一个敏感文件，再在仓内建指向它的 symlink 并提交。
	secret := filepath.Join(t.TempDir(), "secret.txt")
	require.NoError(t, os.WriteFile(secret, []byte("TOPSECRET"), 0o600))

	repo := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(repo, ".git"), 0o755)) // 占位让 git init 复用
	files := map[string]string{
		"catalog.json": `{"apiVersion":"devbox/v1","apps":[{"id":"app","version":"1.0.0","compose":"link.yaml"}]}`,
	}
	for p, c := range files {
		require.NoError(t, os.WriteFile(filepath.Join(repo, p), []byte(c), 0o644))
	}
	// 建仓内 symlink 指向仓外。
	require.NoError(t, os.Symlink(secret, filepath.Join(repo, "link.yaml")))

	requireGit(t)
	cmd := exec.Command("git", "init", "-b", "main")
	cmd.Dir = repo
	require.NoError(t, cmd.Run())
	for _, a := range [][]string{
		{"config", "user.name", "t"}, {"config", "user.email", "t@t"},
		{"add", "-A"}, {"commit", "-m", "init"},
	} {
		c := exec.Command("git", a...)
		c.Dir = repo
		out, err := c.CombinedOutput()
		require.NoErrorf(t, err, "git %v: %s", a, out)
	}

	gc := newGitCatalogForTest(t, repo, "main", "")
	require.NoError(t, gc.Refresh(context.Background()))
	_, err := gc.GetVersion(context.Background(), "app", "1.0.0")
	require.Error(t, err, "compose symlink escaping repo must be rejected")
}
