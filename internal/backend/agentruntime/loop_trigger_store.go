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
)

const (
	LoopTriggerStatusStarted = "started"
	LoopTriggerStatusFailed  = "failed"
)

var ErrLoopTriggerDuplicate = errors.New("duplicate loop trigger")

type LoopTriggerStore interface {
	Init(ctx context.Context) error
	GetActiveLoopTrigger(ctx context.Context, dedupeKey string, now time.Time) (LoopTriggerResult, bool, error)
	UpsertLoopTrigger(ctx context.Context, result LoopTriggerResult, expiresAt time.Time) error
	PruneExpiredLoopTriggers(ctx context.Context, now time.Time) (int, error)
	LoopTriggerLedgerStats(ctx context.Context, since time.Time) (LoopTriggerLedgerStats, error)
}

type LoopTriggerLedgerStat struct {
	Key   string `json:"key"`
	Count int    `json:"count"`
}

type LoopTriggerLedgerStats struct {
	Total       int                     `json:"total"`
	Active      int                     `json:"active"`
	Expired     int                     `json:"expired"`
	ByType      []LoopTriggerLedgerStat `json:"by_type,omitempty"`
	BySource    []LoopTriggerLedgerStat `json:"by_source,omitempty"`
	ByStatus    []LoopTriggerLedgerStat `json:"by_status,omitempty"`
	WindowStart time.Time               `json:"window_start,omitempty"`
	GeneratedAt time.Time               `json:"generated_at"`
	LastError   string                  `json:"last_error,omitempty"`
}

type MemoryLoopTriggerStore struct {
	mu       sync.Mutex
	triggers map[string]loopTriggerDedupeEntry
}

func NewMemoryLoopTriggerStore() *MemoryLoopTriggerStore {
	return &MemoryLoopTriggerStore{triggers: make(map[string]loopTriggerDedupeEntry)}
}

func (s *MemoryLoopTriggerStore) Init(context.Context) error { return nil }

func (s *MemoryLoopTriggerStore) GetActiveLoopTrigger(_ context.Context, dedupeKey string, now time.Time) (LoopTriggerResult, bool, error) {
	dedupeKey = strings.TrimSpace(dedupeKey)
	if s == nil || dedupeKey == "" {
		return LoopTriggerResult{}, false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.triggers[dedupeKey]
	if !ok || !entry.expiresAt.After(now) {
		delete(s.triggers, dedupeKey)
		return LoopTriggerResult{}, false, nil
	}
	return cloneLoopTriggerResult(entry.result), true, nil
}

func (s *MemoryLoopTriggerStore) UpsertLoopTrigger(_ context.Context, result LoopTriggerResult, expiresAt time.Time) error {
	if s == nil || strings.TrimSpace(result.Trigger.DedupeKey) == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.triggers == nil {
		s.triggers = make(map[string]loopTriggerDedupeEntry)
	}
	s.triggers[result.Trigger.DedupeKey] = loopTriggerDedupeEntry{result: cloneLoopTriggerResult(result), expiresAt: expiresAt}
	return nil
}

func (s *MemoryLoopTriggerStore) PruneExpiredLoopTriggers(_ context.Context, now time.Time) (int, error) {
	if s == nil {
		return 0, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	pruned := 0
	for key, entry := range s.triggers {
		if !entry.expiresAt.After(now) {
			delete(s.triggers, key)
			pruned++
		}
	}
	return pruned, nil
}

func (s *MemoryLoopTriggerStore) LoopTriggerLedgerStats(_ context.Context, since time.Time) (LoopTriggerLedgerStats, error) {
	if s == nil {
		return LoopTriggerLedgerStats{}, nil
	}
	now := time.Now().UTC()
	stats := LoopTriggerLedgerStats{WindowStart: since, GeneratedAt: now}
	byType := map[string]int{}
	bySource := map[string]int{}
	byStatus := map[string]int{}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, entry := range s.triggers {
		trigger := entry.result.Trigger
		if !since.IsZero() && trigger.CreatedAt.Before(since) {
			continue
		}
		stats.Total++
		if entry.expiresAt.After(now) {
			stats.Active++
		} else {
			stats.Expired++
		}
		byType[firstNonEmptyString(trigger.Type, "unknown")]++
		bySource[firstNonEmptyString(trigger.Source, "unknown")]++
		byStatus[firstNonEmptyString(trigger.Status, "unknown")]++
	}
	stats.ByType = loopTriggerLedgerStatPairs(byType)
	stats.BySource = loopTriggerLedgerStatPairs(bySource)
	stats.ByStatus = loopTriggerLedgerStatPairs(byStatus)
	return stats, nil
}

type SQLLoopTriggerStore struct {
	db      *sql.DB
	dialect SQLDialect
}

func NewSQLLoopTriggerStoreWithDialect(db *sql.DB, dialect SQLDialect) *SQLLoopTriggerStore {
	return &SQLLoopTriggerStore{db: db, dialect: dialect}
}

func (s *SQLLoopTriggerStore) Init(ctx context.Context) error {
	return requireSQLColumns(ctx, s.db, "agent_loop_triggers",
		"id", "user_id", "session_id", "dedupe_key", "trigger_type", "source", "payload_json",
		"job_id", "loop_goal_id", "status", "created_at", "expires_at")
}

func (s *SQLLoopTriggerStore) GetActiveLoopTrigger(ctx context.Context, dedupeKey string, now time.Time) (LoopTriggerResult, bool, error) {
	dedupeKey = strings.TrimSpace(dedupeKey)
	if s == nil || s.db == nil || dedupeKey == "" {
		return LoopTriggerResult{}, false, nil
	}
	result, expiresAt, err := scanSQLLoopTriggerResult(s.db.QueryRowContext(ctx, s.dialect.Bind(`
SELECT id, user_id, session_id, dedupe_key, trigger_type, source, payload_json, job_id, loop_goal_id, status, created_at, expires_at
FROM agent_loop_triggers
WHERE dedupe_key = ? AND expires_at > ?
`), dedupeKey, sqlTimeValue(now, s.dialect)))
	if errors.Is(err, sql.ErrNoRows) {
		return LoopTriggerResult{}, false, nil
	}
	if err != nil {
		return LoopTriggerResult{}, false, err
	}
	result.Trigger.ExpiresAt = expiresAt
	return result, true, nil
}

func (s *SQLLoopTriggerStore) UpsertLoopTrigger(ctx context.Context, result LoopTriggerResult, expiresAt time.Time) error {
	if s == nil || s.db == nil || strings.TrimSpace(result.Trigger.DedupeKey) == "" {
		return nil
	}
	payload, err := json.Marshal(result.Trigger.Payload)
	if err != nil {
		return err
	}
	resultExec, err := s.db.ExecContext(ctx, s.dialect.Bind(`
INSERT INTO agent_loop_triggers (
	id, user_id, session_id, dedupe_key, trigger_type, source, payload_json,
	job_id, loop_goal_id, status, created_at, expires_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (dedupe_key) DO UPDATE SET
	user_id = EXCLUDED.user_id,
	session_id = EXCLUDED.session_id,
	trigger_type = EXCLUDED.trigger_type,
	source = EXCLUDED.source,
	payload_json = EXCLUDED.payload_json,
	job_id = EXCLUDED.job_id,
	loop_goal_id = EXCLUDED.loop_goal_id,
	status = EXCLUDED.status,
	created_at = EXCLUDED.created_at,
	expires_at = EXCLUDED.expires_at
WHERE agent_loop_triggers.expires_at <= EXCLUDED.created_at
`),
		firstNonEmptyString(result.Trigger.ID, NewLoopTriggerID()),
		firstNonEmptyString(result.Trigger.UserID, triggerResultUserID(result)),
		firstNonEmptyString(result.Trigger.SessionID, triggerResultSessionID(result)),
		result.Trigger.DedupeKey,
		result.Trigger.Type,
		result.Trigger.Source,
		string(payload),
		triggerResultJobID(result),
		triggerResultGoalID(result),
		firstNonEmptyString(result.Trigger.Status, LoopTriggerStatusStarted),
		sqlTimeValue(result.Trigger.CreatedAt, s.dialect),
		sqlTimeValue(expiresAt, s.dialect),
	)
	if err != nil {
		return err
	}
	if rows, err := resultExec.RowsAffected(); err == nil && rows == 0 {
		return ErrLoopTriggerDuplicate
	}
	return err
}

func (s *SQLLoopTriggerStore) PruneExpiredLoopTriggers(ctx context.Context, now time.Time) (int, error) {
	if s == nil || s.db == nil {
		return 0, nil
	}
	result, err := s.db.ExecContext(ctx, s.dialect.Bind(`DELETE FROM agent_loop_triggers WHERE expires_at <= ?`), sqlTimeValue(now, s.dialect))
	if err != nil {
		return 0, err
	}
	rows, _ := result.RowsAffected()
	return int(rows), nil
}

func (s *SQLLoopTriggerStore) LoopTriggerLedgerStats(ctx context.Context, since time.Time) (LoopTriggerLedgerStats, error) {
	if s == nil || s.db == nil {
		return LoopTriggerLedgerStats{}, nil
	}
	now := time.Now().UTC()
	stats := LoopTriggerLedgerStats{WindowStart: since, GeneratedAt: now}
	where := ""
	args := []any{}
	if !since.IsZero() {
		where = " WHERE created_at >= ?"
		args = append(args, sqlTimeValue(since, s.dialect))
	}
	if err := s.db.QueryRowContext(ctx, s.dialect.Bind(`SELECT COUNT(*), COALESCE(SUM(CASE WHEN expires_at > ? THEN 1 ELSE 0 END), 0), COALESCE(SUM(CASE WHEN expires_at <= ? THEN 1 ELSE 0 END), 0) FROM agent_loop_triggers`+where), append([]any{sqlTimeValue(now, s.dialect), sqlTimeValue(now, s.dialect)}, args...)...).Scan(&stats.Total, &stats.Active, &stats.Expired); err != nil {
		return LoopTriggerLedgerStats{}, err
	}
	var err error
	stats.ByType, err = s.loopTriggerLedgerGroup(ctx, "trigger_type", where, args)
	if err != nil {
		return LoopTriggerLedgerStats{}, err
	}
	stats.BySource, err = s.loopTriggerLedgerGroup(ctx, "source", where, args)
	if err != nil {
		return LoopTriggerLedgerStats{}, err
	}
	stats.ByStatus, err = s.loopTriggerLedgerGroup(ctx, "status", where, args)
	if err != nil {
		return LoopTriggerLedgerStats{}, err
	}
	return stats, nil
}

func (s *SQLLoopTriggerStore) loopTriggerLedgerGroup(ctx context.Context, column, where string, args []any) ([]LoopTriggerLedgerStat, error) {
	query := fmt.Sprintf(`SELECT COALESCE(NULLIF(%s, ''), 'unknown'), COUNT(*) FROM agent_loop_triggers%s GROUP BY %s ORDER BY COUNT(*) DESC, %s ASC`, column, where, column, column)
	rows, err := s.db.QueryContext(ctx, s.dialect.Bind(query), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LoopTriggerLedgerStat{}
	for rows.Next() {
		var item LoopTriggerLedgerStat
		if err := rows.Scan(&item.Key, &item.Count); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

type loopTriggerScanner interface {
	Scan(dest ...any) error
}

func scanSQLLoopTriggerResult(row loopTriggerScanner) (LoopTriggerResult, time.Time, error) {
	var record LoopTriggerRecord
	var payloadJSON string
	var jobID, goalID string
	var expiresAt time.Time
	if err := row.Scan(
		&record.ID,
		&record.UserID,
		&record.SessionID,
		&record.DedupeKey,
		&record.Type,
		&record.Source,
		&payloadJSON,
		&jobID,
		&goalID,
		&record.Status,
		&record.CreatedAt,
		&expiresAt,
	); err != nil {
		return LoopTriggerResult{}, time.Time{}, err
	}
	if strings.TrimSpace(payloadJSON) != "" {
		_ = json.Unmarshal([]byte(payloadJSON), &record.Payload)
	}
	result := LoopTriggerResult{
		Job:     &Job{ID: jobID, UserID: record.UserID, SessionID: record.SessionID, LoopGoalID: goalID, Type: JobTypeDeepAgent},
		Goal:    &LoopGoal{ID: goalID, UserID: record.UserID, SessionID: record.SessionID},
		Trigger: record,
	}
	if jobID == "" {
		result.Job = nil
	}
	if goalID == "" {
		result.Goal = nil
	}
	return result, expiresAt, nil
}

func NewLoopTriggerID() string {
	return "ltr-" + newSortableID()
}

func loopTriggerLedgerStatPairs(values map[string]int) []LoopTriggerLedgerStat {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]LoopTriggerLedgerStat, 0, len(keys))
	for _, key := range keys {
		out = append(out, LoopTriggerLedgerStat{Key: key, Count: values[key]})
	}
	return out
}

func triggerResultUserID(result LoopTriggerResult) string {
	if result.Job != nil {
		return result.Job.UserID
	}
	if result.Goal != nil {
		return result.Goal.UserID
	}
	return ""
}

func triggerResultSessionID(result LoopTriggerResult) string {
	if result.Job != nil {
		return result.Job.SessionID
	}
	if result.Goal != nil {
		return result.Goal.SessionID
	}
	return ""
}

func triggerResultJobID(result LoopTriggerResult) string {
	if result.Job == nil {
		return ""
	}
	return result.Job.ID
}

func triggerResultGoalID(result LoopTriggerResult) string {
	if result.Goal != nil {
		return result.Goal.ID
	}
	if result.Job != nil {
		return result.Job.LoopGoalID
	}
	return ""
}
