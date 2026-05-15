package agentruntime

import (
	"context"
	"strings"
	"testing"
	"time"

	"claude-codex/internal/harness/state"
)

func TestMessageWriteServiceWritesInvalidatesCacheAndPublishes(t *testing.T) {
	ctx := context.Background()
	repo := newFakeMessageRepository()
	cache := NewMemorySessionContextCache()
	publisher := &captureMessagePublisher{}
	service := NewMessageWriteService(repo, cache, publisher)

	if err := cache.SetContext(ctx, "alice", "session-1", DefaultSessionLoadOptions(), []state.Message{{Role: state.MessageRoleUser, Content: "cached"}}); err != nil {
		t.Fatalf("set cache: %v", err)
	}
	created, err := service.WriteUserMessage(ctx, "alice", "session-1", "hello", false)
	if err != nil {
		t.Fatalf("write message: %v", err)
	}
	if created.ID == "" || created.SeqNo != 1 || created.UserID != "alice" || created.SessionID != "session-1" {
		t.Fatalf("unexpected created message: %#v", created)
	}
	if created.ContentType != state.MessageContentTypeText || created.Status != state.MessageStatusNormal || !created.IsContextUsed {
		t.Fatalf("message defaults were not populated: %#v", created)
	}
	if _, ok, err := cache.GetContext(ctx, "alice", "session-1", DefaultSessionLoadOptions()); err != nil || ok {
		t.Fatalf("expected cache invalidation, ok=%v err=%v", ok, err)
	}
	if len(publisher.events) != 1 || publisher.events[0].Type != MessageEventCreated || publisher.events[0].Message.ID != created.ID {
		t.Fatalf("unexpected published events: %#v", publisher.events)
	}
}

func TestSessionLoadServiceLoadsRecentMessagesWithinTokenBudget(t *testing.T) {
	ctx := context.Background()
	repo := newFakeMessageRepository()
	cache := NewMemorySessionContextCache()
	service := NewSessionLoadService(repo, cache)

	repo.messages["alice:session-1"] = []state.Message{
		{ID: "system", Role: state.MessageRoleSystem, ContentType: state.MessageContentTypeSummary, Content: "summary", SeqNo: 1, Status: state.MessageStatusNormal, IsContextUsed: true},
		{ID: "old", Role: state.MessageRoleUser, Content: strings.Repeat("old ", 160), SeqNo: 2, Status: state.MessageStatusNormal, IsContextUsed: true},
		{ID: "recent-user", Role: state.MessageRoleUser, Content: "recent question", SeqNo: 3, Status: state.MessageStatusNormal, IsContextUsed: true},
		{ID: "recent-assistant", Role: state.MessageRoleAssistant, Content: "recent answer", SeqNo: 4, Status: state.MessageStatusNormal, IsContextUsed: true},
	}

	messages, err := service.LoadContext(ctx, "alice", "session-1", SessionLoadOptions{
		MaxMessages:   10,
		MaxTokens:     40,
		LoadStrategy:  SessionLoadStrategyRecent,
		IncludeSystem: true,
	})
	if err != nil {
		t.Fatalf("load context: %v", err)
	}
	if len(messages) != 3 {
		t.Fatalf("expected system plus recent pair, got %#v", messages)
	}
	if messages[0].ID != "system" || messages[1].ID != "recent-user" || messages[2].ID != "recent-assistant" {
		t.Fatalf("unexpected context order: %#v", messages)
	}

	repo.messages["alice:session-1"] = nil
	cached, err := service.LoadContext(ctx, "alice", "session-1", SessionLoadOptions{
		MaxMessages:   10,
		MaxTokens:     40,
		LoadStrategy:  SessionLoadStrategyRecent,
		IncludeSystem: true,
	})
	if err != nil {
		t.Fatalf("load cached context: %v", err)
	}
	if len(cached) != len(messages) || cached[0].ID != "system" {
		t.Fatalf("expected cached context, got %#v", cached)
	}
}

func TestSessionLoadServiceSummaryStrategyUsesLatestSummaryAndRecentOrdinary(t *testing.T) {
	ctx := context.Background()
	repo := newFakeMessageRepository()
	service := NewSessionLoadService(repo, NewMemorySessionContextCache())

	repo.messages["alice:session-1"] = []state.Message{
		{ID: "summary", Role: state.MessageRoleSystem, ContentType: state.MessageContentTypeSummary, Content: "compressed history", SeqNo: 1, Status: state.MessageStatusNormal, IsContextUsed: true},
		{ID: "old-1", Role: state.MessageRoleUser, Content: "old one", SeqNo: 2, Status: state.MessageStatusNormal, IsContextUsed: true},
		{ID: "old-2", Role: state.MessageRoleAssistant, Content: "old two", SeqNo: 3, Status: state.MessageStatusNormal, IsContextUsed: true},
		{ID: "recent-user", Role: state.MessageRoleUser, Content: "recent question", SeqNo: 4, Status: state.MessageStatusNormal, IsContextUsed: true},
		{ID: "recent-assistant", Role: state.MessageRoleAssistant, Content: "recent answer", SeqNo: 5, Status: state.MessageStatusNormal, IsContextUsed: true},
	}

	messages, err := service.LoadContext(ctx, "alice", "session-1", SessionLoadOptions{
		MaxMessages:   2,
		MaxTokens:     1000,
		LoadStrategy:  SessionLoadStrategySummary,
		IncludeSystem: true,
	})
	if err != nil {
		t.Fatalf("load summary context: %v", err)
	}
	if len(messages) != 3 || messages[0].ID != "summary" || messages[1].ID != "recent-user" || messages[2].ID != "recent-assistant" {
		t.Fatalf("expected latest summary plus recent ordinary messages, got %#v", messages)
	}
}

func TestSessionLoadServiceSlidingWindowStrategyCombinesSummaryWithTokenTail(t *testing.T) {
	ctx := context.Background()
	repo := newFakeMessageRepository()
	service := NewSessionLoadService(repo, NewMemorySessionContextCache())

	repo.messages["alice:session-1"] = []state.Message{
		{ID: "summary", Role: state.MessageRoleSystem, ContentType: state.MessageContentTypeSummary, Content: "compressed history", SeqNo: 1, Status: state.MessageStatusNormal, IsContextUsed: true},
		{ID: "large-old", Role: state.MessageRoleUser, Content: strings.Repeat("large ", 200), SeqNo: 2, Status: state.MessageStatusNormal, IsContextUsed: true},
		{ID: "recent-user", Role: state.MessageRoleUser, Content: "recent question", SeqNo: 3, Status: state.MessageStatusNormal, IsContextUsed: true},
		{ID: "recent-assistant", Role: state.MessageRoleAssistant, Content: "recent answer", SeqNo: 4, Status: state.MessageStatusNormal, IsContextUsed: true},
	}

	messages, err := service.LoadContext(ctx, "alice", "session-1", SessionLoadOptions{
		MaxMessages:   10,
		MaxTokens:     100,
		LoadStrategy:  SessionLoadStrategySlidingWindow,
		IncludeSystem: true,
	})
	if err != nil {
		t.Fatalf("load sliding context: %v", err)
	}
	if len(messages) != 3 || messages[0].ID != "summary" || messages[1].ID != "recent-user" || messages[2].ID != "recent-assistant" {
		t.Fatalf("expected summary plus token-tail window, got %#v", messages)
	}
}

type fakeMessageRepository struct {
	messages map[string][]state.Message
}

func newFakeMessageRepository() *fakeMessageRepository {
	return &fakeMessageRepository{messages: map[string][]state.Message{}}
}

func (r *fakeMessageRepository) AppendMessage(_ context.Context, userID, sessionID string, message state.Message) (state.Message, error) {
	key := userID + ":" + sessionID
	message.SeqNo = int64(len(r.messages[key]) + 1)
	if message.CreatedAt.IsZero() {
		message.CreatedAt = time.Now().UTC()
	}
	if message.UpdatedAt.IsZero() {
		message.UpdatedAt = message.CreatedAt
	}
	r.messages[key] = append(r.messages[key], message)
	return message, nil
}

func (r *fakeMessageRepository) LoadSessionMessages(_ context.Context, userID, sessionID string, opts SessionLoadOptions) ([]state.Message, error) {
	source := r.messages[userID+":"+sessionID]
	items := make([]state.Message, 0, len(source))
	for _, message := range source {
		if message.Status != state.MessageStatusNormal || !message.IsContextUsed {
			continue
		}
		if !opts.IncludeSystem && (message.Role == state.MessageRoleSystem || message.ContentType == state.MessageContentTypeSummary) {
			continue
		}
		items = append(items, message)
	}
	if opts.MaxMessages > 0 && len(items) > opts.MaxMessages {
		items = items[len(items)-opts.MaxMessages:]
	}
	return cloneStateMessages(items), nil
}

func (r *fakeMessageRepository) LoadLatestSummaryMessage(_ context.Context, userID, sessionID string) (state.Message, bool, error) {
	source := r.messages[userID+":"+sessionID]
	for i := len(source) - 1; i >= 0; i-- {
		message := source[i]
		if message.Status != state.MessageStatusNormal || !message.IsContextUsed {
			continue
		}
		if isSystemContextMessage(message) {
			return message, true, nil
		}
	}
	return state.Message{}, false, nil
}

func (r *fakeMessageRepository) MarkMessagesContextUnused(_ context.Context, userID, sessionID string, messageIDs []string) (int, error) {
	key := userID + ":" + sessionID
	idSet := map[string]bool{}
	for _, id := range messageIDs {
		idSet[id] = true
	}
	count := 0
	for i := range r.messages[key] {
		if !idSet[r.messages[key][i].ID] {
			continue
		}
		r.messages[key][i].IsContextUsed = false
		r.messages[key][i].Status = state.MessageStatusTruncated
		count++
	}
	return count, nil
}

type captureMessagePublisher struct {
	events []MessageEvent
}

func (p *captureMessagePublisher) PublishMessageEvent(_ context.Context, event MessageEvent) error {
	p.events = append(p.events, event)
	return nil
}
