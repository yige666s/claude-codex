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

	"claude-codex/internal/harness/state"
)

type SQLSessionStore struct {
	db      *sql.DB
	dialect SQLDialect
}

func NewSQLSessionStore(db *sql.DB) *SQLSessionStore {
	return NewSQLSessionStoreWithDialect(db, SQLDialectQuestion)
}

func NewSQLSessionStoreWithDialect(db *sql.DB, dialect SQLDialect) *SQLSessionStore {
	if dialect == "" {
		dialect = SQLDialectQuestion
	}
	return &SQLSessionStore{db: db, dialect: dialect}
}

func (s *SQLSessionStore) Init(ctx context.Context) error {
	timeType := s.dialect.TimeType()
	if err := RunSQLMigrations(ctx, s.db, s.dialect, []SQLMigration{
		{
			Version: 1,
			Statements: []string{
				`CREATE TABLE IF NOT EXISTS agent_sessions (
	user_id TEXT NOT NULL,
	session_id TEXT NOT NULL,
	updated_at ` + timeType + ` NOT NULL,
	payload TEXT NOT NULL,
	PRIMARY KEY (user_id, session_id)
)`,
				`CREATE INDEX IF NOT EXISTS idx_agent_sessions_user_updated ON agent_sessions (user_id, updated_at)`,
			},
		},
		{
			Version: 4,
			Statements: []string{
				`CREATE TABLE IF NOT EXISTS agent_messages (
	user_id TEXT NOT NULL,
	session_id TEXT NOT NULL,
	message_index INTEGER NOT NULL,
	role TEXT NOT NULL,
	content TEXT NOT NULL DEFAULT '',
	tool_name TEXT NOT NULL DEFAULT '',
	tool_call_id TEXT NOT NULL DEFAULT '',
	tool_input TEXT NOT NULL DEFAULT '',
	tool_output TEXT NOT NULL DEFAULT '',
	tool_calls TEXT NOT NULL DEFAULT '',
	hidden INTEGER NOT NULL DEFAULT 0,
	created_at ` + timeType + ` NOT NULL,
	payload TEXT NOT NULL,
	PRIMARY KEY (user_id, session_id, message_index)
)`,
				`CREATE INDEX IF NOT EXISTS idx_agent_messages_user_created ON agent_messages (user_id, created_at)`,
				`CREATE INDEX IF NOT EXISTS idx_agent_messages_session_created ON agent_messages (user_id, session_id, created_at)`,
				`CREATE INDEX IF NOT EXISTS idx_agent_messages_role_created ON agent_messages (user_id, role, created_at)`,
			},
		},
		{
			Version:    5,
			Statements: []string{},
		},
	}); err != nil {
		return err
	}
	if err := ensureReadableTimeColumns(ctx, s.db, s.dialect, "agent_sessions", "updated_at"); err != nil {
		return err
	}
	if err := ensureReadableTimeColumns(ctx, s.db, s.dialect, "agent_messages", "created_at"); err != nil {
		return err
	}
	if err := s.initMessageSearchIndexes(ctx); err != nil {
		return err
	}
	return s.BackfillMessages(ctx)
}

func (s *SQLSessionStore) Create(ctx context.Context, userID, workingDir string) (*state.Session, error) {
	session := state.NewSession(workingDir)
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
	var payload string
	if err := s.db.QueryRowContext(ctx, s.dialect.Bind(`SELECT payload FROM agent_sessions WHERE user_id = ? AND session_id = ?`), userID, sessionID).Scan(&payload); err != nil {
		return nil, err
	}
	var session state.Session
	if err := json.Unmarshal([]byte(payload), &session); err != nil {
		return nil, err
	}
	return &session, nil
}

func (s *SQLSessionStore) List(ctx context.Context, userID string) ([]*state.Session, error) {
	rows, err := s.db.QueryContext(ctx, s.dialect.Bind(`SELECT payload FROM agent_sessions WHERE user_id = ? ORDER BY updated_at DESC`), userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*state.Session, 0)
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		var session state.Session
		if err := json.Unmarshal([]byte(payload), &session); err == nil {
			out = append(out, &session)
		}
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
	sessionForSQL := sanitizeSessionForSQL(session)
	payload, err := json.Marshal(sessionForSQL)
	if err != nil {
		return err
	}
	updatedAt := sessionForSQL.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = time.Now().UTC()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, s.dialect.Bind(`
INSERT INTO agent_sessions (user_id, session_id, updated_at, payload)
VALUES (?, ?, ?, ?)
ON CONFLICT(user_id, session_id) DO UPDATE SET updated_at = excluded.updated_at, payload = excluded.payload`),
		userID, sessionForSQL.ID, sqlTimeValue(updatedAt, s.dialect), string(payload)); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := s.replaceMessages(ctx, tx, userID, sessionForSQL); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (s *SQLSessionStore) Delete(ctx context.Context, userID, sessionID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, s.dialect.Bind(`DELETE FROM agent_messages WHERE user_id = ? AND session_id = ?`), userID, sessionID); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.ExecContext(ctx, s.dialect.Bind(`DELETE FROM agent_sessions WHERE user_id = ? AND session_id = ?`), userID, sessionID); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (s *SQLSessionStore) DeleteUser(ctx context.Context, userID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, s.dialect.Bind(`DELETE FROM agent_messages WHERE user_id = ?`), userID); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.ExecContext(ctx, s.dialect.Bind(`DELETE FROM agent_sessions WHERE user_id = ?`), userID); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (s *SQLSessionStore) PruneBefore(ctx context.Context, cutoff time.Time) (int, error) {
	rows, err := s.db.QueryContext(ctx, s.dialect.Bind(`SELECT user_id, session_id FROM agent_sessions WHERE updated_at < ?`), sqlTimeValue(cutoff, s.dialect))
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
SELECT payload FROM agent_messages WHERE user_id = ? AND session_id = ? ORDER BY message_index ASC`), userID, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	messages := make([]state.Message, 0)
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		var message state.Message
		if err := json.Unmarshal([]byte(payload), &message); err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	return messages, rows.Err()
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
SELECT m.session_id, m.message_index, m.role, m.content, m.tool_output, m.created_at, s.payload
FROM agent_messages m
JOIN agent_sessions s ON s.user_id = m.user_id AND s.session_id = m.session_id
WHERE m.user_id = ?
  AND m.hidden = 0
  AND m.role <> 'tool'
  AND (m.content %s ? ESCAPE '\' OR m.tool_output %s ? ESCAPE '\')
ORDER BY m.created_at DESC
LIMIT ? OFFSET ?`, matchOperator, matchOperator)), userID, pattern, pattern, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]MessageSearchResult, 0, limit)
	for rows.Next() {
		var result MessageSearchResult
		var content, toolOutput, sessionPayload string
		var createdRaw any
		if err := rows.Scan(&result.SessionID, &result.MessageIndex, &result.Role, &content, &toolOutput, &createdRaw, &sessionPayload); err != nil {
			return nil, err
		}
		createdAt, err := parseSQLTime(createdRaw)
		if err != nil {
			return nil, err
		}
		var session state.Session
		_ = json.Unmarshal([]byte(sessionPayload), &session)
		searchable := messageSearchContent(content, toolOutput, query)
		result.Content = searchable
		result.Snippet = messageSearchSnippet(searchable, query, 160)
		result.SessionTitle = searchSessionTitle(&session)
		result.CreatedAt = createdAt
		out = append(out, result)
	}
	return out, rows.Err()
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
	rows, err := s.db.QueryContext(ctx, `SELECT user_id, payload FROM agent_sessions`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type pendingSession struct {
		userID  string
		session *state.Session
	}
	var pending []pendingSession
	for rows.Next() {
		var userID, payload string
		if err := rows.Scan(&userID, &payload); err != nil {
			return err
		}
		var session state.Session
		if err := json.Unmarshal([]byte(payload), &session); err != nil {
			continue
		}
		pending = append(pending, pendingSession{userID: userID, session: &session})
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, item := range pending {
		var count int
		err := s.db.QueryRowContext(ctx, s.dialect.Bind(`SELECT COUNT(*) FROM agent_messages WHERE user_id = ? AND session_id = ?`), item.userID, item.session.ID).Scan(&count)
		if err != nil {
			return err
		}
		if count > 0 {
			continue
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if err := s.replaceMessages(ctx, tx, item.userID, item.session); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLSessionStore) replaceMessages(ctx context.Context, tx *sql.Tx, userID string, session *state.Session) error {
	if _, err := tx.ExecContext(ctx, s.dialect.Bind(`DELETE FROM agent_messages WHERE user_id = ? AND session_id = ?`), userID, session.ID); err != nil {
		return err
	}
	for index, message := range session.Messages {
		payload, err := json.Marshal(message)
		if err != nil {
			return err
		}
		toolCalls, err := json.Marshal(message.ToolCalls)
		if err != nil {
			return err
		}
		createdAt := message.CreatedAt
		if createdAt.IsZero() {
			base := session.StartedAt
			if base.IsZero() {
				base = time.Now().UTC()
			}
			createdAt = base.Add(time.Duration(index) * time.Millisecond)
		}
		hidden := 0
		if message.Hidden {
			hidden = 1
		}
		if _, err := tx.ExecContext(ctx, s.dialect.Bind(`
INSERT INTO agent_messages (
	user_id, session_id, message_index, role, content, tool_name, tool_call_id,
	tool_input, tool_output, tool_calls, hidden, created_at, payload
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`),
			userID,
			session.ID,
			index,
			message.Role,
			message.Content,
			message.ToolName,
			message.ToolCallID,
			sanitizeSQLText(string(message.ToolInput)),
			message.ToolOutput,
			sanitizeSQLText(string(toolCalls)),
			hidden,
			sqlTimeValue(createdAt, s.dialect),
			sanitizeSQLText(string(payload)),
		); err != nil {
			return err
		}
	}
	return nil
}

func sanitizeSessionForSQL(session *state.Session) *state.Session {
	if session == nil {
		return nil
	}
	clone := *session
	clone.ID = sanitizeSQLText(clone.ID)
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
	message.Role = sanitizeSQLText(message.Role)
	message.Content = sanitizeSQLText(message.Content)
	message.ToolName = sanitizeSQLText(message.ToolName)
	message.ToolCallID = sanitizeSQLText(message.ToolCallID)
	message.ToolOutput = sanitizeSQLText(message.ToolOutput)
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
