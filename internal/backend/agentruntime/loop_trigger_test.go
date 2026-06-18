package agentruntime

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRuntimeStartDeepAgentLoopTriggerDedupes(t *testing.T) {
	runtime := testRuntime(t)
	runtime.SetJobStore(NewMemoryJobStore())
	runtime.SetLoopTriggerPolicy(testLoopTriggerPolicy(true, true, true, true, "github"))
	queue := &captureJobQueue{}
	runtime.SetJobQueue(queue)

	session, err := runtime.CreateSession(context.Background(), "alice", "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	req := LoopTriggerRequest{
		UserID:      "alice",
		SessionID:   session.ID,
		Objective:   "调研 DeepAgent loop",
		TriggerType: LoopTriggerTypeWebhook,
		Source:      "github",
		DedupeKey:   "same-event",
		Payload:     map[string]any{"issue": float64(42)},
	}

	first, err := runtime.StartDeepAgentLoopTrigger(context.Background(), req)
	if err != nil {
		t.Fatalf("first trigger: %v", err)
	}
	second, err := runtime.StartDeepAgentLoopTrigger(context.Background(), req)
	if err != nil {
		t.Fatalf("second trigger: %v", err)
	}
	if first.Job == nil || second.Job == nil || first.Job.ID != second.Job.ID {
		t.Fatalf("expected duplicate trigger to reuse job: first=%#v second=%#v", first.Job, second.Job)
	}
	if first.Goal == nil || first.Job.LoopGoalID == "" || first.Goal.ID != first.Job.LoopGoalID {
		t.Fatalf("expected trigger to create loop goal linked to job: goal=%#v job=%#v", first.Goal, first.Job)
	}
	loadedGoal, err := runtime.GetLoopGoal(context.Background(), "alice", first.Goal.ID)
	if err != nil {
		t.Fatalf("GetLoopGoal() error = %v", err)
	}
	if loadedGoal.Trigger.Type != LoopTriggerTypeWebhook || loadedGoal.Trigger.Source != "github" || loadedGoal.Trigger.DedupeKey != "same-event" {
		t.Fatalf("unexpected stored goal trigger: %#v", loadedGoal.Trigger)
	}
	if loadedGoal.Budget.MaxActions == 0 || loadedGoal.Budget.MaxSteps == 0 {
		t.Fatalf("expected default loop budget on goal: %#v", loadedGoal.Budget)
	}
	if second.Duplicate != true {
		t.Fatalf("second trigger duplicate = %t, want true", second.Duplicate)
	}
	if len(queue.items) != 1 {
		t.Fatalf("queued items = %d, want 1", len(queue.items))
	}
	trigger, ok := runtime.loopTriggerForJob(first.Job.ID)
	if !ok {
		t.Fatalf("missing trigger metadata for job %s", first.Job.ID)
	}
	if trigger.Type != LoopTriggerTypeWebhook || trigger.Source != "github" || trigger.DedupeKey != "same-event" {
		t.Fatalf("unexpected trigger metadata: %#v", trigger)
	}
}

func TestServerCreateLoopTriggerDedupes(t *testing.T) {
	runtime := testRuntime(t)
	runtime.SetJobStore(NewMemoryJobStore())
	runtime.SetLoopTriggerPolicy(testLoopTriggerPolicy(true, true, true, true, "github"))
	runtime.SetJobQueue(&captureJobQueue{})
	server := NewServer(runtime, HeaderAuthenticator{}, NoopRateLimiter{}, nil)

	session, err := runtime.CreateSession(context.Background(), "alice", "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	body := `{"session_id":"` + session.ID + `","objective":"调研 DeepAgent loop","trigger_type":"webhook","source":"github","dedupe_key":"same-event"}`

	first := postLoopTrigger(t, server, body)
	if first.Code != http.StatusAccepted {
		t.Fatalf("first status = %d body=%s", first.Code, first.Body.String())
	}
	second := postLoopTrigger(t, server, body)
	if second.Code != http.StatusOK {
		t.Fatalf("second status = %d body=%s", second.Code, second.Body.String())
	}
	var firstPayload, secondPayload LoopTriggerResult
	if err := json.Unmarshal(first.Body.Bytes(), &firstPayload); err != nil {
		t.Fatalf("decode first: %v", err)
	}
	if err := json.Unmarshal(second.Body.Bytes(), &secondPayload); err != nil {
		t.Fatalf("decode second: %v", err)
	}
	if firstPayload.Job == nil || secondPayload.Job == nil || firstPayload.Job.ID != secondPayload.Job.ID {
		t.Fatalf("expected duplicate response to reuse job: first=%#v second=%#v", firstPayload.Job, secondPayload.Job)
	}
	if firstPayload.Goal == nil || firstPayload.Job.LoopGoalID != firstPayload.Goal.ID {
		t.Fatalf("expected loop goal in trigger response: %#v", firstPayload)
	}
	if !secondPayload.Duplicate {
		t.Fatalf("duplicate = false, want true")
	}
}

func TestServerLoopGoalCreateAndStartRun(t *testing.T) {
	runtime := testRuntime(t)
	runtime.SetJobStore(NewMemoryJobStore())
	runtime.SetJobQueue(&captureJobQueue{})
	server := NewServer(runtime, HeaderAuthenticator{}, NoopRateLimiter{}, nil)

	session, err := runtime.CreateSession(context.Background(), "alice", "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	body := `{"session_id":"` + session.ID + `","objective":"生成 loop engineering 实施方案","task_type":"research","deliverable":"markdown","rubric":{"acceptance_criteria":["包含 Phase 1"]},"budget":{"max_steps":3,"max_actions":5,"max_duration_ms":60000},"trigger":{"type":"manual","source":"api"}}`

	create := postLoopGoal(t, server, body)
	if create.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", create.Code, create.Body.String())
	}
	var createPayload struct {
		Goal *LoopGoal `json:"goal"`
	}
	if err := json.Unmarshal(create.Body.Bytes(), &createPayload); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if createPayload.Goal == nil || createPayload.Goal.ID == "" {
		t.Fatalf("missing goal in response: %s", create.Body.String())
	}
	if createPayload.Goal.Budget.MaxDuration != time.Minute {
		t.Fatalf("budget max duration = %s, want 1m", createPayload.Goal.Budget.MaxDuration)
	}

	run := postLoopGoalRun(t, server, createPayload.Goal.ID)
	if run.Code != http.StatusAccepted {
		t.Fatalf("run status = %d body=%s", run.Code, run.Body.String())
	}
	var runPayload LoopGoalRunResult
	if err := json.Unmarshal(run.Body.Bytes(), &runPayload); err != nil {
		t.Fatalf("decode run: %v", err)
	}
	if runPayload.Job == nil || runPayload.Job.LoopGoalID != createPayload.Goal.ID {
		t.Fatalf("expected started job linked to goal: %#v", runPayload.Job)
	}
}

func TestRuntimeLoopTriggerStoreDedupesAcrossRuntimeInstances(t *testing.T) {
	store := NewMemoryLoopTriggerStore()
	queue := &captureJobQueue{}
	runtimeA := testRuntime(t)
	runtimeA.SetLoopTriggerStore(store)
	runtimeA.SetJobStore(NewMemoryJobStore())
	runtimeA.SetLoopTriggerPolicy(testLoopTriggerPolicy(true, true, true, true, "github"))
	runtimeA.SetJobQueue(queue)
	session, err := runtimeA.CreateSession(context.Background(), "alice", "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	req := LoopTriggerRequest{UserID: "alice", SessionID: session.ID, Objective: "handle webhook", TriggerType: LoopTriggerTypeWebhook, Source: "github", DedupeKey: "delivery-1"}
	first, err := runtimeA.StartDeepAgentLoopTrigger(context.Background(), req)
	if err != nil {
		t.Fatalf("first trigger: %v", err)
	}
	runtimeB := testRuntime(t)
	runtimeB.SetLoopTriggerStore(store)
	runtimeB.SetJobStore(NewMemoryJobStore())
	runtimeB.SetLoopTriggerPolicy(testLoopTriggerPolicy(true, true, true, true, "github"))
	runtimeB.SetJobQueue(queue)
	second, err := runtimeB.StartDeepAgentLoopTrigger(context.Background(), req)
	if err != nil {
		t.Fatalf("second trigger: %v", err)
	}
	if !second.Duplicate || second.Job == nil || second.Job.ID != first.Job.ID {
		t.Fatalf("expected persisted dedupe to reuse first job: first=%#v second=%#v", first.Job, second.Job)
	}
	if len(queue.items) != 1 {
		t.Fatalf("queued items = %d, want 1", len(queue.items))
	}
}

func TestServerLoopWebhookRequiresSignatureAndStartsTrigger(t *testing.T) {
	runtime := testRuntime(t)
	runtime.SetJobStore(NewMemoryJobStore())
	runtime.SetLoopTriggerPolicy(testLoopTriggerPolicy(false, true, false, false, "github"))
	runtime.SetJobQueue(&captureJobQueue{})
	server := NewServer(runtime, HeaderAuthenticator{}, NoopRateLimiter{}, nil)
	server.SetOperationRateLimiter(NewOperationRateLimiter(nil))
	server.SetLoopWebhookSecrets(map[string]string{"github": "secret"})
	session, err := runtime.CreateSession(context.Background(), "alice", "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	body := `{"user_id":"alice","session_id":"` + session.ID + `","objective":"fix issue","dedupe_key":"delivery-1"}`
	unsigned := httptest.NewRequest(http.MethodPost, "/v1/loop/webhooks/github", strings.NewReader(body))
	unsigned.Header.Set("Content-Type", "application/json")
	unsignedRec := httptest.NewRecorder()
	server.ServeHTTP(unsignedRec, unsigned)
	if unsignedRec.Code != http.StatusUnauthorized {
		t.Fatalf("unsigned status = %d body=%s", unsignedRec.Code, unsignedRec.Body.String())
	}
	signed := httptest.NewRequest(http.MethodPost, "/v1/loop/webhooks/github", strings.NewReader(body))
	signed.Header.Set("Content-Type", "application/json")
	signed.Header.Set("X-Agentapi-Webhook-Signature", "sha256="+testHMACSHA256("secret", []byte(body)))
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, signed)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("signed status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload LoopTriggerResult
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.Trigger.Type != LoopTriggerTypeWebhook || payload.Trigger.Source != "github" || payload.Job == nil {
		t.Fatalf("unexpected webhook trigger result: %#v", payload)
	}
}

func TestLoopTriggerQuotaBlocksNewRun(t *testing.T) {
	runtime := testRuntime(t)
	runtime.SetJobStore(NewMemoryJobStore())
	runtime.SetLoopTriggerPolicy(testLoopTriggerPolicy(true, true, true, true))
	queue := &captureJobQueue{}
	runtime.SetJobQueue(queue)
	usage := NewMemoryLLMUsageStore()
	if err := usage.RecordLLMUsage(context.Background(), LLMUsageRecord{UserID: "alice", TotalTokens: 10, Status: "success", CreatedAt: time.Now()}); err != nil {
		t.Fatalf("record usage: %v", err)
	}
	runtime.SetLoopTriggerQuotaChecker(func(ctx context.Context, req LoopTriggerRequest) error {
		return CheckLoopTriggerQuota(ctx, usage, LLMGovernanceConfig{DailyTokenQuota: 10}, req.UserID)
	})
	session, err := runtime.CreateSession(context.Background(), "alice", "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	_, err = runtime.StartDeepAgentLoopTrigger(context.Background(), LoopTriggerRequest{UserID: "alice", SessionID: session.ID, Objective: "over quota", TriggerType: LoopTriggerTypeManual})
	if err == nil || !strings.Contains(err.Error(), "quota exceeded") {
		t.Fatalf("expected quota error, got %v", err)
	}
	if len(queue.items) != 0 {
		t.Fatalf("queued items = %d, want 0", len(queue.items))
	}
}

func TestLoopTriggerPolicyRequiresReleaseGateForAutomation(t *testing.T) {
	runtime := testRuntime(t)
	runtime.SetJobStore(NewMemoryJobStore())
	runtime.SetJobQueue(&captureJobQueue{})
	session, err := runtime.CreateSession(context.Background(), "alice", "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	_, err = runtime.StartDeepAgentLoopTrigger(context.Background(), LoopTriggerRequest{
		UserID:      "alice",
		SessionID:   session.ID,
		Objective:   "scheduled run",
		TemplateID:  LoopTemplateResearchReport,
		TriggerType: LoopTriggerTypeSchedule,
		Source:      "scheduler",
	})
	if !errors.Is(err, ErrLoopTriggerPolicyBlocked) || !strings.Contains(err.Error(), "release gate") {
		t.Fatalf("expected release gate policy block, got %v", err)
	}
}

func TestLoopTriggerPolicyEnforcesPerTriggerRules(t *testing.T) {
	sessionID := "sess-1"
	cases := []struct {
		name string
		req  LoopTriggerRequest
		want string
	}{
		{
			name: "webhook source without secret",
			req:  LoopTriggerRequest{UserID: "alice", SessionID: sessionID, Objective: "webhook", TriggerType: LoopTriggerTypeWebhook, Source: "unknown"},
			want: "signing secret",
		},
		{
			name: "schedule high risk template",
			req:  LoopTriggerRequest{UserID: "alice", SessionID: sessionID, Objective: "fix code on schedule", TemplateID: LoopTemplateCodeFix, TriggerType: LoopTriggerTypeSchedule, Source: "scheduler"},
			want: "template is not allowed",
		},
		{
			name: "monitor write side effects",
			req:  LoopTriggerRequest{UserID: "alice", SessionID: sessionID, Objective: "monitor", TemplateID: LoopTemplateWebMonitor, TriggerType: LoopTriggerTypeMonitor, Source: "monitor", Payload: map[string]any{"allow_write": true}},
			want: "read-only",
		},
		{
			name: "eval without result scope",
			req:  LoopTriggerRequest{UserID: "alice", SessionID: sessionID, Objective: "repair", TriggerType: LoopTriggerTypeEval, Source: "evaluation:run-1", Payload: map[string]any{"evaluation_run_id": "run-1"}},
			want: "evaluation result scope",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runtime := testRuntime(t)
			runtime.SetJobStore(NewMemoryJobStore())
			runtime.SetJobQueue(&captureJobQueue{})
			runtime.SetLoopTriggerPolicy(testLoopTriggerPolicy(true, true, true, true, "github"))
			_, err := runtime.StartDeepAgentLoopTrigger(context.Background(), tc.req)
			if !errors.Is(err, ErrLoopTriggerPolicyBlocked) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected policy block containing %q, got %v", tc.want, err)
			}
		})
	}
}

func TestLoopTriggerAutomationRunsScheduleAndMonitor(t *testing.T) {
	runtime := testRuntime(t)
	runtime.SetJobStore(NewMemoryJobStore())
	runtime.SetLoopTriggerPolicy(testLoopTriggerPolicy(true, false, true, true))
	queue := &captureJobQueue{}
	runtime.SetJobQueue(queue)
	server := NewServer(runtime, HeaderAuthenticator{}, NoopRateLimiter{}, nil)
	session, err := runtime.CreateSession(context.Background(), "alice", "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	_, err = runtime.CreateLoopGoal(context.Background(), &LoopGoal{
		UserID:    "alice",
		SessionID: session.ID,
		Objective: "scheduled objective",
		Trigger:   LoopTrigger{Type: LoopTriggerTypeSchedule, Source: "cron", Payload: map[string]any{"cron": "* * * * *"}},
		Metadata:  map[string]any{"template_id": LoopTemplateResearchReport},
	})
	if err != nil {
		t.Fatalf("create schedule goal: %v", err)
	}
	job, err := runtime.CreateJob(context.Background(), ChatRequest{UserID: "alice", SessionID: session.ID, Content: "monitored"}, JobTypeChat)
	if err != nil {
		t.Fatalf("create monitored job: %v", err)
	}
	if err := runtime.jobs.UpdateJobStatus(context.Background(), "alice", job.ID, JobStatusFailed, "boom", time.Now()); err != nil {
		t.Fatalf("mark job failed: %v", err)
	}
	_, err = runtime.CreateLoopGoal(context.Background(), &LoopGoal{
		UserID:    "alice",
		SessionID: session.ID,
		Objective: "monitor objective",
		Trigger:   LoopTrigger{Type: LoopTriggerTypeMonitor, Source: "job-monitor", Payload: map[string]any{"resource_type": "job", "job_id": job.ID, "expected_status": JobStatusSucceeded, "terminal_only": true}},
		Metadata:  map[string]any{"template_id": LoopTemplateWebMonitor},
	})
	if err != nil {
		t.Fatalf("create monitor goal: %v", err)
	}
	automationNow := time.Now().UTC().Truncate(time.Minute)
	report, err := server.RunLoopTriggerAutomationOnce(context.Background(), automationNow, LoopTriggerAutomationConfig{})
	if err != nil {
		t.Fatalf("automation: %v", err)
	}
	if report.Triggered != 2 || len(queue.items) != 2 {
		t.Fatalf("triggered=%d queued=%d report=%#v", report.Triggered, len(queue.items), report)
	}
	status := server.LoopTriggerAutomationStatus()
	if status.LastRunAt == nil || status.NextDueAt == nil || status.LastReport.Triggered != 2 {
		t.Fatalf("automation status not updated: %#v", status)
	}
	stats, err := runtime.LoopTriggerLedgerStats(context.Background(), time.Time{})
	if err != nil {
		t.Fatalf("LoopTriggerLedgerStats() error = %v", err)
	}
	if stats.Total != 2 || len(stats.ByType) == 0 || len(stats.BySource) == 0 {
		t.Fatalf("unexpected trigger ledger stats: %#v", stats)
	}
	again, err := server.RunLoopTriggerAutomationOnce(context.Background(), automationNow.Add(30*time.Second), LoopTriggerAutomationConfig{})
	if err != nil {
		t.Fatalf("automation duplicate pass: %v", err)
	}
	if again.DedupeConflicts == 0 {
		t.Fatalf("expected dedupe conflicts on second pass, report=%#v queued=%d", again, len(queue.items))
	}
	pruned, err := runtime.loopTriggers.PruneExpiredLoopTriggers(context.Background(), time.Now().UTC().Add(defaultLoopTriggerDedupeWindow+time.Minute))
	if err != nil {
		t.Fatalf("PruneExpiredLoopTriggers() error = %v", err)
	}
	if pruned < 2 {
		t.Fatalf("pruned = %d, want at least 2", pruned)
	}
}

func TestLoopTriggerAutomationReadinessRequiresRunningWorker(t *testing.T) {
	server := NewServer(nil, HeaderAuthenticator{}, NoopRateLimiter{}, nil)
	stop := server.StartLoopTriggerAutomationScheduler(LoopTriggerAutomationConfig{Enabled: true, PollInterval: time.Hour})
	defer stop()
	check := server.LoopTriggerAutomationReadinessCheck()
	if err := check(context.Background()); err == nil || !strings.Contains(err.Error(), "not running") {
		t.Fatalf("expected readiness failure for missing worker, got %v", err)
	}

	runtime := testRuntime(t)
	runtime.SetJobStore(NewMemoryJobStore())
	runtime.SetJobQueue(&captureJobQueue{})
	server = NewServer(runtime, HeaderAuthenticator{}, NoopRateLimiter{}, nil)
	stop = server.StartLoopTriggerAutomationScheduler(LoopTriggerAutomationConfig{Enabled: true, PollInterval: time.Hour})
	defer stop()
	if err := server.LoopTriggerAutomationReadinessCheck()(context.Background()); err != nil {
		t.Fatalf("expected running automation readiness, got %v", err)
	}
	status := server.LoopTriggerAutomationStatus()
	if !status.Enabled || !status.Running || status.NextDueAt == nil {
		t.Fatalf("unexpected running status: %#v", status)
	}
}

func TestScheduleCronUsesRobfigParserFeatures(t *testing.T) {
	dueAt, ok := cronDueTime("*/5 * * * *", "", time.Date(2026, 6, 18, 12, 10, 30, 0, time.UTC), time.Minute)
	if !ok {
		t.Fatal("expected */5 cron to be due inside current poll window")
	}
	if got := dueAt.UTC().Format("15:04"); got != "12:10" {
		t.Fatalf("due time = %s, want 12:10", got)
	}
	if _, ok := cronDueTime("*/5 * * * *", "", time.Date(2026, 6, 18, 12, 11, 30, 0, time.UTC), time.Minute); ok {
		t.Fatal("did not expect */5 cron to be due at 12:11")
	}
	if _, ok := cronDueTime("10-20/5 12 * jun thu", "", time.Date(2026, 6, 18, 12, 15, 0, 0, time.UTC), time.Minute); !ok {
		t.Fatal("expected range/step/month/day-name cron to be due")
	}
}

func TestScheduleCronHonorsPayloadTimezone(t *testing.T) {
	now := time.Date(2026, 6, 18, 1, 30, 0, 0, time.UTC)
	dueAt, ok := cronDueTime("30 9 * * *", "Asia/Shanghai", now, time.Minute)
	if !ok {
		t.Fatal("expected Asia/Shanghai cron to be due at 09:30 local")
	}
	if got := dueAt.UTC().Format(time.RFC3339); got != "2026-06-18T01:30:00Z" {
		t.Fatalf("due time = %s, want 2026-06-18T01:30:00Z", got)
	}
}

func TestEvalFailureTriggersRepairLoop(t *testing.T) {
	runtime := testRuntime(t)
	runtime.SetJobStore(NewMemoryJobStore())
	runtime.SetLoopTriggerPolicy(testLoopTriggerPolicy(false, false, true, false))
	queue := &captureJobQueue{}
	runtime.SetJobQueue(queue)
	server := NewServer(runtime, HeaderAuthenticator{}, NoopRateLimiter{}, nil)
	session, err := runtime.CreateSession(context.Background(), "alice", "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	report := EvaluationRunReport{
		Run: EvaluationRun{ID: "eval-1"},
		Results: []EvaluationResult{{
			ID:          "result-1",
			Status:      EvaluationResultStatusFailed,
			UserID:      "alice",
			SessionID:   session.ID,
			SubjectType: EvaluationSubjectJob,
			SubjectID:   "job-1",
			Findings:    []EvaluationFinding{{Message: "missing artifact"}},
		}},
	}
	triggered := server.TriggerEvalRepairLoops(context.Background(), report)
	if triggered.Triggered != 1 || len(queue.items) != 1 {
		t.Fatalf("triggered=%#v queued=%d", triggered, len(queue.items))
	}
	again := server.TriggerEvalRepairLoops(context.Background(), report)
	if again.Triggered != 1 || len(queue.items) != 1 {
		t.Fatalf("deduped eval trigger should not enqueue again: %#v queued=%d", again, len(queue.items))
	}
}

func TestEvalRepairLoopIsScopedAndCapped(t *testing.T) {
	runtime := testRuntime(t)
	runtime.SetJobStore(NewMemoryJobStore())
	runtime.SetLoopTriggerPolicy(testLoopTriggerPolicy(false, false, true, false))
	queue := &captureJobQueue{}
	runtime.SetJobQueue(queue)
	server := NewServer(runtime, HeaderAuthenticator{}, NoopRateLimiter{}, nil)
	session, err := runtime.CreateSession(context.Background(), "alice", "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	results := make([]EvaluationResult, 0, 5)
	for i := 0; i < 5; i++ {
		results = append(results, EvaluationResult{
			ID:          fmt.Sprintf("result-%d", i),
			Status:      EvaluationResultStatusFailed,
			UserID:      "alice",
			SessionID:   session.ID,
			SubjectType: EvaluationSubjectDeepAgent,
			SubjectID:   fmt.Sprintf("run-%d", i),
			Findings:    []EvaluationFinding{{Message: "verifier failed"}},
		})
	}
	report := EvaluationRunReport{
		Run:     EvaluationRun{ID: "eval-capped", Trigger: "template_replay"},
		Results: results,
	}
	triggered := server.TriggerEvalRepairLoops(context.Background(), report)
	if triggered.Triggered != 3 || triggered.MaxSkipped != 2 || len(queue.items) != 3 {
		t.Fatalf("expected capped repair triggers, got report=%#v queued=%d", triggered, len(queue.items))
	}

	disabled := server.TriggerEvalRepairLoops(context.Background(), EvaluationRunReport{
		Run:     EvaluationRun{ID: "eval-disabled", Metrics: map[string]any{"eval_repair_disabled": true}},
		Results: results[:1],
	})
	if disabled.Triggered != 0 || disabled.Skipped != 1 {
		t.Fatalf("expected disabled repair to skip: %#v", disabled)
	}

	recursive := server.TriggerEvalRepairLoops(context.Background(), EvaluationRunReport{
		Run:     EvaluationRun{ID: "eval-recursive", Trigger: "eval_repair"},
		Results: results[:1],
	})
	if recursive.Triggered != 0 || recursive.Skipped != 1 {
		t.Fatalf("expected repair-triggered eval to skip: %#v", recursive)
	}
}

func postLoopTrigger(t *testing.T, server http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/loop/triggers", bytes.NewBufferString(body))
	req.Header.Set("X-User-ID", "alice")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	return rec
}

func postLoopGoal(t *testing.T, server http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/loop-goals", bytes.NewBufferString(body))
	req.Header.Set("X-User-ID", "alice")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	return rec
}

func postLoopGoalRun(t *testing.T, server http.Handler, goalID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/loop-goals/"+goalID+"/runs", nil)
	req.Header.Set("X-User-ID", "alice")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	return rec
}

func testHMACSHA256(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func testLoopTriggerPolicy(schedule, webhook, evalRepair, monitor bool, webhookSources ...string) LoopTriggerPolicy {
	policy := DefaultLoopTriggerPolicy()
	policy.ScheduleEnabled = schedule
	policy.WebhookEnabled = webhook
	policy.EvalRepairEnabled = evalRepair
	policy.MonitorEnabled = monitor
	policy.WebhookAllowedSources = append([]string(nil), webhookSources...)
	policy.ReleaseGate = LoopReleaseGateReport{
		CriticalTestsPassed:        true,
		TemplateReplayPassCount:    3,
		GovernanceKillSwitchPassed: true,
		QuotaGuardPassed:           true,
	}
	return policy
}
