package mqtt

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"go.uber.org/zap"
)

// PublishRetryConfig 发布重试配置
type PublishRetryConfig struct {
	MaxRetries    int           // 最大重试次数
	RetryInterval time.Duration // 重试间隔
	BufferSize    int           // 缓冲队列大小
}

// DefaultPublishRetryConfig 默认发布重试配置
var DefaultPublishRetryConfig = PublishRetryConfig{
	MaxRetries:    3,
	RetryInterval: time.Second,
	BufferSize:    100,
}

// Client MQTT 客户端封装
type Client struct {
	client      mqtt.Client
	deviceID    string
	logger      *zap.Logger
	buffer      *ResultBuffer      // Story 3.6: 结果缓冲队列
	retryConfig PublishRetryConfig // 重试配置
}

// Config MQTT 客户端配置
type Config struct {
	Broker   string      // MQTT Broker URL (tls://host:port)
	ClientID string      // MQTT Client ID
	CAFile   string      // CA 证书文件路径
	DeviceID string      // 设备 ID
	Logger   *zap.Logger // 日志记录器
	Auth     *AuthConfig // 认证配置
}

// AuthConfig 认证配置
type AuthConfig struct {
	Type     string // 认证类型: "token" 或 "userpass"
	Token    string // Token认证（type=token时使用）
	Username string // 用户名（type=userpass时使用）
	Password string // 密码（type=userpass时使用）
}

// MessageHandler 消息处理函数类型
type MessageHandler func(topic string, payload []byte) error

// HeartbeatMessage 心跳消息结构
type HeartbeatMessage struct {
	DeviceID     string `json:"device_id"`
	Timestamp    int64  `json:"timestamp"`
	IP           string `json:"ip"`
	OS           string `json:"os"`
	Architecture string `json:"arch"`
	Hostname     string `json:"hostname"`      // 主机名
	Status       string `json:"status"`
}

// New 创建 MQTT 客户端
func New(cfg *Config) (*Client, error) {
	if cfg.Logger == nil {
		logger, _ := zap.NewProduction()
		cfg.Logger = logger
	}

	// 创建 MQTT 客户端选项
	opts := mqtt.NewClientOptions()
	opts.AddBroker(cfg.Broker)
	opts.SetClientID(cfg.ClientID)

	// 配置认证
	if cfg.Auth != nil {
		if cfg.Auth.Type == "token" {
			// Token认证: username="token", password=<actual-token>
			opts.SetUsername("token")
			opts.SetPassword(cfg.Auth.Token)
			cfg.Logger.Info("Using token authentication",
				zap.String("auth_type", "token"))
		} else if cfg.Auth.Type == "userpass" {
			// Username/Password认证
			opts.SetUsername(cfg.Auth.Username)
			opts.SetPassword(cfg.Auth.Password)
			cfg.Logger.Info("Using username/password authentication",
				zap.String("auth_type", "userpass"),
				zap.String("username", cfg.Auth.Username))
		}
	} else {
		// 无认证配置：不设置 username/password，使用真正的匿名连接
		// 注意：不能设置 SetUsername("anonymous")，NATS MQTT 会 hang 在无认证模式下收到 username 字段
		cfg.Logger.Warn("No authentication configured - using anonymous access")
	}

	// 只有提供 CA 文件时才启用 TLS
	if cfg.CAFile != "" {
		tlsConfig, err := newTLSConfig(cfg.CAFile)
		if err != nil {
			return nil, fmt.Errorf("failed to create TLS config: %w", err)
		}
		opts.SetTLSConfig(tlsConfig)
	}
	opts.SetCleanSession(true)
	opts.SetAutoReconnect(true)
	opts.SetConnectRetry(true)
	opts.SetConnectRetryInterval(5 * time.Second)
	opts.SetMaxReconnectInterval(30 * time.Second)

	opts.SetConnectionLostHandler(func(c mqtt.Client, err error) {
		cfg.Logger.Warn("MQTT connection lost",
			zap.Error(err),
			zap.String("broker", cfg.Broker))
	})

	opts.SetReconnectingHandler(func(c mqtt.Client, opts *mqtt.ClientOptions) {
		cfg.Logger.Info("MQTT reconnecting...",
			zap.String("broker", cfg.Broker))
	})

	// 创建 Client 实例（需要在设置 OnConnectHandler 之前创建，以便闭包引用）
	client := &Client{
		deviceID:    cfg.DeviceID,
		logger:      cfg.Logger,
		retryConfig: DefaultPublishRetryConfig,
		buffer:      NewResultBuffer(DefaultPublishRetryConfig.BufferSize, cfg.Logger),
	}

	// Story 3.6: 在连接恢复时刷新缓冲区
	opts.SetOnConnectHandler(func(c mqtt.Client) {
		cfg.Logger.Info("MQTT connected",
			zap.String("broker", cfg.Broker),
			zap.String("client_id", cfg.ClientID))

		// 异步刷新缓冲区，避免阻塞连接回调
		go client.flushBuffer()
	})

	// 创建 MQTT 客户端
	client.client = mqtt.NewClient(opts)

	return client, nil
}

// Connect 连接到 MQTT Broker
// 如果认证失败，将自动重试（由MQTT客户端库的AutoReconnect处理）
func (c *Client) Connect() error {
	c.logger.Info("Connecting to MQTT broker...")

	token := c.client.Connect()
	if !token.WaitTimeout(30 * time.Second) {
		c.logger.Error("Connection timeout - retrying in 30 seconds")
		return fmt.Errorf("connection timeout")
	}

	if err := token.Error(); err != nil {
		// 检查是否为认证错误
		errStr := err.Error()
		if strings.Contains(errStr, "not authorized") ||
			strings.Contains(errStr, "authentication") ||
			strings.Contains(errStr, "bad user name or password") {
			c.logger.Error("Authentication failed - check credentials",
				zap.Error(err))
		} else {
			c.logger.Error("Connection failed",
				zap.Error(err))
		}
		return fmt.Errorf("connection failed: %w", err)
	}

	c.logger.Info("Successfully connected to MQTT broker")
	return nil
}

// Disconnect 断开连接
func (c *Client) Disconnect() {
	c.logger.Info("Disconnecting from MQTT broker...")

	// 发送离线心跳消息
	if err := c.PublishHeartbeatStatus("offline"); err != nil {
		c.logger.Warn("Failed to publish offline heartbeat", zap.Error(err))
	}

	c.client.Disconnect(1000) // 1 秒优雅关闭
	c.logger.Info("Disconnected from MQTT broker")
}

// PublishHeartbeatStatus 发布指定状态的心跳消息
func (c *Client) PublishHeartbeatStatus(status string) error {
	// 使用标准 MQTT topic 格式 (斜杠分隔)
	// NATS Server 会自动转换为 ota.nodes.{deviceID}.heartbeat
	topic := fmt.Sprintf("ota/nodes/%s/heartbeat", c.deviceID)

	heartbeat := HeartbeatMessage{
		DeviceID:     c.deviceID,
		Timestamp:    time.Now().Unix(),
		IP:           getLocalIP(),
		OS:           getOSInfo(),
		Architecture: getArchitecture(),
		Hostname:     getHostname(),
		Status:       status,
	}

	payload, err := json.Marshal(heartbeat)
	if err != nil {
		return fmt.Errorf("failed to marshal heartbeat: %w", err)
	}

	return c.Publish(topic, payload)
}

// IsConnected 检查连接状态
func (c *Client) IsConnected() bool {
	return c.client.IsConnected()
}

// Subscribe 订阅主题
func (c *Client) Subscribe(topic string, handler MessageHandler) error {
	c.logger.Info("Subscribing to topic",
		zap.String("topic", topic))

	// 包装处理函数
	wrappedHandler := func(client mqtt.Client, msg mqtt.Message) {
		c.logger.Debug("Message received",
			zap.String("topic", msg.Topic()),
			zap.Int("payload_size", len(msg.Payload())))

		if err := handler(msg.Topic(), msg.Payload()); err != nil {
			c.logger.Error("Failed to handle message",
				zap.String("topic", msg.Topic()),
				zap.Error(err))
		}
	}

	// 订阅（QoS 1: 至少一次）
	token := c.client.Subscribe(topic, 1, wrappedHandler)
	if !token.WaitTimeout(10 * time.Second) {
		return fmt.Errorf("subscribe timeout")
	}

	if err := token.Error(); err != nil {
		return fmt.Errorf("subscribe failed: %w", err)
	}

	c.logger.Info("Successfully subscribed to topic",
		zap.String("topic", topic))
	return nil
}

// Publish 发布消息
func (c *Client) Publish(topic string, payload []byte) error {
	c.logger.Debug("Publishing message",
		zap.String("topic", topic),
		zap.Int("payload_size", len(payload)))

	// 发布（QoS 1: 至少一次, Retained: false）
	token := c.client.Publish(topic, 1, false, payload)
	if !token.WaitTimeout(30 * time.Second) {
		return fmt.Errorf("publish timeout")
	}

	if err := token.Error(); err != nil {
		return fmt.Errorf("publish failed: %w", err)
	}

	c.logger.Debug("Successfully published message",
		zap.String("topic", topic))
	return nil
}

// PublishWithRetry 发布消息，支持重试和缓冲
// Story 3.6 AC#5: 在 NATS 断连期间缓冲结果
func (c *Client) PublishWithRetry(topic string, payload []byte) error {
	// 首先尝试发布
	for i := 0; i <= c.retryConfig.MaxRetries; i++ {
		if i > 0 {
			c.logger.Debug("Retrying publish",
				zap.String("topic", topic),
				zap.Int("attempt", i+1))
			time.Sleep(c.retryConfig.RetryInterval)
		}

		// 检查连接状态
		if !c.client.IsConnected() {
			c.logger.Warn("Not connected, buffering result",
				zap.String("topic", topic))
			c.buffer.Add(topic, payload)
			return nil // 不返回错误，消息已缓冲
		}

		// 尝试发布
		token := c.client.Publish(topic, 1, false, payload)
		if token.WaitTimeout(10 * time.Second) {
			if err := token.Error(); err == nil {
				c.logger.Debug("Successfully published message with retry",
					zap.String("topic", topic),
					zap.Int("attempts", i+1))
				return nil
			}
			c.logger.Warn("Publish attempt failed",
				zap.String("topic", topic),
				zap.Int("attempt", i+1),
				zap.Error(token.Error()))
		} else {
			c.logger.Warn("Publish timeout",
				zap.String("topic", topic),
				zap.Int("attempt", i+1))
		}
	}

	// 所有重试失败，加入缓冲队列
	c.logger.Warn("All retries failed, buffering result",
		zap.String("topic", topic),
		zap.Int("max_retries", c.retryConfig.MaxRetries))
	c.buffer.Add(topic, payload)
	return nil // 不返回错误，消息已缓冲
}

// flushBuffer 刷新缓冲队列，发布所有缓冲的消息
// Story 3.6: 在 NATS 重连后自动发布缓冲的结果
func (c *Client) flushBuffer() {
	if c.buffer == nil || c.buffer.IsEmpty() {
		return
	}

	bufferSize := c.buffer.Size()
	c.logger.Info("Flushing buffered results",
		zap.Int("count", bufferSize))

	flushed := 0
	failed := 0

	for {
		result := c.buffer.Pop()
		if result == nil {
			break
		}

		// 尝试发布
		token := c.client.Publish(result.Topic, 1, false, result.Payload)
		if token.WaitTimeout(10*time.Second) && token.Error() == nil {
			flushed++
			c.logger.Debug("Flushed buffered result",
				zap.String("topic", result.Topic),
				zap.Duration("buffered_for", time.Since(result.Timestamp)))
		} else {
			failed++
			// 发布失败，重新加入队列
			c.buffer.Add(result.Topic, result.Payload)
			c.logger.Warn("Failed to flush buffered result, re-queued",
				zap.String("topic", result.Topic))
			// 如果连接丢失，停止刷新
			if !c.client.IsConnected() {
				c.logger.Warn("Connection lost during flush, stopping")
				break
			}
		}
	}

	c.logger.Info("Buffer flush completed",
		zap.Int("flushed", flushed),
		zap.Int("failed", failed),
		zap.Int("remaining", c.buffer.Size()))
}

// GetBufferSize 获取当前缓冲队列大小
func (c *Client) GetBufferSize() int {
	if c.buffer == nil {
		return 0
	}
	return c.buffer.Size()
}

// PublishHeartbeat 发布心跳消息
func (c *Client) PublishHeartbeat() error {
	// 使用标准 MQTT topic 格式 (斜杠分隔)
	// NATS Server 会自动转换为 ota.nodes.{deviceID}.heartbeat
	topic := fmt.Sprintf("ota/nodes/%s/heartbeat", c.deviceID)

	// 完整的心跳消息JSON格式，符合Story 2.2要求
	heartbeat := HeartbeatMessage{
		DeviceID:     c.deviceID,
		Timestamp:    time.Now().Unix(),
		IP:           getLocalIP(),
		OS:           getOSInfo(),
		Architecture: getArchitecture(),
		Hostname:     getHostname(),
		Status:       "online",
	}

	payload, err := json.Marshal(heartbeat)
	if err != nil {
		return fmt.Errorf("failed to marshal heartbeat: %w", err)
	}

	return c.Publish(topic, payload)
}

// GetDeviceCommandTopic 获取设备命令主题
// 用于订阅来自云端的命令（通用命令主题，向后兼容）
func (c *Client) GetDeviceCommandTopic() string {
	return fmt.Sprintf("ota/nodes/%s/commands/#", c.deviceID)
}

// GetDeviceResultTopic 获取设备结果主题
// 用于发布任务执行结果（通用结果主题，向后兼容）
func (c *Client) GetDeviceResultTopic(taskID string) string {
	return fmt.Sprintf("ota/nodes/%s/results/%s", c.deviceID, taskID)
}

// GetDeviceExecTopic 获取设备 exec 命令主题（Story 3.1）
// 用于订阅来自云端的 exec 类型命令
// NATS Server 会自动将 MQTT topic 转换为 NATS subject: ota.nodes.{deviceID}.exec
func (c *Client) GetDeviceExecTopic() string {
	return fmt.Sprintf("ota/nodes/%s/exec", c.deviceID)
}

// GetDeviceExecResultTopic 获取设备 exec 结果主题（Story 3.1）
// 用于发布 exec 类型任务的执行结果
// NATS Server 会自动将 MQTT topic 转换为 NATS subject: ota.nodes.{deviceID}.exec.result
func (c *Client) GetDeviceExecResultTopic() string {
	return fmt.Sprintf("ota/nodes/%s/exec/result", c.deviceID)
}

// GetDeviceReadTopic 获取设备 read 命令主题（Story 4.1）
// 用于订阅来自云端的 read 类型命令
// NATS Server 会自动将 MQTT topic 转换为 NATS subject: ota.nodes.{deviceID}.read
func (c *Client) GetDeviceReadTopic() string {
	return fmt.Sprintf("ota/nodes/%s/read", c.deviceID)
}

// GetDeviceReadResultTopic 获取设备 read 结果主题（Story 4.1）
// 用于发布 read 类型任务的执行结果
// NATS Server 会自动将 MQTT topic 转换为 NATS subject: ota.nodes.{deviceID}.read.result
func (c *Client) GetDeviceReadResultTopic() string {
	return fmt.Sprintf("ota/nodes/%s/read/result", c.deviceID)
}

// GetDeviceWriteTopic 获取设备 write 命令主题（Story 4.2）
// 用于订阅来自云端的 write 类型命令
// NATS Server 会自动将 MQTT topic 转换为 NATS subject: ota.nodes.{deviceID}.write
func (c *Client) GetDeviceWriteTopic() string {
	return fmt.Sprintf("ota/nodes/%s/write", c.deviceID)
}

// GetDeviceWriteResultTopic 获取设备 write 结果主题（Story 4.2）
// 用于发布 write 类型任务的执行结果
// NATS Server 会自动将 MQTT topic 转换为 NATS subject: ota.nodes.{deviceID}.write.result
func (c *Client) GetDeviceWriteResultTopic() string {
	return fmt.Sprintf("ota/nodes/%s/write/result", c.deviceID)
}

// newTLSConfig 创建 TLS 配置
func newTLSConfig(caFile string) (*tls.Config, error) {
	// 读取 CA 证书
	caCert, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read CA file: %w", err)
	}

	// 创建证书池
	certPool := x509.NewCertPool()
	if !certPool.AppendCertsFromPEM(caCert) {
		return nil, fmt.Errorf("failed to parse CA certificate")
	}

	// 创建 TLS 配置
	tlsConfig := &tls.Config{
		RootCAs:            certPool,
		InsecureSkipVerify: false, // 强制启用证书验证
		MinVersion:         tls.VersionTLS12,
		// ServerName 会从 broker URL 自动提取
	}

	return tlsConfig, nil
}

// getLocalIP 获取本地IP地址
func getLocalIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "127.0.0.1"
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP.String()
}

// getOSInfo 获取操作系统信息
func getOSInfo() string {
	if runtime.GOOS == "linux" {
		// 尝试获取Linux发行版信息
		if cmd := exec.Command("lsb_release", "-d"); cmd != nil {
			if output, err := cmd.Output(); err == nil {
				fields := strings.Fields(string(output))
				if len(fields) >= 3 {
					return strings.Join(fields[2:], " ")
				}
			}
		}

		// 如果lsb_release不可用，尝试读取/etc/os-release
		if data, err := os.ReadFile("/etc/os-release"); err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				if strings.HasPrefix(line, "PRETTY_NAME=") {
					return strings.Trim(strings.Split(line, "=")[1], `"`)
				}
			}
		}
	}

	return runtime.GOOS
}

// getArchitecture 获取系统架构
func getArchitecture() string {
	if runtime.GOARCH == "amd64" {
		return "x86_64"
	}
	return runtime.GOARCH
}

// getHostname 获取主机名
func getHostname() string {
	hostname, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return hostname
}
