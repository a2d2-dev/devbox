package reporter

import (
	"encoding/json"
	"fmt"

	"github.com/theriseunion/edge-ota/agent/pkg/executor"
	"go.uber.org/zap"
)

// MQTTPublisher 定义 MQTT 发布接口（用于测试和解耦）
type MQTTPublisher interface {
	Publish(topic string, payload []byte) error
	GetDeviceResultTopic(taskID string) string
}

// Reporter 结果上报器
type Reporter struct {
	mqttClient MQTTPublisher
	logger     *zap.Logger
}

// New 创建结果上报器
func New(mqttClient MQTTPublisher, logger *zap.Logger) *Reporter {
	if logger == nil {
		logger, _ = zap.NewProduction()
	}

	return &Reporter{
		mqttClient: mqttClient,
		logger:     logger,
	}
}

// ReportTaskResult 上报任务执行结果
func (r *Reporter) ReportTaskResult(result *executor.Result) error {
	r.logger.Info("Reporting task result",
		zap.String("task_id", result.TaskID),
		zap.String("status", result.Status))

	// 序列化结果为 JSON
	payload, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("failed to marshal result: %w", err)
	}

	// 获取结果主题
	topic := r.mqttClient.GetDeviceResultTopic(result.TaskID)

	// 发布到 MQTT
	if err := r.mqttClient.Publish(topic, payload); err != nil {
		return fmt.Errorf("failed to publish result: %w", err)
	}

	r.logger.Info("Successfully reported task result",
		zap.String("task_id", result.TaskID),
		zap.String("topic", topic))

	return nil
}
