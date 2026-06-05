//go:build integration
// +build integration

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/theriseunion/edge-ota/agent/pkg/config"
	"github.com/theriseunion/edge-ota/agent/pkg/executor"
	"github.com/theriseunion/edge-ota/agent/pkg/reporter"
	"go.uber.org/zap"
)

// MockMQTTBroker 模拟 MQTT Broker 用于集成测试
type MockMQTTBroker struct {
	mu                sync.RWMutex
	commandHandlers   map[string]func([]byte) error
	publishedMessages map[string][]byte
	heartbeats        []map[string]interface{}
}

// NewMockMQTTBroker 创建模拟 MQTT Broker
func NewMockMQTTBroker() *MockMQTTBroker {
	return &MockMQTTBroker{
		commandHandlers:   make(map[string]func([]byte) error),
		publishedMessages: make(map[string][]byte),
		heartbeats:        make([]map[string]interface{}, 0),
	}
}

// Subscribe 订阅主题
func (b *MockMQTTBroker) Subscribe(topic string, handler func([]byte) error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.commandHandlers[topic] = handler
}

// Publish 发布消息
func (b *MockMQTTBroker) Publish(topic string, payload []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.publishedMessages[topic] = payload

	// 如果是心跳消息,解析并存储
	if topic == "ota/devices/test-device/heartbeat" {
		var hb map[string]interface{}
		if err := json.Unmarshal(payload, &hb); err == nil {
			b.heartbeats = append(b.heartbeats, hb)
		}
	}

	return nil
}

// GetDeviceResultTopic 获取结果主题
func (b *MockMQTTBroker) GetDeviceResultTopic(taskID string) string {
	return fmt.Sprintf("ota/devices/test-device/results/%s", taskID)
}

// SendCommand 发送命令到 Agent
func (b *MockMQTTBroker) SendCommand(payload []byte) error {
	b.mu.RLock()
	handler, ok := b.commandHandlers["ota/devices/test-device/commands"]
	b.mu.RUnlock()

	if !ok {
		return fmt.Errorf("no handler for command topic")
	}

	return handler(payload)
}

// GetPublishedMessage 获取发布的消息
func (b *MockMQTTBroker) GetPublishedMessage(topic string) ([]byte, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	msg, ok := b.publishedMessages[topic]
	return msg, ok
}

// GetHeartbeatCount 获取心跳数量
func (b *MockMQTTBroker) GetHeartbeatCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.heartbeats)
}

// TestIntegration_AgentWorkflow 集成测试: Agent 完整工作流
func TestIntegration_AgentWorkflow(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	// 1. 创建模拟 MQTT Broker
	broker := NewMockMQTTBroker()

	// 2. 创建 Agent 组件
	exec := executor.New(logger)
	rep := reporter.New(broker, logger)

	// 3. 设置命令处理器
	broker.Subscribe("ota/devices/test-device/commands", func(payload []byte) error {
		// 解析任务
		task, err := executor.ParseTask(payload)
		if err != nil {
			return err
		}

		// 执行任务
		result := exec.Execute(task)

		// 上报结果
		return rep.ReportTaskResult(result)
	})

	// 4. 测试用例
	testCases := []struct {
		name           string
		taskID         string
		command        string
		timeout        int
		expectedStatus string
		checkOutput    func(*testing.T, string)
	}{
		{
			name:           "simple echo",
			taskID:         "integration-001",
			command:        "echo 'Integration Test'",
			timeout:        10,
			expectedStatus: "success",
			checkOutput: func(t *testing.T, output string) {
				assert.Contains(t, output, "Integration Test")
			},
		},
		{
			name:           "pipe command",
			taskID:         "integration-002",
			command:        "echo 'line1\nline2' | wc -l",
			timeout:        10,
			expectedStatus: "success",
			checkOutput: func(t *testing.T, output string) {
				assert.Contains(t, output, "2")
			},
		},
		{
			name:           "timeout command",
			taskID:         "integration-003",
			command:        "sleep 10",
			timeout:        1,
			expectedStatus: "failed",
			checkOutput: func(t *testing.T, output string) {
				// 超时命令应该有错误
			},
		},
		{
			name:           "failing command",
			taskID:         "integration-004",
			command:        "exit 42",
			timeout:        10,
			expectedStatus: "failed",
			checkOutput: func(t *testing.T, output string) {
				// 失败命令
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// 构造命令
			task := map[string]interface{}{
				"id":      tc.taskID,
				"type":    "shell",
				"command": tc.command,
				"timeout": tc.timeout,
			}

			payload, err := json.Marshal(task)
			require.NoError(t, err)

			// 发送命令
			err = broker.SendCommand(payload)
			require.NoError(t, err)

			// 等待结果发布
			time.Sleep(time.Duration(tc.timeout+2) * time.Second)

			// 验证结果
			resultTopic := broker.GetDeviceResultTopic(tc.taskID)
			resultPayload, ok := broker.GetPublishedMessage(resultTopic)
			require.True(t, ok, "Result should be published")

			var result executor.Result
			err = json.Unmarshal(resultPayload, &result)
			require.NoError(t, err)

			assert.Equal(t, tc.taskID, result.TaskID)
			assert.Equal(t, tc.expectedStatus, result.Status)

			if tc.checkOutput != nil {
				tc.checkOutput(t, result.Output)
			}
		})
	}
}

// TestIntegration_ConcurrentTasks 集成测试:并发任务处理
func TestIntegration_ConcurrentTasks(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	// 创建模拟 Broker 和组件
	broker := NewMockMQTTBroker()
	exec := executor.New(logger)
	rep := reporter.New(broker, logger)

	// 设置命令处理器
	var wg sync.WaitGroup
	broker.Subscribe("ota/devices/test-device/commands", func(payload []byte) error {
		defer wg.Done()

		task, err := executor.ParseTask(payload)
		if err != nil {
			return err
		}

		result := exec.Execute(task)
		return rep.ReportTaskResult(result)
	})

	// 发送多个并发任务
	numTasks := 10
	wg.Add(numTasks)

	start := time.Now()

	for i := 0; i < numTasks; i++ {
		task := map[string]interface{}{
			"id":      fmt.Sprintf("concurrent-%03d", i),
			"type":    "shell",
			"command": fmt.Sprintf("echo 'Task %d' && sleep 0.1", i),
			"timeout": 10,
		}

		payload, _ := json.Marshal(task)
		go broker.SendCommand(payload)
	}

	// 等待所有任务完成
	wg.Wait()
	elapsed := time.Since(start)

	t.Logf("Processed %d tasks in %v", numTasks, elapsed)

	// 验证所有结果都已发布
	for i := 0; i < numTasks; i++ {
		taskID := fmt.Sprintf("concurrent-%03d", i)
		topic := broker.GetDeviceResultTopic(taskID)
		_, ok := broker.GetPublishedMessage(topic)
		assert.True(t, ok, "Result for task %s should be published", taskID)
	}

	// 并发执行应该远快于串行(10个任务串行至少1秒)
	assert.Less(t, elapsed, 5*time.Second, "Concurrent execution should be fast")
}

// TestIntegration_ConfigLoading 集成测试:配置加载
func TestIntegration_ConfigLoading(t *testing.T) {
	// 创建临时配置文件
	tmpFile, err := os.CreateTemp("", "config-integration-*.yaml")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	configContent := `
device:
  name: integration-test-device

mqtt:
  host: test.mqtt.com
  port: 8883
  ca_file: ""
  client_id: test-client-123

logging:
  level: debug
  file: ""
`

	_, err = tmpFile.Write([]byte(configContent))
	require.NoError(t, err)
	tmpFile.Close()

	// 加载配置
	cfg, err := config.Load(tmpFile.Name())
	require.NoError(t, err)

	// 验证配置
	assert.Equal(t, "integration-test-device", cfg.Device.Name)
	assert.Equal(t, "test.mqtt.com", cfg.MQTT.Host)
	assert.Equal(t, 8883, cfg.MQTT.Port)
	assert.Equal(t, "test-client-123", cfg.MQTT.ClientID)
	assert.Equal(t, "debug", cfg.Logging.Level)

	// 验证生成的 MQTT Broker URL
	assert.Equal(t, "tls://test.mqtt.com:8883", cfg.GetMQTTBroker())

	// 验证 Client ID
	assert.Equal(t, "test-client-123", cfg.GetClientID())
}

// TestIntegration_HeartbeatMechanism 集成测试:心跳机制
func TestIntegration_HeartbeatMechanism(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	broker := NewMockMQTTBroker()

	// 模拟心跳发送
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	heartbeatInterval := 500 * time.Millisecond
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				heartbeat := map[string]interface{}{
					"device_id": "test-device",
					"timestamp": time.Now().Unix(),
					"status":    "online",
				}

				payload, _ := json.Marshal(heartbeat)
				broker.Publish("ota/devices/test-device/heartbeat", payload)
			}
		}
	}()

	// 等待一段时间
	<-ctx.Done()

	// 验证心跳数量
	count := broker.GetHeartbeatCount()
	logger.Info("Heartbeat count", zap.Int("count", count))

	// 5秒内以500ms间隔应该至少有8个心跳(允许一些延迟)
	assert.GreaterOrEqual(t, count, 8, "Should have at least 8 heartbeats in 5 seconds")
}

// TestIntegration_ErrorRecovery 集成测试:错误恢复
func TestIntegration_ErrorRecovery(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	broker := NewMockMQTTBroker()
	exec := executor.New(logger)
	rep := reporter.New(broker, logger)

	// 测试无效 JSON
	invalidJSON := []byte(`{invalid json}`)
	task, err := executor.ParseTask(invalidJSON)
	assert.Error(t, err)
	assert.Nil(t, task)

	// 测试缺失字段
	missingFields := []byte(`{"id": "test-001"}`)
	task, err = executor.ParseTask(missingFields)
	assert.Error(t, err)

	// 测试正常任务在错误后仍能工作
	validTask := []byte(`{"id": "test-002", "type": "shell", "command": "echo ok", "timeout": 10}`)
	task, err = executor.ParseTask(validTask)
	require.NoError(t, err)

	result := exec.Execute(task)
	assert.Equal(t, "success", result.Status)

	err = rep.ReportTaskResult(result)
	assert.NoError(t, err)
}

// TestIntegration_CommandTimeout 集成测试: 命令超时 (Story 3.6 AC#1)
// 验证: Agent 在超时后杀死命令进程并返回结构化错误
func TestIntegration_CommandTimeout(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	exec := executor.New(logger)

	// 创建一个会超时的任务
	task := &executor.Task{
		ID:        "timeout-test",
		RequestID: "req-timeout",
		Type:      "exec",
		Command:   "sleep 10", // 10秒的命令
		Timeout:   1,          // 1秒超时
	}

	start := time.Now()
	result := exec.Execute(task)
	duration := time.Since(start)

	// 验证超时行为
	assert.Equal(t, "failed", result.Status)
	assert.Equal(t, -1, result.ExitCode)
	assert.Contains(t, result.Error, "timeout")
	assert.Equal(t, "req-timeout", result.RequestID)

	// 验证超时后快速返回（不等待10秒）
	assert.Less(t, duration, 3*time.Second, "Should return quickly after timeout")

	logger.Info("Timeout test completed",
		zap.Duration("duration", duration),
		zap.String("error", result.Error))
}

// TestIntegration_ProcessGroupKill 集成测试: 进程组杀死 (Story 3.6 AC#1)
// 验证: 超时时杀死整个进程组，包括子进程
func TestIntegration_ProcessGroupKill(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	exec := executor.New(logger)

	// 创建一个会产生子进程的任务
	task := &executor.Task{
		ID:        "pgid-test",
		RequestID: "req-pgid",
		Type:      "exec",
		Command:   "sleep 100 & sleep 100", // 产生后台子进程
		Timeout:   1,
	}

	start := time.Now()
	result := exec.Execute(task)
	duration := time.Since(start)

	// 验证超时行为
	assert.Equal(t, "failed", result.Status)
	assert.Equal(t, -1, result.ExitCode)

	// 验证进程组被杀死（不等待100秒）
	assert.Less(t, duration, 3*time.Second, "Process group should be killed quickly")

	logger.Info("Process group kill test completed",
		zap.Duration("duration", duration))
}

// TestIntegration_NonZeroExitCode 集成测试: 非零退出码 (Story 3.6 AC#4)
// 验证: Agent 正确捕获非零退出码和 stderr
func TestIntegration_NonZeroExitCode(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	exec := executor.New(logger)

	tests := []struct {
		name         string
		command      string
		expectedCode int
		expectStderr bool
	}{
		{
			name:         "exit 1",
			command:      "exit 1",
			expectedCode: 1,
			expectStderr: false,
		},
		{
			name:         "command not found",
			command:      "nonexistent-command-xyz",
			expectedCode: 127,
			expectStderr: true,
		},
		{
			name:         "cat nonexistent file",
			command:      "cat /nonexistent/file/path",
			expectedCode: 1,
			expectStderr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := &executor.Task{
				ID:        "exitcode-test",
				RequestID: "req-exitcode",
				Type:      "exec",
				Command:   tt.command,
				Timeout:   10,
			}

			result := exec.Execute(task)

			assert.Equal(t, "failed", result.Status)
			assert.Equal(t, tt.expectedCode, result.ExitCode)

			if tt.expectStderr {
				assert.NotEmpty(t, result.Stderr, "Should capture stderr")
			}

			logger.Info("Exit code test",
				zap.String("command", tt.command),
				zap.Int("exit_code", result.ExitCode),
				zap.String("stderr", result.Stderr))
		})
	}
}

// TestIntegration_MixedResults 集成测试: 混合成功/失败 (Story 3.6 AC#4)
// 验证: 批量执行中单个失败不影响其他任务
func TestIntegration_MixedResults(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	exec := executor.New(logger)

	tasks := []*executor.Task{
		{ID: "mixed-1", Type: "exec", Command: "echo success1", Timeout: 10},
		{ID: "mixed-2", Type: "exec", Command: "exit 1", Timeout: 10},
		{ID: "mixed-3", Type: "exec", Command: "echo success2", Timeout: 10},
		{ID: "mixed-4", Type: "exec", Command: "sleep 10", Timeout: 1}, // 超时
		{ID: "mixed-5", Type: "exec", Command: "echo success3", Timeout: 10},
	}

	results := make([]*executor.Result, len(tasks))

	// 顺序执行所有任务
	for i, task := range tasks {
		results[i] = exec.Execute(task)
	}

	// 验证结果
	assert.Equal(t, "success", results[0].Status)
	assert.Equal(t, 0, results[0].ExitCode)

	assert.Equal(t, "failed", results[1].Status)
	assert.Equal(t, 1, results[1].ExitCode)

	assert.Equal(t, "success", results[2].Status)
	assert.Equal(t, 0, results[2].ExitCode)

	assert.Equal(t, "failed", results[3].Status)
	assert.Equal(t, -1, results[3].ExitCode) // 超时

	assert.Equal(t, "success", results[4].Status)
	assert.Equal(t, 0, results[4].ExitCode)

	// 统计
	successCount := 0
	failedCount := 0
	for _, r := range results {
		if r.Status == "success" {
			successCount++
		} else {
			failedCount++
		}
	}

	assert.Equal(t, 3, successCount)
	assert.Equal(t, 2, failedCount)

	logger.Info("Mixed results test completed",
		zap.Int("success", successCount),
		zap.Int("failed", failedCount))
}
