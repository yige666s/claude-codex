package agentruntime

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"claude-codex/internal/backend/agentruntime/dbsqlc"
)

const (
	LoopGoalStatusPending        = "pending"
	LoopGoalStatusRunning        = "running"
	LoopGoalStatusSucceeded      = "succeeded"
	LoopGoalStatusFailed         = "failed"
	LoopGoalStatusBlocked        = "blocked"
	LoopGoalStatusBudgetExceeded = "budget_exceeded"
	LoopGoalStatusReviewPending  = "review_pending"
	LoopGoalStatusCancelled      = "cancelled"

	LoopTriggerManual   = "manual"
	LoopTriggerSchedule = "schedule"
	LoopTriggerWebhook  = "webhook"
	LoopTriggerMonitor  = "monitor"
	LoopTriggerEval     = "eval"
	LoopTriggerMemory   = "memory"
)

type LoopGoal struct {
	ID            string         `json:"id"`
	UserID        string         `json:"user_id,omitempty"`
	SessionID     string         `json:"session_id,omitempty"`
	JobID         string         `json:"job_id,omitempty"`
	WorkflowRunID string         `json:"workflow_run_id,omitempty"`
	Objective     string         `json:"objective"`
	TaskType      string         `json:"task_type,omitempty"`
	Deliverable   string         `json:"deliverable,omitempty"`
	Rubric        LoopRubric     `json:"rubric,omitempty"`
	Budget        LoopBudget     `json:"budget,omitempty"`
	Trigger       LoopTrigger    `json:"trigger,omitempty"`
	StopPolicy    LoopStopPolicy `json:"stop_policy,omitempty"`
	Status        string         `json:"status"`
	Metadata      map[string]any `json:"metadata,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
	StartedAt     *time.Time     `json:"started_at,omitempty"`
	FinishedAt    *time.Time     `json:"finished_at,omitempty"`
}

type LoopRubric struct {
	AcceptanceCriteria []string `json:"acceptance_criteria,omitempty"`
	RequiredEvidence   []string `json:"required_evidence,omitempty"`
	RequiredArtifacts  []string `json:"required_artifacts,omitempty"`
	ForbiddenActions   []string `json:"forbidden_actions,omitempty"`
	QualityBar         string   `json:"quality_bar,omitempty"`
}

type LoopBudget struct {
	MaxSteps     int           `json:"max_steps,omitempty"`
	MaxActions   int           `json:"max_actions,omitempty"`
	MaxDuration  time.Duration `json:"-"`
	MaxTokens    int64         `json:"max_tokens,omitempty"`
	MaxCostCents int64         `json:"max_cost_cents,omitempty"`
	MaxToolCalls int           `json:"max_tool_calls,omitempty"`
}

func (b LoopBudget) MarshalJSON() ([]byte, error) {
	return json.Marshal(loopBudgetJSON(b))
}

func (b *LoopBudget) UnmarshalJSON(data []byte) error {
	var persisted loopBudgetPersisted
	if err := json.Unmarshal(data, &persisted); err != nil {
		return err
	}
	*b = persisted.LoopBudget()
	return nil
}

type LoopTrigger struct {
	Type           string         `json:"type,omitempty"`
	Source         string         `json:"source,omitempty"`
	DedupeKey      string         `json:"dedupe_key,omitempty"`
	Payload        map[string]any `json:"payload,omitempty"`
	PermissionHint string         `json:"permission_hint,omitempty"`
}

type LoopStopPolicy struct {
	OnComplete       string `json:"on_complete,omitempty"`
	OnBlocked        string `json:"on_blocked,omitempty"`
	OnBudgetExceeded string `json:"on_budget_exceeded,omitempty"`
	OnReviewPending  string `json:"on_review_pending,omitempty"`
}

type LoopGoalFilter struct {
	UserID    string
	SessionID string
	Status    string
	Limit     int
}

type LoopGoalStore interface {
	Init(ctx context.Context) error
	UpsertLoopGoal(ctx context.Context, goal *LoopGoal) error
	GetLoopGoal(ctx context.Context, userID, goalID string) (*LoopGoal, error)
	GetLoopGoalByWorkflowRun(ctx context.Context, userID, runID string) (*LoopGoal, error)
	ListLoopGoals(ctx context.Context, filter LoopGoalFilter) ([]*LoopGoal, error)
	UpdateLoopGoalRun(ctx context.Context, userID, goalID, jobID, workflowRunID, status string, at time.Time) error
	UpdateLoopGoalStatus(ctx context.Context, userID, goalID, status string, at time.Time) error
}

func NewLoopGoalID() string {
	return "goal-" + newSortableID()
}

func normalizeLoopGoal(goal *LoopGoal) *LoopGoal {
	if goal == nil {
		return nil
	}
	now := time.Now().UTC()
	out := cloneLoopGoal(goal)
	out.ID = strings.TrimSpace(out.ID)
	if out.ID == "" {
		out.ID = NewLoopGoalID()
	}
	out.UserID = strings.TrimSpace(out.UserID)
	out.SessionID = strings.TrimSpace(out.SessionID)
	out.JobID = strings.TrimSpace(out.JobID)
	out.WorkflowRunID = strings.TrimSpace(out.WorkflowRunID)
	out.Objective = strings.TrimSpace(out.Objective)
	out.TaskType = strings.TrimSpace(out.TaskType)
	out.Deliverable = strings.TrimSpace(out.Deliverable)
	out = applyLoopTemplateToGoal(out)
	out.Status = normalizeLoopGoalStatus(out.Status)
	out.Trigger = normalizeLoopTrigger(out.Trigger)
	out.StopPolicy = normalizeLoopStopPolicy(out.StopPolicy)
	if out.Metadata == nil {
		out.Metadata = map[string]any{}
	}
	if out.CreatedAt.IsZero() {
		out.CreatedAt = now
	}
	if out.UpdatedAt.IsZero() {
		out.UpdatedAt = out.CreatedAt
	}
	return out
}

func normalizeLoopGoalStatus(status string) string {
	status = strings.TrimSpace(status)
	if status == "" {
		return LoopGoalStatusPending
	}
	return status
}

func normalizeLoopTrigger(trigger LoopTrigger) LoopTrigger {
	trigger.Type = strings.TrimSpace(trigger.Type)
	if trigger.Type == "" {
		trigger.Type = LoopTriggerManual
	}
	trigger.Source = strings.TrimSpace(trigger.Source)
	trigger.DedupeKey = strings.TrimSpace(trigger.DedupeKey)
	trigger.PermissionHint = strings.TrimSpace(trigger.PermissionHint)
	if trigger.Payload == nil {
		trigger.Payload = map[string]any{}
	}
	return trigger
}

func normalizeLoopStopPolicy(policy LoopStopPolicy) LoopStopPolicy {
	if strings.TrimSpace(policy.OnComplete) == "" {
		policy.OnComplete = "finish"
	}
	if strings.TrimSpace(policy.OnBlocked) == "" {
		policy.OnBlocked = "wait_for_user"
	}
	if strings.TrimSpace(policy.OnBudgetExceeded) == "" {
		policy.OnBudgetExceeded = "wait_for_budget"
	}
	if strings.TrimSpace(policy.OnReviewPending) == "" {
		policy.OnReviewPending = "wait_for_review"
	}
	return policy
}

func loopBudgetFromDeepAgentPolicy(policy DeepAgentPolicy) LoopBudget {
	policy = normalizeDeepAgentPolicy(policy)
	return LoopBudget{
		MaxSteps:    policy.MaxSteps,
		MaxActions:  policy.MaxActions,
		MaxDuration: policy.MaxDuration,
	}
}

func deepAgentPolicyFromLoopBudget(budget LoopBudget, fallback DeepAgentPolicy) DeepAgentPolicy {
	policy := normalizeDeepAgentPolicy(fallback)
	if budget.MaxSteps > 0 {
		policy.MaxSteps = budget.MaxSteps
	}
	if budget.MaxActions > 0 {
		policy.MaxActions = budget.MaxActions
	}
	if budget.MaxDuration > 0 {
		policy.MaxDuration = budget.MaxDuration
	}
	return normalizeDeepAgentPolicy(policy)
}

func normalizeDeepAgentRubric(rubric DeepAgentRubric) DeepAgentRubric {
	rubric.AcceptanceCriteria = normalizeLoopGoalStrings(rubric.AcceptanceCriteria)
	rubric.RequiredEvidence = normalizeLoopGoalStrings(rubric.RequiredEvidence)
	rubric.RequiredArtifacts = normalizeLoopGoalStrings(rubric.RequiredArtifacts)
	rubric.ForbiddenActions = normalizeLoopGoalStrings(rubric.ForbiddenActions)
	rubric.QualityBar = strings.TrimSpace(rubric.QualityBar)
	return rubric
}

func deepAgentRubricEmpty(rubric DeepAgentRubric) bool {
	rubric = normalizeDeepAgentRubric(rubric)
	return len(rubric.AcceptanceCriteria) == 0 &&
		len(rubric.RequiredEvidence) == 0 &&
		len(rubric.RequiredArtifacts) == 0 &&
		len(rubric.ForbiddenActions) == 0 &&
		rubric.QualityBar == ""
}

func normalizeLoopGoalStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func loopGoalStatusFromDeepAgent(status string) string {
	switch strings.TrimSpace(status) {
	case DeepAgentRunStatusSucceeded:
		return LoopGoalStatusSucceeded
	case DeepAgentRunStatusBlocked:
		return LoopGoalStatusBlocked
	case DeepAgentRunStatusBudgetExceeded:
		return LoopGoalStatusBudgetExceeded
	case DeepAgentRunStatusReviewPending:
		return LoopGoalStatusReviewPending
	case DeepAgentRunStatusFailed:
		return LoopGoalStatusFailed
	case DeepAgentRunStatusRunning:
		return LoopGoalStatusRunning
	default:
		return normalizeLoopGoalStatus(status)
	}
}

func cloneLoopGoal(goal *LoopGoal) *LoopGoal {
	if goal == nil {
		return nil
	}
	out := *goal
	out.Rubric.AcceptanceCriteria = append([]string(nil), goal.Rubric.AcceptanceCriteria...)
	out.Rubric.RequiredEvidence = append([]string(nil), goal.Rubric.RequiredEvidence...)
	out.Rubric.RequiredArtifacts = append([]string(nil), goal.Rubric.RequiredArtifacts...)
	out.Rubric.ForbiddenActions = append([]string(nil), goal.Rubric.ForbiddenActions...)
	if goal.Trigger.Payload != nil {
		out.Trigger.Payload = cloneWorkflowMap(goal.Trigger.Payload)
	}
	if goal.Metadata != nil {
		out.Metadata = cloneWorkflowMap(goal.Metadata)
	}
	if goal.StartedAt != nil {
		t := goal.StartedAt.UTC()
		out.StartedAt = &t
	}
	if goal.FinishedAt != nil {
		t := goal.FinishedAt.UTC()
		out.FinishedAt = &t
	}
	return &out
}

type MemoryLoopGoalStore struct {
	mu    sync.Mutex
	goals map[string]*LoopGoal
}

func NewMemoryLoopGoalStore() *MemoryLoopGoalStore {
	return &MemoryLoopGoalStore{goals: make(map[string]*LoopGoal)}
}

func (s *MemoryLoopGoalStore) Init(context.Context) error { return nil }

func (s *MemoryLoopGoalStore) UpsertLoopGoal(_ context.Context, goal *LoopGoal) error {
	goal = normalizeLoopGoal(goal)
	if goal == nil || goal.UserID == "" || goal.Objective == "" {
		return fmt.Errorf("loop goal user_id and objective are required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.goals[goal.ID] = cloneLoopGoal(goal)
	return nil
}

func (s *MemoryLoopGoalStore) GetLoopGoal(_ context.Context, userID, goalID string) (*LoopGoal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	goal, ok := s.goals[strings.TrimSpace(goalID)]
	if !ok || goal.UserID != strings.TrimSpace(userID) {
		return nil, sql.ErrNoRows
	}
	return cloneLoopGoal(goal), nil
}

func (s *MemoryLoopGoalStore) GetLoopGoalByWorkflowRun(_ context.Context, userID, runID string) (*LoopGoal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, goal := range s.goals {
		if goal.UserID == strings.TrimSpace(userID) && goal.WorkflowRunID == strings.TrimSpace(runID) {
			return cloneLoopGoal(goal), nil
		}
	}
	return nil, sql.ErrNoRows
}

func (s *MemoryLoopGoalStore) ListLoopGoals(_ context.Context, filter LoopGoalFilter) ([]*LoopGoal, error) {
	filter = normalizeLoopGoalFilter(filter)
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*LoopGoal, 0, len(s.goals))
	for _, goal := range s.goals {
		if filter.UserID != "" && goal.UserID != filter.UserID {
			continue
		}
		if filter.SessionID != "" && goal.SessionID != filter.SessionID {
			continue
		}
		if filter.Status != "" && goal.Status != filter.Status {
			continue
		}
		out = append(out, cloneLoopGoal(goal))
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	if filter.Limit > 0 && len(out) > filter.Limit {
		out = out[:filter.Limit]
	}
	return out, nil
}

func (s *MemoryLoopGoalStore) UpdateLoopGoalRun(_ context.Context, userID, goalID, jobID, workflowRunID, status string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	goal, ok := s.goals[strings.TrimSpace(goalID)]
	if !ok || goal.UserID != strings.TrimSpace(userID) {
		return sql.ErrNoRows
	}
	goal.JobID = firstNonEmptyString(strings.TrimSpace(jobID), goal.JobID)
	goal.WorkflowRunID = firstNonEmptyString(strings.TrimSpace(workflowRunID), goal.WorkflowRunID)
	goal.Status = normalizeLoopGoalStatus(status)
	if goal.Status == LoopGoalStatusRunning && goal.StartedAt == nil {
		t := at.UTC()
		goal.StartedAt = &t
	}
	if isTerminalLoopGoalStatus(goal.Status) {
		t := at.UTC()
		goal.FinishedAt = &t
	}
	goal.UpdatedAt = at.UTC()
	return nil
}

func (s *MemoryLoopGoalStore) UpdateLoopGoalStatus(_ context.Context, userID, goalID, status string, at time.Time) error {
	return s.UpdateLoopGoalRun(context.Background(), userID, goalID, "", "", status, at)
}

type SQLLoopGoalStore struct {
	db      *sql.DB
	dialect SQLDialect
	queries *dbsqlc.Queries
}

func NewSQLLoopGoalStoreWithDialect(db *sql.DB, dialect SQLDialect) *SQLLoopGoalStore {
	if dialect == "" {
		dialect = SQLDialectQuestion
	}
	return &SQLLoopGoalStore{db: db, dialect: dialect, queries: dbsqlc.New(db)}
}

func (s *SQLLoopGoalStore) Init(ctx context.Context) error {
	return requireSQLColumns(ctx, s.db, "agent_loop_goals",
		"id", "user_id", "session_id", "job_id", "workflow_run_id", "status", "objective",
		"task_type", "deliverable", "rubric_json", "budget_json", "trigger_json", "stop_policy_json",
		"metadata_json", "created_at", "updated_at", "started_at", "finished_at",
	)
}

func (s *SQLLoopGoalStore) UpsertLoopGoal(ctx context.Context, goal *LoopGoal) error {
	goal = normalizeLoopGoal(goal)
	if goal == nil || goal.UserID == "" || goal.Objective == "" {
		return fmt.Errorf("loop goal user_id and objective are required")
	}
	rubricJSON, err := json.Marshal(goal.Rubric)
	if err != nil {
		return err
	}
	budgetJSON, err := json.Marshal(loopBudgetJSON(goal.Budget))
	if err != nil {
		return err
	}
	triggerJSON, err := json.Marshal(goal.Trigger)
	if err != nil {
		return err
	}
	stopPolicyJSON, err := json.Marshal(goal.StopPolicy)
	if err != nil {
		return err
	}
	metadataJSON, err := json.Marshal(goal.Metadata)
	if err != nil {
		return err
	}
	if s.dialect == SQLDialectPostgres && s.queries != nil {
		return s.queries.UpsertLoopGoal(ctx, dbsqlc.UpsertLoopGoalParams{
			ID:             goal.ID,
			UserID:         goal.UserID,
			SessionID:      goal.SessionID,
			JobID:          goal.JobID,
			WorkflowRunID:  goal.WorkflowRunID,
			Status:         goal.Status,
			Objective:      goal.Objective,
			TaskType:       goal.TaskType,
			Deliverable:    goal.Deliverable,
			RubricJson:     json.RawMessage(rubricJSON),
			BudgetJson:     json.RawMessage(budgetJSON),
			TriggerJson:    json.RawMessage(triggerJSON),
			StopPolicyJson: json.RawMessage(stopPolicyJSON),
			MetadataJson:   json.RawMessage(metadataJSON),
			CreatedAt:      goal.CreatedAt.UTC(),
			UpdatedAt:      goal.UpdatedAt.UTC(),
			StartedAt:      sqlNullTime(goal.StartedAt),
			FinishedAt:     sqlNullTime(goal.FinishedAt),
		})
	}
	_, err = s.db.ExecContext(ctx, s.dialect.Bind(`
INSERT INTO agent_loop_goals (id, user_id, session_id, job_id, workflow_run_id, status, objective, task_type, deliverable, rubric_json, budget_json, trigger_json, stop_policy_json, metadata_json, created_at, updated_at, started_at, finished_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (id) DO UPDATE SET
	user_id = excluded.user_id,
	session_id = excluded.session_id,
	job_id = excluded.job_id,
	workflow_run_id = excluded.workflow_run_id,
	status = excluded.status,
	objective = excluded.objective,
	task_type = excluded.task_type,
	deliverable = excluded.deliverable,
	rubric_json = excluded.rubric_json,
	budget_json = excluded.budget_json,
	trigger_json = excluded.trigger_json,
	stop_policy_json = excluded.stop_policy_json,
	metadata_json = excluded.metadata_json,
	updated_at = excluded.updated_at,
	started_at = excluded.started_at,
	finished_at = excluded.finished_at`),
		goal.ID, goal.UserID, goal.SessionID, goal.JobID, goal.WorkflowRunID, goal.Status, goal.Objective, goal.TaskType, goal.Deliverable,
		string(rubricJSON), string(budgetJSON), string(triggerJSON), string(stopPolicyJSON), string(metadataJSON),
		sqlTimeValue(goal.CreatedAt, s.dialect), sqlTimeValue(goal.UpdatedAt, s.dialect), nullableSQLTimeValue(goal.StartedAt, s.dialect), nullableSQLTimeValue(goal.FinishedAt, s.dialect))
	return err
}

func (s *SQLLoopGoalStore) GetLoopGoal(ctx context.Context, userID, goalID string) (*LoopGoal, error) {
	if s.dialect == SQLDialectPostgres && s.queries != nil {
		row, err := s.queries.GetLoopGoal(ctx, dbsqlc.GetLoopGoalParams{UserID: userID, ID: goalID})
		if err != nil {
			return nil, err
		}
		return loopGoalFromSQLC(row)
	}
	return scanSQLLoopGoal(s.db.QueryRowContext(ctx, s.dialect.Bind(`
SELECT id, user_id, session_id, job_id, workflow_run_id, status, objective, task_type, deliverable, rubric_json, budget_json, trigger_json, stop_policy_json, metadata_json, created_at, updated_at, started_at, finished_at
FROM agent_loop_goals WHERE user_id = ? AND id = ?`), userID, goalID))
}

func (s *SQLLoopGoalStore) GetLoopGoalByWorkflowRun(ctx context.Context, userID, runID string) (*LoopGoal, error) {
	if s.dialect == SQLDialectPostgres && s.queries != nil {
		row, err := s.queries.GetLoopGoalByWorkflowRun(ctx, dbsqlc.GetLoopGoalByWorkflowRunParams{UserID: userID, WorkflowRunID: runID})
		if err != nil {
			return nil, err
		}
		return loopGoalFromSQLC(row)
	}
	return scanSQLLoopGoal(s.db.QueryRowContext(ctx, s.dialect.Bind(`
SELECT id, user_id, session_id, job_id, workflow_run_id, status, objective, task_type, deliverable, rubric_json, budget_json, trigger_json, stop_policy_json, metadata_json, created_at, updated_at, started_at, finished_at
FROM agent_loop_goals WHERE user_id = ? AND workflow_run_id = ?`), userID, runID))
}

func (s *SQLLoopGoalStore) ListLoopGoals(ctx context.Context, filter LoopGoalFilter) ([]*LoopGoal, error) {
	filter = normalizeLoopGoalFilter(filter)
	if s.dialect == SQLDialectPostgres && s.queries != nil {
		rows, err := s.queries.ListLoopGoals(ctx, dbsqlc.ListLoopGoalsParams{
			UserID:     filter.UserID,
			SessionID:  filter.SessionID,
			Status:     filter.Status,
			LimitCount: int32(filter.Limit),
		})
		if err != nil {
			return nil, err
		}
		out := make([]*LoopGoal, 0, len(rows))
		for _, row := range rows {
			goal, err := loopGoalFromSQLC(row)
			if err != nil {
				return nil, err
			}
			out = append(out, goal)
		}
		return out, nil
	}
	query := `SELECT id, user_id, session_id, job_id, workflow_run_id, status, objective, task_type, deliverable, rubric_json, budget_json, trigger_json, stop_policy_json, metadata_json, created_at, updated_at, started_at, finished_at FROM agent_loop_goals WHERE 1 = 1`
	args := []any{}
	if filter.UserID != "" {
		query += ` AND user_id = ?`
		args = append(args, filter.UserID)
	}
	if filter.SessionID != "" {
		query += ` AND session_id = ?`
		args = append(args, filter.SessionID)
	}
	if filter.Status != "" {
		query += ` AND status = ?`
		args = append(args, filter.Status)
	}
	query += ` ORDER BY updated_at DESC`
	if filter.Limit > 0 {
		query += ` LIMIT ?`
		args = append(args, filter.Limit)
	}
	rows, err := s.db.QueryContext(ctx, s.dialect.Bind(query), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*LoopGoal{}
	for rows.Next() {
		goal, err := scanSQLLoopGoal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, goal)
	}
	return out, rows.Err()
}

func (s *SQLLoopGoalStore) UpdateLoopGoalRun(ctx context.Context, userID, goalID, jobID, workflowRunID, status string, at time.Time) error {
	if s.dialect == SQLDialectPostgres && s.queries != nil {
		return s.queries.UpdateLoopGoalRun(ctx, dbsqlc.UpdateLoopGoalRunParams{
			Column1:    strings.TrimSpace(jobID),
			Column2:    strings.TrimSpace(workflowRunID),
			Status:     normalizeLoopGoalStatus(status),
			UpdatedAt:  at.UTC(),
			StartedAt:  sqlNullTime(loopGoalStartedAt(status, at)),
			FinishedAt: sqlNullTime(loopGoalFinishedAt(status, at)),
			UserID:     strings.TrimSpace(userID),
			ID:         strings.TrimSpace(goalID),
		})
	}
	_, err := s.db.ExecContext(ctx, s.dialect.Bind(`
UPDATE agent_loop_goals
SET job_id = COALESCE(NULLIF(?, ''), job_id),
	workflow_run_id = COALESCE(NULLIF(?, ''), workflow_run_id),
	status = ?,
	updated_at = ?,
	started_at = COALESCE(started_at, ?),
	finished_at = COALESCE(?, finished_at)
WHERE user_id = ? AND id = ?`),
		strings.TrimSpace(jobID), strings.TrimSpace(workflowRunID), normalizeLoopGoalStatus(status), sqlTimeValue(at, s.dialect),
		nullableSQLTimeValue(loopGoalStartedAt(status, at), s.dialect), nullableSQLTimeValue(loopGoalFinishedAt(status, at), s.dialect), userID, goalID)
	return err
}

func (s *SQLLoopGoalStore) UpdateLoopGoalStatus(ctx context.Context, userID, goalID, status string, at time.Time) error {
	if s.dialect == SQLDialectPostgres && s.queries != nil {
		return s.queries.UpdateLoopGoalStatus(ctx, dbsqlc.UpdateLoopGoalStatusParams{
			Status:     normalizeLoopGoalStatus(status),
			UpdatedAt:  at.UTC(),
			StartedAt:  sqlNullTime(loopGoalStartedAt(status, at)),
			FinishedAt: sqlNullTime(loopGoalFinishedAt(status, at)),
			UserID:     strings.TrimSpace(userID),
			ID:         strings.TrimSpace(goalID),
		})
	}
	_, err := s.db.ExecContext(ctx, s.dialect.Bind(`
UPDATE agent_loop_goals
SET status = ?, updated_at = ?, started_at = COALESCE(started_at, ?), finished_at = COALESCE(?, finished_at)
WHERE user_id = ? AND id = ?`),
		normalizeLoopGoalStatus(status), sqlTimeValue(at, s.dialect), nullableSQLTimeValue(loopGoalStartedAt(status, at), s.dialect), nullableSQLTimeValue(loopGoalFinishedAt(status, at), s.dialect), userID, goalID)
	return err
}

type loopGoalScanner interface {
	Scan(dest ...any) error
}

func scanSQLLoopGoal(row loopGoalScanner) (*LoopGoal, error) {
	var goal LoopGoal
	var rubricJSON, budgetJSON, triggerJSON, stopPolicyJSON, metadataJSON string
	var createdAt, updatedAt, startedAt, finishedAt any
	if err := row.Scan(&goal.ID, &goal.UserID, &goal.SessionID, &goal.JobID, &goal.WorkflowRunID, &goal.Status, &goal.Objective, &goal.TaskType, &goal.Deliverable, &rubricJSON, &budgetJSON, &triggerJSON, &stopPolicyJSON, &metadataJSON, &createdAt, &updatedAt, &startedAt, &finishedAt); err != nil {
		return nil, err
	}
	if err := unmarshalLoopGoalJSON(rubricJSON, &goal.Rubric); err != nil {
		return nil, err
	}
	var budget loopBudgetPersisted
	if err := unmarshalLoopGoalJSON(budgetJSON, &budget); err != nil {
		return nil, err
	}
	goal.Budget = budget.LoopBudget()
	if err := unmarshalLoopGoalJSON(triggerJSON, &goal.Trigger); err != nil {
		return nil, err
	}
	if err := unmarshalLoopGoalJSON(stopPolicyJSON, &goal.StopPolicy); err != nil {
		return nil, err
	}
	if err := unmarshalLoopGoalJSON(metadataJSON, &goal.Metadata); err != nil {
		return nil, err
	}
	var err error
	if goal.CreatedAt, err = parseSQLTime(createdAt); err != nil {
		return nil, err
	}
	if goal.UpdatedAt, err = parseSQLTime(updatedAt); err != nil {
		return nil, err
	}
	if goal.StartedAt, err = parseNullableSQLTime(startedAt); err != nil {
		return nil, err
	}
	if goal.FinishedAt, err = parseNullableSQLTime(finishedAt); err != nil {
		return nil, err
	}
	return normalizeLoopGoal(&goal), nil
}

func loopGoalFromSQLC(row dbsqlc.AgentLoopGoal) (*LoopGoal, error) {
	goal := &LoopGoal{
		ID:            row.ID,
		UserID:        row.UserID,
		SessionID:     row.SessionID,
		JobID:         row.JobID,
		WorkflowRunID: row.WorkflowRunID,
		Status:        row.Status,
		Objective:     row.Objective,
		TaskType:      row.TaskType,
		Deliverable:   row.Deliverable,
		CreatedAt:     row.CreatedAt.UTC(),
		UpdatedAt:     row.UpdatedAt.UTC(),
		StartedAt:     timeFromNull(row.StartedAt),
		FinishedAt:    timeFromNull(row.FinishedAt),
	}
	if err := unmarshalLoopGoalJSON(string(row.RubricJson), &goal.Rubric); err != nil {
		return nil, err
	}
	var budget loopBudgetPersisted
	if err := unmarshalLoopGoalJSON(string(row.BudgetJson), &budget); err != nil {
		return nil, err
	}
	goal.Budget = budget.LoopBudget()
	if err := unmarshalLoopGoalJSON(string(row.TriggerJson), &goal.Trigger); err != nil {
		return nil, err
	}
	if err := unmarshalLoopGoalJSON(string(row.StopPolicyJson), &goal.StopPolicy); err != nil {
		return nil, err
	}
	if err := unmarshalLoopGoalJSON(string(row.MetadataJson), &goal.Metadata); err != nil {
		return nil, err
	}
	return normalizeLoopGoal(goal), nil
}

type loopBudgetPersisted struct {
	MaxSteps      int   `json:"max_steps,omitempty"`
	MaxActions    int   `json:"max_actions,omitempty"`
	MaxDurationMs int64 `json:"max_duration_ms,omitempty"`
	MaxTokens     int64 `json:"max_tokens,omitempty"`
	MaxCostCents  int64 `json:"max_cost_cents,omitempty"`
	MaxToolCalls  int   `json:"max_tool_calls,omitempty"`
}

func loopBudgetJSON(budget LoopBudget) loopBudgetPersisted {
	return loopBudgetPersisted{
		MaxSteps:      budget.MaxSteps,
		MaxActions:    budget.MaxActions,
		MaxDurationMs: budget.MaxDuration.Milliseconds(),
		MaxTokens:     budget.MaxTokens,
		MaxCostCents:  budget.MaxCostCents,
		MaxToolCalls:  budget.MaxToolCalls,
	}
}

func (b loopBudgetPersisted) LoopBudget() LoopBudget {
	return LoopBudget{
		MaxSteps:     b.MaxSteps,
		MaxActions:   b.MaxActions,
		MaxDuration:  time.Duration(b.MaxDurationMs) * time.Millisecond,
		MaxTokens:    b.MaxTokens,
		MaxCostCents: b.MaxCostCents,
		MaxToolCalls: b.MaxToolCalls,
	}
}

func unmarshalLoopGoalJSON(raw string, dest any) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		raw = "{}"
	}
	return json.Unmarshal([]byte(raw), dest)
}

func normalizeLoopGoalFilter(filter LoopGoalFilter) LoopGoalFilter {
	filter.UserID = strings.TrimSpace(filter.UserID)
	filter.SessionID = strings.TrimSpace(filter.SessionID)
	filter.Status = strings.TrimSpace(filter.Status)
	if filter.Limit <= 0 {
		filter.Limit = 100
	}
	if filter.Limit > 300 {
		filter.Limit = 300
	}
	return filter
}

func loopGoalStartedAt(status string, at time.Time) *time.Time {
	if normalizeLoopGoalStatus(status) != LoopGoalStatusRunning {
		return nil
	}
	t := at.UTC()
	return &t
}

func loopGoalFinishedAt(status string, at time.Time) *time.Time {
	if !isTerminalLoopGoalStatus(status) {
		return nil
	}
	t := at.UTC()
	return &t
}

func isTerminalLoopGoalStatus(status string) bool {
	switch normalizeLoopGoalStatus(status) {
	case LoopGoalStatusSucceeded, LoopGoalStatusFailed, LoopGoalStatusBlocked, LoopGoalStatusBudgetExceeded, LoopGoalStatusCancelled:
		return true
	default:
		return false
	}
}
