//go:build !linux

package apps

import (
	"errors"
	"os"
)

// 非 Linux 平台：takeover 的 openat2 安全打开不可用。明确拒绝（返回 capability 错误），
// 而非退化为不安全的路径 ReadFile（devbox 目标是 Linux 单机）。
func safeOpenWorkDir(_ string) (*os.File, error) {
	return nil, errors.New("takeover 仅在 Linux 支持（openat2 安全打开）")
}
func safeOpenConfigBeneath(_ *os.File, _ string) (*os.File, error) {
	return nil, errors.New("takeover 仅在 Linux 支持（openat2 安全打开）")
}
func fdRegularSize(_ *os.File) (bool, int64, error) {
	return false, 0, errors.New("takeover 仅在 Linux 支持")
}
func readAllBounded(_ *os.File, _ int64) ([]byte, error) {
	return nil, errors.New("takeover 仅在 Linux 支持")
}
