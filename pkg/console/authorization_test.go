package console

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/a2d2-dev/devbox/pkg/auth"
	"github.com/a2d2-dev/devbox/pkg/users"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestRegularUserCannotCallPrivilegedWriteEndpoints(t *testing.T) {
	store, err := users.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()
	_, err = store.CreateUser(ctx, users.CreateUser{Username: "admin", Password: "Admin-pass-2026", Role: users.RoleAdmin, Enabled: true})
	require.NoError(t, err)
	_, err = store.CreateUser(ctx, users.CreateUser{Username: "developer", Password: "Developer-2026", Role: users.RoleUser, Enabled: true})
	require.NoError(t, err)
	a := auth.New(auth.Config{Users: store, SessionTTL: 60})
	token, _, ok := a.VerifyCredentials("developer", "Developer-2026")
	require.True(t, ok)

	s := &Server{mux: http.NewServeMux(), auth: a, logger: zap.NewNop()}
	s.mux.HandleFunc("/api/v1/terminal/exec", s.requireAdmin(s.handleTerminalExec))
	s.mux.HandleFunc("/api/v1/store/install", s.requireAdmin(s.handleStoreInstall))
	s.registerAppRoutes()
	s.registerSupervisorRoutes()
	s.registerSystemRoutes()
	s.registerDownloadRoutes()
	s.registerBackupRoutes()
	s.registerDockerRoutes()
	s.registerMaintenanceRoutes()
	s.registerFileRoutes()
	s.registerOnboardingRoutes()
	s.registerNetworkSecurityRoutes()
	h := s.authGate(s.mux)

	tests := []struct {
		name, method, path, body string
	}{
		{"terminal", http.MethodGet, "/api/v1/terminal/exec", ""},
		{"supervisor control", http.MethodPost, "/api/v1/supervisor/services/devbox/control", `{"action":"restart"}`},
		{"application create", http.MethodPost, "/api/v1/apps", `{}`},
		{"application install", http.MethodPost, "/api/v1/store/install", `{}`},
		{"VM control", http.MethodPost, "/api/v1/vms/test/control", `{"action":"start"}`},
		{"process terminate", http.MethodPost, "/api/v1/processes/123/terminate", `{"startTicks":1}`},
		{"system audit write", http.MethodPost, "/api/v1/audit/events", `{}`},
		{"download create", http.MethodPost, "/api/v1/downloads", `{}`},
		{"download delete file", http.MethodDelete, "/api/v1/downloads/task-1?deleteFile=true", ""},
		{"file delete", http.MethodPost, "/api/v1/files/delete", `{"source":"my","path":"file.txt","permanent":true,"confirm":true}`},
		{"file trash", http.MethodGet, "/api/v1/files/trash", ""},
		{"file share create", http.MethodPost, "/api/v1/files/shares", `{}`},
		{"backup create", http.MethodPost, "/api/v1/backups", `{}`},
		{"backup run", http.MethodPost, "/api/v1/backups/task-1/run", ""},
		{"backup restore", http.MethodPost, "/api/v1/backups/task-1/restore", `{}`},
		{"Docker service control", http.MethodPost, "/api/v1/docker/service", `{}`},
		{"Docker autostart", http.MethodPut, "/api/v1/docker/autostart", `{}`},
		{"Docker migration plan", http.MethodPost, "/api/v1/docker/storage/plan", `{}`},
		{"Docker migration execute", http.MethodPost, "/api/v1/docker/storage/execute", `{}`},
		{"WebDAV settings write", http.MethodPut, "/api/v1/maintenance/settings", `{}`},
		{"SMB preview", http.MethodPost, "/api/v1/maintenance/smb/preview", `[]`},
		{"SMB apply", http.MethodPost, "/api/v1/maintenance/smb/apply", `[]`},
		{"maintenance config backup", http.MethodGet, "/api/v1/maintenance/backup?includeSecrets=true", ""},
		{"maintenance restore preview", http.MethodPost, "/api/v1/maintenance/restore/preview", ""},
		{"maintenance restore confirm", http.MethodPost, "/api/v1/maintenance/restore/confirm", `{}`},
		{"maintenance reset", http.MethodPost, "/api/v1/maintenance/reset", `{}`},
		{"onboarding update", http.MethodPatch, "/api/v1/onboarding", `{}`},
		{"network remote access read", http.MethodGet, "/api/v1/network/remote-access", ""},
		{"network DDNS preview", http.MethodPost, "/api/v1/network/ddns/preview", `{}`},
		{"security settings read", http.MethodGet, "/api/v1/security/settings", ""},
		{"security firewall read", http.MethodGet, "/api/v1/security/firewall", ""},
		{"security firewall apply", http.MethodPost, "/api/v1/security/firewall/apply", `{}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(tt.method, tt.path, bytes.NewBufferString(tt.body))
			r.Header.Set("Authorization", "Bearer "+token)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
		})
	}
}
