package agentruntime

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type SQLWorkflowStore struct {
	db      *sql.DB
	dialect SQLDialect
}

func NewSQLWorkflowStoreWithDialect(db *sql.DB, dialect SQLDialect) *SQLWorkflowStore {
	if dialect == "" {
		dialect = SQLDialectQuestion
	}
	return &SQLWorkflowStore{db: db, dialect: dialect}
}

func (s *SQLWorkflowStore) Init(ctx context.Context) error {
	if err := requireSQLColumns(ctx, s.db, "agent_workflow_runs",
		"id", "user_id", "session_id", "job_id", "name", "version", "status",
		"request_id", "idempotency_key", "state_json", "error", "lease_owner",
		"lease_expires_at", "recoverable", "created_at", "updated_at", "started_at", "finished_at",
	); err != nil {
		return err
	}
	return requireSQLColumns(ctx, s.db, "agent_workflow_steps",
		"id", "run_id", "step_index", "step_name", "idempotency_key", "attempt", "status",
		"input_json", "output_json", "error", "metadata_json", "started_at", "finished_at",
	)
}

func (s *SQLWorkflowStore) CreateWorkflowRun(ctx context.Context, run *WorkflowRun) error {
	if s == nil || run == nil || strings.TrimSpace(run.ID) == "" {
		return fmt.Errorf("workflow run is required")
	}
	stateJSON, err := marshalWorkflowJSON(run.State)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, s.dialect.Bind(`
INSERT INTO agent_workflow_runs (id, user_id, session_id, job_id, request_id, idempotency_key, name, version, status, state_json, error, lease_owner, lease_expires_at, recoverable, created_at, updated_at, started_at, finished_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`),
		run.ID,
		run.UserID,
		run.SessionID,
		run.JobID,
		run.RequestID,
		run.IdempotencyKey,
		run.Name,
		run.Version,
		run.Status,
		stateJSON,
		run.Error,
		run.LeaseOwner,
		nullableSQLTimeValue(run.LeaseExpiresAt, s.dialect),
		run.Recoverable,
		sqlTimeValue(run.CreatedAt, s.dialect),
		sqlTimeValue(run.UpdatedAt, s.dialect),
		nullableSQLTimeValue(run.StartedAt, s.dialect),
		nullableSQLTimeValue(run.FinishedAt, s.dialect),
	)
	return err
}

func (s *SQLWorkflowStore) UpdateWorkflowRun(ctx context.Context, run *WorkflowRun) error {
	if s == nil || run == nil || strings.TrimSpace(run.ID) == "" {
		return fmt.Errorf("workflow run is required")
	}
	stateJSON, err := marshalWorkflowJSON(run.State)
	if err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, s.dialect.Bind(`
UPDATE agent_workflow_runs
SET user_id = ?, session_id = ?, job_id = ?, request_id = ?, idempotency_key = ?, name = ?, version = ?, status = ?, state_json = ?, error = ?, lease_owner = ?, lease_expires_at = ?, recoverable = ?, updated_at = ?, started_at = ?, finished_at = ?
WHERE id = ?`),
		run.UserID,
		run.SessionID,
		run.JobID,
		run.RequestID,
		run.IdempotencyKey,
		run.Name,
		run.Version,
		run.Status,
		stateJSON,
		run.Error,
		run.LeaseOwner,
		nullableSQLTimeValue(run.LeaseExpiresAt, s.dialect),
		run.Recoverable,
		sqlTimeValue(run.UpdatedAt, s.dialect),
		nullableSQLTimeValue(run.StartedAt, s.dialect),
		nullableSQLTimeValue(run.FinishedAt, s.dialect),
		run.ID,
	)
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows == 0 {
		return fmt.Errorf("workflow run not found: %s", run.ID)
	}
	return nil
}

func (s *SQLWorkflowStore) GetWorkflowRun(ctx context.Context, runID string) (*WorkflowRun, error) {
	if s == nil || strings.TrimSpace(runID) == "" {
		return nil, fmt.Errorf("workflow run id is required")
	}
	row := s.db.QueryRowContext(ctx, s.dialect.Bind(`
SELECT id, user_id, session_id, job_id, request_id, idempotency_key, name, version, status, state_json, error, lease_owner, lease_expires_at, recoverable, created_at, updated_at, started_at, finished_at
FROM agent_workflow_runs
WHERE id = ?`), runID)
	return scanSQLWorkflowRun(row)
}

func (s *SQLWorkflowStore) ListWorkflowRuns(ctx context.Context, filter WorkflowRunFilter) ([]*WorkflowRun, error) {
	if s == nil {
		return []*WorkflowRun{}, nil
	}
	filter = normalizeWorkflowRunFilter(filter)
	query := `SELECT id, user_id, session_id, job_id, request_id, idempotency_key, name, version, status, state_json, error, lease_owner, lease_expires_at, recoverable, created_at, updated_at, started_at, finished_at FROM agent_workflow_runs`
	where, args := workflowRunWhere(filter)
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY created_at DESC"
	if filter.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filter.Limit)
	}
	rows, err := s.db.QueryContext(ctx, s.dialect.Bind(query), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*WorkflowRun{}
	for rows.Next() {
		run, err := scanSQLWorkflowRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, run)
	}
	return out, rows.Err()
}

func (s *SQLWorkflowStore) AddWorkflowStepRun(ctx context.Context, step *WorkflowStepRun) error {
	if s == nil || step == nil || strings.TrimSpace(step.ID) == "" || strings.TrimSpace(step.RunID) == "" {
		return fmt.Errorf("workflow step run is required")
	}
	inputJSON, err := marshalWorkflowJSON(step.Input)
	if err != nil {
		return err
	}
	outputJSON, err := marshalWorkflowJSON(step.Output)
	if err != nil {
		return err
	}
	metadataJSON, err := marshalWorkflowJSON(step.Metadata)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, s.dialect.Bind(`
INSERT INTO agent_workflow_steps (id, run_id, step_index, step_name, idempotency_key, attempt, status, input_json, output_json, error, metadata_json, started_at, finished_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`),
		step.ID,
		step.RunID,
		step.StepIndex,
		step.StepName,
		step.IdempotencyKey,
		step.Attempt,
		step.Status,
		inputJSON,
		outputJSON,
		step.Error,
		metadataJSON,
		sqlTimeValue(step.StartedAt, s.dialect),
		nullableSQLTimeValue(step.FinishedAt, s.dialect),
	)
	return err
}

func (s *SQLWorkflowStore) UpdateWorkflowStepRun(ctx context.Context, step *WorkflowStepRun) error {
	if s == nil || step == nil || strings.TrimSpace(step.ID) == "" {
		return fmt.Errorf("workflow step run is required")
	}
	inputJSON, err := marshalWorkflowJSON(step.Input)
	if err != nil {
		return err
	}
	outputJSON, err := marshalWorkflowJSON(step.Output)
	if err != nil {
		return err
	}
	metadataJSON, err := marshalWorkflowJSON(step.Metadata)
	if err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, s.dialect.Bind(`
UPDATE agent_workflow_steps
SET run_id = ?, step_index = ?, step_name = ?, idempotency_key = ?, attempt = ?, status = ?, input_json = ?, output_json = ?, error = ?, metadata_json = ?, started_at = ?, finished_at = ?
WHERE id = ?`),
		step.RunID,
		step.StepIndex,
		step.StepName,
		step.IdempotencyKey,
		step.Attempt,
		step.Status,
		inputJSON,
		outputJSON,
		step.Error,
		metadataJSON,
		sqlTimeValue(step.StartedAt, s.dialect),
		nullableSQLTimeValue(step.FinishedAt, s.dialect),
		step.ID,
	)
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows == 0 {
		return fmt.Errorf("workflow step run not found: %s", step.ID)
	}
	return nil
}

func (s *SQLWorkflowStore) ListWorkflowStepRuns(ctx context.Context, runID string) ([]*WorkflowStepRun, error) {
	if s == nil || strings.TrimSpace(runID) == "" {
		return []*WorkflowStepRun{}, nil
	}
	rows, err := s.db.QueryContext(ctx, s.dialect.Bind(`
SELECT id, run_id, step_index, step_name, idempotency_key, attempt, status, input_json, output_json, error, metadata_json, started_at, finished_at
FROM agent_workflow_steps
WHERE run_id = ?
ORDER BY step_index ASC, started_at ASC, id ASC`), runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*WorkflowStepRun{}
	for rows.Next() {
		step, err := scanSQLWorkflowStepRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, step)
	}
	return out, rows.Err()
}

func (s *SQLWorkflowStore) FindWorkflowRunByIdempotencyKey(ctx context.Context, userID, name, idempotencyKey string) (*WorkflowRun, error) {
	if s == nil || strings.TrimSpace(idempotencyKey) == "" {
		return nil, nil
	}
	row := s.db.QueryRowContext(ctx, s.dialect.Bind(`
SELECT id, user_id, session_id, job_id, request_id, idempotency_key, name, version, status, state_json, error, lease_owner, lease_expires_at, recoverable, created_at, updated_at, started_at, finished_at
FROM agent_workflow_runs
WHERE user_id = ? AND name = ? AND idempotency_key = ?
ORDER BY created_at DESC
LIMIT 1`), strings.TrimSpace(userID), strings.TrimSpace(name), strings.TrimSpace(idempotencyKey))
	run, err := scanSQLWorkflowRun(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return run, err
}

func (s *SQLWorkflowStore) GetWorkflowStepByIndex(ctx context.Context, runID string, stepIndex int) (*WorkflowStepRun, error) {
	if s == nil || strings.TrimSpace(runID) == "" {
		return nil, nil
	}
	row := s.db.QueryRowContext(ctx, s.dialect.Bind(`
SELECT id, run_id, step_index, step_name, idempotency_key, attempt, status, input_json, output_json, error, metadata_json, started_at, finished_at
FROM agent_workflow_steps
WHERE run_id = ? AND step_index = ?
ORDER BY started_at DESC, id DESC
LIMIT 1`), strings.TrimSpace(runID), stepIndex)
	step, err := scanSQLWorkflowStepRun(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return step, err
}

func workflowRunWhere(filter WorkflowRunFilter) ([]string, []any) {
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
	if filter.Name != "" {
		where = append(where, "name = ?")
		args = append(args, filter.Name)
	}
	if filter.Status != "" {
		where = append(where, "status = ?")
		args = append(args, filter.Status)
	}
	return where, args
}

func scanSQLWorkflowRun(row workflowScanner) (*WorkflowRun, error) {
	var run WorkflowRun
	var stateJSON []byte
	var createdAt, updatedAt, startedAt, finishedAt, leaseExpiresAt any
	if err := row.Scan(
		&run.ID,
		&run.UserID,
		&run.SessionID,
		&run.JobID,
		&run.RequestID,
		&run.IdempotencyKey,
		&run.Name,
		&run.Version,
		&run.Status,
		&stateJSON,
		&run.Error,
		&run.LeaseOwner,
		&leaseExpiresAt,
		&run.Recoverable,
		&createdAt,
		&updatedAt,
		&startedAt,
		&finishedAt,
	); err != nil {
		return nil, err
	}
	if err := unmarshalWorkflowJSON(stateJSON, &run.State); err != nil {
		return nil, err
	}
	var err error
	if run.CreatedAt, err = parseSQLTime(createdAt); err != nil {
		return nil, err
	}
	if run.UpdatedAt, err = parseSQLTime(updatedAt); err != nil {
		return nil, err
	}
	if run.LeaseExpiresAt, err = parseNullableSQLTime(leaseExpiresAt); err != nil {
		return nil, err
	}
	if run.StartedAt, err = parseNullableSQLTime(startedAt); err != nil {
		return nil, err
	}
	if run.FinishedAt, err = parseNullableSQLTime(finishedAt); err != nil {
		return nil, err
	}
	return cloneWorkflowRun(&run), nil
}

func scanSQLWorkflowStepRun(row workflowScanner) (*WorkflowStepRun, error) {
	var step WorkflowStepRun
	var inputJSON, outputJSON, metadataJSON []byte
	var startedAt, finishedAt any
	if err := row.Scan(
		&step.ID,
		&step.RunID,
		&step.StepIndex,
		&step.StepName,
		&step.IdempotencyKey,
		&step.Attempt,
		&step.Status,
		&inputJSON,
		&outputJSON,
		&step.Error,
		&metadataJSON,
		&startedAt,
		&finishedAt,
	); err != nil {
		return nil, err
	}
	if err := unmarshalWorkflowJSON(inputJSON, &step.Input); err != nil {
		return nil, err
	}
	if err := unmarshalWorkflowJSON(outputJSON, &step.Output); err != nil {
		return nil, err
	}
	if err := unmarshalWorkflowJSON(metadataJSON, &step.Metadata); err != nil {
		return nil, err
	}
	var err error
	if step.StartedAt, err = parseSQLTime(startedAt); err != nil {
		return nil, err
	}
	if step.FinishedAt, err = parseNullableSQLTime(finishedAt); err != nil {
		return nil, err
	}
	return cloneWorkflowStepRun(&step), nil
}

type workflowScanner interface {
	Scan(dest ...any) error
}

func marshalWorkflowJSON(value map[string]any) (string, error) {
	if value == nil {
		value = map[string]any{}
	}
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func unmarshalWorkflowJSON(data []byte, out *map[string]any) error {
	if len(data) == 0 {
		*out = map[string]any{}
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return err
	}
	if *out == nil {
		*out = map[string]any{}
	}
	return nil
}
