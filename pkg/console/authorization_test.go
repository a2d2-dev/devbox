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
	h := s.authGate(s.mux)

	tests := []struct {
		name, method, path, body string
	}{
		{"terminal", http.MethodGet, "/api/v1/terminal/exec", ""},
		{"supervisor control", http.MethodPost, "/api/v1/supervisor/services/devbox/control", `{"action":"restart"}`},
		{"application create", http.MethodPost, "/api/v1/apps", `{}`},
		{"application install", http.MethodPost, "/api/v1/store/install", `{}`},
		{"VM control", http.MethodPost, "/api/v1/vms/test/control", `{"action":"start"}`},
		{"system audit write", http.MethodPost, "/api/v1/audit/events", `{}`},
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
