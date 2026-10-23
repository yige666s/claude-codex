package agentruntime

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestExplicitMultiAgentRouting(t *testing.T) {
	runtime := NewRuntime(RuntimeConfig{}, nil, nil, nil, nil)
	runtime.SetJobStore(NewMemoryJobStore())
	tests := []struct {
		content string
		mode    string
		want    string
	}{
		{content: "请使用多智能体分头调查 A 和 B", want: JobTypeMultiAgent},
		{content: "并行处理这两个独立任务", want: JobTypeMultiAgent},
		{content: "Use multiple agents to compare the two APIs", want: JobTypeMultiAgent},
		{content: "什么是多智能体？"},
		{content: "不要并行处理，请自己完成"},
		{content: "不要使用多智能体，请自己完成"},
		{content: "不要使用多智能体，请自己完成", mode: AgentModeMultiAgent},
		{content: "解释一下并行处理的概念"},
		{content: "解释一下这个概念"},
		{content: "让多个 Agent 调查", mode: AgentModePlanExecute, want: JobTypeDeepAgent},
		{content: "compare these APIs", mode: AgentModeMultiAgent, want: JobTypeMultiAgent},
	}
	for _, tt := range tests {
		t.Run(tt.content+tt.mode, func(t *testing.T) {
			decision := runtime.RouteChat(ChatRequest{Content: tt.content, AgentMode: tt.mode})
			if decision.JobType != tt.want || decision.RunAsJob != (tt.want != "") {
				t.Fatalf("decision = %#v; want job type %q", decision, tt.want)
			}
		})
	}
}

func TestMultiAgentJobUsesChatCoordinatorAndPersistsEvents(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	runner := &promptMetadataCaptureRunner{}
	runtime := NewRuntime(RuntimeConfig{DefaultWorkingDir: root, TurnTimeout: time.Minute}, NewFileSessionStore(root), nil, nil, func(Scope) Runner { return runner })
	runtime.SetJobStore(NewMemoryJobStore())
	session, err := runtime.CreateSession(ctx, "alice", "")
	if err != nil {
		t.Fatal(err)
	}
	job, err := runtime.CreateJob(ctx, ChatRequest{UserID: "alice", SessionID: session.ID, Content: "请使用多智能体分头调查 A 和 B"}, JobTypeMultiAgent)
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.RunQueuedJob(ctx, JobQueueItem{JobID: job.ID, UserID: "alice"}); err != nil {
		t.Fatal(err)
	}
	loaded, err := runtime.GetJob(ctx, "alice", job.ID)
	if err != nil || loaded.Status != JobStatusSucceeded {
		t.Fatalf("job = %#v, err = %v", loaded, err)
	}
	if !messagesContainRuntimeContext(runner.sessionMessages, "<multi-agent-coordinator>") {
		t.Fatalf("coordinator instruction missing from runner session: %#v", runner.sessionMessages)
	}
	if !messagesContainRuntimeContext(runner.sessionMessages, "<consumer-security>") {
		t.Fatal("multi-agent mode must preserve consumer security policy")
	}
	events, err := runtime.ListJobEvents(ctx, "alice", job.ID, "", 100)
	if err != nil {
		t.Fatal(err)
	}
	var started, completed bool
	for _, event := range events {
		started = started || event.Type == "multi_agent_started"
		completed = completed || event.Type == "multi_agent_completed"
	}
	if !started || !completed {
		t.Fatalf("missing multi-agent lifecycle events: %#v", events)
	}
	persisted, err := runtime.GetSession(ctx, "alice", session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if messagesContainRuntimeContext(persisted.Messages, "<multi-agent-coordinator>") {
		t.Fatal("transient coordinator prompt must not persist in session")
	}
}

func TestMultiAgentWorkerEventsOnlyInMultiAgentJob(t *testing.T) {
	var got []Event
	emit := func(_ context.Context, event Event) error { got = append(got, event); return nil }
	ctx := withJobEventEmitter(WithJobID(context.Background(), "job-1"), emit)
	EmitMultiAgentWorkerEvent(ctx, "parent", "child", "Inspect API", "started")
	if len(got) != 0 {
		t.Fatal("ordinary jobs must not emit multi-agent worker events")
	}
	EmitMultiAgentWorkerEvent(withMultiAgentRun(ctx), "parent", "child", "Inspect API", "started")
	if len(got) != 1 || got[0].Type != "multi_agent_worker_started" || !strings.Contains(string(got[0].Data), `"child_session_id":"child"`) {
		t.Fatalf("worker event = %#v", got)
	}
}

func TestMultiAgentWorkerLimits(t *testing.T) {
	ctx := withMultiAgentRun(WithJobID(context.Background(), "job-1"))
	for _, id := range []string{"a", "b", "c"} {
		if err := BeginMultiAgentWorker(ctx, "parent", id, id); err != nil {
			t.Fatal(err)
		}
	}
	if err := BeginMultiAgentWorker(ctx, "parent", "d", "d"); err == nil {
		t.Fatal("fourth concurrent worker should be denied")
	}
	EmitMultiAgentWorkerEvent(ctx, "parent", "a", "a", "succeeded")
	for _, id := range []string{"d", "e", "f"} {
		if err := BeginMultiAgentWorker(ctx, "parent", id, id); err != nil && id == "d" {
			t.Fatal(err)
		}
		if id == "d" {
			EmitMultiAgentWorkerEvent(ctx, "parent", "b", "b", "succeeded")
		}
		if id == "e" {
			EmitMultiAgentWorkerEvent(ctx, "parent", "c", "c", "succeeded")
		}
	}
	EmitMultiAgentWorkerEvent(ctx, "parent", "d", "d", "succeeded")
	if err := BeginMultiAgentWorker(ctx, "parent", "g", "g"); err == nil {
		t.Fatal("seventh worker should be denied")
	}
}

// The HTTP transport reserves the chat turn before Runtime.Chat hands it to a
// durable job. The worker must adopt that reservation rather than reserve a
// second turn for the same session.
func TestMultiAgentJobAdoptsHandedOffChatTurn(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	runner := &promptMetadataCaptureRunner{}
	runtime := NewRuntime(RuntimeConfig{DefaultWorkingDir: root, TurnTimeout: time.Minute}, NewFileSessionStore(root), nil, nil, func(Scope) Runner { return runner })
	jobs := NewMemoryJobStore()
	reservations := NewMemoryRuntimeOutputStore()
	queue := &captureJobQueue{}
	runtime.SetJobStore(jobs)
	runtime.SetChatTurnReservationStore(reservations)
	runtime.SetJobQueue(queue)
	session, err := runtime.CreateSession(ctx, "alice", "")
	if err != nil {
		t.Fatal(err)
	}
	original, err := reservations.ReserveChatTurn(ctx, ChatTurnReservation{
		UserID: "alice", SessionID: session.ID,
		IdempotencyKey: "chat-request", RunID: "chat-run-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	sink := &collectSink{}
	if err := runtime.Chat(ctx, ChatRequest{
		UserID: "alice", SessionID: session.ID,
		IdempotencyKey: original.IdempotencyKey, RunID: original.RunID,
		TurnReserved: true, Content: "请使用多智能体分头调查 A 和 B",
	}, sink); err != nil {
		t.Fatalf("chat handoff: %v", err)
	}
	if len(queue.items) != 1 || !sink.hasEvent("job_handoff") {
		t.Fatalf("expected one handed-off job, queue=%#v events=%#v", queue.items, sink.events)
	}
	item := queue.items[0]
	if err := runtime.RunQueuedJob(ctx, item); err != nil {
		t.Fatalf("run queued job: %v", err)
	}
	job, err := runtime.GetJob(ctx, "alice", item.JobID)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != JobStatusSucceeded {
		t.Fatalf("handed-off multi-agent job status=%s error=%q", job.Status, job.Error)
	}
	adopted, err := reservations.ReserveChatTurn(ctx, ChatTurnReservation{
		UserID: "alice", SessionID: session.ID,
		IdempotencyKey: "job:" + job.ID, RunID: job.ID,
	})
	if err != nil || adopted.Reserved || adopted.RunID != job.ID || adopted.Status != "succeeded" {
		t.Fatalf("completed job reservation = %#v, err = %v", adopted, err)
	}
	events, err := runtime.ListJobEvents(ctx, "alice", item.JobID, "", 100)
	if err != nil {
		t.Fatal(err)
	}
	var completed bool
	for _, event := range events {
		completed = completed || event.Type == "multi_agent_completed"
	}
	if !completed {
		t.Fatalf("missing multi-agent completion event: %#v", events)
	}
}
