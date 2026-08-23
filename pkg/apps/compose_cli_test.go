package apps

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComposeCLICommandPinsDockerEndpointAndIsolatesControlEnvironment(t *testing.T) {
	t.Setenv("DOCKER_HOST", "tcp://attacker:2375")
	t.Setenv("DOCKER_CONTEXT", "remote-context")
	t.Setenv("DOCKER_TLS_VERIFY", "1")
	t.Setenv("DOCKER_CERT_PATH", "/tmp/attacker-certs")
	t.Setenv("COMPOSE_FILE", "/tmp/attacker-compose.yaml")
	t.Setenv("COMPOSE_PROJECT_NAME", "attacker-project")
	t.Setenv("DEVBOX_KEEP_ME", "yes")

	cli := newComposeCLI("/run/devbox/docker.sock")
	cmd := cli.command(context.Background(), "compose", "version")
	env := map[string]string{}
	for _, item := range cmd.Env {
		key, value, ok := strings.Cut(item, "=")
		if ok {
			env[key] = value
		}
	}
	// daemon 端点走 docker 全局参数 -H（在 args 前置），不进 Env（避免 compose 插值读取 DOCKER_HOST）。
	assert.Contains(t, cmd.Args, "-H")
	assert.Contains(t, cmd.Args, "unix:///run/devbox/docker.sock")
	assert.NotContains(t, env, "DOCKER_HOST")
	// 安全策略（最小白名单）：业务/secret/路径类 env 一律不继承。
	for _, key := range []string{"DEVBOX_KEEP_ME", "DOCKER_CONTEXT", "DOCKER_TLS_VERIFY", "DOCKER_CERT_PATH",
		"COMPOSE_FILE", "COMPOSE_PROJECT_NAME", "HOME", "DOCKER_CONFIG", "SSL_CERT_FILE", "SSL_CERT_DIR"} {
		assert.NotContains(t, env, key)
	}
	// 执行所需的白名单项保留。
	assert.NotEmpty(t, env["PATH"])
}

// 这些用例调用真实 `docker compose config`（纯客户端、无需 daemon）。二进制缺失则跳过。

func skipIfNoComposeCLI(t *testing.T) {
	t.Helper()
	if _, err := exec.Command("docker", "compose", "version").Output(); err != nil {
		t.Skipf("docker compose 二进制不可用，跳过渲染测试: %v", err)
	}
}

// HIGH#3：合法 Compose 渲染成功，返回规范化文本。
func TestRenderConfigValid(t *testing.T) {
	skipIfNoComposeCLI(t)
	cli := newComposeCLI()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := cli.RenderConfig(ctx, "services:\n  web:\n    image: nginx:1.27\n    ports: [\"8080:80\"]", "")
	require.NoError(t, err)
	assert.Contains(t, out, "services:")
	assert.Contains(t, out, "nginx:1.27")
}

// 环境隔离（真实 docker）：HOME/DOCKER_HOST 不进子进程 Env，故 compose 插值回退到安全默认值。
func TestRenderConfigEnvIsolationFallsBackToSafeDefault(t *testing.T) {
	skipIfNoComposeCLI(t)
	t.Setenv("HOME", "/home/should-not-leak")
	t.Setenv("DOCKER_HOST", "unix:///var/run/docker.sock")
	cli := newComposeCLI()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := cli.RenderConfig(ctx,
		"services:\n  web:\n    image: nginx\n    environment:\n      A: ${HOME:-safe}\n      B: ${DOCKER_HOST:-safe}\n", "")
	require.NoError(t, err)
	assert.Contains(t, out, "safe", "HOME/DOCKER_HOST 不得注入子进程，插值应回退 safe")
	assert.NotContains(t, out, "should-not-leak", "HOME 值不得泄漏到渲染输出")
}

// HIGH#3：非法 Compose → ValidationErr（不含渲染后 secret）。
func TestRenderConfigInvalid(t *testing.T) {
	skipIfNoComposeCLI(t)
	cli := newComposeCLI()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// 缩进用 tab，YAML 规范禁止 → 解析错误。
	_, err := cli.RenderConfig(ctx, "services:\n\tweb:\n image: nginx:1.27\n", "")
	ae, ok := AsError(err)
	require.True(t, ok, "应为领域错误，got %T %v", err, err)
	assert.Equal(t, ErrKindValidation, ae.Kind)
}

// MED#7：${VAR} 插值展开后，风险分析能捕获 docker.sock（静态原文会漏检）。
func TestRenderConfigInterpolationBlocked(t *testing.T) {
	skipIfNoComposeCLI(t)
	cli := newComposeCLI()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	compose := "services:\n  a:\n    image: nginx:1.27\n    volumes:\n      - \"${SOCK}:/var/run/docker.sock\"\n"
	env := "SOCK=/var/run/docker.sock\n"
	rendered, err := cli.RenderConfig(ctx, compose, env)
	require.NoError(t, err)
	// 渲染后 ${SOCK} 已展开为 /var/run/docker.sock。
	assert.Contains(t, strings.ToLower(rendered), "docker.sock")
	findings, ferr := AnalyzeCompose(rendered)
	require.NoError(t, ferr)
	assert.True(t, HasBlocked(findings), "渲染后 docker.sock 挂载必须被阻断（${VAR} 无法绕过）")
}

func TestComposeCLIEnvIsolationDropsSecrets(t *testing.T) {
	// 模拟 devbox 进程持有业务 secret / 代理凭据。
	t.Setenv("SECRET_SENTINEL", "leak-me")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "leak-aws")
	t.Setenv("HTTPS_PROXY", "http://user:pass@proxy:3128")
	t.Setenv("TOKEN", "leak-token")
	t.Setenv("PATH", "/usr/bin") // 白名单项，应保留

	cli := newComposeCLI("unix:///var/run/docker.sock")
	cmd := cli.command(context.Background(), "compose", "version")
	env := strings.Join(cmd.Env, "\n")
	assert.NotContains(t, env, "SECRET_SENTINEL", "不得继承业务 secret（防 compose 插值读取）")
	assert.NotContains(t, env, "AWS_SECRET_ACCESS_KEY")
	assert.NotContains(t, env, "HTTPS_PROXY", "代理 env 可能带凭据，不得盲留")
	assert.NotContains(t, env, "leak-token")
	assert.NotContains(t, env, "HOME", "HOME 不得进 Env（compose 可引用）")
	assert.NotContains(t, env, "DOCKER_CONFIG")
	assert.NotContains(t, env, "DOCKER_HOST", "DOCKER_HOST 走 -H 参数，不进 Env")
	assert.NotContains(t, env, "SSL_CERT")
	// 完全固定 env（不继承宿主 PATH/locale）：固定标准 PATH + LANG=C。
	assert.Contains(t, env, "PATH=/usr/local/sbin", "PATH 应为固定标准值，非继承")
	assert.NotContains(t, env, "PATH=/usr/bin", "不得继承宿主 PATH（避免 ${PATH} 暴露）")
	assert.Contains(t, env, "LANG=C")
	// daemon 端点经 -H 参数注入。
	assert.Contains(t, cmd.Args, "-H")
	assert.Contains(t, cmd.Args, "unix:///var/run/docker.sock")
}
