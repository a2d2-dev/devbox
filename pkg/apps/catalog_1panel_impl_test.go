package apps

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// 写一个 1Panel 目录 fixture（不依赖 git）。pathSet 由调用方提供以模拟 ls-tree（含 scripts 探测）。
func writeOnePanelFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	must := func(p, c string) {
		full := filepath.Join(root, p)
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(c), 0o644))
	}
	must("data.yaml", "name: 1Panel\ntitle: 测试商店\n")
	must("apps/adguardhome/data.yml", "name: AdGuardHome\nadditionalProperties:\n  key: adguardhome\n  name: AdGuardHome\n  shortDescZh: DNS 拦截\n  tags: [Security]\n")
	must("apps/adguardhome/0.107.78/data.yml", "additionalProperties:\n  formFields:\n    - {envKey: PANEL_APP_PORT_HTTP, type: number, default: 3000, required: true}\n    - {envKey: PANEL_REDIS_ROOT_PASSWORD, type: password, default: upstream-leaked-pw}\n")
	must("apps/adguardhome/0.107.78/docker-compose.yml", "services:\n  web:\n    container_name: ${CONTAINER_NAME}\n    image: adguard/adguardhome:v0.107.78\n    networks: [1panel-network]\n    ports:\n      - ${PANEL_APP_PORT_HTTP}:3000/tcp\n    environment:\n      PW: ${PANEL_REDIS_ROOT_PASSWORD}\nnetworks:\n  1panel-network: {external: true}\n")
	// 非版本辅助目录不能进入版本列表。
	must("apps/adguardhome/docs/readme.md", "not a version")
	// 2fauth 版本带 scripts/init.sh（经 pathSet 模拟）→ 应判不可安装。
	must("apps/2fauth/data.yml", "name: 2FAuth\nadditionalProperties:\n  key: 2fauth\n  name: 2FAuth\n  shortDescZh: 2FA\n")
	must("apps/2fauth/8.0.1/data.yml", "additionalProperties:\n  formFields:\n    - {envKey: PANEL_APP_PORT_HTTP, type: number, default: 8000, required: true}\n")
	must("apps/2fauth/8.0.1/docker-compose.yml", "services:\n  x: {image: 2fauth:8.0.1, networks: [1panel-network]}\nnetworks:\n  1panel-network: {external: true}\n")
	return root
}

func onePanelFixturePaths() []string {
	return []string{
		"data.yaml",
		"apps/adguardhome/data.yml",
		"apps/adguardhome/0.107.78/data.yml",
		"apps/adguardhome/0.107.78/docker-compose.yml",
		"apps/2fauth/data.yml",
		"apps/2fauth/8.0.1/data.yml",
		"apps/2fauth/8.0.1/docker-compose.yml",
		"apps/2fauth/8.0.1/scripts/init.sh",
	}
}

func TestParseOnePanelRepo_FromFixture(t *testing.T) {
	root := writeOnePanelFixture(t)
	apps, storeName, err := parseOnePanelRepo(root, onePanelFixturePaths())
	require.NoError(t, err)
	require.Equal(t, "1Panel", storeName)
	require.Len(t, apps, 2)

	adg := apps["adguardhome"]
	require.NotNil(t, adg)
	require.Len(t, adg.versions, 1)
	require.Equal(t, "0.107.78", adg.versions[0].version)
	require.Contains(t, adg.versions[0].compose, "adguard/adguardhome:v0.107.78")
	require.False(t, adg.versions[0].hasScript)

	two := apps["2fauth"]
	require.NotNil(t, two)
	require.True(t, two.versions[0].hasScript, "2fauth 经 pathSet 探测到 scripts/init.sh")
}

func TestDetectOnePanel(t *testing.T) {
	root := writeOnePanelFixture(t)
	require.True(t, detectOnePanel(root, onePanelFixturePaths()))

	// 缺 root data.yaml → 不判定为受支持格式（pooneyy 类 fork）。
	require.NoError(t, os.Remove(filepath.Join(root, "data.yaml")))
	require.False(t, detectOnePanel(root, onePanelFixturePaths()))
}

// GetVersion：正常版本可安装、compose 已收敛、password 保留 ${} 且默认值不泄露。
func TestOnePanelCatalog_GetVersion_Normal(t *testing.T) {
	root := writeOnePanelFixture(t)
	apps, storeName, err := parseOnePanelRepo(root, onePanelFixturePaths())
	require.NoError(t, err)
	c := &onepanelCatalog{id: "src-1", name: "Test", storeName: storeName, apps: apps}

	ver, err := c.GetVersion(context.Background(), "adguardhome", "")
	require.NoError(t, err)
	require.True(t, ver.Installable)
	require.Equal(t, "0.107.78", ver.Version)
	// compose 收敛：container_name 剥离、网络 project-managed、端口转 Go 模板、密码保留 ${}。
	require.NotContains(t, ver.ComposeTemplate, "container_name")
	require.NotContains(t, ver.ComposeTemplate, "CONTAINER_NAME")
	require.Contains(t, ver.ComposeTemplate, "{{ .PANEL_APP_PORT_HTTP }}")
	require.Contains(t, ver.ComposeTemplate, "${PANEL_REDIS_ROOT_PASSWORD}")
	require.NotContains(t, ver.ComposeTemplate, "upstream-leaked-pw")

	// DefaultValues 不含 password key；响应 JSON 不含上游明文密码默认值。
	_, hasPW := ver.DefaultValues["PANEL_REDIS_ROOT_PASSWORD"]
	require.False(t, hasPW)
	body, _ := json.Marshal(ver)
	require.NotContains(t, string(body), "upstream-leaked-pw")
}

// 带 scripts/init.sh 的版本 → 不可安装，原因明确（不伪装）。
func TestOnePanelCatalog_GetVersion_ScriptsNotInstallable(t *testing.T) {
	root := writeOnePanelFixture(t)
	apps, _, _ := parseOnePanelRepo(root, onePanelFixturePaths())
	c := &onepanelCatalog{id: "src-1", name: "Test", apps: apps}

	ver, err := c.GetVersion(context.Background(), "2fauth", "")
	require.NoError(t, err)
	require.False(t, ver.Installable)
	require.Contains(t, ver.NotInstallableReason, "scripts")
}

// Snapshot：列表含应用，scripts 版本的 app 标 NotInstallableReason。
func TestOnePanelCatalog_Snapshot(t *testing.T) {
	root := writeOnePanelFixture(t)
	apps, storeName, _ := parseOnePanelRepo(root, onePanelFixturePaths())
	c := &onepanelCatalog{id: "src-1", name: "Test", storeName: storeName, apps: apps}
	snap := c.Snapshot()
	require.Equal(t, "1panel", snap.Kind)
	require.Equal(t, CatalogStateOK, snap.Status.State)
	require.Len(t, snap.Apps, 2)

	byID := map[string]StoreApp{}
	for _, a := range snap.Apps {
		byID[a.ID] = a
	}
	require.Equal(t, "0.107.78", byID["adguardhome"].Version)
	require.True(t, byID["adguardhome"].Installable)
	// 2fauth 最新版本带 scripts → 列表层即标不可安装 + 原因。
	require.False(t, byID["2fauth"].Installable)
	require.Contains(t, byID["2fauth"].NotInstallableReason, "scripts")
}

// 残留变量校验：未声明 ${VAR}、modifier ${VAR:-x}、bare $IDENT 检出；
// 声明密码 ${PW} 允许、$$ 转义不计；结果排序。
func TestResidualOnePanelVars(t *testing.T) {
	pw := map[string]bool{"PW": true}
	compose := "a: ${FOO}\nb: ${BAR:-x}\nc: $BAZ\nd: $$cash\ne: ${PW}\nf: ${PW:-leak}"
	out := residualOnePanelVars(compose, pw)
	require.Contains(t, out, "FOO")   // 未声明 brace
	require.Contains(t, out, "BAR!")  // modifier
	require.Contains(t, out, "BAZ")   // bare
	require.Contains(t, out, "PW!")   // ${PW:-leak} modifier（即使 PW 声明也拒）
	require.NotContains(t, out, "PW") // 纯 PW（声明密码）不应作为残留
	require.True(t, sort.StringsAreSorted(out), "残留变量应排序")
}

// 未声明变量版本的解析数据（block YAML，确保 ${UNKNOWN_VAR} 是合法标量、真正走 validator）。
func undeclaredVarApps() map[string]*onepanelParsedApp {
	return map[string]*onepanelParsedApp{
		"appx": {key: "appx", versions: []onepanelParsedVersion{{
			version: "1.0.0",
			fields:  []onePanelFormField{{EnvKey: "PANEL_APP_PORT_HTTP", Type: "number", Default: 8080, Required: true}},
			compose: "services:\n" +
				"  x:\n" +
				"    image: a:1.0.0\n" +
				"    environment:\n" +
				"      SECRET: ${UNKNOWN_VAR}\n" +
				"    networks:\n" +
				"      - 1panel-network\n" +
				"networks:\n" +
				"  1panel-network:\n" +
				"    external: true\n",
		}}},
	}
}

// GetVersion：compose 引用未声明变量 → 不可安装，原因列出变量名。
func TestOnePanelCatalog_GetVersion_UndeclaredVarNotInstallable(t *testing.T) {
	c := &onepanelCatalog{id: "s1", name: "T", apps: undeclaredVarApps()}
	ver, err := c.GetVersion(context.Background(), "appx", "")
	require.NoError(t, err)
	require.False(t, ver.Installable)
	require.Contains(t, ver.NotInstallableReason, "UNKNOWN_VAR")
}

// Snapshot 与 GetVersion 可安装性一致：未声明变量版本在列表层即不可安装。
func TestOnePanelCatalog_Snapshot_UndeclaredVarConsistent(t *testing.T) {
	c := &onepanelCatalog{id: "s1", name: "T", apps: undeclaredVarApps()}
	snap := c.Snapshot()
	require.Len(t, snap.Apps, 1)
	require.False(t, snap.Apps[0].Installable, "列表层即不可安装（与 GetVersion 一致）")
	require.Contains(t, snap.Apps[0].NotInstallableReason, "UNKNOWN_VAR")
}

// CatalogLocalAppID：underscore/超长/全非法归一化为合法 ID；多来源同 key 与 slug 碰撞不冲突。
func TestCatalogLocalAppID(t *testing.T) {
	check := func(upstream, source string) string {
		id := CatalogLocalAppID(upstream, source)
		require.NoErrorf(t, ValidateAppID(id), "upstream=%q id=%q", upstream, id)
		return id
	}
	// underscore（act_runner）→ 归一化为 - 且合法。
	id := check("act_runner", "src-1")
	require.Contains(t, id, "act-runner")
	// 超长 upstreamKey → 最终 ≤63。
	idLong := check(strings.Repeat("a", 200), "src-1")
	require.LessOrEqual(t, len(idLong), 63)
	// 全非法字符（___）→ 兜底 app- 前缀。
	idBad := check("___", "src-1")
	require.True(t, strings.HasPrefix(idBad, "app-"))
	// 两来源同 upstreamKey → 不同 ID（来源隔离）。
	require.NotEqual(t, CatalogLocalAppID("adguardhome", "src-1"), CatalogLocalAppID("adguardhome", "src-2"))
	// slug 碰撞：a_b 与 a-b 同来源 → slug 同为 a-b，但 hash（含原始 upstreamKey）不同 → 不碰撞。
	require.NotEqual(t, CatalogLocalAppID("a_b", "src-1"), CatalogLocalAppID("a-b", "src-1"))
}

// gitSparseClone：服务端忽略 --filter（exit 0 但回退完整 clone）→ 失败（注入 fake runner）。
func TestGitSparseClone_FilterIgnoredFails(t *testing.T) {
	fake := func(ctx context.Context, gitBin string, args []string, token string, stdin []byte) (string, error) {
		// clone 步骤（含 "clone"）模拟 filter-ignored 警告 + exit 0。
		for _, a := range args {
			if a == "clone" {
				return "warning: filtering not recognized by server, ignoring\n", nil
			}
		}
		return "", nil
	}
	err := gitSparseClone(context.Background(), "git", "https://example.com/x.git", "", t.TempDir(), "", fake)
	require.Error(t, err)
	require.Contains(t, err.Error(), "不支持 partial clone")
}

// gitSparseClone：正常输出 → 通过。
func TestGitSparseClone_FakeRunnerSuccess(t *testing.T) {
	fake := func(ctx context.Context, gitBin string, args []string, token string, stdin []byte) (string, error) {
		return "", nil
	}
	require.NoError(t, gitSparseClone(context.Background(), "git", "https://example.com/x.git", "", t.TempDir(), "", fake))
}

// 原始 compose 可变镜像标签预检：latest/main/tagless 检出；版本化不报。
func TestOnePanelComposeMutableImages(t *testing.T) {
	raw := "services:\n  a:\n    image: nginx:latest\n  b:\n    image: redis:7\n  c:\n    image: foo:main\n  d:\n    image: bar\n"
	bad := onePanelComposeMutableImages(raw)
	require.Contains(t, bad, "nginx:latest")
	require.Contains(t, bad, "foo:main")
	require.Contains(t, bad, "bar") // tagless → 默认 latest
	require.NotContains(t, bad, "redis:7")
}

// 可变镜像版本：GetVersion 与 Snapshot 均不可安装，原因含镜像。
func TestOnePanelCatalog_MutableImageNotInstallable(t *testing.T) {
	c := &onepanelCatalog{id: "s1", name: "T", apps: map[string]*onepanelParsedApp{
		"appx": {key: "appx", versions: []onepanelParsedVersion{{
			version: "1.0.0",
			fields:  []onePanelFormField{{EnvKey: "PANEL_APP_PORT_HTTP", Type: "number", Default: 8080, Required: true}},
			compose: "services:\n  x:\n    image: app:latest\n    networks:\n      - 1panel-network\nnetworks:\n  1panel-network:\n    external: true\n",
		}}},
	}}
	ver, err := c.GetVersion(context.Background(), "appx", "")
	require.NoError(t, err)
	require.False(t, ver.Installable)
	require.Contains(t, ver.NotInstallableReason, "app:latest")
	snap := c.Snapshot()
	require.False(t, snap.Apps[0].Installable)
	require.Contains(t, snap.Apps[0].NotInstallableReason, "app:latest")
}

// 并发：写者交替替换 apps/storeName，读者 Snapshot/GetVersion；-race 下须无数据竞争。
func TestOnePanelCatalog_ConcurrentReadSafe(t *testing.T) {
	fixA, _, _ := parseOnePanelRepo(writeOnePanelFixture(t), onePanelFixturePaths())
	fixB, _, _ := parseOnePanelRepo(writeOnePanelFixture(t), onePanelFixturePaths())
	c := &onepanelCatalog{id: "s1", name: "T", storeName: "1Panel", apps: fixA}
	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 300; i++ {
			a := fixA
			if i%2 == 0 {
				a = fixB
			}
			c.mu.Lock()
			c.apps = a
			c.storeName = "1Panel"
			c.mu.Unlock()
		}
		close(stop)
	}()
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				c.Snapshot()
				_, _ = c.GetVersion(context.Background(), "adguardhome", "")
			}
		}()
	}
	wg.Wait()
}
