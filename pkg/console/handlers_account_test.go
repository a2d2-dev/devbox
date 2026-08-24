package console

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/a2d2-dev/devbox/pkg/auth"
	"github.com/a2d2-dev/devbox/pkg/users"
	"github.com/stretchr/testify/require"
)

// accountTestServer builds a console server backed by an in-memory user store
// and returns the gated handler plus the store so tests can seed accounts.
func accountTestServer(t *testing.T) (http.Handler, *users.Store, *auth.Auth) {
	t.Helper()
	store, err := users.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	a := auth.New(auth.Config{Users: store, SessionTTL: 60})
	s := &Server{mux: http.NewServeMux(), auth: a, users: store}
	s.registerAuthRoutes()
	s.registerUserRoutes()
	s.registerAccountRoutes()
	return s.authGate(s.mux), store, a
}

func loginToken(t *testing.T, h http.Handler, username, password string) string {
	t.Helper()
	body, err := json.Marshal(map[string]string{"username": username, "password": password})
	require.NoError(t, err)
	r := httptest.NewRequest(http.MethodPost, "/api/v1/auth/verify", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	require.Equal(t, http.StatusOK, w.Code, "login body: %s", w.Body.String())
	var res struct {
		Token string `json:"token"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &res))
	require.NotEmpty(t, res.Token)
	return res.Token
}

func doJSON(t *testing.T, h http.Handler, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var payload []byte
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		require.NoError(t, err)
	}
	r := httptest.NewRequest(method, path, bytes.NewReader(payload))
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestAccountRequiresSession(t *testing.T) {
	ctx := context.Background()
	h, store, _ := accountTestServer(t)
	_, err := store.CreateUser(ctx, users.CreateUser{Username: "admin", Password: "Admin-pass-2026", Role: users.RoleAdmin, Enabled: true})
	require.NoError(t, err)

	// No token at all.
	require.Equal(t, http.StatusUnauthorized, doJSON(t, h, http.MethodGet, "/api/v1/account", "", nil).Code)
	require.Equal(t, http.StatusUnauthorized, doJSON(t, h, http.MethodPatch, "/api/v1/account", "", map[string]any{"displayName": "x"}).Code)
	require.Equal(t, http.StatusUnauthorized, doJSON(t, h, http.MethodPost, "/api/v1/account/password", "", map[string]any{"currentPassword": "a", "newPassword": "b"}).Code)

	// Garbage token.
	require.Equal(t, http.StatusUnauthorized, doJSON(t, h, http.MethodGet, "/api/v1/account", "not-a-real-token", nil).Code)
}

func TestAccountGetReturnsOwnProfileWithoutSecrets(t *testing.T) {
	ctx := context.Background()
	h, store, _ := accountTestServer(t)
	_, err := store.CreateUser(ctx, users.CreateUser{Username: "admin", Password: "Admin-pass-2026", Role: users.RoleAdmin, Enabled: true})
	require.NoError(t, err)
	member, err := store.CreateUser(ctx, users.CreateUser{Username: "developer", DisplayName: "Dev Eloper", Password: "Developer-2026", Role: users.RoleUser, Enabled: true})
	require.NoError(t, err)

	token := loginToken(t, h, "developer", "Developer-2026")
	w := doJSON(t, h, http.MethodGet, "/api/v1/account", token, nil)
	require.Equal(t, http.StatusOK, w.Code)

	var view map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &view))
	require.Equal(t, member.ID, view["id"])
	require.Equal(t, "developer", view["username"])
	require.Equal(t, "Dev Eloper", view["displayName"])
	require.Equal(t, "user", view["role"])
	require.Equal(t, "initials", view["avatarKind"])
	require.Contains(t, view, "createdAt")

	// Response must never leak a token or password hash.
	require.NotContains(t, w.Body.String(), "password")
	require.NotContains(t, w.Body.String(), "hash")
	require.NotContains(t, w.Body.String(), token)
}

func TestAccountPatchOnlyChangesOwnDisplayNameIgnoringForeignIDs(t *testing.T) {
	ctx := context.Background()
	h, store, _ := accountTestServer(t)
	admin, err := store.CreateUser(ctx, users.CreateUser{Username: "admin", Password: "Admin-pass-2026", Role: users.RoleAdmin, Enabled: true})
	require.NoError(t, err)
	member, err := store.CreateUser(ctx, users.CreateUser{Username: "developer", Password: "Developer-2026", Role: users.RoleUser, Enabled: true})
	require.NoError(t, err)

	token := loginToken(t, h, "developer", "Developer-2026")

	// Body carries a foreign userId, an admin role, and enabled=false. Unknown
	// fields are rejected outright (strict decoder), proving privilege fields are
	// not silently accepted.
	esc := doJSON(t, h, http.MethodPatch, "/api/v1/account", token, map[string]any{
		"displayName": "Renamed",
		"userId":      admin.ID,
		"role":        "admin",
		"enabled":     false,
	})
	require.Equal(t, http.StatusBadRequest, esc.Code)

	// A clean display-name-only change succeeds and only affects the caller.
	ok := doJSON(t, h, http.MethodPatch, "/api/v1/account", token, map[string]any{"displayName": "Renamed"})
	require.Equal(t, http.StatusOK, ok.Code)

	list, err := store.ListUsers(ctx, "")
	require.NoError(t, err)
	byID := map[string]users.User{}
	for _, u := range list {
		byID[u.ID] = u
	}
	require.Equal(t, "Renamed", byID[member.ID].DisplayName)
	require.Equal(t, users.RoleUser, byID[member.ID].Role, "self patch must not change own role")
	require.True(t, byID[member.ID].Enabled, "self patch must not disable own account")
	require.Equal(t, "admin", byID[admin.ID].DisplayName, "foreign account must be untouched")
	require.Equal(t, users.RoleAdmin, byID[admin.ID].Role)
}

func TestAccountPasswordChangeFlow(t *testing.T) {
	ctx := context.Background()
	h, store, a := accountTestServer(t)
	_, err := store.CreateUser(ctx, users.CreateUser{Username: "admin", Password: "Admin-pass-2026", Role: users.RoleAdmin, Enabled: true})
	require.NoError(t, err)
	member, err := store.CreateUser(ctx, users.CreateUser{Username: "developer", Password: "Developer-2026", Role: users.RoleUser, Enabled: true})
	require.NoError(t, err)

	current := loginToken(t, h, "developer", "Developer-2026")
	// A second, independent session for the same user (another device).
	other := loginToken(t, h, "developer", "Developer-2026")
	require.NotEqual(t, current, other)

	// Wrong current password: rejected, nothing changes.
	bad := doJSON(t, h, http.MethodPost, "/api/v1/account/password", current, map[string]any{
		"currentPassword": "Wrong-pass-2026", "newPassword": "Brand-new-2027",
	})
	require.Equal(t, http.StatusUnauthorized, bad.Code)
	require.NotContains(t, bad.Body.String(), "Brand-new-2027")
	_, stillOld := store.Authenticate(ctx, "developer", "Developer-2026")
	require.True(t, stillOld, "password must be unchanged after a failed attempt")

	// Weak new password: 400, still unchanged.
	weak := doJSON(t, h, http.MethodPost, "/api/v1/account/password", current, map[string]any{
		"currentPassword": "Developer-2026", "newPassword": "weak",
	})
	require.Equal(t, http.StatusBadRequest, weak.Code)
	_, stillOld2 := store.Authenticate(ctx, "developer", "Developer-2026")
	require.True(t, stillOld2)

	// Successful change: 204.
	okResp := doJSON(t, h, http.MethodPost, "/api/v1/account/password", current, map[string]any{
		"currentPassword": "Developer-2026", "newPassword": "Brand-new-2027",
	})
	require.Equal(t, http.StatusNoContent, okResp.Code)
	require.Empty(t, okResp.Body.String())

	// New password now authenticates; old one does not.
	_, newOK := store.Authenticate(ctx, "developer", "Brand-new-2027")
	require.True(t, newOK)
	_, oldGone := store.Authenticate(ctx, "developer", "Developer-2026")
	require.False(t, oldGone)

	// The caller's current token still works; the other session was revoked.
	_, currentAlive := a.SessionPrincipal(current)
	require.True(t, currentAlive, "current session must survive a self password change")
	_, otherAlive := a.SessionPrincipal(other)
	require.False(t, otherAlive, "other sessions must be revoked")

	// Confirm end-to-end that the current token is still accepted by the gate.
	getAfter := doJSON(t, h, http.MethodGet, "/api/v1/account", current, nil)
	require.Equal(t, http.StatusOK, getAfter.Code)
	require.Equal(t, member.ID, mustField(t, getAfter.Body.Bytes(), "id"))
}

func mustField(t *testing.T, body []byte, key string) any {
	t.Helper()
	var m map[string]any
	require.NoError(t, json.Unmarshal(body, &m))
	return m[key]
}
