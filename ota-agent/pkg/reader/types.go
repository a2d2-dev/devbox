package reader

import (
	"encoding/json"
	"fmt"
	"time"
)

// ReadRequest 文件读取请求
// 由 OTA Server 通过 MQTT 发送到 Agent
type ReadRequest struct {
	Path      string `json:"path"`      // 文件路径（必需）
	MaxSize   int64  `json:"maxSize"`   // 最大允许大小（字节，可选，默认10MB）
	RequestID string `json:"requestId"` // 请求ID（必需，用于关联响应）
}

// ReadResponse 文件读取响应
// Agent 处理完成后发送到 MQTT result topic
type ReadResponse struct {
	RequestID  string `json:"requestId"`            // 请求ID（必需）
	Content    string `json:"content,omitempty"`    // base64编码内容（成功时）
	Size       int64  `json:"size,omitempty"`       // 文件大小（字节，成功时）
	SHA256     string `json:"sha256,omitempty"`     // SHA256校验和（成功时）
	Mode       string `json:"mode,omitempty"`       // 文件权限（如 "0o644"，成功时）
	ModifiedAt string `json:"modifiedAt,omitempty"` // 修改时间（RFC3339格式，成功时）
	Error      string `json:"error,omitempty"`      // 错误信息（失败时）
}

// 常量定义
const (
	// DefaultMaxSize 默认最大文件大小（10MB）
	DefaultMaxSize = 10 * 1024 * 1024 // 10MB

	// AbsoluteMaxSize 绝对最大文件大小限制（100MB）
	// 超过此大小的文件请求会被直接拒绝
	AbsoluteMaxSize = 100 * 1024 * 1024 // 100MB
)

// 错误信息常量
const (
	ErrEmptyPath          = "path is required"
	ErrEmptyRequestID     = "requestId is required"
	ErrFileNotFound       = "file not found"
	ErrPermissionDenied   = "permission denied"
	ErrFileSizeExceedsMax = "file size exceeds limit"
	ErrPathTraversal      = "path traversal attack detected"
	ErrForbiddenPath      = "forbidden path"
	ErrReadFailed         = "failed to read file"
)

// 禁止访问的路径前缀
// 注意: /etc/passwd 是世界可读的，不在禁止列表中
var forbiddenPaths = []string{
	"/etc/shadow",
	"/etc/sudoers",
	"/proc/",
	"/sys/",
	"/dev/",
	"/boot/",
	"/root/",
}

// ParseReadRequest 从 JSON payload 解析读取请求
// 返回解析后的 ReadRequest 或解析错误
func ParseReadRequest(payload []byte) (*ReadRequest, error) {
	var req ReadRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, fmt.Errorf("failed to unmarshal read request: %w", err)
	}

	// 验证必需字段
	if req.Path == "" {
		return nil, fmt.Errorf(ErrEmptyPath)
	}

	if req.RequestID == "" {
		return nil, fmt.Errorf(ErrEmptyRequestID)
	}

	// 设置默认值
	if req.MaxSize <= 0 {
		req.MaxSize = DefaultMaxSize
	}

	// 检查 MaxSize 不能超过绝对限制
	if req.MaxSize > AbsoluteMaxSize {
		req.MaxSize = AbsoluteMaxSize
	}

	return &req, nil
}

// NewSuccessResponse 创建成功响应
func NewSuccessResponse(requestID string, content string, size int64, sha256 string, mode string, modifiedAt time.Time) *ReadResponse {
	return &ReadResponse{
		RequestID:  requestID,
		Content:    content,
		Size:       size,
		SHA256:     sha256,
		Mode:       mode,
		ModifiedAt: modifiedAt.UTC().Format(time.RFC3339),
	}
}

// NewErrorResponse 创建错误响应
func NewErrorResponse(requestID string, errorMsg string) *ReadResponse {
	return &ReadResponse{
		RequestID: requestID,
		Error:     errorMsg,
	}
}

// ToJSON 将响应序列化为 JSON
func (r *ReadResponse) ToJSON() ([]byte, error) {
	return json.Marshal(r)
}
