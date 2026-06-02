package agentruntime

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestMemoryEvaluationStoreLifecycle(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryEvaluationStore()
	if err := store.Init(ctx); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	started := time.Date(2026, 5, 16, 8, 0, 0, 0, time.UTC)
	run, err := store.CreateEvaluationRun(ctx, EvaluationRun{
		ID:      "run-1",
		Name:    "last 24h",
		Status:  EvaluationRunStatusRunning,
		Trigger: "manual",
		Scope: EvaluationScope{
			SubjectType: EvaluationSubjectJob,
			UserID:      "alice",
		},
		StartedAt: started,
		Metrics:   map[string]any{"cost": 1.25},
	})
	if err != nil {
		t.Fatalf("CreateEvaluationRun() error = %v", err)
	}
	if run.ID != "run-1" || run.Scope.UserID != "alice" {
		t.Fatalf("unexpected run: %#v", run)
	}

	passed, err := store.CreateEvaluationResult(ctx, EvaluationResult{
		ID:          "result-1",
		RunID:       run.ID,
		SubjectType: EvaluationSubjectJob,
		SubjectID:   "job-1",
		UserID:      "alice",
		SessionID:   "session-1",
		JobID:       "job-1",
		Status:      EvaluationResultStatusPassed,
		Score:       1,
		Metrics:     map[string]any{"latency_ms": 2500},
		CreatedAt:   started.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("CreateEvaluationResult(passed) error = %v", err)
	}
	if passed.Status != EvaluationResultStatusPassed {
		t.Fatalf("unexpected passed result: %#v", passed)
	}
	failed, err := store.CreateEvaluationResult(ctx, EvaluationResult{
		ID:          "result-2",
		RunID:       run.ID,
		SubjectType: EvaluationSubjectJob,
		SubjectID:   "job-2",
		UserID:      "alice",
		JobID:       "job-2",
		Status:      EvaluationResultStatusFailed,
		Findings: []EvaluationFinding{{
			Severity: "error",
			Code:     "job_failed",
			Message:  "job failed",
		}},
		CreatedAt: started.Add(2 * time.Minute),
	})
	if err != nil {
		t.Fatalf("CreateEvaluationResult(failed) error = %v", err)
	}

	completedAt := started.Add(10 * time.Minute)
	run.Status = EvaluationRunStatusCompleted
	run.CompletedAt = &completedAt
	run.Total = 2
	run.Passed = 1
	run.Failed = 1
	run.ThresholdStatus = "failed"
	if _, err := store.UpdateEvaluationRun(ctx, run); err != nil {
		t.Fatalf("UpdateEvaluationRun() error = %v", err)
	}

	summary, err := store.SummarizeEvaluationRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("SummarizeEvaluationRun() error = %v", err)
	}
	if summary.Total != 2 || summary.Passed != 1 || summary.Failed != 1 || summary.PassRate != 0.5 {
		t.Fatalf("unexpected summary: %#v", summary)
	}

	failedResults, err := store.ListEvaluationResults(ctx, EvaluationResultFilter{
		RunID:  run.ID,
		Status: EvaluationResultStatusFailed,
	})
	if err != nil {
		t.Fatalf("ListEvaluationResults() error = %v", err)
	}
	if len(failedResults) != 1 || failedResults[0].ID != failed.ID {
		t.Fatalf("unexpected failed results: %#v", failedResults)
	}

	review, err := store.CreateEvaluationReview(ctx, EvaluationReview{
		ID:       "review-1",
		ResultID: failed.ID,
		Status:   EvaluationReviewStatusPending,
	})
	if err != nil {
		t.Fatalf("CreateEvaluationReview() error = %v", err)
	}
	review.Status = EvaluationReviewStatusFailed
	review.Reviewer = "admin"
	review.Note = "confirmed"
	updated, err := store.UpdateEvaluationReview(ctx, review)
	if err != nil {
		t.Fatalf("UpdateEvaluationReview() error = %v", err)
	}
	if updated.Status != EvaluationReviewStatusFailed || updated.ResultID != failed.ID {
		t.Fatalf("unexpected updated review: %#v", updated)
	}
}

func TestMemoryEvaluationStoreClonesMutableFields(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryEvaluationStore()
	run, err := store.CreateEvaluationRun(ctx, EvaluationRun{
		ID:      "run-clone",
		Name:    "clone",
		Metrics: map[string]any{"value": "original"},
	})
	if err != nil {
		t.Fatalf("CreateEvaluationRun() error = %v", err)
	}
	run.Metrics["value"] = "mutated"

	loaded, err := store.GetEvaluationRun(ctx, "run-clone")
	if err != nil {
		t.Fatalf("GetEvaluationRun() error = %v", err)
	}
	if loaded.Metrics["value"] != "original" {
		t.Fatalf("stored metrics mutated: %#v", loaded.Metrics)
	}
}

func TestMemoryEvaluationStoreGoldenSetLifecycle(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryEvaluationStore()
	set, err := store.UpsertGoldenSet(ctx, GoldenSet{
		ID:      "support-rag",
		Name:    "Support RAG",
		Version: "v1",
		Cases: []GoldenCase{{
			ID:            "case-1",
			Query:         "如何提高回答准确率？",
			ExpectedFacts: []string{"权限过滤"},
			GoldEvidence:  []GoldenEvidence{{ID: "doc-1", Content: "需要权限过滤"}},
			Tags:          []string{"rag"},
			Metadata:      map[string]any{"owner": "qa"},
		}},
	})
	if err != nil {
		t.Fatalf("UpsertGoldenSet() error = %v", err)
	}
	set.Cases[0].Metadata["owner"] = "mutated"

	loaded, err := store.GetGoldenSet(ctx, "support-rag")
	if err != nil {
		t.Fatalf("GetGoldenSet() error = %v", err)
	}
	if loaded.Cases[0].Metadata["owner"] != "qa" {
		t.Fatalf("stored golden set mutated: %#v", loaded.Cases[0].Metadata)
	}

	listed, err := store.ListGoldenSets(ctx, GoldenSetFilter{Limit: 10})
	if err != nil {
		t.Fatalf("ListGoldenSets() error = %v", err)
	}
	if len(listed) != 1 || listed[0].ID != "support-rag" {
		t.Fatalf("unexpected golden sets: %#v", listed)
	}
	if err := store.DeleteGoldenSet(ctx, "support-rag"); err != nil {
		t.Fatalf("DeleteGoldenSet() error = %v", err)
	}
	if _, err := store.GetGoldenSet(ctx, "support-rag"); err != sql.ErrNoRows {
		t.Fatalf("GetGoldenSet after delete error = %v, want sql.ErrNoRows", err)
	}
}

func TestSQLEvaluationStorePostgresLifecycle(t *testing.T) {
	dsn := os.Getenv("AGENT_RUNTIME_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("set AGENT_RUNTIME_TEST_PG_DSN to run postgres integration test")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	for _, table := range []string{"agent_eval_reviews", "agent_eval_results", "agent_eval_runs"} {
		if _, err := db.ExecContext(ctx, `DROP TABLE IF EXISTS `+table); err != nil {
			t.Fatalf("drop %s: %v", table, err)
		}
	}

	store := NewSQLEvaluationStoreWithDialect(db, SQLDialectPostgres)
	if err := store.Init(ctx); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	run, err := store.CreateEvaluationRun(ctx, EvaluationRun{
		Name:    "pg run",
		Status:  EvaluationRunStatusRunning,
		Trigger: "manual",
		Scope:   EvaluationScope{SubjectType: EvaluationSubjectJob, Model: "model-a"},
		Metrics: map[string]any{"requests": float64(2)},
	})
	if err != nil {
		t.Fatalf("CreateEvaluationRun() error = %v", err)
	}
	if _, err := store.CreateEvaluationResult(ctx, EvaluationResult{
		RunID:       run.ID,
		SubjectType: EvaluationSubjectJob,
		SubjectID:   "job-1",
		Status:      EvaluationResultStatusWarning,
		Findings:    []EvaluationFinding{{Code: "risk_high", Message: "risk"}},
	}); err != nil {
		t.Fatalf("CreateEvaluationResult() error = %v", err)
	}
	summary, err := store.SummarizeEvaluationRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("SummarizeEvaluationRun() error = %v", err)
	}
	if summary.Total != 1 || summary.Warning != 1 {
		t.Fatalf("unexpected postgres summary: %#v", summary)
	}
}
