package console

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/a2d2-dev/devbox/pkg/auth"
	"github.com/a2d2-dev/devbox/pkg/files"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testFileServer(t *testing.T, password string) (*Server, *files.Browser, string) {
	t.Helper()
	root := t.TempDir()
	browser := files.NewBrowser(files.Config{RootDir: root, AppsDir: filepath.Join(root, "apps"), StateDir: filepath.Join(root, ".state"), MountsFile: filepath.Join(root, "mounts")})
	s := &Server{mux: http.NewServeMux(), fileBrowser: browser, auth: auth.New(auth.Config{Password: password, SessionTTL: 3600})}
	s.registerFileRoutes()
	return s, browser, root
}

func TestPublicShareBypassesAuthThenExpiresOrRevokes(t *testing.T) {
	s, browser, root := testFileServer(t, "required-password")
	require.NoError(t, os.WriteFile(filepath.Join(root, "public.txt"), []byte("public payload"), 0o644))
	share, err := browser.CreateShare("my", "public.txt", time.Hour)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, share.URL, nil)
	rec := httptest.NewRecorder()
	s.authGate(s.mux).ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "public payload", rec.Body.String())
	assert.Contains(t, rec.Header().Get("Content-Disposition"), "attachment")

	require.NoError(t, browser.RevokeShare(share.ID))
	rec = httptest.NewRecorder()
	s.authGate(s.mux).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, share.URL, nil))
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestFileAPIBlocksTraversalAndRequiresDangerousConfirmation(t *testing.T) {
	s, _, root := testFileServer(t, "")
	require.NoError(t, os.WriteFile(filepath.Join(root, "delete-me.txt"), []byte("x"), 0o644))

	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/files?source=my&path=..%2Foutside", nil))
	assert.Equal(t, http.StatusForbidden, rec.Code)
	var response map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	assert.Equal(t, "PATH_FORBIDDEN", response["code"])

	body := []byte(`{"source":"my","path":"delete-me.txt","permanent":true,"confirm":false}`)
	rec = httptest.NewRecorder()
	s.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/files/delete", bytes.NewReader(body)))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.FileExists(t, filepath.Join(root, "delete-me.txt"))

	body = []byte(`{"source":"my","path":"delete-me.txt","permanent":true,"confirm":true}`)
	rec = httptest.NewRecorder()
	s.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/files/delete", bytes.NewReader(body)))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.NoFileExists(t, filepath.Join(root, "delete-me.txt"))
}
