package agentruntime

import (
	"context"
	"database/sql"
	"io/fs"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestRunPostgresGooseMigrationsFreshSchema(t *testing.T) {
	db := openPostgresMigrationTestDB(t)
	ctx := context.Background()

	if err := RunPostgresGooseMigrations(ctx, db, SQLDialectPostgres); err != nil {
		t.Fatalf("RunPostgresGooseMigrations() error = %v", err)
	}

	for _, table := range []string{
		"agent_sessions",
		"agent_messages",
		"agent_message_attachments",
		"agent_audit_logs",
		"agent_users",
		"agent_refresh_tokens",
		"agent_artifacts",
		"agent_jobs",
		"agent_job_events",
		"agent_skills",
		"agent_skill_executions",
		"agent_eval_runs",
		"agent_eval_golden_sets",
		"agent_eval_golden_cases",
		"agent_risk_events",
		"agent_llm_usage",
		"agent_runtime_config",
		"agent_memory",
		"agent_memory_settings",
		"agent_personalization_settings",
	} {
		assertPostgresTableExists(t, db, table)
	}
	assertPostgresGooseAtLatest(t, db)
}

func TestRunPostgresGooseMigrationsLegacySeedsGoose(t *testing.T) {
	db := openPostgresMigrationTestDB(t)
	ctx := context.Background()

	if _, err := db.ExecContext(ctx, `CREATE TABLE agent_schema_migrations (version INTEGER PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL)`); err != nil {
		t.Fatalf("create legacy migration table: %v", err)
	}
	for _, version := range []int{1, 4, 5, 6, 7} {
		if _, err := db.ExecContext(ctx, `INSERT INTO agent_schema_migrations (version, applied_at) VALUES ($1, $2)`, version, time.Now().UTC()); err != nil {
			t.Fatalf("seed legacy version %d: %v", version, err)
		}
	}
	createLegacyMessageCoreTables(t, db)

	if err := RunPostgresGooseMigrations(ctx, db, SQLDialectPostgres); err != nil {
		t.Fatalf("RunPostgresGooseMigrations() error = %v", err)
	}

	for _, version := range []int{1, 4, 5, 6, 7} {
		assertPostgresGooseVersionApplied(t, db, version)
	}
	assertPostgresGooseAtLatest(t, db)
}

func TestRunPostgresGooseMigrationsIsIdempotent(t *testing.T) {
	db := openPostgresMigrationTestDB(t)
	ctx := context.Background()

	if err := RunPostgresGooseMigrations(ctx, db, SQLDialectPostgres); err != nil {
		t.Fatalf("first RunPostgresGooseMigrations() error = %v", err)
	}
	firstCount := postgresAppliedGooseVersionCount(t, db)
	if err := RunPostgresGooseMigrations(ctx, db, SQLDialectPostgres); err != nil {
		t.Fatalf("second RunPostgresGooseMigrations() error = %v", err)
	}
	secondCount := postgresAppliedGooseVersionCount(t, db)
	if firstCount != secondCount {
		t.Fatalf("goose applied version count changed after idempotent rerun: first=%d second=%d", firstCount, secondCount)
	}
}

func TestRunPostgresGooseMigrationsDoesNotRerunLegacyDestructiveVersions(t *testing.T) {
	db := openPostgresMigrationTestDB(t)
	ctx := context.Background()

	if _, err := db.ExecContext(ctx, `CREATE TABLE agent_schema_migrations (version INTEGER PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL)`); err != nil {
		t.Fatalf("create legacy migration table: %v", err)
	}
	for _, version := range []int{6, 7} {
		if _, err := db.ExecContext(ctx, `INSERT INTO agent_schema_migrations (version, applied_at) VALUES ($1, $2)`, version, time.Now().UTC()); err != nil {
			t.Fatalf("seed legacy version %d: %v", version, err)
		}
	}
	createLegacyMessageCoreTables(t, db)
	if _, err := db.ExecContext(ctx, `
INSERT INTO agent_sessions (
	user_id, session_id, agent_id, title, status, message_count, total_tokens, working_dir,
	tags, description, parent_id, branch_point, metadata, archived, created_at, updated_at, last_message_at
) VALUES (
	'legacy-user', 'legacy-session', '', 'sentinel', 1, 0, 0, '',
	'[]', '', '', 0, '{}', 0, now(), now(), NULL
)`); err != nil {
		t.Fatalf("insert legacy sentinel session: %v", err)
	}

	if err := RunPostgresGooseMigrations(ctx, db, SQLDialectPostgres); err != nil {
		t.Fatalf("RunPostgresGooseMigrations() error = %v", err)
	}

	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_sessions WHERE user_id = 'legacy-user' AND session_id = 'legacy-session'`).Scan(&count); err != nil {
		t.Fatalf("count legacy sentinel session: %v", err)
	}
	if count != 1 {
		t.Fatalf("legacy sentinel session count = %d, want 1; destructive migrations likely reran", count)
	}
	assertPostgresGooseVersionApplied(t, db, 6)
	assertPostgresGooseVersionApplied(t, db, 7)
}

func openPostgresMigrationTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("AGENT_RUNTIME_TEST_PG_DSN"))
	if dsn == "" {
		t.Skip("set AGENT_RUNTIME_TEST_PG_DSN to run postgres migration integration tests")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open pg: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	schema := "agentruntime_migration_test_" + strings.NewReplacer("-", "_", ".", "_").Replace(newSortableID())
	quotedSchema := postgresQuoteIdentifier(schema)
	if _, err := db.Exec(`CREATE SCHEMA ` + quotedSchema); err != nil {
		_ = db.Close()
		t.Fatalf("create schema %s: %v", schema, err)
	}
	if _, err := db.Exec(`SET search_path TO ` + quotedSchema); err != nil {
		_, _ = db.Exec(`DROP SCHEMA ` + quotedSchema + ` CASCADE`)
		_ = db.Close()
		t.Fatalf("set search_path %s: %v", schema, err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(`DROP SCHEMA ` + quotedSchema + ` CASCADE`)
		_ = db.Close()
	})
	return db
}

func createLegacyMessageCoreTables(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS agent_sessions (
	user_id TEXT NOT NULL,
	session_id TEXT NOT NULL,
	agent_id TEXT NOT NULL DEFAULT '',
	title TEXT NOT NULL DEFAULT '',
	status INTEGER NOT NULL DEFAULT 1,
	message_count INTEGER NOT NULL DEFAULT 0,
	total_tokens BIGINT NOT NULL DEFAULT 0,
	working_dir TEXT NOT NULL DEFAULT '',
	tags JSONB NOT NULL DEFAULT '[]',
	description TEXT NOT NULL DEFAULT '',
	parent_id TEXT NOT NULL DEFAULT '',
	branch_point INTEGER NOT NULL DEFAULT 0,
	metadata JSONB NOT NULL DEFAULT '{}',
	archived INTEGER NOT NULL DEFAULT 0,
	created_at TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL,
	last_message_at TIMESTAMPTZ,
	PRIMARY KEY (user_id, session_id)
)`,
		`CREATE TABLE IF NOT EXISTS agent_messages (
	message_id TEXT PRIMARY KEY,
	session_id TEXT NOT NULL,
	user_id TEXT NOT NULL,
	seq_no BIGINT NOT NULL,
	parent_id TEXT NOT NULL DEFAULT '',
	role TEXT NOT NULL,
	content_type TEXT NOT NULL DEFAULT 'text',
	content TEXT NOT NULL DEFAULT '',
	content_parts JSONB NOT NULL DEFAULT '[]',
	tool_call_id TEXT NOT NULL DEFAULT '',
	tool_name TEXT NOT NULL DEFAULT '',
	tool_input JSONB NOT NULL DEFAULT '{}',
	tool_output TEXT NOT NULL DEFAULT '',
	tool_calls JSONB NOT NULL DEFAULT '[]',
	prompt_tokens INTEGER NOT NULL DEFAULT 0,
	completion_tokens INTEGER NOT NULL DEFAULT 0,
	status INTEGER NOT NULL DEFAULT 1,
	is_context_used INTEGER NOT NULL DEFAULT 1,
	model_id TEXT NOT NULL DEFAULT '',
	run_id TEXT NOT NULL DEFAULT '',
	hidden INTEGER NOT NULL DEFAULT 0,
	created_at TIMESTAMPTZ NOT NULL,
	updated_at TIMESTAMPTZ NOT NULL
)`,
		`CREATE TABLE IF NOT EXISTS agent_message_attachments (
	attachment_id TEXT NOT NULL,
	message_id TEXT NOT NULL,
	session_id TEXT NOT NULL,
	user_id TEXT NOT NULL,
	file_type TEXT NOT NULL,
	mime_type TEXT NOT NULL,
	file_name TEXT NOT NULL DEFAULT '',
	file_size BIGINT NOT NULL DEFAULT 0,
	storage_key TEXT NOT NULL,
	thumbnail_key TEXT NOT NULL DEFAULT '',
	embedding_status INTEGER NOT NULL DEFAULT 0,
	created_at TIMESTAMPTZ NOT NULL,
	PRIMARY KEY (message_id, attachment_id)
)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("create legacy message core table: %v", err)
		}
	}
}

func assertPostgresTableExists(t *testing.T, db *sql.DB, table string) {
	t.Helper()
	var exists bool
	if err := db.QueryRow(`SELECT to_regclass($1) IS NOT NULL`, table).Scan(&exists); err != nil {
		t.Fatalf("check table %s: %v", table, err)
	}
	if !exists {
		t.Fatalf("expected table %s to exist", table)
	}
}

func assertPostgresGooseAtLatest(t *testing.T, db *sql.DB) {
	t.Helper()
	latest := latestEmbeddedPostgresMigrationVersion(t)
	assertPostgresGooseVersionApplied(t, db, latest)
}

func assertPostgresGooseVersionApplied(t *testing.T, db *sql.DB, version int) {
	t.Helper()
	var exists bool
	if err := db.QueryRow(`SELECT EXISTS (SELECT 1 FROM goose_db_version WHERE version_id = $1 AND is_applied)`, version).Scan(&exists); err != nil {
		t.Fatalf("check goose version %d: %v", version, err)
	}
	if !exists {
		t.Fatalf("expected goose version %d to be applied", version)
	}
}

func postgresAppliedGooseVersionCount(t *testing.T, db *sql.DB) int {
	t.Helper()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM goose_db_version WHERE version_id > 0 AND is_applied`).Scan(&count); err != nil {
		t.Fatalf("count goose versions: %v", err)
	}
	return count
}

func latestEmbeddedPostgresMigrationVersion(t *testing.T) int {
	t.Helper()
	entries, err := fs.ReadDir(postgresMigrationFS, "migrations/postgres")
	if err != nil {
		t.Fatalf("read embedded postgres migrations: %v", err)
	}
	versionPattern := regexp.MustCompile(`^([0-9]+)_.*\.sql$`)
	var versions []int
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		match := versionPattern.FindStringSubmatch(entry.Name())
		if len(match) != 2 {
			continue
		}
		version, err := strconv.Atoi(match[1])
		if err != nil {
			t.Fatalf("parse migration version %q: %v", entry.Name(), err)
		}
		versions = append(versions, version)
	}
	if len(versions) == 0 {
		t.Fatal("no embedded postgres migration versions found")
	}
	sort.Ints(versions)
	return versions[len(versions)-1]
}

func postgresQuoteIdentifier(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}
