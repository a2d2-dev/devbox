package files

import (
	"io"
	"os"
	"path/filepath"
	"strings"
)

func rootReadFile(root *os.Root, name string) ([]byte, error) {
	file, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	data, readErr := io.ReadAll(file)
	closeErr := file.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	return data, nil
}

func rootMkdirAll(root *os.Root, path string, perm os.FileMode) error {
	clean := filepath.Clean(path)
	if clean == "." {
		return nil
	}
	current := ""
	for _, part := range strings.Split(clean, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, err := root.Lstat(rootPath(current))
		if os.IsNotExist(err) {
			if err := root.Mkdir(rootPath(current), perm); err != nil && !os.IsExist(err) {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return &os.PathError{Op: "mkdir", Path: current, Err: os.ErrInvalid}
		}
	}
	return nil
}
