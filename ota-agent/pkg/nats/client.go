package nats

import (
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"
)

// NATSClient NATS 客户端封装
type NATSClient struct {
	conn     *nats.Conn
	deviceID string
	logger   *zap.Logger
}

// Config NATS 客户端配置
type Config struct {
	URL      string      // NATS 服务器 URL
	ClientID string      // 客户端 ID
	DeviceID string      // 设备 ID
	Logger   *zap.Logger // 日志记录器
}

// MessageHandler 消息处理函数类型
type MessageHandler func(topic string, payload []byte) error

// New 创建 NATS 客户端
func New(cfg *Config) (*NATSClient, error) {
	if cfg.Logger == nil {
		logger, _ := zap.NewProduction()
		cfg.Logger = logger
	}

	// 创建连接选项
	opts := []nats.Option{
		nats.Name(cfg.ClientID),
		nats.ReconnectWait(2 * time.Second),
		nats.MaxReconnects(5),
		nats.Timeout(10 * time.Second),
	}

	// 连接到 NATS
	conn, err := nats.Connect(cfg.URL, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to NATS: %w", err)
	}

	return &NATSClient{
		conn:     conn,
		deviceID: cfg.DeviceID,
		logger:   cfg.Logger,
	}, nil
}

// Disconnect 断开连接
func (c *NATSClient) Disconnect() {
	c.logger.Info("Disconnecting from NATS")
	c.conn.Close()
}

// IsConnected 检查连接状态
func (c *NATSClient) IsConnected() bool {
	return c.conn.IsConnected()
}

// Subscribe 订阅主题
func (c *NATSClient) Subscribe(topic string, handler MessageHandler) error {
	c.logger.Info("Subscribing to topic",
		zap.String("topic", topic))

	_, err := c.conn.Subscribe(topic, func(msg *nats.Msg) {
		c.logger.Debug("Message received",
			zap.String("topic", msg.Subject),
			zap.Int("payload_size", len(msg.Data)))

		if err := handler(msg.Subject, msg.Data); err != nil {
			c.logger.Error("Failed to handle message",
				zap.String("topic", msg.Subject),
				zap.Error(err))
		}
	})

	if err != nil {
		return fmt.Errorf("failed to subscribe to %s: %w", topic, err)
	}

	c.logger.Info("Successfully subscribed to topic",
		zap.String("topic", topic))
	return nil
}

// Publish 发布消息
func (c *NATSClient) Publish(topic string, payload []byte) error {
	c.logger.Debug("Publishing message",
		zap.String("topic", topic),
		zap.Int("payload_size", len(payload)))

	err := c.conn.Publish(topic, payload)
	if err != nil {
		return fmt.Errorf("failed to publish to %s: %w", topic, err)
	}

	c.logger.Debug("Successfully published message",
		zap.String("topic", topic))
	return nil
}

// PublishHeartbeat 发布心跳消息
func (c *NATSClient) PublishHeartbeat() error {
	topic := fmt.Sprintf("ota.nodes.%s.heartbeat", c.deviceID)

	payload := []byte(fmt.Sprintf(`{"timestamp":"%s","status":"online"}`,
		time.Now().Format(time.RFC3339)))

	return c.Publish(topic, payload)
}

// GetDeviceCommandTopic 获取设备命令主题
func (c *NATSClient) GetDeviceCommandTopic() string {
	return fmt.Sprintf("ota.nodes.%s.commands.>", c.deviceID)
}

// GetDeviceResultTopic 获取设备结果主题
func (c *NATSClient) GetDeviceResultTopic(taskID string) string {
	return fmt.Sprintf("ota.nodes.%s.results.%s", c.deviceID, taskID)
}
