package agentruntime

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"claude-codex/internal/harness/engine"
	"claude-codex/internal/harness/state"
)

func TestContextCompactionServiceUsesLLMSummaryAndWritesSummaryMessage(t *testing.T) {
	ctx := context.Background()
	repo := newFakeMessageRepository()
	cache := NewMemorySessionContextCache()
	writer := NewMessageWriteService(repo, cache, NoopMessageEventPublisher{})
	loader := NewSessionLoadService(repo, cache)
	generator := &captureSummaryGenerator{summary: "用户正在实现消息模块；已经完成领域模型和 SQL schema。"}
	service := NewContextCompactionService(loader, writer, repo, generator)

	repo.messages["alice:session-1"] = []state.Message{
		{ID: "old-user", Role: state.MessageRoleUser, ContentType: state.MessageContentTypeText, Content: strings.Repeat("old user ", 80), SeqNo: 1, Status: state.MessageStatusNormal, IsContextUsed: true},
		{ID: "old-assistant", Role: state.MessageRoleAssistant, ContentType: state.MessageContentTypeText, Content: strings.Repeat("old assistant ", 80), SeqNo: 2, Status: state.MessageStatusNormal, IsContextUsed: true},
		{ID: "recent-user", Role: state.MessageRoleUser, ContentType: state.MessageContentTypeText, Content: "recent user", SeqNo: 3, Status: state.MessageStatusNormal, IsContextUsed: true},
		{ID: "recent-assistant", Role: state.MessageRoleAssistant, ContentType: state.MessageContentTypeText, Content: "recent assistant", SeqNo: 4, Status: state.MessageStatusNormal, IsContextUsed: true},
	}

	result, err := service.Compact(ctx, "alice", "session-1", ContextCompactionOptions{
		MaxMessages:    20,
		MaxTokens:      80,
		TargetTokens:   40,
		PreserveRecent: 2,
		IncludeSystem:  true,
	})
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	if !result.Compacted || result.CompactedCount != 2 || result.MarkedTruncated != 2 {
		t.Fatalf("unexpected compaction result: %#v", result)
	}
	if !generator.called || len(generator.messages) != 2 {
		t.Fatalf("summary generator was not called with overflow messages: %#v", generator)
	}
	messages := repo.messages["alice:session-1"]
	if messages[0].Status != state.MessageStatusTruncated || messages[0].IsContextUsed {
		t.Fatalf("old user was not marked out of context: %#v", messages[0])
	}
	summary := messages[len(messages)-1]
	if summary.Role != state.MessageRoleSystem || summary.ContentType != state.MessageContentTypeSummary || !strings.Contains(summary.Content, generator.summary) {
		t.Fatalf("summary message was not written correctly: %#v", summary)
	}

	contextMessages, err := loader.LoadContext(ctx, "alice", "session-1", SessionLoadOptions{
		MaxMessages:   20,
		MaxTokens:     200,
		LoadStrategy:  SessionLoadStrategyRecent,
		IncludeSystem: true,
	})
	if err != nil {
		t.Fatalf("load context after compaction: %v", err)
	}
	if len(contextMessages) != 3 || contextMessages[0].ID != summary.ID || contextMessages[1].ID != "recent-user" || contextMessages[2].ID != "recent-assistant" {
		t.Fatalf("unexpected compacted context: %#v", contextMessages)
	}
}

func TestContextCompactionTriggersOnMessageCountAndReplacesPreviousSummary(t *testing.T) {
	ctx := context.Background()
	repo := newFakeMessageRepository()
	cache := NewMemorySessionContextCache()
	writer := NewMessageWriteService(repo, cache, NoopMessageEventPublisher{})
	loader := NewSessionLoadService(repo, cache)
	generator := &captureSummaryGenerator{summary: "replacement summary"}
	service := NewContextCompactionService(loader, writer, repo, generator)

	repo.messages["alice:session-1"] = []state.Message{
		{ID: "previous-summary", Role: state.MessageRoleSystem, ContentType: state.MessageContentTypeSummary, Content: "previous facts", SeqNo: 1, Status: state.MessageStatusNormal, IsContextUsed: true},
		{ID: "message-1", Role: state.MessageRoleUser, Content: "one", SeqNo: 2, Status: state.MessageStatusNormal, IsContextUsed: true},
		{ID: "message-2", Role: state.MessageRoleAssistant, Content: "two", SeqNo: 3, Status: state.MessageStatusNormal, IsContextUsed: true},
		{ID: "message-3", Role: state.MessageRoleUser, Content: "three", SeqNo: 4, Status: state.MessageStatusNormal, IsContextUsed: true},
		{ID: "message-4", Role: state.MessageRoleAssistant, Content: "four", SeqNo: 5, Status: state.MessageStatusNormal, IsContextUsed: true},
	}

	result, err := service.Compact(ctx, "alice", "session-1", ContextCompactionOptions{
		MaxMessages:    3,
		MaxTokens:      1000,
		TargetTokens:   750,
		PreserveRecent: 1,
		IncludeSystem:  true,
	})
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	if !result.Compacted || result.CompactedCount != 4 || result.MarkedTruncated != 4 {
		t.Fatalf("unexpected count-based compaction result: %#v", result)
	}
	if len(generator.messages) != 4 || generator.messages[0].ID != "previous-summary" {
		t.Fatalf("previous summary was not folded into replacement: %#v", generator.messages)
	}
	active, err := loader.LoadContext(ctx, "alice", "session-1", SessionLoadOptions{
		MaxMessages:   10,
		MaxTokens:     1000,
		LoadStrategy:  SessionLoadStrategyRecent,
		IncludeSystem: true,
	})
	if err != nil {
		t.Fatalf("load active context: %v", err)
	}
	if len(active) != 2 || active[0].ContentType != state.MessageContentTypeSummary || active[1].ID != "message-4" {
		t.Fatalf("expected one replacement summary and one preserved message, got %#v", active)
	}
}

func TestLLMSummaryGeneratorUsesGeneratedPromptRunner(t *testing.T) {
	runner := &summaryRunner{output: "llm summary"}
	generator := LLMSummaryGenerator{
		RunnerFactory: func(scope Scope) Runner {
			if scope.UserID != "alice" || scope.SessionID != "session-1" {
				t.Fatalf("unexpected scope: %#v", scope)
			}
			return runner
		},
	}
	summary, err := generator.GenerateSummary(context.Background(), "alice", "session-1", []state.Message{{
		Role:    state.MessageRoleUser,
		Content: "important history",
	}})
	if err != nil {
		t.Fatalf("generate summary: %v", err)
	}
	if summary != "llm summary" {
		t.Fatalf("summary = %q", summary)
	}
	if !runner.generated || !strings.Contains(runner.prompt, "important history") {
		t.Fatalf("runner was not called with generated summary prompt: %#v", runner)
	}
}

type summaryContextKey struct{}

func TestLLMSummaryGeneratorUsesContextFactoryAndPropagatesContext(t *testing.T) {
	ctx := context.WithValue(context.Background(), summaryContextKey{}, "propagated")
	runner := &contextSummaryRunner{}
	generator := LLMSummaryGenerator{
		ContextRunnerFactory: func(got context.Context, scope Scope) (Runner, error) {
			if got.Value(summaryContextKey{}) != "propagated" {
				t.Fatalf("factory received wrong context value: %#v", got.Value(summaryContextKey{}))
			}
			if scope.UserID != "alice" || scope.SessionID != "session-1" {
				t.Fatalf("unexpected scope: %#v", scope)
			}
			return runner, nil
		},
	}

	summary, err := generator.GenerateSummary(ctx, "alice", "session-1", []state.Message{{Role: state.MessageRoleUser, Content: "history"}})
	if err != nil {
		t.Fatalf("generate summary: %v", err)
	}
	if summary != "context summary" {
		t.Fatalf("summary = %q", summary)
	}
	if !runner.generated || runner.contextValue != "propagated" || !strings.Contains(runner.prompt, "history") {
		t.Fatalf("runner did not receive propagated context and prompt: %#v", runner)
	}
}

func TestContextCompactionFallbackWritesSummaryBeforeMarkerFailure(t *testing.T) {
	ctx := context.Background()
	repo := newFakeMessageRepository()
	cache := NewMemorySessionContextCache()
	writer := NewMessageWriteService(repo, cache, NoopMessageEventPublisher{})
	loader := NewSessionLoadService(repo, cache)
	generator := &captureSummaryGenerator{summary: "fallback summary"}
	marker := &recordingMessageContextMarker{err: errors.New("marker failure")}
	service := NewContextCompactionService(loader, writer, marker, generator)

	repo.messages["alice:session-1"] = []state.Message{
		{ID: "old-user", Role: state.MessageRoleUser, ContentType: state.MessageContentTypeText, Content: strings.Repeat("old user ", 80), SeqNo: 1, Status: state.MessageStatusNormal, IsContextUsed: true},
		{ID: "old-assistant", Role: state.MessageRoleAssistant, ContentType: state.MessageContentTypeText, Content: strings.Repeat("old assistant ", 80), SeqNo: 2, Status: state.MessageStatusNormal, IsContextUsed: true},
		{ID: "recent-user", Role: state.MessageRoleUser, ContentType: state.MessageContentTypeText, Content: "recent user", SeqNo: 3, Status: state.MessageStatusNormal, IsContextUsed: true},
		{ID: "recent-assistant", Role: state.MessageRoleAssistant, ContentType: state.MessageContentTypeText, Content: "recent assistant", SeqNo: 4, Status: state.MessageStatusNormal, IsContextUsed: true},
	}

	_, err := service.Compact(ctx, "alice", "session-1", ContextCompactionOptions{
		MaxMessages:    20,
		MaxTokens:      80,
		TargetTokens:   40,
		PreserveRecent: 2,
		IncludeSystem:  true,
	})
	if err == nil || !strings.Contains(err.Error(), "marker failure") {
		t.Fatalf("expected marker failure, got %v", err)
	}
	if !marker.called {
		t.Fatal("marker was not called")
	}
	if !generator.called || len(generator.messages) != 2 {
		t.Fatalf("summary generator was not called with overflow messages: %#v", generator)
	}
	messages := repo.messages["alice:session-1"]
	if len(messages) != 5 {
		t.Fatalf("summary was not written before marker failure: %#v", messages)
	}
	summary := messages[len(messages)-1]
	if summary.UserID != "alice" || summary.SessionID != "session-1" || summary.ContentType != state.MessageContentTypeSummary || !strings.Contains(summary.Content, "fallback summary") {
		t.Fatalf("summary message was not persisted before failure: %#v", summary)
	}
	for _, message := range messages[:4] {
		if message.Status != state.MessageStatusNormal || !message.IsContextUsed {
			t.Fatalf("old messages should remain untouched after marker failure: %#v", message)
		}
	}
}

func TestContextCompactionUsesAtomicSQLWriter(t *testing.T) {
	ctx := context.Background()
	repo := newAtomicCompactionMessageRepository()
	cache := NewMemorySessionContextCache()
	writer := NewMessageWriteService(repo, cache, NoopMessageEventPublisher{})
	loader := NewSessionLoadService(repo, cache)
	generator := &captureSummaryGenerator{summary: "atomic summary"}
	marker := &recordingMessageContextMarker{err: errors.New("marker should not be used")}
	service := NewContextCompactionService(loader, writer, marker, generator)

	repo.messages["alice:session-1"] = []state.Message{
		{ID: "old-user", Role: state.MessageRoleUser, ContentType: state.MessageContentTypeText, Content: strings.Repeat("old user ", 80), SeqNo: 1, Status: state.MessageStatusNormal, IsContextUsed: true},
		{ID: "old-assistant", Role: state.MessageRoleAssistant, ContentType: state.MessageContentTypeText, Content: strings.Repeat("old assistant ", 80), SeqNo: 2, Status: state.MessageStatusNormal, IsContextUsed: true},
		{ID: "recent-user", Role: state.MessageRoleUser, ContentType: state.MessageContentTypeText, Content: "recent user", SeqNo: 3, Status: state.MessageStatusNormal, IsContextUsed: true},
		{ID: "recent-assistant", Role: state.MessageRoleAssistant, ContentType: state.MessageContentTypeText, Content: "recent assistant", SeqNo: 4, Status: state.MessageStatusNormal, IsContextUsed: true},
	}

	result, err := service.Compact(ctx, "alice", "session-1", ContextCompactionOptions{
		MaxMessages:    20,
		MaxTokens:      80,
		TargetTokens:   40,
		PreserveRecent: 2,
		IncludeSystem:  true,
	})
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	if !result.Compacted || result.MarkedTruncated != 2 {
		t.Fatalf("unexpected compaction result: %#v", result)
	}
	if !repo.called {
		t.Fatal("atomic compaction writer was not used")
	}
	if marker.called {
		t.Fatal("marker should not be called on atomic compaction path")
	}
	if !generator.called || len(generator.messages) != 2 {
		t.Fatalf("summary generator was not called with overflow messages: %#v", generator)
	}
	messages := repo.messages["alice:session-1"]
	if messages[0].Status != state.MessageStatusTruncated || messages[1].Status != state.MessageStatusTruncated {
		t.Fatalf("atomic writer did not mark old messages: %#v", messages[:2])
	}
	summary := messages[len(messages)-1]
	if summary.Role != state.MessageRoleSystem || summary.ContentType != state.MessageContentTypeSummary || !strings.Contains(summary.Content, "atomic summary") {
		t.Fatalf("summary message was not persisted atomically: %#v", summary)
	}
}

func TestContextCompactionServiceUsesAtomicSQLWriter(t *testing.T) {
	ctx := context.Background()
	db := openPostgresMigrationTestDB(t)
	if err := RunPostgresGooseMigrations(ctx, db, SQLDialectPostgres); err != nil {
		t.Fatalf("RunPostgresGooseMigrations() error = %v", err)
	}
	store := NewSQLSessionStoreWithDialect(db, SQLDialectPostgres)
	if err := store.Init(ctx); err != nil {
		t.Fatalf("init sql store: %v", err)
	}
	cache := NewMemorySessionContextCache()
	writer := NewMessageWriteService(store, cache, NoopMessageEventPublisher{})
	loader := NewSessionLoadService(store, cache)
	generator := &captureSummaryGenerator{summary: "postgres summary"}
	marker := &recordingMessageContextMarker{err: errors.New("marker should not be used")}
	service := NewContextCompactionService(loader, writer, marker, generator)

	userID := "compaction-" + newSortableID()
	session, err := store.Create(ctx, userID, t.TempDir())
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	oldUser, err := store.AppendMessage(ctx, userID, session.ID, state.Message{
		Role:        state.MessageRoleUser,
		ContentType: state.MessageContentTypeText,
		Content:     strings.Repeat("old user ", 80),
	})
	if err != nil {
		t.Fatalf("append old user: %v", err)
	}
	oldAssistant, err := store.AppendMessage(ctx, userID, session.ID, state.Message{
		Role:        state.MessageRoleAssistant,
		ContentType: state.MessageContentTypeText,
		Content:     strings.Repeat("old assistant ", 80),
	})
	if err != nil {
		t.Fatalf("append old assistant: %v", err)
	}
	if _, err := store.AppendMessage(ctx, userID, session.ID, state.Message{
		Role:        state.MessageRoleUser,
		ContentType: state.MessageContentTypeText,
		Content:     "recent user",
	}); err != nil {
		t.Fatalf("append recent user: %v", err)
	}
	if _, err := store.AppendMessage(ctx, userID, session.ID, state.Message{
		Role:        state.MessageRoleAssistant,
		ContentType: state.MessageContentTypeText,
		Content:     "recent assistant",
	}); err != nil {
		t.Fatalf("append recent assistant: %v", err)
	}

	result, err := service.Compact(ctx, userID, session.ID, ContextCompactionOptions{
		MaxMessages:    20,
		MaxTokens:      80,
		TargetTokens:   40,
		PreserveRecent: 2,
		IncludeSystem:  true,
	})
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	if !result.Compacted || result.MarkedTruncated != 2 {
		t.Fatalf("unexpected compaction result: %#v", result)
	}
	if marker.called {
		t.Fatal("marker should not be called on the SQL atomic path")
	}
	if !generator.called || len(generator.messages) != 2 {
		t.Fatalf("summary generator was not called with overflow messages: %#v", generator)
	}

	var status int
	var contextUsed bool
	if err := db.QueryRowContext(ctx, `SELECT status, is_context_used FROM agent_messages WHERE message_id = $1`, oldUser.ID).Scan(&status, &contextUsed); err != nil {
		t.Fatalf("read old user row: %v", err)
	}
	if status != int(state.MessageStatusTruncated) || contextUsed {
		t.Fatalf("old user row was not atomically truncated: status=%d context_used=%v", status, contextUsed)
	}
	if err := db.QueryRowContext(ctx, `SELECT status, is_context_used FROM agent_messages WHERE message_id = $1`, oldAssistant.ID).Scan(&status, &contextUsed); err != nil {
		t.Fatalf("read old assistant row: %v", err)
	}
	if status != int(state.MessageStatusTruncated) || contextUsed {
		t.Fatalf("old assistant row was not atomically truncated: status=%d context_used=%v", status, contextUsed)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_messages WHERE user_id = $1 AND session_id = $2 AND status = $3 AND content_type = $4`, userID, session.ID, state.MessageStatusNormal, state.MessageContentTypeSummary).Scan(&status); err != nil {
		t.Fatalf("count summary rows: %v", err)
	}
	if status != 1 {
		t.Fatalf("summary row count = %d, want 1", status)
	}
	contextMessages, err := loader.LoadContext(ctx, userID, session.ID, SessionLoadOptions{
		MaxMessages:   20,
		MaxTokens:     200,
		LoadStrategy:  SessionLoadStrategyRecent,
		IncludeSystem: true,
	})
	if err != nil {
		t.Fatalf("load compacted context: %v", err)
	}
	if len(contextMessages) != 3 || contextMessages[0].ContentType != state.MessageContentTypeSummary || contextMessages[1].Role != state.MessageRoleUser || contextMessages[1].Content != "recent user" || contextMessages[2].Role != state.MessageRoleAssistant || contextMessages[2].Content != "recent assistant" {
		t.Fatalf("unexpected compacted context: %#v", contextMessages)
	}
}

type captureSummaryGenerator struct {
	summary  string
	called   bool
	messages []state.Message
}

func (g *captureSummaryGenerator) GenerateSummary(_ context.Context, _, _ string, messages []state.Message) (string, error) {
	g.called = true
	g.messages = cloneStateMessages(messages)
	return g.summary, nil
}

type summaryRunner struct {
	output    string
	prompt    string
	generated bool
}

func (r *summaryRunner) Run(_ context.Context, session *state.Session, prompt string) (engine.Result, error) {
	return r.RunGeneratedPrompt(context.Background(), session, prompt)
}

func (r *summaryRunner) RunGeneratedPrompt(_ context.Context, session *state.Session, prompt string) (engine.Result, error) {
	r.generated = true
	r.prompt = prompt
	if session == nil {
		session = state.NewSession("")
	}
	session.AddAssistantMessage(r.output)
	return engine.Result{Output: r.output, Session: session}, nil
}

type contextSummaryRunner struct {
	generated    bool
	contextValue any
	prompt       string
}

func (r *contextSummaryRunner) Run(ctx context.Context, session *state.Session, prompt string) (engine.Result, error) {
	return r.RunGeneratedPrompt(ctx, session, prompt)
}

func (r *contextSummaryRunner) RunGeneratedPrompt(ctx context.Context, session *state.Session, prompt string) (engine.Result, error) {
	r.generated = true
	r.contextValue = ctx.Value(summaryContextKey{})
	r.prompt = prompt
	if session == nil {
		session = state.NewSession("")
	}
	session.AddAssistantMessage("context summary")
	return engine.Result{Output: "context summary", Session: session}, nil
}

type recordingMessageContextMarker struct {
	called bool
	err    error
}

func (m *recordingMessageContextMarker) MarkMessagesContextUnused(_ context.Context, _, _ string, _ []string) (int, error) {
	m.called = true
	if m.err != nil {
		return 0, m.err
	}
	return 0, nil
}

type atomicCompactionMessageRepository struct {
	*fakeMessageRepository
	called bool
}

func newAtomicCompactionMessageRepository() *atomicCompactionMessageRepository {
	return &atomicCompactionMessageRepository{fakeMessageRepository: newFakeMessageRepository()}
}

func (r *atomicCompactionMessageRepository) WriteSummaryAndMarkMessagesContextUnused(ctx context.Context, userID, sessionID string, summary state.Message, messageIDs []string) (state.Message, int, error) {
	r.called = true
	normalized := normalizeWriteMessage(summary, userID, sessionID, time.Now().UTC())
	created, err := r.AppendMessage(ctx, userID, sessionID, normalized)
	if err != nil {
		return state.Message{}, 0, err
	}
	marked, err := r.MarkMessagesContextUnused(ctx, userID, sessionID, messageIDs)
	if err != nil {
		return state.Message{}, 0, err
	}
	return created, marked, nil
}
