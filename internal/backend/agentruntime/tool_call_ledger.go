package agentruntime

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"claude-codex/internal/harness/engine"
)

type ToolCallLedgerFilter struct {
	UserID         string
	SessionID      string
	JobID          string
	WorkflowRunID  string
	WorkflowStepID string
	ToolName       string
	Status         string
	Limit          int
}

type ToolCallLedgerStore interface {
	engine.ToolLedger
	ListToolCalls(ctx context.Context, filter ToolCallLedgerFilter) ([]engine.ToolLedgerEntry, error)
}

type MemoryToolCallLedgerStore struct {
	mu      sync.Mutex
	entries map[string]engine.ToolLedgerEntry
}

func NewMemoryToolCallLedgerStore() *MemoryToolCallLedgerStore {
	return &MemoryToolCallLedgerStore{entries: make(map[string]engine.ToolLedgerEntry)}
}

func (s *MemoryToolCallLedgerStore) BeginToolCall(_ context.Context, entry engine.ToolLedgerEntry) (engine.ToolLedgerEntry, bool, error) {
	if s == nil {
		return entry, false, fmt.Errorf("tool call ledger is not configured")
	}
	entry = normalizeToolLedgerEntry(entry)
	if entry.IdempotencyKey == "" {
		return entry, false, fmt.Errorf("tool call idempotency key is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.entries[entry.IdempotencyKey]; ok {
		switch existing.Status {
		case engine.ToolLedgerStatusSucceeded:
			return cloneToolLedgerEntry(existing), true, nil
		case engine.ToolLedgerStatusFailed:
			existing.Status = engine.ToolLedgerStatusRunning
			existing.Error = ""
			existing.Output = ""
			existing.CompletedAt = nil
			existing.Attempt++
			if existing.Attempt <= 0 {
				existing.Attempt = 1
			}
			existing.StartedAt = time.Now().UTC()
			existing.Metadata = mergeLedgerMetadata(existing.Metadata, entry.Metadata)
			s.entries[entry.IdempotencyKey] = cloneToolLedgerEntry(existing)
			return cloneToolLedgerEntry(existing), false, nil
		default:
			existing.Status = engine.ToolLedgerStatusRequiresReview
			existing.Error = "tool call was already in progress; manual review required before replay"
			completed := time.Now().UTC()
			existing.CompletedAt = &completed
			s.entries[entry.IdempotencyKey] = cloneToolLedgerEntry(existing)
			return cloneToolLedgerEntry(existing), false, fmt.Errorf("tool call requires review before replay: %s", entry.IdempotencyKey)
		}
	}
	if entry.ID == "" {
		entry.ID = NewToolCallLedgerID()
	}
	if entry.StartedAt.IsZero() {
		entry.StartedAt = time.Now().UTC()
	}
	entry.Status = engine.ToolLedgerStatusRunning
	entry.Attempt = maxToolLedgerInt(entry.Attempt, 1)
	s.entries[entry.IdempotencyKey] = cloneToolLedgerEntry(entry)
	return cloneToolLedgerEntry(entry), false, nil
}

func (s *MemoryToolCallLedgerStore) CompleteToolCall(_ context.Context, idempotencyKey, output string, metadata map[string]any) error {
	if s == nil {
		return fmt.Errorf("tool call ledger is not configured")
	}
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if idempotencyKey == "" {
		return fmt.Errorf("tool call idempotency key is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.entries[idempotencyKey]
	if !ok {
		return fmt.Errorf("tool call ledger entry not found: %s", idempotencyKey)
	}
	completed := time.Now().UTC()
	entry.Status = engine.ToolLedgerStatusSucceeded
	entry.Output = output
	entry.Error = ""
	entry.CompletedAt = &completed
	entry.Metadata = mergeLedgerMetadata(entry.Metadata, metadata)
	s.entries[idempotencyKey] = cloneToolLedgerEntry(entry)
	return nil
}

func (s *MemoryToolCallLedgerStore) FailToolCall(_ context.Context, idempotencyKey, errText string, metadata map[string]any) error {
	if s == nil {
		return fmt.Errorf("tool call ledger is not configured")
	}
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if idempotencyKey == "" {
		return fmt.Errorf("tool call idempotency key is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.entries[idempotencyKey]
	if !ok {
		return fmt.Errorf("tool call ledger entry not found: %s", idempotencyKey)
	}
	completed := time.Now().UTC()
	entry.Status = engine.ToolLedgerStatusFailed
	entry.Error = errText
	entry.CompletedAt = &completed
	entry.Metadata = mergeLedgerMetadata(entry.Metadata, metadata)
	s.entries[idempotencyKey] = cloneToolLedgerEntry(entry)
	return nil
}

func (s *MemoryToolCallLedgerStore) ListToolCalls(_ context.Context, filter ToolCallLedgerFilter) ([]engine.ToolLedgerEntry, error) {
	if s == nil {
		return []engine.ToolLedgerEntry{}, nil
	}
	filter = normalizeToolCallLedgerFilter(filter)
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]engine.ToolLedgerEntry, 0, len(s.entries))
	for _, entry := range s.entries {
		if !toolLedgerEntryMatches(entry, filter) {
			continue
		}
		out = append(out, cloneToolLedgerEntry(entry))
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].StartedAt.After(out[j].StartedAt)
	})
	if filter.Limit > 0 && len(out) > filter.Limit {
		out = out[:filter.Limit]
	}
	return out, nil
}

type SQLToolCallLedgerStore struct {
	db      *sql.DB
	dialect SQLDialect
}

func NewSQLToolCallLedgerStoreWithDialect(db *sql.DB, dialect SQLDialect) *SQLToolCallLedgerStore {
	if dialect == "" {
		dialect = SQLDialectQuestion
	}
	return &SQLToolCallLedgerStore{db: db, dialect: dialect}
}

func (s *SQLToolCallLedgerStore) Init(ctx context.Context) error {
	return requireSQLColumns(ctx, s.db, "agent_tool_call_ledger",
		"id", "user_id", "session_id", "job_id", "workflow_run_id", "workflow_step_id",
		"workflow_step_index", "tool_call_id", "tool_name", "args_hash", "idempotency_key",
		"status", "input_json", "output_text", "error", "external_idempotency_key",
		"attempt", "metadata_json", "started_at", "completed_at",
	)
}

func (s *SQLToolCallLedgerStore) BeginToolCall(ctx context.Context, entry engine.ToolLedgerEntry) (engine.ToolLedgerEntry, bool, error) {
	if s == nil {
		return entry, false, fmt.Errorf("tool call ledger is not configured")
	}
	entry = normalizeToolLedgerEntry(entry)
	if entry.IdempotencyKey == "" {
		return entry, false, fmt.Errorf("tool call idempotency key is required")
	}
	existing, err := s.getByIdempotencyKey(ctx, entry.IdempotencyKey)
	if err != nil {
		return entry, false, err
	}
	if existing != nil {
		switch existing.Status {
		case engine.ToolLedgerStatusSucceeded:
			return *existing, true, nil
		case engine.ToolLedgerStatusFailed:
			existing.Status = engine.ToolLedgerStatusRunning
			existing.Error = ""
			existing.Output = ""
			existing.CompletedAt = nil
			existing.Attempt++
			if existing.Attempt <= 0 {
				existing.Attempt = 1
			}
			existing.StartedAt = time.Now().UTC()
			existing.Metadata = mergeLedgerMetadata(existing.Metadata, entry.Metadata)
			if err := s.update(ctx, *existing); err != nil {
				return *existing, false, err
			}
			return *existing, false, nil
		default:
			existing.Status = engine.ToolLedgerStatusRequiresReview
			existing.Error = "tool call was already in progress; manual review required before replay"
			completed := time.Now().UTC()
			existing.CompletedAt = &completed
			if err := s.update(ctx, *existing); err != nil {
				return *existing, false, err
			}
			return *existing, false, fmt.Errorf("tool call requires review before replay: %s", entry.IdempotencyKey)
		}
	}
	if entry.ID == "" {
		entry.ID = NewToolCallLedgerID()
	}
	if entry.StartedAt.IsZero() {
		entry.StartedAt = time.Now().UTC()
	}
	entry.Status = engine.ToolLedgerStatusRunning
	entry.Attempt = maxToolLedgerInt(entry.Attempt, 1)
	if err := s.insert(ctx, entry); err != nil {
		return entry, false, err
	}
	return entry, false, nil
}

func (s *SQLToolCallLedgerStore) CompleteToolCall(ctx context.Context, idempotencyKey, output string, metadata map[string]any) error {
	entry, err := s.getByIdempotencyKey(ctx, idempotencyKey)
	if err != nil {
		return err
	}
	if entry == nil {
		return fmt.Errorf("tool call ledger entry not found: %s", idempotencyKey)
	}
	completed := time.Now().UTC()
	entry.Status = engine.ToolLedgerStatusSucceeded
	entry.Output = output
	entry.Error = ""
	entry.CompletedAt = &completed
	entry.Metadata = mergeLedgerMetadata(entry.Metadata, metadata)
	return s.update(ctx, *entry)
}

func (s *SQLToolCallLedgerStore) FailToolCall(ctx context.Context, idempotencyKey, errText string, metadata map[string]any) error {
	entry, err := s.getByIdempotencyKey(ctx, idempotencyKey)
	if err != nil {
		return err
	}
	if entry == nil {
		return fmt.Errorf("tool call ledger entry not found: %s", idempotencyKey)
	}
	completed := time.Now().UTC()
	entry.Status = engine.ToolLedgerStatusFailed
	entry.Error = errText
	entry.CompletedAt = &completed
	entry.Metadata = mergeLedgerMetadata(entry.Metadata, metadata)
	return s.update(ctx, *entry)
}

func (s *SQLToolCallLedgerStore) ListToolCalls(ctx context.Context, filter ToolCallLedgerFilter) ([]engine.ToolLedgerEntry, error) {
	if s == nil {
		return []engine.ToolLedgerEntry{}, nil
	}
	filter = normalizeToolCallLedgerFilter(filter)
	query := `SELECT id, user_id, session_id, job_id, workflow_run_id, workflow_step_id, workflow_step_index, tool_call_id, tool_name, args_hash, idempotency_key, status, input_json, output_text, error, external_idempotency_key, attempt, metadata_json, started_at, completed_at FROM agent_tool_call_ledger`
	where, args := toolCallLedgerWhere(filter)
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY started_at DESC, id DESC"
	if filter.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filter.Limit)
	}
	rows, err := s.db.QueryContext(ctx, s.dialect.Bind(query), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []engine.ToolLedgerEntry{}
	for rows.Next() {
		entry, err := scanToolLedgerEntry(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, entry)
	}
	return out, rows.Err()
}

func (s *SQLToolCallLedgerStore) getByIdempotencyKey(ctx context.Context, idempotencyKey string) (*engine.ToolLedgerEntry, error) {
	if s == nil || strings.TrimSpace(idempotencyKey) == "" {
		return nil, nil
	}
	row := s.db.QueryRowContext(ctx, s.dialect.Bind(`
SELECT id, user_id, session_id, job_id, workflow_run_id, workflow_step_id, workflow_step_index, tool_call_id, tool_name, args_hash, idempotency_key, status, input_json, output_text, error, external_idempotency_key, attempt, metadata_json, started_at, completed_at
FROM agent_tool_call_ledger
WHERE idempotency_key = ?
LIMIT 1`), strings.TrimSpace(idempotencyKey))
	entry, err := scanToolLedgerEntry(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &entry, nil
}

func (s *SQLToolCallLedgerStore) insert(ctx context.Context, entry engine.ToolLedgerEntry) error {
	input := string(entry.Input)
	if strings.TrimSpace(input) == "" {
		input = "{}"
	}
	metadata, err := marshalWorkflowJSON(entry.Metadata)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, s.dialect.Bind(`
INSERT INTO agent_tool_call_ledger (id, user_id, session_id, job_id, workflow_run_id, workflow_step_id, workflow_step_index, tool_call_id, tool_name, args_hash, idempotency_key, status, input_json, output_text, error, external_idempotency_key, attempt, metadata_json, started_at, completed_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`),
		entry.ID, entry.UserID, entry.SessionID, entry.JobID, entry.WorkflowRunID, entry.WorkflowStepID,
		entry.WorkflowStepIndex, entry.ToolCallID, entry.ToolName, entry.ArgsHash, entry.IdempotencyKey,
		entry.Status, input, entry.Output, entry.Error, entry.ExternalIdempotencyKey, entry.Attempt,
		metadata, sqlTimeValue(entry.StartedAt, s.dialect), nullableSQLTimeValue(entry.CompletedAt, s.dialect),
	)
	return err
}

func (s *SQLToolCallLedgerStore) update(ctx context.Context, entry engine.ToolLedgerEntry) error {
	metadata, err := marshalWorkflowJSON(entry.Metadata)
	if err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, s.dialect.Bind(`
UPDATE agent_tool_call_ledger
SET user_id = ?, session_id = ?, job_id = ?, workflow_run_id = ?, workflow_step_id = ?, workflow_step_index = ?, tool_call_id = ?, tool_name = ?, args_hash = ?, status = ?, input_json = ?, output_text = ?, error = ?, external_idempotency_key = ?, attempt = ?, metadata_json = ?, started_at = ?, completed_at = ?
WHERE idempotency_key = ?`),
		entry.UserID, entry.SessionID, entry.JobID, entry.WorkflowRunID, entry.WorkflowStepID,
		entry.WorkflowStepIndex, entry.ToolCallID, entry.ToolName, entry.ArgsHash, entry.Status,
		toolLedgerInputJSON(entry.Input), entry.Output, entry.Error, entry.ExternalIdempotencyKey, entry.Attempt,
		metadata, sqlTimeValue(entry.StartedAt, s.dialect), nullableSQLTimeValue(entry.CompletedAt, s.dialect),
		entry.IdempotencyKey,
	)
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows == 0 {
		return fmt.Errorf("tool call ledger entry not found: %s", entry.IdempotencyKey)
	}
	return nil
}

func toolLedgerInputJSON(input json.RawMessage) string {
	text := strings.TrimSpace(string(input))
	if text == "" {
		return "{}"
	}
	return text
}

func toolCallLedgerWhere(filter ToolCallLedgerFilter) ([]string, []any) {
	var where []string
	var args []any
	if filter.UserID != "" {
		where = append(where, "user_id = ?")
		args = append(args, filter.UserID)
	}
	if filter.SessionID != "" {
		where = append(where, "session_id = ?")
		args = append(args, filter.SessionID)
	}
	if filter.JobID != "" {
		where = append(where, "job_id = ?")
		args = append(args, filter.JobID)
	}
	if filter.WorkflowRunID != "" {
		where = append(where, "workflow_run_id = ?")
		args = append(args, filter.WorkflowRunID)
	}
	if filter.WorkflowStepID != "" {
		where = append(where, "workflow_step_id = ?")
		args = append(args, filter.WorkflowStepID)
	}
	if filter.ToolName != "" {
		where = append(where, "tool_name = ?")
		args = append(args, filter.ToolName)
	}
	if filter.Status != "" {
		where = append(where, "status = ?")
		args = append(args, filter.Status)
	}
	return where, args
}

func scanToolLedgerEntry(row workflowScanner) (engine.ToolLedgerEntry, error) {
	var entry engine.ToolLedgerEntry
	var inputJSON, metadataJSON []byte
	var startedAt, completedAt any
	if err := row.Scan(
		&entry.ID,
		&entry.UserID,
		&entry.SessionID,
		&entry.JobID,
		&entry.WorkflowRunID,
		&entry.WorkflowStepID,
		&entry.WorkflowStepIndex,
		&entry.ToolCallID,
		&entry.ToolName,
		&entry.ArgsHash,
		&entry.IdempotencyKey,
		&entry.Status,
		&inputJSON,
		&entry.Output,
		&entry.Error,
		&entry.ExternalIdempotencyKey,
		&entry.Attempt,
		&metadataJSON,
		&startedAt,
		&completedAt,
	); err != nil {
		return entry, err
	}
	entry.Input = json.RawMessage(inputJSON)
	if err := unmarshalWorkflowJSON(metadataJSON, &entry.Metadata); err != nil {
		return entry, err
	}
	var err error
	if entry.StartedAt, err = parseSQLTime(startedAt); err != nil {
		return entry, err
	}
	if entry.CompletedAt, err = parseNullableSQLTime(completedAt); err != nil {
		return entry, err
	}
	return cloneToolLedgerEntry(entry), nil
}

func normalizeToolLedgerEntry(entry engine.ToolLedgerEntry) engine.ToolLedgerEntry {
	entry.UserID = strings.TrimSpace(entry.UserID)
	entry.SessionID = strings.TrimSpace(entry.SessionID)
	entry.JobID = strings.TrimSpace(entry.JobID)
	entry.WorkflowRunID = strings.TrimSpace(entry.WorkflowRunID)
	entry.WorkflowStepID = strings.TrimSpace(entry.WorkflowStepID)
	entry.ToolCallID = strings.TrimSpace(entry.ToolCallID)
	entry.ToolName = strings.TrimSpace(entry.ToolName)
	entry.ArgsHash = strings.TrimSpace(entry.ArgsHash)
	entry.IdempotencyKey = strings.TrimSpace(entry.IdempotencyKey)
	entry.Status = strings.TrimSpace(entry.Status)
	entry.ExternalIdempotencyKey = strings.TrimSpace(entry.ExternalIdempotencyKey)
	if entry.Metadata == nil {
		entry.Metadata = map[string]any{}
	}
	return entry
}

func normalizeToolCallLedgerFilter(filter ToolCallLedgerFilter) ToolCallLedgerFilter {
	filter.UserID = strings.TrimSpace(filter.UserID)
	filter.SessionID = strings.TrimSpace(filter.SessionID)
	filter.JobID = strings.TrimSpace(filter.JobID)
	filter.WorkflowRunID = strings.TrimSpace(filter.WorkflowRunID)
	filter.WorkflowStepID = strings.TrimSpace(filter.WorkflowStepID)
	filter.ToolName = strings.TrimSpace(filter.ToolName)
	filter.Status = strings.TrimSpace(filter.Status)
	if filter.Limit < 0 {
		filter.Limit = 0
	}
	return filter
}

func toolLedgerEntryMatches(entry engine.ToolLedgerEntry, filter ToolCallLedgerFilter) bool {
	if filter.UserID != "" && entry.UserID != filter.UserID {
		return false
	}
	if filter.SessionID != "" && entry.SessionID != filter.SessionID {
		return false
	}
	if filter.JobID != "" && entry.JobID != filter.JobID {
		return false
	}
	if filter.WorkflowRunID != "" && entry.WorkflowRunID != filter.WorkflowRunID {
		return false
	}
	if filter.WorkflowStepID != "" && entry.WorkflowStepID != filter.WorkflowStepID {
		return false
	}
	if filter.ToolName != "" && entry.ToolName != filter.ToolName {
		return false
	}
	if filter.Status != "" && entry.Status != filter.Status {
		return false
	}
	return true
}

func cloneToolLedgerEntry(entry engine.ToolLedgerEntry) engine.ToolLedgerEntry {
	out := entry
	if entry.Input != nil {
		out.Input = append(json.RawMessage(nil), entry.Input...)
	}
	out.Metadata = cloneWorkflowMap(entry.Metadata)
	return out
}

func mergeLedgerMetadata(base, overlay map[string]any) map[string]any {
	out := cloneWorkflowMap(base)
	for key, value := range overlay {
		out[key] = value
	}
	return out
}

func NewToolCallLedgerID() string {
	return "tcl-" + newSortableID()
}

func maxToolLedgerInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
