package agentruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLoopDiscoveryManualCreatesJobAndDedupes(t *testing.T) {
	ctx := context.Background()
	runtime := testRuntime(t)
	runtime.SetJobStore(NewMemoryJobStore())
	queue := &captureJobQueue{}
	runtime.SetJobQueue(queue)
	session, err := runtime.CreateSession(ctx, "alice", "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	event := LoopDiscoveryEvent{
		UserID:      "alice",
		SessionID:   session.ID,
		TriggerType: LoopDiscoveryManual,
		Source:      "admin_ops",
		DedupeKey:   "manual:test",
		Objective:   "ship the loop trigger panel",
	}
	first, err := runtime.SubmitLoopDiscoveryEvent(ctx, event)
	if err != nil {
		t.Fatalf("submit discovery: %v", err)
	}
	if first.Duplicate || first.Job == nil || first.Trigger.JobID != first.Job.ID || first.Trigger.LoopGoalID == "" {
		t.Fatalf("unexpected first result: %#v", first)
	}
	second, err := runtime.SubmitLoopDiscoveryEvent(ctx, event)
	if err != nil {
		t.Fatalf("submit duplicate discovery: %v", err)
	}
	if !second.Duplicate || second.Trigger.JobID != first.Job.ID {
		t.Fatalf("duplicate should reuse first trigger/job: first=%#v second=%#v", first, second)
	}
	jobs, err := runtime.ListJobs(ctx, "alice", session.ID)
	if err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	if len(jobs) != 1 || len(queue.items) != 1 {
		t.Fatalf("duplicate created extra work: jobs=%d queue=%d", len(jobs), len(queue.items))
	}
}

func TestLoopDiscoveryAutomationKillSwitchBlocksSchedule(t *testing.T) {
	ctx := context.Background()
	runtime := testRuntime(t)
	runtime.SetJobStore(NewMemoryJobStore())

	_, err := runtime.SubmitLoopDiscoveryEvent(ctx, LoopDiscoveryEvent{
		UserID:      "alice",
		TriggerType: LoopDiscoverySchedule,
		Source:      "daily",
		Objective:   "run scheduled report",
	})
	if err == nil {
		t.Fatal("schedule trigger should be blocked while automation kill switch is disabled")
	}
}

func TestLoopDiscoveryScheduleAndWebhookCreateJobsWhenEnabled(t *testing.T) {
	ctx := context.Background()
	runtime := testRuntime(t)
	runtime.config.LoopDiscovery = LoopDiscoveryConfig{
		AutomationEnabled:       true,
		ScheduleTriggersEnabled: true,
		WebhookTriggersEnabled:  true,
	}
	runtime.SetJobStore(NewMemoryJobStore())
	queue := &captureJobQueue{}
	runtime.SetJobQueue(queue)

	for _, triggerType := range []string{LoopDiscoverySchedule, LoopDiscoveryWebhook} {
		result, err := runtime.SubmitLoopDiscoveryEvent(ctx, LoopDiscoveryEvent{
			UserID:      "alice",
			TriggerType: triggerType,
			Source:      triggerType + "_test",
			DedupeKey:   triggerType + ":test",
			Objective:   "handle " + triggerType,
		})
		if err != nil {
			t.Fatalf("%s discovery: %v", triggerType, err)
		}
		if result.Job == nil || result.Trigger.SessionID == "" || result.Trigger.JobID == "" {
			t.Fatalf("%s did not create job/session: %#v", triggerType, result)
		}
	}
	if len(queue.items) != 2 {
		t.Fatalf("queued jobs = %d, want 2", len(queue.items))
	}
}

func TestLoopDiscoveryHTTPRoute(t *testing.T) {
	runtime := testRuntime(t)
	runtime.SetJobStore(NewMemoryJobStore())
	runtime.SetJobQueue(&captureJobQueue{})
	session, err := runtime.CreateSession(context.Background(), "alice", "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	server := NewServer(runtime, HeaderAuthenticator{UserHeader: "X-User-ID"}, NoopRateLimiter{}, nil)
	body, _ := json.Marshal(map[string]any{
		"session_id":   session.ID,
		"trigger_type": "manual",
		"source":       "http_test",
		"dedupe_key":   "manual:http",
		"objective":    "create a loop job from http",
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/loop/discovery", bytes.NewReader(body))
	req.Header.Set("X-User-ID", "alice")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var result LoopDiscoveryResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if result.Trigger.JobID == "" || result.Job == nil {
		t.Fatalf("missing job in response: %#v", result)
	}
}
