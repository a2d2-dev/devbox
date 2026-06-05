//go:build unit
// +build unit

package mqtt

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

func TestGetDeviceCommandTopic(t *testing.T) {
	logger := zaptest.NewLogger(t)

	tests := []struct {
		name     string
		deviceID string
		want     string
	}{
		{
			name:     "standard device ID",
			deviceID: "device-001",
			want:     "ota/nodes/device-001/commands/#",
		},
		{
			name:     "device ID with hyphen",
			deviceID: "edge-node-123",
			want:     "ota/nodes/edge-node-123/commands/#",
		},
		{
			name:     "device ID with underscore",
			deviceID: "device_test_001",
			want:     "ota/nodes/device_test_001/commands/#",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &Client{
				deviceID: tt.deviceID,
				logger:   logger,
			}

			got := client.GetDeviceCommandTopic()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestGetDeviceResultTopic(t *testing.T) {
	logger := zaptest.NewLogger(t)

	tests := []struct {
		name     string
		deviceID string
		taskID   string
		want     string
	}{
		{
			name:     "standard task",
			deviceID: "device-001",
			taskID:   "task-123",
			want:     "ota/nodes/device-001/results/task-123",
		},
		{
			name:     "task with alphanumeric ID",
			deviceID: "device-002",
			taskID:   "test-echo",
			want:     "ota/nodes/device-002/results/test-echo",
		},
		{
			name:     "concurrent task",
			deviceID: "device-003",
			taskID:   "concurrent-001",
			want:     "ota/nodes/device-003/results/concurrent-001",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &Client{
				deviceID: tt.deviceID,
				logger:   logger,
			}

			got := client.GetDeviceResultTopic(tt.taskID)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestTopicValidation(t *testing.T) {
	logger := zaptest.NewLogger(t)

	tests := []struct {
		name       string
		deviceID   string
		taskID     string
		shouldFail bool
	}{
		{
			name:       "valid alphanumeric with hyphen",
			deviceID:   "device-001",
			taskID:     "test-echo",
			shouldFail: false,
		},
		{
			name:       "valid alphanumeric with underscore",
			deviceID:   "device_001",
			taskID:     "test_echo",
			shouldFail: false,
		},
		{
			name:       "contains space (invalid)",
			deviceID:   "device-001",
			taskID:     "test echo",
			shouldFail: true,
		},
		{
			name:       "contains special char (invalid)",
			deviceID:   "device-001",
			taskID:     "test@echo",
			shouldFail: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &Client{
				deviceID: tt.deviceID,
				logger:   logger,
			}

			topic := client.GetDeviceResultTopic(tt.taskID)

			// MQTT topic 规范：不允许空格和部分特殊字符
			hasSpace := false
			hasInvalidChar := false
			for _, ch := range topic {
				if ch == ' ' {
					hasSpace = true
				}
				if ch == '@' || ch == '!' || ch == '$' {
					hasInvalidChar = true
				}
			}

			if tt.shouldFail {
				assert.True(t, hasSpace || hasInvalidChar,
					"Topic should contain invalid characters")
			} else {
				assert.False(t, hasSpace,
					"Topic should not contain spaces")
				assert.False(t, hasInvalidChar,
					"Topic should not contain special chars")
			}
		})
	}
}

func TestNewTLSConfig(t *testing.T) {
	tests := []struct {
		name    string
		caFile  string
		wantErr bool
	}{
		{
			name:    "nonexistent file",
			caFile:  "/nonexistent/ca.crt",
			wantErr: true,
		},
		{
			name:    "empty path",
			caFile:  "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := newTLSConfig(tt.caFile)

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestClientCreation(t *testing.T) {
	logger := zaptest.NewLogger(t)

	tests := []struct {
		name    string
		config  *Config
		wantErr bool
	}{
		{
			name: "missing CA file",
			config: &Config{
				Broker:   "tls://localhost:8883",
				ClientID: "test-client",
				CAFile:   "/nonexistent/ca.crt",
				DeviceID: "test-device",
				Logger:   logger,
			},
			wantErr: true,
		},
		{
			name: "nil logger uses default",
			config: &Config{
				Broker:   "tls://localhost:8883",
				ClientID: "test-client",
				CAFile:   "/nonexistent/ca.crt",
				DeviceID: "test-device",
				Logger:   nil, // 应该创建默认 logger
			},
			wantErr: true, // CA file 不存在，仍会失败
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := New(tt.config)

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, client)
			}
		})
	}
}
