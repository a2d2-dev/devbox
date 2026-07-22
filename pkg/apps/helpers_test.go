package apps

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"My App!":    "my-app",
		"nginx 1.27": "nginx-1-27",
		"  Leading ": "leading",
		"A__B":       "a-b",
		"中文应用":       "",
		"ab":         "ab",
	}
	for in, want := range cases {
		assert.Equal(t, want, Slugify(in), "Slugify(%q)", in)
	}
}

func TestValidateAppID(t *testing.T) {
	for _, ok := range []string{"abc", "my-app", "nginx-1-27", "a1b2"} {
		assert.NoError(t, ValidateAppID(ok), "应合法: %s", ok)
	}
	for _, bad := range []string{"", "ab", "-abc", "abc-", "AB", "a.b", "../etc", "a/b", "a..b", "中文"} {
		assert.Error(t, ValidateAppID(bad), "应非法: %q", bad)
	}
}

func TestAppIDFromProject(t *testing.T) {
	assert.Equal(t, "my-app", AppIDFromProject("devbox-my-app"))
	assert.Equal(t, "", AppIDFromProject("my-app"))     // 无前缀
	assert.Equal(t, "", AppIDFromProject("devbox-"))    // 空 id
	assert.Equal(t, "", AppIDFromProject("devbox-A.B")) // 非法 id（防穿越）
	assert.Equal(t, "abc", AppIDFromProject("devbox-abc"))
}

func TestDetectDuplicateHostPorts(t *testing.T) {
	previews := []ServicePreview{
		{Name: "web", Ports: []string{"8080:80", "8443:443"}},
		{Name: "api", Ports: []string{"8080:8080"}}, // 8080 重复
	}
	dups := detectDuplicateHostPorts(previews)
	assert.Equal(t, []string{"8080"}, dups)
}

func TestDetectDuplicateHostPortsNone(t *testing.T) {
	previews := []ServicePreview{
		{Name: "web", Ports: []string{"8080:80", "3000"}}, // 3000 仅容器端口
	}
	assert.Empty(t, detectDuplicateHostPorts(previews))
}

func TestExtractHostPortWithBoundIP(t *testing.T) {
	assert.Equal(t, "8080", extractHostPort("127.0.0.1:8080:80/tcp"))
	assert.Equal(t, "8081", extractHostPort("[::1]:8081:80"))
	assert.Equal(t, "8082", extractHostPort("8082:80"))
	assert.Empty(t, extractHostPort("80"))
}

func TestRenderEnvFile(t *testing.T) {
	out := renderEnvFile(
		map[string]string{"DB_PASSWORD": "s3cr et", "TOKEN": "xyz"},
		map[string]string{"PORT": "8080", "DB_PASSWORD": "ignored"}, // secret 优先
	)
	assert.Contains(t, out, "DB_PASSWORD=\"s3cr et\"") // 含空格被引号包裹
	assert.Contains(t, out, "TOKEN=xyz")
	assert.Contains(t, out, "PORT=8080")
	// secret 不被 params 覆盖。
	assert.NotContains(t, out, "DB_PASSWORD=ignored")
}

func TestRenderEnvFileEmpty(t *testing.T) {
	assert.Empty(t, renderEnvFile(nil, nil))
}

func TestRestoreEnvParametersKeepsSecretsAndReplacesParameterSet(t *testing.T) {
	got := restoreEnvParameters("PORT=9090\nOLD_FLAG=on\nDB_PASSWORD=secret\n",
		map[string]string{"PORT": "9090", "OLD_FLAG": "on"},
		map[string]string{"PORT": "8080", "NEW_FLAG": "off"})
	assert.Contains(t, got, "PORT=8080")
	assert.Contains(t, got, "NEW_FLAG=off")
	assert.Contains(t, got, "DB_PASSWORD=secret")
	assert.NotContains(t, got, "OLD_FLAG")
}

func TestComposeHashStable(t *testing.T) {
	h1 := composeHash("services:\n  a:\n    image: x:1", map[string]string{"P": "1", "Q": "2"})
	h2 := composeHash("services:\n  a:\n    image: x:1", map[string]string{"Q": "2", "P": "1"}) // 顺序不同
	assert.Equal(t, h1, h2, "参数顺序不应影响 hash")
}

func TestSanitizeLogRedactsSecrets(t *testing.T) {
	got := sanitizeLog("DB_PASSWORD=hunter2 started\nplain line")
	assert.Contains(t, got, "DB_PASSWORD=***")
	assert.NotContains(t, got, "hunter2")
	assert.Contains(t, got, "plain line")
}

func TestSanitizeWithEnvValuesRedactsSecretAnywhere(t *testing.T) {
	got := sanitizeWithEnvValues("interpolation failed near super-secret-value in service", "DB_PASSWORD=super-secret-value\nPORT=8080\n")
	assert.NotContains(t, got, "super-secret-value")
	assert.Contains(t, got, "***")
	assert.Contains(t, sanitizeWithEnvValues("port 8080 failed", "PORT=8080\n"), "8080", "non-secret values are not redacted")
}
