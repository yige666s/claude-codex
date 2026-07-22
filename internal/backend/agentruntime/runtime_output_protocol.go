package agentruntime

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	StructuredOutputEventType     = "structured_output"
	StructuredOutputSchemaVersion = "agentapi_structured_output.v1"
)

type MessageStructuredOutput struct {
	ID            string          `json:"id"`
	MessageID     string          `json:"message_id,omitempty"`
	UserID        string          `json:"user_id,omitempty"`
	SessionID     string          `json:"session_id,omitempty"`
	RunID         string          `json:"run_id,omitempty"`
	Kind          string          `json:"kind"`
	SchemaVersion string          `json:"version"`
	Title         string          `json:"title,omitempty"`
	Summary       string          `json:"summary,omitempty"`
	Payload       json.RawMessage `json:"payload,omitempty"`
	Source        string          `json:"source,omitempty"`
	CreatedAt     time.Time       `json:"created_at,omitempty"`
}

type StructuredOutputStore interface {
	Init(context.Context) error
	SaveStructuredOutput(context.Context, MessageStructuredOutput) (MessageStructuredOutput, error)
	ListStructuredOutputsBySession(context.Context, string, string) ([]MessageStructuredOutput, error)
	ListStructuredOutputsByRun(context.Context, string, string) ([]MessageStructuredOutput, error)
}

type ChatRunSnapshot struct {
	RunID                 string          `json:"run_id"`
	UserID                string          `json:"user_id,omitempty"`
	SessionID             string          `json:"session_id,omitempty"`
	Status                string          `json:"status"`
	FinalMessageID        string          `json:"final_message_id,omitempty"`
	FinalContent          string          `json:"final_content,omitempty"`
	EventCount            int             `json:"event_count"`
	StructuredOutputCount int             `json:"structured_output_count"`
	ArtifactCount         int             `json:"artifact_count"`
	Error                 string          `json:"error,omitempty"`
	LastEventID           string          `json:"last_event_id,omitempty"`
	Payload               json.RawMessage `json:"payload_json,omitempty"`
	CreatedAt             time.Time       `json:"created_at,omitempty"`
	UpdatedAt             time.Time       `json:"updated_at,omitempty"`
}

type ChatRunSnapshotStore interface {
	Init(context.Context) error
	SaveChatRunSnapshot(context.Context, ChatRunSnapshot) error
	GetChatRunSnapshot(context.Context, string, string) (ChatRunSnapshot, error)
}

type ChatTurnReservation struct {
	UserID             string    `json:"user_id,omitempty"`
	SessionID          string    `json:"session_id,omitempty"`
	IdempotencyKey     string    `json:"idempotency_key"`
	RunID              string    `json:"run_id"`
	UserMessageID      string    `json:"user_message_id,omitempty"`
	AssistantMessageID string    `json:"assistant_message_id,omitempty"`
	Status             string    `json:"status"`
	Reserved           bool      `json:"reserved"`
	CreatedAt          time.Time `json:"created_at,omitempty"`
	UpdatedAt          time.Time `json:"updated_at,omitempty"`
}

type ChatTurnReservationStore interface {
	Init(context.Context) error
	ReserveChatTurn(context.Context, ChatTurnReservation) (ChatTurnReservation, error)
	HandoffChatTurn(context.Context, string, ChatTurnReservation) (ChatTurnReservation, error)
	UpdateChatTurnReservationStatus(context.Context, string, string, string, string) error
}

const chatTurnReservationLeaseTTL = 30 * time.Minute

type RunUsageSummary struct {
	RunID                 string  `json:"run_id"`
	UserID                string  `json:"user_id,omitempty"`
	SessionID             string  `json:"session_id,omitempty"`
	Status                string  `json:"status,omitempty"`
	LLMRequests           int     `json:"llm_requests"`
	InputTokens           int     `json:"input_tokens"`
	OutputTokens          int     `json:"output_tokens"`
	TotalTokens           int     `json:"total_tokens"`
	EstimatedCostUSD      float64 `json:"estimated_cost_usd"`
	ToolCallCount         int     `json:"tool_call_count"`
	ToolErrorCount        int     `json:"tool_error_count"`
	ArtifactCount         int     `json:"artifact_count"`
	StructuredOutputCount int     `json:"structured_output_count"`
	LastEventID           string  `json:"last_event_id,omitempty"`
	Error                 string  `json:"error,omitempty"`
}

func NormalizeMessageStructuredOutput(raw json.RawMessage, fallback MessageStructuredOutput) (MessageStructuredOutput, error) {
	var obj map[string]any
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &obj); err != nil {
			return MessageStructuredOutput{}, err
		}
	}
	if obj == nil {
		obj = map[string]any{}
	}
	version := firstNonEmptyString(structuredOutputString(obj, "version"), fallback.SchemaVersion, StructuredOutputSchemaVersion)
	kind := firstNonEmptyString(structuredOutputString(obj, "kind"), fallback.Kind, "card")
	kind = strings.ToLower(strings.TrimSpace(kind))
	switch kind {
	case "card", "artifact_card", "choice_set", "progress", "diagnostic":
	default:
		kind = "card"
	}
	id := firstNonEmptyString(structuredOutputString(obj, "id"), fallback.ID)
	if id == "" {
		id = "so-" + newSortableID()
	}
	obj["id"] = id
	obj["version"] = version
	obj["kind"] = kind
	payload, err := json.Marshal(obj)
	if err != nil {
		return MessageStructuredOutput{}, err
	}
	out := fallback
	out.ID = id
	out.Kind = kind
	out.SchemaVersion = version
	out.Title = firstNonEmptyString(structuredOutputString(obj, "title"), fallback.Title)
	out.Summary = firstNonEmptyString(structuredOutputString(obj, "summary"), fallback.Summary, out.Title, kind)
	out.Payload = payload
	if out.CreatedAt.IsZero() {
		out.CreatedAt = time.Now().UTC()
	}
	return out, nil
}

func structuredOutputString(obj map[string]any, key string) string {
	if obj == nil {
		return ""
	}
	value, ok := obj[key]
	if !ok || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}

func structuredOutputFromEvent(event Event, userID, sessionID, runID string) (MessageStructuredOutput, bool) {
	if event.Type != StructuredOutputEventType {
		return MessageStructuredOutput{}, false
	}
	raw := event.Data
	if len(raw) == 0 {
		return MessageStructuredOutput{}, false
	}
	out, err := NormalizeMessageStructuredOutput(raw, MessageStructuredOutput{
		UserID:    strings.TrimSpace(userID),
		SessionID: firstNonEmptyString(strings.TrimSpace(event.SessionID), strings.TrimSpace(sessionID)),
		RunID:     firstNonEmptyString(strings.TrimSpace(event.RunID), strings.TrimSpace(runID)),
		Source:    "runtime_event",
	})
	if err != nil {
		return MessageStructuredOutput{}, false
	}
	return out, true
}

type MemoryRuntimeOutputStore struct {
	mu                sync.Mutex
	structuredOutputs map[string]MessageStructuredOutput
	chatRunSnapshots  map[string]ChatRunSnapshot
	turnReservations  map[string]ChatTurnReservation
}

func NewMemoryRuntimeOutputStore() *MemoryRuntimeOutputStore {
	return &MemoryRuntimeOutputStore{
		structuredOutputs: make(map[string]MessageStructuredOutput),
		chatRunSnapshots:  make(map[string]ChatRunSnapshot),
		turnReservations:  make(map[string]ChatTurnReservation),
	}
}

func (s *MemoryRuntimeOutputStore) Init(context.Context) error { return nil }

func (s *MemoryRuntimeOutputStore) SaveStructuredOutput(ctx context.Context, output MessageStructuredOutput) (MessageStructuredOutput, error) {
	if s == nil {
		return MessageStructuredOutput{}, fmt.Errorf("structured output store is not configured")
	}
	select {
	case <-ctx.Done():
		return MessageStructuredOutput{}, ctx.Err()
	default:
	}
	output, err := NormalizeMessageStructuredOutput(output.Payload, output)
	if err != nil {
		return MessageStructuredOutput{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.structuredOutputs[output.ID] = output
	return output, nil
}

func (s *MemoryRuntimeOutputStore) ListStructuredOutputsBySession(ctx context.Context, userID, sessionID string) ([]MessageStructuredOutput, error) {
	return s.listStructuredOutputs(ctx, userID, sessionID, "")
}

func (s *MemoryRuntimeOutputStore) ListStructuredOutputsByRun(ctx context.Context, userID, runID string) ([]MessageStructuredOutput, error) {
	return s.listStructuredOutputs(ctx, userID, "", runID)
}

func (s *MemoryRuntimeOutputStore) listStructuredOutputs(ctx context.Context, userID, sessionID, runID string) ([]MessageStructuredOutput, error) {
	if s == nil {
		return nil, fmt.Errorf("structured output store is not configured")
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	userID = strings.TrimSpace(userID)
	sessionID = strings.TrimSpace(sessionID)
	runID = strings.TrimSpace(runID)
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]MessageStructuredOutput, 0)
	for _, item := range s.structuredOutputs {
		if userID != "" && item.UserID != "" && item.UserID != userID {
			continue
		}
		if sessionID != "" && item.SessionID != sessionID {
			continue
		}
		if runID != "" && item.RunID != runID {
			continue
		}
		out = append(out, item)
	}
	return out, nil
}

func (s *MemoryRuntimeOutputStore) SaveChatRunSnapshot(ctx context.Context, snapshot ChatRunSnapshot) error {
	if s == nil {
		return fmt.Errorf("chat run snapshot store is not configured")
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	snapshot.RunID = strings.TrimSpace(snapshot.RunID)
	if snapshot.RunID == "" {
		return fmt.Errorf("run_id is required")
	}
	now := time.Now().UTC()
	if snapshot.CreatedAt.IsZero() {
		snapshot.CreatedAt = now
	}
	snapshot.UpdatedAt = now
	s.mu.Lock()
	s.chatRunSnapshots[snapshot.RunID] = snapshot
	s.mu.Unlock()
	return nil
}

func (s *MemoryRuntimeOutputStore) GetChatRunSnapshot(ctx context.Context, userID, runID string) (ChatRunSnapshot, error) {
	if s == nil {
		return ChatRunSnapshot{}, fmt.Errorf("chat run snapshot store is not configured")
	}
	select {
	case <-ctx.Done():
		return ChatRunSnapshot{}, ctx.Err()
	default:
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot, ok := s.chatRunSnapshots[strings.TrimSpace(runID)]
	if !ok || (strings.TrimSpace(userID) != "" && snapshot.UserID != "" && snapshot.UserID != strings.TrimSpace(userID)) {
		return ChatRunSnapshot{}, sql.ErrNoRows
	}
	return snapshot, nil
}

func (s *MemoryRuntimeOutputStore) ReserveChatTurn(ctx context.Context, reservation ChatTurnReservation) (ChatTurnReservation, error) {
	if s == nil {
		return ChatTurnReservation{}, fmt.Errorf("chat turn reservation store is not configured")
	}
	select {
	case <-ctx.Done():
		return ChatTurnReservation{}, ctx.Err()
	default:
	}
	key := chatTurnReservationKey(reservation.UserID, reservation.SessionID, reservation.IdempotencyKey)
	if key == "" {
		return ChatTurnReservation{}, fmt.Errorf("idempotency_key is required")
	}
	now := time.Now().UTC()
	if reservation.RunID == "" {
		reservation.RunID = NewChatRunID()
	}
	if reservation.UserMessageID == "" {
		reservation.UserMessageID = "msg-" + newSortableID()
	}
	if reservation.AssistantMessageID == "" {
		reservation.AssistantMessageID = "msg-" + newSortableID()
	}
	reservation.Status = firstNonEmptyString(reservation.Status, "reserved")
	reservation.CreatedAt = now
	reservation.UpdatedAt = now
	reservation.Reserved = true
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.turnReservations[key]; ok {
		existing.Reserved = false
		return existing, nil
	}
	cutoff := now.Add(-chatTurnReservationLeaseTTL)
	for existingKey, existing := range s.turnReservations {
		if existing.UserID != reservation.UserID || existing.SessionID != reservation.SessionID || existing.Status != "reserved" {
			continue
		}
		if existing.UpdatedAt.Before(cutoff) {
			existing.Status = "expired"
			existing.UpdatedAt = now
			s.turnReservations[existingKey] = existing
			continue
		}
		return ChatTurnReservation{}, ErrSessionTurnRunning
	}
	s.turnReservations[key] = reservation
	return reservation, nil
}

func (s *MemoryRuntimeOutputStore) HandoffChatTurn(ctx context.Context, fromRunID string, reservation ChatTurnReservation) (ChatTurnReservation, error) {
	if s == nil {
		return ChatTurnReservation{}, fmt.Errorf("chat turn reservation store is not configured")
	}
	select {
	case <-ctx.Done():
		return ChatTurnReservation{}, ctx.Err()
	default:
	}
	reservation.UserID = strings.TrimSpace(reservation.UserID)
	reservation.SessionID = strings.TrimSpace(reservation.SessionID)
	reservation.IdempotencyKey = strings.TrimSpace(reservation.IdempotencyKey)
	fromRunID = strings.TrimSpace(fromRunID)
	key := chatTurnReservationKey(reservation.UserID, reservation.SessionID, reservation.IdempotencyKey)
	if fromRunID == "" || key == "" {
		return ChatTurnReservation{}, fmt.Errorf("handoff source run and target reservation are required")
	}
	now := time.Now().UTC()
	if reservation.RunID == "" {
		reservation.RunID = NewChatRunID()
	}
	if reservation.UserMessageID == "" {
		reservation.UserMessageID = "msg-" + newSortableID()
	}
	if reservation.AssistantMessageID == "" {
		reservation.AssistantMessageID = "msg-" + newSortableID()
	}
	reservation.Status = "reserved"
	reservation.CreatedAt = now
	reservation.UpdatedAt = now
	reservation.Reserved = true

	s.mu.Lock()
	defer s.mu.Unlock()
	fromKey := ""
	var from ChatTurnReservation
	for existingKey, existing := range s.turnReservations {
		if existing.UserID == reservation.UserID && existing.SessionID == reservation.SessionID && existing.RunID == fromRunID {
			fromKey = existingKey
			from = existing
			break
		}
	}
	if existing, ok := s.turnReservations[key]; ok {
		if fromKey != "" && from.Status == "handed_off" && existing.Status == "reserved" && existing.RunID == reservation.RunID {
			existing.Reserved = false
			return existing, nil
		}
		return ChatTurnReservation{}, ErrSessionTurnRunning
	}
	if fromKey == "" || from.Status != "reserved" {
		return ChatTurnReservation{}, ErrSessionTurnRunning
	}
	from.Status = "handed_off"
	from.UpdatedAt = now
	s.turnReservations[fromKey] = from
	s.turnReservations[key] = reservation
	return reservation, nil
}

func (s *MemoryRuntimeOutputStore) UpdateChatTurnReservationStatus(ctx context.Context, userID, sessionID, runID, status string) error {
	if s == nil {
		return fmt.Errorf("chat turn reservation store is not configured")
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, reservation := range s.turnReservations {
		if reservation.UserID == strings.TrimSpace(userID) && reservation.SessionID == strings.TrimSpace(sessionID) && reservation.RunID == strings.TrimSpace(runID) {
			reservation.Status = strings.TrimSpace(status)
			reservation.UpdatedAt = time.Now().UTC()
			s.turnReservations[key] = reservation
			return nil
		}
	}
	return nil
}

type SQLRuntimeOutputStore struct {
	db      *sql.DB
	dialect SQLDialect
}

func NewSQLRuntimeOutputStoreWithDialect(db *sql.DB, dialect SQLDialect) *SQLRuntimeOutputStore {
	return &SQLRuntimeOutputStore{db: db, dialect: dialect}
}

func (s *SQLRuntimeOutputStore) Init(ctx context.Context) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("runtime output db is required")
	}
	if err := requireSQLColumns(ctx, s.db, "agent_message_structured_outputs",
		"id", "user_id", "session_id", "run_id", "message_id", "kind", "schema_version",
		"payload_json", "source", "created_at",
	); err != nil {
		return err
	}
	if err := requireSQLColumns(ctx, s.db, "agent_chat_run_snapshots",
		"run_id", "user_id", "session_id", "status", "final_message_id", "final_content",
		"event_count", "structured_output_count", "artifact_count", "error", "last_event_id",
		"payload_json", "created_at", "updated_at",
	); err != nil {
		return err
	}
	return requireSQLColumns(ctx, s.db, "agent_chat_turn_reservations",
		"user_id", "session_id", "idempotency_key", "run_id", "user_message_id",
		"assistant_message_id", "status", "created_at", "updated_at",
	)
}

func (s *SQLRuntimeOutputStore) SaveStructuredOutput(ctx context.Context, output MessageStructuredOutput) (MessageStructuredOutput, error) {
	output, err := NormalizeMessageStructuredOutput(output.Payload, output)
	if err != nil {
		return MessageStructuredOutput{}, err
	}
	if output.UserID == "" || output.SessionID == "" {
		return MessageStructuredOutput{}, fmt.Errorf("user_id and session_id are required")
	}
	query := `
INSERT INTO agent_message_structured_outputs (
	id, user_id, session_id, run_id, message_id, kind, schema_version, payload_json, source, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (id) DO UPDATE SET
	user_id = EXCLUDED.user_id,
	session_id = EXCLUDED.session_id,
	run_id = EXCLUDED.run_id,
	message_id = EXCLUDED.message_id,
	kind = EXCLUDED.kind,
	schema_version = EXCLUDED.schema_version,
	payload_json = EXCLUDED.payload_json,
	source = EXCLUDED.source`
	if s.dialect != SQLDialectPostgres {
		query = `
INSERT INTO agent_message_structured_outputs (
	id, user_id, session_id, run_id, message_id, kind, schema_version, payload_json, source, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
	user_id = excluded.user_id,
	session_id = excluded.session_id,
	run_id = excluded.run_id,
	message_id = excluded.message_id,
	kind = excluded.kind,
	schema_version = excluded.schema_version,
	payload_json = excluded.payload_json,
	source = excluded.source`
	}
	_, err = s.db.ExecContext(ctx, s.dialect.Bind(query),
		output.ID, output.UserID, output.SessionID, output.RunID, output.MessageID, output.Kind,
		output.SchemaVersion, sanitizeSQLText(string(output.Payload)), output.Source, sqlTimeValue(output.CreatedAt, s.dialect),
	)
	if err != nil {
		return MessageStructuredOutput{}, err
	}
	return output, nil
}

func (s *SQLRuntimeOutputStore) ListStructuredOutputsBySession(ctx context.Context, userID, sessionID string) ([]MessageStructuredOutput, error) {
	return s.listStructuredOutputs(ctx, `user_id = ? AND session_id = ?`, userID, sessionID)
}

func (s *SQLRuntimeOutputStore) ListStructuredOutputsByRun(ctx context.Context, userID, runID string) ([]MessageStructuredOutput, error) {
	if strings.TrimSpace(userID) == "" {
		return s.listStructuredOutputs(ctx, `run_id = ?`, runID)
	}
	return s.listStructuredOutputs(ctx, `user_id = ? AND run_id = ?`, userID, runID)
}

func (s *SQLRuntimeOutputStore) listStructuredOutputs(ctx context.Context, where string, args ...any) ([]MessageStructuredOutput, error) {
	rows, err := s.db.QueryContext(ctx, s.dialect.Bind(`
SELECT id, user_id, session_id, run_id, message_id, kind, schema_version, payload_json, source, created_at
FROM agent_message_structured_outputs
WHERE `+where+`
ORDER BY created_at ASC, id ASC`), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MessageStructuredOutput
	for rows.Next() {
		item, err := scanMessageStructuredOutput(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func scanMessageStructuredOutput(scanner sqlScanner) (MessageStructuredOutput, error) {
	var item MessageStructuredOutput
	var raw string
	var created any
	if err := scanner.Scan(&item.ID, &item.UserID, &item.SessionID, &item.RunID, &item.MessageID, &item.Kind, &item.SchemaVersion, &raw, &item.Source, &created); err != nil {
		return MessageStructuredOutput{}, err
	}
	item.Payload = json.RawMessage(raw)
	createdAt, err := parseSQLTime(created)
	if err != nil {
		return MessageStructuredOutput{}, err
	}
	item.CreatedAt = createdAt
	var obj map[string]any
	if err := json.Unmarshal(item.Payload, &obj); err == nil {
		item.Title = structuredOutputString(obj, "title")
		item.Summary = structuredOutputString(obj, "summary")
	}
	return item, nil
}

func (s *SQLRuntimeOutputStore) SaveChatRunSnapshot(ctx context.Context, snapshot ChatRunSnapshot) error {
	snapshot.RunID = strings.TrimSpace(snapshot.RunID)
	if snapshot.RunID == "" {
		return fmt.Errorf("run_id is required")
	}
	now := time.Now().UTC()
	if snapshot.CreatedAt.IsZero() {
		snapshot.CreatedAt = now
	}
	snapshot.UpdatedAt = now
	if len(snapshot.Payload) == 0 {
		snapshot.Payload = json.RawMessage(`{}`)
	}
	query := `
INSERT INTO agent_chat_run_snapshots (
	run_id, user_id, session_id, status, final_message_id, final_content, event_count,
	structured_output_count, artifact_count, error, last_event_id, payload_json, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (run_id) DO UPDATE SET
	user_id = EXCLUDED.user_id,
	session_id = EXCLUDED.session_id,
	status = EXCLUDED.status,
	final_message_id = EXCLUDED.final_message_id,
	final_content = EXCLUDED.final_content,
	event_count = EXCLUDED.event_count,
	structured_output_count = EXCLUDED.structured_output_count,
	artifact_count = EXCLUDED.artifact_count,
	error = EXCLUDED.error,
	last_event_id = EXCLUDED.last_event_id,
	payload_json = EXCLUDED.payload_json,
	updated_at = EXCLUDED.updated_at`
	if s.dialect != SQLDialectPostgres {
		query = strings.ReplaceAll(query, "EXCLUDED.", "excluded.")
	}
	_, err := s.db.ExecContext(ctx, s.dialect.Bind(query),
		snapshot.RunID, snapshot.UserID, snapshot.SessionID, snapshot.Status, snapshot.FinalMessageID,
		snapshot.FinalContent, snapshot.EventCount, snapshot.StructuredOutputCount, snapshot.ArtifactCount,
		snapshot.Error, snapshot.LastEventID, sanitizeSQLText(string(snapshot.Payload)),
		sqlTimeValue(snapshot.CreatedAt, s.dialect), sqlTimeValue(snapshot.UpdatedAt, s.dialect),
	)
	return err
}

func (s *SQLRuntimeOutputStore) GetChatRunSnapshot(ctx context.Context, userID, runID string) (ChatRunSnapshot, error) {
	if strings.TrimSpace(userID) == "" {
		row := s.db.QueryRowContext(ctx, s.dialect.Bind(`
SELECT run_id, user_id, session_id, status, final_message_id, final_content, event_count,
	structured_output_count, artifact_count, error, last_event_id, payload_json, created_at, updated_at
FROM agent_chat_run_snapshots
WHERE run_id = ?`), strings.TrimSpace(runID))
		return scanChatRunSnapshot(row)
	}
	row := s.db.QueryRowContext(ctx, s.dialect.Bind(`
SELECT run_id, user_id, session_id, status, final_message_id, final_content, event_count,
	structured_output_count, artifact_count, error, last_event_id, payload_json, created_at, updated_at
FROM agent_chat_run_snapshots
WHERE user_id = ? AND run_id = ?`), strings.TrimSpace(userID), strings.TrimSpace(runID))
	return scanChatRunSnapshot(row)
}

func SummarizeRunUsage(ctx context.Context, snapshots ChatRunSnapshotStore, structuredOutputs StructuredOutputStore, toolLedger ToolCallLedgerStore, userID, runID string) (RunUsageSummary, error) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return RunUsageSummary{}, fmt.Errorf("run_id is required")
	}
	summary := RunUsageSummary{RunID: runID}
	if snapshots != nil {
		snapshot, err := snapshots.GetChatRunSnapshot(ctx, userID, runID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return RunUsageSummary{}, err
		}
		if err == nil {
			summary.UserID = snapshot.UserID
			summary.SessionID = snapshot.SessionID
			summary.Status = snapshot.Status
			summary.ArtifactCount = snapshot.ArtifactCount
			summary.StructuredOutputCount = snapshot.StructuredOutputCount
			summary.LastEventID = snapshot.LastEventID
			summary.Error = snapshot.Error
		}
	}
	if structuredOutputs != nil {
		outputs, err := structuredOutputs.ListStructuredOutputsByRun(ctx, userID, runID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return RunUsageSummary{}, err
		}
		if len(outputs) > summary.StructuredOutputCount {
			summary.StructuredOutputCount = len(outputs)
		}
		if summary.UserID == "" && len(outputs) > 0 {
			summary.UserID = outputs[0].UserID
		}
		if summary.SessionID == "" && len(outputs) > 0 {
			summary.SessionID = outputs[0].SessionID
		}
	}
	if toolLedger != nil {
		toolCalls, err := toolLedger.ListToolCalls(ctx, ToolCallLedgerFilter{UserID: strings.TrimSpace(userID), WorkflowRunID: runID})
		if err != nil {
			return RunUsageSummary{}, err
		}
		summary.ToolCallCount = len(toolCalls)
		for _, toolCall := range toolCalls {
			if strings.EqualFold(strings.TrimSpace(toolCall.Status), "failed") || strings.TrimSpace(toolCall.Error) != "" {
				summary.ToolErrorCount++
			}
		}
	}
	return summary, nil
}

func scanChatRunSnapshot(scanner sqlScanner) (ChatRunSnapshot, error) {
	var item ChatRunSnapshot
	var raw string
	var created, updated any
	if err := scanner.Scan(&item.RunID, &item.UserID, &item.SessionID, &item.Status, &item.FinalMessageID, &item.FinalContent, &item.EventCount, &item.StructuredOutputCount, &item.ArtifactCount, &item.Error, &item.LastEventID, &raw, &created, &updated); err != nil {
		return ChatRunSnapshot{}, err
	}
	item.Payload = json.RawMessage(raw)
	createdAt, err := parseSQLTime(created)
	if err != nil {
		return ChatRunSnapshot{}, err
	}
	updatedAt, err := parseSQLTime(updated)
	if err != nil {
		return ChatRunSnapshot{}, err
	}
	item.CreatedAt = createdAt
	item.UpdatedAt = updatedAt
	return item, nil
}

func (s *SQLRuntimeOutputStore) ReserveChatTurn(ctx context.Context, reservation ChatTurnReservation) (ChatTurnReservation, error) {
	reservation.UserID = strings.TrimSpace(reservation.UserID)
	reservation.SessionID = strings.TrimSpace(reservation.SessionID)
	reservation.IdempotencyKey = strings.TrimSpace(reservation.IdempotencyKey)
	if reservation.UserID == "" || reservation.SessionID == "" || reservation.IdempotencyKey == "" {
		return ChatTurnReservation{}, fmt.Errorf("user_id, session_id and idempotency_key are required")
	}
	if reservation.RunID == "" {
		reservation.RunID = NewChatRunID()
	}
	if reservation.UserMessageID == "" {
		reservation.UserMessageID = "msg-" + newSortableID()
	}
	if reservation.AssistantMessageID == "" {
		reservation.AssistantMessageID = "msg-" + newSortableID()
	}
	reservation.Status = firstNonEmptyString(reservation.Status, "reserved")
	now := time.Now().UTC()
	if reservation.CreatedAt.IsZero() {
		reservation.CreatedAt = now
	}
	reservation.UpdatedAt = now
	if _, err := s.db.ExecContext(ctx, s.dialect.Bind(`
UPDATE agent_chat_turn_reservations
SET status = 'expired', updated_at = ?
WHERE user_id = ? AND session_id = ? AND status = 'reserved' AND updated_at < ?
	AND NOT EXISTS (
		SELECT 1 FROM agent_jobs
		WHERE agent_jobs.job_id = agent_chat_turn_reservations.run_id
			AND agent_jobs.user_id = agent_chat_turn_reservations.user_id
			AND agent_jobs.session_id = agent_chat_turn_reservations.session_id
			AND agent_jobs.status IN ('queued', 'running')
	)`),
		sqlTimeValue(now, s.dialect), reservation.UserID, reservation.SessionID,
		sqlTimeValue(now.Add(-chatTurnReservationLeaseTTL), s.dialect),
	); err != nil {
		return ChatTurnReservation{}, err
	}
	query := `
INSERT INTO agent_chat_turn_reservations (
	user_id, session_id, idempotency_key, run_id, user_message_id, assistant_message_id, status, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT DO NOTHING
RETURNING user_id, session_id, idempotency_key, run_id, user_message_id, assistant_message_id, status, created_at, updated_at`
	row := s.db.QueryRowContext(ctx, s.dialect.Bind(query),
		reservation.UserID, reservation.SessionID, reservation.IdempotencyKey, reservation.RunID,
		reservation.UserMessageID, reservation.AssistantMessageID, reservation.Status,
		sqlTimeValue(reservation.CreatedAt, s.dialect), sqlTimeValue(reservation.UpdatedAt, s.dialect),
	)
	created, err := scanChatTurnReservation(row)
	if err == nil {
		created.Reserved = true
		return created, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return ChatTurnReservation{}, err
	}
	existing, err := s.getChatTurnReservation(ctx, reservation.UserID, reservation.SessionID, reservation.IdempotencyKey)
	if err == nil {
		existing.Reserved = false
		return existing, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return ChatTurnReservation{}, err
	}
	if _, err := s.getActiveChatTurnReservation(ctx, reservation.UserID, reservation.SessionID); err == nil {
		return ChatTurnReservation{}, ErrSessionTurnRunning
	} else if !errors.Is(err, sql.ErrNoRows) {
		return ChatTurnReservation{}, err
	}
	return ChatTurnReservation{}, fmt.Errorf("reserve chat turn: conflicting reservation disappeared")
}

func (s *SQLRuntimeOutputStore) HandoffChatTurn(ctx context.Context, fromRunID string, reservation ChatTurnReservation) (ChatTurnReservation, error) {
	reservation.UserID = strings.TrimSpace(reservation.UserID)
	reservation.SessionID = strings.TrimSpace(reservation.SessionID)
	reservation.IdempotencyKey = strings.TrimSpace(reservation.IdempotencyKey)
	fromRunID = strings.TrimSpace(fromRunID)
	if reservation.UserID == "" || reservation.SessionID == "" || reservation.IdempotencyKey == "" || fromRunID == "" {
		return ChatTurnReservation{}, fmt.Errorf("handoff source run, user_id, session_id and idempotency_key are required")
	}
	if reservation.RunID == "" {
		reservation.RunID = NewChatRunID()
	}
	if reservation.UserMessageID == "" {
		reservation.UserMessageID = "msg-" + newSortableID()
	}
	if reservation.AssistantMessageID == "" {
		reservation.AssistantMessageID = "msg-" + newSortableID()
	}
	now := time.Now().UTC()
	reservation.Status = "reserved"
	reservation.CreatedAt = now
	reservation.UpdatedAt = now

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ChatTurnReservation{}, err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, s.dialect.Bind(`
UPDATE agent_chat_turn_reservations
SET status = 'handed_off', updated_at = ?
WHERE user_id = ? AND session_id = ? AND run_id = ? AND status = 'reserved'`),
		sqlTimeValue(now, s.dialect), reservation.UserID, reservation.SessionID, fromRunID,
	)
	if err != nil {
		return ChatTurnReservation{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return ChatTurnReservation{}, err
	}
	if affected != 1 {
		var sourceStatus string
		sourceErr := tx.QueryRowContext(ctx, s.dialect.Bind(`
SELECT status
FROM agent_chat_turn_reservations
WHERE user_id = ? AND session_id = ? AND run_id = ?`), reservation.UserID, reservation.SessionID, fromRunID).Scan(&sourceStatus)
		if sourceErr == nil && sourceStatus == "handed_off" {
			existing, existingErr := scanChatTurnReservation(tx.QueryRowContext(ctx, s.dialect.Bind(`
SELECT user_id, session_id, idempotency_key, run_id, user_message_id, assistant_message_id, status, created_at, updated_at
FROM agent_chat_turn_reservations
WHERE user_id = ? AND session_id = ? AND idempotency_key = ? AND run_id = ?`),
				reservation.UserID, reservation.SessionID, reservation.IdempotencyKey, reservation.RunID,
			))
			if existingErr == nil && existing.Status == "reserved" {
				existing.Reserved = false
				return existing, nil
			}
		}
		return ChatTurnReservation{}, ErrSessionTurnRunning
	}
	row := tx.QueryRowContext(ctx, s.dialect.Bind(`
INSERT INTO agent_chat_turn_reservations (
	user_id, session_id, idempotency_key, run_id, user_message_id, assistant_message_id, status, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, 'reserved', ?, ?)
RETURNING user_id, session_id, idempotency_key, run_id, user_message_id, assistant_message_id, status, created_at, updated_at`),
		reservation.UserID, reservation.SessionID, reservation.IdempotencyKey, reservation.RunID,
		reservation.UserMessageID, reservation.AssistantMessageID,
		sqlTimeValue(now, s.dialect), sqlTimeValue(now, s.dialect),
	)
	created, err := scanChatTurnReservation(row)
	if err != nil {
		return ChatTurnReservation{}, err
	}
	if err := tx.Commit(); err != nil {
		return ChatTurnReservation{}, err
	}
	created.Reserved = true
	return created, nil
}

func (s *SQLRuntimeOutputStore) getChatTurnReservation(ctx context.Context, userID, sessionID, idempotencyKey string) (ChatTurnReservation, error) {
	row := s.db.QueryRowContext(ctx, s.dialect.Bind(`
SELECT user_id, session_id, idempotency_key, run_id, user_message_id, assistant_message_id, status, created_at, updated_at
FROM agent_chat_turn_reservations
WHERE user_id = ? AND session_id = ? AND idempotency_key = ?`), userID, sessionID, idempotencyKey)
	return scanChatTurnReservation(row)
}

func (s *SQLRuntimeOutputStore) getActiveChatTurnReservation(ctx context.Context, userID, sessionID string) (ChatTurnReservation, error) {
	row := s.db.QueryRowContext(ctx, s.dialect.Bind(`
SELECT user_id, session_id, idempotency_key, run_id, user_message_id, assistant_message_id, status, created_at, updated_at
FROM agent_chat_turn_reservations
WHERE user_id = ? AND session_id = ? AND status = 'reserved'
ORDER BY updated_at DESC
LIMIT 1`), userID, sessionID)
	return scanChatTurnReservation(row)
}

func (s *SQLRuntimeOutputStore) UpdateChatTurnReservationStatus(ctx context.Context, userID, sessionID, runID, status string) error {
	_, err := s.db.ExecContext(ctx, s.dialect.Bind(`
UPDATE agent_chat_turn_reservations
SET status = ?, updated_at = ?
WHERE user_id = ? AND session_id = ? AND run_id = ?`),
		strings.TrimSpace(status), sqlTimeValue(time.Now().UTC(), s.dialect),
		strings.TrimSpace(userID), strings.TrimSpace(sessionID), strings.TrimSpace(runID),
	)
	return err
}

func scanChatTurnReservation(scanner sqlScanner) (ChatTurnReservation, error) {
	var item ChatTurnReservation
	var created, updated any
	if err := scanner.Scan(&item.UserID, &item.SessionID, &item.IdempotencyKey, &item.RunID, &item.UserMessageID, &item.AssistantMessageID, &item.Status, &created, &updated); err != nil {
		return ChatTurnReservation{}, err
	}
	createdAt, err := parseSQLTime(created)
	if err != nil {
		return ChatTurnReservation{}, err
	}
	updatedAt, err := parseSQLTime(updated)
	if err != nil {
		return ChatTurnReservation{}, err
	}
	item.CreatedAt = createdAt
	item.UpdatedAt = updatedAt
	return item, nil
}

func chatTurnReservationKey(userID, sessionID, idempotencyKey string) string {
	userID = strings.TrimSpace(userID)
	sessionID = strings.TrimSpace(sessionID)
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if userID == "" || sessionID == "" || idempotencyKey == "" {
		return ""
	}
	return userID + "\x00" + sessionID + "\x00" + idempotencyKey
}
