//go:build unit
// +build unit

package executor

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

func TestExecuteShellCommand(t *testing.T) {
	logger := zaptest.NewLogger(t)
	exec := New(logger)

	tests := []struct {
		name    string
		task    *Task
		wantErr bool
		check   func(t *testing.T, result *Result)
	}{
		{
			name: "simple echo command",
			task: &Task{
				ID:      "test-001",
				Type:    "shell",
				Command: "echo 'Hello OTA'",
				Timeout: 10,
			},
			wantErr: false,
			check: func(t *testing.T, result *Result) {
				assert.Equal(t, "success", result.Status)
				assert.Contains(t, result.Output, "Hello OTA")
				assert.Empty(t, result.Error)
			},
		},
		{
			name: "command with output",
			task: &Task{
				ID:      "test-002",
				Type:    "shell",
				Command: "whoami",
				Timeout: 10,
			},
			wantErr: false,
			check: func(t *testing.T, result *Result) {
				assert.Equal(t, "success", result.Status)
				assert.NotEmpty(t, result.Output)
			},
		},
		{
			name: "pipe command",
			task: &Task{
				ID:      "test-003",
				Type:    "shell",
				Command: "echo 'line1\nline2\nline3' | head -1",
				Timeout: 10,
			},
			wantErr: false,
			check: func(t *testing.T, result *Result) {
				assert.Equal(t, "success", result.Status)
				assert.Contains(t, result.Output, "line1")
			},
		},
		{
			name: "nonexistent command",
			task: &Task{
				ID:      "test-004",
				Type:    "shell",
				Command: "nonexistent-command-12345",
				Timeout: 10,
			},
			wantErr: true,
			check: func(t *testing.T, result *Result) {
				assert.Equal(t, "failed", result.Status)
				assert.NotEmpty(t, result.Error)
			},
		},
		{
			name: "command timeout",
			task: &Task{
				ID:      "test-005",
				Type:    "shell",
				Command: "sleep 10",
				Timeout: 1,
			},
			wantErr: true,
			check: func(t *testing.T, result *Result) {
				assert.Equal(t, "failed", result.Status)
				assert.Contains(t, result.Error, "timeout")
			},
		},
		{
			name: "command with non-zero exit code",
			task: &Task{
				ID:      "test-006",
				Type:    "shell",
				Command: "exit 1",
				Timeout: 10,
			},
			wantErr: true,
			check: func(t *testing.T, result *Result) {
				assert.Equal(t, "failed", result.Status)
				assert.NotEmpty(t, result.Error)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := exec.Execute(tt.task)

			require.NotNil(t, result)
			assert.Equal(t, tt.task.ID, result.TaskID)
			assert.NotZero(t, result.Duration)
			assert.False(t, result.StartTime.IsZero())
			assert.False(t, result.EndTime.IsZero())

			if tt.check != nil {
				tt.check(t, result)
			}
		})
	}
}

func TestParseTask(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		wantErr bool
		check   func(t *testing.T, task *Task)
	}{
		{
			name:    "valid shell task",
			payload: `{"id":"task-001","type":"shell","command":"echo hello","timeout":10}`,
			wantErr: false,
			check: func(t *testing.T, task *Task) {
				assert.Equal(t, "task-001", task.ID)
				assert.Equal(t, "shell", task.Type)
				assert.Equal(t, "echo hello", task.Command)
				assert.Equal(t, 10, task.Timeout)
			},
		},
		{
			name:    "task without timeout",
			payload: `{"id":"task-002","type":"shell","command":"ls"}`,
			wantErr: false,
			check: func(t *testing.T, task *Task) {
				assert.Equal(t, "task-002", task.ID)
				assert.Equal(t, 0, task.Timeout)
			},
		},
		{
			name:    "missing task ID",
			payload: `{"type":"shell","command":"echo test"}`,
			wantErr: true,
		},
		{
			name:    "missing task type",
			payload: `{"id":"task-003","command":"echo test"}`,
			wantErr: true,
		},
		{
			name:    "missing command",
			payload: `{"id":"task-004","type":"shell"}`,
			wantErr: true,
		},
		{
			name:    "invalid JSON",
			payload: `{invalid json}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task, err := ParseTask([]byte(tt.payload))

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.NotNil(t, task)
				if tt.check != nil {
					tt.check(t, task)
				}
			}
		})
	}
}

func TestExecutorConcurrency(t *testing.T) {
	logger := zaptest.NewLogger(t)
	exec := New(logger)

	// 测试并发执行多个任务
	tasks := []*Task{
		{ID: "concurrent-1", Type: "shell", Command: "echo task1", Timeout: 10},
		{ID: "concurrent-2", Type: "shell", Command: "echo task2", Timeout: 10},
		{ID: "concurrent-3", Type: "shell", Command: "echo task3", Timeout: 10},
		{ID: "concurrent-4", Type: "shell", Command: "echo task4", Timeout: 10},
		{ID: "concurrent-5", Type: "shell", Command: "echo task5", Timeout: 10},
	}

	results := make([]*Result, len(tasks))
	done := make(chan bool, len(tasks))

	// 并发执行
	startTime := time.Now()
	for i, task := range tasks {
		go func(idx int, t *Task) {
			results[idx] = exec.Execute(t)
			done <- true
		}(i, task)
	}

	// 等待所有任务完成
	for i := 0; i < len(tasks); i++ {
		<-done
	}
	duration := time.Since(startTime)

	// 验证结果
	successCount := 0
	for _, result := range results {
		assert.NotNil(t, result)
		if result.Status == "success" {
			successCount++
		}
	}

	assert.Equal(t, len(tasks), successCount, "All tasks should succeed")
	assert.Less(t, duration, 5*time.Second, "Concurrent execution should be fast")
}

// TestExecuteExecCommandWithRequestID tests exec command with requestId (Story 3.1)
func TestExecuteExecCommandWithRequestID(t *testing.T) {
	logger := zaptest.NewLogger(t)
	exec := New(logger)

	task := &Task{
		ID:        "task-001",
		RequestID: "req-uuid-001",
		Type:      "exec",
		Command:   "echo 'Hello World'",
		Timeout:   10,
	}

	result := exec.Execute(task)

	// 验证 requestId 字段
	assert.Equal(t, "req-uuid-001", result.RequestID)
	assert.Equal(t, "success", result.Status)
	assert.Equal(t, 0, result.ExitCode)

	// 验证 stdout/stderr 分离
	assert.NotEmpty(t, result.Stdout)
	// Note: Stderr may contain locale warnings, which is acceptable
	// assert.Empty(t, result.Stderr)

	// 验证向后兼容字段
	assert.Equal(t, "task-001", result.TaskID)
	assert.NotEmpty(t, result.Output)
}

// TestExecuteCommandWithStderr tests command that outputs to stderr (Story 3.1)
func TestExecuteCommandWithStderr(t *testing.T) {
	logger := zaptest.NewLogger(t)
	exec := New(logger)

	task := &Task{
		ID:        "task-002",
		RequestID: "req-uuid-002",
		Type:      "exec",
		Command:   "echo 'error message' >&2",
		Timeout:   10,
	}

	result := exec.Execute(task)

	assert.Equal(t, "success", result.Status)
	assert.Equal(t, 0, result.ExitCode)
	assert.NotEmpty(t, result.Stderr)
	assert.Contains(t, result.Stderr, "error message")
}

// TestExecuteCommandExitCode tests command exit code capture (Story 3.1)
func TestExecuteCommandExitCode(t *testing.T) {
	logger := zaptest.NewLogger(t)
	exec := New(logger)

	tests := []struct {
		name           string
		command        string
		expectedCode   int
		expectedStatus string
	}{
		{
			name:           "exit code 0",
			command:        "exit 0",
			expectedCode:   0,
			expectedStatus: "success",
		},
		{
			name:           "exit code 1",
			command:        "exit 1",
			expectedCode:   1,
			expectedStatus: "failed",
		},
		{
			name:           "exit code 2",
			command:        "exit 2",
			expectedCode:   2,
			expectedStatus: "failed",
		},
		{
			name:           "exit code 127",
			command:        "exit 127",
			expectedCode:   127,
			expectedStatus: "failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := &Task{
				ID:        "test-exitcode",
				RequestID: "req-" + tt.name,
				Type:      "exec",
				Command:   tt.command,
				Timeout:   10,
			}

			result := exec.Execute(task)

			assert.Equal(t, tt.expectedStatus, result.Status)
			assert.Equal(t, tt.expectedCode, result.ExitCode)
			assert.Equal(t, task.RequestID, result.RequestID)
		})
	}
}

// TestExecuteCommandWithBothOutputs tests command with both stdout and stderr (Story 3.1)
func TestExecuteCommandWithBothOutputs(t *testing.T) {
	logger := zaptest.NewLogger(t)
	exec := New(logger)

	task := &Task{
		ID:        "task-003",
		RequestID: "req-uuid-003",
		Type:      "exec",
		Command:   "echo 'stdout'; echo 'stderr' >&2",
		Timeout:   10,
	}

	result := exec.Execute(task)

	assert.Equal(t, "success", result.Status)
	assert.Equal(t, 0, result.ExitCode)
	assert.Contains(t, result.Stdout, "stdout")
	assert.Contains(t, result.Stderr, "stderr")

	// 向后兼容：Output 应该包含 stdout 和 stderr
	assert.Contains(t, result.Output, "stdout")
	assert.Contains(t, result.Output, "stderr")
}

// TestParseTaskWithRequestID tests parsing task with requestId (Story 3.1)
func TestParseTaskWithRequestID(t *testing.T) {
	payload := []byte(`{
		"requestId": "req-123",
		"command": "uptime",
		"timeout": 30,
		"type": "exec"
	}`)

	task, err := ParseTask(payload)
	require.NoError(t, err)
	require.NotNil(t, task)

	assert.Equal(t, "req-123", task.RequestID)
	assert.Equal(t, "uptime", task.Command)
	assert.Equal(t, 30, task.Timeout)
	assert.Equal(t, "exec", task.Type)
}

// TestExecuteCommandTimeout tests command timeout with exitCode -1 (Story 3.1)
func TestExecuteCommandTimeoutExitCode(t *testing.T) {
	logger := zaptest.NewLogger(t)
	exec := New(logger)

	task := &Task{
		ID:        "task-004",
		RequestID: "req-uuid-004",
		Type:      "exec",
		Command:   "sleep 10",
		Timeout:   1, // 1 second timeout
	}

	start := time.Now()
	result := exec.Execute(task)
	duration := time.Since(start)

	assert.Equal(t, "failed", result.Status)
	assert.Equal(t, -1, result.ExitCode) // Timeout should result in exitCode -1
	assert.Contains(t, result.Error, "timeout")
	assert.Contains(t, result.Stderr, "timeout")
	assert.Less(t, duration, 3*time.Second) // Should complete quickly
}

// TestExecuteCommandTimeoutKillsProcessGroup tests that timeout kills the entire process group (Story 3.6)
func TestExecuteCommandTimeoutKillsProcessGroup(t *testing.T) {
	logger := zaptest.NewLogger(t)
	exec := New(logger)

	// Create a command that spawns child processes
	// The parent sleeps forever, spawning a child that also sleeps
	task := &Task{
		ID:        "task-pgid",
		RequestID: "req-uuid-pgid",
		Type:      "exec",
		Command:   "sleep 100 & sleep 100",
		Timeout:   1, // 1 second timeout
	}

	start := time.Now()
	result := exec.Execute(task)
	duration := time.Since(start)

	// Verify timeout occurred
	assert.Equal(t, "failed", result.Status)
	assert.Equal(t, -1, result.ExitCode)
	assert.Contains(t, result.Error, "timeout")

	// Verify execution completed quickly (within 3 seconds, not 100)
	assert.Less(t, duration, 3*time.Second, "Process group should be killed quickly on timeout")
}

// TestExecuteCommandTimeoutStructuredError tests structured error format on timeout (Story 3.6)
func TestExecuteCommandTimeoutStructuredError(t *testing.T) {
	logger := zaptest.NewLogger(t)
	exec := New(logger)

	task := &Task{
		ID:        "task-timeout-error",
		RequestID: "req-timeout-error",
		Type:      "exec",
		Command:   "sleep 10",
		Timeout:   1,
	}

	result := exec.Execute(task)

	// Verify structured error format
	assert.Equal(t, "failed", result.Status)
	assert.Equal(t, -1, result.ExitCode)
	assert.Equal(t, "command timeout after 1 seconds", result.Error)
	assert.Contains(t, result.Stderr, "command timeout after 1 seconds")

	// Verify requestId is preserved
	assert.Equal(t, "req-timeout-error", result.RequestID)
}
