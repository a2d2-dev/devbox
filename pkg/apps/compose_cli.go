package apps

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"time"
)

// Compose CLI v2 封装（Issue #2：写操作走 Compose CLI）。
//
// 安全：exec.CommandContext 独立参数数组，绝不 shell 拼接（防注入）；固定
// project name、project-directory、文件名；stdout/stderr 有上限；超时由 ctx 控制。
// 固定 project name = devbox-<app-id>，使受管容器可按 label 发现。

// composeCLI 封装 docker compose 子命令。
type composeCLI struct {
	binary string // 默认 "docker"（compose 作为子命令）
}

func newComposeCLI() *composeCLI { return &composeCLI{binary: "docker"} }

// argsFor 构造 compose 公共前缀参数。
func (c *composeCLI) argsFor(dir, project string, sub ...string) []string {
	args := []string{
		"compose",
		"--project-directory", dir,
		"-p", project,
		"-f", "compose.yaml",
		"--ansi", "never",
	}
	return append(args, sub...)
}

// run 执行 compose 子命令，返回合并的 stdout+stderr（上限 1MB）。
// exit 非 0 返回 error（含输出摘要）。
func (c *composeCLI) run(ctx context.Context, dir, project string, sub ...string) (string, error) {
	cmd := exec.CommandContext(ctx, c.binary, c.argsFor(dir, project, sub...)...)
	cmd.Dir = dir
	var buf bytes.Buffer
	cmd.Stdout = &limitedWriter{w: &buf, max: 1 << 20}
	cmd.Stderr = &limitedWriter{w: &buf, max: 1 << 20}
	if err := cmd.Run(); err != nil {
		out := buf.String()
		return out, &composeExecError{Cmd: sub[0], ExitErr: err, Output: out}
	}
	return buf.String(), nil
}

// config 预检（compose config -q；非法 YAML/配置 → 非 0）。
func (c *composeCLI) config(ctx context.Context, dir, project string) error {
	_, err := c.run(ctx, dir, project, "config", "-q")
	return err
}

func (c *composeCLI) up(ctx context.Context, dir, project string) error {
	// pull + up 分两步：pull 失败更易定位；up -d 后台启动。
	if _, err := c.run(ctx, dir, project, "pull", "--ignore-pull-failures"); err != nil {
		// pull 失败不立即放弃（私有镜像可能已存在）；继续 up，由 up 报错。
	}
	_, err := c.run(ctx, dir, project, "up", "-d", "--remove-orphans")
	return err
}

func (c *composeCLI) start(ctx context.Context, dir, project string) error {
	_, err := c.run(ctx, dir, project, "start")
	return err
}

func (c *composeCLI) stop(ctx context.Context, dir, project string) error {
	_, err := c.run(ctx, dir, project, "stop")
	return err
}

func (c *composeCLI) restart(ctx context.Context, dir, project string) error {
	_, err := c.run(ctx, dir, project, "restart")
	return err
}

// down 停止并移除容器/网络；purge 时加 --volumes 删除受管 volume
// （external volume 永不被 compose down 删除，符合安全模型）。
func (c *composeCLI) down(ctx context.Context, dir, project string, purge bool) error {
	args := []string{"down", "--remove-orphans"}
	if purge {
		args = append(args, "--volumes")
	}
	_, err := c.run(ctx, dir, project, args...)
	return err
}

// composeExecError 封装 compose 退出错误 + 输出。
type composeExecError struct {
	Cmd     string
	ExitErr error
	Output  string
}

func (e *composeExecError) Error() string {
	out := e.Output
	if len(out) > 500 {
		out = out[:500] + "...(truncated)"
	}
	return "compose " + e.Cmd + " failed: " + e.ExitErr.Error() + ": " + out
}

func (e *composeExecError) Unwrap() error { return e.ExitErr }

// limitedWriter 写入上限 max 字节，超出静默丢弃（防止超大日志爆内存）。
type limitedWriter struct {
	w   *bytes.Buffer
	max int
	n   int
}

func (l *limitedWriter) Write(p []byte) (int, error) {
	remain := l.max - l.n
	if remain <= 0 {
		return len(p), nil
	}
	if len(p) > remain {
		l.w.Write(p[:remain])
		l.n = l.max
		return len(p), nil
	}
	l.w.Write(p)
	l.n += len(p)
	return len(p), nil
}

// withTimeout 包装一个操作超时（写操作用）。
func withTimeout(parent context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	if d <= 0 {
		return context.WithCancel(parent)
	}
	return context.WithTimeout(parent, d)
}

// errComposeNotFound 项目不存在（compose down 对已删除项目）。
var errComposeNotFound = errors.New("compose project not found")
