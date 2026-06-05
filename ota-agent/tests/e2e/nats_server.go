//go:build e2e
// +build e2e

package e2e

import (
	"fmt"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
)

// EmbeddedNATSServer 内嵌 NATS 服务器用于测试
type EmbeddedNATSServer struct {
	server *natsserver.Server
	opts   *natsserver.Options
}

// NewEmbeddedNATSServer 创建内嵌 NATS 服务器
func NewEmbeddedNATSServer() (*EmbeddedNATSServer, error) {
	// 配置 NATS 服务器选项
	opts := &natsserver.Options{
		Host:           "127.0.0.1",
		Port:           -1, // 随机端口
		NoLog:          true,
		NoSigs:         true,
		MaxControlLine: 2048,
	}

	// 创建 NATS 服务器
	server, err := natsserver.NewServer(opts)
	if err != nil {
		return nil, fmt.Errorf("failed to create NATS server: %w", err)
	}

	return &EmbeddedNATSServer{
		server: server,
		opts:   opts,
	}, nil
}

// Start 启动 NATS 服务器
func (s *EmbeddedNATSServer) Start() error {
	// 启动服务器
	go s.server.Start()

	// 等待服务器就绪
	if !s.server.ReadyForConnections(5 * time.Second) {
		return fmt.Errorf("NATS server failed to start")
	}

	return nil
}

// Stop 停止 NATS 服务器
func (s *EmbeddedNATSServer) Stop() {
	if s.server != nil {
		s.server.Shutdown()
		s.server.WaitForShutdown()
	}
}

// ClientURL 获取客户端连接 URL
func (s *EmbeddedNATSServer) ClientURL() string {
	return s.server.ClientURL()
}

// CreateClient 创建 NATS 客户端连接
func (s *EmbeddedNATSServer) CreateClient() (*nats.Conn, error) {
	nc, err := nats.Connect(s.ClientURL())
	if err != nil {
		return nil, fmt.Errorf("failed to connect to NATS: %w", err)
	}
	return nc, nil
}
