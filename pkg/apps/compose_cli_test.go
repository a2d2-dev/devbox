package apps

import (
	"context"
	"os"
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
	assert.Equal(t, "unix:///run/devbox/docker.sock", env["DOCKER_HOST"])
	assert.Equal(t, "yes", env["DEVBOX_KEEP_ME"])
	for _, key := range []string{"DOCKER_CONTEXT", "DOCKER_TLS_VERIFY", "DOCKER_CERT_PATH", "COMPOSE_FILE", "COMPOSE_PROJECT_NAME"} {
		assert.NotContains(t, env, key)
	}
	assert.NotEmpty(t, os.Getenv("PATH"))
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
