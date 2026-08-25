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

// loginToken is shared with handlers_account_test.go in this package.

func prefsServer(t *testing.T) (http.Handler, *users.Store) {
	t.Helper()
	ctx := context.Background()
	store, err := users.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })
	_, err = store.CreateUser(ctx, users.CreateUser{Username: "admin", Password: "Admin-pass-2026", Role: users.RoleAdmin, Enabled: true})
	require.NoError(t, err)
	_, err = store.CreateUser(ctx, users.CreateUser{Username: "developer", Password: "Developer-2026", Role: users.RoleUser, Enabled: true})
	require.NoError(t, err)

	a := auth.New(auth.Config{Users: store, SessionTTL: 60})
	s := &Server{mux: http.NewServeMux(), auth: a, users: store}
	s.registerAuthRoutes()
	s.registerAccountPrefsRoutes()
	return s.authGate(s.mux), store
}

// prefsServerWithAuth is like prefsServer but also returns the *auth.Auth so a
// test can mint a legacy single-password admin session (a real session whose
// principal has an empty UserID and no backing users row).
func prefsServerWithAuth(t *testing.T) (http.Handler, *users.Store, *auth.Auth) {
	t.Helper()
	ctx := context.Background()
	store, err := users.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })
	_, err = store.CreateUser(ctx, users.CreateUser{Username: "admin", Password: "Admin-pass-2026", Role: users.RoleAdmin, Enabled: true})
	require.NoError(t, err)
	_, err = store.CreateUser(ctx, users.CreateUser{Username: "developer", Password: "Developer-2026", Role: users.RoleUser, Enabled: true})
	require.NoError(t, err)

	a := auth.New(auth.Config{Users: store, SessionTTL: 60})
	s := &Server{mux: http.NewServeMux(), auth: a, users: store}
	s.registerAuthRoutes()
	s.registerAccountPrefsRoutes()
	return s.authGate(s.mux), store, a
}

// GET without a session must be rejected with 401.
func TestAccountPrefsRequiresSession(t *testing.T) {
	h, _ := prefsServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/account/preferences", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

// A legacy single-password admin has a valid session but no users row, so its
// principal carries an empty UserID. Both GET and PUT preferences must answer
// 403 with reason not_a_managed_account — never 401. Returning 401 here would
// trip the frontend's authFetch contract (401 + token => session expired =>
// global logout) and evict a logged-in legacy admin in a loop.
func TestAccountPrefsLegacyAdminForbidden(t *testing.T) {
	h, _, a := prefsServerWithAuth(t)
	// Mint a legacy admin session directly: a real, valid session whose
	// principal has no UserID, exactly as AuthenticateCredentials produces for
	// the configured single-password administrator.
	token := a.IssueSession(auth.Principal{Username: "admin", DisplayName: "admin", Role: users.RoleAdmin, Legacy: true})

	assertNotManaged := func(t *testing.T, w *httptest.ResponseRecorder) {
		t.Helper()
		require.Equal(t, http.StatusForbidden, w.Code, "body: %s", w.Body.String())
		var body map[string]string
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		require.Equal(t, "not_a_managed_account", body["reason"])
		require.NotEmpty(t, body["error"])
	}

	get := httptest.NewRequest(http.MethodGet, "/api/v1/account/preferences", nil)
	get.Header.Set("Authorization", "Bearer "+token)
	gw := httptest.NewRecorder()
	h.ServeHTTP(gw, get)
	assertNotManaged(t, gw)

	putBody, _ := json.Marshal(map[string]any{"theme": "dark"})
	put := httptest.NewRequest(http.MethodPut, "/api/v1/account/preferences", bytes.NewReader(putBody))
	put.Header.Set("Authorization", "Bearer "+token)
	put.Header.Set("Content-Type", "application/json")
	pw := httptest.NewRecorder()
	h.ServeHTTP(pw, put)
	assertNotManaged(t, pw)
}

// Empty record returns the default {} for an authenticated user.
func TestAccountPrefsGetDefaultsEmpty(t *testing.T) {
	h, _ := prefsServer(t)
	token := loginToken(t, h, "developer", "Developer-2026")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/account/preferences", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.JSONEq(t, `{}`, w.Body.String())
}

// PUT whitelists the body, and a subsequent GET returns the sanitized record.
func TestAccountPrefsPutGetRoundTripAndFiltering(t *testing.T) {
	h, _ := prefsServer(t)
	token := loginToken(t, h, "developer", "Developer-2026")

	body, _ := json.Marshal(map[string]any{
		"theme":      "dark",
		"wallpaper":  "grid",
		"showRecent": true,
		"evil":       "drop-me",
		"user_id":    "someone-else",
		"wallpaper2": "bad",
	})
	put := httptest.NewRequest(http.MethodPut, "/api/v1/account/preferences", bytes.NewReader(body))
	put.Header.Set("Authorization", "Bearer "+token)
	put.Header.Set("Content-Type", "application/json")
	pw := httptest.NewRecorder()
	h.ServeHTTP(pw, put)
	require.Equal(t, http.StatusOK, pw.Code)

	var saved map[string]any
	require.NoError(t, json.Unmarshal(pw.Body.Bytes(), &saved))
	require.Equal(t, map[string]any{"theme": "dark", "wallpaper": "grid", "showRecent": true}, saved)

	get := httptest.NewRequest(http.MethodGet, "/api/v1/account/preferences", nil)
	get.Header.Set("Authorization", "Bearer "+token)
	gw := httptest.NewRecorder()
	h.ServeHTTP(gw, get)
	require.Equal(t, http.StatusOK, gw.Code)
	var got map[string]any
	require.NoError(t, json.Unmarshal(gw.Body.Bytes(), &got))
	require.Equal(t, map[string]any{"theme": "dark", "wallpaper": "grid", "showRecent": true}, got)
	_, hasEvil := got["evil"]
	require.False(t, hasEvil)
	_, hasUserID := got["user_id"]
	require.False(t, hasUserID)
}

// Each user's preferences are isolated: one user's PUT never leaks to another.
func TestAccountPrefsPerUserIsolation(t *testing.T) {
	h, _ := prefsServer(t)
	devToken := loginToken(t, h, "developer", "Developer-2026")
	adminToken := loginToken(t, h, "admin", "Admin-pass-2026")

	body, _ := json.Marshal(map[string]any{"theme": "dark"})
	put := httptest.NewRequest(http.MethodPut, "/api/v1/account/preferences", bytes.NewReader(body))
	put.Header.Set("Authorization", "Bearer "+devToken)
	put.Header.Set("Content-Type", "application/json")
	pw := httptest.NewRecorder()
	h.ServeHTTP(pw, put)
	require.Equal(t, http.StatusOK, pw.Code)

	get := httptest.NewRequest(http.MethodGet, "/api/v1/account/preferences", nil)
	get.Header.Set("Authorization", "Bearer "+adminToken)
	gw := httptest.NewRecorder()
	h.ServeHTTP(gw, get)
	require.Equal(t, http.StatusOK, gw.Code)
	require.JSONEq(t, `{}`, gw.Body.String())
}
