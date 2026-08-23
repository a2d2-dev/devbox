package apps

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// TestComposeProjectName 单一 project helper：接管保留原名，devbox 创建用 devbox-<id>。
func TestComposeProjectName(t *testing.T) {
	cases := []struct {
		name string
		meta AppRecord
		want string
	}{
		{"devbox-created", AppRecord{ID: "nginx-svc"}, "devbox-nginx-svc"},
		{"taken-over-keeps-original", AppRecord{ID: "ext-gitea-abcd1234abcd", OriginalProject: "gitea"}, "gitea"},
		{"original-wins-over-id", AppRecord{ID: "whatever", OriginalProject: "my-stack"}, "my-stack"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ComposeProjectName(c.meta); got != c.want {
				t.Fatalf("ComposeProjectName(%+v) = %q, want %q", c.meta, got, c.want)
			}
		})
	}
}

// TestExternalID 稳定、合法、互不碰撞、≤63。重点覆盖 a_b vs a-b 的 slug 碰撞。
func TestExternalID(t *testing.T) {
	t.Run("a_b_vs_a_b_dash_do_not_collide", func(t *testing.T) {
		// "a_b" 与 "a-b" 的 slug 都是 "a-b"；必须靠原始 name 的 hash 区分。
		idUnderscore := ExternalID("a_b")
		idDash := ExternalID("a-b")
		if idUnderscore == idDash {
			t.Fatalf("a_b (%q) 与 a-b (%q) 碰撞为同一 ID", idUnderscore, idDash)
		}
		// 二者都应带可读 slug 前缀 ext-a-b-。
		if len(idUnderscore) < 4 || idUnderscore[:4] != "ext-" {
			t.Fatalf("a_b ID %q 缺少 ext- 前缀", idUnderscore)
		}
	})

	t.Run("stable_across_calls", func(t *testing.T) {
		for _, project := range []string{"gitea", "My Stack", "a_b", "长名字项目", "x"} {
			first := ExternalID(project)
			second := ExternalID(project)
			if first != second {
				t.Fatalf("project %q 不稳定: %q vs %q", project, first, second)
			}
			if !isValidAppID(first) {
				t.Fatalf("project %q 生成非法 ID %q", project, first)
			}
		}
	})

	t.Run("distinct_projects_distinct_ids", func(t *testing.T) {
		seen := map[string]string{}
		for _, project := range []string{"gitea", "nextcloud", "grafana", "a-b", "a_b", "A_B"} {
			id := ExternalID(project)
			if owner, dup := seen[id]; dup {
				t.Fatalf("project %q 与 %q 碰撞为 %q", project, owner, id)
			}
			seen[id] = project
		}
	})

	t.Run("all_illegal_chars_fallback", func(t *testing.T) {
		// 全非法/符号 → slug 兜底 "discovered"，仍合法稳定。
		id := ExternalID("___###___")
		if !isValidAppID(id) {
			t.Fatalf("全非法字符生成非法 ID %q", id)
		}
		if ExternalID("___###___") != id {
			t.Fatalf("全非法字符 ID 不稳定")
		}
	})

	t.Run("super_long_truncated_to_63_and_valid", func(t *testing.T) {
		long := ""
		for i := 0; i < 200; i++ {
			long += "z"
		}
		id := ExternalID(long)
		if len(id) > 63 {
			t.Fatalf("超长 project 生成 ID 长度 %d > 63: %q", len(id), id)
		}
		if !isValidAppID(id) {
			t.Fatalf("超长 project 生成非法 ID %q", id)
		}
		// 截断后仍稳定。
		if ExternalID(long) != id {
			t.Fatalf("超长 project ID 不稳定")
		}
	})

	t.Run("empty_project_returns_empty", func(t *testing.T) {
		if ExternalID("") != "" {
			t.Fatalf("空 project 应返回空 ID")
		}
	})

	t.Run("is_external_id", func(t *testing.T) {
		if !IsExternalID(ExternalID("gitea")) {
			t.Fatalf("ExternalID 结果未被 IsExternalID 识别")
		}
		if IsExternalID("nginx-svc") {
			t.Fatalf("普通 managed id 被误判为 external")
		}
	})
}

// TestOriginalProjectMigration 验证 apps.original_project 列：新库 round-trip + 旧库 ALTER。
func TestOriginalProjectMigration(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	t.Run("roundtrip_preserves_original_project", func(t *testing.T) {
		dbPath := filepath.Join(dir, "new.db")
		repo, err := OpenRepository(ctx, dbPath)
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		meta := AppRecord{
			ID: "ext-gitea-hashhash12", Name: "gitea", Runtime: RuntimeCompose,
			Source: ApplicationSource{Kind: SourceLocal}, OriginalProject: "gitea",
			Revision: 1, ObservedRevision: 1, Parameters: map[string]string{},
		}
		if err := repo.UpsertAppMeta(ctx, meta); err != nil {
			t.Fatalf("upsert: %v", err)
		}
		if err := repo.Close(); err != nil {
			t.Fatalf("close: %v", err)
		}

		repo2, err := OpenRepository(ctx, dbPath) // 模拟进程重启后重新打开
		if err != nil {
			t.Fatalf("reopen: %v", err)
		}
		defer repo2.Close()
		got, ok, err := repo2.GetAppMeta(ctx, meta.ID)
		if err != nil || !ok {
			t.Fatalf("get after reopen: ok=%v err=%v", ok, err)
		}
		if got.OriginalProject != "gitea" {
			t.Fatalf("OriginalProject 未持久化/恢复: got %q", got.OriginalProject)
		}
		if ComposeProjectName(got) != "gitea" {
			t.Fatalf("重启后 ComposeProjectName 应解析为原 project name, got %q", ComposeProjectName(got))
		}
		// ListAppMetas 也应带回 OriginalProject。
		all, err := repo2.ListAppMetas(ctx)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(all) != 1 || all[0].OriginalProject != "gitea" {
			t.Fatalf("ListAppMetas 未带回 OriginalProject: %+v", all)
		}
	})

	t.Run("legacy_db_without_column_gets_altered", func(t *testing.T) {
		dbPath := filepath.Join(dir, "legacy.db")
		// 手工建一个「旧 schema」apps 表（无 original_project 列），写入一行旧数据。
		raw, err := sql.Open("sqlite", "file:"+dbPath)
		if err != nil {
			t.Fatalf("open raw: %v", err)
		}
		_, err = raw.Exec(`CREATE TABLE apps (
			id TEXT PRIMARY KEY, name TEXT NOT NULL, runtime TEXT NOT NULL,
			source_kind TEXT NOT NULL DEFAULT 'inline', source_store_id TEXT NOT NULL DEFAULT '',
			source_catalog_id TEXT NOT NULL DEFAULT '', source_version TEXT NOT NULL DEFAULT '',
			revision INTEGER NOT NULL DEFAULT 0, observed_revision INTEGER NOT NULL DEFAULT 0,
			parameters TEXT NOT NULL DEFAULT '{}', created_at TEXT NOT NULL, updated_at TEXT NOT NULL
		)`)
		if err != nil {
			t.Fatalf("create legacy table: %v", err)
		}
		_, err = raw.Exec(`INSERT INTO apps(id,name,runtime,revision,observed_revision,created_at,updated_at)
			VALUES('legacy-app','legacy','compose',2,2,'2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`)
		if err != nil {
			t.Fatalf("seed legacy row: %v", err)
		}
		if err := raw.Close(); err != nil {
			t.Fatalf("close raw: %v", err)
		}

		// OpenRepository 应通过 ensureTextColumn 补列，旧行 OriginalProject 默认空。
		repo, err := OpenRepository(ctx, dbPath)
		if err != nil {
			t.Fatalf("open legacy (migrate): %v", err)
		}
		defer repo.Close()
		got, ok, err := repo.GetAppMeta(ctx, "legacy-app")
		if err != nil || !ok {
			t.Fatalf("get legacy: ok=%v err=%v", ok, err)
		}
		if got.OriginalProject != "" {
			t.Fatalf("旧行 OriginalProject 应为空, got %q", got.OriginalProject)
		}
		// 补列后能正常 upsert 写入 OriginalProject（接管升级路径）。
		got.OriginalProject = "legacy-stack"
		got.Revision = 2
		if err := repo.UpsertAppMeta(ctx, got); err != nil {
			t.Fatalf("upsert after migrate: %v", err)
		}
		again, _, err := repo.GetAppMeta(ctx, "legacy-app")
		if err != nil {
			t.Fatalf("get again: %v", err)
		}
		if again.OriginalProject != "legacy-stack" {
			t.Fatalf("迁移后写入 OriginalProject 未生效: %q", again.OriginalProject)
		}
	})
}
