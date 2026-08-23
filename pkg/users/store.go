// Package users provides the persistent console account and file-root authorization model.
package users

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

type Role string

const (
	RoleAdmin Role = "admin"
	RoleUser  Role = "user"
)

var (
	ErrConflict        = errors.New("name already exists")
	ErrNotFound        = errors.New("not found")
	ErrLastAdmin       = errors.New("cannot remove or disable the last administrator")
	ErrInvalidUsername = errors.New("username must be 3-32 characters using letters, numbers, dot, underscore or hyphen")
	ErrWeakPassword    = errors.New("password must be at least 10 characters and contain at least three of uppercase, lowercase, number and symbol")
	ErrInvalidRole     = errors.New("role must be admin or user")
	usernamePattern    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{2,31}$`)
)

type User struct {
	ID          string    `json:"id"`
	Username    string    `json:"username"`
	DisplayName string    `json:"displayName"`
	Role        Role      `json:"role"`
	Enabled     bool      `json:"enabled"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type Group struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	MemberIDs   []string `json:"memberIds"`
}

type FileRoot struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Path string `json:"path"`
}

type CreateUser struct {
	Username, DisplayName, Password string
	Role                            Role
	Enabled                         bool
}

type UpdateUser struct {
	DisplayName *string
	Password    *string
	Role        *Role
	Enabled     *bool
}

type Store struct{ db *sql.DB }

func Open(path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("users database path is required")
	}
	if path != ":memory:" {
		path = filepath.Clean(path)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
PRAGMA foreign_keys = ON;
CREATE TABLE IF NOT EXISTS users (
 id TEXT PRIMARY KEY, username TEXT NOT NULL COLLATE NOCASE UNIQUE,
 display_name TEXT NOT NULL, password_hash TEXT NOT NULL,
 role TEXT NOT NULL CHECK(role IN ('admin','user')), enabled INTEGER NOT NULL DEFAULT 1,
 created_at TEXT NOT NULL, updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS user_groups (
 id TEXT PRIMARY KEY, name TEXT NOT NULL COLLATE NOCASE UNIQUE, description TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS group_members (
 group_id TEXT NOT NULL REFERENCES user_groups(id) ON DELETE CASCADE,
 user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 PRIMARY KEY(group_id,user_id)
);
CREATE TABLE IF NOT EXISTS file_roots (
 id TEXT PRIMARY KEY, name TEXT NOT NULL COLLATE NOCASE UNIQUE, path TEXT NOT NULL UNIQUE
);
CREATE TABLE IF NOT EXISTS user_file_roots (
 user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 root_id TEXT NOT NULL REFERENCES file_roots(id) ON DELETE CASCADE,
 PRIMARY KEY(user_id,root_id)
);
CREATE TABLE IF NOT EXISTS group_file_roots (
 group_id TEXT NOT NULL REFERENCES user_groups(id) ON DELETE CASCADE,
 root_id TEXT NOT NULL REFERENCES file_roots(id) ON DELETE CASCADE,
 PRIMARY KEY(group_id,root_id)
);`)
	return err
}

func ValidateUsername(v string) error {
	if !usernamePattern.MatchString(v) {
		return ErrInvalidUsername
	}
	return nil
}

func ValidatePassword(v string) error {
	if len(v) < 10 || len(v) > 128 {
		return ErrWeakPassword
	}
	classes := 0
	var upper, lower, digit, symbol bool
	for _, r := range v {
		switch {
		case r >= 'A' && r <= 'Z':
			upper = true
		case r >= 'a' && r <= 'z':
			lower = true
		case r >= '0' && r <= '9':
			digit = true
		default:
			symbol = true
		}
	}
	for _, ok := range []bool{upper, lower, digit, symbol} {
		if ok {
			classes++
		}
	}
	if classes < 3 {
		return ErrWeakPassword
	}
	return nil
}

func validateRole(role Role) error {
	if role != RoleAdmin && role != RoleUser {
		return ErrInvalidRole
	}
	return nil
}

func (s *Store) Count(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

func (s *Store) CreateUser(ctx context.Context, in CreateUser) (User, error) {
	in.Username = strings.TrimSpace(in.Username)
	in.DisplayName = strings.TrimSpace(in.DisplayName)
	if err := ValidateUsername(in.Username); err != nil {
		return User{}, err
	}
	if err := ValidatePassword(in.Password); err != nil {
		return User{}, err
	}
	if err := validateRole(in.Role); err != nil {
		return User{}, err
	}
	if in.DisplayName == "" {
		in.DisplayName = in.Username
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	if err != nil {
		return User{}, err
	}
	now := time.Now().UTC()
	u := User{ID: uuid.NewString(), Username: in.Username, DisplayName: in.DisplayName, Role: in.Role, Enabled: in.Enabled, CreatedAt: now, UpdatedAt: now}
	_, err = s.db.ExecContext(ctx, `INSERT INTO users(id,username,display_name,password_hash,role,enabled,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)`,
		u.ID, u.Username, u.DisplayName, string(hash), u.Role, boolInt(u.Enabled), now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	if isUnique(err) {
		return User{}, ErrConflict
	}
	return u, err
}

func (s *Store) Authenticate(ctx context.Context, username, password string) (User, bool) {
	var u User
	var hash, created, updated string
	var enabled int
	err := s.db.QueryRowContext(ctx, `SELECT id,username,display_name,password_hash,role,enabled,created_at,updated_at FROM users WHERE username=?`, strings.TrimSpace(username)).
		Scan(&u.ID, &u.Username, &u.DisplayName, &hash, &u.Role, &enabled, &created, &updated)
	if err != nil || enabled == 0 || bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return User{}, false
	}
	u.Enabled = true
	u.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	u.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return u, true
}

func (s *Store) ListUsers(ctx context.Context, search string) ([]User, error) {
	q := `SELECT id,username,display_name,role,enabled,created_at,updated_at FROM users`
	args := []any{}
	if strings.TrimSpace(search) != "" {
		q += ` WHERE username LIKE ? OR display_name LIKE ?`
		v := "%" + strings.TrimSpace(search) + "%"
		args = []any{v, v}
	}
	q += ` ORDER BY username COLLATE NOCASE`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []User{}
	for rows.Next() {
		var u User
		var enabled int
		var c, m string
		if err := rows.Scan(&u.ID, &u.Username, &u.DisplayName, &u.Role, &enabled, &c, &m); err != nil {
			return nil, err
		}
		u.Enabled = enabled != 0
		u.CreatedAt, _ = time.Parse(time.RFC3339Nano, c)
		u.UpdatedAt, _ = time.Parse(time.RFC3339Nano, m)
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) UpdateUser(ctx context.Context, id string, in UpdateUser) (User, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback()
	u, hash, err := getUserTx(ctx, tx, id)
	if err != nil {
		return User{}, err
	}
	if in.DisplayName != nil {
		v := strings.TrimSpace(*in.DisplayName)
		if v == "" {
			return User{}, errors.New("display name is required")
		}
		u.DisplayName = v
	}
	if in.Role != nil {
		if err := validateRole(*in.Role); err != nil {
			return User{}, err
		}
		u.Role = *in.Role
	}
	if in.Enabled != nil {
		u.Enabled = *in.Enabled
	}
	if in.Password != nil {
		if err := ValidatePassword(*in.Password); err != nil {
			return User{}, err
		}
		b, err := bcrypt.GenerateFromPassword([]byte(*in.Password), bcrypt.DefaultCost)
		if err != nil {
			return User{}, err
		}
		hash = string(b)
	}
	if u.Role != RoleAdmin || !u.Enabled {
		var admins int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE role='admin' AND enabled=1 AND id<>?`, id).Scan(&admins); err != nil {
			return User{}, err
		}
		if admins == 0 {
			return User{}, ErrLastAdmin
		}
	}
	u.UpdatedAt = time.Now().UTC()
	_, err = tx.ExecContext(ctx, `UPDATE users SET display_name=?,password_hash=?,role=?,enabled=?,updated_at=? WHERE id=?`, u.DisplayName, hash, u.Role, boolInt(u.Enabled), u.UpdatedAt.Format(time.RFC3339Nano), id)
	if err != nil {
		return User{}, err
	}
	if err = tx.Commit(); err != nil {
		return User{}, err
	}
	return u, nil
}

func (s *Store) DeleteUser(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	u, _, err := getUserTx(ctx, tx, id)
	if err != nil {
		return err
	}
	if u.Role == RoleAdmin && u.Enabled {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE role='admin' AND enabled=1 AND id<>?`, id).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			return ErrLastAdmin
		}
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM users WHERE id=?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}

func getUserTx(ctx context.Context, tx *sql.Tx, id string) (User, string, error) {
	var u User
	var hash, c, m string
	var enabled int
	err := tx.QueryRowContext(ctx, `SELECT id,username,display_name,password_hash,role,enabled,created_at,updated_at FROM users WHERE id=?`, id).Scan(&u.ID, &u.Username, &u.DisplayName, &hash, &u.Role, &enabled, &c, &m)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, "", ErrNotFound
	}
	if err != nil {
		return User{}, "", err
	}
	u.Enabled = enabled != 0
	u.CreatedAt, _ = time.Parse(time.RFC3339Nano, c)
	u.UpdatedAt, _ = time.Parse(time.RFC3339Nano, m)
	return u, hash, nil
}

func (s *Store) CreateGroup(ctx context.Context, name, description string, members []string) (Group, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Group{}, errors.New("group name is required")
	}
	g := Group{ID: uuid.NewString(), Name: name, Description: strings.TrimSpace(description), MemberIDs: members}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Group{}, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO user_groups(id,name,description) VALUES(?,?,?)`, g.ID, g.Name, g.Description); isUnique(err) {
		return Group{}, ErrConflict
	} else if err != nil {
		return Group{}, err
	}
	if err = replaceLinks(ctx, tx, "group_members", "group_id", "user_id", g.ID, members); err != nil {
		return Group{}, err
	}
	return g, tx.Commit()
}

func (s *Store) ListGroups(ctx context.Context, search string) ([]Group, error) {
	q := `SELECT id,name,description FROM user_groups`
	args := []any{}
	if strings.TrimSpace(search) != "" {
		q += ` WHERE name LIKE ?`
		args = append(args, "%"+strings.TrimSpace(search)+"%")
	}
	q += ` ORDER BY name COLLATE NOCASE`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	out := []Group{}
	for rows.Next() {
		var g Group
		if err := rows.Scan(&g.ID, &g.Name, &g.Description); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, g)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	// The store intentionally uses one SQLite connection. Finish the group
	// result set before loading memberships so the nested queries can proceed.
	for i := range out {
		out[i].MemberIDs, err = listIDs(ctx, s.db, `SELECT user_id FROM group_members WHERE group_id=? ORDER BY user_id`, out[i].ID)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *Store) UpdateGroup(ctx context.Context, id, name, description string, members []string) (Group, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Group{}, errors.New("group name is required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Group{}, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE user_groups SET name=?,description=? WHERE id=?`, name, strings.TrimSpace(description), id)
	if isUnique(err) {
		return Group{}, ErrConflict
	} else if err != nil {
		return Group{}, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return Group{}, ErrNotFound
	}
	if err = replaceLinks(ctx, tx, "group_members", "group_id", "user_id", id, members); err != nil {
		return Group{}, err
	}
	return Group{ID: id, Name: name, Description: strings.TrimSpace(description), MemberIDs: members}, tx.Commit()
}
func (s *Store) DeleteGroup(ctx context.Context, id string) error {
	return deleteByID(ctx, s.db, "user_groups", id)
}

func (s *Store) CreateRoot(ctx context.Context, name, path string) (FileRoot, error) {
	name = strings.TrimSpace(name)
	path = filepath.Clean(path)
	if name == "" || !filepath.IsAbs(path) {
		return FileRoot{}, errors.New("root name and absolute path are required")
	}
	r := FileRoot{ID: uuid.NewString(), Name: name, Path: path}
	_, err := s.db.ExecContext(ctx, `INSERT INTO file_roots(id,name,path)VALUES(?,?,?)`, r.ID, r.Name, r.Path)
	if isUnique(err) {
		return FileRoot{}, ErrConflict
	}
	return r, err
}
func (s *Store) ListRoots(ctx context.Context) ([]FileRoot, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,path FROM file_roots ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []FileRoot{}
	for rows.Next() {
		var r FileRoot
		if err := rows.Scan(&r.ID, &r.Name, &r.Path); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
func (s *Store) DeleteRoot(ctx context.Context, id string) error {
	return deleteByID(ctx, s.db, "file_roots", id)
}
func (s *Store) SetUserRoots(ctx context.Context, id string, roots []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = replaceLinks(ctx, tx, "user_file_roots", "user_id", "root_id", id, roots); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) SetGroupRoots(ctx context.Context, id string, roots []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = replaceLinks(ctx, tx, "group_file_roots", "group_id", "root_id", id, roots); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) UserRootIDs(ctx context.Context, id string) ([]string, error) {
	return listIDs(ctx, s.db, `SELECT root_id FROM user_file_roots WHERE user_id=? ORDER BY root_id`, id)
}
func (s *Store) GroupRootIDs(ctx context.Context, id string) ([]string, error) {
	return listIDs(ctx, s.db, `SELECT root_id FROM group_file_roots WHERE group_id=? ORDER BY root_id`, id)
}
func (s *Store) AllowedPaths(ctx context.Context, userID string) ([]string, error) {
	return listIDs(ctx, s.db, `SELECT DISTINCT r.path FROM file_roots r WHERE r.id IN (SELECT root_id FROM user_file_roots WHERE user_id=? UNION SELECT gfr.root_id FROM group_file_roots gfr JOIN group_members gm ON gm.group_id=gfr.group_id WHERE gm.user_id=?) ORDER BY r.path`, userID, userID)
}

func replaceLinks(ctx context.Context, tx *sql.Tx, table, left, right, id string, values []string) error {
	q := fmt.Sprintf("DELETE FROM %s WHERE %s=?", table, left)
	if _, err := tx.ExecContext(ctx, q, id); err != nil {
		return err
	}
	q = fmt.Sprintf("INSERT INTO %s(%s,%s) VALUES(?,?)", table, left, right)
	for _, v := range values {
		if _, err := tx.ExecContext(ctx, q, id, v); err != nil {
			return err
		}
	}
	return nil
}

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func listIDs(ctx context.Context, q queryer, stmt string, args ...any) ([]string, error) {
	rows, err := q.QueryContext(ctx, stmt, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
func deleteByID(ctx context.Context, db *sql.DB, table, id string) error {
	res, err := db.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s WHERE id=?", table), id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
func isUnique(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "unique constraint")
}
