package files

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

var errNoReplaceUnsupported = errors.New("atomic no-replace rename is unsupported")

func renameNoReplace(rootDir, oldRel, newRel string) error {
	rootFD, err := openRootFD(rootDir)
	if err != nil {
		return err
	}
	defer unix.Close(rootFD)

	oldParent, oldName, err := openParentFD(rootFD, oldRel)
	if err != nil {
		return err
	}
	defer unix.Close(oldParent)
	newParent, newName, err := openParentFD(rootFD, newRel)
	if err != nil {
		return err
	}
	defer unix.Close(newParent)

	err = unix.Renameat2(oldParent, oldName, newParent, newName, unix.RENAME_NOREPLACE)
	if err == nil || errors.Is(err, unix.EEXIST) {
		return err
	}
	if !errors.Is(err, unix.ENOSYS) && !errors.Is(err, unix.EINVAL) && !errors.Is(err, unix.EOPNOTSUPP) {
		return err
	}

	var stat unix.Stat_t
	if statErr := unix.Fstatat(oldParent, oldName, &stat, unix.AT_SYMLINK_NOFOLLOW); statErr != nil {
		return statErr
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG {
		return errNoReplaceUnsupported
	}
	if err := unix.Linkat(oldParent, oldName, newParent, newName, 0); err != nil {
		return err
	}
	if err := unix.Unlinkat(oldParent, oldName, 0); err != nil {
		_ = unix.Unlinkat(newParent, newName, 0)
		return err
	}
	return nil
}

func renameReplace(rootDir, oldRel, newRel string) error {
	rootFD, err := openRootFD(rootDir)
	if err != nil {
		return err
	}
	defer unix.Close(rootFD)
	oldParent, oldName, err := openParentFD(rootFD, oldRel)
	if err != nil {
		return err
	}
	defer unix.Close(oldParent)
	newParent, newName, err := openParentFD(rootFD, newRel)
	if err != nil {
		return err
	}
	defer unix.Close(newParent)
	return unix.Renameat(oldParent, oldName, newParent, newName)
}

func removeAllInRoot(rootDir, rel string) error {
	rootFD, err := openRootFD(rootDir)
	if err != nil {
		return err
	}
	defer unix.Close(rootFD)
	parent, name, err := openParentFD(rootFD, rel)
	if err != nil {
		return err
	}
	defer unix.Close(parent)
	return removeAllAt(parent, name)
}

func removeAllAt(parent int, name string) error {
	fd, err := unix.Openat(parent, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		if errors.Is(err, unix.ENOENT) {
			return nil
		}
		if errors.Is(err, unix.ENOTDIR) || errors.Is(err, unix.ELOOP) {
			err = unix.Unlinkat(parent, name, 0)
			if errors.Is(err, unix.ENOENT) {
				return nil
			}
			return err
		}
		return err
	}
	dir := os.NewFile(uintptr(fd), name)
	entries, readErr := dir.ReadDir(-1)
	if readErr != nil {
		dir.Close()
		return readErr
	}
	for _, entry := range entries {
		if err := removeAllAt(fd, entry.Name()); err != nil {
			dir.Close()
			return err
		}
	}
	if err := dir.Close(); err != nil {
		return err
	}
	err = unix.Unlinkat(parent, name, unix.AT_REMOVEDIR)
	if errors.Is(err, unix.ENOENT) {
		return nil
	}
	return err
}

func openRootFD(rootDir string) (int, error) {
	return unix.Open(rootDir, unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
}

func openParentFD(rootFD int, rel string) (int, string, error) {
	rel = filepath.Clean(rel)
	if rel == "." || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return -1, "", fmt.Errorf("invalid rename path %q", rel)
	}
	parent := filepath.Dir(rel)
	name := filepath.Base(rel)
	fd, err := unix.Dup(rootFD)
	if err != nil {
		return -1, "", err
	}
	if parent == "." {
		return fd, name, nil
	}
	for _, part := range strings.Split(parent, string(filepath.Separator)) {
		next, openErr := unix.Openat(fd, part, unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		unix.Close(fd)
		if openErr != nil {
			return -1, "", openErr
		}
		fd = next
	}
	return fd, name, nil
}
