package shares

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeRunner struct {
	paths  map[string]bool
	active bool
	fail   map[string]error
	out    map[string]string
	calls  []string
}

func (f *fakeRunner) LookPath(name string) (string, error) {
	if f.paths[name] {
		return "/usr/bin/" + name, nil
	}
	return "", errors.New("not found")
}
func (f *fakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	key := name + " " + strings.Join(args, " ")
	f.calls = append(f.calls, key)
	if name == "systemctl" && len(args) > 0 && args[0] == "is-active" {
		if f.active {
			return []byte("active\n"), nil
		}
		return []byte("inactive\n"), errors.New("exit 3")
	}
	for prefix, err := range f.fail {
		if strings.HasPrefix(key, prefix) {
			return []byte(f.out[prefix]), err
		}
	}
	for prefix, out := range f.out {
		if strings.HasPrefix(key, prefix) {
			return []byte(out), nil
		}
	}
	return nil, nil
}

func TestRenderSMBAndBoundary(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	if err := os.Mkdir(project, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := RenderSMB(root, []SMBShare{{Name: "code", Path: project, Guest: true}})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"[code]", "path = " + project, "read only = no", "guest ok = yes"} {
		if !strings.Contains(got, want) {
			t.Fatalf("rendered config missing %q:\n%s", want, got)
		}
	}
	if _, err := RenderSMB(root, []SMBShare{{Name: "bad]\n[global", Path: project}}); err == nil {
		t.Fatal("expected injected name to be rejected")
	}
	if _, err := RenderSMB(root, []SMBShare{{Name: "escape", Path: filepath.Dir(root)}}); err == nil {
		t.Fatal("expected outside path to be rejected")
	}
}

func TestApplySMBTestparmFailureDoesNotWrite(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "managed.conf")
	if err := os.WriteFile(target, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{
		paths: map[string]bool{"smbd": true, "testparm": true},
		fail:  map[string]error{"testparm ": errors.New("invalid")},
		out:   map[string]string{"testparm ": "ERROR invalid value"},
	}
	_, err := ApplySMB(context.Background(), runner, root, target, []SMBShare{{Name: "data", Path: root}})
	if err == nil || !strings.Contains(err.Error(), "testparm rejected") {
		t.Fatalf("expected validation error, got %v", err)
	}
	got, _ := os.ReadFile(target)
	if string(got) != "original" {
		t.Fatalf("target changed after failed validation: %q", got)
	}
}

func TestApplySMBRefusesActiveConnections(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "managed.conf")
	runner := &fakeRunner{
		paths:  map[string]bool{"smbd": true, "testparm": true, "smbstatus": true},
		active: true,
		out: map[string]string{
			"smbstatus ": "Service pid Machine Connected at Encryption Signing\ndata 123 client now - -\n",
		},
	}
	_, err := ApplySMB(context.Background(), runner, root, target, []SMBShare{{Name: "data", Path: root}})
	if err == nil || !strings.Contains(err.Error(), "active connections") {
		t.Fatalf("expected active connection refusal, got %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("managed file should not exist, stat err = %v", err)
	}
}
