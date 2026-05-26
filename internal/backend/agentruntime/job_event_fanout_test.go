package agentruntime

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestJobEventSinkPublishesFanout(t *testing.T) {
	runtime := testRuntime(t)
	runtime.SetJobStore(NewMemoryJobStore())
	publisher := &captureJobEventPublisher{}
	runtime.SetJobEventFanout(publisher)

	session, err := runtime.CreateSession(context.Background(), "alice", "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	job, err := runtime.CreateJob(context.Background(), ChatRequest{UserID: "alice", SessionID: session.ID, Content: "hello"}, "chat")
	if err != nil {
		t.Fatalf("create job: %v", err)
	}
	sink := &jobEventSink{store: runtime.jobs, bus: runtime.jobEvents, fanout: runtime.jobEventFanout, job: job}
	if err := sink.Send(context.Background(), Event{Type: "delta", Content: "hello"}); err != nil {
		t.Fatalf("send event: %v", err)
	}
	if len(publisher.events) != 1 {
		t.Fatalf("published fanout events = %d, want 1", len(publisher.events))
	}
	if publisher.events[0].JobID != job.ID || publisher.events[0].Type != "delta" {
		t.Fatalf("unexpected fanout event: %#v", publisher.events[0])
	}
}

func TestRuntimePublishesRemoteJobEventToLocalSubscribers(t *testing.T) {
	runtime := testRuntime(t)
	updates, unsubscribe := runtime.subscribeJobEvents("job-remote")
	defer unsubscribe()

	record := &JobEvent{
		ID:        NewJobEventID(),
		JobID:     "job-remote",
		UserID:    "alice",
		SessionID: "session-1",
		Type:      "delta",
		Event:     Event{Type: "delta", JobID: "job-remote", Content: "hello"},
		CreatedAt: time.Now().UTC(),
	}
	runtime.PublishRemoteJobEvent(record)
	select {
	case got := <-updates:
		if got.ID != record.ID || got.JobID != record.JobID {
			t.Fatalf("unexpected remote event: %#v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for remote job event")
	}
}

func TestRedisJobEventFanoutPublishesAcrossOrigins(t *testing.T) {
	rawURL := os.Getenv("AGENT_RUNTIME_TEST_REDIS_URL")
	if rawURL == "" {
		t.Skip("set AGENT_RUNTIME_TEST_REDIS_URL to run redis integration test")
	}
	client, err := NewRedisClientFromURL(rawURL)
	if err != nil {
		t.Fatalf("new redis client: %v", err)
	}
	defer client.Close()

	channel := "agentapi:test:job-events:" + newSortableID()
	first := NewRedisJobEventFanout(client, RedisJobEventFanoutConfig{Channel: channel, Origin: "origin-a"}, nil)
	second := NewRedisJobEventFanout(client, RedisJobEventFanoutConfig{Channel: channel, Origin: "origin-b"}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	received := make(chan *JobEvent, 1)
	errCh := make(chan error, 1)
	go func() {
		errCh <- second.Run(ctx, func(event *JobEvent) {
			received <- event
		})
	}()
	time.Sleep(50 * time.Millisecond)

	record := &JobEvent{
		ID:        NewJobEventID(),
		JobID:     "job-1",
		UserID:    "alice",
		SessionID: "session-1",
		Type:      "done",
		Event:     Event{Type: "done", JobID: "job-1"},
		CreatedAt: time.Now().UTC(),
	}
	if err := first.PublishJobEvent(context.Background(), record); err != nil {
		t.Fatalf("publish fanout: %v", err)
	}
	select {
	case got := <-received:
		if got.ID != record.ID || got.JobID != record.JobID {
			t.Fatalf("unexpected redis fanout event: %#v", got)
		}
	case err := <-errCh:
		t.Fatalf("fanout subscriber stopped early: %v", err)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for redis fanout event")
	}
	cancel()
}

type captureJobEventPublisher struct {
	events []*JobEvent
}

func (p *captureJobEventPublisher) PublishJobEvent(_ context.Context, event *JobEvent) error {
	p.events = append(p.events, cloneJobEvent(event))
	return nil
}
