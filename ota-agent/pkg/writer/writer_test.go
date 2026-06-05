package writer

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/zap"
)

// 测试辅助函数
func setupTestWriter(t *testing.T) *Writer {
	logger, _ := zap.NewDevelopment()
	return New(logger)
}

func calculateSHA256(content []byte) string {
	hash := sha256.Sum256(content)
	return fmt.Sprintf("%x", hash)
}

func encodeBase64(content []byte) string {
	return base64.StdEncoding.EncodeToString(content)
}

// ========== ParseWriteRequest Tests ==========

func TestParseWriteRequest_Valid(t *testing.T) {
	content := []byte("test content")
	sha256Hash := calculateSHA256(content)

	payload := map[string]string{
		"path":      "/tmp/test.txt",
		"content":   encodeBase64(content),
		"mode":      "0o644",
		"sha256":    sha256Hash,
		"requestId": "req-123",
	}
	jsonPayload, _ := json.Marshal(payload)

	req, err := ParseWriteRequest(jsonPayload)
	if err != nil {
		t.Fatalf("ParseWriteRequest failed: %v", err)
	}

	if req.Path != "/tmp/test.txt" {
		t.Errorf("Expected path /tmp/test.txt, got %s", req.Path)
	}
	if req.Mode != "0o644" {
		t.Errorf("Expected mode 0o644, got %s", req.Mode)
	}
	if req.SHA256 != sha256Hash {
		t.Errorf("Expected sha256 %s, got %s", sha256Hash, req.SHA256)
	}
	if req.RequestID != "req-123" {
		t.Errorf("Expected requestId req-123, got %s", req.RequestID)
	}
}

func TestParseWriteRequest_DefaultMode(t *testing.T) {
	content := []byte("test content")
	sha256Hash := calculateSHA256(content)

	payload := map[string]string{
		"path":      "/tmp/test.txt",
		"content":   encodeBase64(content),
		"sha256":    sha256Hash,
		"requestId": "req-123",
	}
	jsonPayload, _ := json.Marshal(payload)

	req, err := ParseWriteRequest(jsonPayload)
	if err != nil {
		t.Fatalf("ParseWriteRequest failed: %v", err)
	}

	if req.Mode != DefaultFileMode {
		t.Errorf("Expected default mode %s, got %s", DefaultFileMode, req.Mode)
	}
}

func TestParseWriteRequest_EmptyPath(t *testing.T) {
	payload := map[string]string{
		"content":   "dGVzdA==",
		"sha256":    "abc123",
		"requestId": "req-123",
	}
	jsonPayload, _ := json.Marshal(payload)

	_, err := ParseWriteRequest(jsonPayload)
	if err == nil {
		t.Error("Expected error for empty path")
	}
	if err.Error() != ErrEmptyPath {
		t.Errorf("Expected error '%s', got '%s'", ErrEmptyPath, err.Error())
	}
}

func TestParseWriteRequest_EmptyContent(t *testing.T) {
	payload := map[string]string{
		"path":      "/tmp/test.txt",
		"sha256":    "abc123",
		"requestId": "req-123",
	}
	jsonPayload, _ := json.Marshal(payload)

	_, err := ParseWriteRequest(jsonPayload)
	if err == nil {
		t.Error("Expected error for empty content")
	}
	if err.Error() != ErrEmptyContent {
		t.Errorf("Expected error '%s', got '%s'", ErrEmptyContent, err.Error())
	}
}

func TestParseWriteRequest_EmptyRequestID(t *testing.T) {
	payload := map[string]string{
		"path":    "/tmp/test.txt",
		"content": "dGVzdA==",
		"sha256":  "abc123",
	}
	jsonPayload, _ := json.Marshal(payload)

	_, err := ParseWriteRequest(jsonPayload)
	if err == nil {
		t.Error("Expected error for empty requestId")
	}
	if err.Error() != ErrEmptyRequestID {
		t.Errorf("Expected error '%s', got '%s'", ErrEmptyRequestID, err.Error())
	}
}

func TestParseWriteRequest_EmptySHA256(t *testing.T) {
	payload := map[string]string{
		"path":      "/tmp/test.txt",
		"content":   "dGVzdA==",
		"requestId": "req-123",
	}
	jsonPayload, _ := json.Marshal(payload)

	_, err := ParseWriteRequest(jsonPayload)
	if err == nil {
		t.Error("Expected error for empty sha256")
	}
	if err.Error() != ErrEmptySHA256 {
		t.Errorf("Expected error '%s', got '%s'", ErrEmptySHA256, err.Error())
	}
}

func TestParseWriteRequest_InvalidJSON(t *testing.T) {
	_, err := ParseWriteRequest([]byte("invalid json"))
	if err == nil {
		t.Error("Expected error for invalid JSON")
	}
}

// ========== Writer.Write Tests ==========

func TestWriter_Write_Success(t *testing.T) {
	w := setupTestWriter(t)

	// 创建临时目录
	tmpDir := t.TempDir()
	testPath := filepath.Join(tmpDir, "test.txt")

	content := []byte("Hello, World!")
	sha256Hash := calculateSHA256(content)

	req := &WriteRequest{
		Path:      testPath,
		Content:   encodeBase64(content),
		Mode:      "0o644",
		SHA256:    sha256Hash,
		RequestID: "req-success",
	}

	resp := w.Write(req)

	if resp.Error != "" {
		t.Fatalf("Write failed: %s", resp.Error)
	}
	if !resp.Success {
		t.Error("Expected success=true")
	}
	if resp.Path != testPath {
		t.Errorf("Expected path %s, got %s", testPath, resp.Path)
	}
	if resp.Size != int64(len(content)) {
		t.Errorf("Expected size %d, got %d", len(content), resp.Size)
	}
	if resp.SHA256 != sha256Hash {
		t.Errorf("Expected sha256 %s, got %s", sha256Hash, resp.SHA256)
	}

	// 验证文件内容
	readContent, err := os.ReadFile(testPath)
	if err != nil {
		t.Fatalf("Failed to read written file: %v", err)
	}
	if string(readContent) != string(content) {
		t.Errorf("File content mismatch: expected %s, got %s", string(content), string(readContent))
	}
}

func TestWriter_Write_CreateParentDir(t *testing.T) {
	w := setupTestWriter(t)

	// 创建临时目录
	tmpDir := t.TempDir()
	testPath := filepath.Join(tmpDir, "subdir1", "subdir2", "test.txt")

	content := []byte("nested file content")
	sha256Hash := calculateSHA256(content)

	req := &WriteRequest{
		Path:      testPath,
		Content:   encodeBase64(content),
		Mode:      "0o644",
		SHA256:    sha256Hash,
		RequestID: "req-nested",
	}

	resp := w.Write(req)

	if resp.Error != "" {
		t.Fatalf("Write failed: %s", resp.Error)
	}
	if !resp.Success {
		t.Error("Expected success=true")
	}

	// 验证文件存在
	if _, err := os.Stat(testPath); os.IsNotExist(err) {
		t.Error("File was not created")
	}
}

func TestWriter_Write_ChecksumMismatch(t *testing.T) {
	w := setupTestWriter(t)

	tmpDir := t.TempDir()
	testPath := filepath.Join(tmpDir, "test.txt")

	content := []byte("test content")
	wrongSHA256 := "0000000000000000000000000000000000000000000000000000000000000000"

	req := &WriteRequest{
		Path:      testPath,
		Content:   encodeBase64(content),
		Mode:      "0o644",
		SHA256:    wrongSHA256,
		RequestID: "req-checksum",
	}

	resp := w.Write(req)

	if resp.Success {
		t.Error("Expected success=false for checksum mismatch")
	}
	if resp.Error == "" {
		t.Error("Expected error message")
	}
	if !contains(resp.Error, ErrChecksumMismatch) {
		t.Errorf("Expected error to contain '%s', got '%s'", ErrChecksumMismatch, resp.Error)
	}

	// 验证文件未创建
	if _, err := os.Stat(testPath); !os.IsNotExist(err) {
		t.Error("File should not be created on checksum mismatch")
	}
}

func TestWriter_Write_InvalidBase64(t *testing.T) {
	w := setupTestWriter(t)

	tmpDir := t.TempDir()
	testPath := filepath.Join(tmpDir, "test.txt")

	req := &WriteRequest{
		Path:      testPath,
		Content:   "invalid base64 !!!",
		Mode:      "0o644",
		SHA256:    "abc123",
		RequestID: "req-invalid-b64",
	}

	resp := w.Write(req)

	if resp.Success {
		t.Error("Expected success=false for invalid base64")
	}
	if !contains(resp.Error, ErrInvalidBase64) {
		t.Errorf("Expected error to contain '%s', got '%s'", ErrInvalidBase64, resp.Error)
	}
}

func TestWriter_Write_InvalidMode(t *testing.T) {
	w := setupTestWriter(t)

	tmpDir := t.TempDir()
	testPath := filepath.Join(tmpDir, "test.txt")

	content := []byte("test content")
	sha256Hash := calculateSHA256(content)

	req := &WriteRequest{
		Path:      testPath,
		Content:   encodeBase64(content),
		Mode:      "invalid",
		SHA256:    sha256Hash,
		RequestID: "req-invalid-mode",
	}

	resp := w.Write(req)

	if resp.Success {
		t.Error("Expected success=false for invalid mode")
	}
	if !contains(resp.Error, ErrInvalidMode) {
		t.Errorf("Expected error to contain '%s', got '%s'", ErrInvalidMode, resp.Error)
	}
}

func TestWriter_Write_FileMode(t *testing.T) {
	w := setupTestWriter(t)

	tmpDir := t.TempDir()
	testPath := filepath.Join(tmpDir, "test.txt")

	content := []byte("test content")
	sha256Hash := calculateSHA256(content)

	req := &WriteRequest{
		Path:      testPath,
		Content:   encodeBase64(content),
		Mode:      "0o755",
		SHA256:    sha256Hash,
		RequestID: "req-mode",
	}

	resp := w.Write(req)

	if resp.Error != "" {
		t.Fatalf("Write failed: %s", resp.Error)
	}

	// 验证文件权限
	fileInfo, err := os.Stat(testPath)
	if err != nil {
		t.Fatalf("Failed to stat file: %v", err)
	}

	expectedMode := os.FileMode(0o755)
	actualMode := fileInfo.Mode().Perm()
	if actualMode != expectedMode {
		t.Errorf("Expected mode %o, got %o", expectedMode, actualMode)
	}
}

// ========== Path Validation Tests ==========

func TestWriter_Write_ForbiddenPaths(t *testing.T) {
	w := setupTestWriter(t)

	content := []byte("test")
	sha256Hash := calculateSHA256(content)

	forbiddenPaths := []string{
		"/",
		"/bin/test",
		"/sbin/test",
		"/usr/bin/test",
		"/usr/sbin/test",
		"/lib/test",
		"/boot/test",
		"/dev/test",
		"/proc/test",
		"/sys/test",
		"/etc/shadow",
		"/etc/sudoers",
		"/etc/passwd",
		"/root/test",
	}

	for _, path := range forbiddenPaths {
		t.Run(path, func(t *testing.T) {
			req := &WriteRequest{
				Path:      path,
				Content:   encodeBase64(content),
				Mode:      "0o644",
				SHA256:    sha256Hash,
				RequestID: "req-forbidden",
			}

			resp := w.Write(req)

			if resp.Success {
				t.Errorf("Expected success=false for forbidden path: %s", path)
			}
			if !contains(resp.Error, ErrForbiddenPath) {
				t.Errorf("Expected error to contain '%s' for path %s, got '%s'", ErrForbiddenPath, path, resp.Error)
			}
		})
	}
}

func TestWriter_Write_PathTraversal(t *testing.T) {
	w := setupTestWriter(t)

	content := []byte("test")
	sha256Hash := calculateSHA256(content)

	traversalPaths := []string{
		"../etc/passwd",
		"./test/../../../etc/passwd",
		"relative/path",
	}

	for _, path := range traversalPaths {
		t.Run(path, func(t *testing.T) {
			req := &WriteRequest{
				Path:      path,
				Content:   encodeBase64(content),
				Mode:      "0o644",
				SHA256:    sha256Hash,
				RequestID: "req-traversal",
			}

			resp := w.Write(req)

			if resp.Success {
				t.Errorf("Expected success=false for path traversal: %s", path)
			}
			if !contains(resp.Error, ErrPathTraversal) {
				t.Errorf("Expected error to contain '%s' for path %s, got '%s'", ErrPathTraversal, path, resp.Error)
			}
		})
	}
}

func TestWriter_Write_AllowedPaths(t *testing.T) {
	w := setupTestWriter(t)

	content := []byte("test content")
	sha256Hash := calculateSHA256(content)

	// 使用临时目录作为允许的路径
	tmpDir := t.TempDir()

	allowedPaths := []string{
		filepath.Join(tmpDir, "test.txt"),
		filepath.Join(tmpDir, "subdir", "test.txt"),
	}

	for _, path := range allowedPaths {
		t.Run(path, func(t *testing.T) {
			req := &WriteRequest{
				Path:      path,
				Content:   encodeBase64(content),
				Mode:      "0o644",
				SHA256:    sha256Hash,
				RequestID: "req-allowed",
			}

			resp := w.Write(req)

			if !resp.Success {
				t.Errorf("Expected success=true for allowed path: %s, error: %s", path, resp.Error)
			}
		})
	}
}

// ========== Response Tests ==========

func TestNewSuccessResponse(t *testing.T) {
	resp := NewSuccessResponse("req-123", "/tmp/test.txt", 1024, "abc123")

	if resp.RequestID != "req-123" {
		t.Errorf("Expected requestId 'req-123', got '%s'", resp.RequestID)
	}
	if !resp.Success {
		t.Error("Expected success=true")
	}
	if resp.Path != "/tmp/test.txt" {
		t.Errorf("Expected path '/tmp/test.txt', got '%s'", resp.Path)
	}
	if resp.Size != 1024 {
		t.Errorf("Expected size 1024, got %d", resp.Size)
	}
	if resp.SHA256 != "abc123" {
		t.Errorf("Expected sha256 'abc123', got '%s'", resp.SHA256)
	}
	if resp.Error != "" {
		t.Errorf("Expected empty error, got '%s'", resp.Error)
	}
}

func TestNewErrorResponse(t *testing.T) {
	resp := NewErrorResponse("req-123", "test error")

	if resp.RequestID != "req-123" {
		t.Errorf("Expected requestId 'req-123', got '%s'", resp.RequestID)
	}
	if resp.Success {
		t.Error("Expected success=false")
	}
	if resp.Error != "test error" {
		t.Errorf("Expected error 'test error', got '%s'", resp.Error)
	}
}

func TestWriteResponse_ToJSON(t *testing.T) {
	resp := NewSuccessResponse("req-123", "/tmp/test.txt", 1024, "abc123")
	jsonData, err := resp.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON failed: %v", err)
	}

	var decoded WriteResponse
	if err := json.Unmarshal(jsonData, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal JSON: %v", err)
	}

	if decoded.RequestID != resp.RequestID {
		t.Errorf("RequestID mismatch after JSON roundtrip")
	}
	if decoded.Success != resp.Success {
		t.Errorf("Success mismatch after JSON roundtrip")
	}
}

// ========== IsForbiddenWritePath Tests ==========

func TestIsForbiddenWritePath(t *testing.T) {
	tests := []struct {
		path     string
		expected bool
	}{
		{"/", true},
		{"/bin", true},
		{"/bin/bash", true},
		{"/sbin", true},
		{"/usr/bin", true},
		{"/usr/bin/vim", true},
		{"/lib", true},
		{"/boot", true},
		{"/dev", true},
		{"/proc", true},
		{"/sys", true},
		{"/etc/shadow", true},
		{"/etc/passwd", true},
		{"/root", true},
		{"/root/.bashrc", true},
		{"/tmp", false},
		{"/tmp/test.txt", false},
		{"/home/user", false},
		{"/var/log", false},
		{"/opt/app", false},
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			result := IsForbiddenWritePath(tc.path)
			if result != tc.expected {
				t.Errorf("IsForbiddenWritePath(%s) = %v, expected %v", tc.path, result, tc.expected)
			}
		})
	}
}

// ========== Symlink Validation Tests ==========

func TestWriter_Write_SymlinkToForbiddenPath(t *testing.T) {
	// Skip on systems where symlink creation may fail
	if os.Getuid() == 0 {
		t.Skip("Skipping symlink test when running as root")
	}

	w := setupTestWriter(t)

	tmpDir := t.TempDir()

	// Create a symlink that points to a forbidden directory (simulating attack)
	// Note: We can't actually create a symlink to /bin on most systems without permission
	// So we test the path validation logic with a subdirectory symlink

	// Create a nested structure where the parent is a symlink
	realDir := filepath.Join(tmpDir, "realdir")
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatalf("Failed to create real directory: %v", err)
	}

	symlinkDir := filepath.Join(tmpDir, "symlinkdir")
	if err := os.Symlink(realDir, symlinkDir); err != nil {
		t.Skip("Cannot create symlinks on this system")
	}

	// Test writing through symlink (should succeed for non-forbidden paths)
	content := []byte("test content through symlink")
	sha256Hash := calculateSHA256(content)

	testPath := filepath.Join(symlinkDir, "test.txt")
	req := &WriteRequest{
		Path:      testPath,
		Content:   encodeBase64(content),
		Mode:      "0o644",
		SHA256:    sha256Hash,
		RequestID: "req-symlink",
	}

	resp := w.Write(req)

	// Writing through symlink to allowed path should succeed
	if !resp.Success {
		t.Errorf("Expected success=true for symlink to allowed path, got error: %s", resp.Error)
	}
}

// ========== Atomic Write Tests ==========

func TestWriter_AtomicWrite_Overwrite(t *testing.T) {
	w := setupTestWriter(t)

	tmpDir := t.TempDir()
	testPath := filepath.Join(tmpDir, "test.txt")

	// 第一次写入
	content1 := []byte("first content")
	sha256_1 := calculateSHA256(content1)
	req1 := &WriteRequest{
		Path:      testPath,
		Content:   encodeBase64(content1),
		Mode:      "0o644",
		SHA256:    sha256_1,
		RequestID: "req-1",
	}
	resp1 := w.Write(req1)
	if !resp1.Success {
		t.Fatalf("First write failed: %s", resp1.Error)
	}

	// 第二次写入（覆盖）
	content2 := []byte("second content - different")
	sha256_2 := calculateSHA256(content2)
	req2 := &WriteRequest{
		Path:      testPath,
		Content:   encodeBase64(content2),
		Mode:      "0o644",
		SHA256:    sha256_2,
		RequestID: "req-2",
	}
	resp2 := w.Write(req2)
	if !resp2.Success {
		t.Fatalf("Second write failed: %s", resp2.Error)
	}

	// 验证最终内容
	readContent, err := os.ReadFile(testPath)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}
	if string(readContent) != string(content2) {
		t.Errorf("Expected content '%s', got '%s'", string(content2), string(readContent))
	}
}

func TestWriter_Write_BinaryContent(t *testing.T) {
	w := setupTestWriter(t)

	tmpDir := t.TempDir()
	testPath := filepath.Join(tmpDir, "binary.bin")

	// 创建包含各种字节的二进制内容
	content := make([]byte, 256)
	for i := 0; i < 256; i++ {
		content[i] = byte(i)
	}
	sha256Hash := calculateSHA256(content)

	req := &WriteRequest{
		Path:      testPath,
		Content:   encodeBase64(content),
		Mode:      "0o644",
		SHA256:    sha256Hash,
		RequestID: "req-binary",
	}

	resp := w.Write(req)

	if !resp.Success {
		t.Fatalf("Write failed: %s", resp.Error)
	}

	// 验证二进制内容完全匹配
	readContent, err := os.ReadFile(testPath)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}
	if len(readContent) != len(content) {
		t.Errorf("Content length mismatch: expected %d, got %d", len(content), len(readContent))
	}
	for i := 0; i < len(content); i++ {
		if readContent[i] != content[i] {
			t.Errorf("Content mismatch at byte %d: expected %d, got %d", i, content[i], readContent[i])
			break
		}
	}
}

func TestWriter_Write_EmptyFile(t *testing.T) {
	w := setupTestWriter(t)

	tmpDir := t.TempDir()
	testPath := filepath.Join(tmpDir, "empty.txt")

	content := []byte{}
	sha256Hash := calculateSHA256(content)

	req := &WriteRequest{
		Path:      testPath,
		Content:   encodeBase64(content),
		Mode:      "0o644",
		SHA256:    sha256Hash,
		RequestID: "req-empty",
	}

	resp := w.Write(req)

	if !resp.Success {
		t.Fatalf("Write failed: %s", resp.Error)
	}
	if resp.Size != 0 {
		t.Errorf("Expected size 0, got %d", resp.Size)
	}

	// 验证文件存在且为空
	fileInfo, err := os.Stat(testPath)
	if err != nil {
		t.Fatalf("Failed to stat file: %v", err)
	}
	if fileInfo.Size() != 0 {
		t.Errorf("Expected empty file, got size %d", fileInfo.Size())
	}
}

// 辅助函数
func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}
