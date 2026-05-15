package agentruntime

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/segmentio/kafka-go"

	"claude-codex/internal/harness/state"
)

func TestKafkaMessageEventConsumerRetriesAndCommitsThroughRun(t *testing.T) {
	event := MessageEvent{
		Type:      MessageEventCreated,
		UserID:    "alice",
		SessionID: "session-1",
		Message:   state.Message{ID: "message-1", UserID: "alice", SessionID: "session-1", Role: state.MessageRoleAssistant, Content: "hello"},
		CreatedAt: time.Now().UTC(),
	}
	data := mustJSON(t, event)
	reader := &fakeKafkaReader{messages: []kafka.Message{{Topic: "agent.messages", Key: []byte("session-1"), Value: data}}}
	handler := &flakyMessageEventHandler{failures: 1}
	worker := NewKafkaMessageEventConsumerWorker(reader, handler, KafkaMessageEventConfig{RetryAttempts: 2, RetryBackoff: time.Millisecond, ProcessTimeout: time.Second})

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- worker.Run(ctx) }()

	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); {
		if reader.commits > 0 {
			cancel()
			err := <-errCh
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("expected context cancellation after commit, got %v", err)
			}
			if handler.calls != 2 {
				t.Fatalf("expected one retry, got %d calls", handler.calls)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	t.Fatal("message was not committed")
}

func TestKafkaMessageEventConsumerSkipsProcessedEvent(t *testing.T) {
	event := MessageEvent{Type: MessageEventCreated, Message: state.Message{ID: "message-1"}}
	worker := NewKafkaMessageEventConsumerWorker(&fakeKafkaReader{}, MessageEventHandlerFunc(func(context.Context, MessageEvent) error {
		t.Fatal("handler should not run for an already processed event")
		return nil
	}), KafkaMessageEventConfig{})
	worker.SetProcessedLock(skipMessageEventLock{})

	if err := worker.processKafkaMessage(context.Background(), kafka.Message{Value: mustJSON(t, event)}); err != nil {
		t.Fatalf("process skipped event: %v", err)
	}
}

func TestKafkaMessageEventConsumerWritesDLQ(t *testing.T) {
	reader := &fakeKafkaReader{}
	dlq := &fakeKafkaWriter{}
	worker := NewKafkaMessageEventConsumerWorker(reader, MessageEventHandlerFunc(func(context.Context, MessageEvent) error {
		return errors.New("boom")
	}), KafkaMessageEventConfig{RetryAttempts: 1})
	worker.SetDLQWriter(dlq)

	err := worker.processKafkaMessage(context.Background(), kafka.Message{
		Topic: "agent.messages",
		Key:   []byte("session-1"),
		Value: mustJSON(t, MessageEvent{Type: MessageEventCreated, SessionID: "session-1", Message: state.Message{ID: "message-1"}}),
	})
	if err != nil {
		t.Fatalf("expected DLQ to absorb processing error, got %v", err)
	}
	if len(dlq.messages) != 1 {
		t.Fatalf("expected one DLQ message, got %d", len(dlq.messages))
	}
	foundErrorHeader := false
	for _, header := range dlq.messages[0].Headers {
		if header.Key == "x-error" && strings.Contains(string(header.Value), "boom") {
			foundErrorHeader = true
		}
	}
	if !foundErrorHeader {
		t.Fatalf("missing DLQ error header: %#v", dlq.messages[0].Headers)
	}
}

func TestCompositeMessageEventHandlerRunsAllHandlers(t *testing.T) {
	event := MessageEvent{Type: MessageEventCreated, Message: state.Message{ID: "message-1"}}
	first := &captureMessageEventHandler{}
	second := &captureMessageEventHandler{}
	handler := CompositeMessageEventHandler{first, second}

	if err := handler.HandleMessageEvent(context.Background(), event); err != nil {
		t.Fatalf("handle composite event: %v", err)
	}
	if first.calls != 1 || second.calls != 1 {
		t.Fatalf("expected both handlers to run, first=%d second=%d", first.calls, second.calls)
	}
}

func TestMessageFullTextIndexEventHandlerIndexesCreatedEvents(t *testing.T) {
	indexer := &captureFullTextIndexer{}
	handler := NewMessageFullTextIndexEventHandler(indexer)
	event := MessageEvent{Type: MessageEventCreated, Message: state.Message{ID: "message-1", UserID: "alice", SessionID: "session-1"}}

	if err := handler.HandleMessageEvent(context.Background(), event); err != nil {
		t.Fatalf("handle full-text index event: %v", err)
	}
	if indexer.calls != 1 || indexer.messages[0].ID != "message-1" {
		t.Fatalf("expected message to be indexed, got %#v", indexer)
	}
	if err := handler.HandleMessageEvent(context.Background(), MessageEvent{Type: "other", Message: event.Message}); err != nil {
		t.Fatalf("ignore other event: %v", err)
	}
	if indexer.calls != 1 {
		t.Fatalf("non-created event should not be indexed, calls=%d", indexer.calls)
	}
	if err := handler.HandleMessageEvent(context.Background(), MessageEvent{Type: MessageEventDeleted, Message: event.Message}); err != nil {
		t.Fatalf("handle deleted event: %v", err)
	}
	if indexer.deletes != 1 {
		t.Fatalf("deleted event should delete full-text document, deletes=%d", indexer.deletes)
	}
}

func TestMessageVectorIndexEventHandlerDeletesDeletedEvents(t *testing.T) {
	indexer := &captureVectorIndexer{}
	handler := NewMessageVectorIndexEventHandler(indexer)
	message := state.Message{ID: "message-1", UserID: "alice", SessionID: "session-1", Role: state.MessageRoleUser, Content: "hello", Status: state.MessageStatusNormal}

	if err := handler.HandleMessageEvent(context.Background(), MessageEvent{Type: MessageEventCreated, Message: message}); err != nil {
		t.Fatalf("handle created event: %v", err)
	}
	if indexer.indexes != 1 {
		t.Fatalf("created event should index vector, indexes=%d", indexer.indexes)
	}
	message.Status = state.MessageStatusDeleted
	if err := handler.HandleMessageEvent(context.Background(), MessageEvent{Type: MessageEventDeleted, Message: message}); err != nil {
		t.Fatalf("handle deleted event: %v", err)
	}
	if indexer.deletes != 1 {
		t.Fatalf("deleted event should delete vector, deletes=%d", indexer.deletes)
	}
}

type fakeKafkaReader struct {
	messages []kafka.Message
	commits  int
	closed   bool
}

func (r *fakeKafkaReader) FetchMessage(ctx context.Context) (kafka.Message, error) {
	for {
		if len(r.messages) > 0 {
			message := r.messages[0]
			r.messages = r.messages[1:]
			return message, nil
		}
		select {
		case <-ctx.Done():
			return kafka.Message{}, ctx.Err()
		default:
			time.Sleep(time.Millisecond)
		}
	}
}

func (r *fakeKafkaReader) CommitMessages(context.Context, ...kafka.Message) error {
	r.commits++
	return nil
}

func (r *fakeKafkaReader) Close() error {
	r.closed = true
	return nil
}

type fakeKafkaWriter struct {
	messages []kafka.Message
}

func (w *fakeKafkaWriter) WriteMessages(_ context.Context, messages ...kafka.Message) error {
	w.messages = append(w.messages, messages...)
	return nil
}

func (w *fakeKafkaWriter) Close() error { return nil }

type flakyMessageEventHandler struct {
	failures int
	calls    int
}

type captureMessageEventHandler struct {
	calls int
}

func (h *captureMessageEventHandler) HandleMessageEvent(context.Context, MessageEvent) error {
	h.calls++
	return nil
}

type captureFullTextIndexer struct {
	calls    int
	deletes  int
	messages []state.Message
}

func (i *captureFullTextIndexer) IndexMessage(_ context.Context, message state.Message) error {
	i.calls++
	i.messages = append(i.messages, message)
	return nil
}

func (i *captureFullTextIndexer) DeleteMessage(_ context.Context, message state.Message) error {
	i.deletes++
	i.messages = append(i.messages, message)
	return nil
}

type captureVectorIndexer struct {
	indexes int
	deletes int
}

func (i *captureVectorIndexer) IndexMessage(context.Context, state.Message) error {
	i.indexes++
	return nil
}

func (i *captureVectorIndexer) DeleteMessage(context.Context, state.Message) error {
	i.deletes++
	return nil
}

func (h *flakyMessageEventHandler) HandleMessageEvent(context.Context, MessageEvent) error {
	h.calls++
	if h.calls <= h.failures {
		return errors.New("temporary")
	}
	return nil
}

type skipMessageEventLock struct{}

func (skipMessageEventLock) AcquireMessageEvent(context.Context, string, MessageEvent) (bool, func(context.Context, bool) error, error) {
	return false, func(context.Context, bool) error { return nil }, nil
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	return data
}
