package reader

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.uber.org/zap"
)

// Reader 文件读取器
// 实现 Agent Read 原语，用于远程读取设备上的文件
type Reader struct {
	logger *zap.Logger
}

// New 创建文件读取器实例
func New(logger *zap.Logger) *Reader {
	if logger == nil {
		logger, _ = zap.NewProduction()
	}

	return &Reader{
		logger: logger,
	}
}

// Read 执行文件读取操作
// 返回包含文件内容的响应或错误响应
func (r *Reader) Read(req *ReadRequest) *ReadResponse {
	r.logger.Info("Reading file",
		zap.String("request_id", req.RequestID),
		zap.String("path", req.Path),
		zap.Int64("max_size", req.MaxSize))

	// 1. 验证路径安全性
	if err := r.validatePath(req.Path); err != nil {
		r.logger.Warn("Path validation failed",
			zap.String("request_id", req.RequestID),
			zap.String("path", req.Path),
			zap.Error(err))
		return NewErrorResponse(req.RequestID, err.Error())
	}

	// 2. 获取文件信息
	fileInfo, err := os.Stat(req.Path)
	if err != nil {
		if os.IsNotExist(err) {
			r.logger.Warn("File not found",
				zap.String("request_id", req.RequestID),
				zap.String("path", req.Path))
			return NewErrorResponse(req.RequestID, fmt.Sprintf("%s: %s", ErrFileNotFound, req.Path))
		}
		if os.IsPermission(err) {
			r.logger.Warn("Permission denied",
				zap.String("request_id", req.RequestID),
				zap.String("path", req.Path))
			return NewErrorResponse(req.RequestID, fmt.Sprintf("%s: %s", ErrPermissionDenied, req.Path))
		}
		r.logger.Error("Failed to stat file",
			zap.String("request_id", req.RequestID),
			zap.String("path", req.Path),
			zap.Error(err))
		return NewErrorResponse(req.RequestID, fmt.Sprintf("%s: %s", ErrReadFailed, err.Error()))
	}

	// 检查是否为目录
	if fileInfo.IsDir() {
		r.logger.Warn("Path is a directory",
			zap.String("request_id", req.RequestID),
			zap.String("path", req.Path))
		return NewErrorResponse(req.RequestID, fmt.Sprintf("path is a directory: %s", req.Path))
	}

	// 3. 检查文件大小
	fileSize := fileInfo.Size()

	// 检查绝对限制（100MB）
	if fileSize > AbsoluteMaxSize {
		r.logger.Warn("File size exceeds 100MB limit",
			zap.String("request_id", req.RequestID),
			zap.String("path", req.Path),
			zap.Int64("file_size", fileSize))
		return NewErrorResponse(req.RequestID,
			fmt.Sprintf("file size %dMB exceeds 100MB limit", fileSize/(1024*1024)))
	}

	// 检查请求限制
	if fileSize > req.MaxSize {
		r.logger.Warn("File size exceeds max size",
			zap.String("request_id", req.RequestID),
			zap.String("path", req.Path),
			zap.Int64("file_size", fileSize),
			zap.Int64("max_size", req.MaxSize))
		return NewErrorResponse(req.RequestID,
			fmt.Sprintf("%s: file size %d bytes exceeds limit %d bytes", ErrFileSizeExceedsMax, fileSize, req.MaxSize))
	}

	// 4. 读取文件内容
	content, err := os.ReadFile(req.Path)
	if err != nil {
		if os.IsPermission(err) {
			r.logger.Warn("Permission denied when reading",
				zap.String("request_id", req.RequestID),
				zap.String("path", req.Path))
			return NewErrorResponse(req.RequestID, fmt.Sprintf("%s: %s", ErrPermissionDenied, req.Path))
		}
		r.logger.Error("Failed to read file",
			zap.String("request_id", req.RequestID),
			zap.String("path", req.Path),
			zap.Error(err))
		return NewErrorResponse(req.RequestID, fmt.Sprintf("%s: %s", ErrReadFailed, err.Error()))
	}

	// 5. 计算 SHA256 校验和
	hash := sha256.Sum256(content)
	sha256Hex := fmt.Sprintf("%x", hash)

	// 6. Base64 编码内容
	base64Content := base64.StdEncoding.EncodeToString(content)

	// 7. 提取文件权限
	mode := fmt.Sprintf("%04o", fileInfo.Mode().Perm())

	// 8. 构建响应
	response := NewSuccessResponse(
		req.RequestID,
		base64Content,
		fileSize,
		sha256Hex,
		mode,
		fileInfo.ModTime(),
	)

	r.logger.Info("File read successful",
		zap.String("request_id", req.RequestID),
		zap.String("path", req.Path),
		zap.Int64("size", fileSize),
		zap.String("sha256", sha256Hex),
		zap.String("mode", mode))

	return response
}

// validatePath 验证路径安全性
// 防止路径遍历攻击和访问敏感系统路径
func (r *Reader) validatePath(path string) error {
	// 1. 路径必须是绝对路径
	if !filepath.IsAbs(path) {
		return fmt.Errorf("%s: path must be absolute", ErrPathTraversal)
	}

	// 2. 清理路径（解析 .. 和 . 等）
	cleanPath := filepath.Clean(path)

	// 3. 检查路径遍历
	// 如果清理后的路径包含 ".." 仍然可能是尝试遍历
	if strings.Contains(cleanPath, "..") {
		return fmt.Errorf(ErrPathTraversal)
	}

	// 4. 检查禁止访问的路径
	for _, forbidden := range forbiddenPaths {
		if strings.HasPrefix(cleanPath, forbidden) || cleanPath == strings.TrimSuffix(forbidden, "/") {
			return fmt.Errorf("%s: %s", ErrForbiddenPath, cleanPath)
		}
	}

	// 5. 检查符号链接
	// 解析符号链接后再次检查禁止路径
	realPath, err := filepath.EvalSymlinks(path)
	if err == nil && realPath != cleanPath {
		// 如果符号链接解析后的路径不同，检查真实路径
		for _, forbidden := range forbiddenPaths {
			if strings.HasPrefix(realPath, forbidden) || realPath == strings.TrimSuffix(forbidden, "/") {
				r.logger.Warn("Symlink points to forbidden path",
					zap.String("path", path),
					zap.String("real_path", realPath))
				return fmt.Errorf("%s: symlink points to %s", ErrForbiddenPath, realPath)
			}
		}
	} else if err != nil && os.IsPermission(err) {
		// 如果无法解析符号链接（权限问题），拒绝访问
		r.logger.Warn("Cannot resolve symlink due to permission",
			zap.String("path", path),
			zap.Error(err))
		return fmt.Errorf("%s: cannot resolve symlink", ErrPermissionDenied)
	}

	return nil
}

// IsForbiddenPath 检查路径是否为禁止访问的路径
// 用于外部调用检查
func IsForbiddenPath(path string) bool {
	cleanPath := filepath.Clean(path)
	for _, forbidden := range forbiddenPaths {
		if strings.HasPrefix(cleanPath, forbidden) || cleanPath == strings.TrimSuffix(forbidden, "/") {
			return true
		}
	}
	return false
}
