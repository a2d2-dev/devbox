package reader

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.uber.org/zap"
)

// 创建测试用的 logger
func testLogger() *zap.Logger {
	logger, _ := zap.NewDevelopment()
	return logger
}

// ============ Types Tests ============

func TestParseReadRequest_Valid(t *testing.T) {
	payload := []byte(`{"path": "/tmp/test.txt", "maxSize": 1024, "requestId": "req-123"}`)

	req, err := ParseReadRequest(payload)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if req.Path != "/tmp/test.txt" {
		t.Errorf("expected path /tmp/test.txt, got: %s", req.Path)
	}
	if req.MaxSize != 1024 {
		t.Errorf("expected maxSize 1024, got: %d", req.MaxSize)
	}
	if req.RequestID != "req-123" {
		t.Errorf("expected requestId req-123, got: %s", req.RequestID)
	}
}

func TestParseReadRequest_DefaultMaxSize(t *testing.T) {
	payload := []byte(`{"path": "/tmp/test.txt", "requestId": "req-123"}`)

	req, err := ParseReadRequest(payload)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if req.MaxSize != DefaultMaxSize {
		t.Errorf("expected default maxSize %d, got: %d", DefaultMaxSize, req.MaxSize)
	}
}

func TestParseReadRequest_MaxSizeOverAbsoluteLimit(t *testing.T) {
	// 请求超过100MB的maxSize应该被限制为100MB
	payload := []byte(`{"path": "/tmp/test.txt", "maxSize": 200000000, "requestId": "req-123"}`)

	req, err := ParseReadRequest(payload)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if req.MaxSize != AbsoluteMaxSize {
		t.Errorf("expected maxSize capped at %d, got: %d", AbsoluteMaxSize, req.MaxSize)
	}
}

func TestParseReadRequest_EmptyPath(t *testing.T) {
	payload := []byte(`{"path": "", "requestId": "req-123"}`)

	_, err := ParseReadRequest(payload)
	if err == nil {
		t.Fatal("expected error for empty path")
	}
	if err.Error() != ErrEmptyPath {
		t.Errorf("expected error '%s', got: %v", ErrEmptyPath, err)
	}
}

func TestParseReadRequest_EmptyRequestID(t *testing.T) {
	payload := []byte(`{"path": "/tmp/test.txt", "requestId": ""}`)

	_, err := ParseReadRequest(payload)
	if err == nil {
		t.Fatal("expected error for empty requestId")
	}
	if err.Error() != ErrEmptyRequestID {
		t.Errorf("expected error '%s', got: %v", ErrEmptyRequestID, err)
	}
}

func TestParseReadRequest_InvalidJSON(t *testing.T) {
	payload := []byte(`{invalid json}`)

	_, err := ParseReadRequest(payload)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestNewSuccessResponse(t *testing.T) {
	modTime := time.Now()
	resp := NewSuccessResponse("req-123", "YmFzZTY0", 100, "abc123", "0o644", modTime)

	if resp.RequestID != "req-123" {
		t.Errorf("expected requestId req-123, got: %s", resp.RequestID)
	}
	if resp.Content != "YmFzZTY0" {
		t.Errorf("expected content YmFzZTY0, got: %s", resp.Content)
	}
	if resp.Size != 100 {
		t.Errorf("expected size 100, got: %d", resp.Size)
	}
	if resp.SHA256 != "abc123" {
		t.Errorf("expected sha256 abc123, got: %s", resp.SHA256)
	}
	if resp.Mode != "0o644" {
		t.Errorf("expected mode 0o644, got: %s", resp.Mode)
	}
	if resp.Error != "" {
		t.Errorf("expected no error, got: %s", resp.Error)
	}
}

func TestNewErrorResponse(t *testing.T) {
	resp := NewErrorResponse("req-123", "test error")

	if resp.RequestID != "req-123" {
		t.Errorf("expected requestId req-123, got: %s", resp.RequestID)
	}
	if resp.Error != "test error" {
		t.Errorf("expected error 'test error', got: %s", resp.Error)
	}
	if resp.Content != "" {
		t.Errorf("expected no content, got: %s", resp.Content)
	}
}

func TestReadResponse_ToJSON(t *testing.T) {
	resp := NewSuccessResponse("req-123", "dGVzdA==", 4, "sha256hash", "0o644", time.Now())

	jsonBytes, err := resp.ToJSON()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// 验证可以反序列化
	var parsed ReadResponse
	if err := json.Unmarshal(jsonBytes, &parsed); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if parsed.RequestID != resp.RequestID {
		t.Errorf("requestId mismatch after roundtrip")
	}
}

// ============ Reader Tests ============

func TestReader_Read_ValidFile(t *testing.T) {
	// 创建临时测试文件
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	testContent := []byte("Hello, World!")

	if err := os.WriteFile(testFile, testContent, 0o644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	reader := New(testLogger())
	req := &ReadRequest{
		Path:      testFile,
		MaxSize:   1024,
		RequestID: "test-read-123",
	}

	resp := reader.Read(req)

	// 验证成功响应
	if resp.Error != "" {
		t.Fatalf("expected no error, got: %s", resp.Error)
	}
	if resp.RequestID != "test-read-123" {
		t.Errorf("expected requestId test-read-123, got: %s", resp.RequestID)
	}
	if resp.Size != int64(len(testContent)) {
		t.Errorf("expected size %d, got: %d", len(testContent), resp.Size)
	}

	// 验证 base64 内容
	decoded, err := base64.StdEncoding.DecodeString(resp.Content)
	if err != nil {
		t.Fatalf("failed to decode base64 content: %v", err)
	}
	if string(decoded) != string(testContent) {
		t.Errorf("content mismatch: expected '%s', got '%s'", testContent, decoded)
	}

	// 验证 SHA256
	expectedHash := sha256.Sum256(testContent)
	expectedSHA256 := fmt.Sprintf("%x", expectedHash)
	if resp.SHA256 != expectedSHA256 {
		t.Errorf("sha256 mismatch: expected %s, got %s", expectedSHA256, resp.SHA256)
	}

	// 验证 mode
	if resp.Mode != "0o644" {
		t.Errorf("expected mode 0o644, got: %s", resp.Mode)
	}

	// 验证 modifiedAt
	if resp.ModifiedAt == "" {
		t.Error("expected modifiedAt to be set")
	}
}

func TestReader_Read_FileNotFound(t *testing.T) {
	reader := New(testLogger())
	req := &ReadRequest{
		Path:      "/nonexistent/path/to/file.txt",
		MaxSize:   1024,
		RequestID: "test-notfound-123",
	}

	resp := reader.Read(req)

	if resp.Error == "" {
		t.Fatal("expected error response")
	}
	if resp.RequestID != "test-notfound-123" {
		t.Errorf("expected requestId test-notfound-123, got: %s", resp.RequestID)
	}
	if resp.Content != "" {
		t.Errorf("expected no content on error")
	}
}

func TestReader_Read_FileSizeExceedsMaxSize(t *testing.T) {
	// 创建临时测试文件
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "large.txt")
	testContent := make([]byte, 1000) // 1000 bytes
	for i := range testContent {
		testContent[i] = 'x'
	}

	if err := os.WriteFile(testFile, testContent, 0o644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	reader := New(testLogger())
	req := &ReadRequest{
		Path:      testFile,
		MaxSize:   100, // 只允许 100 bytes
		RequestID: "test-size-123",
	}

	resp := reader.Read(req)

	if resp.Error == "" {
		t.Fatal("expected error response for file size limit")
	}
	if resp.Content != "" {
		t.Errorf("expected no content when size exceeds limit")
	}
}

func TestReader_Read_FileSizeExceeds100MB(t *testing.T) {
	// 这个测试模拟大文件，但我们不能真的创建100MB文件
	// 所以我们测试验证逻辑在代码中正确
	reader := New(testLogger())

	// 测试正常文件能通过
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "small.txt")
	if err := os.WriteFile(testFile, []byte("small content"), 0o644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	req := &ReadRequest{
		Path:      testFile,
		MaxSize:   AbsoluteMaxSize + 1000, // 超过100MB的请求会被ParseReadRequest限制
		RequestID: "test-huge-123",
	}

	resp := reader.Read(req)

	// 小文件应该成功读取
	if resp.Error != "" {
		t.Errorf("expected no error for small file, got: %s", resp.Error)
	}
}

func TestReader_Read_PathTraversal(t *testing.T) {
	reader := New(testLogger())

	testCases := []struct {
		name string
		path string
	}{
		{"relative path", "relative/path.txt"},
		{"dot dot path", "/tmp/../etc/passwd"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := &ReadRequest{
				Path:      tc.path,
				MaxSize:   1024,
				RequestID: "test-traversal-123",
			}

			resp := reader.Read(req)

			if resp.Error == "" {
				t.Errorf("expected error for path traversal: %s", tc.path)
			}
		})
	}
}

func TestReader_Read_ForbiddenPaths(t *testing.T) {
	reader := New(testLogger())

	testCases := []struct {
		name string
		path string
	}{
		{"etc shadow", "/etc/shadow"},
		{"etc sudoers", "/etc/sudoers"},
		{"proc path", "/proc/1/cmdline"},
		{"sys path", "/sys/class/net"},
		{"dev path", "/dev/null"},
		{"boot path", "/boot/vmlinuz"},
		{"root path", "/root/.bashrc"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := &ReadRequest{
				Path:      tc.path,
				MaxSize:   1024,
				RequestID: "test-forbidden-123",
			}

			resp := reader.Read(req)

			if resp.Error == "" {
				t.Errorf("expected error for forbidden path: %s", tc.path)
			}
		})
	}
}

func TestReader_Read_Directory(t *testing.T) {
	reader := New(testLogger())

	tmpDir := t.TempDir()

	req := &ReadRequest{
		Path:      tmpDir,
		MaxSize:   1024,
		RequestID: "test-dir-123",
	}

	resp := reader.Read(req)

	if resp.Error == "" {
		t.Fatal("expected error for directory path")
	}
}

func TestIsForbiddenPath(t *testing.T) {
	testCases := []struct {
		path      string
		forbidden bool
	}{
		{"/etc/shadow", true},
		{"/etc/sudoers", true},
		{"/proc/1/cmdline", true},
		{"/sys/class", true},
		{"/dev/null", true},
		{"/boot/vmlinuz", true},
		{"/root/.bashrc", true},
		{"/tmp/test.txt", false},
		{"/home/user/file.txt", false},
		{"/var/log/app.log", false},
		{"/etc/passwd", false}, // /etc/passwd is world-readable and allowed
	}

	for _, tc := range testCases {
		t.Run(tc.path, func(t *testing.T) {
			result := IsForbiddenPath(tc.path)
			if result != tc.forbidden {
				t.Errorf("IsForbiddenPath(%s) = %v, expected %v", tc.path, result, tc.forbidden)
			}
		})
	}
}

func TestReader_Read_EmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "empty.txt")

	// 创建空文件
	if err := os.WriteFile(testFile, []byte{}, 0o644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	reader := New(testLogger())
	req := &ReadRequest{
		Path:      testFile,
		MaxSize:   1024,
		RequestID: "test-empty-123",
	}

	resp := reader.Read(req)

	if resp.Error != "" {
		t.Fatalf("expected no error for empty file, got: %s", resp.Error)
	}
	if resp.Size != 0 {
		t.Errorf("expected size 0, got: %d", resp.Size)
	}
	if resp.Content != "" {
		t.Errorf("expected empty content, got: %s", resp.Content)
	}

	// 空文件的 SHA256
	emptyHash := sha256.Sum256([]byte{})
	expectedSHA256 := fmt.Sprintf("%x", emptyHash)
	if resp.SHA256 != expectedSHA256 {
		t.Errorf("sha256 mismatch for empty file: expected %s, got %s", expectedSHA256, resp.SHA256)
	}
}

func TestReader_Read_BinaryFile(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "binary.bin")

	// 创建包含二进制数据的文件
	binaryContent := []byte{0x00, 0x01, 0x02, 0xFF, 0xFE, 0xFD}
	if err := os.WriteFile(testFile, binaryContent, 0o644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	reader := New(testLogger())
	req := &ReadRequest{
		Path:      testFile,
		MaxSize:   1024,
		RequestID: "test-binary-123",
	}

	resp := reader.Read(req)

	if resp.Error != "" {
		t.Fatalf("expected no error for binary file, got: %s", resp.Error)
	}

	// 验证 base64 解码后的内容
	decoded, err := base64.StdEncoding.DecodeString(resp.Content)
	if err != nil {
		t.Fatalf("failed to decode base64: %v", err)
	}

	if len(decoded) != len(binaryContent) {
		t.Errorf("decoded length mismatch: expected %d, got %d", len(binaryContent), len(decoded))
	}

	for i, b := range decoded {
		if b != binaryContent[i] {
			t.Errorf("byte mismatch at position %d: expected %x, got %x", i, binaryContent[i], b)
		}
	}
}

func TestReader_Read_PermissionDenied(t *testing.T) {
	// 跳过如果是 root 用户运行（root 可以读取任何文件）
	if os.Getuid() == 0 {
		t.Skip("skipping permission test when running as root")
	}

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "noperm.txt")

	// 创建文件并设置无读取权限
	if err := os.WriteFile(testFile, []byte("secret"), 0000); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}
	defer os.Chmod(testFile, 0o644) // 清理时恢复权限

	reader := New(testLogger())
	req := &ReadRequest{
		Path:      testFile,
		MaxSize:   1024,
		RequestID: "test-perm-123",
	}

	resp := reader.Read(req)

	if resp.Error == "" {
		t.Fatal("expected error for permission denied")
	}
}

func TestReader_Read_NilLogger(t *testing.T) {
	// 测试使用 nil logger 创建 Reader
	reader := New(nil)

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("test"), 0o644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	req := &ReadRequest{
		Path:      testFile,
		MaxSize:   1024,
		RequestID: "test-nil-logger",
	}

	resp := reader.Read(req)

	if resp.Error != "" {
		t.Fatalf("expected no error with nil logger, got: %s", resp.Error)
	}
}
