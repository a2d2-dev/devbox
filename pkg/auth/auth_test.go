package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/a2d2-dev/devbox/pkg/users"
	"github.com/stretchr/testify/require"
)

func TestMultiUserAuthenticationAndRoleBoundary(t *testing.T) {
	store, err := users.Open(":memory:")
	require.NoError(t, err)
	defer store.Close()
	u, err := store.CreateUser(context.Background(), users.CreateUser{Username: "developer", Password: "Developer-2026", Role: users.RoleUser, Enabled: true})
	require.NoError(t, err)
	a := New(Config{Password: "legacy-secret", SessionTTL: 60, Users: store})
	token, p, ok := a.VerifyCredentials("developer", "Developer-2026")
	require.True(t, ok)
	require.Equal(t, u.ID, p.UserID)
	require.False(t, p.IsAdmin())
	require.True(t, a.ValidateToken("Bearer "+token))

	called := false
	h := a.Middleware(a.RequireAdmin(func(http.ResponseWriter, *http.Request) { called = true }))
	r := httptest.NewRequest(http.MethodGet, "/api/v1/users", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	h(w, r)
	require.Equal(t, http.StatusForbidden, w.Code)
	require.False(t, called)

	legacyToken, legacy, ok := a.VerifyCredentials("admin", "legacy-secret")
	require.True(t, ok)
	require.True(t, legacy.IsAdmin())
	require.True(t, a.ValidateToken(legacyToken))
	_, _, ok = a.VerifyCredentials("developer", "legacy-secret")
	require.False(t, ok)
}

func TestDisabledAuthRemainsBackwardCompatible(t *testing.T) {
	a := New(Config{})
	token, p, ok := a.VerifyCredentials("anything", "anything")
	require.True(t, ok)
	require.Empty(t, token)
	require.True(t, p.IsAdmin())
	require.True(t, a.ValidateToken(""))
}
