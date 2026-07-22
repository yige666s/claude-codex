package agentruntime

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"claude-codex/internal/harness/engine"
	"claude-codex/internal/harness/state"
)

func TestRuntimeStartJobEnqueuesRedisWorkItem(t *testing.T) {
	runtime := testRuntime(t)
	runtime.SetJobStore(NewMemoryJobStore())
	queue := &captureJobQueue{}
	runtime.SetJobQueue(queue)

	session, err := runtime.CreateSession(context.Background(), "alice", "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	job, err := runtime.CreateJob(context.Background(), ChatRequest{UserID: "alice", SessionID: session.ID, Content: "/docx write report"}, "skill")
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	runtime.markJobUserMessageHidden(job.ID)

	ctx := withRequestID(context.Background(), "req-123")
	if err := runtime.StartJob(ctx, job); err != nil {
		t.Fatalf("start job: %v", err)
	}
	if len(queue.items) != 1 {
		t.Fatalf("queued items = %d, want 1", len(queue.items))
	}
	item := queue.items[0]
	if item.JobID != job.ID || item.UserID != "alice" || item.RequestID != "req-123" || !item.HideUserMessage {
		t.Fatalf("unexpected queued item: %#v", item)
	}
	loaded, err := runtime.GetJob(context.Background(), "alice", job.ID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if loaded.Status != JobStatusQueued {
		t.Fatalf("queued job status = %s, want queued", loaded.Status)
	}
}

func TestJobWorkerRunsQueuedJob(t *testing.T) {
	runtime := testRuntime(t)
	runtime.SetJobStore(NewMemoryJobStore())
	queue := &captureJobQueue{}

	session, err := runtime.CreateSession(context.Background(), "alice", "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	job, err := runtime.CreateJob(context.Background(), ChatRequest{UserID: "alice", SessionID: session.ID, Content: "hello"}, "chat")
	if err != nil {
		t.Fatalf("create job: %v", err)
	}

	worker := NewJobWorker(queue, runtime, JobWorkerConfig{LockTTL: time.Second}, nil)
	ack, err := worker.process(context.Background(), &JobQueueMessage{ID: "1-0", Item: JobQueueItem{JobID: job.ID, UserID: "alice"}})
	if err != nil {
		t.Fatalf("process job: %v", err)
	}
	if !ack {
		t.Fatal("expected worker to ack processed job")
	}
	loaded, err := runtime.GetJob(context.Background(), "alice", job.ID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if loaded.Status != JobStatusSucceeded {
		t.Fatalf("job status = %s, want succeeded", loaded.Status)
	}
	events, err := runtime.ListJobEvents(context.Background(), "alice", job.ID, "", 100)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("expected persisted job events")
	}
}

func TestJobWorkerRunsDeepAgentJob(t *testing.T) {
	runtime := NewRuntime(
		RuntimeConfig{TurnTimeout: time.Minute},
		NewFileSessionStore(t.TempDir()),
		nil,
		nil,
		func(Scope) Runner { return deepAgentPlanJSONRunner{} },
	)
	runtime.SetJobStore(NewMemoryJobStore())
	workflowStore := NewMemoryWorkflowStore()
	runtime.SetWorkflowStore(workflowStore)

	session, err := runtime.CreateSession(context.Background(), "alice", "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	session.AddUserMessage("postgres timeout happened yesterday")
	if err := runtime.sessions.Save(context.Background(), "alice", session); err != nil {
		t.Fatalf("save seed session: %v", err)
	}
	job, err := runtime.CreateJob(context.Background(), ChatRequest{
		UserID:    "alice",
		SessionID: session.ID,
		Content:   "summarize previous postgres issue",
	}, JobTypeDeepAgent)
	if err != nil {
		t.Fatalf("create job: %v", err)
	}

	if err := runtime.RunQueuedJob(context.Background(), JobQueueItem{JobID: job.ID, UserID: "alice"}); err != nil {
		t.Fatalf("run deep agent job: %v", err)
	}
	loaded, err := runtime.GetJob(context.Background(), "alice", job.ID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if loaded.Status != JobStatusSucceeded {
		t.Fatalf("job status = %s, want succeeded: %#v", loaded.Status, loaded)
	}
	runs, err := workflowStore.ListWorkflowRuns(context.Background(), WorkflowRunFilter{Name: deepAgentTaskWorkflowName, JobID: job.ID})
	if err != nil {
		t.Fatalf("list workflow runs: %v", err)
	}
	if len(runs) != 1 || runs[0].Status != WorkflowStatusSucceeded {
		t.Fatalf("expected one succeeded deep agent workflow, got %#v", runs)
	}
	events, err := runtime.ListJobEvents(context.Background(), "alice", job.ID, "", 100)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	var sawStart, sawWorkflow, sawDone bool
	for _, event := range events {
		switch event.Type {
		case "deep_agent_started":
			sawStart = true
		case "done":
			sawDone = true
		}
		if strings.HasPrefix(event.Type, "workflow_") {
			sawWorkflow = true
		}
	}
	if !sawStart || !sawWorkflow || !sawDone {
		t.Fatalf("missing deep agent job events start=%t workflow=%t done=%t events=%#v", sawStart, sawWorkflow, sawDone, events)
	}
	updated, err := runtime.GetSession(context.Background(), "alice", session.ID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	var final string
	for _, message := range updated.Messages {
		if message.Role == "assistant" && strings.Contains(message.Content, "计划执行完成") {
			final = message.Content
		}
	}
	if !strings.Contains(final, runs[0].ID) || !strings.Contains(final, "Search relevant history") {
		t.Fatalf("unexpected final deep agent message: %q", final)
	}
}

func TestJobWorkerAcksAlreadyCancelledJob(t *testing.T) {
	runtime := testRuntime(t)
	runtime.SetJobStore(NewMemoryJobStore())
	queue := &captureJobQueue{}

	session, err := runtime.CreateSession(context.Background(), "alice", "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	job, err := runtime.CreateJob(context.Background(), ChatRequest{UserID: "alice", SessionID: session.ID, Content: "hello"}, "chat")
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	if err := runtime.jobs.UpdateJobStatus(context.Background(), "alice", job.ID, JobStatusCancelled, "cancelled before execution", time.Now().UTC()); err != nil {
		t.Fatalf("cancel job: %v", err)
	}

	worker := NewJobWorker(queue, runtime, JobWorkerConfig{}, nil)
	ack, err := worker.process(context.Background(), &JobQueueMessage{ID: "1-0", Item: JobQueueItem{JobID: job.ID, UserID: "alice"}})
	if err != nil {
		t.Fatalf("process cancelled job: %v", err)
	}
	if !ack {
		t.Fatal("expected worker to ack terminal job")
	}
	loaded, err := runtime.GetJob(context.Background(), "alice", job.ID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if loaded.Status != JobStatusCancelled {
		t.Fatalf("job status = %s, want cancelled", loaded.Status)
	}
}

func TestRuntimeCancelsJobRunningOnAnotherRuntimeInstance(t *testing.T) {
	root := t.TempDir()
	jobs := NewMemoryJobStore()
	started := make(chan struct{})
	apiRuntime := NewRuntime(
		RuntimeConfig{DefaultWorkingDir: root, TurnTimeout: time.Minute},
		NewFileSessionStore(root),
		NewFileMemoryService(root),
		nil,
		nil,
	)
	workerRuntime := NewRuntime(
		RuntimeConfig{DefaultWorkingDir: root, TurnTimeout: time.Minute},
		NewFileSessionStore(root),
		NewFileMemoryService(root),
		nil,
		func(Scope) Runner { return blockingRunner{started: started} },
	)
	apiRuntime.SetJobStore(jobs)
	workerRuntime.SetJobStore(jobs)

	session, err := apiRuntime.CreateSession(context.Background(), "alice", "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	job, err := apiRuntime.CreateJob(context.Background(), ChatRequest{UserID: "alice", SessionID: session.ID, Content: "wait"}, "chat")
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		done <- workerRuntime.RunQueuedJob(context.Background(), JobQueueItem{JobID: job.ID, UserID: "alice"})
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("remote worker did not start")
	}

	if err := apiRuntime.CancelJob(context.Background(), "alice", job.ID); err != nil {
		t.Fatalf("cancel job from api runtime: %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("remote worker should acknowledge durable cancellation: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("remote worker did not observe durable cancellation")
	}

	loaded, err := jobs.GetJob(context.Background(), "alice", job.ID)
	if err != nil {
		t.Fatalf("load cancelled job: %v", err)
	}
	if loaded.Status != JobStatusCancelled {
		t.Fatalf("job status = %s, want cancelled", loaded.Status)
	}
	events, err := jobs.ListJobEvents(context.Background(), "alice", job.ID, "", 100)
	if err != nil {
		t.Fatalf("list cancelled job events: %v", err)
	}
	terminalCount := 0
	for _, event := range events {
		if event.Type == "done" || event.Type == "error" || event.Type == "cancelled" {
			terminalCount++
			if event.Type != "cancelled" {
				t.Fatalf("unexpected terminal event after cancellation: %#v", event)
			}
		}
	}
	if terminalCount != 1 {
		t.Fatalf("terminal event count = %d, want one authoritative cancelled event: %#v", terminalCount, events)
	}
}

func TestRunJobPersistsTerminalStatusBeforeTerminalEvent(t *testing.T) {
	store := &terminalStatusCheckingJobStore{MemoryJobStore: NewMemoryJobStore()}
	runtime := testRuntime(t)
	runtime.SetJobStore(store)
	session, err := runtime.CreateSession(context.Background(), "alice", "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	job, err := runtime.CreateJob(context.Background(), ChatRequest{UserID: "alice", SessionID: session.ID, Content: "hello"}, "chat")
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	if err := runtime.RunQueuedJob(context.Background(), JobQueueItem{JobID: job.ID, UserID: "alice"}); err != nil {
		t.Fatalf("run job: %v", err)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.terminalStatuses) != 1 || store.terminalStatuses[0] != JobStatusSucceeded {
		t.Fatalf("terminal events observed statuses = %#v, want [succeeded]", store.terminalStatuses)
	}
}

func TestMemoryJobStoreCompareAndSetStatus(t *testing.T) {
	store := NewMemoryJobStore()
	now := time.Now().UTC()
	job := &Job{ID: "job-cas", UserID: "alice", SessionID: "session-1", Type: "chat", Status: JobStatusQueued, CreatedAt: now, UpdatedAt: now}
	if err := store.CreateJob(context.Background(), job); err != nil {
		t.Fatalf("create job: %v", err)
	}
	updated, err := store.TransitionJobStatus(context.Background(), "alice", job.ID, JobStatusQueued, JobStatusRunning, "", now)
	if err != nil || !updated {
		t.Fatalf("queued -> running transition = %t, %v", updated, err)
	}
	updated, err = store.TransitionJobStatus(context.Background(), "alice", job.ID, JobStatusQueued, JobStatusCancelled, "", now)
	if err != nil {
		t.Fatalf("stale transition: %v", err)
	}
	if updated {
		t.Fatal("stale compare-and-set unexpectedly updated the job")
	}
	loaded, err := store.GetJob(context.Background(), "alice", job.ID)
	if err != nil {
		t.Fatalf("load job: %v", err)
	}
	if loaded.Status != JobStatusRunning {
		t.Fatalf("status = %s, want running", loaded.Status)
	}
}

func TestMemoryJobExecutionLeaseFencesTakeoverAndTerminal(t *testing.T) {
	store := NewMemoryJobStore()
	now := time.Now().UTC()
	job := &Job{ID: "job-lease", UserID: "alice", SessionID: "session-1", Type: "chat", Status: JobStatusQueued, CreatedAt: now, UpdatedAt: now}
	if err := store.CreateJob(context.Background(), job); err != nil {
		t.Fatalf("create job: %v", err)
	}
	acquired, err := store.AcquireJobExecutionLease(context.Background(), "alice", job.ID, "owner-1", now, now.Add(time.Minute))
	if err != nil || !acquired {
		t.Fatalf("first acquire = %t, %v", acquired, err)
	}
	acquired, err = store.AcquireJobExecutionLease(context.Background(), "alice", job.ID, "owner-2", now.Add(time.Second), now.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("competing acquire: %v", err)
	}
	if acquired {
		t.Fatal("competing owner acquired an unexpired execution lease")
	}
	transitioned, err := store.TransitionOwnedJobStatus(context.Background(), "alice", job.ID, "owner-2", JobStatusSucceeded, "", now.Add(2*time.Second))
	if err != nil {
		t.Fatalf("stale owner transition: %v", err)
	}
	if transitioned {
		t.Fatal("non-owner published terminal status")
	}
	if err := store.ReleaseJobExecutionLease(context.Background(), "alice", job.ID, "owner-1", now.Add(3*time.Second)); err != nil {
		t.Fatalf("release first owner: %v", err)
	}
	acquired, err = store.AcquireJobExecutionLease(context.Background(), "alice", job.ID, "owner-2", now.Add(4*time.Second), now.Add(2*time.Minute))
	if err != nil || !acquired {
		t.Fatalf("takeover after release = %t, %v", acquired, err)
	}
	transitioned, err = store.TransitionOwnedJobStatus(context.Background(), "alice", job.ID, "owner-1", JobStatusFailed, "stale", now.Add(5*time.Second))
	if err != nil {
		t.Fatalf("old owner transition: %v", err)
	}
	if transitioned {
		t.Fatal("old owner crossed the takeover fence")
	}
	transitioned, err = store.TransitionOwnedJobStatus(context.Background(), "alice", job.ID, "owner-2", JobStatusSucceeded, "", now.Add(5*time.Second))
	if err != nil || !transitioned {
		t.Fatalf("active owner terminal transition = %t, %v", transitioned, err)
	}
	loaded, err := store.GetJob(context.Background(), "alice", job.ID)
	if err != nil {
		t.Fatalf("load job: %v", err)
	}
	if loaded.Status != JobStatusSucceeded || loaded.ExecutionEpoch != 2 || loaded.ExecutionOwner != "" || loaded.ExecutionLeaseExpiresAt != nil {
		t.Fatalf("unexpected terminal lease state: %#v", loaded)
	}
}

func TestDuplicateQueuedJobDeliveryDoesNotExecuteBodyTwice(t *testing.T) {
	root := t.TempDir()
	jobs := NewMemoryJobStore()
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	secondStarted := make(chan struct{})
	first := NewRuntime(
		RuntimeConfig{DefaultWorkingDir: root, TurnTimeout: time.Minute},
		NewFileSessionStore(root), NewFileMemoryService(root), nil,
		func(Scope) Runner { return &releasableJobRunner{started: firstStarted, release: releaseFirst} },
	)
	second := NewRuntime(
		RuntimeConfig{DefaultWorkingDir: root, TurnTimeout: time.Minute},
		NewFileSessionStore(root), NewFileMemoryService(root), nil,
		func(Scope) Runner { return &releasableJobRunner{started: secondStarted, release: make(chan struct{})} },
	)
	first.SetJobStore(jobs)
	second.SetJobStore(jobs)
	session, err := first.CreateSession(context.Background(), "alice", "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	job, err := first.CreateJob(context.Background(), ChatRequest{UserID: "alice", SessionID: session.ID, Content: "once"}, "chat")
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- first.RunQueuedJob(context.Background(), JobQueueItem{JobID: job.ID, UserID: "alice"})
	}()
	select {
	case <-firstStarted:
	case <-time.After(time.Second):
		t.Fatal("first worker did not start")
	}
	if err := second.RunQueuedJob(context.Background(), JobQueueItem{JobID: job.ID, UserID: "alice"}); err != nil {
		t.Fatalf("duplicate delivery: %v", err)
	}
	select {
	case <-secondStarted:
		t.Fatal("duplicate delivery executed the job body")
	default:
	}
	close(releaseFirst)
	if err := <-firstDone; err != nil {
		t.Fatalf("first worker: %v", err)
	}
}

func TestWorkerWithQueueLeaseCanRecoverRunningJob(t *testing.T) {
	runtime := testRuntime(t)
	jobs := NewMemoryJobStore()
	runtime.SetJobStore(jobs)
	session, err := runtime.CreateSession(context.Background(), "alice", "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	job, err := runtime.CreateJob(context.Background(), ChatRequest{UserID: "alice", SessionID: session.ID, Content: "recover"}, "chat")
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	if err := jobs.UpdateJobStatus(context.Background(), "alice", job.ID, JobStatusRunning, "", time.Now().UTC()); err != nil {
		t.Fatalf("mark running: %v", err)
	}
	if err := runtime.RunQueuedJob(withJobExecutionLease(context.Background()), JobQueueItem{JobID: job.ID, UserID: "alice"}); err != nil {
		t.Fatalf("recover running job: %v", err)
	}
	loaded, err := jobs.GetJob(context.Background(), "alice", job.ID)
	if err != nil {
		t.Fatalf("load recovered job: %v", err)
	}
	if loaded.Status != JobStatusSucceeded {
		t.Fatalf("recovered job status = %s, want succeeded", loaded.Status)
	}
}

func TestJobWorkerStopsWithoutTerminalTransitionWhenQueueLeaseIsLost(t *testing.T) {
	root := t.TempDir()
	started := make(chan struct{})
	runtime := NewRuntime(
		RuntimeConfig{DefaultWorkingDir: root, TurnTimeout: time.Minute},
		NewFileSessionStore(root), NewFileMemoryService(root), nil,
		func(Scope) Runner { return genericErrorOnCancelRunner{started: started} },
	)
	jobs := NewMemoryJobStore()
	runtime.SetJobStore(jobs)
	session, err := runtime.CreateSession(context.Background(), "alice", "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	job, err := runtime.CreateJob(context.Background(), ChatRequest{UserID: "alice", SessionID: session.ID, Content: "wait"}, "chat")
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	queue := &leaseFailingJobQueue{captureJobQueue: &captureJobQueue{}}
	worker := NewJobWorker(queue, runtime, JobWorkerConfig{LockTTL: time.Second, LockRefresh: 5 * time.Millisecond}, nil)
	ack, err := worker.process(context.Background(), &JobQueueMessage{ID: "1-0", Item: JobQueueItem{JobID: job.ID, UserID: "alice"}})
	if ack || !errors.Is(err, ErrJobExecutionLeaseLost) {
		t.Fatalf("process after lease loss = ack %t, err %v", ack, err)
	}
	loaded, loadErr := jobs.GetJob(context.Background(), "alice", job.ID)
	if loadErr != nil {
		t.Fatalf("load job: %v", loadErr)
	}
	if loaded.Status != JobStatusRunning {
		t.Fatalf("lease-lost job status = %s, want recoverable running", loaded.Status)
	}
}

func TestRuntimeShutdownLeavesQueuedJobRecoverable(t *testing.T) {
	root := t.TempDir()
	release := make(chan struct{})
	started := make(chan struct{})
	runner := &releasableJobRunner{started: started, release: release}
	runtime := NewRuntime(
		RuntimeConfig{DefaultWorkingDir: root, TurnTimeout: time.Minute},
		NewFileSessionStore(root),
		NewFileMemoryService(root),
		nil,
		func(Scope) Runner { return runner },
	)
	runtime.SetJobStore(NewMemoryJobStore())
	runtime.SetJobQueue(&captureJobQueue{})

	session, err := runtime.CreateSession(context.Background(), "alice", "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	job, err := runtime.CreateJob(context.Background(), ChatRequest{UserID: "alice", SessionID: session.ID, Content: "slow"}, "chat")
	if err != nil {
		t.Fatalf("create job: %v", err)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- runtime.RunQueuedJob(context.Background(), JobQueueItem{JobID: job.ID, UserID: "alice"})
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("queued job did not start")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := runtime.Shutdown(shutdownCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("shutdown error = %v, want deadline exceeded while job remains claimable", err)
	}
	loaded, err := runtime.GetJob(context.Background(), "alice", job.ID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if loaded.Status != JobStatusRunning {
		t.Fatalf("job status after shutdown timeout = %s, want running", loaded.Status)
	}

	close(release)
	if err := <-errCh; err != nil {
		t.Fatalf("queued job finish: %v", err)
	}
	loaded, err = runtime.GetJob(context.Background(), "alice", job.ID)
	if err != nil {
		t.Fatalf("get finished job: %v", err)
	}
	if loaded.Status != JobStatusSucceeded {
		t.Fatalf("job status after release = %s, want succeeded", loaded.Status)
	}
}

func TestRuntimeShutdownStillCancelsInlineChatWithJobQueue(t *testing.T) {
	root := t.TempDir()
	started := make(chan struct{})
	runtime := NewRuntime(
		RuntimeConfig{DefaultWorkingDir: root, TurnTimeout: time.Minute},
		NewFileSessionStore(root),
		NewFileMemoryService(root),
		nil,
		func(Scope) Runner { return blockingRunner{started: started} },
	)
	runtime.SetJobStore(NewMemoryJobStore())
	runtime.SetJobQueue(&captureJobQueue{})

	session, err := runtime.CreateSession(context.Background(), "alice", "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		done <- runtime.Chat(context.Background(), ChatRequest{UserID: "alice", SessionID: session.ID, Content: "wait"}, &collectSink{})
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("inline chat did not start")
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := runtime.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("shutdown inline chat: %v", err)
	}
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("chat error = %v, want context.Canceled", err)
	}
}

func TestRedisJobQueueClaimsPendingWork(t *testing.T) {
	rawURL := os.Getenv("AGENT_RUNTIME_TEST_REDIS_URL")
	if rawURL == "" {
		t.Skip("set AGENT_RUNTIME_TEST_REDIS_URL to run redis integration test")
	}
	client, err := NewRedisClientFromURL(rawURL)
	if err != nil {
		t.Fatalf("new redis client: %v", err)
	}
	defer client.Close()

	stream := "agentapi:test:jobs:" + newSortableID()
	group := "agentapi-test-workers"
	ctx := context.Background()
	t.Cleanup(func() {
		_ = client.Del(context.Background(), stream, stream+":lock:job-1").Err()
	})

	first := NewRedisJobQueue(client, RedisJobQueueConfig{
		Stream:       stream,
		Group:        group,
		Consumer:     "worker-a",
		BlockTimeout: 10 * time.Millisecond,
		ClaimIdle:    time.Millisecond,
		LockTTL:      time.Second,
	})
	second := NewRedisJobQueue(client, RedisJobQueueConfig{
		Stream:       stream,
		Group:        group,
		Consumer:     "worker-b",
		BlockTimeout: 10 * time.Millisecond,
		ClaimIdle:    time.Millisecond,
		LockTTL:      time.Second,
	})
	if err := first.Ensure(ctx); err != nil {
		t.Fatalf("ensure queue: %v", err)
	}
	if err := first.EnqueueJob(ctx, JobQueueItem{JobID: "job-1", UserID: "alice"}); err != nil {
		t.Fatalf("enqueue job: %v", err)
	}
	message, err := first.ReceiveJob(ctx)
	if err != nil {
		t.Fatalf("receive job: %v", err)
	}
	if message == nil || message.Item.JobID != "job-1" {
		t.Fatalf("unexpected first message: %#v", message)
	}
	time.Sleep(5 * time.Millisecond)
	claimed, err := second.ReceiveJob(ctx)
	if err != nil {
		t.Fatalf("claim job: %v", err)
	}
	if claimed == nil || claimed.ID != message.ID || claimed.Item.JobID != "job-1" {
		t.Fatalf("unexpected claimed message: %#v", claimed)
	}
	if err := second.AckJob(ctx, claimed); err != nil {
		t.Fatalf("ack claimed job: %v", err)
	}
}

type captureJobQueue struct {
	items []JobQueueItem
	acks  []string
}

type terminalStatusCheckingJobStore struct {
	*MemoryJobStore
	mu               sync.Mutex
	terminalStatuses []string
}

type leaseFailingJobQueue struct {
	*captureJobQueue
}

func (q *leaseFailingJobQueue) AcquireJobLock(context.Context, string, time.Duration) (JobQueueLock, bool, error) {
	return leaseFailingJobLock{}, true, nil
}

type leaseFailingJobLock struct{}

func (leaseFailingJobLock) Refresh(context.Context) error { return errors.New("lease expired") }
func (leaseFailingJobLock) Release(context.Context) error { return nil }

type genericErrorOnCancelRunner struct {
	started chan struct{}
}

func (r genericErrorOnCancelRunner) Run(ctx context.Context, session *state.Session, _ string) (engine.Result, error) {
	if r.started != nil {
		select {
		case <-r.started:
		default:
			close(r.started)
		}
	}
	<-ctx.Done()
	return engine.Result{Session: session}, errors.New("engine stopped after cancellation")
}

func (r genericErrorOnCancelRunner) RunGeneratedPrompt(ctx context.Context, session *state.Session, prompt string) (engine.Result, error) {
	return r.Run(ctx, session, prompt)
}

func (r genericErrorOnCancelRunner) RunStream(ctx context.Context, session *state.Session, prompt string, _ func(string)) (engine.Result, error) {
	return r.Run(ctx, session, prompt)
}

func (r genericErrorOnCancelRunner) RunGeneratedPromptStream(ctx context.Context, session *state.Session, prompt string, _ func(string)) (engine.Result, error) {
	return r.Run(ctx, session, prompt)
}

func (s *terminalStatusCheckingJobStore) AddJobEvent(ctx context.Context, event *JobEvent) error {
	if event != nil && (event.Type == "done" || event.Type == "error" || event.Type == "cancelled") {
		job, err := s.MemoryJobStore.GetJob(ctx, event.UserID, event.JobID)
		if err != nil {
			return err
		}
		s.mu.Lock()
		s.terminalStatuses = append(s.terminalStatuses, job.Status)
		s.mu.Unlock()
	}
	return s.MemoryJobStore.AddJobEvent(ctx, event)
}

func (q *captureJobQueue) EnqueueJob(_ context.Context, item JobQueueItem) error {
	q.items = append(q.items, item)
	return nil
}

func (q *captureJobQueue) Ensure(context.Context) error { return nil }

func (q *captureJobQueue) ReceiveJob(context.Context) (*JobQueueMessage, error) { return nil, nil }

func (q *captureJobQueue) AckJob(_ context.Context, message *JobQueueMessage) error {
	if message != nil {
		q.acks = append(q.acks, message.ID)
	}
	return nil
}

func (q *captureJobQueue) Close() error { return nil }

type releasableJobRunner struct {
	started chan struct{}
	release chan struct{}
}

func (r *releasableJobRunner) Run(ctx context.Context, session *state.Session, prompt string) (engine.Result, error) {
	return r.RunGeneratedPrompt(ctx, session, prompt)
}

func (r *releasableJobRunner) RunGeneratedPrompt(ctx context.Context, session *state.Session, prompt string) (engine.Result, error) {
	select {
	case <-r.started:
	default:
		close(r.started)
	}
	select {
	case <-ctx.Done():
		return engine.Result{Session: session}, ctx.Err()
	case <-r.release:
		session.AddUserMessage(prompt)
		session.AddAssistantMessage("assistant: " + prompt)
		return engine.Result{Output: "assistant: " + prompt, Session: session}, nil
	}
}
