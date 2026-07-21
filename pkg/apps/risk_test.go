package apps

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 风险策略测试（Issue #2 安全模型：Docker daemon ≈ 宿主 root）。

func TestAnalyzeComposeBlockedPrivileged(t *testing.T) {
	yaml := `services:
  web:
    image: nginx:1.27
    privileged: true`
	f, err := AnalyzeCompose(yaml)
	require.NoError(t, err)
	assert.True(t, HasBlocked(f), "privileged 必须阻断")
}

func TestAnalyzeComposeBlockedDockerSocket(t *testing.T) {
	cases := []string{
		"/var/run/docker.sock:/var/run/docker.sock",
		"./docker.sock:/var/run/docker.sock",
	}
	for _, spec := range cases {
		yaml := "services:\n  a:\n    image: nginx:1.27\n    volumes:\n      - " + spec
		f, err := AnalyzeCompose(yaml)
		require.NoError(t, err)
		assert.True(t, HasBlocked(f), "docker.sock 挂载必须阻断: %s", spec)
	}
}

func TestAnalyzeComposeBlockedRootBind(t *testing.T) {
	for _, p := range []string{"/", "/etc", "/usr", "/proc", "/run", "/var", "/var/run", "/var/lib/docker"} {
		yaml := "services:\n  a:\n    image: nginx:1.27\n    volumes:\n      - " + p + ":/host"
		f, err := AnalyzeCompose(yaml)
		require.NoError(t, err)
		assert.True(t, HasBlocked(f), "根/系统目录 bind 必须阻断: %s", p)
	}
	// 子目录允许（非系统关键目录本身）。
	yaml := "services:\n  a:\n    image: nginx:1.27\n    volumes:\n      - /etc/devbox:/host"
	f, err := AnalyzeCompose(yaml)
	require.NoError(t, err)
	assert.False(t, HasBlocked(f), "/etc 子目录不应阻断")
}

func TestAnalyzeComposeBlockedTopLevelHostNetwork(t *testing.T) {
	for _, network := range []string{
		"networks:\n  hostnet:\n    driver: host",
		"networks:\n  hostnet:\n    external: true\n    name: host",
	} {
		raw := "services:\n  app:\n    image: nginx:1.27\n    networks: [hostnet]\n" + network
		findings, err := AnalyzeCompose(raw)
		require.NoError(t, err)
		assert.True(t, HasBlocked(findings), "顶层 host network 必须阻断")
	}
}

func TestAnalyzeComposeBlockedHostNetworkAndPID(t *testing.T) {
	yaml := `services:
  a:
    image: nginx:1.27
    network_mode: host
    pid: host`
	f, err := AnalyzeCompose(yaml)
	require.NoError(t, err)
	assert.True(t, HasBlocked(f))
	// 两者都应各产生一条 blocked。
	var blocked int
	for _, x := range f {
		if x.Level == RiskBlocked {
			blocked++
		}
	}
	assert.Equal(t, 2, blocked)
}

func TestAnalyzeComposeWarningLatest(t *testing.T) {
	for _, img := range []string{"nginx", "nginx:latest", "nginx:main"} {
		yaml := "services:\n  a:\n    image: " + img
		f, err := AnalyzeCompose(yaml)
		require.NoError(t, err)
		assert.False(t, HasBlocked(f), "latest 不应阻断")
		var warned bool
		for _, x := range f {
			if x.Level == RiskWarning {
				warned = true
			}
		}
		assert.True(t, warned, "latest/main 必须警告: %s", img)
	}
	// 固定版本无 latest 警告。
	f, err := AnalyzeCompose("services:\n  a:\n    image: nginx:1.27")
	require.NoError(t, err)
	for _, x := range f {
		if x.Level == RiskWarning && x.Field == "image" {
			t.Fatalf("固定版本不应有 image warning")
		}
	}
}

func TestAnalyzeComposeConfirmationCapAndIPC(t *testing.T) {
	yaml := `services:
  a:
    image: nginx:1.27
    ipc: host
    cap_add: [SYS_ADMIN]`
	f, err := AnalyzeCompose(yaml)
	require.NoError(t, err)
	assert.False(t, HasBlocked(f), "confirmation 不是 blocked")
	assert.True(t, NeedsConfirmation(f, false), "未确认时应需确认")
	assert.False(t, NeedsConfirmation(f, true), "已确认后不再阻断")
}

func TestAnalyzeComposeInvalidYAML(t *testing.T) {
	_, err := AnalyzeCompose("services: [this is not: valid")
	assert.Error(t, err)
}

func TestAnalyzeComposeNoServices(t *testing.T) {
	_, err := AnalyzeCompose("version: '3'")
	assert.Error(t, err)
}

func TestAnalyzeComposeSafe(t *testing.T) {
	yaml := `services:
  web:
    image: nginx:1.27
    ports: ["8080:80"]`
	f, err := AnalyzeCompose(yaml)
	require.NoError(t, err)
	assert.False(t, HasBlocked(f))
	assert.False(t, NeedsConfirmation(f, false))
}

// 长语法 volume 的 bind 检测。
func TestAnalyzeComposeLongSyntaxBind(t *testing.T) {
	yaml := `services:
  a:
    image: nginx:1.27
    volumes:
      - type: bind
        source: /etc
        target: /host`
	f, err := AnalyzeCompose(yaml)
	require.NoError(t, err)
	assert.True(t, HasBlocked(f))
}

// MED#7：相对 bind 含 ".." 路径穿越不应被 filepath.Clean 折叠后漏检 → 需确认。
func TestAnalyzeComposeRelativeTraversalBind(t *testing.T) {
	for _, spec := range []string{"../../etc:/host", "../../../:/host"} {
		yaml := "services:\n  a:\n    image: nginx:1.27\n    volumes:\n      - " + spec
		f, err := AnalyzeCompose(yaml)
		require.NoError(t, err)
		assert.True(t, NeedsConfirmation(f, false), "相对 .. 穿越 bind 应需确认: %s", spec)
	}
	// 子目录（非系统关键目录本身，无 ..）不应误判。
	yaml := "services:\n  a:\n    image: nginx:1.27\n    volumes:\n      - /etc/devbox:/host"
	f, err := AnalyzeCompose(yaml)
	require.NoError(t, err)
	assert.False(t, HasBlocked(f), "/etc 子目录不应阻断")
	assert.False(t, NeedsConfirmation(f, false), "/etc 子目录不应需确认")
}
