package console

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/a2d2-dev/devbox/pkg/auth"
	devfiles "github.com/a2d2-dev/devbox/pkg/files"
	"github.com/a2d2-dev/devbox/pkg/users"
	"github.com/stretchr/testify/require"
)

func TestUserAPIAndFileAuthorization(t *testing.T) {
	ctx := context.Background()
	store, err := users.Open(":memory:")
	require.NoError(t, err)
	defer store.Close()
	admin, err := store.CreateUser(ctx, users.CreateUser{Username: "admin", Password: "Admin-pass-2026", Role: users.RoleAdmin, Enabled: true})
	require.NoError(t, err)
	_ = admin
	member, err := store.CreateUser(ctx, users.CreateUser{Username: "developer", Password: "Developer-2026", Role: users.RoleUser, Enabled: true})
	require.NoError(t, err)

	work := t.TempDir()
	allowed := filepath.Join(work, "allowed")
	denied := filepath.Join(work, "denied")
	require.NoError(t, os.MkdirAll(allowed, 0o755))
	require.NoError(t, os.MkdirAll(denied, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(allowed, "visible.txt"), []byte("ok"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(denied, "hidden.txt"), []byte("no"), 0o644))
	root, err := store.CreateRoot(ctx, "Allowed", allowed)
	require.NoError(t, err)
	require.NoError(t, store.SetUserRoots(ctx, member.ID, []string{root.ID}))

	a := auth.New(auth.Config{Users: store, SessionTTL: 60})
	s := &Server{config: Config{WorkDir: work}, mux: http.NewServeMux(), auth: a, users: store, fileBrowser: devfiles.NewBrowser(devfiles.Config{RootDir: work})}
	s.registerAuthRoutes()
	s.registerUserRoutes()
	s.mux.HandleFunc("/api/v1/files", s.handleFiles)
	s.mux.HandleFunc("/api/v1/files/content", s.handleFileContent)
	h := s.authGate(s.mux)

	loginBody, _ := json.Marshal(map[string]string{"username": "developer", "password": "Developer-2026"})
	login := httptest.NewRequest(http.MethodPost, "/api/v1/auth/verify", bytes.NewReader(loginBody))
	login.Header.Set("Content-Type", "application/json")
	lw := httptest.NewRecorder()
	h.ServeHTTP(lw, login)
	require.Equal(t, http.StatusOK, lw.Code)
	var loginResult struct {
		Token string `json:"token"`
	}
	require.NoError(t, json.Unmarshal(lw.Body.Bytes(), &loginResult))
	require.NotEmpty(t, loginResult.Token)

	management := httptest.NewRequest(http.MethodGet, "/api/v1/users", nil)
	management.Header.Set("Authorization", "Bearer "+loginResult.Token)
	mw := httptest.NewRecorder()
	h.ServeHTTP(mw, management)
	require.Equal(t, http.StatusForbidden, mw.Code)

	list := httptest.NewRequest(http.MethodGet, "/api/v1/files", nil)
	list.Header.Set("Authorization", "Bearer "+loginResult.Token)
	fw := httptest.NewRecorder()
	h.ServeHTTP(fw, list)
	require.Equal(t, http.StatusOK, fw.Code)
	var entries []devfiles.FileEntry
	require.NoError(t, json.Unmarshal(fw.Body.Bytes(), &entries))
	require.Len(t, entries, 1)
	require.Equal(t, "allowed", entries[0].Name)

	content := httptest.NewRequest(http.MethodGet, "/api/v1/files/content?path=denied&name=hidden.txt", nil)
	content.Header.Set("Authorization", "Bearer "+loginResult.Token)
	cw := httptest.NewRecorder()
	h.ServeHTTP(cw, content)
	require.Equal(t, http.StatusForbidden, cw.Code)
}
