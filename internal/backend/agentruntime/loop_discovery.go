package agentruntime

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

func NewLoopTriggerID() string {
	return "loop-trigger-" + newSortableID()
}

type MemoryLoopTriggerStore struct {
	mu       sync.Mutex
	triggers map[string]LoopTriggerRecord
	dedupe   map[string]string
}

func NewMemoryLoopTriggerStore() *MemoryLoopTriggerStore {
	return &MemoryLoopTriggerStore{
		triggers: make(map[string]LoopTriggerRecord),
		dedupe:   make(map[string]string),
	}
}

func (s *MemoryLoopTriggerStore) Init(context.Context) error { return nil }

func (s *MemoryLoopTriggerStore) CreateTrigger(_ context.Context, trigger LoopTriggerRecord) (LoopTriggerRecord, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.triggers == nil {
		s.triggers = make(map[string]LoopTriggerRecord)
	}
	if s.dedupe == nil {
		s.dedupe = make(map[string]string)
	}
	if existingID := s.dedupe[trigger.DedupeKey]; existingID != "" {
		existing := cloneLoopTriggerRecord(s.triggers[existingID])
		existing.Status = firstNonEmptyString(existing.Status, LoopTriggerStatusSkipped)
		return existing, false, nil
	}
	s.triggers[trigger.ID] = cloneLoopTriggerRecord(trigger)
	s.dedupe[trigger.DedupeKey] = trigger.ID
	return cloneLoopTriggerRecord(trigger), true, nil
}

func (s *MemoryLoopTriggerStore) UpdateTriggerJob(_ context.Context, userID, triggerID, sessionID, jobID, loopGoalID, status, failureReason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	trigger, ok := s.triggers[triggerID]
	if !ok || trigger.UserID != userID {
		return sql.ErrNoRows
	}
	trigger.SessionID = firstNonEmptyString(sessionID, trigger.SessionID)
	trigger.JobID = jobID
	trigger.LoopGoalID = loopGoalID
	trigger.Status = firstNonEmptyString(status, trigger.Status)
	trigger.FailureReason = failureReason
	s.triggers[triggerID] = trigger
	return nil
}

func (s *MemoryLoopTriggerStore) ListTriggers(_ context.Context, userID, sessionID string, limit int) ([]LoopTriggerRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]LoopTriggerRecord, 0)
	for _, trigger := range s.triggers {
		if trigger.UserID != userID {
			continue
		}
		if strings.TrimSpace(sessionID) != "" && trigger.SessionID != sessionID {
			continue
		}
		out = append(out, cloneLoopTriggerRecord(trigger))
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *MemoryLoopTriggerStore) DeleteSession(_ context.Context, userID, sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, trigger := range s.triggers {
		if trigger.UserID == userID && trigger.SessionID == sessionID {
			delete(s.triggers, id)
			delete(s.dedupe, trigger.DedupeKey)
		}
	}
	return nil
}

func (s *MemoryLoopTriggerStore) DeleteUser(_ context.Context, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, trigger := range s.triggers {
		if trigger.UserID == userID {
			delete(s.triggers, id)
			delete(s.dedupe, trigger.DedupeKey)
		}
	}
	return nil
}

type SQLLoopTriggerStore struct {
	db      *sql.DB
	dialect SQLDialect
}

func NewSQLLoopTriggerStoreWithDialect(db *sql.DB, dialect SQLDialect) *SQLLoopTriggerStore {
	if dialect == "" {
		dialect = SQLDialectQuestion
	}
	return &SQLLoopTriggerStore{db: db, dialect: dialect}
}

func (s *SQLLoopTriggerStore) Init(ctx context.Context) error {
	return requireSQLColumns(ctx, s.db, "agent_loop_triggers",
		"id", "user_id", "session_id", "dedupe_key", "trigger_type", "source", "payload_json", "job_id",
		"loop_goal_id", "status", "failure_reason", "created_at", "expires_at",
	)
}

func (s *SQLLoopTriggerStore) CreateTrigger(ctx context.Context, trigger LoopTriggerRecord) (LoopTriggerRecord, bool, error) {
	payload, err := json.Marshal(trigger.Payload)
	if err != nil {
		return LoopTriggerRecord{}, false, err
	}
	if s.dialect == SQLDialectPostgres {
		result, err := s.db.ExecContext(ctx, s.dialect.Bind(`
INSERT INTO agent_loop_triggers (id, user_id, session_id, dedupe_key, trigger_type, source, payload_json, job_id, loop_goal_id, status, failure_reason, created_at, expires_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (dedupe_key) DO NOTHING`),
			trigger.ID, trigger.UserID, trigger.SessionID, trigger.DedupeKey, trigger.TriggerType, trigger.Source, string(payload),
			trigger.JobID, trigger.LoopGoalID, trigger.Status, trigger.FailureReason, sqlTimeValue(trigger.CreatedAt, s.dialect), sqlTimeValue(trigger.ExpiresAt, s.dialect))
		if err != nil {
			return LoopTriggerRecord{}, false, err
		}
		if rows, _ := result.RowsAffected(); rows > 0 {
			return trigger, true, nil
		}
		existing, err := s.getByDedupeKey(ctx, trigger.DedupeKey)
		return existing, false, err
	}
	_, err = s.db.ExecContext(ctx, s.dialect.Bind(`
INSERT INTO agent_loop_triggers (id, user_id, session_id, dedupe_key, trigger_type, source, payload_json, job_id, loop_goal_id, status, failure_reason, created_at, expires_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`),
		trigger.ID, trigger.UserID, trigger.SessionID, trigger.DedupeKey, trigger.TriggerType, trigger.Source, string(payload),
		trigger.JobID, trigger.LoopGoalID, trigger.Status, trigger.FailureReason, sqlTimeValue(trigger.CreatedAt, s.dialect), sqlTimeValue(trigger.ExpiresAt, s.dialect))
	if err != nil {
		existing, getErr := s.getByDedupeKey(ctx, trigger.DedupeKey)
		if getErr == nil {
			return existing, false, nil
		}
		return LoopTriggerRecord{}, false, err
	}
	return trigger, true, nil
}

func (s *SQLLoopTriggerStore) UpdateTriggerJob(ctx context.Context, userID, triggerID, sessionID, jobID, loopGoalID, status, failureReason string) error {
	_, err := s.db.ExecContext(ctx, s.dialect.Bind(`
UPDATE agent_loop_triggers
SET session_id = ?, job_id = ?, loop_goal_id = ?, status = ?, failure_reason = ?
WHERE user_id = ? AND id = ?`),
		sessionID, jobID, loopGoalID, status, failureReason, userID, triggerID)
	return err
}

func (s *SQLLoopTriggerStore) ListTriggers(ctx context.Context, userID, sessionID string, limit int) ([]LoopTriggerRecord, error) {
	if limit <= 0 {
		limit = 50
	}
	query := `
SELECT id, user_id, session_id, dedupe_key, trigger_type, source, payload_json, job_id, loop_goal_id, status, failure_reason, created_at, expires_at
FROM agent_loop_triggers
WHERE user_id = ?`
	args := []any{userID}
	if strings.TrimSpace(sessionID) != "" {
		query += ` AND session_id = ?`
		args = append(args, sessionID)
	}
	query += ` ORDER BY created_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, s.dialect.Bind(query), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanLoopTriggerRows(rows)
}

func (s *SQLLoopTriggerStore) DeleteSession(ctx context.Context, userID, sessionID string) error {
	_, err := s.db.ExecContext(ctx, s.dialect.Bind(`DELETE FROM agent_loop_triggers WHERE user_id = ? AND session_id = ?`), userID, sessionID)
	return err
}

func (s *SQLLoopTriggerStore) DeleteUser(ctx context.Context, userID string) error {
	_, err := s.db.ExecContext(ctx, s.dialect.Bind(`DELETE FROM agent_loop_triggers WHERE user_id = ?`), userID)
	return err
}

func (s *SQLLoopTriggerStore) getByDedupeKey(ctx context.Context, dedupeKey string) (LoopTriggerRecord, error) {
	row := s.db.QueryRowContext(ctx, s.dialect.Bind(`
SELECT id, user_id, session_id, dedupe_key, trigger_type, source, payload_json, job_id, loop_goal_id, status, failure_reason, created_at, expires_at
FROM agent_loop_triggers
WHERE dedupe_key = ?`), dedupeKey)
	return scanLoopTrigger(row)
}

type loopTriggerScanner interface {
	Scan(dest ...any) error
}

func scanLoopTrigger(row loopTriggerScanner) (LoopTriggerRecord, error) {
	var trigger LoopTriggerRecord
	var payloadRaw any
	if err := row.Scan(
		&trigger.ID,
		&trigger.UserID,
		&trigger.SessionID,
		&trigger.DedupeKey,
		&trigger.TriggerType,
		&trigger.Source,
		&payloadRaw,
		&trigger.JobID,
		&trigger.LoopGoalID,
		&trigger.Status,
		&trigger.FailureReason,
		&trigger.CreatedAt,
		&trigger.ExpiresAt,
	); err != nil {
		return LoopTriggerRecord{}, err
	}
	trigger.Payload = decodeSQLJSONMap(payloadRaw)
	return trigger, nil
}

func scanLoopTriggerRows(rows *sql.Rows) ([]LoopTriggerRecord, error) {
	var out []LoopTriggerRecord
	for rows.Next() {
		trigger, err := scanLoopTrigger(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, trigger)
	}
	return out, rows.Err()
}

func (r *Runtime) SetLoopTriggerStore(store LoopTriggerStore) {
	if r == nil {
		return
	}
	if store == nil {
		store = NewMemoryLoopTriggerStore()
	}
	r.loopTriggers = store
}

func (r *Runtime) SubmitLoopDiscoveryEvent(ctx context.Context, event LoopDiscoveryEvent) (LoopDiscoveryResult, error) {
	if r == nil {
		return LoopDiscoveryResult{}, fmt.Errorf("runtime is not configured")
	}
	if r.loopTriggers == nil {
		r.loopTriggers = NewMemoryLoopTriggerStore()
	}
	normalized, err := normalizeLoopDiscoveryEvent(event)
	if err != nil {
		return LoopDiscoveryResult{}, err
	}
	if reason := r.loopDiscoveryBlockReason(normalized.TriggerType); reason != "" {
		return LoopDiscoveryResult{}, fmt.Errorf("%w: %s", ErrLoopDiscoveryBlocked, reason)
	}
	now := time.Now().UTC()
	ttl := r.config.LoopDiscovery.TriggerTTL
	if ttl <= 0 {
		ttl = 7 * 24 * time.Hour
	}
	trigger := LoopTriggerRecord{
		ID:          NewLoopTriggerID(),
		UserID:      normalized.UserID,
		SessionID:   normalized.SessionID,
		DedupeKey:   normalized.DedupeKey,
		TriggerType: normalized.TriggerType,
		Source:      normalized.Source,
		Payload:     normalized.Payload,
		Status:      LoopTriggerStatusStarted,
		CreatedAt:   now,
		ExpiresAt:   now.Add(ttl),
	}
	trigger, created, err := r.loopTriggers.CreateTrigger(ctx, trigger)
	if err != nil {
		return LoopDiscoveryResult{}, err
	}
	if !created {
		return LoopDiscoveryResult{Trigger: trigger, Duplicate: true}, nil
	}
	sessionID := normalized.SessionID
	if strings.TrimSpace(sessionID) == "" {
		session, err := r.CreateSession(ctx, normalized.UserID, "")
		if err != nil {
			_ = r.loopTriggers.UpdateTriggerJob(ctx, trigger.UserID, trigger.ID, "", "", "", LoopTriggerStatusFailed, err.Error())
			trigger.Status = LoopTriggerStatusFailed
			trigger.FailureReason = err.Error()
			return LoopDiscoveryResult{Trigger: trigger}, err
		}
		sessionID = session.ID
	}
	job, err := r.CreateJob(ctx, ChatRequest{
		UserID:    normalized.UserID,
		SessionID: sessionID,
		Content:   normalized.Objective,
	}, JobTypeDeepAgent)
	if err != nil {
		_ = r.loopTriggers.UpdateTriggerJob(ctx, trigger.UserID, trigger.ID, sessionID, "", "", LoopTriggerStatusFailed, err.Error())
		trigger.SessionID = sessionID
		trigger.Status = LoopTriggerStatusFailed
		trigger.FailureReason = err.Error()
		return LoopDiscoveryResult{Trigger: trigger}, err
	}
	if err := r.loopTriggers.UpdateTriggerJob(ctx, trigger.UserID, trigger.ID, sessionID, job.ID, job.LoopGoalID, LoopTriggerStatusStarted, ""); err != nil {
		return LoopDiscoveryResult{}, err
	}
	trigger.SessionID = sessionID
	trigger.JobID = job.ID
	trigger.LoopGoalID = job.LoopGoalID
	if err := r.StartJob(ctx, job); err != nil {
		_ = r.loopTriggers.UpdateTriggerJob(ctx, trigger.UserID, trigger.ID, sessionID, job.ID, job.LoopGoalID, LoopTriggerStatusFailed, err.Error())
		trigger.Status = LoopTriggerStatusFailed
		trigger.FailureReason = err.Error()
		return LoopDiscoveryResult{Trigger: trigger, Job: job}, err
	}
	return LoopDiscoveryResult{Trigger: trigger, Job: job}, nil
}

func (r *Runtime) ListLoopTriggers(ctx context.Context, userID, sessionID string, limit int) ([]LoopTriggerRecord, error) {
	if r == nil || r.loopTriggers == nil {
		return []LoopTriggerRecord{}, nil
	}
	return r.loopTriggers.ListTriggers(ctx, userID, sessionID, limit)
}

var ErrLoopDiscoveryBlocked = errors.New("loop discovery blocked")

func (r *Runtime) loopDiscoveryBlockReason(triggerType string) string {
	if triggerType == LoopDiscoveryManual {
		return ""
	}
	cfg := r.config.LoopDiscovery
	automationEnabled := cfg.AutomationEnabled
	if r != nil && r.config.LLMGovernanceProvider != nil {
		automationEnabled = r.config.LLMGovernanceProvider().normalized().AutomaticTriggerEnabled
	}
	if !automationEnabled {
		return "loop automation kill switch is disabled"
	}
	switch triggerType {
	case LoopDiscoverySchedule:
		if !cfg.ScheduleTriggersEnabled {
			return "schedule loop triggers are disabled"
		}
	case LoopDiscoveryWebhook:
		if !cfg.WebhookTriggersEnabled {
			return "webhook loop triggers are disabled"
		}
	case LoopDiscoveryMonitor:
		if !cfg.MonitorTriggersEnabled {
			return "monitor loop triggers are disabled"
		}
	case LoopDiscoveryEvalFailure:
		if !cfg.EvalRepairTriggersEnabled {
			return "eval failure loop triggers are disabled"
		}
	case LoopDiscoveryConnectorEvent:
		if !cfg.ConnectorTriggersEnabled {
			return "connector event loop triggers are disabled"
		}
	}
	return ""
}

func normalizeLoopDiscoveryEvent(event LoopDiscoveryEvent) (LoopDiscoveryEvent, error) {
	event.UserID = strings.TrimSpace(event.UserID)
	event.SessionID = strings.TrimSpace(event.SessionID)
	event.TriggerType = normalizeLoopTriggerType(event.TriggerType)
	event.Source = strings.TrimSpace(event.Source)
	event.DedupeKey = strings.TrimSpace(event.DedupeKey)
	event.Objective = strings.TrimSpace(event.Objective)
	if event.UserID == "" {
		return LoopDiscoveryEvent{}, fmt.Errorf("user ID is required")
	}
	if event.TriggerType == "" {
		return LoopDiscoveryEvent{}, fmt.Errorf("trigger_type is required")
	}
	if event.Objective == "" {
		event.Objective = objectiveFromLoopPayload(event.Payload)
	}
	if event.Objective == "" {
		return LoopDiscoveryEvent{}, fmt.Errorf("objective is required")
	}
	if event.Payload == nil {
		event.Payload = map[string]any{}
	}
	if event.Source == "" {
		event.Source = event.TriggerType
	}
	if event.DedupeKey == "" {
		event.DedupeKey = buildLoopDedupeKey(event)
	}
	return event, nil
}

func normalizeLoopTriggerType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", LoopDiscoveryManual:
		return LoopDiscoveryManual
	case LoopDiscoverySchedule:
		return LoopDiscoverySchedule
	case LoopDiscoveryWebhook:
		return LoopDiscoveryWebhook
	case LoopDiscoveryMonitor:
		return LoopDiscoveryMonitor
	case LoopDiscoveryEvalFailure, "eval-failure":
		return LoopDiscoveryEvalFailure
	case LoopDiscoveryConnectorEvent, "connector-event":
		return LoopDiscoveryConnectorEvent
	default:
		return ""
	}
}

func objectiveFromLoopPayload(payload map[string]any) string {
	for _, key := range []string{"objective", "prompt", "content", "title"} {
		if value, ok := payload[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func buildLoopDedupeKey(event LoopDiscoveryEvent) string {
	payload, _ := json.Marshal(event.Payload)
	sum := sha256.Sum256([]byte(strings.Join([]string{
		event.UserID,
		event.SessionID,
		event.TriggerType,
		event.Source,
		event.Objective,
		string(payload),
	}, "\x00")))
	return event.TriggerType + ":" + hex.EncodeToString(sum[:])
}

func cloneLoopTriggerRecord(trigger LoopTriggerRecord) LoopTriggerRecord {
	if trigger.Payload != nil {
		trigger.Payload = cloneWorkflowMap(trigger.Payload)
	}
	return trigger
}

func decodeSQLJSONMap(raw any) map[string]any {
	var data []byte
	switch value := raw.(type) {
	case []byte:
		data = value
	case string:
		data = []byte(value)
	case nil:
		return map[string]any{}
	default:
		data, _ = json.Marshal(value)
	}
	var out map[string]any
	if len(data) == 0 || json.Unmarshal(data, &out) != nil {
		return map[string]any{}
	}
	return out
}
