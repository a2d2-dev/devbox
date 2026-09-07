package auth

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"path/filepath"
	"time"

	"github.com/a2d2-dev/devbox/pkg/users"
	_ "modernc.org/sqlite"
)

// SessionStore persists authenticated console sessions so tokens survive
// process restarts and can be revoked by stable, non-token public ids.
type SessionStore struct{ db *sql.DB }

func OpenSessionStore(path string) (*SessionStore, error) {
	if path == "" {
		return nil, errors.New("session database path is required")
	}
	if path != ":memory:" {
		path = filepath.Clean(path)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &SessionStore{db: db}
	if err := s.migrate(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *SessionStore) Close() error { return s.db.Close() }

func (s *SessionStore) migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS auth_sessions (
 id TEXT PRIMARY KEY,
 token_hash TEXT NOT NULL UNIQUE,
 user_id TEXT NOT NULL DEFAULT '',
 username TEXT NOT NULL,
 display_name TEXT NOT NULL DEFAULT '',
 role TEXT NOT NULL DEFAULT '',
 legacy INTEGER NOT NULL DEFAULT 0,
 source_ip TEXT NOT NULL DEFAULT '',
 user_agent TEXT NOT NULL DEFAULT '',
 login_at TEXT NOT NULL,
 last_active_at TEXT NOT NULL,
 expires_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_auth_sessions_user_active ON auth_sessions(user_id, expires_at);
CREATE INDEX IF NOT EXISTS idx_auth_sessions_username_active ON auth_sessions(username COLLATE NOCASE, expires_at);`)
	return err
}

func (s *SessionStore) Put(ctx context.Context, sess Session) error {
	tokenHash := HashToken(sess.Token)
	_, err := s.db.ExecContext(ctx, `INSERT OR REPLACE INTO auth_sessions(
 id,token_hash,user_id,username,display_name,role,legacy,source_ip,user_agent,login_at,last_active_at,expires_at
) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		sess.ID, tokenHash, sess.Principal.UserID, sess.Principal.Username, sess.Principal.DisplayName,
		string(sess.Principal.Role), boolInt(sess.Principal.Legacy), sess.SourceIP, sess.UserAgent,
		formatTime(sess.LoginAt), formatTime(sess.LastActiveAt), formatTime(sess.ExpiresAt))
	return err
}

func (s *SessionStore) ByToken(ctx context.Context, token string) (Session, bool, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id,token_hash,user_id,username,display_name,role,legacy,source_ip,user_agent,login_at,last_active_at,expires_at
FROM auth_sessions WHERE token_hash=?`, HashToken(token))
	sess, found, err := scanSession(row)
	if found {
		sess.Token = token
	}
	return sess, found, err
}

func (s *SessionStore) ListByUserID(ctx context.Context, userID string, now time.Time) ([]Session, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,token_hash,user_id,username,display_name,role,legacy,source_ip,user_agent,login_at,last_active_at,expires_at
FROM auth_sessions WHERE user_id=? AND expires_at>? ORDER BY last_active_at DESC, login_at DESC`, userID, formatTime(now.UTC()))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSessions(rows)
}

func (s *SessionStore) DeleteToken(ctx context.Context, token string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM auth_sessions WHERE token_hash=?`, HashToken(token))
	n, err := rowsAffected(res, err)
	return n > 0, err
}

func (s *SessionStore) DeleteUserExcept(ctx context.Context, userID, keepToken string) ([]string, error) {
	hashes, err := s.tokenHashes(ctx, `SELECT token_hash FROM auth_sessions WHERE user_id=? AND token_hash<>?`, userID, HashToken(keepToken))
	if err != nil || len(hashes) == 0 {
		return hashes, err
	}
	_, err = s.db.ExecContext(ctx, `DELETE FROM auth_sessions WHERE user_id=? AND token_hash<>?`, userID, HashToken(keepToken))
	return hashes, err
}

func (s *SessionStore) DeleteUsernameExcept(ctx context.Context, username, keepToken string) ([]string, error) {
	hashes, err := s.tokenHashes(ctx, `SELECT token_hash FROM auth_sessions WHERE legacy=1 AND username=? COLLATE NOCASE AND token_hash<>?`, username, HashToken(keepToken))
	if err != nil || len(hashes) == 0 {
		return hashes, err
	}
	_, err = s.db.ExecContext(ctx, `DELETE FROM auth_sessions WHERE legacy=1 AND username=? COLLATE NOCASE AND token_hash<>?`, username, HashToken(keepToken))
	return hashes, err
}

func (s *SessionStore) DeleteUserSession(ctx context.Context, userID, sessionID string) (string, bool, error) {
	row := s.db.QueryRowContext(ctx, `SELECT token_hash FROM auth_sessions WHERE user_id=? AND id=?`, userID, sessionID)
	var tokenHash string
	if err := row.Scan(&tokenHash); errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	} else if err != nil {
		return "", false, err
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM auth_sessions WHERE user_id=? AND id=?`, userID, sessionID)
	n, err := rowsAffected(res, err)
	return tokenHash, n > 0, err
}

func (s *SessionStore) Touch(ctx context.Context, token string, when time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE auth_sessions SET last_active_at=? WHERE token_hash=?`, formatTime(when.UTC()), HashToken(token))
	return err
}

func (s *SessionStore) PruneExpired(ctx context.Context, now time.Time) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT token_hash FROM auth_sessions WHERE expires_at<=?`, formatTime(now.UTC()))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tokens []string
	for rows.Next() {
		var token string
		if err := rows.Scan(&token); err != nil {
			return nil, err
		}
		tokens = append(tokens, token)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(tokens) == 0 {
		return nil, nil
	}
	_, err = s.db.ExecContext(ctx, `DELETE FROM auth_sessions WHERE expires_at<=?`, formatTime(now.UTC()))
	return tokens, err
}

func (s *SessionStore) tokenHashes(ctx context.Context, query string, args ...any) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var hashes []string
	for rows.Next() {
		var tokenHash string
		if err := rows.Scan(&tokenHash); err != nil {
			return nil, err
		}
		hashes = append(hashes, tokenHash)
	}
	return hashes, rows.Err()
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanSession(row rowScanner) (Session, bool, error) {
	var sess Session
	var role string
	var legacy int
	var loginAt, lastActiveAt, expiresAt string
	err := row.Scan(&sess.ID, &sess.TokenHash, &sess.Principal.UserID, &sess.Principal.Username,
		&sess.Principal.DisplayName, &role, &legacy, &sess.SourceIP, &sess.UserAgent,
		&loginAt, &lastActiveAt, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, false, nil
	}
	if err != nil {
		return Session{}, false, err
	}
	sess.Principal.Role = users.Role(role)
	sess.Principal.Legacy = legacy != 0
	sess.LoginAt, _ = time.Parse(time.RFC3339Nano, loginAt)
	sess.LastActiveAt, _ = time.Parse(time.RFC3339Nano, lastActiveAt)
	sess.ExpiresAt, _ = time.Parse(time.RFC3339Nano, expiresAt)
	return sess, true, nil
}

func scanSessions(rows *sql.Rows) ([]Session, error) {
	out := []Session{}
	for rows.Next() {
		sess, _, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sess)
	}
	return out, rows.Err()
}

func rowsAffected(res sql.Result, err error) (int, error) {
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
