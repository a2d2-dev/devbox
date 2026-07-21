package apps

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite" // 纯 Go SQLite 驱动，无 cgo
)

// SQLite 持久化（Issue #2）：元数据/任务/revision/幂等/审计。
//
// 为什么 SQLite 而非纯文件：并发写、任务恢复、幂等请求、审计检索、schema
// migration、半写防护都需要事务。Compose 事实源仍落 compose.yaml 文件。

// AppRecord 应用持久化元数据（不含 secret；compose 内容存文件）。
type AppRecord struct {
	ID               string
	Name             string
	Runtime          RuntimeKind
	Source           ApplicationSource
	Revision         int64             // desired generation
	ObservedRevision int64             // 已成功部署到的 revision（desired>observed 表示未同步）
	Parameters       map[string]string // 非敏感参数快照
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// AuditRecord 审计日志（detail 已脱敏，不含 secret）。
type AuditRecord struct {
	ID     int64
	At     time.Time
	Actor  string
	AppID  string
	Action string // apply/operate:start/remove/restore/...
	TaskID string
	Detail string // 脱敏 JSON
}

// idemRecord 幂等键记录。
type idemRecord struct {
	Key         string
	RequestHash string
	TaskID      string
	CreatedAt   time.Time
}

// Repository 持久化仓库接口（controller 依赖它，便于测试）。
type Repository interface {
	Close() error

	// apps
	UpsertAppMeta(ctx context.Context, a AppRecord) error
	GetAppMeta(ctx context.Context, id string) (AppRecord, bool, error)
	ListAppMetas(ctx context.Context) ([]AppRecord, error)
	DeleteAppMeta(ctx context.Context, id string) error
	SetObservedRevision(ctx context.Context, appID string, rev int64) error
	// PurgeApp 删除应用元数据与 revision 记录（remove 成功后调用；tasks/audit/idempotency 保留历史）。
	PurgeApp(ctx context.Context, appID string) error

	// revisions
	NextRevisionNumber(ctx context.Context, appID string) (int64, error)
	InsertRevision(ctx context.Context, rev Revision) error
	ListRevisions(ctx context.Context, appID string) ([]Revision, error)
	GetRevision(ctx context.Context, appID string, num int64) (Revision, bool, error)

	// tasks
	CreateTask(ctx context.Context, t Task) error
	GetTask(ctx context.Context, id string) (Task, error)
	UpdateTask(ctx context.Context, id string, mut func(*Task)) error
	ListTasksByApp(ctx context.Context, appID string, limit int) ([]Task, error)
	ListNonTerminalTasks(ctx context.Context) ([]Task, error)

	// idempotency
	SaveIdempotency(ctx context.Context, key, requestHash, taskID string) error
	GetIdempotency(ctx context.Context, key string) (idemRecord, bool, error)

	// audit
	InsertAudit(ctx context.Context, a AuditRecord) error
}

// sqliteRepo Repository 的 SQLite 实现。
type sqliteRepo struct {
	db *sql.DB
}

// OpenRepository 打开/创建 SQLite 仓库并执行 migration。
func OpenRepository(ctx context.Context, dbPath string) (Repository, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %s: %w", dbPath, err)
	}
	// SQLite 单连接写更简单且足够（per-app 串行 + 有限并发读）。
	db.SetMaxOpenConns(1)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	r := &sqliteRepo{db: db}
	if err := r.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return r, nil
}

func (r *sqliteRepo) migrate(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS schema_version (version INTEGER NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS apps (
			id            TEXT PRIMARY KEY,
			name          TEXT NOT NULL,
			runtime       TEXT NOT NULL,
			source_kind   TEXT NOT NULL DEFAULT 'inline',
			source_store_id TEXT NOT NULL DEFAULT '',
			source_version TEXT NOT NULL DEFAULT '',
			revision      INTEGER NOT NULL DEFAULT 0,
			observed_revision INTEGER NOT NULL DEFAULT 0,
			parameters    TEXT NOT NULL DEFAULT '{}',
			created_at    TEXT NOT NULL,
			updated_at    TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS revisions (
			app_id      TEXT NOT NULL,
			number      INTEGER NOT NULL,
			compose_hash TEXT NOT NULL,
			source_kind TEXT NOT NULL DEFAULT 'inline',
			source_version TEXT NOT NULL DEFAULT '',
			parameters  TEXT NOT NULL DEFAULT '{}',
			created_at  TEXT NOT NULL,
			created_by  TEXT NOT NULL DEFAULT '',
			note        TEXT NOT NULL DEFAULT '',
			PRIMARY KEY (app_id, number)
		)`,
		`CREATE TABLE IF NOT EXISTS tasks (
			id             TEXT PRIMARY KEY,
			app_id         TEXT NOT NULL,
			type           TEXT NOT NULL,
			action         TEXT NOT NULL DEFAULT '',
			status         TEXT NOT NULL,
			phase          TEXT NOT NULL DEFAULT '',
			revision       INTEGER NOT NULL DEFAULT 0,
			purge          INTEGER NOT NULL DEFAULT 0,
			idempotency_key TEXT NOT NULL DEFAULT '',
			request_summary TEXT NOT NULL DEFAULT '',
			message        TEXT NOT NULL DEFAULT '',
			created_at     TEXT NOT NULL,
			started_at     TEXT NOT NULL DEFAULT '',
			finished_at    TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_app_status ON tasks(app_id, status)`,
		`CREATE TABLE IF NOT EXISTS idempotency (
			key          TEXT PRIMARY KEY,
			request_hash TEXT NOT NULL,
			task_id      TEXT NOT NULL,
			created_at   TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS audit (
			id     INTEGER PRIMARY KEY AUTOINCREMENT,
			at     TEXT NOT NULL,
			actor  TEXT NOT NULL DEFAULT '',
			app_id TEXT NOT NULL DEFAULT '',
			action TEXT NOT NULL,
			task_id TEXT NOT NULL DEFAULT '',
			detail TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_app ON audit(app_id, at DESC)`,
	}
	for _, s := range stmts {
		if _, err := r.db.ExecContext(ctx, s); err != nil {
			return fmt.Errorf("migrate (%s): %w", firstLine(s), err)
		}
	}
	return nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}

func (r *sqliteRepo) Close() error { return r.db.Close() }

// --- apps ---

func (r *sqliteRepo) UpsertAppMeta(ctx context.Context, a AppRecord) error {
	params, _ := json.Marshal(a.Parameters)
	_, err := r.db.ExecContext(ctx, `INSERT INTO apps
		(id,name,runtime,source_kind,source_store_id,source_version,revision,observed_revision,parameters,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
		  name=excluded.name, runtime=excluded.runtime,
		  source_kind=excluded.source_kind, source_store_id=excluded.source_store_id,
		  source_version=excluded.source_version, revision=excluded.revision,
		  observed_revision=excluded.observed_revision,
		  parameters=excluded.parameters, updated_at=excluded.updated_at`,
		a.ID, a.Name, string(a.Runtime),
		string(a.Source.Kind), a.Source.StoreID, a.Source.Version,
		a.Revision, a.ObservedRevision, string(params), a.CreatedAt.Format(time.RFC3339Nano), a.UpdatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("upsert app meta: %w", err)
	}
	return nil
}

func (r *sqliteRepo) GetAppMeta(ctx context.Context, id string) (AppRecord, bool, error) {
	row := r.db.QueryRowContext(ctx, `SELECT id,name,runtime,source_kind,source_store_id,source_version,revision,observed_revision,parameters,created_at,updated_at FROM apps WHERE id=?`, id)
	a, err := scanApp(row.Scan)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return AppRecord{}, false, nil
		}
		return AppRecord{}, false, err
	}
	return a, true, nil
}

func (r *sqliteRepo) ListAppMetas(ctx context.Context) ([]AppRecord, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,name,runtime,source_kind,source_store_id,source_version,revision,observed_revision,parameters,created_at,updated_at FROM apps ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AppRecord
	for rows.Next() {
		a, err := scanApp(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (r *sqliteRepo) DeleteAppMeta(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM apps WHERE id=?`, id)
	return err
}

// SetObservedRevision apply 成功后更新已部署 revision。
func (r *sqliteRepo) SetObservedRevision(ctx context.Context, appID string, rev int64) error {
	_, err := r.db.ExecContext(ctx, `UPDATE apps SET observed_revision=?, updated_at=? WHERE id=?`,
		rev, time.Now().UTC().Format(time.RFC3339Nano), appID)
	return err
}

// PurgeApp 删除应用元数据与 revision。
func (r *sqliteRepo) PurgeApp(ctx context.Context, appID string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM revisions WHERE app_id=?`, appID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM apps WHERE id=?`, appID); err != nil {
		return err
	}
	return tx.Commit()
}

type scanner func(dest ...any) error

func scanApp(scan scanner) (AppRecord, error) {
	var a AppRecord
	var runtime, sourceKind, params, created, updated string
	var storeID, sourceVer sql.NullString
	if err := scan(&a.ID, &a.Name, &runtime, &sourceKind, &storeID, &sourceVer, &a.Revision, &a.ObservedRevision, &params, &created, &updated); err != nil {
		return AppRecord{}, err
	}
	a.Runtime = RuntimeKind(runtime)
	a.Source = ApplicationSource{Kind: SourceKind(sourceKind), StoreID: storeID.String, Version: sourceVer.String}
	_ = json.Unmarshal([]byte(params), &a.Parameters)
	if a.Parameters == nil {
		a.Parameters = map[string]string{}
	}
	a.CreatedAt = parseTime(created)
	a.UpdatedAt = parseTime(updated)
	return a, nil
}

// --- revisions ---

func (r *sqliteRepo) NextRevisionNumber(ctx context.Context, appID string) (int64, error) {
	// 单连接串行，简单 max+1。
	var maxNum sql.NullInt64
	if err := r.db.QueryRowContext(ctx, `SELECT MAX(number) FROM revisions WHERE app_id=?`, appID).Scan(&maxNum); err != nil {
		return 0, err
	}
	return maxNum.Int64 + 1, nil
}

func (r *sqliteRepo) InsertRevision(ctx context.Context, rev Revision) error {
	params, _ := json.Marshal(rev.Parameters)
	_, err := r.db.ExecContext(ctx, `INSERT INTO revisions
		(app_id,number,compose_hash,source_kind,source_version,parameters,created_at,created_by,note)
		VALUES (?,?,?,?,?,?,?,?,?)`,
		rev.AppID, rev.Number, rev.ComposeHash,
		string(rev.Source.Kind), rev.Source.Version, string(params),
		rev.CreatedAt.Format(time.RFC3339Nano), rev.CreatedBy, rev.Note)
	if err != nil {
		return fmt.Errorf("insert revision: %w", err)
	}
	return nil
}

func (r *sqliteRepo) ListRevisions(ctx context.Context, appID string) ([]Revision, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT app_id,number,compose_hash,source_kind,source_version,parameters,created_at,created_by,note FROM revisions WHERE app_id=? ORDER BY number DESC`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Revision
	for rows.Next() {
		rev, err := scanRevision(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, rev)
	}
	return out, rows.Err()
}

func (r *sqliteRepo) GetRevision(ctx context.Context, appID string, num int64) (Revision, bool, error) {
	row := r.db.QueryRowContext(ctx, `SELECT app_id,number,compose_hash,source_kind,source_version,parameters,created_at,created_by,note FROM revisions WHERE app_id=? AND number=?`, appID, num)
	rev, err := scanRevision(row.Scan)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Revision{}, false, nil
		}
		return Revision{}, false, err
	}
	return rev, true, nil
}

func scanRevision(scan scanner) (Revision, error) {
	var rev Revision
	var sourceKind, params, created string
	var createdBy, note sql.NullString
	if err := scan(&rev.AppID, &rev.Number, &rev.ComposeHash, &sourceKind, &rev.Source.Version, &params, &created, &createdBy, &note); err != nil {
		return Revision{}, err
	}
	rev.Source.Kind = SourceKind(sourceKind)
	_ = json.Unmarshal([]byte(params), &rev.Parameters)
	rev.CreatedAt = parseTime(created)
	rev.CreatedBy = createdBy.String
	rev.Note = note.String
	return rev, nil
}

// --- tasks ---

func (r *sqliteRepo) CreateTask(ctx context.Context, t Task) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO tasks
		(id,app_id,type,action,status,phase,revision,purge,idempotency_key,request_summary,message,created_at,started_at,finished_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		t.ID, t.AppID, string(t.Type), string(t.Action), string(t.Status), string(t.Phase),
		t.Revision, t.Purge, t.IdempotencyKey, t.RequestSummary, t.Message,
		t.CreatedAt.Format(time.RFC3339Nano), formatTimePtr(t.StartedAt), formatTimePtr(t.FinishedAt))
	if err != nil {
		return fmt.Errorf("create task: %w", err)
	}
	return nil
}

func (r *sqliteRepo) GetTask(ctx context.Context, id string) (Task, error) {
	row := r.db.QueryRowContext(ctx, `SELECT id,app_id,type,action,status,phase,revision,purge,idempotency_key,request_summary,message,created_at,started_at,finished_at FROM tasks WHERE id=?`, id)
	t, err := scanTask(row.Scan)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Task{}, NotFoundErr(id)
		}
		return Task{}, err
	}
	return t, nil
}

func (r *sqliteRepo) UpdateTask(ctx context.Context, id string, mut func(*Task)) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	row := tx.QueryRowContext(ctx, `SELECT id,app_id,type,action,status,phase,revision,purge,idempotency_key,request_summary,message,created_at,started_at,finished_at FROM tasks WHERE id=?`, id)
	t, err := scanTask(row.Scan)
	if err != nil {
		return err
	}
	mut(&t)
	_, err = tx.ExecContext(ctx, `UPDATE tasks SET status=?,phase=?,message=?,started_at=?,finished_at=? WHERE id=?`,
		string(t.Status), string(t.Phase), t.Message, formatTimePtr(t.StartedAt), formatTimePtr(t.FinishedAt), id)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (r *sqliteRepo) ListTasksByApp(ctx context.Context, appID string, limit int) ([]Task, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.db.QueryContext(ctx, `SELECT id,app_id,type,action,status,phase,revision,purge,idempotency_key,request_summary,message,created_at,started_at,finished_at FROM tasks WHERE app_id=? ORDER BY created_at DESC LIMIT ?`, appID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Task
	for rows.Next() {
		t, err := scanTask(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (r *sqliteRepo) ListNonTerminalTasks(ctx context.Context) ([]Task, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,app_id,type,action,status,phase,revision,purge,idempotency_key,request_summary,message,created_at,started_at,finished_at FROM tasks WHERE status IN ('queued','running') ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Task
	for rows.Next() {
		t, err := scanTask(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func scanTask(scan scanner) (Task, error) {
	var t Task
	var typ, action, status, phase, created string
	var started, finished sql.NullString
	if err := scan(&t.ID, &t.AppID, &typ, &action, &status, &phase, &t.Revision, &t.Purge, &t.IdempotencyKey, &t.RequestSummary, &t.Message, &created, &started, &finished); err != nil {
		return Task{}, err
	}
	t.Type = TaskType(typ)
	t.Action = Action(action)
	t.Status = TaskStatus(status)
	t.Phase = TaskPhase(phase)
	t.CreatedAt = parseTime(created)
	t.StartedAt = parseTimePtr(started.String)
	t.FinishedAt = parseTimePtr(finished.String)
	return t, nil
}

// --- idempotency ---

func (r *sqliteRepo) SaveIdempotency(ctx context.Context, key, requestHash, taskID string) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO idempotency (key,request_hash,task_id,created_at) VALUES (?,?,?,?)`,
		key, requestHash, taskID, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func (r *sqliteRepo) GetIdempotency(ctx context.Context, key string) (idemRecord, bool, error) {
	var rec idemRecord
	var created string
	err := r.db.QueryRowContext(ctx, `SELECT key,request_hash,task_id,created_at FROM idempotency WHERE key=?`, key).
		Scan(&rec.Key, &rec.RequestHash, &rec.TaskID, &created)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return idemRecord{}, false, nil
		}
		return idemRecord{}, false, err
	}
	rec.CreatedAt = parseTime(created)
	return rec, true, nil
}

// --- audit ---

func (r *sqliteRepo) InsertAudit(ctx context.Context, a AuditRecord) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO audit (at,actor,app_id,action,task_id,detail) VALUES (?,?,?,?,?,?)`,
		a.At.Format(time.RFC3339Nano), a.Actor, a.AppID, a.Action, a.TaskID, a.Detail)
	return err
}

// --- time helpers ---

func formatTimePtr(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format(time.RFC3339Nano)
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}

func parseTimePtr(s string) *time.Time {
	if s == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return nil
	}
	return &t
}
