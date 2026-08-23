package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/a2d2-dev/devbox/pkg/users"
	"github.com/stretchr/testify/require"
)

func TestMultiUserAuthenticationAndRoleBoundary(t *testing.T) {
	store, err := users.Open(":memory:")
	require.NoError(t, err)
	defer store.Close()
	_, err = store.CreateUser(context.Background(), users.CreateUser{Username: "admin", Password: "Admin-pass-2026", Role: users.RoleAdmin, Enabled: true})
	require.NoError(t, err)
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

func TestConfiguredUserStoreFailureFailsClosed(t *testing.T) {
	store, err := users.Open(":memory:")
	require.NoError(t, err)
	require.NoError(t, store.Close())

	a := New(Config{Users: store, UsersConfigured: true})
	require.True(t, a.Enabled())

	called := false
	h := a.Middleware(func(http.ResponseWriter, *http.Request) { called = true })
	w := httptest.NewRecorder()
	h(w, httptest.NewRequest(http.MethodGet, "/api/v1/metrics", nil))
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
	require.Contains(t, w.Body.String(), "user_database_unavailable")
	require.False(t, called)
}

func TestExpiredSessionNotifiesCleanup(t *testing.T) {
	a := New(Config{Password: "pw", SessionTTL: -1})
	removed := ""
	a.SetSessionRemovedHook(func(token string) { removed = token })
	token, ok := a.Verify("pw")
	require.True(t, ok)
	require.False(t, a.ValidateToken(token))
	require.Equal(t, token, removed)
}

func TestCredentialCheckDoesNotCreateSessionBeforeAdditionalFactors(t *testing.T) {
	a := New(Config{Password: "pw", SessionTTL: 60})
	p, ok := a.AuthenticateCredentials("admin", "pw")
	require.True(t, ok)
	require.True(t, p.IsAdmin())
	a.mu.RLock()
	require.Empty(t, a.sessions)
	a.mu.RUnlock()

	token := a.IssueSession(p)
	require.NotEmpty(t, token)
	require.True(t, a.ValidateToken(token))
}

func TestValidateTokenPrunesOtherExpiredSessions(t *testing.T) {
	a := New(Config{Password: "pw", SessionTTL: 60})
	valid := a.NewSession()
	expired := "expired-token"
	a.mu.Lock()
	a.sessions[expired] = session{expires: time.Now().Add(-time.Minute), principal: Principal{Username: "admin", Role: users.RoleAdmin}}
	a.mu.Unlock()

	removed := ""
	a.SetSessionRemovedHook(func(token string) { removed = token })
	require.True(t, a.ValidateToken(valid))
	require.Equal(t, expired, removed)
	a.mu.RLock()
	_, exists := a.sessions[expired]
	a.mu.RUnlock()
	require.False(t, exists)
}
