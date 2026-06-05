//go:build unit
// +build unit

package reporter

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/theriseunion/edge-ota/agent/pkg/executor"
	"go.uber.org/zap/zaptest"
)

// MockMQTTClient 模拟 MQTT 客户端
type MockMQTTClient struct {
	publishedMessages map[string][]byte
	publishError      error
	deviceID          string
}

func NewMockMQTTClient(deviceID string) *MockMQTTClient {
	return &MockMQTTClient{
		publishedMessages: make(map[string][]byte),
		deviceID:          deviceID,
	}
}

func (m *MockMQTTClient) Publish(topic string, payload []byte) error {
	if m.publishError != nil {
		return m.publishError
	}
	m.publishedMessages[topic] = payload
	return nil
}

func (m *MockMQTTClient) GetDeviceResultTopic(taskID string) string {
	return "ota/nodes/" + m.deviceID + "/results/" + taskID
}

func (m *MockMQTTClient) GetPublishedPayload(topic string) []byte {
	return m.publishedMessages[topic]
}

func TestReportTaskResult_Success(t *testing.T) {
	logger := zaptest.NewLogger(t)
	mockClient := NewMockMQTTClient("test-device-001")

	reporter := &Reporter{
		mqttClient: mockClient,
		logger:     logger,
	}

	result := &executor.Result{
		TaskID:    "test-task-001",
		Status:    "success",
		Output:    "command executed successfully",
		Error:     "",
		StartTime: time.Now(),
		EndTime:   time.Now().Add(1 * time.Second),
		Duration:  1.0,
	}

	err := reporter.ReportTaskResult(result)

	require.NoError(t, err)

	// 验证消息已发布
	expectedTopic := "ota/nodes/test-device-001/results/test-task-001"
	payload := mockClient.GetPublishedPayload(expectedTopic)
	require.NotNil(t, payload)

	// 验证 JSON 序列化正确
	var reportedResult executor.Result
	err = json.Unmarshal(payload, &reportedResult)
	require.NoError(t, err)

	assert.Equal(t, result.TaskID, reportedResult.TaskID)
	assert.Equal(t, result.Status, reportedResult.Status)
	assert.Equal(t, result.Output, reportedResult.Output)
}

func TestReportTaskResult_Failed(t *testing.T) {
	logger := zaptest.NewLogger(t)
	mockClient := NewMockMQTTClient("test-device-002")

	reporter := &Reporter{
		mqttClient: mockClient,
		logger:     logger,
	}

	result := &executor.Result{
		TaskID:    "test-task-002",
		Status:    "failed",
		Output:    "",
		Error:     "command timeout after 10 seconds",
		StartTime: time.Now(),
		EndTime:   time.Now().Add(10 * time.Second),
		Duration:  10.0,
	}

	err := reporter.ReportTaskResult(result)

	require.NoError(t, err)

	// 验证错误信息正确序列化
	expectedTopic := "ota/nodes/test-device-002/results/test-task-002"
	payload := mockClient.GetPublishedPayload(expectedTopic)

	var reportedResult executor.Result
	json.Unmarshal(payload, &reportedResult)

	assert.Equal(t, "failed", reportedResult.Status)
	assert.Contains(t, reportedResult.Error, "timeout")
}

func TestReportTaskResult_PublishError(t *testing.T) {
	logger := zaptest.NewLogger(t)
	mockClient := NewMockMQTTClient("test-device-003")
	mockClient.publishError = errors.New("mqtt connection lost")

	reporter := &Reporter{
		mqttClient: mockClient,
		logger:     logger,
	}

	result := &executor.Result{
		TaskID: "test-task-003",
		Status: "success",
		Output: "test output",
	}

	err := reporter.ReportTaskResult(result)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to publish result")
	assert.Contains(t, err.Error(), "mqtt connection lost")
}

func TestReporter_MultipleResults(t *testing.T) {
	logger := zaptest.NewLogger(t)
	mockClient := NewMockMQTTClient("test-device-004")

	reporter := &Reporter{
		mqttClient: mockClient,
		logger:     logger,
	}

	// 上报多个结果
	results := []*executor.Result{
		{TaskID: "task-1", Status: "success", Output: "output1"},
		{TaskID: "task-2", Status: "success", Output: "output2"},
		{TaskID: "task-3", Status: "failed", Error: "error3"},
	}

	for _, result := range results {
		err := reporter.ReportTaskResult(result)
		require.NoError(t, err)
	}

	// 验证所有结果都已发布
	assert.Len(t, mockClient.publishedMessages, 3)

	// 验证每个任务的主题正确
	for _, result := range results {
		expectedTopic := "ota/nodes/test-device-004/results/" + result.TaskID
		payload := mockClient.GetPublishedPayload(expectedTopic)
		assert.NotNil(t, payload, "Missing payload for task: %s", result.TaskID)
	}
}

func TestNew_NilLogger(t *testing.T) {
	mockClient := NewMockMQTTClient("test-device")

	reporter := New(mockClient, nil)

	assert.NotNil(t, reporter)
	assert.NotNil(t, reporter.logger, "Should create default logger")
	assert.Equal(t, mockClient, reporter.mqttClient)
}
