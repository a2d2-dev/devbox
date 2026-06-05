//go:build unit
// +build unit

package mqtt

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

func TestNewResultBuffer(t *testing.T) {
	logger := zaptest.NewLogger(t)

	tests := []struct {
		name        string
		maxSize     int
		wantMaxSize int
	}{
		{"default size", 0, 100},
		{"custom size", 50, 50},
		{"negative size", -1, 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buffer := NewResultBuffer(tt.maxSize, logger)
			require.NotNil(t, buffer)
			assert.Equal(t, tt.wantMaxSize, buffer.maxSize)
			assert.True(t, buffer.IsEmpty())
		})
	}
}

func TestResultBuffer_AddAndPop(t *testing.T) {
	logger := zaptest.NewLogger(t)
	buffer := NewResultBuffer(10, logger)

	// 添加消息
	buffer.Add("topic1", []byte("payload1"))
	buffer.Add("topic2", []byte("payload2"))

	assert.Equal(t, 2, buffer.Size())

	// Pop 第一个
	result := buffer.Pop()
	require.NotNil(t, result)
	assert.Equal(t, "topic1", result.Topic)
	assert.Equal(t, []byte("payload1"), result.Payload)
	assert.Equal(t, 1, buffer.Size())

	// Pop 第二个
	result = buffer.Pop()
	require.NotNil(t, result)
	assert.Equal(t, "topic2", result.Topic)
	assert.Equal(t, []byte("payload2"), result.Payload)
	assert.True(t, buffer.IsEmpty())

	// Pop 空队列
	result = buffer.Pop()
	assert.Nil(t, result)
}

func TestResultBuffer_Peek(t *testing.T) {
	logger := zaptest.NewLogger(t)
	buffer := NewResultBuffer(10, logger)

	// Peek 空队列
	result := buffer.Peek()
	assert.Nil(t, result)

	// 添加消息
	buffer.Add("topic1", []byte("payload1"))

	// Peek 不移除
	result = buffer.Peek()
	require.NotNil(t, result)
	assert.Equal(t, "topic1", result.Topic)
	assert.Equal(t, 1, buffer.Size())

	// 再次 Peek 仍然是同一个
	result = buffer.Peek()
	require.NotNil(t, result)
	assert.Equal(t, "topic1", result.Topic)
}

func TestResultBuffer_MaxSize(t *testing.T) {
	logger := zaptest.NewLogger(t)
	buffer := NewResultBuffer(3, logger)

	// 添加 3 个消息
	buffer.Add("topic1", []byte("payload1"))
	buffer.Add("topic2", []byte("payload2"))
	buffer.Add("topic3", []byte("payload3"))
	assert.Equal(t, 3, buffer.Size())

	// 添加第 4 个，应该丢弃最旧的 (topic1)
	buffer.Add("topic4", []byte("payload4"))
	assert.Equal(t, 3, buffer.Size())

	// 验证 topic1 被丢弃
	result := buffer.Pop()
	require.NotNil(t, result)
	assert.Equal(t, "topic2", result.Topic)
}

func TestResultBuffer_Clear(t *testing.T) {
	logger := zaptest.NewLogger(t)
	buffer := NewResultBuffer(10, logger)

	buffer.Add("topic1", []byte("payload1"))
	buffer.Add("topic2", []byte("payload2"))
	assert.Equal(t, 2, buffer.Size())

	buffer.Clear()
	assert.True(t, buffer.IsEmpty())
}

func TestResultBuffer_GetAll(t *testing.T) {
	logger := zaptest.NewLogger(t)
	buffer := NewResultBuffer(10, logger)

	buffer.Add("topic1", []byte("payload1"))
	buffer.Add("topic2", []byte("payload2"))

	all := buffer.GetAll()
	assert.Len(t, all, 2)
	assert.Equal(t, "topic1", all[0].Topic)
	assert.Equal(t, "topic2", all[1].Topic)

	// 确保是副本
	all[0].Topic = "modified"
	peek := buffer.Peek()
	assert.Equal(t, "topic1", peek.Topic)
}

func TestResultBuffer_IncrementRetry(t *testing.T) {
	logger := zaptest.NewLogger(t)
	buffer := NewResultBuffer(10, logger)

	buffer.Add("topic1", []byte("payload1"))

	// 增加重试次数
	result := buffer.IncrementRetry(3)
	require.NotNil(t, result)
	assert.Equal(t, 1, result.Retries)

	result = buffer.IncrementRetry(3)
	assert.Equal(t, 2, result.Retries)

	result = buffer.IncrementRetry(3)
	assert.Equal(t, 3, result.Retries)

	// 超过最大重试次数，应该被移除
	result = buffer.IncrementRetry(3)
	assert.Nil(t, result)
	assert.True(t, buffer.IsEmpty())
}

func TestResultBuffer_Concurrency(t *testing.T) {
	logger := zaptest.NewLogger(t)
	buffer := NewResultBuffer(100, logger)

	// 并发添加
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(n int) {
			for j := 0; j < 10; j++ {
				buffer.Add("topic", []byte("payload"))
			}
			done <- true
		}(i)
	}

	// 等待完成
	for i := 0; i < 10; i++ {
		<-done
	}

	// 应该有 100 条消息
	assert.Equal(t, 100, buffer.Size())

	// 并发 Pop
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 10; j++ {
				buffer.Pop()
			}
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	assert.True(t, buffer.IsEmpty())
}

func TestResultBuffer_Timestamp(t *testing.T) {
	logger := zaptest.NewLogger(t)
	buffer := NewResultBuffer(10, logger)

	before := time.Now()
	buffer.Add("topic1", []byte("payload1"))
	after := time.Now()

	result := buffer.Pop()
	require.NotNil(t, result)
	assert.True(t, result.Timestamp.After(before) || result.Timestamp.Equal(before))
	assert.True(t, result.Timestamp.Before(after) || result.Timestamp.Equal(after))
}
