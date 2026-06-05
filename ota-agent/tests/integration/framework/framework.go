//go:build integration
// +build integration

package framework

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/stretchr/testify/require"
)

// Framework 集成测试框架
type Framework struct {
	t          *testing.T
	ctx        context.Context
	mqttClient mqtt.Client
	deviceID   string
	broker     string
	results    map[string]chan *TaskResult
	resultsMu  sync.RWMutex
}

// TaskResult 任务执行结果
type TaskResult struct {
	TaskID    string `json:"task_id"`
	Status    string `json:"status"` // success, failed
	Output    string `json:"output"`
	Error     string `json:"error,omitempty"`
	StartTime string `json:"start_time"`
	EndTime   string `json:"end_time"`
}

// Option 配置选项
type Option func(*Framework)

// WithDeviceID 设置设备ID
func WithDeviceID(deviceID string) Option {
	return func(f *Framework) {
		f.deviceID = deviceID
	}
}

// WithMQTTBroker 设置 MQTT Broker 地址
func WithMQTTBroker(broker string) Option {
	return func(f *Framework) {
		f.broker = broker
	}
}

// Run 运行集成测试框架
func Run(t *testing.T, ctx context.Context, opts ...Option) *Framework {
	f := &Framework{
		t:       t,
		ctx:     ctx,
		results: make(map[string]chan *TaskResult),
	}

	// 应用选项
	for _, opt := range opts {
		opt(f)
	}

	// 设置默认值
	if f.deviceID == "" {
		f.deviceID = "test-device-001"
	}
	if f.broker == "" {
		f.broker = "tls://198.19.249.2:8883"
	}

	// 连接 MQTT
	f.connectMQTT()

	return f
}

// connectMQTT 连接到 MQTT Broker
func (f *Framework) connectMQTT() {
	// 加载 CA 证书
	caCertPath := os.Getenv("DAPR_INTEGRATION_CA_CERT")
	if caCertPath == "" {
		caCertPath = "../testdata/certs/ca.crt"
	}

	caCert, err := os.ReadFile(caCertPath)
	require.NoError(f.t, err, "Failed to read CA certificate")

	certPool := x509.NewCertPool()
	ok := certPool.AppendCertsFromPEM(caCert)
	require.True(f.t, ok, "Failed to append CA certificate")

	tlsConfig := &tls.Config{
		RootCAs:    certPool,
		MinVersion: tls.VersionTLS12,
	}

	// 创建 MQTT 客户端
	opts := mqtt.NewClientOptions()
	opts.AddBroker(f.broker)
	opts.SetClientID(fmt.Sprintf("test-framework-%d", time.Now().Unix()))
	opts.SetTLSConfig(tlsConfig)
	opts.SetAutoReconnect(true)
	opts.SetConnectTimeout(10 * time.Second)

	f.mqttClient = mqtt.NewClient(opts)
	token := f.mqttClient.Connect()
	require.True(f.t, token.WaitTimeout(10*time.Second), "MQTT connection timeout")
	require.NoError(f.t, token.Error(), "Failed to connect to MQTT broker")

	f.t.Logf("✅ Connected to MQTT broker: %s", f.broker)
}

// SendCommand 发送命令到 Agent
func (f *Framework) SendCommand(taskID, command string, timeout int) {
	payload := fmt.Sprintf(`{"id":"%s","type":"shell","command":"%s","timeout":%d}`,
		taskID, command, timeout)

	topic := fmt.Sprintf("ota/devices/%s/commands/shell", f.deviceID)

	token := f.mqttClient.Publish(topic, 1, false, []byte(payload))
	require.True(f.t, token.WaitTimeout(5*time.Second), "Publish timeout")
	require.NoError(f.t, token.Error(), "Failed to publish command")

	f.t.Logf("📤 Sent command: taskID=%s, command=%s", taskID, command)
}

// WaitForResult 等待任务结果
func (f *Framework) WaitForResult(taskID string, timeout time.Duration) (*TaskResult, error) {
	// 创建结果通道
	resultChan := make(chan *TaskResult, 1)
	f.resultsMu.Lock()
	f.results[taskID] = resultChan
	f.resultsMu.Unlock()

	// 订阅结果主题
	resultTopic := fmt.Sprintf("ota/devices/%s/results/%s", f.deviceID, taskID)
	token := f.mqttClient.Subscribe(resultTopic, 1, func(c mqtt.Client, msg mqtt.Message) {
		var result TaskResult
		if err := json.Unmarshal(msg.Payload(), &result); err != nil {
			f.t.Logf("❌ Failed to unmarshal result: %v", err)
			return
		}

		f.resultsMu.RLock()
		ch, ok := f.results[taskID]
		f.resultsMu.RUnlock()

		if ok {
			select {
			case ch <- &result:
				f.t.Logf("📥 Received result: taskID=%s, status=%s", taskID, result.Status)
			default:
				f.t.Logf("⚠️  Result channel full for taskID=%s", taskID)
			}
		}
	})

	require.True(f.t, token.WaitTimeout(5*time.Second), "Subscribe timeout")
	require.NoError(f.t, token.Error(), "Failed to subscribe to result topic")

	// 等待结果
	select {
	case result := <-resultChan:
		return result, nil
	case <-time.After(timeout):
		return nil, fmt.Errorf("timeout waiting for result (taskID=%s)", taskID)
	case <-f.ctx.Done():
		return nil, fmt.Errorf("context canceled")
	}
}

// SubscribeHeartbeat 订阅心跳消息
func (f *Framework) SubscribeHeartbeat(callback func(payload []byte)) {
	topic := fmt.Sprintf("ota/devices/%s/heartbeat", f.deviceID)
	token := f.mqttClient.Subscribe(topic, 1, func(c mqtt.Client, msg mqtt.Message) {
		callback(msg.Payload())
	})

	require.True(f.t, token.WaitTimeout(5*time.Second), "Subscribe heartbeat timeout")
	require.NoError(f.t, token.Error(), "Failed to subscribe to heartbeat topic")

	f.t.Logf("💓 Subscribed to heartbeat: %s", topic)
}

// Cleanup 清理资源
func (f *Framework) Cleanup(t *testing.T) {
	if f.mqttClient != nil && f.mqttClient.IsConnected() {
		f.mqttClient.Disconnect(100)
		t.Log("🧹 Framework cleaned up")
	}
}
