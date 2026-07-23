package apps

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
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

// CatalogSourceRecord 持久化的动态 catalog source（同 apps.db）。
// Token 为只读 Git token（secret）：入库，但绝不回前端（由 CatalogSourceManager 产出 SafeView）。
type CatalogSourceRecord struct {
	ID        string
	Name      string
	Kind      string // http|git|1panel
	URL       string
	Ref       string
	Path      string
	Token     string
	Enabled   bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

// CatalogSourceChange 一次来源变更 + 审计（同事务提交）。
//   - Record != nil → upsert（create/update）；Record == nil → 删除 DeleteID。
//   - PreserveEmptyToken：upsert 时若 Record.Token 为空，保留库中既有 token（避免误擦除）。
//     本 PR 不支持显式清空 token（无 clear 语义），避免误删只读凭证。
//   - Audit.Detail 由调用方脱敏：仅 source id/kind/action/changed-fields/managedBy，
//     绝不含 token、tokenSet 值、完整敏感 URL/query（query 已被 validateDynamicCatalogURL 拒绝）。
type CatalogSourceChange struct {
	Record             *CatalogSourceRecord
	DeleteID           string
	PreserveEmptyToken bool
	Audit              AuditRecord
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

	// 原子写（Issue #2 一致性）：把多步 DB 变更放进单事务，避免半状态。
	// CommitApply：revision + app meta + task (+ idempotency) 同事务。
	CommitApply(ctx context.Context, meta AppRecord, rev Revision, task Task, idemKey, requestHash string) error
	// CommitTask：task (+ idempotency) 同事务（operate/remove 用）。
	CommitTask(ctx context.Context, task Task, idemKey, requestHash string) error

	// audit
	InsertAudit(ctx context.Context, a AuditRecord) error

	// catalog sources（动态来源持久化，同 apps.db；token 为 secret，调用方负责不回显）
	// 变更与审计同事务（CommitCatalogSourceChange），避免崩溃留下未审计变更。
	ListCatalogSources(ctx context.Context) ([]CatalogSourceRecord, error)
	GetCatalogSource(ctx context.Context, id string) (CatalogSourceRecord, bool, error)
	CommitCatalogSourceChange(ctx context.Context, change CatalogSourceChange) error
}

// sqliteRepo Repository 的 SQLite 实现。
type sqliteRepo struct {
	db  *sql.DB
	now func() time.Time // 时钟注入（默认 time.Now；测试可覆盖）
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
	r := &sqliteRepo{db: db, now: time.Now}
	if err := r.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	// catalog_sources.token 等敏感数据入库，收紧数据库及当前 WAL/SHM 文件权限。
	// 不修改调用方提供的父目录权限，避免影响共享数据目录。
	for _, suffix := range []string{"", "-wal", "-shm"} {
		_ = os.Chmod(dbPath+suffix, 0o600)
	}
	return r, nil
}

// setClock 注入仓库时钟（仅测试用）。
func (r *sqliteRepo) setClock(now func() time.Time) {
	if now != nil {
		r.now = now
	}
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
			source_catalog_id TEXT NOT NULL DEFAULT '',
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
			source_store_id TEXT NOT NULL DEFAULT '',
			source_catalog_id TEXT NOT NULL DEFAULT '',
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
		`CREATE TABLE IF NOT EXISTS catalog_sources (
			id         TEXT PRIMARY KEY,
			name       TEXT NOT NULL DEFAULT '',
			kind       TEXT NOT NULL,
			url        TEXT NOT NULL,
			ref        TEXT NOT NULL DEFAULT '',
			path       TEXT NOT NULL DEFAULT '',
			token      TEXT NOT NULL DEFAULT '',
			enabled    INTEGER NOT NULL DEFAULT 1,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
	}
	for _, s := range stmts {
		if _, err := r.db.ExecContext(ctx, s); err != nil {
			return fmt.Errorf("migrate (%s): %w", firstLine(s), err)
		}
	}
	// 旧版 Issue #2 数据库已经存在 apps/revisions 表时，CREATE TABLE IF NOT
	// EXISTS 不会补列。来源是升级/restore 的可信路由，必须随 migration 保留。
	for _, column := range []struct {
		table, name string
	}{
		{"apps", "source_catalog_id"},
		{"revisions", "source_store_id"},
		{"revisions", "source_catalog_id"},
	} {
		if err := r.ensureTextColumn(ctx, column.table, column.name); err != nil {
			return err
		}
	}
	return nil
}

func (r *sqliteRepo) ensureTextColumn(ctx context.Context, table, column string) error {
	rows, err := r.db.QueryContext(ctx, "PRAGMA table_info("+table+")")
	if err != nil {
		return fmt.Errorf("inspect schema %s: %w", table, err)
	}
	found := false
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, kind string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &kind, &notNull, &defaultValue, &primaryKey); err != nil {
			rows.Close()
			return fmt.Errorf("inspect schema %s: %w", table, err)
		}
		if name == column {
			found = true
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("inspect schema %s: %w", table, err)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if found {
		return nil
	}
	if _, err := r.db.ExecContext(ctx, "ALTER TABLE "+table+" ADD COLUMN "+column+" TEXT NOT NULL DEFAULT ''"); err != nil {
		return fmt.Errorf("add schema column %s.%s: %w", table, column, err)
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

// dbExecer 被 *sql.DB 与 *sql.Tx 同时满足，便于把多条写放进同一事务。
type dbExecer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func (r *sqliteRepo) UpsertAppMeta(ctx context.Context, a AppRecord) error {
	return upsertAppMetaExec(r.db, ctx, a)
}

func upsertAppMetaExec(e dbExecer, ctx context.Context, a AppRecord) error {
	params, _ := json.Marshal(a.Parameters)
	_, err := e.ExecContext(ctx, `INSERT INTO apps
		(id,name,runtime,source_kind,source_store_id,source_catalog_id,source_version,revision,observed_revision,parameters,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
		  name=excluded.name, runtime=excluded.runtime,
		  source_kind=excluded.source_kind, source_store_id=excluded.source_store_id,
		  source_catalog_id=excluded.source_catalog_id, source_version=excluded.source_version, revision=excluded.revision,
		  observed_revision=excluded.observed_revision,
		  parameters=excluded.parameters, updated_at=excluded.updated_at`,
		a.ID, a.Name, string(a.Runtime),
		string(a.Source.Kind), a.Source.StoreID, a.Source.CatalogID, a.Source.Version,
		a.Revision, a.ObservedRevision, string(params), a.CreatedAt.Format(time.RFC3339Nano), a.UpdatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("upsert app meta: %w", err)
	}
	return nil
}

func (r *sqliteRepo) GetAppMeta(ctx context.Context, id string) (AppRecord, bool, error) {
	row := r.db.QueryRowContext(ctx, `SELECT id,name,runtime,source_kind,source_store_id,source_catalog_id,source_version,revision,observed_revision,parameters,created_at,updated_at FROM apps WHERE id=?`, id)
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
	rows, err := r.db.QueryContext(ctx, `SELECT id,name,runtime,source_kind,source_store_id,source_catalog_id,source_version,revision,observed_revision,parameters,created_at,updated_at FROM apps ORDER BY name`)
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
		rev, r.now().UTC().Format(time.RFC3339Nano), appID)
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

// CommitApply 原子写入 revision + app meta + task (+ idempotency)。任一步失败整体回滚，
// 不留半状态。task.CreatedAt 复用为 idempotency.created_at（同一逻辑时刻）。
func (r *sqliteRepo) CommitApply(ctx context.Context, meta AppRecord, rev Revision, task Task, idemKey, requestHash string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := insertRevisionExec(tx, ctx, rev); err != nil {
		return err
	}
	if err := upsertAppMetaExec(tx, ctx, meta); err != nil {
		return err
	}
	if err := createTaskExec(tx, ctx, task); err != nil {
		return err
	}
	if idemKey != "" {
		if err := saveIdempotencyExec(tx, ctx, idemKey, requestHash, task.ID, task.CreatedAt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// CommitTask 原子写入 task (+ idempotency)（operate/remove 用）。
func (r *sqliteRepo) CommitTask(ctx context.Context, task Task, idemKey, requestHash string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := createTaskExec(tx, ctx, task); err != nil {
		return err
	}
	if idemKey != "" {
		if err := saveIdempotencyExec(tx, ctx, idemKey, requestHash, task.ID, task.CreatedAt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

type scanner func(dest ...any) error

func scanApp(scan scanner) (AppRecord, error) {
	var a AppRecord
	var runtime, sourceKind, params, created, updated string
	var storeID, catalogID, sourceVer sql.NullString
	if err := scan(&a.ID, &a.Name, &runtime, &sourceKind, &storeID, &catalogID, &sourceVer, &a.Revision, &a.ObservedRevision, &params, &created, &updated); err != nil {
		return AppRecord{}, err
	}
	a.Runtime = RuntimeKind(runtime)
	a.Source = ApplicationSource{Kind: SourceKind(sourceKind), StoreID: storeID.String, CatalogID: catalogID.String, Version: sourceVer.String}
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
	return insertRevisionExec(r.db, ctx, rev)
}

func insertRevisionExec(e dbExecer, ctx context.Context, rev Revision) error {
	params, _ := json.Marshal(rev.Parameters)
	_, err := e.ExecContext(ctx, `INSERT INTO revisions
		(app_id,number,compose_hash,source_kind,source_store_id,source_catalog_id,source_version,parameters,created_at,created_by,note)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		rev.AppID, rev.Number, rev.ComposeHash,
		string(rev.Source.Kind), rev.Source.StoreID, rev.Source.CatalogID, rev.Source.Version, string(params),
		rev.CreatedAt.Format(time.RFC3339Nano), rev.CreatedBy, rev.Note)
	if err != nil {
		return fmt.Errorf("insert revision: %w", err)
	}
	return nil
}

func (r *sqliteRepo) ListRevisions(ctx context.Context, appID string) ([]Revision, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT app_id,number,compose_hash,source_kind,source_store_id,source_catalog_id,source_version,parameters,created_at,created_by,note FROM revisions WHERE app_id=? ORDER BY number DESC`, appID)
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
	row := r.db.QueryRowContext(ctx, `SELECT app_id,number,compose_hash,source_kind,source_store_id,source_catalog_id,source_version,parameters,created_at,created_by,note FROM revisions WHERE app_id=? AND number=?`, appID, num)
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
	var storeID, catalogID, sourceVersion, createdBy, note sql.NullString
	if err := scan(&rev.AppID, &rev.Number, &rev.ComposeHash, &sourceKind, &storeID, &catalogID, &sourceVersion, &params, &created, &createdBy, &note); err != nil {
		return Revision{}, err
	}
	rev.Source = ApplicationSource{Kind: SourceKind(sourceKind), StoreID: storeID.String, CatalogID: catalogID.String, Version: sourceVersion.String}
	_ = json.Unmarshal([]byte(params), &rev.Parameters)
	rev.CreatedAt = parseTime(created)
	rev.CreatedBy = createdBy.String
	rev.Note = note.String
	return rev, nil
}

// --- tasks ---

func (r *sqliteRepo) CreateTask(ctx context.Context, t Task) error {
	return createTaskExec(r.db, ctx, t)
}

func createTaskExec(e dbExecer, ctx context.Context, t Task) error {
	_, err := e.ExecContext(ctx, `INSERT INTO tasks
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
	return saveIdempotencyExec(r.db, ctx, key, requestHash, taskID, r.now())
}

func saveIdempotencyExec(e dbExecer, ctx context.Context, key, requestHash, taskID string, now time.Time) error {
	_, err := e.ExecContext(ctx, `INSERT INTO idempotency (key,request_hash,task_id,created_at) VALUES (?,?,?,?)`,
		key, requestHash, taskID, now.UTC().Format(time.RFC3339Nano))
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

// --- catalog_sources CRUD ---

func (r *sqliteRepo) ListCatalogSources(ctx context.Context) ([]CatalogSourceRecord, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,name,kind,url,ref,path,token,enabled,created_at,updated_at FROM catalog_sources ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("list catalog sources: %w", err)
	}
	defer rows.Close()
	var out []CatalogSourceRecord
	for rows.Next() {
		var rec CatalogSourceRecord
		var enabled int
		var createdAt, updatedAt string
		if err := rows.Scan(&rec.ID, &rec.Name, &rec.Kind, &rec.URL, &rec.Ref, &rec.Path, &rec.Token, &enabled, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("scan catalog source: %w", err)
		}
		rec.Enabled = enabled != 0
		rec.CreatedAt = parseTime(createdAt)
		rec.UpdatedAt = parseTime(updatedAt)
		out = append(out, rec)
	}
	return out, rows.Err()
}

// GetCatalogSource 按 id 取单条（含 token；调用方负责脱敏）。
func (r *sqliteRepo) GetCatalogSource(ctx context.Context, id string) (CatalogSourceRecord, bool, error) {
	var rec CatalogSourceRecord
	var enabled int
	var createdAt, updatedAt string
	err := r.db.QueryRowContext(ctx,
		`SELECT id,name,kind,url,ref,path,token,enabled,created_at,updated_at FROM catalog_sources WHERE id=?`, id).
		Scan(&rec.ID, &rec.Name, &rec.Kind, &rec.URL, &rec.Ref, &rec.Path, &rec.Token, &enabled, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return CatalogSourceRecord{}, false, nil
	}
	if err != nil {
		return CatalogSourceRecord{}, false, fmt.Errorf("get catalog source: %w", err)
	}
	rec.Enabled = enabled != 0
	rec.CreatedAt = parseTime(createdAt)
	rec.UpdatedAt = parseTime(updatedAt)
	return rec, true, nil
}

// CommitCatalogSourceChange 单事务提交来源变更 + 审计（任一步失败整体回滚）。
// upsert 时 PreserveEmptyToken 且 Record.Token 为空 → 复用库中既有 token。
func (r *sqliteRepo) CommitCatalogSourceChange(ctx context.Context, change CatalogSourceChange) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if change.Record != nil {
		rec := *change.Record
		if strings.TrimSpace(rec.ID) == "" {
			return fmt.Errorf("catalog source id required")
		}
		if change.PreserveEmptyToken && strings.TrimSpace(rec.Token) == "" {
			existing, ok, err := catalogSourceInTx(ctx, tx, rec.ID)
			if err != nil {
				return fmt.Errorf("read existing catalog source: %w", err)
			}
			if ok && existing.Token != "" {
				rec.Token = existing.Token
			}
		}
		now := r.now().UTC()
		if rec.CreatedAt.IsZero() {
			rec.CreatedAt = now
		}
		rec.UpdatedAt = now
		enabled := 0
		if rec.Enabled {
			enabled = 1
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO catalog_sources(id,name,kind,url,ref,path,token,enabled,created_at,updated_at)
			VALUES(?,?,?,?,?,?,?,?,?,?)
			ON CONFLICT(id) DO UPDATE SET name=excluded.name, kind=excluded.kind, url=excluded.url, ref=excluded.ref, path=excluded.path, token=excluded.token, enabled=excluded.enabled, updated_at=excluded.updated_at`,
			rec.ID, rec.Name, rec.Kind, rec.URL, rec.Ref, rec.Path, rec.Token, enabled,
			rec.CreatedAt.Format(time.RFC3339Nano), rec.UpdatedAt.Format(time.RFC3339Nano)); err != nil {
			return fmt.Errorf("upsert catalog source: %w", err)
		}
	} else {
		id := strings.TrimSpace(change.DeleteID)
		if id == "" {
			return fmt.Errorf("delete catalog source id required")
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM catalog_sources WHERE id=?`, id); err != nil {
			return fmt.Errorf("delete catalog source: %w", err)
		}
	}

	// 同事务写审计（detail 由调用方脱敏：仅 id/kind/action/changed-fields/managedBy，不含 token/URL 全文）。
	if err := insertAuditTx(ctx, tx, change.Audit, r.now); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit catalog source change: %w", err)
	}
	committed = true
	return nil
}

// catalogSourceInTx 事务内按 id 读单条（preserve-token 用）。
func catalogSourceInTx(ctx context.Context, tx *sql.Tx, id string) (CatalogSourceRecord, bool, error) {
	var rec CatalogSourceRecord
	var enabled int
	var createdAt, updatedAt string
	err := tx.QueryRowContext(ctx,
		`SELECT id,name,kind,url,ref,path,token,enabled,created_at,updated_at FROM catalog_sources WHERE id=?`, id).
		Scan(&rec.ID, &rec.Name, &rec.Kind, &rec.URL, &rec.Ref, &rec.Path, &rec.Token, &enabled, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return CatalogSourceRecord{}, false, nil
	}
	if err != nil {
		return CatalogSourceRecord{}, false, err
	}
	rec.Enabled = enabled != 0
	return rec, true, nil
}

// insertAuditTx 事务内写审计（与 insertAuditImpl 共享语句）。
func insertAuditTx(ctx context.Context, tx *sql.Tx, a AuditRecord, now func() time.Time) error {
	at := a.At
	if at.IsZero() {
		at = now().UTC()
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO audit(at,actor,app_id,action,task_id,detail) VALUES(?,?,?,?,?,?)`,
		at.Format(time.RFC3339Nano), a.Actor, a.AppID, a.Action, a.TaskID, a.Detail); err != nil {
		return fmt.Errorf("insert audit: %w", err)
	}
	return nil
}
