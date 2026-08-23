package shares

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestWebDAVAuthenticationAndReadWrite(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "hello.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	port := freePort(t)
	service := NewWebDAVService()
	if err := service.Start(root, WebDAVConfig{Enabled: true, Port: port, Path: root}, "secret"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Stop(context.Background()) })

	base := "http://127.0.0.1:" + strconv.Itoa(port)
	resp, err := http.Get(base + "/hello.txt")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d", resp.StatusCode)
	}

	req, _ := http.NewRequest(http.MethodPut, base+"/created.txt", strings.NewReader("created"))
	req.SetBasicAuth("devbox", "secret")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		t.Fatalf("PUT status = %d", resp.StatusCode)
	}

	req, _ = http.NewRequest("PROPFIND", base+"/", nil)
	req.Header.Set("Depth", "1")
	req.SetBasicAuth("devbox", "secret")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusMultiStatus || !strings.Contains(string(body), "created.txt") {
		t.Fatalf("PROPFIND status/body = %d %s", resp.StatusCode, body)
	}
}

func TestWebDAVReadOnlyAndPortConflict(t *testing.T) {
	root := t.TempDir()
	service := NewWebDAVService()
	port := freePort(t)
	if err := service.Start(root, WebDAVConfig{Enabled: true, Port: port, Path: root, ReadOnly: true}, "secret"); err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodPut, "http://127.0.0.1:"+strconv.Itoa(port)+"/blocked.txt", strings.NewReader("x"))
	req.SetBasicAuth("devbox", "secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("read-only PUT status = %d, want 403", resp.StatusCode)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	conflictPort := ln.Addr().(*net.TCPAddr).Port
	if err := service.Start(root, WebDAVConfig{Enabled: true, Port: conflictPort, Path: root}, "secret"); err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("expected clear port conflict, got %v", err)
	}
	if !service.Status().Running || service.Config().Port != port {
		t.Fatalf("port conflict stopped the existing service: %+v", service.Status())
	}
	_ = service.Stop(context.Background())
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return port
}
