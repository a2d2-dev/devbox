package users

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func newPrefUser(t *testing.T) (*Store, context.Context, string) {
	t.Helper()
	ctx := context.Background()
	store, err := Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })
	u, err := store.CreateUser(ctx, CreateUser{Username: "prefuser", Password: "Pref-pass-2026", Role: RoleAdmin, Enabled: true})
	require.NoError(t, err)
	return store, ctx, u.ID
}

// Empty record returns the default empty object rather than an error.
func TestGetPrefsDefaultsToEmpty(t *testing.T) {
	store, ctx, uid := newPrefUser(t)
	raw, err := store.GetPrefs(ctx, uid)
	require.NoError(t, err)
	require.JSONEq(t, `{}`, string(raw))
}

// put -> get round trips the whitelisted values intact.
func TestPutGetPrefsRoundTrip(t *testing.T) {
	store, ctx, uid := newPrefUser(t)
	in := map[string]any{
		"theme":      "dark",
		"wallpaper":  "topo",
		"accent":     "#0af",
		"iconStyle":  "rounded",
		"layout":     "grid",
		"iconSize":   float64(48),
		"topbar":     "compact",
		"showRecent": true,
	}
	saved, err := store.PutPrefs(ctx, uid, in)
	require.NoError(t, err)

	var savedMap map[string]any
	require.NoError(t, json.Unmarshal(saved, &savedMap))
	require.Equal(t, in, savedMap)

	raw, err := store.GetPrefs(ctx, uid)
	require.NoError(t, err)
	var got map[string]any
	require.NoError(t, json.Unmarshal(raw, &got))
	require.Equal(t, in, got)
}

// Non-whitelisted keys are dropped, and whitelisted keys with invalid enum or
// type values are also discarded.
func TestPutPrefsWhitelistFiltering(t *testing.T) {
	store, ctx, uid := newPrefUser(t)
	in := map[string]any{
		"theme":      "dark",           // valid enum -> kept
		"wallpaper":  "not-a-choice",   // invalid enum -> dropped
		"showRecent": "yes",            // wrong type (want bool) -> dropped
		"evil":       "rm -rf",         // non-whitelisted -> dropped
		"user_id":    "someone-else",   // must never be honored -> dropped
		"__proto__":  map[string]any{}, // non-whitelisted -> dropped
		"accent":     "#123",           // free string -> kept
	}
	saved, err := store.PutPrefs(ctx, uid, in)
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, json.Unmarshal(saved, &got))

	require.Equal(t, map[string]any{"theme": "dark", "accent": "#123"}, got)
	_, hasEvil := got["evil"]
	require.False(t, hasEvil)
	_, hasUserID := got["user_id"]
	require.False(t, hasUserID)
	_, hasBadWallpaper := got["wallpaper"]
	require.False(t, hasBadWallpaper)
	_, hasBadShowRecent := got["showRecent"]
	require.False(t, hasBadShowRecent)
}

// A second PutPrefs fully replaces the previous record.
func TestPutPrefsReplacesWholeRecord(t *testing.T) {
	store, ctx, uid := newPrefUser(t)
	_, err := store.PutPrefs(ctx, uid, map[string]any{"theme": "dark", "accent": "#000"})
	require.NoError(t, err)
	_, err = store.PutPrefs(ctx, uid, map[string]any{"theme": "light"})
	require.NoError(t, err)

	raw, err := store.GetPrefs(ctx, uid)
	require.NoError(t, err)
	var got map[string]any
	require.NoError(t, json.Unmarshal(raw, &got))
	require.Equal(t, map[string]any{"theme": "light"}, got)
}

// SanitizePrefs enforces the theme enumeration directly.
func TestSanitizePrefsThemeEnum(t *testing.T) {
	require.Equal(t, map[string]any{"theme": "system"}, SanitizePrefs(map[string]any{"theme": "system"}))
	require.Equal(t, map[string]any{}, SanitizePrefs(map[string]any{"theme": "neon"}))
}

// migrate must be idempotent: opening the same on-disk database twice (each
// Open runs migrate) must not error and must preserve stored data.
func TestMigrateIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/users.db"

	ctx := context.Background()
	s1, err := Open(path)
	require.NoError(t, err)
	u, err := s1.CreateUser(ctx, CreateUser{Username: "keep", Password: "Keep-pass-2026", Role: RoleAdmin, Enabled: true})
	require.NoError(t, err)
	_, err = s1.PutPrefs(ctx, u.ID, map[string]any{"theme": "dark"})
	require.NoError(t, err)
	require.NoError(t, s1.Close())

	// Re-open: migrate runs again against an existing schema.
	s2, err := Open(path)
	require.NoError(t, err)
	defer s2.Close()
	// Running migrate a third time explicitly must also be a no-op.
	require.NoError(t, s2.migrate(ctx))

	raw, err := s2.GetPrefs(ctx, u.ID)
	require.NoError(t, err)
	require.JSONEq(t, `{"theme":"dark"}`, string(raw))
}
