package agentruntime

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"claude-codex/internal/harness/state"
)

type SQLSessionStore struct {
	db             *sql.DB
	dialect        SQLDialect
	messageArchive *MessageArchiveObjectStore
	sessionList    SessionListCache
	messageSeq     MessageSequenceAllocator
}

const sqlMessageColumns = `message_id, session_id, user_id, seq_no, parent_id, role, content_type, content,
	content_parts, tool_call_id, tool_name, tool_input, tool_output, tool_calls,
	prompt_tokens, completion_tokens, status, is_context_used, model_id, run_id,
	hidden, created_at, updated_at, archive_uri, archive_checksum, archived_at`

func NewSQLSessionStore(db *sql.DB) *SQLSessionStore {
	return NewSQLSessionStoreWithDialect(db, SQLDialectQuestion)
}

func NewSQLSessionStoreWithDialect(db *sql.DB, dialect SQLDialect) *SQLSessionStore {
	if dialect == "" {
		dialect = SQLDialectQuestion
	}
	return &SQLSessionStore{db: db, dialect: dialect}
}

func (s *SQLSessionStore) SetMessageArchiveObjectStore(objects ObjectStore, prefix string) {
	if s == nil || objects == nil {
		return
	}
	s.messageArchive = NewMessageArchiveObjectStore(objects, prefix)
}

func (s *SQLSessionStore) SetSessionListCache(cache SessionListCache) {
	if s == nil {
		return
	}
	s.sessionList = cache
}

func (s *SQLSessionStore) SetMessageSequenceAllocator(allocator MessageSequenceAllocator) {
	if s == nil {
		return
	}
	s.messageSeq = allocator
}

func (s *SQLSessionStore) Init(ctx context.Context) error {
	timeType := s.dialect.TimeType()
	jsonType := "TEXT"
	if s.dialect == SQLDialectPostgres {
		jsonType = "JSONB"
	}
	if err := RunSQLMigrations(ctx, s.db, s.dialect, []SQLMigration{
		{
			Version:    1,
			Statements: agentSessionSchemaStatements(timeType, jsonType),
		},
		{
			Version:    4,
			Statements: agentMessageSchemaStatements(timeType, jsonType),
		},
		{
			Version:    5,
			Statements: agentMessageAuxiliarySchemaStatements(timeType),
		},
		{
			Version: 6,
			Statements: append([]string{
				`DROP TABLE IF EXISTS agent_message_embedding_meta`,
				`DROP TABLE IF EXISTS agent_message_attachments`,
				`DROP TABLE IF EXISTS agent_messages`,
				`DROP TABLE IF EXISTS agent_sessions`,
			}, append(agentSessionSchemaStatements(timeType, jsonType), append(agentMessageSchemaStatements(timeType, jsonType), agentMessageAuxiliarySchemaStatements(timeType)...)...)...),
		},
		{
			Version: 7,
			Statements: append([]string{
				`DROP TABLE IF EXISTS agent_message_attachments`,
			}, agentMessageAttachmentSchemaStatements(timeType)...),
		},
		{
			Version:    8,
			Statements: agentMessageAttachmentProcessingSchemaStatements(s.dialect),
		},
		{
			Version:    9,
			Statements: agentMessageSoftDeleteSchemaStatements(timeType, jsonType, s.dialect),
		},
		{
			Version:    10,
			Statements: agentMessageArchiveSchemaStatements(timeType, s.dialect),
		},
		{
			Version:    11,
			Statements: agentSessionLegacyReconcileStatements(timeType, jsonType, s.dialect),
		},
	}); err != nil {
		return err
	}
	if err := ensureReadableTimeColumns(ctx, s.db, s.dialect, "agent_sessions", "created_at", "updated_at", "last_message_at"); err != nil {
		return err
	}
	if err := ensureReadableTimeColumns(ctx, s.db, s.dialect, "agent_messages", "created_at", "updated_at"); err != nil {
		return err
	}
	if err := ensureReadableTimeColumns(ctx, s.db, s.dialect, "agent_messages", "archived_at"); err != nil {
		return err
	}
	if err := s.initMessageSearchIndexes(ctx); err != nil {
		return err
	}
	return nil
}

func agentSessionSchemaStatements(timeType, jsonType string) []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS agent_sessions (
	user_id TEXT NOT NULL,
	session_id TEXT NOT NULL,
	agent_id TEXT NOT NULL DEFAULT '',
	title TEXT NOT NULL DEFAULT '',
	status INTEGER NOT NULL DEFAULT 1,
	message_count INTEGER NOT NULL DEFAULT 0,
	total_tokens BIGINT NOT NULL DEFAULT 0,
	working_dir TEXT NOT NULL DEFAULT '',
	tags ` + jsonType + ` NOT NULL DEFAULT '[]',
	description TEXT NOT NULL DEFAULT '',
	parent_id TEXT NOT NULL DEFAULT '',
	branch_point INTEGER NOT NULL DEFAULT 0,
	metadata ` + jsonType + ` NOT NULL DEFAULT '{}',
	archived INTEGER NOT NULL DEFAULT 0,
	created_at ` + timeType + ` NOT NULL,
	updated_at ` + timeType + ` NOT NULL,
	last_message_at ` + timeType + `,
	PRIMARY KEY (user_id, session_id)
)`,
		`CREATE INDEX IF NOT EXISTS idx_agent_sessions_user_status_last_message ON agent_sessions (user_id, status, last_message_at)`,
		`CREATE INDEX IF NOT EXISTS idx_agent_sessions_agent_status ON agent_sessions (agent_id, status)`,
	}
}

func agentSessionLegacyReconcileStatements(timeType, jsonType string, dialect SQLDialect) []string {
	if dialect != SQLDialectPostgres {
		return nil
	}
	statements := []string{
		`DO $$
BEGIN
	IF EXISTS (
		SELECT 1
		FROM information_schema.columns
		WHERE table_schema = current_schema()
		  AND table_name = 'agent_sessions'
		  AND column_name = 'payload'
	) AND NOT EXISTS (
		SELECT 1
		FROM information_schema.columns
		WHERE table_schema = current_schema()
		  AND table_name = 'agent_sessions'
		  AND column_name = 'status'
	) THEN
		DROP TABLE IF EXISTS agent_sessions_legacy_pre_message_module;
		ALTER TABLE agent_sessions RENAME TO agent_sessions_legacy_pre_message_module;
	END IF;
END $$`,
	}
	statements = append(statements, agentSessionSchemaStatements(timeType, jsonType)...)
	return statements
}

func agentMessageSchemaStatements(timeType, jsonType string) []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS agent_messages (
	message_id TEXT PRIMARY KEY,
	session_id TEXT NOT NULL,
	user_id TEXT NOT NULL,
	seq_no BIGINT NOT NULL,
	parent_id TEXT NOT NULL DEFAULT '',
	role TEXT NOT NULL,
	content_type TEXT NOT NULL DEFAULT 'text',
	content TEXT NOT NULL DEFAULT '',
	content_parts ` + jsonType + ` NOT NULL DEFAULT '[]',
	tool_call_id TEXT NOT NULL DEFAULT '',
	tool_name TEXT NOT NULL DEFAULT '',
	tool_input ` + jsonType + ` NOT NULL DEFAULT '{}',
	tool_output TEXT NOT NULL DEFAULT '',
	tool_calls ` + jsonType + ` NOT NULL DEFAULT '[]',
	prompt_tokens INTEGER NOT NULL DEFAULT 0,
	completion_tokens INTEGER NOT NULL DEFAULT 0,
	status INTEGER NOT NULL DEFAULT 1,
	is_context_used INTEGER NOT NULL DEFAULT 1,
	model_id TEXT NOT NULL DEFAULT '',
	run_id TEXT NOT NULL DEFAULT '',
	hidden INTEGER NOT NULL DEFAULT 0,
	created_at ` + timeType + ` NOT NULL,
	updated_at ` + timeType + ` NOT NULL
)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_agent_messages_session_active_seq ON agent_messages (user_id, session_id, seq_no) WHERE status <> 2`,
		`CREATE INDEX IF NOT EXISTS idx_agent_messages_session_created ON agent_messages (user_id, session_id, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_agent_messages_run_id ON agent_messages (run_id)`,
		`CREATE INDEX IF NOT EXISTS idx_agent_messages_user_created ON agent_messages (user_id, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_agent_messages_role_created ON agent_messages (user_id, role, created_at)`,
	}
}

func agentMessageAuxiliarySchemaStatements(timeType string) []string {
	return append(agentMessageAttachmentSchemaStatements(timeType), agentMessageEmbeddingSchemaStatements(timeType)...)
}

func agentMessageAttachmentSchemaStatements(timeType string) []string {
	return []string{
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
	created_at ` + timeType + ` NOT NULL,
	PRIMARY KEY (message_id, attachment_id)
)`,
		`CREATE INDEX IF NOT EXISTS idx_agent_message_attachments_message ON agent_message_attachments (message_id)`,
		`CREATE INDEX IF NOT EXISTS idx_agent_message_attachments_session ON agent_message_attachments (session_id)`,
		`CREATE INDEX IF NOT EXISTS idx_agent_message_attachments_user_status ON agent_message_attachments (user_id, embedding_status, created_at)`,
	}
}

func agentMessageAttachmentProcessingSchemaStatements(dialect SQLDialect) []string {
	stmt := `ALTER TABLE agent_message_attachments ADD COLUMN `
	if dialect == SQLDialectPostgres {
		stmt += `IF NOT EXISTS `
	}
	stmt += `extracted_text_key TEXT NOT NULL DEFAULT ''`
	return []string{stmt}
}

func agentMessageSoftDeleteSchemaStatements(timeType, jsonType string, dialect SQLDialect) []string {
	statements := make([]string, 0, 8)
	if dialect == SQLDialectPostgres {
		statements = append(statements, agentMessagePostgresLegacyReconcileStatements(timeType, jsonType)...)
	}
	statements = append(statements,
		`DROP INDEX IF EXISTS idx_agent_messages_session_seq`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_agent_messages_session_active_seq ON agent_messages (user_id, session_id, seq_no) WHERE status <> 2`,
	)
	return statements
}

func agentMessagePostgresLegacyReconcileStatements(timeType, jsonType string) []string {
	statements := []string{
		`DO $$
BEGIN
	IF EXISTS (
		SELECT 1
		FROM information_schema.columns
		WHERE table_schema = current_schema()
		  AND table_name = 'agent_messages'
		  AND column_name = 'message_index'
	) AND NOT EXISTS (
		SELECT 1
		FROM information_schema.columns
		WHERE table_schema = current_schema()
		  AND table_name = 'agent_messages'
		  AND column_name = 'message_id'
	) THEN
		DROP TABLE IF EXISTS agent_message_embedding_meta;
		DROP TABLE IF EXISTS agent_message_attachments;
		DROP TABLE IF EXISTS agent_messages_legacy_pre_message_module;
		ALTER TABLE agent_messages RENAME TO agent_messages_legacy_pre_message_module;
	END IF;
END $$`,
	}
	statements = append(statements, agentMessageSchemaStatements(timeType, jsonType)...)
	statements = append(statements, agentMessageAuxiliarySchemaStatements(timeType)...)
	statements = append(statements, agentMessageAttachmentProcessingSchemaStatements(SQLDialectPostgres)...)
	return statements
}

func agentMessageArchiveSchemaStatements(timeType string, dialect SQLDialect) []string {
	columnPrefix := `ALTER TABLE agent_messages ADD COLUMN `
	if dialect == SQLDialectPostgres {
		columnPrefix += `IF NOT EXISTS `
	}
	return []string{
		columnPrefix + `archive_uri TEXT NOT NULL DEFAULT ''`,
		columnPrefix + `archive_checksum TEXT NOT NULL DEFAULT ''`,
		columnPrefix + `archived_at ` + timeType,
		`CREATE INDEX IF NOT EXISTS idx_agent_messages_archive_due ON agent_messages (created_at, archived_at)`,
		`CREATE INDEX IF NOT EXISTS idx_agent_messages_archive_uri ON agent_messages (archive_uri)`,
	}
}

func agentMessageEmbeddingSchemaStatements(timeType string) []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS agent_message_embedding_meta (
	embedding_id TEXT PRIMARY KEY,
	message_id TEXT NOT NULL,
	session_id TEXT NOT NULL,
	user_id TEXT NOT NULL,
	chunk_index INTEGER NOT NULL DEFAULT 0,
	vector_id TEXT NOT NULL,
	model_version TEXT NOT NULL DEFAULT '',
	created_at ` + timeType + ` NOT NULL
)`,
		`CREATE INDEX IF NOT EXISTS idx_agent_message_embedding_message ON agent_message_embedding_meta (message_id)`,
		`CREATE INDEX IF NOT EXISTS idx_agent_message_embedding_user ON agent_message_embedding_meta (user_id)`,
	}
}

type MessageEmbeddingMeta struct {
	EmbeddingID  string
	MessageID    string
	SessionID    string
	UserID       string
	ChunkIndex   int
	VectorID     string
	ModelVersion string
	CreatedAt    time.Time
}

func (s *SQLSessionStore) Create(ctx context.Context, userID, workingDir string) (*state.Session, error) {
	session := state.NewSession(workingDir)
	session.UserID = userID
	if session.Metadata == nil {
		session.Metadata = map[string]string{}
	}
	session.Metadata["user_id_hash"] = userPathID(userID)
	if err := s.Save(ctx, userID, session); err != nil {
		return nil, err
	}
	return session, nil
}

func (s *SQLSessionStore) Get(ctx context.Context, userID, sessionID string) (*state.Session, error) {
	row := s.db.QueryRowContext(ctx, s.dialect.Bind(`
SELECT user_id, session_id, agent_id, title, status, message_count, total_tokens,
	working_dir, tags, description, parent_id, branch_point, metadata, archived,
	created_at, updated_at, last_message_at
FROM agent_sessions
WHERE user_id = ? AND session_id = ? AND status <> ?`), userID, sessionID, state.SessionStatusDeleted)
	session, err := scanSQLSession(row)
	if err != nil {
		return nil, err
	}
	messages, err := s.ListMessages(ctx, userID, sessionID)
	if err != nil {
		return nil, err
	}
	session.Messages = messages
	return session, nil
}

func (s *SQLSessionStore) List(ctx context.Context, userID string) ([]*state.Session, error) {
	if s.sessionList != nil {
		if sessions, ok, err := s.sessionList.GetSessions(ctx, userID, 0, 0); err != nil {
			return nil, err
		} else if ok {
			return sessions, nil
		}
	}
	sessions, err := s.listSessionsFromSQL(ctx, userID, 0, 0)
	if err != nil {
		return nil, err
	}
	if s.sessionList != nil {
		if err := s.sessionList.SetSessions(ctx, userID, sessions); err != nil {
			return nil, err
		}
	}
	return sessions, nil
}

func (s *SQLSessionStore) ListPage(ctx context.Context, userID string, limit, offset int) ([]*state.Session, error) {
	if limit <= 0 {
		return s.List(ctx, userID)
	}
	if offset < 0 {
		offset = 0
	}
	if s.sessionList != nil {
		if sessions, ok, err := s.sessionList.GetSessions(ctx, userID, offset, limit); err != nil {
			return nil, err
		} else if ok {
			return sessions, nil
		}
	}
	sessions, err := s.List(ctx, userID)
	if err != nil {
		return nil, err
	}
	if offset >= len(sessions) {
		return []*state.Session{}, nil
	}
	end := offset + limit
	if end > len(sessions) {
		end = len(sessions)
	}
	return sessions[offset:end], nil
}

func (s *SQLSessionStore) listSessionsFromSQL(ctx context.Context, userID string, limit, offset int) ([]*state.Session, error) {
	rows, err := s.db.QueryContext(ctx, s.dialect.Bind(`
SELECT s.user_id, s.session_id, s.agent_id,
	COALESCE(NULLIF(s.title, ''), (
		SELECT m.content
		FROM agent_messages m
		WHERE m.user_id = s.user_id
		  AND m.session_id = s.session_id
		  AND m.status <> 2
		  AND m.hidden = 0
		  AND m.role = 'user'
		  AND TRIM(m.content) <> ''
		ORDER BY m.seq_no ASC
		LIMIT 1
	), '') AS title,
	s.status, s.message_count, s.total_tokens,
	s.working_dir, s.tags, s.description, s.parent_id, s.branch_point, s.metadata, s.archived,
	s.created_at, s.updated_at, s.last_message_at
FROM agent_sessions s
WHERE s.user_id = ? AND s.status <> ?
ORDER BY s.updated_at DESC`), userID, state.SessionStatusDeleted)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*state.Session, 0)
	for rows.Next() {
		session, err := scanSQLSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, session)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	return out, nil
}

func (s *SQLSessionStore) Save(ctx context.Context, userID string, session *state.Session) error {
	if session == nil {
		return fmt.Errorf("session is required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	sessionForSQL, err := s.saveSessionMetadataTx(ctx, tx, userID, session)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := s.replaceMessages(ctx, tx, userID, sessionForSQL); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if s.sessionList != nil {
		return s.sessionList.UpsertSession(ctx, userID, sessionForSQL)
	}
	return nil
}

func (s *SQLSessionStore) SaveSessionMetadata(ctx context.Context, userID string, session *state.Session) error {
	if session == nil {
		return fmt.Errorf("session is required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	sessionForSQL, err := s.saveSessionMetadataTx(ctx, tx, userID, session)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if s.sessionList != nil {
		return s.sessionList.UpsertSession(ctx, userID, sessionForSQL)
	}
	return nil
}

func (s *SQLSessionStore) saveSessionMetadataTx(ctx context.Context, tx *sql.Tx, userID string, session *state.Session) (*state.Session, error) {
	sessionForSQL := sanitizeSessionForSQL(session)
	sessionForSQL.UserID = sanitizeSQLText(userID)
	normalizeSessionForSQL(sessionForSQL)
	updatedAt := sessionForSQL.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = time.Now().UTC()
	}
	tags, err := json.Marshal(sessionForSQL.Tags)
	if err != nil {
		return nil, err
	}
	metadata, err := json.Marshal(sessionForSQL.Metadata)
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, s.dialect.Bind(`
INSERT INTO agent_sessions (
	user_id, session_id, agent_id, title, status, message_count, total_tokens,
	working_dir, tags, description, parent_id, branch_point, metadata, archived,
	created_at, updated_at, last_message_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(user_id, session_id) DO UPDATE SET
	agent_id = excluded.agent_id,
	title = excluded.title,
	status = excluded.status,
	message_count = excluded.message_count,
	total_tokens = excluded.total_tokens,
	working_dir = excluded.working_dir,
	tags = excluded.tags,
	description = excluded.description,
	parent_id = excluded.parent_id,
	branch_point = excluded.branch_point,
	metadata = excluded.metadata,
	archived = excluded.archived,
	updated_at = excluded.updated_at,
	last_message_at = excluded.last_message_at`),
		userID,
		sessionForSQL.ID,
		sessionForSQL.AgentID,
		sessionForSQL.Title,
		sessionForSQL.Status,
		sessionForSQL.MessageCount,
		sessionForSQL.TotalTokens,
		sessionForSQL.WorkingDir,
		string(tags),
		sessionForSQL.Description,
		sessionForSQL.ParentID,
		sessionForSQL.BranchPoint,
		string(metadata),
		sqlIntFromBool(sessionForSQL.Archived),
		sqlTimeValue(sessionForSQL.StartedAt, s.dialect),
		sqlTimeValue(updatedAt, s.dialect),
		nullableSQLTimeValue(zeroTimeAsNil(sessionForSQL.LastMessageAt), s.dialect)); err != nil {
		return nil, err
	}
	return sessionForSQL, nil
}

func (s *SQLSessionStore) Delete(ctx context.Context, userID, sessionID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	if _, err := tx.ExecContext(ctx, s.dialect.Bind(`
UPDATE agent_messages
SET status = ?,
	updated_at = ?
WHERE user_id = ? AND session_id = ? AND status <> ?`),
		state.MessageStatusDeleted,
		sqlTimeValue(now, s.dialect),
		userID,
		sessionID,
		state.MessageStatusDeleted,
	); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.ExecContext(ctx, s.dialect.Bind(`
UPDATE agent_sessions
SET status = ?, archived = ?, updated_at = ?
WHERE user_id = ? AND session_id = ? AND status <> ?`),
		state.SessionStatusDeleted,
		sqlIntFromBool(true),
		sqlTimeValue(now, s.dialect),
		userID,
		sessionID,
		state.SessionStatusDeleted,
	); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if s.sessionList != nil {
		return s.sessionList.RemoveSession(ctx, userID, sessionID)
	}
	return nil
}

func (s *SQLSessionStore) DeleteUser(ctx context.Context, userID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	if _, err := tx.ExecContext(ctx, s.dialect.Bind(`
UPDATE agent_messages
SET status = ?,
	updated_at = ?
WHERE user_id = ? AND status <> ?`),
		state.MessageStatusDeleted,
		sqlTimeValue(now, s.dialect),
		userID,
		state.MessageStatusDeleted,
	); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.ExecContext(ctx, s.dialect.Bind(`
UPDATE agent_sessions
SET status = ?, archived = ?, updated_at = ?
WHERE user_id = ? AND status <> ?`),
		state.SessionStatusDeleted,
		sqlIntFromBool(true),
		sqlTimeValue(now, s.dialect),
		userID,
		state.SessionStatusDeleted,
	); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if s.sessionList != nil {
		return s.sessionList.InvalidateUser(ctx, userID)
	}
	return nil
}

func (s *SQLSessionStore) PruneBefore(ctx context.Context, cutoff time.Time) (int, error) {
	rows, err := s.db.QueryContext(ctx, s.dialect.Bind(`SELECT user_id, session_id FROM agent_sessions WHERE updated_at < ? AND status <> ?`), sqlTimeValue(cutoff, s.dialect), state.SessionStatusDeleted)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	type key struct {
		userID    string
		sessionID string
	}
	var keys []key
	for rows.Next() {
		var item key
		if err := rows.Scan(&item.userID, &item.sessionID); err != nil {
			return 0, err
		}
		keys = append(keys, item)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	for _, item := range keys {
		if err := s.Delete(ctx, item.userID, item.sessionID); err != nil {
			return 0, err
		}
	}
	return len(keys), nil
}

func (s *SQLSessionStore) ListMessages(ctx context.Context, userID, sessionID string) ([]state.Message, error) {
	rows, err := s.db.QueryContext(ctx, s.dialect.Bind(`
SELECT `+sqlMessageColumns+`
FROM agent_messages
WHERE user_id = ? AND session_id = ? AND status <> ?
ORDER BY seq_no ASC`), userID, sessionID, state.MessageStatusDeleted)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	messages := make([]state.Message, 0)
	for rows.Next() {
		message, err := scanSQLMessage(rows)
		if err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return s.hydrateSQLMessages(ctx, userID, messages)
}

func (s *SQLSessionStore) AppendMessage(ctx context.Context, userID, sessionID string, message state.Message) (state.Message, error) {
	if strings.TrimSpace(userID) == "" {
		return state.Message{}, fmt.Errorf("user ID is required")
	}
	if strings.TrimSpace(sessionID) == "" {
		return state.Message{}, fmt.Errorf("session ID is required")
	}
	releaseSeqLock, err := s.acquireMessageSeqLock(ctx, userID, sessionID)
	if err != nil {
		return state.Message{}, err
	}
	if releaseSeqLock != nil {
		defer func() { _ = releaseSeqLock(context.Background()) }()
	}
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		created, err := s.appendMessageOnce(ctx, userID, sessionID, message)
		if err == nil {
			return created, nil
		}
		lastErr = err
		if s.messageSeq != nil {
			if syncErr := s.syncMessageSeqToSQL(ctx, userID, sessionID); syncErr != nil {
				return state.Message{}, fmt.Errorf("%w; sync redis message seq: %v", err, syncErr)
			}
		}
		if s.messageSeq == nil || attempt > 0 || !isMessageSeqConflict(err) {
			break
		}
	}
	return state.Message{}, lastErr
}

func (s *SQLSessionStore) acquireMessageSeqLock(ctx context.Context, userID, sessionID string) (func(context.Context) error, error) {
	locker, ok := s.messageSeq.(MessageSequenceLocker)
	if !ok || locker == nil {
		return nil, nil
	}
	return locker.AcquireMessageSeqLock(ctx, userID, sessionID)
}

func (s *SQLSessionStore) appendMessageOnce(ctx context.Context, userID, sessionID string, message state.Message) (state.Message, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return state.Message{}, err
	}
	if strings.TrimSpace(message.ID) != "" {
		existing, ok, err := s.findMessageByID(ctx, tx, userID, sessionID, message.ID)
		if err != nil {
			_ = tx.Rollback()
			return state.Message{}, err
		}
		if ok {
			if err := tx.Commit(); err != nil {
				return state.Message{}, err
			}
			hydrated, err := s.hydrateSQLMessages(ctx, userID, []state.Message{existing})
			if err != nil {
				return state.Message{}, err
			}
			return hydrated[0], nil
		}
	}
	var sessionStartedRaw any
	if err := tx.QueryRowContext(ctx, s.dialect.Bind(`SELECT created_at FROM agent_sessions WHERE user_id = ? AND session_id = ? AND status <> ?`), userID, sessionID, state.SessionStatusDeleted).Scan(&sessionStartedRaw); err != nil {
		_ = tx.Rollback()
		if errors.Is(err, sql.ErrNoRows) {
			return state.Message{}, fmt.Errorf("session %s not found", sessionID)
		}
		return state.Message{}, err
	}
	sessionStartedAt, err := parseSQLTime(sessionStartedRaw)
	if err != nil {
		_ = tx.Rollback()
		return state.Message{}, err
	}
	nextSeq, err := s.nextMessageSeq(ctx, tx, userID, sessionID)
	if err != nil {
		_ = tx.Rollback()
		return state.Message{}, err
	}
	message = normalizeMessageForSQL(message, userID, sessionID, nextSeq, sessionStartedAt)
	if err := insertSQLMessage(ctx, tx, s.dialect, message); err != nil {
		_ = tx.Rollback()
		return state.Message{}, err
	}
	tokenDelta := int64(message.PromptTokens + message.CompletionTokens)
	titleCandidate := sessionTitleCandidateFromMessage(message)
	if _, err := tx.ExecContext(ctx, s.dialect.Bind(`
UPDATE agent_sessions
SET message_count = (
		SELECT COUNT(*) FROM agent_messages
		WHERE user_id = ? AND session_id = ? AND status <> ?
	),
	total_tokens = total_tokens + ?,
	title = CASE WHEN title = '' AND ? <> '' THEN ? ELSE title END,
	updated_at = ?,
	last_message_at = ?
WHERE user_id = ? AND session_id = ?`),
		userID,
		sessionID,
		state.MessageStatusDeleted,
		tokenDelta,
		titleCandidate,
		titleCandidate,
		sqlTimeValue(message.CreatedAt, s.dialect),
		sqlTimeValue(message.CreatedAt, s.dialect),
		userID,
		sessionID,
	); err != nil {
		_ = tx.Rollback()
		return state.Message{}, err
	}
	if err := tx.Commit(); err != nil {
		return state.Message{}, err
	}
	if s.sessionList != nil {
		_ = s.sessionList.InvalidateUser(ctx, userID)
	}
	hydrated, err := s.hydrateSQLMessages(ctx, userID, []state.Message{message})
	if err != nil {
		return state.Message{}, err
	}
	return hydrated[0], nil
}

func sessionTitleCandidateFromMessage(message state.Message) string {
	if message.Role != state.MessageRoleUser || message.Hidden {
		return ""
	}
	if text := strings.TrimSpace(message.Content); text != "" {
		return text
	}
	for _, block := range message.ContentParts {
		if block.Type == "text" {
			if text := strings.TrimSpace(firstNonEmptyString(block.Text, block.Content)); text != "" {
				return text
			}
		}
	}
	for _, block := range message.ContentBlocks {
		if block.Type == "text" {
			if text := strings.TrimSpace(firstNonEmptyString(block.Text, block.Content)); text != "" {
				return text
			}
		}
	}
	return ""
}

func (s *SQLSessionStore) nextMessageSeq(ctx context.Context, tx *sql.Tx, userID, sessionID string) (int64, error) {
	if s.messageSeq != nil {
		maxSeq, err := s.maxMessageSeqTx(ctx, tx, userID, sessionID)
		if err != nil {
			return 0, err
		}
		if err := s.reconcileMessageSeq(ctx, userID, sessionID, maxSeq); err != nil {
			return 0, err
		}
		return s.messageSeq.NextMessageSeq(ctx, userID, sessionID)
	}
	var nextSeq int64
	if err := tx.QueryRowContext(ctx, s.dialect.Bind(`SELECT COALESCE(MAX(seq_no), 0) + 1 FROM agent_messages WHERE user_id = ? AND session_id = ?`), userID, sessionID).Scan(&nextSeq); err != nil {
		return 0, err
	}
	return nextSeq, nil
}

func (s *SQLSessionStore) syncMessageSeqToSQL(ctx context.Context, userID, sessionID string) error {
	if s.messageSeq == nil {
		return nil
	}
	var maxSeq int64
	if err := s.db.QueryRowContext(ctx, s.dialect.Bind(`SELECT COALESCE(MAX(seq_no), 0) FROM agent_messages WHERE user_id = ? AND session_id = ?`), userID, sessionID).Scan(&maxSeq); err != nil {
		return err
	}
	return s.reconcileMessageSeq(ctx, userID, sessionID, maxSeq)
}

func (s *SQLSessionStore) maxMessageSeqTx(ctx context.Context, tx *sql.Tx, userID, sessionID string) (int64, error) {
	var maxSeq int64
	if err := tx.QueryRowContext(ctx, s.dialect.Bind(`SELECT COALESCE(MAX(seq_no), 0) FROM agent_messages WHERE user_id = ? AND session_id = ?`), userID, sessionID).Scan(&maxSeq); err != nil {
		return 0, err
	}
	return maxSeq, nil
}

func (s *SQLSessionStore) reconcileMessageSeq(ctx context.Context, userID, sessionID string, maxSeq int64) error {
	if reconciler, ok := s.messageSeq.(MessageSequenceReconciler); ok && reconciler != nil {
		return reconciler.ReconcileMessageSeq(ctx, userID, sessionID, maxSeq)
	}
	return s.messageSeq.SetMessageSeqFloor(ctx, userID, sessionID, maxSeq)
}

func isMessageSeqConflict(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	if strings.Contains(text, "idx_agent_messages_session_active_seq") {
		return true
	}
	return strings.Contains(text, "unique") &&
		strings.Contains(text, "user_id") &&
		strings.Contains(text, "session_id") &&
		strings.Contains(text, "seq_no")
}

func (s *SQLSessionStore) LoadSessionMessages(ctx context.Context, userID, sessionID string, opts SessionLoadOptions) ([]state.Message, error) {
	opts = normalizeSessionLoadOptions(opts)
	predicates := []string{
		"user_id = ?",
		"session_id = ?",
		"status = ?",
		"is_context_used = 1",
	}
	args := []any{userID, sessionID, state.MessageStatusNormal}
	if !opts.IncludeSystem {
		predicates = append(predicates, "role <> ?", "content_type <> ?")
		args = append(args, state.MessageRoleSystem, state.MessageContentTypeSummary)
	}
	args = append(args, opts.MaxMessages)
	rows, err := s.db.QueryContext(ctx, s.dialect.Bind(`
SELECT `+sqlMessageColumns+`
FROM agent_messages
WHERE `+strings.Join(predicates, " AND ")+`
ORDER BY seq_no DESC
LIMIT ?`), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	reversed := make([]state.Message, 0, opts.MaxMessages)
	for rows.Next() {
		message, err := scanSQLMessage(rows)
		if err != nil {
			return nil, err
		}
		reversed = append(reversed, message)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	messages := make([]state.Message, 0, len(reversed))
	for i := len(reversed) - 1; i >= 0; i-- {
		messages = append(messages, reversed[i])
	}
	return s.hydrateSQLMessages(ctx, userID, messages)
}

func (s *SQLSessionStore) LoadLatestSummaryMessage(ctx context.Context, userID, sessionID string) (state.Message, bool, error) {
	row := s.db.QueryRowContext(ctx, s.dialect.Bind(`
SELECT `+sqlMessageColumns+`
FROM agent_messages
WHERE user_id = ? AND session_id = ? AND status = ? AND is_context_used = 1
	AND (role = ? OR content_type = ?)
ORDER BY seq_no DESC
LIMIT 1`),
		userID,
		sessionID,
		state.MessageStatusNormal,
		state.MessageRoleSystem,
		state.MessageContentTypeSummary,
	)
	message, err := scanSQLMessage(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return state.Message{}, false, nil
		}
		return state.Message{}, false, err
	}
	hydrated, err := s.hydrateSQLMessages(ctx, userID, []state.Message{message})
	if err != nil {
		return state.Message{}, false, err
	}
	return hydrated[0], true, nil
}

func (s *SQLSessionStore) MarkMessagesContextUnused(ctx context.Context, userID, sessionID string, messageIDs []string) (int, error) {
	if len(messageIDs) == 0 {
		return 0, nil
	}
	ids := make([]string, 0, len(messageIDs))
	args := []any{state.MessageStatusTruncated, sqlTimeValue(time.Now().UTC(), s.dialect), userID, sessionID}
	for _, id := range messageIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		ids = append(ids, id)
		args = append(args, id)
	}
	if len(ids) == 0 {
		return 0, nil
	}
	placeholders := make([]string, len(ids))
	for i := range placeholders {
		placeholders[i] = "?"
	}
	query := s.dialect.Bind(`
UPDATE agent_messages
SET is_context_used = 0,
	status = ?,
	updated_at = ?
WHERE user_id = ?
  AND session_id = ?
  AND message_id IN (` + strings.Join(placeholders, ",") + `)
  AND status = ?`)
	args = append(args, state.MessageStatusNormal)
	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, nil
	}
	return int(affected), nil
}

func (s *SQLSessionStore) SearchMessages(ctx context.Context, userID, query string, limit, offset int) ([]MessageSearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return []MessageSearchResult{}, nil
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	pattern := "%" + escapeLikePattern(query) + "%"
	matchOperator := "LIKE"
	if s.dialect == SQLDialectPostgres {
		matchOperator = "ILIKE"
	}
	rows, err := s.db.QueryContext(ctx, s.dialect.Bind(fmt.Sprintf(`
SELECT m.message_id, m.session_id, m.seq_no, m.role, m.content, m.tool_output, m.created_at, s.title, s.description
FROM agent_messages m
JOIN agent_sessions s ON s.user_id = m.user_id AND s.session_id = m.session_id
WHERE m.user_id = ?
  AND m.status = ?
  AND s.status <> ?
  AND m.hidden = 0
  AND m.role <> 'tool'
  AND (m.content %s ? ESCAPE '\' OR m.tool_output %s ? ESCAPE '\')
ORDER BY m.created_at DESC
LIMIT ? OFFSET ?`, matchOperator, matchOperator)), userID, state.MessageStatusNormal, state.SessionStatusDeleted, pattern, pattern, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]MessageSearchResult, 0, limit)
	for rows.Next() {
		var result MessageSearchResult
		var content, toolOutput, title, description string
		var seqNo int64
		var createdRaw any
		if err := rows.Scan(&result.MessageID, &result.SessionID, &seqNo, &result.Role, &content, &toolOutput, &createdRaw, &title, &description); err != nil {
			return nil, err
		}
		createdAt, err := parseSQLTime(createdRaw)
		if err != nil {
			return nil, err
		}
		searchable := messageSearchContent(content, toolOutput, query)
		if seqNo > 0 {
			result.MessageIndex = int(seqNo - 1)
		}
		result.Content = searchable
		result.Snippet = messageSearchSnippet(searchable, query, 160)
		result.SessionTitle = firstNonEmptyString(title, description, result.SessionID)
		result.CreatedAt = createdAt
		out = append(out, result)
	}
	return out, rows.Err()
}

func (s *SQLSessionStore) HydrateMessageSearchResults(ctx context.Context, userID string, results []MessageSearchResult) ([]MessageSearchResult, error) {
	if len(results) == 0 {
		return []MessageSearchResult{}, nil
	}
	ids := make([]string, 0, len(results))
	seen := make(map[string]bool)
	for _, result := range results {
		id := strings.TrimSpace(result.MessageID)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return results, nil
	}
	placeholders := make([]string, len(ids))
	args := []any{userID, state.MessageStatusNormal, state.SessionStatusDeleted}
	for i, id := range ids {
		placeholders[i] = "?"
		args = append(args, id)
	}
	rows, err := s.db.QueryContext(ctx, s.dialect.Bind(`
SELECT m.message_id, m.session_id, m.seq_no, m.role, m.content, m.tool_output, m.created_at, s.title, s.description
FROM agent_messages m
JOIN agent_sessions s ON s.user_id = m.user_id AND s.session_id = m.session_id
WHERE m.user_id = ?
  AND m.status = ?
  AND s.status <> ?
  AND m.message_id IN (`+strings.Join(placeholders, ",")+`)`), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	hydrated := make(map[string]MessageSearchResult, len(ids))
	for rows.Next() {
		var result MessageSearchResult
		var content, toolOutput, title, description string
		var seqNo int64
		var createdRaw any
		if err := rows.Scan(&result.MessageID, &result.SessionID, &seqNo, &result.Role, &content, &toolOutput, &createdRaw, &title, &description); err != nil {
			return nil, err
		}
		createdAt, err := parseSQLTime(createdRaw)
		if err != nil {
			return nil, err
		}
		if seqNo > 0 {
			result.MessageIndex = int(seqNo - 1)
		}
		result.Content = firstNonEmptyString(content, toolOutput)
		result.Snippet = messageSearchSnippet(result.Content, "", 160)
		result.SessionTitle = firstNonEmptyString(title, description, result.SessionID)
		result.CreatedAt = createdAt
		hydrated[result.MessageID] = result
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := append([]MessageSearchResult(nil), results...)
	for i, result := range out {
		full, ok := hydrated[result.MessageID]
		if !ok {
			continue
		}
		full.Score = result.Score
		full.Source = result.Source
		if strings.TrimSpace(result.Snippet) != "" {
			full.Snippet = result.Snippet
		}
		out[i] = full
	}
	return out, nil
}

func (s *SQLSessionStore) SaveMessageEmbeddingMeta(ctx context.Context, meta MessageEmbeddingMeta) error {
	meta.EmbeddingID = strings.TrimSpace(meta.EmbeddingID)
	meta.MessageID = strings.TrimSpace(meta.MessageID)
	meta.SessionID = strings.TrimSpace(meta.SessionID)
	meta.UserID = strings.TrimSpace(meta.UserID)
	meta.VectorID = strings.TrimSpace(meta.VectorID)
	if meta.EmbeddingID == "" || meta.MessageID == "" || meta.SessionID == "" || meta.UserID == "" || meta.VectorID == "" {
		return fmt.Errorf("message embedding meta requires embedding, message, session, user, and vector IDs")
	}
	if meta.CreatedAt.IsZero() {
		meta.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx, s.dialect.Bind(`
INSERT INTO agent_message_embedding_meta (
	embedding_id, message_id, session_id, user_id, chunk_index, vector_id, model_version, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(embedding_id) DO UPDATE SET
	vector_id = excluded.vector_id,
	model_version = excluded.model_version,
	created_at = excluded.created_at`),
		meta.EmbeddingID,
		meta.MessageID,
		meta.SessionID,
		meta.UserID,
		meta.ChunkIndex,
		meta.VectorID,
		meta.ModelVersion,
		sqlTimeValue(meta.CreatedAt, s.dialect),
	)
	return err
}

func (s *SQLSessionStore) initMessageSearchIndexes(ctx context.Context) error {
	if s.dialect != SQLDialectPostgres {
		return nil
	}
	for _, stmt := range []string{
		`CREATE EXTENSION IF NOT EXISTS pg_trgm`,
		`CREATE INDEX IF NOT EXISTS idx_agent_messages_content_trgm ON agent_messages USING GIN (content gin_trgm_ops)`,
		`CREATE INDEX IF NOT EXISTS idx_agent_messages_tool_output_trgm ON agent_messages USING GIN (tool_output gin_trgm_ops)`,
	} {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLSessionStore) BackfillMessages(ctx context.Context) error {
	return nil
}

func (s *SQLSessionStore) replaceMessages(ctx context.Context, tx *sql.Tx, userID string, session *state.Session) error {
	normalized := make([]state.Message, 0, len(session.Messages))
	for index, message := range session.Messages {
		message = normalizeMessageForSQL(message, userID, session.ID, int64(index+1), session.StartedAt)
		normalized = append(normalized, message)
	}
	if err := softDeleteSessionMessagesForReplace(ctx, tx, s.dialect, userID, session.ID); err != nil {
		return err
	}
	for _, message := range normalized {
		if err := upsertSQLMessage(ctx, tx, s.dialect, message); err != nil {
			return err
		}
	}
	return nil
}

func softDeleteSessionMessagesForReplace(ctx context.Context, tx *sql.Tx, dialect SQLDialect, userID, sessionID string) error {
	_, err := tx.ExecContext(ctx, dialect.Bind(`
UPDATE agent_messages
SET status = ?,
	updated_at = ?
WHERE user_id = ?
  AND session_id = ?
  AND status <> ?`),
		state.MessageStatusDeleted,
		sqlTimeValue(time.Now().UTC(), dialect),
		userID,
		sessionID,
		state.MessageStatusDeleted,
	)
	return err
}

func (s *SQLSessionStore) findMessageByID(ctx context.Context, tx *sql.Tx, userID, sessionID, messageID string) (state.Message, bool, error) {
	row := tx.QueryRowContext(ctx, s.dialect.Bind(`
SELECT `+sqlMessageColumns+`
FROM agent_messages
WHERE user_id = ? AND session_id = ? AND message_id = ?`), userID, sessionID, messageID)
	message, err := scanSQLMessage(row)
	if err == nil {
		return message, true, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return state.Message{}, false, nil
	}
	return state.Message{}, false, err
}

func insertSQLMessage(ctx context.Context, tx *sql.Tx, dialect SQLDialect, message state.Message) error {
	return writeSQLMessage(ctx, tx, dialect, message, false)
}

func upsertSQLMessage(ctx context.Context, tx *sql.Tx, dialect SQLDialect, message state.Message) error {
	return writeSQLMessage(ctx, tx, dialect, message, true)
}

func writeSQLMessage(ctx context.Context, tx *sql.Tx, dialect SQLDialect, message state.Message, upsert bool) error {
	contentParts := message.ContentParts
	if len(contentParts) == 0 && len(message.ContentBlocks) > 0 {
		contentParts = message.ContentBlocks
	}
	if len(contentParts) > 0 {
		contentParts = normalizeMessageContentParts(contentParts)
		message.ContentParts = contentParts
		message.ContentBlocks = contentParts
	}
	contentPartsJSON, err := json.Marshal(contentParts)
	if err != nil {
		return err
	}
	toolInput := message.ToolInput
	if len(toolInput) == 0 {
		toolInput = json.RawMessage(`{}`)
	}
	toolCalls, err := json.Marshal(message.ToolCalls)
	if err != nil {
		return err
	}
	query := `
INSERT INTO agent_messages (
	message_id, session_id, user_id, seq_no, parent_id, role, content_type, content,
	content_parts, tool_call_id, tool_name, tool_input, tool_output, tool_calls,
	prompt_tokens, completion_tokens, status, is_context_used, model_id, run_id,
	hidden, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	if upsert {
		query += `
ON CONFLICT(message_id) DO UPDATE SET
	session_id = excluded.session_id,
	user_id = excluded.user_id,
	seq_no = excluded.seq_no,
	parent_id = excluded.parent_id,
	role = excluded.role,
	content_type = excluded.content_type,
	content = excluded.content,
	content_parts = excluded.content_parts,
	tool_call_id = excluded.tool_call_id,
	tool_name = excluded.tool_name,
	tool_input = excluded.tool_input,
	tool_output = excluded.tool_output,
	tool_calls = excluded.tool_calls,
	prompt_tokens = excluded.prompt_tokens,
	completion_tokens = excluded.completion_tokens,
	status = excluded.status,
	is_context_used = excluded.is_context_used,
	model_id = excluded.model_id,
	run_id = excluded.run_id,
	hidden = excluded.hidden,
	updated_at = excluded.updated_at`
	}
	_, err = tx.ExecContext(ctx, dialect.Bind(query),
		message.ID,
		message.SessionID,
		message.UserID,
		message.SeqNo,
		message.ParentID,
		message.Role,
		message.ContentType,
		message.Content,
		string(contentPartsJSON),
		message.ToolCallID,
		message.ToolName,
		sanitizeSQLText(string(toolInput)),
		message.ToolOutput,
		sanitizeSQLText(string(toolCalls)),
		message.PromptTokens,
		message.CompletionTokens,
		message.Status,
		sqlIntFromBool(message.IsContextUsed),
		message.ModelID,
		message.RunID,
		sqlIntFromBool(message.Hidden),
		sqlTimeValue(message.CreatedAt, dialect),
		sqlTimeValue(message.UpdatedAt, dialect),
	)
	if err != nil {
		return err
	}
	return insertSQLMessageAttachments(ctx, tx, dialect, message)
}

func insertSQLMessageAttachments(ctx context.Context, tx *sql.Tx, dialect SQLDialect, message state.Message) error {
	refs := messageAttachmentRefs(message)
	if len(refs) == 0 {
		return nil
	}
	for _, ref := range refs {
		artifact, err := findSQLAttachmentArtifact(ctx, tx, dialect, message.UserID, ref.ID)
		if err != nil {
			return err
		}
		ref = mergeMessageAttachmentMetadata(ref, artifact)
		ref.MessageID = message.ID
		ref.SessionID = firstNonEmptyString(message.SessionID, ref.SessionID)
		ref.UserID = firstNonEmptyString(message.UserID, ref.UserID)
		_, err = tx.ExecContext(ctx, dialect.Bind(`
INSERT INTO agent_message_attachments (
	attachment_id, message_id, session_id, user_id, file_type, mime_type,
	file_name, file_size, storage_key, thumbnail_key, extracted_text_key, embedding_status, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(message_id, attachment_id) DO UPDATE SET
	file_type = excluded.file_type,
	mime_type = excluded.mime_type,
	file_name = excluded.file_name,
	file_size = excluded.file_size,
	storage_key = excluded.storage_key,
	thumbnail_key = excluded.thumbnail_key,
	extracted_text_key = excluded.extracted_text_key,
	embedding_status = excluded.embedding_status`),
			ref.ID,
			ref.MessageID,
			ref.SessionID,
			ref.UserID,
			ref.FileType,
			ref.MimeType,
			ref.FileName,
			ref.FileSize,
			ref.StorageKey,
			ref.ThumbnailKey,
			ref.ExtractedTextKey,
			ref.EmbeddingStatus,
			sqlTimeValue(ref.CreatedAt, dialect),
		)
		if err != nil {
			return err
		}
	}
	return nil
}

func findSQLAttachmentArtifact(ctx context.Context, tx *sql.Tx, dialect SQLDialect, userID, attachmentID string) (*Artifact, error) {
	userID = strings.TrimSpace(userID)
	attachmentID = strings.TrimSpace(attachmentID)
	if userID == "" || attachmentID == "" {
		return nil, nil
	}
	row := tx.QueryRowContext(ctx, dialect.Bind(`
SELECT artifact_id, kind, user_id, session_id, job_id, object_key, filename, content_type, size_bytes, created_at, deleted_at
FROM agent_artifacts
WHERE user_id = ? AND artifact_id = ? AND kind = ? AND deleted_at IS NULL`), userID, attachmentID, AssetKindAttachment)
	artifact, err := scanArtifactRows(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil && isMissingSQLTableError(err) {
		return nil, nil
	}
	return artifact, err
}

func isMissingSQLTableError(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "no such table") ||
		strings.Contains(text, "undefined_table") ||
		strings.Contains(text, "sqlstate 42p01") ||
		(strings.Contains(text, "relation") && strings.Contains(text, "does not exist"))
}

func (s *SQLSessionStore) hydrateMessageAttachments(ctx context.Context, userID string, messages []state.Message) ([]state.Message, error) {
	if len(messages) == 0 {
		return messages, nil
	}
	ids := make([]string, 0, len(messages))
	indexByID := make(map[string]int, len(messages))
	for i, message := range messages {
		id := strings.TrimSpace(message.ID)
		if id == "" {
			continue
		}
		messages[i].Attachments = nil
		ids = append(ids, id)
		indexByID[id] = i
	}
	if len(ids) == 0 {
		return messages, nil
	}
	placeholders := make([]string, len(ids))
	args := make([]any, 0, len(ids)+1)
	userPredicate := ""
	if strings.TrimSpace(userID) != "" {
		userPredicate = "user_id = ? AND "
		args = append(args, userID)
	}
	for i, id := range ids {
		placeholders[i] = "?"
		args = append(args, id)
	}
	rows, err := s.db.QueryContext(ctx, s.dialect.Bind(`
SELECT attachment_id, message_id, session_id, user_id, file_type, mime_type,
	file_name, file_size, storage_key, thumbnail_key, extracted_text_key, embedding_status, created_at
FROM agent_message_attachments
WHERE `+userPredicate+`message_id IN (`+strings.Join(placeholders, ",")+`)
ORDER BY created_at ASC`), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		attachment, err := scanSQLMessageAttachment(rows)
		if err != nil {
			return nil, err
		}
		index, ok := indexByID[attachment.MessageID]
		if !ok {
			continue
		}
		messages[index].Attachments = append(messages[index].Attachments, attachment)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return messages, nil
}

func (s *SQLSessionStore) ListPendingMessageAttachments(ctx context.Context, userID string, limit int) ([]state.MessageAttachment, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, s.dialect.Bind(`
SELECT attachment_id, message_id, session_id, user_id, file_type, mime_type,
	file_name, file_size, storage_key, thumbnail_key, extracted_text_key, embedding_status, created_at
FROM agent_message_attachments
WHERE user_id = ? AND embedding_status = ?
ORDER BY created_at ASC
LIMIT ?`), userID, state.MessageAttachmentEmbeddingPending, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]state.MessageAttachment, 0, limit)
	for rows.Next() {
		attachment, err := scanSQLMessageAttachment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, attachment)
	}
	return out, rows.Err()
}

func (s *SQLSessionStore) ListPendingMessageAttachmentsForProcessing(ctx context.Context, limit int) ([]state.MessageAttachment, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, s.dialect.Bind(`
SELECT attachment_id, message_id, session_id, user_id, file_type, mime_type,
	file_name, file_size, storage_key, thumbnail_key, extracted_text_key, embedding_status, created_at
FROM agent_message_attachments
WHERE embedding_status = ?
ORDER BY created_at ASC
LIMIT ?`), state.MessageAttachmentEmbeddingPending, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]state.MessageAttachment, 0, limit)
	for rows.Next() {
		attachment, err := scanSQLMessageAttachment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, attachment)
	}
	return out, rows.Err()
}

func (s *SQLSessionStore) UpdateMessageAttachmentProcessing(ctx context.Context, userID, messageID, attachmentID string, status int, thumbnailKey, extractedTextKey string) error {
	if status == 0 {
		status = state.MessageAttachmentEmbeddingPending
	}
	_, err := s.db.ExecContext(ctx, s.dialect.Bind(`
UPDATE agent_message_attachments
SET embedding_status = ?,
	thumbnail_key = ?,
	extracted_text_key = ?
WHERE user_id = ?
  AND message_id = ?
  AND attachment_id = ?`), status, strings.TrimSpace(thumbnailKey), strings.TrimSpace(extractedTextKey), userID, messageID, attachmentID)
	return err
}

func scanSQLMessageAttachment(scanner sqlScanner) (state.MessageAttachment, error) {
	var attachment state.MessageAttachment
	var createdRaw any
	if err := scanner.Scan(
		&attachment.ID,
		&attachment.MessageID,
		&attachment.SessionID,
		&attachment.UserID,
		&attachment.FileType,
		&attachment.MimeType,
		&attachment.FileName,
		&attachment.FileSize,
		&attachment.StorageKey,
		&attachment.ThumbnailKey,
		&attachment.ExtractedTextKey,
		&attachment.EmbeddingStatus,
		&createdRaw,
	); err != nil {
		return state.MessageAttachment{}, err
	}
	createdAt, err := parseSQLTime(createdRaw)
	if err != nil {
		return state.MessageAttachment{}, err
	}
	attachment.CreatedAt = createdAt
	return attachment, nil
}

type sqlScanner interface {
	Scan(dest ...any) error
}

func scanSQLSession(scanner sqlScanner) (*state.Session, error) {
	var session state.Session
	var tagsRaw, metadataRaw string
	var archived int
	var startedRaw, updatedRaw, lastRaw any
	if err := scanner.Scan(
		&session.UserID,
		&session.ID,
		&session.AgentID,
		&session.Title,
		&session.Status,
		&session.MessageCount,
		&session.TotalTokens,
		&session.WorkingDir,
		&tagsRaw,
		&session.Description,
		&session.ParentID,
		&session.BranchPoint,
		&metadataRaw,
		&archived,
		&startedRaw,
		&updatedRaw,
		&lastRaw,
	); err != nil {
		return nil, err
	}
	startedAt, err := parseSQLTime(startedRaw)
	if err != nil {
		return nil, err
	}
	updatedAt, err := parseSQLTime(updatedRaw)
	if err != nil {
		return nil, err
	}
	lastMessageAt, err := parseSQLTime(lastRaw)
	if err != nil {
		return nil, err
	}
	session.StartedAt = startedAt
	session.UpdatedAt = updatedAt
	session.LastMessageAt = lastMessageAt
	session.Archived = archived != 0
	if session.Status == 0 {
		session.Status = state.SessionStatusActive
	}
	if strings.TrimSpace(tagsRaw) != "" {
		_ = json.Unmarshal([]byte(tagsRaw), &session.Tags)
	}
	if strings.TrimSpace(metadataRaw) != "" {
		_ = json.Unmarshal([]byte(metadataRaw), &session.Metadata)
	}
	if session.Metadata == nil {
		session.Metadata = map[string]string{}
	}
	return &session, nil
}

func scanSQLMessage(scanner sqlScanner) (state.Message, error) {
	var message state.Message
	var contentPartsRaw, toolInputRaw, toolCallsRaw string
	var contextUsed, hidden int
	var createdRaw, updatedRaw, archivedRaw any
	if err := scanner.Scan(
		&message.ID,
		&message.SessionID,
		&message.UserID,
		&message.SeqNo,
		&message.ParentID,
		&message.Role,
		&message.ContentType,
		&message.Content,
		&contentPartsRaw,
		&message.ToolCallID,
		&message.ToolName,
		&toolInputRaw,
		&message.ToolOutput,
		&toolCallsRaw,
		&message.PromptTokens,
		&message.CompletionTokens,
		&message.Status,
		&contextUsed,
		&message.ModelID,
		&message.RunID,
		&hidden,
		&createdRaw,
		&updatedRaw,
		&message.ArchiveURI,
		&message.ArchiveChecksum,
		&archivedRaw,
	); err != nil {
		return state.Message{}, err
	}
	createdAt, err := parseSQLTime(createdRaw)
	if err != nil {
		return state.Message{}, err
	}
	updatedAt, err := parseSQLTime(updatedRaw)
	if err != nil {
		return state.Message{}, err
	}
	archivedAt, err := parseNullableSQLTime(archivedRaw)
	if err != nil {
		return state.Message{}, err
	}
	message.CreatedAt = createdAt
	message.UpdatedAt = updatedAt
	message.ArchivedAt = archivedAt
	message.IsContextUsed = contextUsed != 0
	message.Hidden = hidden != 0
	if strings.TrimSpace(contentPartsRaw) != "" {
		_ = json.Unmarshal([]byte(contentPartsRaw), &message.ContentParts)
		message.ContentBlocks = message.ContentParts
	}
	if strings.TrimSpace(toolInputRaw) != "" && toolInputRaw != "{}" {
		message.ToolInput = json.RawMessage(toolInputRaw)
	}
	if strings.TrimSpace(toolCallsRaw) != "" {
		_ = json.Unmarshal([]byte(toolCallsRaw), &message.ToolCalls)
	}
	return message, nil
}

func normalizeSessionForSQL(session *state.Session) {
	if session.Status == 0 {
		session.Status = state.SessionStatusActive
	}
	if session.Archived {
		session.Status = state.SessionStatusArchived
	}
	if session.StartedAt.IsZero() {
		session.StartedAt = time.Now().UTC()
	}
	if session.UpdatedAt.IsZero() {
		session.UpdatedAt = session.StartedAt
	}
	session.MessageCount = len(session.Messages)
	session.TotalTokens = int64(session.Usage.TotalTokens)
	if len(session.Messages) > 0 {
		last := session.Messages[len(session.Messages)-1]
		if !last.CreatedAt.IsZero() {
			session.LastMessageAt = last.CreatedAt
		}
	}
}

func normalizeMessageForSQL(message state.Message, userID, sessionID string, seqNo int64, sessionStartedAt time.Time) state.Message {
	if message.ID == "" {
		message.ID = stateMessageID()
	}
	message.UserID = userID
	message.SessionID = sessionID
	message.SeqNo = seqNo
	if message.Status == 0 {
		message.Status = state.MessageStatusNormal
	}
	if message.ContentType == "" {
		message.ContentType = inferSQLMessageContentType(message)
	}
	if len(message.ContentParts) == 0 && len(message.ContentBlocks) > 0 {
		message.ContentParts = message.ContentBlocks
	}
	if len(message.ContentParts) > 0 {
		message.ContentParts = normalizeMessageContentParts(message.ContentParts)
		message.ContentBlocks = message.ContentParts
	}
	if message.CreatedAt.IsZero() {
		base := sessionStartedAt
		if base.IsZero() {
			base = time.Now().UTC()
		}
		message.CreatedAt = base.Add(time.Duration(seqNo-1) * time.Millisecond)
	}
	if message.UpdatedAt.IsZero() {
		message.UpdatedAt = message.CreatedAt
	}
	if message.Status == state.MessageStatusNormal {
		message.IsContextUsed = true
	}
	return sanitizeMessageForSQL(message)
}

func inferSQLMessageContentType(message state.Message) string {
	if len(message.ContentParts) > 0 || len(message.ContentBlocks) > 0 {
		return state.MessageContentTypeMultipart
	}
	if message.Role == state.MessageRoleTool || message.ToolCallID != "" || message.ToolOutput != "" {
		return state.MessageContentTypeToolResult
	}
	if len(message.ToolCalls) > 0 {
		return state.MessageContentTypeToolCall
	}
	return state.MessageContentTypeText
}

func stateMessageID() string {
	return uuid.NewString()
}

func sqlIntFromBool(value bool) int {
	if value {
		return 1
	}
	return 0
}

func zeroTimeAsNil(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	return &value
}

func sanitizeSessionForSQL(session *state.Session) *state.Session {
	if session == nil {
		return nil
	}
	clone := *session
	clone.ID = sanitizeSQLText(clone.ID)
	clone.UserID = sanitizeSQLText(clone.UserID)
	clone.AgentID = sanitizeSQLText(clone.AgentID)
	clone.Title = sanitizeSQLText(clone.Title)
	clone.WorkingDir = sanitizeSQLText(clone.WorkingDir)
	clone.Description = sanitizeSQLText(clone.Description)
	clone.ParentID = sanitizeSQLText(clone.ParentID)
	clone.Tags = sanitizeStringSliceForSQL(clone.Tags)
	if clone.Metadata != nil {
		clone.Metadata = sanitizeStringMapForSQL(clone.Metadata)
	}
	if session.Messages != nil {
		clone.Messages = make([]state.Message, len(session.Messages))
		for i, message := range session.Messages {
			clone.Messages[i] = sanitizeMessageForSQL(message)
		}
	}
	return &clone
}

func sanitizeMessageForSQL(message state.Message) state.Message {
	message.ID = sanitizeSQLText(message.ID)
	message.SessionID = sanitizeSQLText(message.SessionID)
	message.UserID = sanitizeSQLText(message.UserID)
	message.ParentID = sanitizeSQLText(message.ParentID)
	message.Role = sanitizeSQLText(message.Role)
	message.ContentType = sanitizeSQLText(message.ContentType)
	message.Content = sanitizeSQLText(message.Content)
	message.ToolName = sanitizeSQLText(message.ToolName)
	message.ToolCallID = sanitizeSQLText(message.ToolCallID)
	message.ToolOutput = sanitizeSQLText(message.ToolOutput)
	message.ModelID = sanitizeSQLText(message.ModelID)
	message.RunID = sanitizeSQLText(message.RunID)
	message.ArchiveURI = sanitizeSQLText(message.ArchiveURI)
	message.ArchiveChecksum = sanitizeSQLText(message.ArchiveChecksum)
	for i := range message.ToolCalls {
		message.ToolCalls[i].ID = sanitizeSQLText(message.ToolCalls[i].ID)
		message.ToolCalls[i].Name = sanitizeSQLText(message.ToolCalls[i].Name)
	}
	return message
}

func sanitizeStringSliceForSQL(values []string) []string {
	if values == nil {
		return nil
	}
	out := make([]string, len(values))
	for i, value := range values {
		out[i] = sanitizeSQLText(value)
	}
	return out
}

func sanitizeStringMapForSQL(values map[string]string) map[string]string {
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[sanitizeSQLText(key)] = sanitizeSQLText(value)
	}
	return out
}

func sanitizeSQLText(value string) string {
	return strings.ToValidUTF8(value, "\uFFFD")
}

type SQLMemoryService struct {
	db      *sql.DB
	dialect SQLDialect
}

func NewSQLMemoryService(db *sql.DB) *SQLMemoryService {
	return NewSQLMemoryServiceWithDialect(db, SQLDialectQuestion)
}

func NewSQLMemoryServiceWithDialect(db *sql.DB, dialect SQLDialect) *SQLMemoryService {
	if dialect == "" {
		dialect = SQLDialectQuestion
	}
	return &SQLMemoryService{db: db, dialect: dialect}
}

func (m *SQLMemoryService) Init(ctx context.Context) error {
	if err := m.renameLegacyMemoryItemsTable(ctx); err != nil {
		return err
	}
	for _, stmt := range []string{
		`DROP TABLE IF EXISTS agent_memories`,
		`CREATE TABLE IF NOT EXISTS agent_memory (
	id TEXT PRIMARY KEY,
	user_id TEXT NOT NULL,
	session_id TEXT,
	namespace TEXT NOT NULL DEFAULT 'default',
	kind TEXT NOT NULL,
	level TEXT NOT NULL DEFAULT 'atomic',
	category TEXT NOT NULL DEFAULT 'fact',
	tags TEXT NOT NULL DEFAULT '',
	source TEXT NOT NULL,
	source_refs TEXT NOT NULL DEFAULT '',
	visibility TEXT NOT NULL,
	status TEXT NOT NULL DEFAULT 'active',
	content TEXT NOT NULL,
	raw_hash TEXT NOT NULL DEFAULT '',
	confidence DOUBLE PRECISION NOT NULL DEFAULT 0.7,
	weight DOUBLE PRECISION NOT NULL DEFAULT 0.65,
	access_count BIGINT NOT NULL DEFAULT 0,
	parent_id TEXT NOT NULL DEFAULT '',
	related_ids TEXT NOT NULL DEFAULT '',
	conflict_ids TEXT NOT NULL DEFAULT '',
	supersedes_id TEXT NOT NULL DEFAULT '',
	superseded_by_id TEXT NOT NULL DEFAULT '',
	last_injected_at ` + m.dialect.TimeType() + `,
	metadata TEXT NOT NULL DEFAULT '{}',
	expires_at ` + m.dialect.TimeType() + `,
	created_at ` + m.dialect.TimeType() + ` NOT NULL,
	updated_at ` + m.dialect.TimeType() + ` NOT NULL
)`,
		`ALTER TABLE agent_memory ADD COLUMN IF NOT EXISTS level TEXT NOT NULL DEFAULT 'atomic'`,
		`ALTER TABLE agent_memory ADD COLUMN IF NOT EXISTS namespace TEXT NOT NULL DEFAULT 'default'`,
		`ALTER TABLE agent_memory ADD COLUMN IF NOT EXISTS category TEXT NOT NULL DEFAULT 'fact'`,
		`ALTER TABLE agent_memory ADD COLUMN IF NOT EXISTS tags TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE agent_memory ADD COLUMN IF NOT EXISTS source_refs TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE agent_memory ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'active'`,
		`ALTER TABLE agent_memory ADD COLUMN IF NOT EXISTS raw_hash TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE agent_memory ADD COLUMN IF NOT EXISTS confidence DOUBLE PRECISION NOT NULL DEFAULT 0.7`,
		`ALTER TABLE agent_memory ADD COLUMN IF NOT EXISTS weight DOUBLE PRECISION NOT NULL DEFAULT 0.65`,
		`ALTER TABLE agent_memory ADD COLUMN IF NOT EXISTS access_count BIGINT NOT NULL DEFAULT 0`,
		`ALTER TABLE agent_memory ADD COLUMN IF NOT EXISTS parent_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE agent_memory ADD COLUMN IF NOT EXISTS related_ids TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE agent_memory ADD COLUMN IF NOT EXISTS conflict_ids TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE agent_memory ADD COLUMN IF NOT EXISTS supersedes_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE agent_memory ADD COLUMN IF NOT EXISTS superseded_by_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE agent_memory ADD COLUMN IF NOT EXISTS last_injected_at ` + m.dialect.TimeType(),
		`ALTER TABLE agent_memory ADD COLUMN IF NOT EXISTS metadata TEXT NOT NULL DEFAULT '{}'`,
		`ALTER TABLE agent_memory ADD COLUMN IF NOT EXISTS expires_at ` + m.dialect.TimeType(),
		`CREATE INDEX IF NOT EXISTS idx_agent_memory_user_created ON agent_memory (user_id, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_agent_memory_user_session ON agent_memory (user_id, session_id, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_agent_memory_user_weight ON agent_memory (user_id, status, visibility, weight)`,
		`CREATE INDEX IF NOT EXISTS idx_agent_memory_user_hash ON agent_memory (user_id, raw_hash)`,
		`CREATE INDEX IF NOT EXISTS idx_agent_memory_user_level ON agent_memory (user_id, level, status)`,
		`CREATE INDEX IF NOT EXISTS idx_agent_memory_user_namespace ON agent_memory (user_id, namespace, status)`,
		`CREATE TABLE IF NOT EXISTS agent_memory_settings (
	user_id TEXT PRIMARY KEY,
	payload TEXT NOT NULL,
	updated_at ` + m.dialect.TimeType() + ` NOT NULL
)`,
	} {
		if _, err := m.db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	if err := ensureReadableTimeColumns(ctx, m.db, m.dialect, "agent_memory", "created_at", "updated_at", "expires_at", "last_injected_at"); err != nil {
		return err
	}
	return ensureReadableTimeColumns(ctx, m.db, m.dialect, "agent_memory_settings", "updated_at")
}

func (m *SQLMemoryService) renameLegacyMemoryItemsTable(ctx context.Context) error {
	hasCurrent, err := m.sqlTableExists(ctx, "agent_memory")
	if err != nil {
		return err
	}
	if hasCurrent {
		return nil
	}
	hasLegacy, err := m.sqlTableExists(ctx, "agent_memory_items")
	if err != nil {
		return err
	}
	if !hasLegacy {
		return nil
	}
	_, err = m.db.ExecContext(ctx, `ALTER TABLE agent_memory_items RENAME TO agent_memory`)
	return err
}

func (m *SQLMemoryService) sqlTableExists(ctx context.Context, table string) (bool, error) {
	var name string
	var err error
	if m.dialect == SQLDialectPostgres {
		err = m.db.QueryRowContext(ctx, `
SELECT table_name
FROM information_schema.tables
WHERE table_schema = current_schema()
  AND table_name = $1`, table).Scan(&name)
	} else {
		err = m.db.QueryRowContext(ctx, `SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&name)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return name == table, nil
}

func (m *SQLMemoryService) LoadContext(ctx context.Context, userID string, session *state.Session) (string, error) {
	if session == nil {
		return "", nil
	}
	items, err := m.ListMemoryItems(ctx, userID, MemoryItemFilter{
		Status: MemoryStatusActive,
	})
	if err != nil {
		return "", err
	}
	if len(items) > 0 {
		query := lastVisibleUserMessage(session)
		selected := selectMemoryItemsForSessionContext(items, query, session.ID, 12)
		now := time.Now().UTC()
		for i := range selected {
			selected[i] = recordMemoryInjection(selected[i], session.ID, query, now)
			if _, err := m.UpdateMemoryItem(ctx, userID, selected[i]); err != nil {
				return "", err
			}
		}
		return "# Memory\n\n" + formatMemoryItems(selected), nil
	}
	content, err := m.LoadSessionMemory(ctx, userID, session.ID)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(content) == "" {
		return "", nil
	}
	return "# Session memory\n\n" + content, nil
}

func (m *SQLMemoryService) LoadUserMemory(context.Context, string) (string, error) {
	return "", nil
}

func (m *SQLMemoryService) LoadSessionMemory(ctx context.Context, userID, sessionID string) (string, error) {
	items, err := m.ListMemoryItems(ctx, userID, MemoryItemFilter{
		SessionID: sessionID,
		Kind:      MemoryKindSession,
	})
	if err != nil {
		return "", err
	}
	if len(items) > 0 {
		return formatMemoryItems(items), nil
	}
	return "", nil
}

func (m *SQLMemoryService) AfterTurn(ctx context.Context, userID string, session *state.Session) error {
	if session == nil {
		return nil
	}
	candidates := extractMemoryItems(userID, session)
	if len(candidates) == 0 {
		return nil
	}
	existing, err := m.ListMemoryItems(ctx, userID, MemoryItemFilter{})
	if err != nil {
		return err
	}
	for _, candidate := range candidates {
		var conflictUpdates []MemoryItem
		candidate, conflictUpdates = applyMemoryConflictResolution(existing, candidate)
		for _, update := range conflictUpdates {
			if _, err := m.UpdateMemoryItem(ctx, userID, update); err != nil {
				return err
			}
			existing = append(existing, update)
		}
		item := upsertMemoryItem(existing, candidate)
		if _, err := m.upsertMemoryItem(ctx, item); err != nil {
			return err
		}
		existing = append(existing, item)
	}
	return nil
}

func (m *SQLMemoryService) DeleteSession(ctx context.Context, userID, sessionID string) error {
	_, err := m.db.ExecContext(ctx, m.dialect.Bind(`DELETE FROM agent_memory WHERE user_id = ? AND session_id = ?`), userID, sessionID)
	return err
}

func (m *SQLMemoryService) DeleteUser(ctx context.Context, userID string) error {
	if _, err := m.db.ExecContext(ctx, m.dialect.Bind(`DELETE FROM agent_memory WHERE user_id = ?`), userID); err != nil {
		return err
	}
	if _, err := m.db.ExecContext(ctx, m.dialect.Bind(`DELETE FROM agent_memory_settings WHERE user_id = ?`), userID); err != nil {
		return err
	}
	return nil
}

func (m *SQLMemoryService) GetMemorySettings(ctx context.Context, userID string) (MemorySettings, error) {
	var payload string
	err := m.db.QueryRowContext(ctx, m.dialect.Bind(`SELECT payload FROM agent_memory_settings WHERE user_id = ?`), userID).Scan(&payload)
	if err == sql.ErrNoRows {
		return defaultMemorySettings(), nil
	}
	if err != nil {
		return MemorySettings{}, err
	}
	var settings MemorySettings
	if err := json.Unmarshal([]byte(payload), &settings); err != nil {
		return MemorySettings{}, err
	}
	return normalizeMemorySettings(settings), nil
}

func (m *SQLMemoryService) UpdateMemorySettings(ctx context.Context, userID string, settings MemorySettings) (MemorySettings, error) {
	settings.UpdatedAt = time.Now().UTC()
	settings = normalizeMemorySettings(settings)
	payload, err := json.Marshal(settings)
	if err != nil {
		return MemorySettings{}, err
	}
	_, err = m.db.ExecContext(ctx, m.dialect.Bind(`
INSERT INTO agent_memory_settings (user_id, payload, updated_at)
VALUES (?, ?, ?)
ON CONFLICT(user_id) DO UPDATE SET
	payload = excluded.payload,
	updated_at = excluded.updated_at`), userID, string(payload), sqlTimeValue(settings.UpdatedAt, m.dialect))
	return settings, err
}

func (m *SQLMemoryService) PruneBefore(ctx context.Context, cutoff time.Time) (int, error) {
	items, err := m.ListAllMemoryItems(ctx)
	if err != nil {
		return 0, err
	}
	changed := 0
	now := time.Now().UTC()
	for _, item := range items {
		updated, ok := applyMemoryLifecycle(item, now)
		if !ok {
			continue
		}
		if _, err := m.UpdateMemoryItem(ctx, item.UserID, updated); err != nil {
			return changed, err
		}
		changed++
	}
	result, err := m.db.ExecContext(ctx, m.dialect.Bind(`DELETE FROM agent_memory WHERE status = ? AND updated_at < ?`), MemoryStatusDeleted, sqlTimeValue(cutoff, m.dialect))
	if err != nil {
		return changed, err
	}
	rows, _ := result.RowsAffected()
	return changed + int(rows), nil
}

func (m *SQLMemoryService) GetMemoryItem(ctx context.Context, userID, itemID string) (MemoryItem, error) {
	row := m.db.QueryRowContext(ctx, m.dialect.Bind(`SELECT id, user_id, session_id, namespace, kind, level, category, tags, source, source_refs, visibility, status, content, raw_hash, confidence, weight, access_count, parent_id, related_ids, conflict_ids, supersedes_id, superseded_by_id, last_injected_at, metadata, expires_at, created_at, updated_at
FROM agent_memory
WHERE user_id = ? AND id = ?`), userID, itemID)
	item, err := scanMemoryItemRows(row)
	if err != nil {
		return MemoryItem{}, err
	}
	return normalizeMemoryItem(item), nil
}

func (m *SQLMemoryService) ListMemoryItems(ctx context.Context, userID string, filter MemoryItemFilter) ([]MemoryItem, error) {
	query := `SELECT id, user_id, session_id, namespace, kind, level, category, tags, source, source_refs, visibility, status, content, raw_hash, confidence, weight, access_count, parent_id, related_ids, conflict_ids, supersedes_id, superseded_by_id, last_injected_at, metadata, expires_at, created_at, updated_at
FROM agent_memory
WHERE user_id = ?`
	args := []any{userID}
	if filter.SessionID != "" {
		query += ` AND session_id = ?`
		args = append(args, filter.SessionID)
	}
	if filter.Namespace != "" {
		query += ` AND namespace = ?`
		args = append(args, normalizeMemoryNamespace(filter.Namespace))
	}
	if filter.Kind != "" {
		query += ` AND kind = ?`
		args = append(args, filter.Kind)
	}
	if filter.Level != "" {
		query += ` AND level = ?`
		args = append(args, filter.Level)
	}
	if filter.Category != "" {
		query += ` AND category = ?`
		args = append(args, filter.Category)
	}
	if filter.Visibility != "" {
		query += ` AND visibility = ?`
		args = append(args, filter.Visibility)
	}
	if filter.Status != "" {
		query += ` AND status = ?`
		args = append(args, filter.Status)
	}
	if filter.Query != "" {
		query += ` AND LOWER(content) LIKE ?`
		args = append(args, "%"+strings.ToLower(filter.Query)+"%")
	}
	if filter.SourceKind != "" || filter.SourceID != "" {
		if filter.SourceKind != "" {
			query += ` AND source_refs LIKE ?`
			args = append(args, "%\"kind\":\""+normalizeAssetKind(filter.SourceKind)+"\"%")
		}
		if filter.SourceID != "" {
			query += ` AND source_refs LIKE ?`
			args = append(args, "%\"id\":\""+strings.TrimSpace(filter.SourceID)+"\"%")
		}
	}
	query += ` ORDER BY weight DESC, updated_at DESC, id DESC`
	if filter.Limit > 0 {
		query += ` LIMIT ?`
		args = append(args, filter.Limit)
	}
	rows, err := m.db.QueryContext(ctx, m.dialect.Bind(query), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]MemoryItem, 0)
	for rows.Next() {
		item, err := scanMemoryItemRows(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, normalizeMemoryItem(item))
	}
	return items, rows.Err()
}

func (m *SQLMemoryService) ListAllMemoryItems(ctx context.Context) ([]MemoryItem, error) {
	rows, err := m.db.QueryContext(ctx, `SELECT id, user_id, session_id, namespace, kind, level, category, tags, source, source_refs, visibility, status, content, raw_hash, confidence, weight, access_count, parent_id, related_ids, conflict_ids, supersedes_id, superseded_by_id, last_injected_at, metadata, expires_at, created_at, updated_at FROM agent_memory`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []MemoryItem
	for rows.Next() {
		item, err := scanMemoryItemRows(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, normalizeMemoryItem(item))
	}
	return items, rows.Err()
}

func (m *SQLMemoryService) UpdateMemoryItem(ctx context.Context, userID string, item MemoryItem) (MemoryItem, error) {
	if strings.TrimSpace(item.ID) == "" {
		return MemoryItem{}, fmt.Errorf("memory item ID is required")
	}
	item.UserID = userID
	if item.UpdatedAt.IsZero() {
		item.UpdatedAt = time.Now().UTC()
	}
	return m.upsertMemoryItem(ctx, item)
}

func (m *SQLMemoryService) upsertMemoryItem(ctx context.Context, item MemoryItem) (MemoryItem, error) {
	item.Content = sanitizeSQLText(item.Content)
	item = normalizeMemoryItem(item)
	tagsJSON, err := json.Marshal(item.Tags)
	if err != nil {
		return MemoryItem{}, err
	}
	relatedIDsJSON, err := json.Marshal(item.RelatedIDs)
	if err != nil {
		return MemoryItem{}, err
	}
	conflictIDsJSON, err := json.Marshal(item.ConflictIDs)
	if err != nil {
		return MemoryItem{}, err
	}
	sourceRefsJSON, err := json.Marshal(item.SourceRefs)
	if err != nil {
		return MemoryItem{}, err
	}
	metadataJSON, err := json.Marshal(item.Metadata)
	if err != nil {
		return MemoryItem{}, err
	}
	_, err = m.db.ExecContext(ctx, m.dialect.Bind(`
INSERT INTO agent_memory (id, user_id, session_id, namespace, kind, level, category, tags, source, source_refs, visibility, status, content, raw_hash, confidence, weight, access_count, parent_id, related_ids, conflict_ids, supersedes_id, superseded_by_id, last_injected_at, metadata, expires_at, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
	session_id = excluded.session_id,
	namespace = excluded.namespace,
	kind = excluded.kind,
	level = excluded.level,
	category = excluded.category,
	tags = excluded.tags,
	source = excluded.source,
	source_refs = excluded.source_refs,
	visibility = excluded.visibility,
	status = excluded.status,
	content = excluded.content,
	raw_hash = excluded.raw_hash,
	confidence = excluded.confidence,
	weight = excluded.weight,
	access_count = excluded.access_count,
	parent_id = excluded.parent_id,
	related_ids = excluded.related_ids,
	conflict_ids = excluded.conflict_ids,
	supersedes_id = excluded.supersedes_id,
	superseded_by_id = excluded.superseded_by_id,
	last_injected_at = excluded.last_injected_at,
	metadata = excluded.metadata,
	expires_at = excluded.expires_at,
	updated_at = excluded.updated_at`),
		item.ID,
		item.UserID,
		item.SessionID,
		item.Namespace,
		item.Kind,
		item.Level,
		item.Category,
		string(tagsJSON),
		item.Source,
		string(sourceRefsJSON),
		item.Visibility,
		item.Status,
		item.Content,
		item.RawHash,
		item.Confidence,
		item.Weight,
		item.AccessCount,
		item.ParentID,
		string(relatedIDsJSON),
		string(conflictIDsJSON),
		item.SupersedesID,
		item.SupersededByID,
		nullableSQLTimeValue(item.LastInjectedAt, m.dialect),
		string(metadataJSON),
		nullableSQLTimeValue(item.ExpiresAt, m.dialect),
		sqlTimeValue(item.CreatedAt, m.dialect),
		sqlTimeValue(item.UpdatedAt, m.dialect))
	return item, err
}

func (m *SQLMemoryService) DeleteMemoryItem(ctx context.Context, userID, itemID string) error {
	_, err := m.db.ExecContext(ctx, m.dialect.Bind(`DELETE FROM agent_memory WHERE user_id = ? AND id = ?`), userID, itemID)
	return err
}

type memoryItemScanner interface {
	Scan(dest ...any) error
}

func scanMemoryItemRows(row memoryItemScanner) (MemoryItem, error) {
	var item MemoryItem
	var tagsJSON, sourceRefsJSON, relatedIDsJSON, conflictIDsJSON, metadataJSON string
	var createdAt, updatedAt, expiresAt, lastInjectedAt any
	var sessionID sql.NullString
	if err := row.Scan(&item.ID, &item.UserID, &sessionID, &item.Namespace, &item.Kind, &item.Level, &item.Category, &tagsJSON, &item.Source, &sourceRefsJSON, &item.Visibility, &item.Status, &item.Content, &item.RawHash, &item.Confidence, &item.Weight, &item.AccessCount, &item.ParentID, &relatedIDsJSON, &conflictIDsJSON, &item.SupersedesID, &item.SupersededByID, &lastInjectedAt, &metadataJSON, &expiresAt, &createdAt, &updatedAt); err != nil {
		return MemoryItem{}, err
	}
	if sessionID.Valid {
		item.SessionID = sessionID.String
	}
	_ = json.Unmarshal([]byte(tagsJSON), &item.Tags)
	_ = json.Unmarshal([]byte(sourceRefsJSON), &item.SourceRefs)
	_ = json.Unmarshal([]byte(relatedIDsJSON), &item.RelatedIDs)
	_ = json.Unmarshal([]byte(conflictIDsJSON), &item.ConflictIDs)
	_ = json.Unmarshal([]byte(metadataJSON), &item.Metadata)
	var err error
	if item.LastInjectedAt, err = parseNullableSQLTime(lastInjectedAt); err != nil {
		return MemoryItem{}, err
	}
	if item.ExpiresAt, err = parseNullableSQLTime(expiresAt); err != nil {
		return MemoryItem{}, err
	}
	if item.CreatedAt, err = parseSQLTime(createdAt); err != nil {
		return MemoryItem{}, err
	}
	if item.UpdatedAt, err = parseSQLTime(updatedAt); err != nil {
		return MemoryItem{}, err
	}
	return item, nil
}

type SQLDialect string

const (
	SQLDialectQuestion SQLDialect = "question"
	SQLDialectPostgres SQLDialect = "postgres"
)

func ParseSQLDialect(value string) SQLDialect {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "postgres", "postgresql", "pg", "pgx":
		return SQLDialectPostgres
	default:
		return SQLDialectQuestion
	}
}

func (d SQLDialect) Placeholder(index int) string {
	if d == SQLDialectPostgres {
		return "$" + fmt.Sprint(index)
	}
	return "?"
}

func (d SQLDialect) Bind(query string) string {
	if d != SQLDialectPostgres {
		return query
	}
	var out strings.Builder
	index := 1
	for _, r := range query {
		if r == '?' {
			out.WriteString(d.Placeholder(index))
			index++
			continue
		}
		out.WriteRune(r)
	}
	return out.String()
}
