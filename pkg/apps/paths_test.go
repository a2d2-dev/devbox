package apps

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// LOW#10：SafeWriteFile 路径逃逸检测（显式括号条件）必须拒绝越界写。
func TestSafeWriteFileRejectsEscape(t *testing.T) {
	dir := t.TempDir()
	paths := NewPaths(dir)
	require.NoError(t, paths.EnsureAppDir("myapp"))
	// 合法写。
	require.NoError(t, paths.SafeWriteFile("myapp", "compose.yaml", []byte("x"), 0o600))
	// 逃逸到应用目录外 → 拒绝，且不产生副作用目录。
	err := paths.SafeWriteFile("myapp", "../../etc/evil", []byte("x"), 0o600)
	ae, ok := AsError(err)
	require.True(t, ok)
	assert.Equal(t, ErrKindValidation, ae.Kind)
	assert.NoDirExists(t, dir+"/etc")
}

// MED#6：ping 必须尊重 ctx；已取消的 ctx 应立即返回，不等待拨号超时。
func TestPingRespectsCanceledCtx(t *testing.T) {
	e := newDockerEngine("/nonexistent/devbox-test.sock") // unix 拨号目标不存在
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	err := e.ping(ctx)
	elapsed := time.Since(start)
	assert.Error(t, err)
	assert.Less(t, elapsed, 2*time.Second, "已取消 ctx 应立即返回")
}
