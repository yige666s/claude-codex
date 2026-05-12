package agentruntime

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"strings"
	"sync"
	"time"
)

const (
	SkillExecutionStatusSucceeded = "succeeded"
	SkillExecutionStatusFailed    = "failed"
)

type SkillExecutionRecord struct {
	ID          string         `json:"id"`
	SkillName   string         `json:"skill_name"`
	UserID      string         `json:"user_id,omitempty"`
	SessionID   string         `json:"session_id,omitempty"`
	JobID       string         `json:"job_id,omitempty"`
	RequestID   string         `json:"request_id,omitempty"`
	Status      string         `json:"status"`
	Error       string         `json:"error,omitempty"`
	DurationMS  int64          `json:"duration_ms"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	StartedAt   time.Time      `json:"started_at"`
	CompletedAt time.Time      `json:"completed_at"`
}

type SkillExecutionFilter struct {
	SkillName string
	Status    string
	UserID    string
	SessionID string
	JobID     string
	Limit     int
}

type SkillExecutionSummary struct {
	SkillName        string  `json:"skill_name,omitempty"`
	Total            int     `json:"total"`
	Succeeded        int     `json:"succeeded"`
	Failed           int     `json:"failed"`
	FailureRate      float64 `json:"failure_rate"`
	AverageLatencyMS int64   `json:"average_latency_ms"`
}

type SkillExecutionStore interface {
	Init(ctx context.Context) error
	RecordSkillExecution(ctx context.Context, record SkillExecutionRecord) error
	ListSkillExecutions(ctx context.Context, filter SkillExecutionFilter) ([]SkillExecutionRecord, error)
	SummarizeSkillExecutions(ctx context.Context, filter SkillExecutionFilter) (SkillExecutionSummary, error)
}

type MemorySkillExecutionStore struct {
	mu      sync.Mutex
	records []SkillExecutionRecord
}

func NewMemorySkillExecutionStore() *MemorySkillExecutionStore {
	return &MemorySkillExecutionStore{}
}

func (s *MemorySkillExecutionStore) Init(context.Context) error {
	return nil
}

func (s *MemorySkillExecutionStore) RecordSkillExecution(_ context.Context, record SkillExecutionRecord) error {
	record = normalizeSkillExecutionRecord(record)
	s.mu.Lock()
	s.records = append(s.records, record)
	s.mu.Unlock()
	return nil
}

func (s *MemorySkillExecutionStore) ListSkillExecutions(_ context.Context, filter SkillExecutionFilter) ([]SkillExecutionRecord, error) {
	filter = normalizeSkillExecutionFilter(filter)
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]SkillExecutionRecord, 0, len(s.records))
	for i := len(s.records) - 1; i >= 0; i-- {
		record := s.records[i]
		if !skillExecutionMatches(record, filter) {
			continue
		}
		out = append(out, record)
		if filter.Limit > 0 && len(out) >= filter.Limit {
			break
		}
	}
	return out, nil
}

func (s *MemorySkillExecutionStore) SummarizeSkillExecutions(ctx context.Context, filter SkillExecutionFilter) (SkillExecutionSummary, error) {
	records, err := s.ListSkillExecutions(ctx, SkillExecutionFilter{
		SkillName: filter.SkillName,
		Status:    filter.Status,
		UserID:    filter.UserID,
		SessionID: filter.SessionID,
		JobID:     filter.JobID,
		Limit:     0,
	})
	if err != nil {
		return SkillExecutionSummary{}, err
	}
	return summarizeSkillExecutions(filter.SkillName, records), nil
}

type SQLSkillExecutionStore struct {
	db      *sql.DB
	dialect SQLDialect
}

func NewSQLSkillExecutionStoreWithDialect(db *sql.DB, dialect SQLDialect) *SQLSkillExecutionStore {
	if dialect == "" {
		dialect = SQLDialectQuestion
	}
	return &SQLSkillExecutionStore{db: db, dialect: dialect}
}

func (s *SQLSkillExecutionStore) Init(ctx context.Context) error {
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS agent_skill_executions (
	id TEXT PRIMARY KEY,
	skill_name TEXT NOT NULL,
	user_id TEXT NOT NULL DEFAULT '',
	session_id TEXT NOT NULL DEFAULT '',
	job_id TEXT NOT NULL DEFAULT '',
	request_id TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL,
	error TEXT NOT NULL DEFAULT '',
	duration_ms BIGINT NOT NULL DEFAULT 0,
	metadata TEXT NOT NULL DEFAULT '{}',
	started_at ` + s.dialect.TimeType() + ` NOT NULL,
	completed_at ` + s.dialect.TimeType() + ` NOT NULL
)`,
		`CREATE INDEX IF NOT EXISTS idx_agent_skill_executions_skill_time ON agent_skill_executions (skill_name, completed_at)`,
		`CREATE INDEX IF NOT EXISTS idx_agent_skill_executions_status_time ON agent_skill_executions (status, completed_at)`,
		`CREATE INDEX IF NOT EXISTS idx_agent_skill_executions_user_time ON agent_skill_executions (user_id, completed_at)`,
	} {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return ensureReadableTimeColumns(ctx, s.db, s.dialect, "agent_skill_executions", "started_at", "completed_at")
}

func (s *SQLSkillExecutionStore) RecordSkillExecution(ctx context.Context, record SkillExecutionRecord) error {
	record = normalizeSkillExecutionRecord(record)
	metadata, err := json.Marshal(record.Metadata)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, s.dialect.Bind(`
INSERT INTO agent_skill_executions (id, skill_name, user_id, session_id, job_id, request_id, status, error, duration_ms, metadata, started_at, completed_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`),
		record.ID,
		record.SkillName,
		record.UserID,
		record.SessionID,
		record.JobID,
		record.RequestID,
		record.Status,
		record.Error,
		record.DurationMS,
		string(metadata),
		sqlTimeValue(record.StartedAt, s.dialect),
		sqlTimeValue(record.CompletedAt, s.dialect))
	return err
}

func (s *SQLSkillExecutionStore) ListSkillExecutions(ctx context.Context, filter SkillExecutionFilter) ([]SkillExecutionRecord, error) {
	filter = normalizeSkillExecutionFilter(filter)
	query := `SELECT id, skill_name, user_id, session_id, job_id, request_id, status, error, duration_ms, metadata, started_at, completed_at FROM agent_skill_executions`
	where, args := skillExecutionWhere(filter)
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY completed_at DESC"
	if filter.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filter.Limit)
	}
	rows, err := s.db.QueryContext(ctx, s.dialect.Bind(query), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var records []SkillExecutionRecord
	for rows.Next() {
		record, err := scanSkillExecutionRecord(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if records == nil {
		records = []SkillExecutionRecord{}
	}
	return records, rows.Err()
}

func (s *SQLSkillExecutionStore) SummarizeSkillExecutions(ctx context.Context, filter SkillExecutionFilter) (SkillExecutionSummary, error) {
	records, err := s.ListSkillExecutions(ctx, SkillExecutionFilter{
		SkillName: filter.SkillName,
		Status:    filter.Status,
		UserID:    filter.UserID,
		SessionID: filter.SessionID,
		JobID:     filter.JobID,
	})
	if err != nil {
		return SkillExecutionSummary{}, err
	}
	return summarizeSkillExecutions(filter.SkillName, records), nil
}

func skillExecutionWhere(filter SkillExecutionFilter) ([]string, []any) {
	var where []string
	var args []any
	if filter.SkillName != "" {
		where = append(where, "skill_name = ?")
		args = append(args, filter.SkillName)
	}
	if filter.Status != "" {
		where = append(where, "status = ?")
		args = append(args, filter.Status)
	}
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
	return where, args
}

func scanSkillExecutionRecord(row skillRegistryScanner) (SkillExecutionRecord, error) {
	var record SkillExecutionRecord
	var metadata string
	var startedAt, completedAt any
	if err := row.Scan(&record.ID, &record.SkillName, &record.UserID, &record.SessionID, &record.JobID, &record.RequestID, &record.Status, &record.Error, &record.DurationMS, &metadata, &startedAt, &completedAt); err != nil {
		return SkillExecutionRecord{}, err
	}
	_ = json.Unmarshal([]byte(metadata), &record.Metadata)
	var err error
	if record.StartedAt, err = parseSQLTime(startedAt); err != nil {
		return SkillExecutionRecord{}, err
	}
	if record.CompletedAt, err = parseSQLTime(completedAt); err != nil {
		return SkillExecutionRecord{}, err
	}
	return normalizeSkillExecutionRecord(record), nil
}

func normalizeSkillExecutionRecord(record SkillExecutionRecord) SkillExecutionRecord {
	record.ID = strings.TrimSpace(record.ID)
	if record.ID == "" {
		record.ID = newSkillExecutionID()
	}
	record.SkillName = strings.TrimSpace(record.SkillName)
	record.UserID = strings.TrimSpace(record.UserID)
	record.SessionID = strings.TrimSpace(record.SessionID)
	record.JobID = strings.TrimSpace(record.JobID)
	record.RequestID = strings.TrimSpace(record.RequestID)
	record.Status = normalizeSkillExecutionStatus(record.Status)
	record.Error = truncateSkillExecutionString(strings.TrimSpace(record.Error), 2048)
	if record.Metadata == nil {
		record.Metadata = map[string]any{}
	}
	if record.StartedAt.IsZero() {
		record.StartedAt = time.Now().UTC()
	}
	if record.CompletedAt.IsZero() {
		record.CompletedAt = record.StartedAt
	}
	if record.DurationMS < 0 {
		record.DurationMS = 0
	}
	return record
}

func normalizeSkillExecutionFilter(filter SkillExecutionFilter) SkillExecutionFilter {
	filter.SkillName = strings.TrimSpace(filter.SkillName)
	filter.Status = normalizeOptionalSkillExecutionStatus(filter.Status)
	filter.UserID = strings.TrimSpace(filter.UserID)
	filter.SessionID = strings.TrimSpace(filter.SessionID)
	filter.JobID = strings.TrimSpace(filter.JobID)
	if filter.Limit < 0 {
		filter.Limit = 0
	}
	if filter.Limit > 500 {
		filter.Limit = 500
	}
	return filter
}

func normalizeSkillExecutionStatus(status string) string {
	status = normalizeOptionalSkillExecutionStatus(status)
	if status == "" {
		return SkillExecutionStatusFailed
	}
	return status
}

func normalizeOptionalSkillExecutionStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case SkillExecutionStatusSucceeded:
		return SkillExecutionStatusSucceeded
	case SkillExecutionStatusFailed:
		return SkillExecutionStatusFailed
	default:
		return ""
	}
}

func skillExecutionMatches(record SkillExecutionRecord, filter SkillExecutionFilter) bool {
	if filter.SkillName != "" && record.SkillName != filter.SkillName {
		return false
	}
	if filter.Status != "" && record.Status != filter.Status {
		return false
	}
	if filter.UserID != "" && record.UserID != filter.UserID {
		return false
	}
	if filter.SessionID != "" && record.SessionID != filter.SessionID {
		return false
	}
	if filter.JobID != "" && record.JobID != filter.JobID {
		return false
	}
	return true
}

func summarizeSkillExecutions(skillName string, records []SkillExecutionRecord) SkillExecutionSummary {
	summary := SkillExecutionSummary{SkillName: strings.TrimSpace(skillName)}
	var totalLatency int64
	for _, record := range records {
		summary.Total++
		totalLatency += record.DurationMS
		switch record.Status {
		case SkillExecutionStatusSucceeded:
			summary.Succeeded++
		case SkillExecutionStatusFailed:
			summary.Failed++
		}
	}
	if summary.Total > 0 {
		summary.FailureRate = float64(summary.Failed) / float64(summary.Total)
		summary.AverageLatencyMS = totalLatency / int64(summary.Total)
	}
	return summary
}

func truncateSkillExecutionString(value string, max int) string {
	if max <= 0 || len(value) <= max {
		return value
	}
	return value[:max]
}

func newSkillExecutionID() string {
	var data [8]byte
	if _, err := rand.Read(data[:]); err != nil {
		return hex.EncodeToString([]byte(time.Now().UTC().Format(time.RFC3339Nano)))
	}
	return "skexec_" + hex.EncodeToString(data[:])
}
