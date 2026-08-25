package users

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// prefKey enumerates the whitelisted user-preference keys. Any key absent from
// this set is discarded before storage so a malicious or stale client cannot
// persist arbitrary state under a user's row.
type prefKey string

const (
	prefTheme      prefKey = "theme"
	prefWallpaper  prefKey = "wallpaper"
	prefAccent     prefKey = "accent"
	prefIconStyle  prefKey = "iconStyle"
	prefLayout     prefKey = "layout"
	prefIconSize   prefKey = "iconSize"
	prefTopbar     prefKey = "topbar"
	prefShowRecent prefKey = "showRecent"
)

// allowedPrefKeys is the whitelist. Values not listed here are dropped.
var allowedPrefKeys = map[prefKey]struct{}{
	prefTheme:      {},
	prefWallpaper:  {},
	prefAccent:     {},
	prefIconStyle:  {},
	prefLayout:     {},
	prefIconSize:   {},
	prefTopbar:     {},
	prefShowRecent: {},
}

// enumConstraints holds the closed value sets for keys that accept only a fixed
// vocabulary. Keys absent from this map accept any well-typed value.
var enumConstraints = map[prefKey]map[string]struct{}{
	prefTheme:     {"light": {}, "dark": {}, "system": {}},
	prefWallpaper: {"fnos": {}, "grid": {}, "topo": {}, "plain": {}},
}

// SanitizePrefs filters an arbitrary decoded preference object down to the
// whitelisted, well-typed keys. Unknown keys are dropped silently. A whitelisted
// key carrying an invalid enum value or wrong type is also dropped rather than
// rejected, so a single bad field never discards a whole valid update. It always
// returns a non-nil map (possibly empty).
func SanitizePrefs(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		key := prefKey(k)
		if _, ok := allowedPrefKeys[key]; !ok {
			continue // non-whitelisted key: discard
		}
		if !validPrefValue(key, v) {
			continue // whitelisted but fails type/enum check: discard
		}
		out[k] = v
	}
	return out
}

// validPrefValue applies basic type and enumeration validation for a single
// whitelisted key.
func validPrefValue(key prefKey, v any) bool {
	switch key {
	case prefShowRecent:
		_, ok := v.(bool)
		return ok
	case prefIconSize:
		// Numeric (JSON numbers decode to float64) or a short string label.
		switch v.(type) {
		case float64, string:
			return true
		default:
			return false
		}
	default:
		// Remaining keys are string-valued; enum-constrained ones must match.
		s, ok := v.(string)
		if !ok {
			return false
		}
		if allowed, constrained := enumConstraints[key]; constrained {
			_, ok := allowed[s]
			return ok
		}
		return true
	}
}

// GetPrefs returns the stored preference object for userID as raw JSON. When no
// row exists it returns the default empty object "{}" with a nil error so
// callers can pass it straight through.
func (s *Store) GetPrefs(ctx context.Context, userID string) (json.RawMessage, error) {
	if userID == "" {
		return nil, errors.New("user id is required")
	}
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT prefs_json FROM user_prefs WHERE user_id=?`, userID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return json.RawMessage("{}"), nil
	}
	if err != nil {
		return nil, err
	}
	if raw == "" {
		return json.RawMessage("{}"), nil
	}
	return json.RawMessage(raw), nil
}

// PutPrefs whitelists and validates prefs, then persists the sanitized object as
// the complete preference record for userID (upsert). Non-whitelisted keys are
// dropped before storage, so the row can never hold arbitrary data.
func (s *Store) PutPrefs(ctx context.Context, userID string, prefs map[string]any) (json.RawMessage, error) {
	if userID == "" {
		return nil, errors.New("user id is required")
	}
	clean := SanitizePrefs(prefs)
	encoded, err := json.Marshal(clean)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = s.db.ExecContext(ctx, `
INSERT INTO user_prefs(user_id,prefs_json,updated_at) VALUES(?,?,?)
ON CONFLICT(user_id) DO UPDATE SET prefs_json=excluded.prefs_json, updated_at=excluded.updated_at`,
		userID, string(encoded), now)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(encoded), nil
}
