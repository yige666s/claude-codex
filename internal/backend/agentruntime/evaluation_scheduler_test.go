package agentruntime

import (
	"context"
	"testing"
	"time"
)

func TestDailyEvaluationUsesPreviousUTC8DayIncrementally(t *testing.T) {
	ctx := context.Background()
	runtime := testRuntime(t)
	runtime.SetJobStore(NewMemoryJobStore())
	runtime.SetSkillExecutionStore(NewMemorySkillExecutionStore())
	evaluations := NewMemoryEvaluationStore()
	server := NewServer(runtime, HeaderAuthenticator{UserHeader: "X-User-ID"}, NoopRateLimiter{}, nil)
	server.SetEvaluationStore(evaluations)

	location := time.FixedZone("UTC+8", 8*60*60)
	now := time.Date(2026, 5, 16, 5, 0, 0, 0, location)
	previousDayAt := time.Date(2026, 5, 15, 10, 0, 0, 0, location).UTC()
	currentDayAt := time.Date(2026, 5, 16, 1, 0, 0, 0, location).UTC()

	session, err := runtime.CreateSession(ctx, "alice", "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	session.AddUserMessage("make daily report")
	session.AddAssistantMessage("daily report ready")
	if err := runtime.sessions.Save(ctx, "alice", session); err != nil {
		t.Fatalf("save session: %v", err)
	}

	previousJob, err := runtime.CreateJob(ctx, ChatRequest{UserID: "alice", SessionID: session.ID, Content: "previous day"}, "chat")
	if err != nil {
		t.Fatalf("create previous job: %v", err)
	}
	if err := runtime.jobs.UpdateJobStatus(ctx, previousJob.UserID, previousJob.ID, JobStatusSucceeded, "", previousDayAt); err != nil {
		t.Fatalf("update previous job: %v", err)
	}
	currentJob, err := runtime.CreateJob(ctx, ChatRequest{UserID: "alice", SessionID: session.ID, Content: "current day"}, "chat")
	if err != nil {
		t.Fatalf("create current job: %v", err)
	}
	if err := runtime.jobs.UpdateJobStatus(ctx, currentJob.UserID, currentJob.ID, JobStatusSucceeded, "", currentDayAt); err != nil {
		t.Fatalf("update current job: %v", err)
	}

	report, err := server.RunDailyEvaluationOnce(ctx, now, DailyEvaluationConfig{
		Enabled:  true,
		Location: location,
		Hour:     5,
		Minute:   0,
		UserIDs:  []string{"alice"},
	})
	if err != nil {
		t.Fatalf("run daily eval: %v", err)
	}
	if report.Created != 1 || report.Skipped != 0 || report.Total != 1 {
		t.Fatalf("unexpected daily report: %+v", report)
	}
	if got, want := report.Day, "2026-05-15"; got != want {
		t.Fatalf("day = %q, want %q", got, want)
	}
	runs, err := evaluations.ListEvaluationRuns(ctx, EvaluationRunFilter{Trigger: EvaluationTriggerDailyIncremental})
	if err != nil {
		t.Fatalf("list eval runs: %v", err)
	}
	if len(runs) != 1 || runs[0].Total != 1 {
		t.Fatalf("unexpected runs: %+v", runs)
	}
	results, err := evaluations.ListEvaluationResults(ctx, EvaluationResultFilter{RunID: runs[0].ID})
	if err != nil {
		t.Fatalf("list eval results: %v", err)
	}
	if len(results) != 1 || results[0].JobID != previousJob.ID {
		t.Fatalf("unexpected results: %+v", results)
	}

	second, err := server.RunDailyEvaluationOnce(ctx, now, DailyEvaluationConfig{
		Enabled:  true,
		Location: location,
		Hour:     5,
		Minute:   0,
		UserIDs:  []string{"alice"},
	})
	if err != nil {
		t.Fatalf("rerun daily eval: %v", err)
	}
	if second.Created != 0 || second.Skipped != 1 {
		t.Fatalf("expected idempotent skip, got %+v", second)
	}
}

func TestDurationUntilNextDailyEvaluation(t *testing.T) {
	location := time.FixedZone("UTC+8", 8*60*60)
	config := DailyEvaluationConfig{Location: location, Hour: 5, Minute: 0}
	before := time.Date(2026, 5, 16, 4, 30, 0, 0, location)
	if got := durationUntilNextDailyEvaluation(before, config); got != 30*time.Minute {
		t.Fatalf("delay before schedule = %s, want 30m", got)
	}
	after := time.Date(2026, 5, 16, 5, 1, 0, 0, location)
	if got := durationUntilNextDailyEvaluation(after, config); got != 23*time.Hour+59*time.Minute {
		t.Fatalf("delay after schedule = %s, want 23h59m", got)
	}
}
