package agentruntime

import (
	"context"
	"strings"
	"testing"

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
