//go:build linux

package apps

import (
	"fmt"
	"io"
	"os"

	"golang.org/x/sys/unix"
)

// safeOpenWorkDir 用 openat2(RESOLVE_NO_SYMLINKS|NO_MAGICLINKS) 打开 canonical working_dir
// 为 *os.File（dirfd）：路径链中任何 symlink 都被原子拒绝，消除 check→open TOCTOU
// （final/父目录在检查与打开之间被换成 symlink 的攻击失效）。
func safeOpenWorkDir(workDir string) (*os.File, error) {
	how := &unix.OpenHow{
		Flags:   uint64(unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC),
		Resolve: unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS,
	}
	fd, err := unix.Openat2(unix.AT_FDCWD, workDir, how)
	if err != nil {
		return nil, fmt.Errorf("working_dir 不可安全打开: %w", err)
	}
	return os.NewFile(uintptr(fd), workDir), nil
}

// safeOpenConfigBeneath 用 openat2(workDirFd,
// RESOLVE_BENEATH|NO_SYMLINKS|NO_MAGICLINKS|NO_XDEV) 打开 working_dir 下的相对
// config path：禁止逃逸 working_dir、禁止 symlink、禁止通过子 bind mount 跨到其它
// 文件系统（原子安全，不再依赖先 Lstat/EvalSymlinks 再 ReadFile 的可被替换序列）。
func safeOpenConfigBeneath(workDir *os.File, rel string) (*os.File, error) {
	how := &unix.OpenHow{
		// O_NONBLOCK 防止攻击者控制的 FIFO/device path 在 fdRegularSize 拒绝
		// 非普通文件之前阻塞请求；它不影响普通 Compose 文件。
		Flags:   uint64(unix.O_RDONLY | unix.O_NONBLOCK | unix.O_CLOEXEC),
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS | unix.RESOLVE_NO_XDEV,
	}
	fd, err := unix.Openat2(int(workDir.Fd()), rel, how)
	if err != nil {
		return nil, fmt.Errorf("config file 不可安全打开: %w", err)
	}
	return os.NewFile(uintptr(fd), rel), nil
}

// fdRegularSize fstat fd，返回是否普通文件与大小（用于大小上限判定）。
func fdRegularSize(f *os.File) (bool, int64, error) {
	var st unix.Stat_t
	if err := unix.Fstat(int(f.Fd()), &st); err != nil {
		return false, 0, err
	}
	return (st.Mode & unix.S_IFMT) == unix.S_IFREG, int64(st.Size), nil
}

// readAllBounded 从 f 读取，并校验实际长度 == expected（fstat 时刻的大小）。若不一致（文件在
// fstat 与 read 之间被并发改写、增长或收缩）则报错——拒绝截断/不一致快照，而非接受。
func readAllBounded(f *os.File, expected int64) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(f, expected+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) != expected {
		return nil, fmt.Errorf("config file 在读取期间大小发生变化")
	}
	return b, nil
}
