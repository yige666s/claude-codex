package agentruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"claude-codex/internal/harness/state"
)

func TestAdminOpsEvaluationRoutes(t *testing.T) {
	ctx := context.Background()
	runtime := testRuntime(t)
	runtime.SetJobStore(NewMemoryJobStore())
	skills := NewMemorySkillExecutionStore()
	runtime.SetSkillExecutionStore(skills)
	usage := NewMemoryLLMUsageStore()
	risk := NewMemoryRiskStore()
	evaluations := NewMemoryEvaluationStore()

	session, err := runtime.CreateSession(ctx, "alice", "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	session.AddUserMessage("build report")
	session.AddAssistantMessage("report ready")
	if err := runtime.sessions.Save(ctx, "alice", session); err != nil {
		t.Fatalf("save session: %v", err)
	}

	succeeded, err := runtime.CreateJob(ctx, ChatRequest{UserID: "alice", SessionID: session.ID, Content: "build report"}, "chat")
	if err != nil {
		t.Fatalf("create succeeded job: %v", err)
	}
	failed, err := runtime.CreateJob(ctx, ChatRequest{UserID: "alice", SessionID: session.ID, Content: "broken report"}, "chat")
	if err != nil {
		t.Fatalf("create failed job: %v", err)
	}
	started := time.Now().UTC().Add(-2 * time.Second)
	finished := time.Now().UTC()
	for _, job := range []*Job{succeeded, failed} {
		if err := runtime.jobs.UpdateJobStatus(ctx, job.UserID, job.ID, JobStatusRunning, "", started); err != nil {
			t.Fatalf("mark running: %v", err)
		}
	}
	if err := runtime.jobs.UpdateJobStatus(ctx, succeeded.UserID, succeeded.ID, JobStatusSucceeded, "", finished); err != nil {
		t.Fatalf("mark succeeded: %v", err)
	}
	if err := runtime.jobs.UpdateJobStatus(ctx, failed.UserID, failed.ID, JobStatusFailed, "tool failed", finished); err != nil {
		t.Fatalf("mark failed: %v", err)
	}
	if err := runtime.jobs.AddJobEvent(ctx, &JobEvent{
		ID:        NewJobEventID(),
		JobID:     failed.ID,
		UserID:    "alice",
		SessionID: session.ID,
		Type:      "error",
		Event:     Event{Type: "error", Role: state.MessageRoleTool, Error: "tool failed"},
		CreatedAt: finished,
	}); err != nil {
		t.Fatalf("add failed event: %v", err)
	}
	if err := skills.RecordSkillExecution(ctx, SkillExecutionRecord{
		SkillName:   "docx",
		UserID:      "alice",
		SessionID:   session.ID,
		JobID:       failed.ID,
		Status:      SkillExecutionStatusFailed,
		Error:       "tool failed",
		StartedAt:   started,
		CompletedAt: finished,
	}); err != nil {
		t.Fatalf("record skill execution: %v", err)
	}
	if err := usage.RecordLLMUsage(ctx, LLMUsageRecord{
		UserID:           "alice",
		SessionID:        session.ID,
		Provider:         "openai",
		Model:            "gpt-test",
		Status:           "success",
		InputTokens:      10,
		OutputTokens:     20,
		TotalTokens:      30,
		EstimatedCostUSD: 0.001,
		CreatedAt:        finished,
	}); err != nil {
		t.Fatalf("record usage: %v", err)
	}
	if err := risk.RecordRiskEvent(ctx, RiskEvent{
		UserID:    "alice",
		SessionID: session.ID,
		JobID:     failed.ID,
		Operation: RiskOperationJobCreate,
		Reason:    "job failed",
		RiskLevel: RiskLevelHigh,
		CreatedAt: finished,
	}); err != nil {
		t.Fatalf("record risk: %v", err)
	}

	server := NewServer(runtime, HeaderAuthenticator{UserHeader: "X-User-ID"}, NoopRateLimiter{}, nil)
	server.SetAdminToken("secret")
	server.SetLLMUsageStore(usage)
	server.SetRiskStore(risk)
	server.SetEvaluationStore(evaluations)

	body := `{"name":"real-data-eval","trigger":"manual","scope":{"subject_type":"job","user_id":"alice","session_id":"` + session.ID + `"}}`
	createReq := httptest.NewRequest(http.MethodPost, "/v1/admin/ops/eval/runs", bytes.NewBufferString(body))
	createReq.Header.Set("X-User-ID", "admin")
	createReq.Header.Set("X-Admin-Token", "secret")
	createRec := httptest.NewRecorder()
	server.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create eval status = %d body=%s", createRec.Code, createRec.Body.String())
	}
	var created struct {
		Run     EvaluationRun        `json:"run"`
		Results []EvaluationResult   `json:"results"`
		Reviews []EvaluationReview   `json:"reviews"`
		Summary EvaluationRunSummary `json:"summary"`
	}
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if created.Run.Total != 2 || created.Run.Failed != 1 || created.Run.Passed != 1 {
		t.Fatalf("unexpected eval counters: %+v", created.Run)
	}
	if created.Run.ThresholdStatus != "" {
		t.Fatalf("threshold status = %q, want empty", created.Run.ThresholdStatus)
	}
	if len(created.Reviews) != 1 {
		t.Fatalf("review count = %d, want 1", len(created.Reviews))
	}

	for _, req := range []*http.Request{
		httptest.NewRequest(http.MethodGet, "/v1/admin/ops/eval/runs", nil),
		httptest.NewRequest(http.MethodGet, "/v1/admin/ops/eval/runs/"+created.Run.ID, nil),
		httptest.NewRequest(http.MethodGet, "/v1/admin/ops/eval/results?run_id="+created.Run.ID+"&user_id=alice&status=failed", nil),
		httptest.NewRequest(http.MethodGet, "/v1/admin/ops/eval/summary", nil),
	} {
		req.Header.Set("X-User-ID", "admin")
		req.Header.Set("X-Admin-Token", "secret")
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d body=%s", req.URL.Path, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), created.Run.ID) && req.URL.Path != "/v1/admin/ops/eval/summary" {
			t.Fatalf("%s missing run id in %s", req.URL.Path, rec.Body.String())
		}
	}

	csvReq := httptest.NewRequest(http.MethodGet, "/v1/admin/ops/eval/results?run_id="+created.Run.ID+"&status=failed&format=csv", nil)
	csvReq.Header.Set("X-User-ID", "admin")
	csvReq.Header.Set("X-Admin-Token", "secret")
	csvRec := httptest.NewRecorder()
	server.ServeHTTP(csvRec, csvReq)
	if csvRec.Code != http.StatusOK {
		t.Fatalf("csv export status = %d body=%s", csvRec.Code, csvRec.Body.String())
	}
	if contentType := csvRec.Header().Get("Content-Type"); !strings.Contains(contentType, "text/csv") {
		t.Fatalf("csv content type = %q", contentType)
	}
	if body := csvRec.Body.String(); !strings.Contains(body, "job_failed") || !strings.Contains(body, failed.ID) {
		t.Fatalf("csv export missing failed details: %s", body)
	}

	markdownReq := httptest.NewRequest(http.MethodGet, "/v1/admin/ops/eval/summary?format=markdown", nil)
	markdownReq.Header.Set("X-User-ID", "admin")
	markdownReq.Header.Set("X-Admin-Token", "secret")
	markdownRec := httptest.NewRecorder()
	server.ServeHTTP(markdownRec, markdownReq)
	if markdownRec.Code != http.StatusOK {
		t.Fatalf("markdown export status = %d body=%s", markdownRec.Code, markdownRec.Body.String())
	}
	if contentType := markdownRec.Header().Get("Content-Type"); !strings.Contains(contentType, "text/markdown") {
		t.Fatalf("markdown content type = %q", contentType)
	}
	if body := markdownRec.Body.String(); !strings.Contains(body, "# Agent Evaluation Summary") || !strings.Contains(body, created.Run.ID) {
		t.Fatalf("markdown export missing summary details: %s", body)
	}

	patchReq := httptest.NewRequest(http.MethodPatch, "/v1/admin/ops/eval/reviews/"+created.Reviews[0].ID, bytes.NewBufferString(`{"status":"passed","note":"checked"}`))
	patchReq.Header.Set("X-User-ID", "admin")
	patchReq.Header.Set("X-Admin-Token", "secret")
	patchRec := httptest.NewRecorder()
	server.ServeHTTP(patchRec, patchReq)
	if patchRec.Code != http.StatusOK || !strings.Contains(patchRec.Body.String(), `"status":"passed"`) {
		t.Fatalf("patch review status = %d body=%s", patchRec.Code, patchRec.Body.String())
	}
}

func TestAdminOpsEvaluationRequiresAdminToken(t *testing.T) {
	server := NewServer(testRuntime(t), HeaderAuthenticator{UserHeader: "X-User-ID"}, NoopRateLimiter{}, nil)
	server.SetAdminToken("secret")
	server.SetEvaluationStore(NewMemoryEvaluationStore())

	req := httptest.NewRequest(http.MethodGet, "/v1/admin/ops/eval/runs", nil)
	req.Header.Set("X-User-ID", "admin")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}
