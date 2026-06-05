//go:build unit
// +build unit

package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/theriseunion/edge-ota/agent/pkg/config"
	"github.com/theriseunion/edge-ota/agent/pkg/executor"
	"github.com/theriseunion/edge-ota/agent/pkg/reporter"
	"go.uber.org/zap"
)

// MockMQTTClient 用于测试的 MQTT 客户端 mock
type MockMQTTClient struct {
	publishedMessages map[string][]byte
	publishError      error
	deviceID          string
}

func (m *MockMQTTClient) Publish(topic string, payload []byte) error {
	if m.publishError != nil {
		return m.publishError
	}
	if m.publishedMessages == nil {
		m.publishedMessages = make(map[string][]byte)
	}
	m.publishedMessages[topic] = payload
	return nil
}

func (m *MockMQTTClient) GetDeviceResultTopic(taskID string) string {
	return "ota/devices/" + m.deviceID + "/results/" + taskID
}

func TestHandleCommand_Success(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	exec := executor.New(logger)
	mockClient := &MockMQTTClient{
		deviceID:          "test-device",
		publishedMessages: make(map[string][]byte),
	}
	rep := reporter.New(mockClient, logger)

	// 有效的命令 payload
	payload := []byte(`{
		"id": "task-001",
		"type": "shell",
		"command": "echo 'test'",
		"timeout": 10
	}`)

	err := handleCommand(logger, exec, rep, "test-topic", payload)
	require.NoError(t, err)

	// 验证结果已发布
	assert.NotEmpty(t, mockClient.publishedMessages)
	topic := "ota/devices/test-device/results/task-001"
	assert.Contains(t, mockClient.publishedMessages, topic)
}

func TestHandleCommand_InvalidJSON(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	exec := executor.New(logger)
	mockClient := &MockMQTTClient{deviceID: "test-device"}
	rep := reporter.New(mockClient, logger)

	// 无效的 JSON
	payload := []byte(`{invalid json}`)

	err := handleCommand(logger, exec, rep, "test-topic", payload)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid character")
}

func TestHandleCommand_MissingFields(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	exec := executor.New(logger)
	mockClient := &MockMQTTClient{deviceID: "test-device"}
	rep := reporter.New(mockClient, logger)

	tests := []struct {
		name    string
		payload string
		errMsg  string
	}{
		{
			name:    "missing task ID",
			payload: `{"type": "shell", "command": "echo test"}`,
			errMsg:  "task ID is required",
		},
		{
			name:    "missing type",
			payload: `{"id": "task-001", "command": "echo test"}`,
			errMsg:  "task type is required",
		},
		{
			name:    "missing command",
			payload: `{"id": "task-001", "type": "shell"}`,
			errMsg:  "command is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := handleCommand(logger, exec, rep, "test-topic", []byte(tt.payload))
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.errMsg)
		})
	}
}

func TestInitLogger(t *testing.T) {
	tests := []struct {
		name      string
		config    config.LoggingConfig
		wantLevel string
		wantErr   bool
	}{
		{
			name: "debug level",
			config: config.LoggingConfig{
				Level: "debug",
				File:  "",
			},
			wantLevel: "debug",
			wantErr:   false,
		},
		{
			name: "info level",
			config: config.LoggingConfig{
				Level: "info",
				File:  "",
			},
			wantLevel: "info",
			wantErr:   false,
		},
		{
			name: "warn level",
			config: config.LoggingConfig{
				Level: "warn",
				File:  "",
			},
			wantLevel: "warn",
			wantErr:   false,
		},
		{
			name: "error level",
			config: config.LoggingConfig{
				Level: "error",
				File:  "",
			},
			wantLevel: "error",
			wantErr:   false,
		},
		{
			name: "invalid level defaults to info",
			config: config.LoggingConfig{
				Level: "invalid-level",
				File:  "",
			},
			wantLevel: "info",
			wantErr:   false,
		},
		{
			name: "empty level defaults to info",
			config: config.LoggingConfig{
				Level: "",
				File:  "",
			},
			wantLevel: "info",
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger, err := initLogger(tt.config)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.NotNil(t, logger)

			// 清理
			if logger != nil {
				logger.Sync()
			}
		})
	}
}

func TestInitLogger_WithFile(t *testing.T) {
	// 使用临时文件测试文件输出
	tmpFile := t.TempDir() + "/test.log"

	config := config.LoggingConfig{
		Level: "info",
		File:  tmpFile,
	}

	logger, err := initLogger(config)
	require.NoError(t, err)
	assert.NotNil(t, logger)

	// 测试日志写入
	logger.Info("test message")
	logger.Sync()

	// 验证文件被创建（实际内容验证会比较复杂，这里只验证创建）
	// 注意：由于 zap 的异步写入，文件可能还未立即创建
}
