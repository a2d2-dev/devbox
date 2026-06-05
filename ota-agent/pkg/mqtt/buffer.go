package mqtt

import (
	"sync"
	"time"

	"go.uber.org/zap"
)

// BufferedResult 缓冲的结果消息
type BufferedResult struct {
	Topic     string    // MQTT 主题
	Payload   []byte    // 消息内容
	Timestamp time.Time // 缓冲时间
	Retries   int       // 重试次数
}

// ResultBuffer 结果缓冲队列
// Story 3.6 AC#5: 在 NATS 断连期间缓冲结果
type ResultBuffer struct {
	mu      sync.Mutex
	queue   []BufferedResult
	maxSize int // 最大缓冲数量
	logger  *zap.Logger
}

// NewResultBuffer 创建结果缓冲队列
func NewResultBuffer(maxSize int, logger *zap.Logger) *ResultBuffer {
	if maxSize <= 0 {
		maxSize = 100 // 默认最多缓冲 100 条
	}
	if logger == nil {
		logger, _ = zap.NewProduction()
	}

	return &ResultBuffer{
		queue:   make([]BufferedResult, 0, maxSize),
		maxSize: maxSize,
		logger:  logger,
	}
}

// Add 添加结果到缓冲队列
// 如果队列已满，丢弃最旧的结果
func (b *ResultBuffer) Add(topic string, payload []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()

	// 如果队列已满，丢弃最旧的结果
	if len(b.queue) >= b.maxSize {
		dropped := b.queue[0]
		b.queue = b.queue[1:]
		b.logger.Warn("Buffer full, dropping oldest result",
			zap.String("dropped_topic", dropped.Topic),
			zap.Time("dropped_timestamp", dropped.Timestamp),
			zap.Int("queue_size", b.maxSize))
	}

	// 添加新结果
	b.queue = append(b.queue, BufferedResult{
		Topic:     topic,
		Payload:   payload,
		Timestamp: time.Now(),
		Retries:   0,
	})

	b.logger.Info("Result buffered",
		zap.String("topic", topic),
		zap.Int("queue_size", len(b.queue)))
}

// Pop 从队列头部取出一个结果
// 如果队列为空，返回 nil
func (b *ResultBuffer) Pop() *BufferedResult {
	b.mu.Lock()
	defer b.mu.Unlock()

	if len(b.queue) == 0 {
		return nil
	}

	result := b.queue[0]
	b.queue = b.queue[1:]
	return &result
}

// Peek 查看队列头部的结果，但不移除
func (b *ResultBuffer) Peek() *BufferedResult {
	b.mu.Lock()
	defer b.mu.Unlock()

	if len(b.queue) == 0 {
		return nil
	}

	return &b.queue[0]
}

// Size 返回当前队列大小
func (b *ResultBuffer) Size() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.queue)
}

// IsEmpty 检查队列是否为空
func (b *ResultBuffer) IsEmpty() bool {
	return b.Size() == 0
}

// Clear 清空队列
func (b *ResultBuffer) Clear() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.queue = b.queue[:0]
	b.logger.Info("Buffer cleared")
}

// GetAll 获取所有缓冲的结果（用于调试）
func (b *ResultBuffer) GetAll() []BufferedResult {
	b.mu.Lock()
	defer b.mu.Unlock()

	// 返回副本以避免并发问题
	result := make([]BufferedResult, len(b.queue))
	copy(result, b.queue)
	return result
}

// IncrementRetry 增加指定索引结果的重试次数
// 如果超过最大重试次数，从队列中移除
func (b *ResultBuffer) IncrementRetry(maxRetries int) *BufferedResult {
	b.mu.Lock()
	defer b.mu.Unlock()

	if len(b.queue) == 0 {
		return nil
	}

	b.queue[0].Retries++
	if b.queue[0].Retries > maxRetries {
		// 超过最大重试次数，移除
		dropped := b.queue[0]
		b.queue = b.queue[1:]
		b.logger.Warn("Max retries exceeded, dropping result",
			zap.String("topic", dropped.Topic),
			zap.Int("retries", dropped.Retries))
		return nil
	}

	return &b.queue[0]
}
