package writer

import (
	"encoding/json"
	"fmt"
)

// WriteRequest 文件写入请求
// 由 OTA Server 通过 MQTT 发送到 Agent
type WriteRequest struct {
	Path      string `json:"path"`      // 目标文件路径（必需）
	Content   string `json:"content"`   // base64编码内容（必需）
	Mode      string `json:"mode"`      // 文件权限（如 "0o644"，可选，默认0644）
	SHA256    string `json:"sha256"`    // 期望的SHA256校验和（必需）
	RequestID string `json:"requestId"` // 请求ID（必需，用于关联响应）
}

// WriteResponse 文件写入响应
// Agent 处理完成后发送到 MQTT result topic
type WriteResponse struct {
	RequestID string `json:"requestId"`        // 请求ID（必需）
	Success   bool   `json:"success"`          // 是否成功
	Path      string `json:"path,omitempty"`   // 写入的文件路径（成功时）
	Size      int64  `json:"size,omitempty"`   // 文件大小（字节，成功时）
	SHA256    string `json:"sha256,omitempty"` // 实际SHA256校验和（成功时）
	Error     string `json:"error,omitempty"`  // 错误信息（失败时）
}

// 常量定义
const (
	// DefaultFileMode 默认文件权限
	DefaultFileMode = "0o644"

	// MaxContentSize 最大内容大小（100MB base64编码后约133MB）
	// 与 reader 包的 AbsoluteMaxSize 保持一致
	MaxContentSize = 100 * 1024 * 1024 // 100MB
)

// 错误信息常量
const (
	ErrEmptyPath          = "path is required"
	ErrEmptyContent       = "content is required"
	ErrEmptyRequestID     = "requestId is required"
	ErrEmptySHA256        = "sha256 is required"
	ErrInvalidBase64      = "invalid base64 content"
	ErrChecksumMismatch   = "checksum mismatch"
	ErrPermissionDenied   = "permission denied"
	ErrDiskFull           = "disk full"
	ErrPathTraversal      = "path traversal attack detected"
	ErrForbiddenPath      = "forbidden path"
	ErrWriteFailed        = "failed to write file"
	ErrInvalidMode        = "invalid file mode"
	ErrContentTooLarge    = "content size exceeds limit"
	ErrCreateDirFailed    = "failed to create parent directory"
	ErrAtomicRenameFailed = "failed to atomically rename file"
)

// 禁止写入的路径前缀
// 比 reader 更严格，防止写入系统关键目录
var forbiddenWritePaths = []string{
	"/",          // 根目录
	"/bin",       // 系统二进制
	"/sbin",      // 系统管理二进制
	"/usr/bin",   // 用户二进制
	"/usr/sbin",  // 用户管理二进制
	"/lib",       // 系统库
	"/lib64",     // 64位系统库
	"/usr/lib",   // 用户库
	"/usr/lib64", // 用户64位库
	"/boot",      // 启动目录
	"/dev",       // 设备目录
	"/proc",      // 进程目录
	"/sys",       // 系统目录
	"/etc/shadow",
	"/etc/sudoers",
	"/etc/passwd",
	"/root",
}

// ParseWriteRequest 从 JSON payload 解析写入请求
// 返回解析后的 WriteRequest 或解析错误
func ParseWriteRequest(payload []byte) (*WriteRequest, error) {
	var req WriteRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, fmt.Errorf("failed to unmarshal write request: %w", err)
	}

	// 验证必需字段
	if req.Path == "" {
		return nil, fmt.Errorf(ErrEmptyPath)
	}

	if req.Content == "" {
		return nil, fmt.Errorf(ErrEmptyContent)
	}

	if req.RequestID == "" {
		return nil, fmt.Errorf(ErrEmptyRequestID)
	}

	if req.SHA256 == "" {
		return nil, fmt.Errorf(ErrEmptySHA256)
	}

	// 设置默认文件权限
	if req.Mode == "" {
		req.Mode = DefaultFileMode
	}

	return &req, nil
}

// NewSuccessResponse 创建成功响应
func NewSuccessResponse(requestID string, path string, size int64, sha256 string) *WriteResponse {
	return &WriteResponse{
		RequestID: requestID,
		Success:   true,
		Path:      path,
		Size:      size,
		SHA256:    sha256,
	}
}

// NewErrorResponse 创建错误响应
func NewErrorResponse(requestID string, errorMsg string) *WriteResponse {
	return &WriteResponse{
		RequestID: requestID,
		Success:   false,
		Error:     errorMsg,
	}
}

// ToJSON 将响应序列化为 JSON
func (r *WriteResponse) ToJSON() ([]byte, error) {
	return json.Marshal(r)
}
