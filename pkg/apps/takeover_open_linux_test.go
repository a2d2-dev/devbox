//go:build linux

package apps

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func TestValidateTakeoverPathsRejectsFIFOWithoutBlocking(t *testing.T) {
	workDir := t.TempDir()
	dataDir := t.TempDir()
	fifo := filepath.Join(workDir, "compose.yaml")
	require.NoError(t, unix.Mkfifo(fifo, 0o600))

	done := make(chan error, 1)
	go func() {
		_, _, err := validateTakeoverPaths(workDir, []string{fifo}, dataDir, "")
		done <- err
	}()

	select {
	case err := <-done:
		require.Error(t, err)
		ae, ok := AsError(err)
		require.True(t, ok)
		assert.Equal(t, ErrKindValidation, ae.Kind)
		assert.Contains(t, ae.Message, "普通文件")
	case <-time.After(2 * time.Second):
		// 缺少 O_NONBLOCK 时，打开 FIFO 会永远等待 writer；这里打开 writer 也能释放
		// 有缺陷的实现，让测试报告失败后正常退出。
		if fd, err := unix.Open(fifo, unix.O_WRONLY|unix.O_NONBLOCK, 0); err == nil {
			_ = unix.Close(fd)
		}
		t.Fatal("FIFO config path blocked takeover validation")
	}
}
