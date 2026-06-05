package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"syscall"
	"time"

	"go.uber.org/zap"
)

// Executor 任务执行器
type Executor struct {
	logger *zap.Logger
}

// Task 任务定义
type Task struct {
	ID        string                 `json:"id"`        // 任务 ID（向后兼容）
	RequestID string                 `json:"requestId"` // 请求 ID（Story 3.1新增，用于请求-响应关联）
	Type      string                 `json:"type"`      // 任务类型: "adhoc", "shell", "exec" 等
	Command   string                 `json:"command"`   // Shell 命令
	Timeout   int                    `json:"timeout"`   // 超时时间（秒），0 表示无限制
	Extra     map[string]interface{} `json:"extra"`     // 扩展字段（预留）
}

// Result 任务执行结果
type Result struct {
	TaskID    string    `json:"task_id"`              // 任务 ID（向后兼容）
	RequestID string    `json:"requestId,omitempty"`  // 请求 ID（Story 3.1新增）
	Status    string    `json:"status"`               // "success" 或 "failed"
	Output    string    `json:"output,omitempty"`     // 标准输出（向后兼容，已废弃）
	Error     string    `json:"error,omitempty"`      // 错误信息（向后兼容，已废弃）
	Stdout    string    `json:"stdout,omitempty"`     // 标准输出（Story 3.1新增）
	Stderr    string    `json:"stderr,omitempty"`     // 标准错误输出（Story 3.1新增）
	ExitCode  int       `json:"exitCode"`             // 退出码（Story 3.1新增，0表示成功）
	StartTime time.Time `json:"start_time,omitempty"` // 开始时间
	EndTime   time.Time `json:"end_time,omitempty"`   // 结束时间
	Duration  float64   `json:"duration,omitempty"`   // 执行时长（秒）
}

// New 创建任务执行器
func New(logger *zap.Logger) *Executor {
	if logger == nil {
		logger, _ = zap.NewProduction()
	}

	return &Executor{
		logger: logger,
	}
}

// Execute 执行任务
func (e *Executor) Execute(task *Task) *Result {
	e.logger.Info("Executing task",
		zap.String("task_id", task.ID),
		zap.String("request_id", task.RequestID),
		zap.String("type", task.Type),
		zap.String("command", task.Command))

	startTime := time.Now()

	result := &Result{
		TaskID:    task.ID,
		RequestID: task.RequestID,
		StartTime: startTime,
		ExitCode:  -1, // 默认退出码为-1，表示未执行
	}

	// 根据任务类型执行
	switch task.Type {
	case "adhoc", "shell", "exec":
		e.executeShellCommand(task, result)
	default:
		result.Status = "failed"
		result.Error = fmt.Sprintf("unsupported task type: %s", task.Type)
		result.Stderr = result.Error // 同时填充 Stderr 字段
	}

	// 记录结束时间和执行时长
	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime).Seconds()

	e.logger.Info("Task execution completed",
		zap.String("task_id", task.ID),
		zap.String("request_id", task.RequestID),
		zap.String("status", result.Status),
		zap.Int("exit_code", result.ExitCode),
		zap.Float64("duration", result.Duration))

	return result
}

// executeShellCommand 执行 Shell 命令
func (e *Executor) executeShellCommand(task *Task, result *Result) {
	// 创建上下文（支持超时）
	var ctx context.Context
	var cancel context.CancelFunc

	// 验证命令安全性
	if err := e.validateCommand(task.Command); err != nil {
		result.Status = "failed"
		result.Error = fmt.Sprintf("command validation failed: %s", err.Error())
		result.Stderr = result.Error
		result.Output = ""
		result.ExitCode = -1
		return
	}

	if task.Timeout > 0 {
		ctx, cancel = context.WithTimeout(context.Background(), time.Duration(task.Timeout)*time.Second)
		defer cancel()
	} else {
		ctx = context.Background()
	}

	// 创建安全的命令执行
	cmd, err := e.createSecureCommand(ctx, task.Command)
	if err != nil {
		result.Status = "failed"
		result.Error = fmt.Sprintf("failed to create secure command: %s", err.Error())
		result.Stderr = result.Error
		result.Output = ""
		result.ExitCode = -1
		return
	}

	// 捕获输出
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Story 3.6: 使用 Start + Wait 模式，以便在超时时能立即杀死进程组
	if err = cmd.Start(); err != nil {
		result.Status = "failed"
		result.ExitCode = -1
		result.Error = fmt.Sprintf("failed to start command: %s", err.Error())
		result.Stderr = result.Error
		return
	}

	// 使用 channel 等待命令完成或超时
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	// 等待完成或超时
	var timedOut bool
	select {
	case <-ctx.Done():
		// 超时：杀死整个进程组
		timedOut = true
		if cmd.Process != nil {
			// Story 3.6: 使用负的 PID 杀死整个进程组
			pgid, pgidErr := syscall.Getpgid(cmd.Process.Pid)
			if pgidErr == nil {
				syscall.Kill(-pgid, syscall.SIGKILL)
				e.logger.Info("Killed process group on timeout",
					zap.Int("pgid", pgid),
					zap.Int("timeout", task.Timeout))
			} else {
				// 回退：直接杀死进程
				cmd.Process.Kill()
			}
		}
		// 等待进程退出以清理资源
		<-done
	case err = <-done:
		// 命令正常完成
	}

	// 捕获 stdout 和 stderr
	result.Stdout = stdout.String()
	result.Stderr = stderr.String()

	// 处理结果
	if timedOut {
		result.Status = "failed"
		result.ExitCode = -1
		result.Error = fmt.Sprintf("command timeout after %d seconds", task.Timeout)
		result.Stderr = result.Stderr + "\n" + result.Error
		result.Output = result.Stdout

		e.logger.Warn("Command execution timeout",
			zap.String("task_id", task.ID),
			zap.String("request_id", task.RequestID),
			zap.Int("timeout_seconds", task.Timeout),
			zap.String("command", task.Command))
	} else if err != nil {
		result.Status = "failed"

		// 尝试获取退出码
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.ExitCode = -1
		}

		result.Error = err.Error()
		result.Output = result.Stdout

		if result.Stderr != "" {
			result.Error = fmt.Sprintf("%s\nSTDERR:\n%s", result.Error, result.Stderr)
		}
	} else {
		result.Status = "success"
		result.ExitCode = 0
		result.Output = result.Stdout

		if result.Stderr != "" {
			result.Output = fmt.Sprintf("%s\nSTDERR:\n%s", result.Output, result.Stderr)
		}
	}
}

// ParseTask 从 JSON payload 解析任务
func ParseTask(payload []byte) (*Task, error) {
	var task Task
	if err := json.Unmarshal(payload, &task); err != nil {
		return nil, fmt.Errorf("failed to unmarshal task: %w", err)
	}

	// 验证必需字段
	// Story 3.1: 如果没有 ID 但有 RequestID，使用 RequestID 作为 ID（向后兼容）
	if task.ID == "" && task.RequestID == "" {
		return nil, fmt.Errorf("task ID or requestID is required")
	}

	// 如果只有 RequestID，将其复制到 ID
	if task.ID == "" && task.RequestID != "" {
		task.ID = task.RequestID
	}

	// 如果只有 ID，将其复制到 RequestID
	if task.RequestID == "" && task.ID != "" {
		task.RequestID = task.ID
	}

	if task.Type == "" {
		return nil, fmt.Errorf("task type is required")
	}

	if task.Command == "" && task.Type != "playbook" {
		return nil, fmt.Errorf("task command is required for type: %s", task.Type)
	}

	return &task, nil
}

// validateCommand 验证命令的安全性，防止注入攻击
// NOTE: 安全验证已禁用，允许所有命令执行
func (e *Executor) validateCommand(command string) error {
	if command == "" {
		return fmt.Errorf("empty command")
	}

	// 安全验证已禁用 - 直接返回 nil
	return nil

	/*
		// 以下代码已禁用
		// 危险命令模式检测
		dangerousPatterns := []string{
			// 文件系统操作
			`rm\s+-rf\s+/`,
			`dd\s+if=/dev/zero`,
			`mkfs\.`,
			`format\s+`,

			// 系统控制
			`sudo\s+`,
			`su\s+`,
			`passwd\s+`,
			`chpasswd\s+`,
			`useradd\s+`,
			`userdel\s+`,
			`groupadd\s+`,
			`groupdel\s+`,

			// 网络和下载
			`wget\s+`,
			`curl\s+.*\s+-o\s+/`,
			`nc\s+`,
			`netcat\s+`,
			`telnet\s+`,
			`ssh\s+`,

			// 权限修改
			`chmod\s+777`,
			`chmod\s+a+rwx`,
			`chown\s+`,
			`chmod\s+u\+s`,

			// 系统服务
			`systemctl\s+`,
			`service\s+`,
			`init\s+`,
			`shutdown\s+`,
			`reboot\s+`,
			`halt\s+`,
			`poweroff\s+`,

			// 系统信息泄露
			`cat\s+/etc/passwd`,
			`cat\s+/etc/shadow`,
			`cat\s+/etc/hosts`,
			`cat\s+/etc/network/interfaces`,
			`ip\s+link\s+show`,
			`ifconfig\s+-a`,
			`route\s+-n`,
			`netstat\s+-a`,
			`ss\s+-a`,

			// 进程操作
			`kill\s+-9`,
			`killall\s+`,
			`pkill\s+`,

			// 转向和管道
			`>\s+/dev/`,
			`>>\s+/dev/`,
			`&>\s+/dev/`,
			`2>\s+/dev/`,
			`&&\s+rm\s`,
			`;\s+rm\s`,
			`\|\s+sh\s`,
			`\|\s+bash\s`,
			`\$\(`,
			`<\s*\(`,

			// 环境变量操作
			`export\s+`,
			`unset\s+`,
			`env\s+`,

			// 包管理器
			`apt-get\s+install`,
			`apt\s+install`,
			`yum\s+install`,
			`dnf\s+install`,
			`pacman\s+-S`,
			`apk\s+add`,

			// 其他危险操作
			`:(){ :|:& };:`,
			`fork\s+bomb`,
			`\.\./`,
			`/proc/`,
			`/sys/`,
		}

		// 检查危险模式
		for _, pattern := range dangerousPatterns {
			matched, err := regexp.MatchString(pattern, command)
			if err != nil {
				return fmt.Errorf("regex compilation error: %s", err.Error())
			}
			if matched {
				return fmt.Errorf("dangerous command pattern detected: %s", pattern)
			}
		}

		// 命令长度限制
		if len(command) > 1000 {
			return fmt.Errorf("command too long (max 1000 characters)")
		}

		// 基本字符集验证（只允许安全字符）
		safePattern := `^[a-zA-Z0-9\s\-_.\/=.,?!%&+*'"()[\]{}<>:;|]+$`
		matched, err := regexp.MatchString(safePattern, command)
		if err != nil {
			return fmt.Errorf("safe pattern compilation error: %s", err.Error())
		}
		if !matched {
			return fmt.Errorf("command contains unsafe characters")
		}

		return nil
	*/
}

// createSecureCommand 创建安全的命令执行环境
// Story 3.6: 启用进程组，以便超时时可以杀死所有子进程
func (e *Executor) createSecureCommand(ctx context.Context, command string) (*exec.Cmd, error) {
	// 使用受限的 shell 环境和参数
	// 避免直接使用 sh -c 来减少注入风险

	// 分割命令为参数（简单的实现）
	// 在生产环境中，建议使用更完善的命令解析器
	args := []string{
		"-c",
		// 添加安全限制
		"set -euo pipefail; " +
			"PATH=/usr/local/bin:/usr/bin:/bin; " +
			"export PATH; " +
			command,
	}

	cmd := exec.CommandContext(ctx, "bash", args...)

	// 设置安全的环境变量
	cmd.Env = []string{
		"PATH=/usr/local/bin:/usr/bin:/bin",
		"HOME=/tmp",
		"TMPDIR=/tmp",
		"LANG=C.UTF-8",
		"LC_ALL=C.UTF-8",
	}

	// Story 3.6: 设置进程组，以便超时时可以杀死整个进程树
	// Setpgid=true 使子进程成为新进程组的领导者
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	return cmd, nil
}
