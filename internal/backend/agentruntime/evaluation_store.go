package agentruntime

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"claude-codex/internal/backend/agentruntime/dbsqlc"
)

type EvaluationStore interface {
	Init(ctx context.Context) error
	CreateEvaluationRun(ctx context.Context, run EvaluationRun) (EvaluationRun, error)
	UpdateEvaluationRun(ctx context.Context, run EvaluationRun) (EvaluationRun, error)
	GetEvaluationRun(ctx context.Context, id string) (EvaluationRun, error)
	ListEvaluationRuns(ctx context.Context, filter EvaluationRunFilter) ([]EvaluationRun, error)
	CreateEvaluationResult(ctx context.Context, result EvaluationResult) (EvaluationResult, error)
	ListEvaluationResults(ctx context.Context, filter EvaluationResultFilter) ([]EvaluationResult, error)
	CreateEvaluationReview(ctx context.Context, review EvaluationReview) (EvaluationReview, error)
	UpdateEvaluationReview(ctx context.Context, review EvaluationReview) (EvaluationReview, error)
	ListEvaluationReviews(ctx context.Context, filter EvaluationReviewFilter) ([]EvaluationReview, error)
	SummarizeEvaluationRun(ctx context.Context, runID string) (EvaluationRunSummary, error)
	UpsertGoldenSet(ctx context.Context, set GoldenSet) (GoldenSet, error)
	GetGoldenSet(ctx context.Context, id string) (GoldenSet, error)
	GetGoldenSetVersion(ctx context.Context, id, version string) (GoldenSet, error)
	ListGoldenSets(ctx context.Context, filter GoldenSetFilter) ([]GoldenSet, error)
	DeleteGoldenSet(ctx context.Context, id string) error
}

type MemoryEvaluationStore struct {
	mu         sync.Mutex
	runs       map[string]EvaluationRun
	results    map[string]EvaluationResult
	reviews    map[string]EvaluationReview
	goldenSets map[string]GoldenSet
}

func NewMemoryEvaluationStore() *MemoryEvaluationStore {
	return &MemoryEvaluationStore{
		runs:       map[string]EvaluationRun{},
		results:    map[string]EvaluationResult{},
		reviews:    map[string]EvaluationReview{},
		goldenSets: map[string]GoldenSet{},
	}
}

func (s *MemoryEvaluationStore) Init(context.Context) error { return nil }

func (s *MemoryEvaluationStore) CreateEvaluationRun(_ context.Context, run EvaluationRun) (EvaluationRun, error) {
	run = normalizeEvaluationRun(run)
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.runs[run.ID]; ok {
		return EvaluationRun{}, fmt.Errorf("evaluation run already exists")
	}
	s.runs[run.ID] = cloneEvaluationRun(run)
	return cloneEvaluationRun(run), nil
}

func (s *MemoryEvaluationStore) UpdateEvaluationRun(_ context.Context, run EvaluationRun) (EvaluationRun, error) {
	run = normalizeEvaluationRun(run)
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.runs[run.ID]; !ok {
		return EvaluationRun{}, sql.ErrNoRows
	}
	s.runs[run.ID] = cloneEvaluationRun(run)
	return cloneEvaluationRun(run), nil
}

func (s *MemoryEvaluationStore) GetEvaluationRun(_ context.Context, id string) (EvaluationRun, error) {
	id = strings.TrimSpace(id)
	s.mu.Lock()
	defer s.mu.Unlock()
	run, ok := s.runs[id]
	if !ok {
		return EvaluationRun{}, sql.ErrNoRows
	}
	return cloneEvaluationRun(run), nil
}

func (s *MemoryEvaluationStore) ListEvaluationRuns(_ context.Context, filter EvaluationRunFilter) ([]EvaluationRun, error) {
	filter = normalizeEvaluationRunFilter(filter)
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]EvaluationRun, 0, len(s.runs))
	for _, run := range s.runs {
		if !evaluationRunMatches(run, filter) {
			continue
		}
		out = append(out, cloneEvaluationRun(run))
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].StartedAt.After(out[j].StartedAt)
	})
	if filter.Limit > 0 && len(out) > filter.Limit {
		out = out[:filter.Limit]
	}
	return out, nil
}

func (s *MemoryEvaluationStore) CreateEvaluationResult(_ context.Context, result EvaluationResult) (EvaluationResult, error) {
	result = normalizeEvaluationResult(result)
	if result.RunID == "" {
		return EvaluationResult{}, fmt.Errorf("evaluation run id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.runs[result.RunID]; !ok {
		return EvaluationResult{}, sql.ErrNoRows
	}
	if _, ok := s.results[result.ID]; ok {
		return EvaluationResult{}, fmt.Errorf("evaluation result already exists")
	}
	s.results[result.ID] = cloneEvaluationResult(result)
	return cloneEvaluationResult(result), nil
}

func (s *MemoryEvaluationStore) ListEvaluationResults(_ context.Context, filter EvaluationResultFilter) ([]EvaluationResult, error) {
	filter = normalizeEvaluationResultFilter(filter)
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]EvaluationResult, 0, len(s.results))
	for _, result := range s.results {
		if !evaluationResultMatches(result, filter) {
			continue
		}
		out = append(out, cloneEvaluationResult(result))
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	if filter.Limit > 0 && len(out) > filter.Limit {
		out = out[:filter.Limit]
	}
	return out, nil
}

func (s *MemoryEvaluationStore) CreateEvaluationReview(_ context.Context, review EvaluationReview) (EvaluationReview, error) {
	review = normalizeEvaluationReview(review)
	if review.ResultID == "" {
		return EvaluationReview{}, fmt.Errorf("evaluation result id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.results[review.ResultID]; !ok {
		return EvaluationReview{}, sql.ErrNoRows
	}
	if _, ok := s.reviews[review.ID]; ok {
		return EvaluationReview{}, fmt.Errorf("evaluation review already exists")
	}
	s.reviews[review.ID] = cloneEvaluationReview(review)
	return cloneEvaluationReview(review), nil
}

func (s *MemoryEvaluationStore) UpdateEvaluationReview(_ context.Context, review EvaluationReview) (EvaluationReview, error) {
	review = normalizeEvaluationReview(review)
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.reviews[review.ID]
	if !ok {
		return EvaluationReview{}, sql.ErrNoRows
	}
	if review.ResultID == "" {
		review.ResultID = existing.ResultID
	}
	if review.CreatedAt.IsZero() {
		review.CreatedAt = existing.CreatedAt
	}
	s.reviews[review.ID] = cloneEvaluationReview(review)
	return cloneEvaluationReview(review), nil
}

func (s *MemoryEvaluationStore) ListEvaluationReviews(_ context.Context, filter EvaluationReviewFilter) ([]EvaluationReview, error) {
	filter = normalizeEvaluationReviewFilter(filter)
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]EvaluationReview, 0, len(s.reviews))
	for _, review := range s.reviews {
		if !evaluationReviewMatches(review, filter) {
			continue
		}
		out = append(out, cloneEvaluationReview(review))
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	if filter.Limit > 0 && len(out) > filter.Limit {
		out = out[:filter.Limit]
	}
	return out, nil
}

func (s *MemoryEvaluationStore) SummarizeEvaluationRun(ctx context.Context, runID string) (EvaluationRunSummary, error) {
	run, err := s.GetEvaluationRun(ctx, runID)
	if err != nil {
		return EvaluationRunSummary{}, err
	}
	results, err := s.ListEvaluationResults(ctx, EvaluationResultFilter{RunID: run.ID})
	if err != nil {
		return EvaluationRunSummary{}, err
	}
	return summarizeEvaluationResults(run, results), nil
}

type SQLEvaluationStore struct {
	db      *sql.DB
	dialect SQLDialect
	queries *dbsqlc.Queries
}

func NewSQLEvaluationStore(db *sql.DB) *SQLEvaluationStore {
	return NewSQLEvaluationStoreWithDialect(db, SQLDialectQuestion)
}

func NewSQLEvaluationStoreWithDialect(db *sql.DB, dialect SQLDialect) *SQLEvaluationStore {
	if dialect == "" {
		dialect = SQLDialectQuestion
	}
	return &SQLEvaluationStore{db: db, dialect: dialect, queries: dbsqlc.New(db)}
}

func (s *SQLEvaluationStore) Init(ctx context.Context) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("sql evaluation store is not configured")
	}
	if err := requireSQLColumns(ctx, s.db, "agent_eval_runs",
		"id", "name", "status", "trigger", "scope", "started_at", "completed_at",
		"total", "passed", "failed", "warning", "metrics", "threshold_status", "summary",
	); err != nil {
		return err
	}
	if err := requireSQLColumns(ctx, s.db, "agent_eval_results",
		"id", "run_id", "subject_type", "subject_id", "user_id", "session_id", "job_id",
		"skill_name", "provider", "model", "prompt_id", "prompt_version", "prompt_hash", "experiment_id", "variant_id", "status", "score", "input", "output",
		"metrics", "findings", "created_at",
	); err != nil {
		return err
	}
	if err := requireSQLColumns(ctx, s.db, "agent_eval_reviews",
		"id", "result_id", "status", "reviewer", "note", "created_at", "updated_at",
	); err != nil {
		return err
	}
	if err := requireSQLColumns(ctx, s.db, "agent_eval_golden_sets",
		"id", "name", "description", "version", "metadata", "created_at", "updated_at",
	); err != nil {
		return err
	}
	return requireSQLColumns(ctx, s.db, "agent_eval_golden_cases",
		"id", "set_id", "set_version", "position", "query", "expected_answer", "expected_facts", "gold_evidence", "tags", "metadata", "created_at", "updated_at",
	)
}

func (s *SQLEvaluationStore) CreateEvaluationRun(ctx context.Context, run EvaluationRun) (EvaluationRun, error) {
	run = normalizeEvaluationRun(run)
	scope, metrics, err := marshalEvaluationRunJSON(run)
	if err != nil {
		return EvaluationRun{}, err
	}
	if s.dialect == SQLDialectPostgres && s.queries != nil {
		err = s.queries.InsertEvaluationRun(ctx, dbsqlc.InsertEvaluationRunParams{
			ID:              run.ID,
			Name:            run.Name,
			Status:          run.Status,
			Trigger:         run.Trigger,
			Scope:           string(scope),
			StartedAt:       run.StartedAt.UTC(),
			CompletedAt:     sqlNullTime(run.CompletedAt),
			Total:           int64(run.Total),
			Passed:          int64(run.Passed),
			Failed:          int64(run.Failed),
			Warning:         int64(run.Warning),
			Metrics:         string(metrics),
			ThresholdStatus: run.ThresholdStatus,
			Summary:         run.Summary,
		})
		if err != nil {
			return EvaluationRun{}, err
		}
		return run, nil
	}
	_, err = s.db.ExecContext(ctx, s.dialect.Bind(`
INSERT INTO agent_eval_runs (id, name, status, trigger, scope, started_at, completed_at, total, passed, failed, warning, metrics, threshold_status, summary)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`),
		run.ID, run.Name, run.Status, run.Trigger, string(scope), sqlTimeValue(run.StartedAt, s.dialect), nullableSQLTimeValue(run.CompletedAt, s.dialect),
		run.Total, run.Passed, run.Failed, run.Warning, string(metrics), run.ThresholdStatus, run.Summary)
	if err != nil {
		return EvaluationRun{}, err
	}
	return run, nil
}

func (s *SQLEvaluationStore) UpdateEvaluationRun(ctx context.Context, run EvaluationRun) (EvaluationRun, error) {
	run = normalizeEvaluationRun(run)
	scope, metrics, err := marshalEvaluationRunJSON(run)
	if err != nil {
		return EvaluationRun{}, err
	}
	if s.dialect == SQLDialectPostgres && s.queries != nil {
		count, err := s.queries.UpdateEvaluationRun(ctx, dbsqlc.UpdateEvaluationRunParams{
			Name:            run.Name,
			Status:          run.Status,
			Trigger:         run.Trigger,
			Scope:           string(scope),
			StartedAt:       run.StartedAt.UTC(),
			CompletedAt:     sqlNullTime(run.CompletedAt),
			Total:           int64(run.Total),
			Passed:          int64(run.Passed),
			Failed:          int64(run.Failed),
			Warning:         int64(run.Warning),
			Metrics:         string(metrics),
			ThresholdStatus: run.ThresholdStatus,
			Summary:         run.Summary,
			ID:              run.ID,
		})
		if err != nil {
			return EvaluationRun{}, err
		}
		if count == 0 {
			return EvaluationRun{}, sql.ErrNoRows
		}
		return run, nil
	}
	result, err := s.db.ExecContext(ctx, s.dialect.Bind(`
UPDATE agent_eval_runs
SET name = ?, status = ?, trigger = ?, scope = ?, started_at = ?, completed_at = ?, total = ?, passed = ?, failed = ?, warning = ?, metrics = ?, threshold_status = ?, summary = ?
WHERE id = ?`),
		run.Name, run.Status, run.Trigger, string(scope), sqlTimeValue(run.StartedAt, s.dialect), nullableSQLTimeValue(run.CompletedAt, s.dialect),
		run.Total, run.Passed, run.Failed, run.Warning, string(metrics), run.ThresholdStatus, run.Summary, run.ID)
	if err != nil {
		return EvaluationRun{}, err
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return EvaluationRun{}, sql.ErrNoRows
	}
	return run, nil
}

func (s *SQLEvaluationStore) GetEvaluationRun(ctx context.Context, id string) (EvaluationRun, error) {
	if s.dialect == SQLDialectPostgres && s.queries != nil {
		row, err := s.queries.GetEvaluationRun(ctx, strings.TrimSpace(id))
		if err != nil {
			return EvaluationRun{}, err
		}
		return evaluationRunFromSQLC(row), nil
	}
	return scanEvaluationRun(s.db.QueryRowContext(ctx, s.dialect.Bind(`
SELECT id, name, status, trigger, scope, started_at, completed_at, total, passed, failed, warning, metrics, threshold_status, summary
FROM agent_eval_runs WHERE id = ?`), strings.TrimSpace(id)))
}

func (s *SQLEvaluationStore) ListEvaluationRuns(ctx context.Context, filter EvaluationRunFilter) ([]EvaluationRun, error) {
	filter = normalizeEvaluationRunFilter(filter)
	if s.dialect == SQLDialectPostgres && s.queries != nil {
		rows, err := s.queries.ListEvaluationRuns(ctx, dbsqlc.ListEvaluationRunsParams{
			Status:     filter.Status,
			Trigger:    filter.Trigger,
			LimitCount: int32(filter.Limit),
		})
		if err != nil {
			return nil, err
		}
		return evaluationRunsFromSQLC(rows), nil
	}
	query := `SELECT id, name, status, trigger, scope, started_at, completed_at, total, passed, failed, warning, metrics, threshold_status, summary FROM agent_eval_runs`
	where, args := evaluationRunWhere(filter)
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY started_at DESC"
	if filter.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filter.Limit)
	}
	rows, err := s.db.QueryContext(ctx, s.dialect.Bind(query), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []EvaluationRun{}
	for rows.Next() {
		run, err := scanEvaluationRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, run)
	}
	return out, rows.Err()
}

func (s *SQLEvaluationStore) CreateEvaluationResult(ctx context.Context, result EvaluationResult) (EvaluationResult, error) {
	result = normalizeEvaluationResult(result)
	metrics, findings, err := marshalEvaluationResultJSON(result)
	if err != nil {
		return EvaluationResult{}, err
	}
	_, err = s.db.ExecContext(ctx, s.dialect.Bind(`
INSERT INTO agent_eval_results (id, run_id, subject_type, subject_id, user_id, session_id, job_id, skill_name, provider, model, prompt_id, prompt_version, prompt_hash, experiment_id, variant_id, status, score, input, output, metrics, findings, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`),
		result.ID, result.RunID, result.SubjectType, result.SubjectID, result.UserID, result.SessionID, result.JobID, result.SkillName, result.Provider, result.Model,
		result.PromptID, result.PromptVersion, result.PromptHash, result.ExperimentID, result.VariantID, result.Status, result.Score, result.Input, result.Output, string(metrics), string(findings), sqlTimeValue(result.CreatedAt, s.dialect))
	if err != nil {
		return EvaluationResult{}, err
	}
	return result, nil
}

func (s *SQLEvaluationStore) ListEvaluationResults(ctx context.Context, filter EvaluationResultFilter) ([]EvaluationResult, error) {
	filter = normalizeEvaluationResultFilter(filter)
	query := `SELECT id, run_id, subject_type, subject_id, user_id, session_id, job_id, skill_name, provider, model, prompt_id, prompt_version, prompt_hash, experiment_id, variant_id, status, score, input, output, metrics, findings, created_at FROM agent_eval_results`
	where, args := evaluationResultWhere(filter)
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
	out := []EvaluationResult{}
	for rows.Next() {
		result, err := scanEvaluationResult(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, result)
	}
	return out, rows.Err()
}

func (s *SQLEvaluationStore) CreateEvaluationReview(ctx context.Context, review EvaluationReview) (EvaluationReview, error) {
	review = normalizeEvaluationReview(review)
	if s.dialect == SQLDialectPostgres && s.queries != nil {
		err := s.queries.InsertEvaluationReview(ctx, dbsqlc.InsertEvaluationReviewParams{
			ID:        review.ID,
			ResultID:  review.ResultID,
			Status:    review.Status,
			Reviewer:  review.Reviewer,
			Note:      review.Note,
			CreatedAt: review.CreatedAt.UTC(),
			UpdatedAt: review.UpdatedAt.UTC(),
		})
		if err != nil {
			return EvaluationReview{}, err
		}
		return review, nil
	}
	_, err := s.db.ExecContext(ctx, s.dialect.Bind(`
INSERT INTO agent_eval_reviews (id, result_id, status, reviewer, note, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?)`),
		review.ID, review.ResultID, review.Status, review.Reviewer, review.Note, sqlTimeValue(review.CreatedAt, s.dialect), sqlTimeValue(review.UpdatedAt, s.dialect))
	if err != nil {
		return EvaluationReview{}, err
	}
	return review, nil
}

func (s *SQLEvaluationStore) UpdateEvaluationReview(ctx context.Context, review EvaluationReview) (EvaluationReview, error) {
	review = normalizeEvaluationReview(review)
	if s.dialect == SQLDialectPostgres && s.queries != nil {
		count, err := s.queries.UpdateEvaluationReview(ctx, dbsqlc.UpdateEvaluationReviewParams{
			Status:    review.Status,
			Reviewer:  review.Reviewer,
			Note:      review.Note,
			UpdatedAt: review.UpdatedAt.UTC(),
			ID:        review.ID,
		})
		if err != nil {
			return EvaluationReview{}, err
		}
		if count == 0 {
			return EvaluationReview{}, sql.ErrNoRows
		}
		return s.getEvaluationReview(ctx, review.ID)
	}
	result, err := s.db.ExecContext(ctx, s.dialect.Bind(`
UPDATE agent_eval_reviews
SET status = ?, reviewer = ?, note = ?, updated_at = ?
WHERE id = ?`), review.Status, review.Reviewer, review.Note, sqlTimeValue(review.UpdatedAt, s.dialect), review.ID)
	if err != nil {
		return EvaluationReview{}, err
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return EvaluationReview{}, sql.ErrNoRows
	}
	return s.getEvaluationReview(ctx, review.ID)
}

func (s *SQLEvaluationStore) getEvaluationReview(ctx context.Context, id string) (EvaluationReview, error) {
	if s.dialect == SQLDialectPostgres && s.queries != nil {
		row, err := s.queries.GetEvaluationReview(ctx, strings.TrimSpace(id))
		if err != nil {
			return EvaluationReview{}, err
		}
		return evaluationReviewFromSQLC(row), nil
	}
	return scanEvaluationReview(s.db.QueryRowContext(ctx, s.dialect.Bind(`
SELECT id, result_id, status, reviewer, note, created_at, updated_at
FROM agent_eval_reviews WHERE id = ?`), strings.TrimSpace(id)))
}

func (s *SQLEvaluationStore) ListEvaluationReviews(ctx context.Context, filter EvaluationReviewFilter) ([]EvaluationReview, error) {
	filter = normalizeEvaluationReviewFilter(filter)
	if s.dialect == SQLDialectPostgres && s.queries != nil {
		rows, err := s.queries.ListEvaluationReviews(ctx, dbsqlc.ListEvaluationReviewsParams{
			ResultID:   filter.ResultID,
			Status:     filter.Status,
			LimitCount: int32(filter.Limit),
		})
		if err != nil {
			return nil, err
		}
		return evaluationReviewsFromSQLC(rows), nil
	}
	query := `SELECT id, result_id, status, reviewer, note, created_at, updated_at FROM agent_eval_reviews`
	where, args := evaluationReviewWhere(filter)
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY updated_at DESC"
	if filter.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filter.Limit)
	}
	rows, err := s.db.QueryContext(ctx, s.dialect.Bind(query), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []EvaluationReview{}
	for rows.Next() {
		review, err := scanEvaluationReview(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, review)
	}
	return out, rows.Err()
}

func (s *SQLEvaluationStore) SummarizeEvaluationRun(ctx context.Context, runID string) (EvaluationRunSummary, error) {
	run, err := s.GetEvaluationRun(ctx, runID)
	if err != nil {
		return EvaluationRunSummary{}, err
	}
	results, err := s.ListEvaluationResults(ctx, EvaluationResultFilter{RunID: run.ID})
	if err != nil {
		return EvaluationRunSummary{}, err
	}
	return summarizeEvaluationResults(run, results), nil
}

type evaluationScanner interface {
	Scan(dest ...any) error
}

func scanEvaluationRun(row evaluationScanner) (EvaluationRun, error) {
	var run EvaluationRun
	var scope, metrics string
	var startedAt, completedAt any
	if err := row.Scan(&run.ID, &run.Name, &run.Status, &run.Trigger, &scope, &startedAt, &completedAt, &run.Total, &run.Passed, &run.Failed, &run.Warning, &metrics, &run.ThresholdStatus, &run.Summary); err != nil {
		return EvaluationRun{}, err
	}
	_ = json.Unmarshal([]byte(scope), &run.Scope)
	_ = json.Unmarshal([]byte(metrics), &run.Metrics)
	var err error
	if run.StartedAt, err = parseSQLTime(startedAt); err != nil {
		return EvaluationRun{}, err
	}
	if run.CompletedAt, err = parseNullableSQLTime(completedAt); err != nil {
		return EvaluationRun{}, err
	}
	return normalizeEvaluationRun(run), nil
}

func scanEvaluationResult(row evaluationScanner) (EvaluationResult, error) {
	var result EvaluationResult
	var metrics, findings string
	var createdAt any
	if err := row.Scan(&result.ID, &result.RunID, &result.SubjectType, &result.SubjectID, &result.UserID, &result.SessionID, &result.JobID, &result.SkillName, &result.Provider, &result.Model, &result.PromptID, &result.PromptVersion, &result.PromptHash, &result.ExperimentID, &result.VariantID, &result.Status, &result.Score, &result.Input, &result.Output, &metrics, &findings, &createdAt); err != nil {
		return EvaluationResult{}, err
	}
	_ = json.Unmarshal([]byte(metrics), &result.Metrics)
	_ = json.Unmarshal([]byte(findings), &result.Findings)
	var err error
	if result.CreatedAt, err = parseSQLTime(createdAt); err != nil {
		return EvaluationResult{}, err
	}
	return normalizeEvaluationResult(result), nil
}

func scanEvaluationReview(row evaluationScanner) (EvaluationReview, error) {
	var review EvaluationReview
	var createdAt, updatedAt any
	if err := row.Scan(&review.ID, &review.ResultID, &review.Status, &review.Reviewer, &review.Note, &createdAt, &updatedAt); err != nil {
		return EvaluationReview{}, err
	}
	var err error
	if review.CreatedAt, err = parseSQLTime(createdAt); err != nil {
		return EvaluationReview{}, err
	}
	if review.UpdatedAt, err = parseSQLTime(updatedAt); err != nil {
		return EvaluationReview{}, err
	}
	return normalizeEvaluationReview(review), nil
}

func evaluationRunFromSQLC(row dbsqlc.AgentEvalRun) EvaluationRun {
	run := EvaluationRun{
		ID:              row.ID,
		Name:            row.Name,
		Status:          row.Status,
		Trigger:         row.Trigger,
		StartedAt:       row.StartedAt.UTC(),
		CompletedAt:     timeFromNull(row.CompletedAt),
		Total:           int(row.Total),
		Passed:          int(row.Passed),
		Failed:          int(row.Failed),
		Warning:         int(row.Warning),
		ThresholdStatus: row.ThresholdStatus,
		Summary:         row.Summary,
	}
	_ = json.Unmarshal([]byte(row.Scope), &run.Scope)
	_ = json.Unmarshal([]byte(row.Metrics), &run.Metrics)
	return normalizeEvaluationRun(run)
}

func evaluationRunsFromSQLC(rows []dbsqlc.AgentEvalRun) []EvaluationRun {
	out := make([]EvaluationRun, 0, len(rows))
	for _, row := range rows {
		out = append(out, evaluationRunFromSQLC(row))
	}
	return out
}

func evaluationResultFromSQLC(row dbsqlc.AgentEvalResult) EvaluationResult {
	result := EvaluationResult{
		ID:          row.ID,
		RunID:       row.RunID,
		SubjectType: row.SubjectType,
		SubjectID:   row.SubjectID,
		UserID:      row.UserID,
		SessionID:   row.SessionID,
		JobID:       row.JobID,
		SkillName:   row.SkillName,
		Provider:    row.Provider,
		Model:       row.Model,
		Status:      row.Status,
		Score:       row.Score,
		Input:       row.Input,
		Output:      row.Output,
		CreatedAt:   row.CreatedAt.UTC(),
	}
	_ = json.Unmarshal([]byte(row.Metrics), &result.Metrics)
	_ = json.Unmarshal([]byte(row.Findings), &result.Findings)
	return normalizeEvaluationResult(result)
}

func evaluationResultsFromSQLC(rows []dbsqlc.AgentEvalResult) []EvaluationResult {
	out := make([]EvaluationResult, 0, len(rows))
	for _, row := range rows {
		out = append(out, evaluationResultFromSQLC(row))
	}
	return out
}

func evaluationReviewFromSQLC(row dbsqlc.AgentEvalReview) EvaluationReview {
	return normalizeEvaluationReview(EvaluationReview{
		ID:        row.ID,
		ResultID:  row.ResultID,
		Status:    row.Status,
		Reviewer:  row.Reviewer,
		Note:      row.Note,
		CreatedAt: row.CreatedAt.UTC(),
		UpdatedAt: row.UpdatedAt.UTC(),
	})
}

func evaluationReviewsFromSQLC(rows []dbsqlc.AgentEvalReview) []EvaluationReview {
	out := make([]EvaluationReview, 0, len(rows))
	for _, row := range rows {
		out = append(out, evaluationReviewFromSQLC(row))
	}
	return out
}

func marshalEvaluationRunJSON(run EvaluationRun) ([]byte, []byte, error) {
	scope, err := json.Marshal(run.Scope)
	if err != nil {
		return nil, nil, err
	}
	metrics, err := json.Marshal(run.Metrics)
	if err != nil {
		return nil, nil, err
	}
	return scope, metrics, nil
}

func marshalEvaluationResultJSON(result EvaluationResult) ([]byte, []byte, error) {
	metrics, err := json.Marshal(result.Metrics)
	if err != nil {
		return nil, nil, err
	}
	findings, err := json.Marshal(result.Findings)
	if err != nil {
		return nil, nil, err
	}
	return metrics, findings, nil
}

func evaluationRunWhere(filter EvaluationRunFilter) ([]string, []any) {
	var where []string
	var args []any
	if filter.Status != "" {
		where = append(where, "status = ?")
		args = append(args, filter.Status)
	}
	if filter.Trigger != "" {
		where = append(where, "trigger = ?")
		args = append(args, filter.Trigger)
	}
	return where, args
}

func evaluationResultWhere(filter EvaluationResultFilter) ([]string, []any) {
	var where []string
	var args []any
	if filter.RunID != "" {
		where = append(where, "run_id = ?")
		args = append(args, filter.RunID)
	}
	if filter.Status != "" {
		where = append(where, "status = ?")
		args = append(args, filter.Status)
	}
	if filter.SubjectType != "" {
		where = append(where, "subject_type = ?")
		args = append(args, filter.SubjectType)
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
	if filter.SkillName != "" {
		where = append(where, "skill_name = ?")
		args = append(args, filter.SkillName)
	}
	if filter.Provider != "" {
		where = append(where, "provider = ?")
		args = append(args, filter.Provider)
	}
	if filter.Model != "" {
		where = append(where, "model = ?")
		args = append(args, filter.Model)
	}
	if filter.PromptID != "" {
		where = append(where, "prompt_id = ?")
		args = append(args, filter.PromptID)
	}
	if filter.PromptVersion != "" {
		where = append(where, "prompt_version = ?")
		args = append(args, filter.PromptVersion)
	}
	if filter.PromptHash != "" {
		where = append(where, "prompt_hash = ?")
		args = append(args, filter.PromptHash)
	}
	if filter.ExperimentID != "" {
		where = append(where, "experiment_id = ?")
		args = append(args, filter.ExperimentID)
	}
	if filter.VariantID != "" {
		where = append(where, "variant_id = ?")
		args = append(args, filter.VariantID)
	}
	return where, args
}

func evaluationReviewWhere(filter EvaluationReviewFilter) ([]string, []any) {
	var where []string
	var args []any
	if filter.ResultID != "" {
		where = append(where, "result_id = ?")
		args = append(args, filter.ResultID)
	}
	if filter.Status != "" {
		where = append(where, "status = ?")
		args = append(args, filter.Status)
	}
	return where, args
}

func normalizeEvaluationRun(run EvaluationRun) EvaluationRun {
	run.ID = strings.TrimSpace(run.ID)
	if run.ID == "" {
		run.ID = newEvaluationID("evalrun")
	}
	run.Name = truncateEvaluationString(strings.TrimSpace(run.Name), 256)
	if run.Name == "" {
		run.Name = run.ID
	}
	run.Status = normalizeEvaluationRunStatus(run.Status)
	run.Trigger = truncateEvaluationString(strings.TrimSpace(run.Trigger), 128)
	run.Scope = normalizeEvaluationScope(run.Scope)
	if run.StartedAt.IsZero() {
		run.StartedAt = time.Now().UTC()
	} else {
		run.StartedAt = run.StartedAt.UTC()
	}
	if run.CompletedAt != nil {
		completedAt := run.CompletedAt.UTC()
		run.CompletedAt = &completedAt
	}
	if run.Total < 0 {
		run.Total = 0
	}
	if run.Passed < 0 {
		run.Passed = 0
	}
	if run.Failed < 0 {
		run.Failed = 0
	}
	if run.Warning < 0 {
		run.Warning = 0
	}
	if run.Metrics == nil {
		run.Metrics = map[string]any{}
	}
	run.ThresholdStatus = truncateEvaluationString(strings.TrimSpace(run.ThresholdStatus), 64)
	run.Summary = truncateEvaluationString(strings.TrimSpace(run.Summary), 4096)
	return run
}

func normalizeEvaluationResult(result EvaluationResult) EvaluationResult {
	result.ID = strings.TrimSpace(result.ID)
	if result.ID == "" {
		result.ID = newEvaluationID("evalres")
	}
	result.RunID = strings.TrimSpace(result.RunID)
	result.SubjectType = normalizeEvaluationSubjectType(result.SubjectType)
	result.SubjectID = truncateEvaluationString(strings.TrimSpace(result.SubjectID), 256)
	result.UserID = strings.TrimSpace(result.UserID)
	result.SessionID = strings.TrimSpace(result.SessionID)
	result.JobID = strings.TrimSpace(result.JobID)
	result.SkillName = strings.TrimSpace(result.SkillName)
	result.Provider = strings.TrimSpace(result.Provider)
	result.Model = strings.TrimSpace(result.Model)
	result.PromptID = strings.TrimSpace(result.PromptID)
	result.PromptVersion = strings.TrimSpace(result.PromptVersion)
	result.PromptHash = strings.TrimSpace(result.PromptHash)
	result.ExperimentID = strings.TrimSpace(result.ExperimentID)
	result.VariantID = strings.TrimSpace(result.VariantID)
	result.Status = normalizeEvaluationResultStatus(result.Status)
	if result.Score < 0 {
		result.Score = 0
	}
	result.Input = truncateEvaluationString(strings.TrimSpace(result.Input), 8192)
	result.Output = truncateEvaluationString(strings.TrimSpace(result.Output), 8192)
	if result.Metrics == nil {
		result.Metrics = map[string]any{}
	}
	result.Findings = normalizeEvaluationFindings(result.Findings)
	if result.CreatedAt.IsZero() {
		result.CreatedAt = time.Now().UTC()
	} else {
		result.CreatedAt = result.CreatedAt.UTC()
	}
	return result
}

func normalizeEvaluationReview(review EvaluationReview) EvaluationReview {
	review.ID = strings.TrimSpace(review.ID)
	if review.ID == "" {
		review.ID = newEvaluationID("evalrev")
	}
	review.ResultID = strings.TrimSpace(review.ResultID)
	review.Status = normalizeEvaluationReviewStatus(review.Status)
	review.Reviewer = truncateEvaluationString(strings.TrimSpace(review.Reviewer), 256)
	review.Note = truncateEvaluationString(strings.TrimSpace(review.Note), 4096)
	now := time.Now().UTC()
	if review.CreatedAt.IsZero() {
		review.CreatedAt = now
	} else {
		review.CreatedAt = review.CreatedAt.UTC()
	}
	if review.UpdatedAt.IsZero() {
		review.UpdatedAt = review.CreatedAt
	} else {
		review.UpdatedAt = review.UpdatedAt.UTC()
	}
	return review
}

func normalizeEvaluationScope(scope EvaluationScope) EvaluationScope {
	if scope.From != nil {
		value := scope.From.UTC()
		scope.From = &value
	}
	if scope.To != nil {
		value := scope.To.UTC()
		scope.To = &value
	}
	scope.SubjectType = normalizeEvaluationSubjectType(scope.SubjectType)
	scope.UserID = strings.TrimSpace(scope.UserID)
	scope.SessionID = strings.TrimSpace(scope.SessionID)
	scope.JobID = strings.TrimSpace(scope.JobID)
	scope.JobStatus = strings.TrimSpace(scope.JobStatus)
	scope.TemplateID = normalizeLoopTemplateID(scope.TemplateID)
	scope.TaskType = strings.TrimSpace(scope.TaskType)
	scope.SkillName = strings.TrimSpace(scope.SkillName)
	scope.Provider = strings.TrimSpace(scope.Provider)
	scope.Model = strings.TrimSpace(scope.Model)
	scope.PromptID = strings.TrimSpace(scope.PromptID)
	scope.PromptVersion = strings.TrimSpace(scope.PromptVersion)
	scope.PromptHash = strings.TrimSpace(scope.PromptHash)
	scope.ExperimentID = strings.TrimSpace(scope.ExperimentID)
	scope.VariantID = strings.TrimSpace(scope.VariantID)
	return scope
}

func normalizeEvaluationFindings(findings []EvaluationFinding) []EvaluationFinding {
	if len(findings) == 0 {
		return []EvaluationFinding{}
	}
	out := make([]EvaluationFinding, 0, len(findings))
	for _, finding := range findings {
		finding.Severity = normalizeEvaluationFindingSeverity(finding.Severity)
		finding.Code = truncateEvaluationString(strings.TrimSpace(finding.Code), 128)
		finding.Message = truncateEvaluationString(strings.TrimSpace(finding.Message), 1024)
		if finding.Metadata == nil {
			finding.Metadata = map[string]any{}
		}
		if finding.Code == "" && finding.Message == "" {
			continue
		}
		out = append(out, finding)
	}
	return out
}

func normalizeEvaluationRunFilter(filter EvaluationRunFilter) EvaluationRunFilter {
	filter.Status = normalizeOptionalEvaluationRunStatus(filter.Status)
	filter.Trigger = strings.TrimSpace(filter.Trigger)
	filter.Limit = normalizeEvaluationLimit(filter.Limit)
	return filter
}

func normalizeEvaluationResultFilter(filter EvaluationResultFilter) EvaluationResultFilter {
	filter.RunID = strings.TrimSpace(filter.RunID)
	filter.Status = normalizeOptionalEvaluationResultStatus(filter.Status)
	filter.SubjectType = normalizeEvaluationSubjectType(filter.SubjectType)
	filter.UserID = strings.TrimSpace(filter.UserID)
	filter.SessionID = strings.TrimSpace(filter.SessionID)
	filter.JobID = strings.TrimSpace(filter.JobID)
	filter.SkillName = strings.TrimSpace(filter.SkillName)
	filter.Provider = strings.TrimSpace(filter.Provider)
	filter.Model = strings.TrimSpace(filter.Model)
	filter.PromptID = strings.TrimSpace(filter.PromptID)
	filter.PromptVersion = strings.TrimSpace(filter.PromptVersion)
	filter.PromptHash = strings.TrimSpace(filter.PromptHash)
	filter.ExperimentID = strings.TrimSpace(filter.ExperimentID)
	filter.VariantID = strings.TrimSpace(filter.VariantID)
	filter.Limit = normalizeEvaluationLimit(filter.Limit)
	return filter
}

func normalizeEvaluationReviewFilter(filter EvaluationReviewFilter) EvaluationReviewFilter {
	filter.ResultID = strings.TrimSpace(filter.ResultID)
	filter.Status = normalizeOptionalEvaluationReviewStatus(filter.Status)
	filter.Limit = normalizeEvaluationLimit(filter.Limit)
	return filter
}

func normalizeEvaluationLimit(limit int) int {
	if limit < 0 {
		return 0
	}
	if limit > 1000 {
		return 1000
	}
	return limit
}

func normalizeEvaluationRunStatus(status string) string {
	status = normalizeOptionalEvaluationRunStatus(status)
	if status == "" {
		return EvaluationRunStatusPending
	}
	return status
}

func normalizeOptionalEvaluationRunStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case EvaluationRunStatusPending:
		return EvaluationRunStatusPending
	case EvaluationRunStatusRunning:
		return EvaluationRunStatusRunning
	case EvaluationRunStatusCompleted:
		return EvaluationRunStatusCompleted
	case EvaluationRunStatusFailed:
		return EvaluationRunStatusFailed
	default:
		return ""
	}
}

func normalizeEvaluationResultStatus(status string) string {
	status = normalizeOptionalEvaluationResultStatus(status)
	if status == "" {
		return EvaluationResultStatusWarning
	}
	return status
}

func normalizeOptionalEvaluationResultStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case EvaluationResultStatusPassed:
		return EvaluationResultStatusPassed
	case EvaluationResultStatusFailed:
		return EvaluationResultStatusFailed
	case EvaluationResultStatusWarning:
		return EvaluationResultStatusWarning
	default:
		return ""
	}
}

func normalizeEvaluationReviewStatus(status string) string {
	status = normalizeOptionalEvaluationReviewStatus(status)
	if status == "" {
		return EvaluationReviewStatusPending
	}
	return status
}

func normalizeOptionalEvaluationReviewStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case EvaluationReviewStatusPending:
		return EvaluationReviewStatusPending
	case EvaluationReviewStatusPassed:
		return EvaluationReviewStatusPassed
	case EvaluationReviewStatusFailed:
		return EvaluationReviewStatusFailed
	case EvaluationReviewStatusIgnored:
		return EvaluationReviewStatusIgnored
	default:
		return ""
	}
}

func normalizeEvaluationSubjectType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case EvaluationSubjectJob:
		return EvaluationSubjectJob
	case EvaluationSubjectSession:
		return EvaluationSubjectSession
	case EvaluationSubjectSkillExecution:
		return EvaluationSubjectSkillExecution
	case EvaluationSubjectGoldenCase:
		return EvaluationSubjectGoldenCase
	case EvaluationSubjectDeepAgent:
		return EvaluationSubjectDeepAgent
	default:
		return ""
	}
}

func normalizeEvaluationFindingSeverity(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "error", "warning", "info":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "warning"
	}
}

func evaluationRunMatches(run EvaluationRun, filter EvaluationRunFilter) bool {
	if filter.Status != "" && run.Status != filter.Status {
		return false
	}
	if filter.Trigger != "" && run.Trigger != filter.Trigger {
		return false
	}
	return true
}

func evaluationResultMatches(result EvaluationResult, filter EvaluationResultFilter) bool {
	if filter.RunID != "" && result.RunID != filter.RunID {
		return false
	}
	if filter.Status != "" && result.Status != filter.Status {
		return false
	}
	if filter.SubjectType != "" && result.SubjectType != filter.SubjectType {
		return false
	}
	if filter.UserID != "" && result.UserID != filter.UserID {
		return false
	}
	if filter.SessionID != "" && result.SessionID != filter.SessionID {
		return false
	}
	if filter.JobID != "" && result.JobID != filter.JobID {
		return false
	}
	if filter.SkillName != "" && result.SkillName != filter.SkillName {
		return false
	}
	if filter.Provider != "" && result.Provider != filter.Provider {
		return false
	}
	if filter.Model != "" && result.Model != filter.Model {
		return false
	}
	if filter.PromptID != "" && result.PromptID != filter.PromptID {
		return false
	}
	if filter.PromptVersion != "" && result.PromptVersion != filter.PromptVersion {
		return false
	}
	if filter.PromptHash != "" && result.PromptHash != filter.PromptHash {
		return false
	}
	if filter.ExperimentID != "" && result.ExperimentID != filter.ExperimentID {
		return false
	}
	if filter.VariantID != "" && result.VariantID != filter.VariantID {
		return false
	}
	return true
}

func evaluationReviewMatches(review EvaluationReview, filter EvaluationReviewFilter) bool {
	if filter.ResultID != "" && review.ResultID != filter.ResultID {
		return false
	}
	if filter.Status != "" && review.Status != filter.Status {
		return false
	}
	return true
}

func summarizeEvaluationResults(run EvaluationRun, results []EvaluationResult) EvaluationRunSummary {
	summary := EvaluationRunSummary{
		RunID:           run.ID,
		Metrics:         cloneEvaluationMap(run.Metrics),
		ThresholdStatus: run.ThresholdStatus,
	}
	for _, result := range results {
		summary.Total++
		switch result.Status {
		case EvaluationResultStatusPassed:
			summary.Passed++
		case EvaluationResultStatusFailed:
			summary.Failed++
		case EvaluationResultStatusWarning:
			summary.Warning++
		}
	}
	if summary.Total == 0 {
		summary.Total = run.Total
		summary.Passed = run.Passed
		summary.Failed = run.Failed
		summary.Warning = run.Warning
	}
	if summary.Total > 0 {
		total := float64(summary.Total)
		summary.PassRate = float64(summary.Passed) / total
		summary.FailureRate = float64(summary.Failed) / total
		summary.WarningRate = float64(summary.Warning) / total
	}
	return summary
}

func cloneEvaluationRun(run EvaluationRun) EvaluationRun {
	run.Scope = cloneEvaluationScope(run.Scope)
	run.Metrics = cloneEvaluationMap(run.Metrics)
	if run.CompletedAt != nil {
		value := *run.CompletedAt
		run.CompletedAt = &value
	}
	return run
}

func cloneEvaluationScope(scope EvaluationScope) EvaluationScope {
	if scope.From != nil {
		value := *scope.From
		scope.From = &value
	}
	if scope.To != nil {
		value := *scope.To
		scope.To = &value
	}
	return scope
}

func cloneEvaluationResult(result EvaluationResult) EvaluationResult {
	result.Metrics = cloneEvaluationMap(result.Metrics)
	result.Findings = cloneEvaluationFindings(result.Findings)
	return result
}

func cloneEvaluationReview(review EvaluationReview) EvaluationReview {
	return review
}

func cloneEvaluationFindings(findings []EvaluationFinding) []EvaluationFinding {
	out := make([]EvaluationFinding, len(findings))
	for i, finding := range findings {
		finding.Metadata = cloneEvaluationMap(finding.Metadata)
		out[i] = finding
	}
	if out == nil {
		return []EvaluationFinding{}
	}
	return out
}

func cloneEvaluationMap(values map[string]any) map[string]any {
	if values == nil {
		return map[string]any{}
	}
	clone := make(map[string]any, len(values))
	for key, value := range values {
		clone[key] = value
	}
	return clone
}

func truncateEvaluationString(value string, max int) string {
	if max <= 0 || len(value) <= max {
		return value
	}
	return value[:max]
}

func newEvaluationID(prefix string) string {
	var data [8]byte
	if _, err := rand.Read(data[:]); err != nil {
		return prefix + "-" + time.Now().UTC().Format("20060102T150405.000000000Z")
	}
	return prefix + "-" + time.Now().UTC().Format("20060102T150405.000000000Z") + "-" + hex.EncodeToString(data[:])
}
