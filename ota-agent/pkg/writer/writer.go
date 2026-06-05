package writer

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"go.uber.org/zap"
)

// Writer 文件写入器
// 实现 Agent Write 原语，用于远程写入文件到设备
type Writer struct {
	logger *zap.Logger
}

// New 创建文件写入器实例
func New(logger *zap.Logger) *Writer {
	if logger == nil {
		logger, _ = zap.NewProduction()
	}

	return &Writer{
		logger: logger,
	}
}

// Write 执行文件写入操作
// 返回包含写入结果的响应或错误响应
func (w *Writer) Write(req *WriteRequest) *WriteResponse {
	w.logger.Info("Writing file",
		zap.String("request_id", req.RequestID),
		zap.String("path", req.Path),
		zap.String("mode", req.Mode))

	// 1. 验证路径安全性
	if err := w.validatePath(req.Path); err != nil {
		w.logger.Warn("Path validation failed",
			zap.String("request_id", req.RequestID),
			zap.String("path", req.Path),
			zap.Error(err))
		return NewErrorResponse(req.RequestID, err.Error())
	}

	// 2. 解码 base64 内容
	content, err := base64.StdEncoding.DecodeString(req.Content)
	if err != nil {
		w.logger.Warn("Base64 decode failed",
			zap.String("request_id", req.RequestID),
			zap.Error(err))
		return NewErrorResponse(req.RequestID, fmt.Sprintf("%s: %s", ErrInvalidBase64, err.Error()))
	}

	// 3. 检查内容大小
	if int64(len(content)) > MaxContentSize {
		w.logger.Warn("Content size exceeds limit",
			zap.String("request_id", req.RequestID),
			zap.Int("content_size", len(content)),
			zap.Int64("max_size", MaxContentSize))
		return NewErrorResponse(req.RequestID,
			fmt.Sprintf("%s: %dMB exceeds 100MB limit", ErrContentTooLarge, len(content)/(1024*1024)))
	}

	// 4. 验证 SHA256 校验和
	hash := sha256.Sum256(content)
	actualSHA256 := fmt.Sprintf("%x", hash)

	if actualSHA256 != req.SHA256 {
		w.logger.Warn("Checksum mismatch",
			zap.String("request_id", req.RequestID),
			zap.String("expected", req.SHA256),
			zap.String("actual", actualSHA256))
		return NewErrorResponse(req.RequestID,
			fmt.Sprintf("%s: expected %s, got %s", ErrChecksumMismatch, req.SHA256, actualSHA256))
	}

	// 5. 解析文件权限
	mode, err := w.parseFileMode(req.Mode)
	if err != nil {
		w.logger.Warn("Invalid file mode",
			zap.String("request_id", req.RequestID),
			zap.String("mode", req.Mode),
			zap.Error(err))
		return NewErrorResponse(req.RequestID, fmt.Sprintf("%s: %s", ErrInvalidMode, req.Mode))
	}

	// 6. 创建父目录
	dir := filepath.Dir(req.Path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		w.logger.Error("Failed to create parent directory",
			zap.String("request_id", req.RequestID),
			zap.String("dir", dir),
			zap.Error(err))
		if os.IsPermission(err) {
			return NewErrorResponse(req.RequestID, fmt.Sprintf("%s: %s", ErrPermissionDenied, dir))
		}
		return NewErrorResponse(req.RequestID, fmt.Sprintf("%s: %s", ErrCreateDirFailed, err.Error()))
	}

	// 7. 原子写入文件
	if err := w.atomicWrite(req.Path, content, mode); err != nil {
		w.logger.Error("Atomic write failed",
			zap.String("request_id", req.RequestID),
			zap.String("path", req.Path),
			zap.Error(err))
		if os.IsPermission(err) {
			return NewErrorResponse(req.RequestID, fmt.Sprintf("%s: %s", ErrPermissionDenied, req.Path))
		}
		// 检查是否是磁盘空间不足
		if strings.Contains(err.Error(), "no space left") {
			return NewErrorResponse(req.RequestID, fmt.Sprintf("%s: %s", ErrDiskFull, req.Path))
		}
		return NewErrorResponse(req.RequestID, fmt.Sprintf("%s: %s", ErrWriteFailed, err.Error()))
	}

	// 8. 验证写入结果
	fileInfo, err := os.Stat(req.Path)
	if err != nil {
		w.logger.Error("Failed to verify written file",
			zap.String("request_id", req.RequestID),
			zap.String("path", req.Path),
			zap.Error(err))
		return NewErrorResponse(req.RequestID, fmt.Sprintf("%s: verification failed", ErrWriteFailed))
	}

	// 9. 构建响应
	response := NewSuccessResponse(
		req.RequestID,
		req.Path,
		fileInfo.Size(),
		actualSHA256,
	)

	w.logger.Info("File write successful",
		zap.String("request_id", req.RequestID),
		zap.String("path", req.Path),
		zap.Int64("size", fileInfo.Size()),
		zap.String("sha256", actualSHA256),
		zap.String("mode", req.Mode))

	return response
}

// validatePath 验证路径安全性
// 防止路径遍历攻击和写入敏感系统路径
func (w *Writer) validatePath(path string) error {
	// 1. 路径必须是绝对路径
	if !filepath.IsAbs(path) {
		return fmt.Errorf("%s: path must be absolute", ErrPathTraversal)
	}

	// 2. 清理路径（解析 .. 和 . 等）
	cleanPath := filepath.Clean(path)

	// 3. 检查路径遍历
	if strings.Contains(cleanPath, "..") {
		return fmt.Errorf(ErrPathTraversal)
	}

	// 4. 检查禁止写入的路径
	for _, forbidden := range forbiddenWritePaths {
		// 精确匹配根目录
		if forbidden == "/" && cleanPath == "/" {
			return fmt.Errorf("%s: %s", ErrForbiddenPath, cleanPath)
		}
		// 前缀匹配其他禁止路径
		if forbidden != "/" {
			if cleanPath == forbidden || strings.HasPrefix(cleanPath, forbidden+"/") {
				return fmt.Errorf("%s: %s", ErrForbiddenPath, cleanPath)
			}
		}
	}

	// 5. 检查符号链接
	// 解析符号链接后再次检查禁止路径
	// 注意：目标文件可能不存在，只检查父目录的符号链接
	dir := filepath.Dir(cleanPath)
	realDir, err := filepath.EvalSymlinks(dir)
	if err == nil && realDir != dir {
		// 如果父目录是符号链接，检查真实路径
		for _, forbidden := range forbiddenWritePaths {
			if forbidden == "/" && realDir == "/" {
				w.logger.Warn("Symlink parent points to root",
					zap.String("path", path),
					zap.String("real_dir", realDir))
				return fmt.Errorf("%s: symlink points to %s", ErrForbiddenPath, realDir)
			}
			if forbidden != "/" {
				if realDir == forbidden || strings.HasPrefix(realDir, forbidden+"/") {
					w.logger.Warn("Symlink parent points to forbidden path",
						zap.String("path", path),
						zap.String("real_dir", realDir))
					return fmt.Errorf("%s: symlink points to %s", ErrForbiddenPath, realDir)
				}
			}
		}
	} else if err != nil && os.IsPermission(err) {
		// 如果无法解析符号链接（权限问题），拒绝写入
		w.logger.Warn("Cannot resolve symlink due to permission",
			zap.String("path", path),
			zap.Error(err))
		return fmt.Errorf("%s: cannot resolve symlink", ErrPermissionDenied)
	}

	return nil
}

// parseFileMode 解析文件权限字符串
// 支持格式: "0o644", "644", "0o755", "755" 等
func (w *Writer) parseFileMode(modeStr string) (os.FileMode, error) {
	// 解析八进制权限
	mode, err := strconv.ParseUint(modeStr, 8, 32)
	if err != nil {
		return 0, err
	}

	// 验证权限值范围
	if mode > 0777 {
		return 0, fmt.Errorf("mode %s exceeds maximum value 0777", modeStr)
	}

	return os.FileMode(mode), nil
}

// atomicWrite 原子写入文件
// 使用临时文件 + 重命名的方式确保原子性
func (w *Writer) atomicWrite(path string, content []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)

	// 1. 创建临时文件
	tmpFile, err := os.CreateTemp(dir, ".tmp-write-*")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}

	tmpPath := tmpFile.Name()

	// 2. 确保失败时清理临时文件
	success := false
	defer func() {
		if !success {
			os.Remove(tmpPath)
		}
	}()

	// 3. 写入内容
	if _, err := tmpFile.Write(content); err != nil {
		tmpFile.Close()
		return fmt.Errorf("failed to write content: %w", err)
	}

	// 4. 设置文件权限
	if err := tmpFile.Chmod(mode); err != nil {
		tmpFile.Close()
		return fmt.Errorf("failed to set permissions: %w", err)
	}

	// 5. 同步到磁盘
	if err := tmpFile.Sync(); err != nil {
		tmpFile.Close()
		return fmt.Errorf("failed to sync file: %w", err)
	}

	// 6. 关闭文件
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close temp file: %w", err)
	}

	// 7. 原子重命名
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("%s: %w", ErrAtomicRenameFailed, err)
	}

	// 8. 标记成功，防止 defer 清理
	success = true
	return nil
}

// IsForbiddenWritePath 检查路径是否为禁止写入的路径
// 用于外部调用检查
func IsForbiddenWritePath(path string) bool {
	cleanPath := filepath.Clean(path)
	for _, forbidden := range forbiddenWritePaths {
		if forbidden == "/" && cleanPath == "/" {
			return true
		}
		if forbidden != "/" {
			if cleanPath == forbidden || strings.HasPrefix(cleanPath, forbidden+"/") {
				return true
			}
		}
	}
	return false
}
