package agentruntime

import (
	"context"
	"errors"
	"os"
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
