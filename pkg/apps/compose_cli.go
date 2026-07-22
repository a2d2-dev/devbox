package apps

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

// precheckProject 固定的安全临时 project 名。`config` 纯客户端、不触碰 daemon，
// 多次并发预检共用同一 project 名也不会冲突。
const precheckProject = "devbox-precheck"

// RenderConfig 用 `docker compose config` 在隔离临时目录中渲染 content，返回插值/
// 规范化后的 Compose 文本，用于落盘前预检与（渲染后）风险分析。
//
// 安全约束：
//   - 临时目录 0700、compose/.env 文件 0600；固定临时 project；无 shell（独立参数数组）。
//   - 30s 超时；stdout/stderr 各 1MB 上限；目录用后清理。
//   - 渲染输出（stdout）可能含被插值替换的 secret 原值 —— 仅在成功时返回，供调用方
//     在内存做风险分析，绝不写入 task/revision/audit/log/error。
//   - 失败时只回显 stderr（结构化校验信息，经脱敏截断），丢弃 stdout。
//   - docker/compose 二进制缺失 → ErrKindCapability；配置非法 → ErrKindValidation。
func (c *composeCLI) RenderConfig(ctx context.Context, content, env string) (string, error) {
	dir, err := os.MkdirTemp("", "devbox-precheck-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	if err := os.Chmod(dir, 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte(content), 0o600); err != nil {
		return "", err
	}
	if strings.TrimSpace(env) != "" {
		if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(env), 0o600); err != nil {
			return "", err
		}
	}

	runCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(runCtx, c.binary, c.argsFor(dir, precheckProject, "config")...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &limitedWriter{w: &stdout, max: 1 << 20}
	cmd.Stderr = &limitedWriter{w: &stderr, max: 1 << 20}
	if err := cmd.Run(); err != nil {
		if isExecNotFound(err) {
			return "", CapabilityErr("docker compose 不可用，无法预检 Compose 配置")
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return "", CapabilityErr("compose 预检超时")
		}
		// 仅回显 stderr（结构化校验信息，脱敏截断）；丢弃 stdout（可能含渲染后的 secret）。
		return "", ValidationErr("compose 配置无效: " + sanitizeWithEnvValues(strings.TrimSpace(stderr.String()), env))
	}
	return stdout.String(), nil
}

// isExecNotFound 判定是否为 docker/compose 二进制缺失。
func isExecNotFound(err error) bool {
	if errors.Is(err, exec.ErrNotFound) {
		return true
	}
	var pErr *exec.Error
	if errors.As(err, &pErr) && pErr.Err == exec.ErrNotFound {
		return true
	}
	return strings.Contains(err.Error(), "executable file not found")
}

// pull 拉取服务镜像（best-effort：--ignore-pull-failures，私有镜像可能已存在）。
// 调用方应记录返回的 error 而非吞掉；失败后仍可继续 up（up 会自拉取）。
func (c *composeCLI) pull(ctx context.Context, dir, project string) (string, error) {
	return c.run(ctx, dir, project, "pull", "--ignore-pull-failures")
}

func (c *composeCLI) up(ctx context.Context, dir, project string) error {
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
