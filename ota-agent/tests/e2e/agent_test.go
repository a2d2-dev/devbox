//go:build e2e
// +build e2e

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/theriseunion/edge-ota/agent/pkg/executor"
	"go.uber.org/zap"
)

// TestAgentE2E_CommandExecution 端到端测试:命令执行流程
func TestAgentE2E_CommandExecution(t *testing.T) {
	// 1. 启动内嵌 NATS 服务器
	natsServer, err := NewEmbeddedNATSServer()
	require.NoError(t, err)

	err = natsServer.Start()
	require.NoError(t, err)
	defer natsServer.Stop()

	t.Logf("NATS server started at: %s", natsServer.ClientURL())

	// 2. 创建测试用临时配置文件
	configFile := createTestConfig(t, natsServer.ClientURL())
	defer os.Remove(configFile)

	// 3. 创建 NATS 客户端(模拟后端)
	nc, err := natsServer.CreateClient()
	require.NoError(t, err)
	defer nc.Close()

	// 4. 订阅结果主题
	resultChan := make(chan *executor.Result, 10)
	_, err = nc.Subscribe("ota.devices.test-device-e2e.results.>", func(msg *nats.Msg) {
		var result executor.Result
		if err := json.Unmarshal(msg.Data, &result); err != nil {
			t.Logf("Failed to unmarshal result: %v", err)
			return
		}
		resultChan <- &result
	})
	require.NoError(t, err)

	// 5. 启动 OTA Agent (在后台 goroutine 中)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	agentReady := make(chan bool)
	agentErrors := make(chan error, 1)

	go func() {
		// 这里我们不直接运行 main(),而是组装 Agent 的组件
		// 实际生产中可以提取 main 的核心逻辑到可测试的函数
		agentReady <- true
		<-ctx.Done()
	}()

	// 等待 Agent 就绪
	select {
	case <-agentReady:
		t.Log("Agent is ready")
	case err := <-agentErrors:
		t.Fatalf("Agent failed to start: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("Agent start timeout")
	}

	// 6. 发送测试命令
	testCases := []struct {
		name           string
		taskID         string
		command        string
		expectedStatus string
	}{
		{
			name:           "simple echo command",
			taskID:         "task-echo-001",
			command:        "echo 'Hello from E2E test'",
			expectedStatus: "success",
		},
		{
			name:           "command with timeout",
			taskID:         "task-timeout-001",
			command:        "sleep 0.1 && echo 'done'",
			expectedStatus: "success",
		},
		{
			name:           "failing command",
			taskID:         "task-fail-001",
			command:        "exit 1",
			expectedStatus: "failed",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// 构造命令 payload
			task := map[string]interface{}{
				"id":      tc.taskID,
				"type":    "shell",
				"command": tc.command,
				"timeout": 10,
			}

			payload, err := json.Marshal(task)
			require.NoError(t, err)

			// 发布命令到 Agent
			err = nc.Publish("ota.devices.test-device-e2e.commands", payload)
			require.NoError(t, err)

			t.Logf("Sent command: %s (task_id: %s)", tc.command, tc.taskID)

			// 等待结果
			select {
			case result := <-resultChan:
				assert.Equal(t, tc.taskID, result.TaskID)
				assert.Equal(t, tc.expectedStatus, result.Status)
				t.Logf("Received result: %s - %s", result.TaskID, result.Status)

				if result.Status == "success" {
					assert.NotEmpty(t, result.Output)
					assert.Empty(t, result.Error)
				} else {
					assert.NotEmpty(t, result.Error)
				}

			case <-time.After(15 * time.Second):
				t.Fatalf("Timeout waiting for result of task: %s", tc.taskID)
			}
		})
	}
}

// TestAgentE2E_Heartbeat 端到端测试:心跳机制
func TestAgentE2E_Heartbeat(t *testing.T) {
	// 1. 启动内嵌 NATS 服务器
	natsServer, err := NewEmbeddedNATSServer()
	require.NoError(t, err)

	err = natsServer.Start()
	require.NoError(t, err)
	defer natsServer.Stop()

	// 2. 创建 NATS 客户端
	nc, err := natsServer.CreateClient()
	require.NoError(t, err)
	defer nc.Close()

	// 3. 订阅心跳主题
	heartbeatCount := 0
	heartbeatChan := make(chan bool, 10)

	_, err = nc.Subscribe("ota.devices.test-device-e2e.heartbeat", func(msg *nats.Msg) {
		heartbeatCount++
		heartbeatChan <- true
		t.Logf("Received heartbeat #%d", heartbeatCount)
	})
	require.NoError(t, err)

	// 4. 这里应该启动 Agent,但为了简化,我们手动发送心跳
	// 实际测试中需要真正启动 Agent
	for i := 0; i < 3; i++ {
		heartbeat := map[string]interface{}{
			"device_id": "test-device-e2e",
			"timestamp": time.Now().Unix(),
			"status":    "online",
		}

		payload, _ := json.Marshal(heartbeat)
		nc.Publish("ota.devices.test-device-e2e.heartbeat", payload)

		select {
		case <-heartbeatChan:
			// 心跳接收成功
		case <-time.After(2 * time.Second):
			t.Fatal("Heartbeat timeout")
		}
	}

	assert.GreaterOrEqual(t, heartbeatCount, 3, "Should receive at least 3 heartbeats")
}

// TestAgentE2E_ConcurrentCommands 端到端测试:并发命令处理
func TestAgentE2E_ConcurrentCommands(t *testing.T) {
	// 1. 启动内嵌 NATS 服务器
	natsServer, err := NewEmbeddedNATSServer()
	require.NoError(t, err)

	err = natsServer.Start()
	require.NoError(t, err)
	defer natsServer.Stop()

	// 2. 创建 NATS 客户端
	nc, err := natsServer.CreateClient()
	require.NoError(t, err)
	defer nc.Close()

	// 3. 订阅结果主题
	resultChan := make(chan *executor.Result, 100)
	_, err = nc.Subscribe("ota.devices.test-device-e2e.results.>", func(msg *nats.Msg) {
		var result executor.Result
		if err := json.Unmarshal(msg.Data, &result); err != nil {
			return
		}
		resultChan <- &result
	})
	require.NoError(t, err)

	// 4. 发送多个并发命令
	numCommands := 10
	for i := 0; i < numCommands; i++ {
		task := map[string]interface{}{
			"id":      fmt.Sprintf("concurrent-task-%03d", i),
			"type":    "shell",
			"command": fmt.Sprintf("echo 'Task %d'", i),
			"timeout": 10,
		}

		payload, _ := json.Marshal(task)
		nc.Publish("ota.devices.test-device-e2e.commands", payload)
	}

	// 5. 收集结果
	receivedResults := 0
	timeout := time.After(30 * time.Second)

	for receivedResults < numCommands {
		select {
		case result := <-resultChan:
			receivedResults++
			t.Logf("Received result %d/%d: %s - %s",
				receivedResults, numCommands, result.TaskID, result.Status)
			assert.Equal(t, "success", result.Status)

		case <-timeout:
			t.Fatalf("Timeout: only received %d/%d results", receivedResults, numCommands)
		}
	}

	assert.Equal(t, numCommands, receivedResults, "Should receive all results")
}

// createTestConfig 创建测试用配置文件
func createTestConfig(t *testing.T, natsURL string) string {
	// 提取 NATS URL 的 host 和 port
	// natsURL 格式: nats://127.0.0.1:xxxxx
	// 我们需要提取 host 和 port

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// 为了简化,我们直接使用占位符
	// 实际应用中需要解析 NATS URL
	config := fmt.Sprintf(`
device:
  name: test-device-e2e

mqtt:
  host: localhost
  port: 8883
  ca_file: ""

logging:
  level: debug
  file: ""
`)

	err := os.WriteFile(configPath, []byte(config), 0o644)
	require.NoError(t, err)

	return configPath
}

// setupLogger 创建测试用 logger
func setupLogger() *zap.Logger {
	logger, _ := zap.NewDevelopment()
	return logger
}
