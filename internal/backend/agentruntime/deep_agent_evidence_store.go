package agentruntime

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"
)

const deepAgentEvidenceStoreKey = "evidence_store"

type DeepAgentEvidenceStore interface {
	PutStepEvidence(state *DeepAgentState, evidence DeepAgentStepEvidence)
	ListStepEvidence(state *DeepAgentState) []DeepAgentStepEvidence
	GetStepEvidence(state *DeepAgentState, stepID string) (DeepAgentStepEvidence, bool)
}

type StateDeepAgentEvidenceStore struct{}

func (StateDeepAgentEvidenceStore) PutStepEvidence(state *DeepAgentState, evidence DeepAgentStepEvidence) {
	if state == nil {
		return
	}
	if state.WorkingMemory == nil {
		state.WorkingMemory = map[string]any{}
	}
	stepID := strings.TrimSpace(evidence.StepID)
	if stepID == "" {
		stepID = strings.TrimSpace(evidence.Route.StepID)
	}
	actionID := strings.TrimSpace(evidence.ActionID)
	store := deepAgentStateEvidenceStore(state, true)
	byStep, _ := store["by_step"].(map[string]any)
	items, _ := store["items"].([]any)
	if byStep == nil {
		byStep = map[string]any{}
		store["by_step"] = byStep
	}
	key := strings.TrimSpace(firstNonEmptyString(actionID, stepID))
	record := deepAgentStepEvidenceMap(evidence)
	if stepID != "" {
		byStep[stepID] = record
	}
	if key == "" {
		items = append(items, record)
		store["items"] = items
		return
	}
	replaced := false
	for idx, raw := range items {
		existing, ok := deepAgentStepEvidenceFromAny(raw)
		if !ok {
			continue
		}
		existingKey := strings.TrimSpace(firstNonEmptyString(existing.ActionID, existing.StepID, existing.Route.StepID))
		if existingKey == key {
			items[idx] = record
			replaced = true
			break
		}
	}
	if !replaced {
		items = append(items, record)
	}
	store["items"] = items
}

func (StateDeepAgentEvidenceStore) ListStepEvidence(state *DeepAgentState) []DeepAgentStepEvidence {
	if state == nil {
		return nil
	}
	store := deepAgentStateEvidenceStore(state, false)
	items, _ := store["items"].([]any)
	out := make([]DeepAgentStepEvidence, 0, len(items))
	seen := map[string]struct{}{}
	appendEvidence := func(evidence DeepAgentStepEvidence) {
		key := firstNonEmptyString(evidence.ActionID, evidence.StepID, evidence.Route.StepID)
		if key == "" {
			key = evidence.Summary
		}
		if key != "" {
			if _, ok := seen[key]; ok {
				return
			}
			seen[key] = struct{}{}
		}
		out = append(out, evidence)
	}
	for _, raw := range items {
		if evidence, ok := deepAgentStepEvidenceFromAny(raw); ok {
			appendEvidence(evidence)
		}
	}
	byStep, _ := store["by_step"].(map[string]any)
	for _, raw := range byStep {
		if evidence, ok := deepAgentStepEvidenceFromAny(raw); ok {
			appendEvidence(evidence)
		}
	}
	return out
}

func (StateDeepAgentEvidenceStore) GetStepEvidence(state *DeepAgentState, stepID string) (DeepAgentStepEvidence, bool) {
	stepID = strings.TrimSpace(stepID)
	if stepID == "" || state == nil {
		return DeepAgentStepEvidence{}, false
	}
	store := deepAgentStateEvidenceStore(state, false)
	byStep, _ := store["by_step"].(map[string]any)
	if evidence, ok := deepAgentStepEvidenceFromAny(byStep[stepID]); ok {
		return evidence, true
	}
	for _, evidence := range (StateDeepAgentEvidenceStore{}).ListStepEvidence(state) {
		if evidence.StepID == stepID || evidence.Route.StepID == stepID {
			return evidence, true
		}
	}
	return DeepAgentStepEvidence{}, false
}

func deepAgentStateEvidenceStore(state *DeepAgentState, create bool) map[string]any {
	if state == nil {
		return nil
	}
	if state.WorkingMemory == nil {
		if !create {
			return nil
		}
		state.WorkingMemory = map[string]any{}
	}
	store, _ := state.WorkingMemory[deepAgentEvidenceStoreKey].(map[string]any)
	if store == nil && create {
		store = map[string]any{
			"items":   []any{},
			"by_step": map[string]any{},
		}
		state.WorkingMemory[deepAgentEvidenceStoreKey] = store
	}
	return store
}

func deepAgentDefaultEvidenceStore() DeepAgentEvidenceStore {
	return StateDeepAgentEvidenceStore{}
}

type DeepAgentEvidenceFilter struct {
	UserID     string
	SessionID  string
	RunID      string
	JobID      string
	LoopGoalID string
	StepID     string
	TaskType   string
	TemplateID string
	Limit      int
}

type DeepAgentEvidenceRecord struct {
	ID              string
	RunID           string
	UserID          string
	SessionID       string
	JobID           string
	LoopGoalID      string
	StepID          string
	ActionID        string
	TemplateID      string
	TaskType        string
	TriggerType     string
	Route           DeepAgentStepRoute
	Evidence        DeepAgentStepEvidence
	ArtifactCount   int
	SourceCount     int
	ToolCallCount   int
	ChildJobCount   int
	ErrorClass      string
	SideEffectLevel string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type DeepAgentEvidenceRepository interface {
	Init(ctx context.Context) error
	UpsertRunEvidence(ctx context.Context, run *WorkflowRun, state *DeepAgentState) error
	ListDeepAgentEvidence(ctx context.Context, filter DeepAgentEvidenceFilter) ([]DeepAgentEvidenceRecord, error)
}

type MemoryDeepAgentEvidenceRepository struct {
	mu      sync.Mutex
	records map[string]DeepAgentEvidenceRecord
}

func NewMemoryDeepAgentEvidenceRepository() *MemoryDeepAgentEvidenceRepository {
	return &MemoryDeepAgentEvidenceRepository{records: make(map[string]DeepAgentEvidenceRecord)}
}

func (r *MemoryDeepAgentEvidenceRepository) Init(context.Context) error { return nil }

func (r *MemoryDeepAgentEvidenceRepository) UpsertRunEvidence(_ context.Context, run *WorkflowRun, state *DeepAgentState) error {
	if r == nil || run == nil || state == nil {
		return nil
	}
	records := deepAgentEvidenceRecordsForRun(run, state, time.Now().UTC())
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.records == nil {
		r.records = make(map[string]DeepAgentEvidenceRecord)
	}
	for _, record := range records {
		if existing, ok := r.records[record.ID]; ok && !existing.CreatedAt.IsZero() {
			record.CreatedAt = existing.CreatedAt
		}
		r.records[record.ID] = record
	}
	return nil
}

func (r *MemoryDeepAgentEvidenceRepository) ListDeepAgentEvidence(_ context.Context, filter DeepAgentEvidenceFilter) ([]DeepAgentEvidenceRecord, error) {
	if r == nil {
		return []DeepAgentEvidenceRecord{}, nil
	}
	filter = normalizeDeepAgentEvidenceFilter(filter)
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]DeepAgentEvidenceRecord, 0, len(r.records))
	for _, record := range r.records {
		if !deepAgentEvidenceRecordMatches(record, filter) {
			continue
		}
		out = append(out, cloneDeepAgentEvidenceRecord(record))
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	if filter.Limit > 0 && len(out) > filter.Limit {
		out = out[:filter.Limit]
	}
	return out, nil
}

type SQLDeepAgentEvidenceRepository struct {
	db      *sql.DB
	dialect SQLDialect
}

func NewSQLDeepAgentEvidenceRepositoryWithDialect(db *sql.DB, dialect SQLDialect) *SQLDeepAgentEvidenceRepository {
	if dialect == "" {
		dialect = SQLDialectQuestion
	}
	return &SQLDeepAgentEvidenceRepository{db: db, dialect: dialect}
}

func (r *SQLDeepAgentEvidenceRepository) Init(ctx context.Context) error {
	return requireSQLColumns(ctx, r.db, "agent_deep_agent_evidence",
		"id", "run_id", "user_id", "session_id", "job_id", "loop_goal_id",
		"step_id", "action_id", "template_id", "task_type", "trigger_type",
		"route_json", "evidence_json", "artifact_count", "source_count", "tool_call_count",
		"child_job_count", "error_class", "side_effect_level", "created_at", "updated_at",
	)
}

func (r *SQLDeepAgentEvidenceRepository) UpsertRunEvidence(ctx context.Context, run *WorkflowRun, state *DeepAgentState) error {
	if r == nil || r.db == nil || run == nil || state == nil {
		return nil
	}
	records := deepAgentEvidenceRecordsForRun(run, state, time.Now().UTC())
	for _, record := range records {
		routeJSON, err := json.Marshal(record.Route)
		if err != nil {
			return err
		}
		evidenceJSON, err := json.Marshal(record.Evidence)
		if err != nil {
			return err
		}
		_, err = r.db.ExecContext(ctx, r.dialect.Bind(`
INSERT INTO agent_deep_agent_evidence (
	id, run_id, user_id, session_id, job_id, loop_goal_id, step_id, action_id,
	template_id, task_type, trigger_type, route_json, evidence_json, artifact_count,
	source_count, tool_call_count, child_job_count, error_class, side_effect_level,
	created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (id) DO UPDATE SET
	run_id = EXCLUDED.run_id,
	user_id = EXCLUDED.user_id,
	session_id = EXCLUDED.session_id,
	job_id = EXCLUDED.job_id,
	loop_goal_id = EXCLUDED.loop_goal_id,
	step_id = EXCLUDED.step_id,
	action_id = EXCLUDED.action_id,
	template_id = EXCLUDED.template_id,
	task_type = EXCLUDED.task_type,
	trigger_type = EXCLUDED.trigger_type,
	route_json = EXCLUDED.route_json,
	evidence_json = EXCLUDED.evidence_json,
	artifact_count = EXCLUDED.artifact_count,
	source_count = EXCLUDED.source_count,
	tool_call_count = EXCLUDED.tool_call_count,
	child_job_count = EXCLUDED.child_job_count,
	error_class = EXCLUDED.error_class,
	side_effect_level = EXCLUDED.side_effect_level,
	updated_at = EXCLUDED.updated_at
`),
			record.ID,
			record.RunID,
			record.UserID,
			record.SessionID,
			record.JobID,
			record.LoopGoalID,
			record.StepID,
			record.ActionID,
			record.TemplateID,
			record.TaskType,
			record.TriggerType,
			string(routeJSON),
			string(evidenceJSON),
			record.ArtifactCount,
			record.SourceCount,
			record.ToolCallCount,
			record.ChildJobCount,
			record.ErrorClass,
			record.SideEffectLevel,
			sqlTimeValue(record.CreatedAt, r.dialect),
			sqlTimeValue(record.UpdatedAt, r.dialect),
		)
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *SQLDeepAgentEvidenceRepository) ListDeepAgentEvidence(ctx context.Context, filter DeepAgentEvidenceFilter) ([]DeepAgentEvidenceRecord, error) {
	if r == nil || r.db == nil {
		return []DeepAgentEvidenceRecord{}, nil
	}
	filter = normalizeDeepAgentEvidenceFilter(filter)
	query := `SELECT id, run_id, user_id, session_id, job_id, loop_goal_id, step_id, action_id, template_id, task_type, trigger_type, route_json, evidence_json, artifact_count, source_count, tool_call_count, child_job_count, error_class, side_effect_level, created_at, updated_at FROM agent_deep_agent_evidence`
	where, args := deepAgentEvidenceWhere(filter)
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY created_at DESC"
	if filter.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filter.Limit)
	}
	rows, err := r.db.QueryContext(ctx, r.dialect.Bind(query), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DeepAgentEvidenceRecord{}
	for rows.Next() {
		record, err := scanSQLDeepAgentEvidenceRecord(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	return out, rows.Err()
}

type deepAgentEvidenceScanner interface {
	Scan(dest ...any) error
}

func scanSQLDeepAgentEvidenceRecord(row deepAgentEvidenceScanner) (DeepAgentEvidenceRecord, error) {
	var record DeepAgentEvidenceRecord
	var routeJSON, evidenceJSON string
	var createdAt, updatedAt any
	if err := row.Scan(
		&record.ID,
		&record.RunID,
		&record.UserID,
		&record.SessionID,
		&record.JobID,
		&record.LoopGoalID,
		&record.StepID,
		&record.ActionID,
		&record.TemplateID,
		&record.TaskType,
		&record.TriggerType,
		&routeJSON,
		&evidenceJSON,
		&record.ArtifactCount,
		&record.SourceCount,
		&record.ToolCallCount,
		&record.ChildJobCount,
		&record.ErrorClass,
		&record.SideEffectLevel,
		&createdAt,
		&updatedAt,
	); err != nil {
		return DeepAgentEvidenceRecord{}, err
	}
	if strings.TrimSpace(routeJSON) != "" {
		_ = json.Unmarshal([]byte(routeJSON), &record.Route)
	}
	if strings.TrimSpace(evidenceJSON) != "" {
		_ = json.Unmarshal([]byte(evidenceJSON), &record.Evidence)
	}
	parsedCreatedAt, err := parseSQLTime(createdAt)
	if err != nil {
		return DeepAgentEvidenceRecord{}, err
	}
	parsedUpdatedAt, err := parseSQLTime(updatedAt)
	if err != nil {
		return DeepAgentEvidenceRecord{}, err
	}
	record.CreatedAt = parsedCreatedAt
	record.UpdatedAt = parsedUpdatedAt
	return record, nil
}

func deepAgentEvidenceRecordsForRun(run *WorkflowRun, state *DeepAgentState, now time.Time) []DeepAgentEvidenceRecord {
	if run == nil || state == nil {
		return nil
	}
	triggerType := deepAgentWorkflowString(state.WorkingMemory, "trigger_type")
	templateID := deepAgentTemplateID(state)
	taskType := deepAgentTaskType(state)
	evidence := (StateDeepAgentEvidenceStore{}).ListStepEvidence(state)
	out := make([]DeepAgentEvidenceRecord, 0, len(evidence))
	for _, item := range evidence {
		stepID := firstNonEmptyString(item.StepID, item.Route.StepID)
		actionID := strings.TrimSpace(item.ActionID)
		id := deepAgentEvidenceRecordID(run.ID, stepID, actionID, item.Summary)
		if id == "" {
			continue
		}
		out = append(out, DeepAgentEvidenceRecord{
			ID:              id,
			RunID:           run.ID,
			UserID:          run.UserID,
			SessionID:       run.SessionID,
			JobID:           run.JobID,
			LoopGoalID:      run.JobID,
			StepID:          stepID,
			ActionID:        actionID,
			TemplateID:      templateID,
			TaskType:        taskType,
			TriggerType:     triggerType,
			Route:           item.Route,
			Evidence:        item,
			ArtifactCount:   len(item.Artifacts),
			SourceCount:     len(item.Sources),
			ToolCallCount:   len(item.ToolCalls),
			ChildJobCount:   len(item.ChildJobs),
			ErrorClass:      strings.TrimSpace(item.ErrorClass),
			SideEffectLevel: strings.TrimSpace(item.SideEffectLevel),
			CreatedAt:       now,
			UpdatedAt:       now,
		})
	}
	return out
}

func deepAgentEvidenceRecordID(runID, stepID, actionID, fallback string) string {
	raw := strings.Join([]string{strings.TrimSpace(runID), strings.TrimSpace(stepID), strings.TrimSpace(actionID), strings.TrimSpace(fallback)}, "|")
	if strings.Trim(raw, "| ") == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(raw))
	return "dae-" + hex.EncodeToString(sum[:16])
}

func deepAgentEvidenceWhere(filter DeepAgentEvidenceFilter) ([]string, []any) {
	where := []string{}
	args := []any{}
	add := func(column, value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		where = append(where, column+" = ?")
		args = append(args, value)
	}
	add("user_id", filter.UserID)
	add("session_id", filter.SessionID)
	add("run_id", filter.RunID)
	add("job_id", filter.JobID)
	add("loop_goal_id", filter.LoopGoalID)
	add("step_id", filter.StepID)
	add("task_type", filter.TaskType)
	add("template_id", filter.TemplateID)
	return where, args
}

func normalizeDeepAgentEvidenceFilter(filter DeepAgentEvidenceFilter) DeepAgentEvidenceFilter {
	filter.UserID = strings.TrimSpace(filter.UserID)
	filter.SessionID = strings.TrimSpace(filter.SessionID)
	filter.RunID = strings.TrimSpace(filter.RunID)
	filter.JobID = strings.TrimSpace(filter.JobID)
	filter.LoopGoalID = strings.TrimSpace(filter.LoopGoalID)
	filter.StepID = strings.TrimSpace(filter.StepID)
	filter.TaskType = strings.TrimSpace(filter.TaskType)
	filter.TemplateID = normalizeDeepAgentTemplateID(strings.TrimSpace(filter.TemplateID))
	if filter.Limit < 0 {
		filter.Limit = 0
	}
	return filter
}

func deepAgentEvidenceRecordMatches(record DeepAgentEvidenceRecord, filter DeepAgentEvidenceFilter) bool {
	if filter.UserID != "" && record.UserID != filter.UserID {
		return false
	}
	if filter.SessionID != "" && record.SessionID != filter.SessionID {
		return false
	}
	if filter.RunID != "" && record.RunID != filter.RunID {
		return false
	}
	if filter.JobID != "" && record.JobID != filter.JobID {
		return false
	}
	if filter.LoopGoalID != "" && record.LoopGoalID != filter.LoopGoalID {
		return false
	}
	if filter.StepID != "" && record.StepID != filter.StepID {
		return false
	}
	if filter.TaskType != "" && record.TaskType != filter.TaskType {
		return false
	}
	if filter.TemplateID != "" && record.TemplateID != filter.TemplateID {
		return false
	}
	return true
}

func cloneDeepAgentEvidenceRecord(record DeepAgentEvidenceRecord) DeepAgentEvidenceRecord {
	var out DeepAgentEvidenceRecord
	raw, err := json.Marshal(record)
	if err == nil {
		_ = json.Unmarshal(raw, &out)
	}
	if out.ID == "" {
		out = record
	}
	return out
}

func deepAgentEvidenceRepositoryError(err error) string {
	if err == nil || errors.Is(err, context.Canceled) {
		return ""
	}
	return err.Error()
}
