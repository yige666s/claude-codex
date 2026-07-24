package agentruntime

import (
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"claude-codex/internal/harness/state"

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
		"agent_message_event_outbox",
		"agent_audit_logs",
		"agent_users",
		"agent_refresh_tokens",
		"agent_artifacts",
		"agent_jobs",
		"agent_job_events",
		"agent_job_queue_outbox",
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
		"agent_memory_episodes",
		"agent_personalization_settings",
		"agent_workflow_runs",
		"agent_workflow_steps",
		"agent_tool_call_ledger",
		"agent_loop_goals",
		"agent_loop_triggers",
		"agent_deep_agent_evidence",
		"agent_connector_connections",
		"agent_connector_oauth_states",
		"agent_connector_tokens",
		"agent_mcp_servers",
		"agent_mcp_tool_policies",
	} {
		assertPostgresTableExists(t, db, table)
	}
	assertPostgresColumnExists(t, db, "agent_jobs", "loop_goal_id")
	assertPostgresColumnExists(t, db, "agent_jobs", "execution_owner")
	assertPostgresColumnExists(t, db, "agent_jobs", "execution_epoch")
	assertPostgresColumnExists(t, db, "agent_jobs", "execution_lease_expires_at")
	assertPostgresGooseAtLatest(t, db)
}

func TestSQLMessageAppendCreatesDurableEventOutbox(t *testing.T) {
	db := openPostgresMigrationTestDB(t)
	ctx := context.Background()
	if err := RunPostgresGooseMigrations(ctx, db, SQLDialectPostgres); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	store := NewSQLSessionStoreWithDialect(db, SQLDialectPostgres)
	if err := store.Init(ctx); err != nil {
		t.Fatalf("init session store: %v", err)
	}
	session, err := store.Create(ctx, "outbox-user", "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	message, err := store.AppendMessage(ctx, "outbox-user", session.ID, state.Message{
		ID:          "message-outbox-" + newSortableID(),
		Role:        state.MessageRoleUser,
		ContentType: state.MessageContentTypeText,
		Content:     "durable event",
	})
	if err != nil {
		t.Fatalf("append message: %v", err)
	}
	var count int
	if err := db.QueryRowContext(ctx, `
SELECT count(*)
FROM agent_message_event_outbox
WHERE event_id = $1 AND message_id = $2 AND published_at IS NULL
`, messageEventOutboxID(MessageEventCreated, message.ID), message.ID).Scan(&count); err != nil {
		t.Fatalf("count message outbox: %v", err)
	}
	if count != 1 {
		t.Fatalf("message outbox count = %d, want 1", count)
	}
	items, err := store.ClaimMessageEventOutbox(ctx, "message-publisher", time.Minute, 10)
	if err != nil {
		t.Fatalf("claim message outbox: %v", err)
	}
	var claimed *MessageEventOutboxItem
	for i := range items {
		if items[i].ID == messageEventOutboxID(MessageEventCreated, message.ID) {
			claimed = &items[i]
			break
		}
	}
	if claimed == nil || claimed.Event.Message.ID != message.ID {
		t.Fatalf("claimed message outbox = %#v", items)
	}
	if err := store.MarkMessageEventOutboxPublished(ctx, claimed.ID, "message-publisher"); err != nil {
		t.Fatalf("mark message event published: %v", err)
	}
}

func TestSQLJobSchedulingAndTerminalEventAreDurable(t *testing.T) {
	db := openPostgresMigrationTestDB(t)
	ctx := context.Background()
	if err := RunPostgresGooseMigrations(ctx, db, SQLDialectPostgres); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	store := NewSQLJobStoreWithDialect(db, SQLDialectPostgres)
	if err := store.Init(ctx); err != nil {
		t.Fatalf("init job store: %v", err)
	}
	now := time.Now().UTC().Add(-10 * time.Second)
	job := &Job{
		ID:        NewJobID(),
		UserID:    "job-outbox-user",
		SessionID: "job-outbox-session",
		Type:      JobTypeChat,
		Status:    JobStatusQueued,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := store.CreateJob(ctx, job); err != nil {
		t.Fatalf("create job: %v", err)
	}
	recovered, err := store.RecoverJobQueueOutbox(ctx, time.Now().UTC(), 10)
	if err != nil || recovered < 1 {
		t.Fatalf("recover queued job outbox = %d, %v", recovered, err)
	}
	items, err := store.ClaimJobQueueOutbox(ctx, "job-publisher", time.Minute, 10)
	if err != nil {
		t.Fatalf("claim job outbox: %v", err)
	}
	var claimed bool
	for _, item := range items {
		if item.Item.JobID == job.ID {
			claimed = true
			if err := store.MarkJobQueueOutboxPublished(ctx, job.ID, "job-publisher"); err != nil {
				t.Fatalf("mark job outbox published: %v", err)
			}
		}
	}
	if !claimed {
		t.Fatalf("job %s was not claimed from outbox: %#v", job.ID, items)
	}

	leaseNow := time.Now().UTC()
	acquired, err := store.AcquireJobExecutionLease(ctx, job.UserID, job.ID, "job-owner", leaseNow, leaseNow.Add(time.Minute))
	if err != nil || !acquired {
		t.Fatalf("acquire job lease = %t, %v", acquired, err)
	}
	duplicateID := NewJobEventID()
	existing := &JobEvent{
		ID: duplicateID, JobID: job.ID, UserID: job.UserID, SessionID: job.SessionID,
		Type: "progress", Event: Event{Type: "progress", JobID: job.ID}, CreatedAt: leaseNow,
	}
	if err := store.AddJobEvent(ctx, existing); err != nil {
		t.Fatalf("insert existing job event: %v", err)
	}
	terminal := &JobEvent{
		ID: duplicateID, JobID: job.ID, UserID: job.UserID, SessionID: job.SessionID,
		Type: "done", Event: Event{Type: "done", JobID: job.ID}, CreatedAt: leaseNow.Add(time.Second),
	}
	updated, err := store.TransitionOwnedJobStatusWithEvent(
		ctx, job.UserID, job.ID, "job-owner", JobStatusSucceeded, "", leaseNow.Add(time.Second), terminal,
	)
	if err == nil || updated {
		t.Fatalf("duplicate terminal event transition = %t, %v; want rollback", updated, err)
	}
	loaded, err := store.GetJob(ctx, job.UserID, job.ID)
	if err != nil || loaded.Status != JobStatusRunning {
		t.Fatalf("job status after rolled-back terminal = %#v, %v", loaded, err)
	}
	terminal.ID = NewJobEventID()
	updated, err = store.TransitionOwnedJobStatusWithEvent(
		ctx, job.UserID, job.ID, "job-owner", JobStatusSucceeded, "", leaseNow.Add(2*time.Second), terminal,
	)
	if err != nil || !updated {
		t.Fatalf("terminal transition = %t, %v", updated, err)
	}
	loaded, err = store.GetJob(ctx, job.UserID, job.ID)
	if err != nil || loaded.Status != JobStatusSucceeded {
		t.Fatalf("final job status = %#v, %v", loaded, err)
	}
}

func TestSQLJobExecutionLeaseFencesOwnersPostgres(t *testing.T) {
	db := openPostgresMigrationTestDB(t)
	ctx := context.Background()
	if err := RunPostgresGooseMigrations(ctx, db, SQLDialectPostgres); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	store := NewSQLJobStoreWithDialect(db, SQLDialectPostgres)
	if err := store.Init(ctx); err != nil {
		t.Fatalf("init job store: %v", err)
	}
	now := time.Now().UTC()
	job := &Job{ID: NewJobID(), UserID: "lease-user", SessionID: "lease-session", Type: JobTypeDeepAgent, Status: JobStatusQueued, CreatedAt: now, UpdatedAt: now}
	if err := store.CreateJob(ctx, job); err != nil {
		t.Fatalf("create job: %v", err)
	}
	acquired, err := store.AcquireJobExecutionLease(ctx, job.UserID, job.ID, "owner-1", now, now.Add(time.Minute))
	if err != nil || !acquired {
		t.Fatalf("first acquire = %t, %v", acquired, err)
	}
	loaded, err := store.GetJob(ctx, job.UserID, job.ID)
	if err != nil || loaded.ExecutionOwner != "owner-1" || loaded.ExecutionEpoch != 1 || loaded.ExecutionLeaseExpiresAt == nil {
		t.Fatalf("loaded first lease = %#v, %v", loaded, err)
	}
	acquired, err = store.AcquireJobExecutionLease(ctx, job.UserID, job.ID, "owner-2", now.Add(time.Second), now.Add(2*time.Minute))
	if err != nil || acquired {
		t.Fatalf("competing acquire = %t, %v", acquired, err)
	}
	transitioned, err := store.TransitionOwnedJobStatus(ctx, job.UserID, job.ID, "owner-2", JobStatusSucceeded, "", now.Add(2*time.Second))
	if err != nil || transitioned {
		t.Fatalf("non-owner transition = %t, %v", transitioned, err)
	}
	if err := store.ReleaseJobExecutionLease(ctx, job.UserID, job.ID, "owner-1", now.Add(3*time.Second)); err != nil {
		t.Fatalf("release first owner: %v", err)
	}
	acquired, err = store.AcquireJobExecutionLease(ctx, job.UserID, job.ID, "owner-2", now.Add(4*time.Second), now.Add(2*time.Minute))
	if err != nil || !acquired {
		t.Fatalf("takeover = %t, %v", acquired, err)
	}
	transitioned, err = store.TransitionOwnedJobStatus(ctx, job.UserID, job.ID, "owner-2", JobStatusSucceeded, "", now.Add(5*time.Second))
	if err != nil || !transitioned {
		t.Fatalf("active owner transition = %t, %v", transitioned, err)
	}
	loaded, err = store.GetJob(ctx, job.UserID, job.ID)
	if err != nil || loaded.Status != JobStatusSucceeded || loaded.ExecutionOwner != "" || loaded.ExecutionLeaseExpiresAt != nil {
		t.Fatalf("loaded terminal lease = %#v, %v", loaded, err)
	}
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

func TestSQLRuntimeOutputStoreRejectsConcurrentSessionTurnPostgres(t *testing.T) {
	db := openPostgresMigrationTestDB(t)
	ctx := context.Background()
	if err := RunPostgresGooseMigrations(ctx, db, SQLDialectPostgres); err != nil {
		t.Fatalf("RunPostgresGooseMigrations() error = %v", err)
	}
	store := NewSQLRuntimeOutputStoreWithDialect(db, SQLDialectPostgres)
	first, err := store.ReserveChatTurn(ctx, ChatTurnReservation{
		UserID:         "user-1",
		SessionID:      "session-1",
		IdempotencyKey: "turn-1",
		RunID:          "run-1",
	})
	if err != nil || !first.Reserved {
		t.Fatalf("first ReserveChatTurn() = %#v, %v", first, err)
	}
	if _, err := store.ReserveChatTurn(ctx, ChatTurnReservation{
		UserID:         "user-1",
		SessionID:      "session-1",
		IdempotencyKey: "turn-2",
		RunID:          "run-2",
	}); !errors.Is(err, ErrSessionTurnRunning) {
		t.Fatalf("concurrent ReserveChatTurn() error = %v, want %v", err, ErrSessionTurnRunning)
	}
	if err := store.UpdateChatTurnReservationStatus(ctx, "user-1", "session-1", "run-1", "succeeded"); err != nil {
		t.Fatalf("finish first reservation: %v", err)
	}
	second, err := store.ReserveChatTurn(ctx, ChatTurnReservation{
		UserID:         "user-1",
		SessionID:      "session-1",
		IdempotencyKey: "turn-2",
		RunID:          "run-2",
	})
	if err != nil || !second.Reserved {
		t.Fatalf("second ReserveChatTurn() after release = %#v, %v", second, err)
	}
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

func assertPostgresColumnExists(t *testing.T, db *sql.DB, table, column string) {
	t.Helper()
	var exists bool
	if err := db.QueryRow(`
SELECT EXISTS (
	SELECT 1
	FROM information_schema.columns
	WHERE table_schema = current_schema()
	  AND table_name = $1
	  AND column_name = $2
)`, table, column).Scan(&exists); err != nil {
		t.Fatalf("check column %s.%s: %v", table, column, err)
	}
	if !exists {
		t.Fatalf("expected column %s.%s to exist", table, column)
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
