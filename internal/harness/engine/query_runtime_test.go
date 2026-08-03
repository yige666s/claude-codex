package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"claude-codex/internal/harness/permissions"
	queryengine "claude-codex/internal/harness/queryengine"
	"claude-codex/internal/harness/skills"
	"claude-codex/internal/harness/state"
	toolkit "claude-codex/internal/harness/tools"
)

func TestUseQueryRuntime_RunGeneratedPromptKeepsMessageHidden(t *testing.T) {
	session := state.NewSession(t.TempDir())
	session.AddUserMessage("/find-skills demo")

	engine := NewWithDir(NewSimplePlanner(), toolkit.NewRegistry(), permissions.NewChecker(permissions.ModeBypass, nil, nil), 2, t.TempDir())
	engine.UseQueryRuntime()

	result, err := engine.RunGeneratedPrompt(context.Background(), session, "internal generated prompt")
	if err != nil {
		t.Fatalf("RunGeneratedPrompt() error = %v", err)
	}
	if result.Session != session {
		t.Fatalf("expected original session pointer, got %#v", result.Session)
	}

	userCount := 0
	hiddenUserCount := 0
	for _, message := range session.Messages {
		if message.Role == "user" {
			if message.Hidden {
				hiddenUserCount++
			} else {
				userCount++
			}
		}
	}
	if userCount != 1 {
		t.Fatalf("expected one visible user message, got %#v", session.Messages)
	}
	if hiddenUserCount != 1 {
		t.Fatalf("expected one hidden generated prompt message, got %#v", session.Messages)
	}
}

func TestQueryRuntimeInitialMessagesIncludeFullSkillDescriptionsAfterSessionStarted(t *testing.T) {
	session := state.NewSession(t.TempDir())
	session.AddUserMessage("earlier turn")
	fullDescription := "Use this skill for Word documents. " + strings.Repeat("detail ", 80) + "unique-tail-marker"
	manager := skills.NewSkillManager()
	if err := manager.RegisterLoadedSkills([]*skills.SkillDefinition{{
		Name:          "docx",
		Description:   fullDescription,
		UserInvocable: true,
	}}); err != nil {
		t.Fatalf("register skill: %v", err)
	}

	engine := NewWithDir(NewSimplePlanner(), toolkit.NewRegistry(), permissions.NewChecker(permissions.ModeBypass, nil, nil), 2, t.TempDir())
	engine.SetSkillManager(manager)
	runtime := newQueryRuntime(engine).(*queryRuntime)

	messages := runtime.initialQueryMessages(session)
	found := false
	for _, message := range messages {
		if message.IsMeta && strings.Contains(fmt.Sprint(message.Content), "unique-tail-marker") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected full skill description in query runtime context, got %#v", messages)
	}
}

func TestQueryRuntimeInitialMessagesIncludeToolCapabilityContext(t *testing.T) {
	session := state.NewSession(t.TempDir())
	registry := toolkit.NewRegistry(
		staticDescriptorTool{name: "WebSearch"},
		staticDescriptorTool{name: "WebFetch"},
		staticDescriptorTool{name: "Artifact"},
		staticDescriptorTool{name: "Skill"},
	)
	engine := NewWithDir(NewSimplePlanner(), registry, permissions.NewChecker(permissions.ModeBypass, nil, nil), 2, t.TempDir())
	runtime := newQueryRuntime(engine).(*queryRuntime)

	messages := runtime.initialQueryMessages(session)
	for _, needle := range []string{"<tool-capabilities>", "WebSearch", "WebFetch", "Artifact", "Markdown", "Skill"} {
		found := false
		for _, message := range messages {
			if message.IsMeta && strings.Contains(fmt.Sprint(message.Content), needle) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected tool capability context to contain %q, got %#v", needle, messages)
		}
	}
	if session.Metadata[toolCapabilityInjectedKey] != "true" {
		t.Fatalf("tool capability metadata not marked: %#v", session.Metadata)
	}
}

func TestQueryRuntimeMessageRoundTripPreservesToolHistory(t *testing.T) {
	session := state.NewSession(t.TempDir())
	session.Messages = []state.Message{
		{
			Role:      "user",
			Content:   "read config",
			CreatedAt: session.StartedAt,
		},
		{
			Role:      "assistant",
			Content:   "I'll inspect it.",
			CreatedAt: session.StartedAt,
			ToolCalls: []state.ToolCall{{
				ID:               "tool-1",
				Name:             "file_read",
				Input:            json.RawMessage(`{"path":"README.md"}`),
				ThoughtSignature: "signed-thought",
			}},
		},
		{
			Role:       "tool",
			ToolCallID: "tool-1",
			ToolName:   "file_read",
			ToolInput:  json.RawMessage(`{"path":"README.md"}`),
			ToolOutput: "README contents",
			CreatedAt:  session.StartedAt,
		},
		{
			Role:      "user",
			Content:   "hidden context",
			Hidden:    true,
			CreatedAt: session.StartedAt,
		},
		{
			Role:      "assistant",
			Content:   "hidden assistant context",
			Hidden:    true,
			CreatedAt: session.StartedAt,
		},
	}

	queryMessages := sessionToQueryMessages(session)
	roundTripped := state.NewSession(session.WorkingDir)
	syncSessionFromQueryMessages(roundTripped, queryMessages)

	if len(roundTripped.Messages) != len(session.Messages) {
		t.Fatalf("expected %d messages, got %#v", len(session.Messages), roundTripped.Messages)
	}

	assistant := roundTripped.Messages[1]
	if assistant.Role != "assistant" || len(assistant.ToolCalls) != 1 {
		t.Fatalf("expected assistant tool call to round-trip, got %#v", assistant)
	}
	if assistant.ToolCalls[0].Name != "file_read" {
		t.Fatalf("unexpected tool call after round-trip: %#v", assistant.ToolCalls[0])
	}
	if assistant.ToolCalls[0].ThoughtSignature != "signed-thought" {
		t.Fatalf("thought signature was not preserved: %#v", assistant.ToolCalls[0])
	}

	toolResult := roundTripped.Messages[2]
	if toolResult.Role != "tool" || toolResult.ToolName != "file_read" || toolResult.ToolOutput != "README contents" {
		t.Fatalf("expected tool result to round-trip, got %#v", toolResult)
	}

	hidden := roundTripped.Messages[3]
	if hidden.Role != "user" || !hidden.Hidden || hidden.Content != "hidden context" {
		t.Fatalf("expected hidden user message to round-trip, got %#v", hidden)
	}
	hiddenAssistant := roundTripped.Messages[4]
	if hiddenAssistant.Role != "assistant" || !hiddenAssistant.Hidden || hiddenAssistant.Content != "hidden assistant context" {
		t.Fatalf("expected hidden assistant message to round-trip, got %#v", hiddenAssistant)
	}
}

func TestMergeNewQueryMessagesPreservesDurableCompactedHistory(t *testing.T) {
	session := state.NewSession(t.TempDir())
	session.Messages = []state.Message{
		{ID: "truncated-1", Role: state.MessageRoleUser, Content: "old", Status: state.MessageStatusTruncated, IsContextUsed: false, SeqNo: 1},
		{ID: "summary-1", Role: state.MessageRoleSystem, ContentType: state.MessageContentTypeSummary, Content: "summary", Status: state.MessageStatusNormal, IsContextUsed: true, SeqNo: 2},
		{ID: "recent-1", Role: state.MessageRoleUser, Content: "recent", Status: state.MessageStatusNormal, IsContextUsed: true, SeqNo: 3},
	}
	initial := sessionToQueryMessages(session)
	queryMessages := append(append([]queryengine.Message(nil), initial...), queryengine.Message{
		Type:    "assistant",
		UUID:    "assistant-new",
		Content: "answer",
	})

	mergeNewQueryMessagesIntoSession(session, queryMessages, initial)

	if len(session.Messages) != 4 {
		t.Fatalf("durable history was replaced: %#v", session.Messages)
	}
	if got := session.Messages[0]; got.ID != "truncated-1" || got.Status != state.MessageStatusTruncated || got.IsContextUsed {
		t.Fatalf("compacted tombstone was not preserved: %#v", got)
	}
	if got := session.Messages[1]; got.ID != "summary-1" || got.ContentType != state.MessageContentTypeSummary || got.SeqNo != 2 {
		t.Fatalf("summary metadata was not preserved: %#v", got)
	}
	if got := session.Messages[3]; got.ID != "assistant-new" || got.Role != state.MessageRoleAssistant || got.Content != "answer" || !got.IsContextUsed {
		t.Fatalf("new query message was not appended correctly: %#v", got)
	}
}

func TestQueryRuntimeLastAssistantMessageIgnoresHiddenContext(t *testing.T) {
	messages := []state.Message{
		{Role: "user", Content: "do work"},
		{Role: "assistant", Content: "Understood. I have the workspace context.", Hidden: true},
		{Role: "assistant", Content: "visible result"},
	}

	if got := lastAssistantMessage(messages); got != "visible result" {
		t.Fatalf("lastAssistantMessage() = %q, want visible result", got)
	}
}

func TestQueryRuntimeRejectsEmptyAssistantResponse(t *testing.T) {
	session := state.NewSession(t.TempDir())
	engine := NewWithDir(emptyPlanner{}, toolkit.NewRegistry(), permissions.NewChecker(permissions.ModeBypass, nil, nil), 2, t.TempDir())
	engine.UseQueryRuntime()

	_, err := engine.RunGeneratedPrompt(context.Background(), session, "internal generated prompt")
	if err == nil {
		t.Fatal("expected empty response error")
	}
	if !strings.Contains(err.Error(), "no assistant text or tool calls") {
		t.Fatalf("error = %v, want no assistant text marker", err)
	}
	for _, want := range []string{"query loop failed", "model=", "assistant_messages="} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %v, want diagnostic %q", err, want)
		}
	}
}

func TestSessionToQueryMessagesSkipsCompactedHistory(t *testing.T) {
	session := state.NewSession(t.TempDir())
	session.Messages = []state.Message{
		{ID: "old", Role: state.MessageRoleUser, Content: "old context", Status: state.MessageStatusTruncated, IsContextUsed: false},
		{ID: "summary", Role: state.MessageRoleSystem, ContentType: state.MessageContentTypeSummary, Content: "history summary", Status: state.MessageStatusNormal, IsContextUsed: true},
		{ID: "recent", Role: state.MessageRoleUser, Content: "recent context", Status: state.MessageStatusNormal, IsContextUsed: true},
	}

	messages := sessionToQueryMessages(session)
	if len(messages) != 2 {
		t.Fatalf("expected summary and recent context only, got %#v", messages)
	}
	for _, message := range messages {
		if strings.Contains(fmt.Sprint(message.Content), "old context") {
			t.Fatalf("truncated context leaked into query messages: %#v", messages)
		}
	}
}

func TestQueryRuntimeToolLedgerReusesSucceededToolResult(t *testing.T) {
	calls := 0
	input := json.RawMessage(`{"value":"same"}`)
	registry := toolkit.NewRegistry(countingTool{count: &calls})
	engine := New(followupPlanner{call: ToolCall{ID: "tool-1", Name: "counting_tool", Input: input}}, registry, permissions.NewChecker(permissions.ModeBypass, nil, nil), 3)
	engine.UseQueryRuntime()
	ledger := newMemoryEngineLedger()
	engine.SetToolLedger(ledger)
	engine.SetDefaultToolExecutionScope(ToolExecutionScope{UserID: "alice", SessionID: "session-1"})

	ctx := WithToolExecutionScope(context.Background(), ToolExecutionScope{
		WorkflowRunID:     "run-1",
		WorkflowStepID:    "step-1",
		WorkflowStepIndex: 3,
	})
	session := state.NewSession("")
	first, err := engine.Run(ctx, session, "do the work")
	if err != nil {
		t.Fatalf("first Run() error = %v", err)
	}
	second, err := engine.Run(ctx, session, "do the work again")
	if err != nil {
		t.Fatalf("second Run() error = %v", err)
	}

	if calls != 1 {
		t.Fatalf("expected tool to execute once, executed %d times", calls)
	}
	if first.Output != "handled: fresh" || second.Output != "handled: fresh" {
		t.Fatalf("unexpected outputs: first=%q second=%q", first.Output, second.Output)
	}
}

type emptyPlanner struct{}

func (emptyPlanner) Next(context.Context, *state.Session, []toolkit.Descriptor) (Plan, error) {
	return Plan{StopReason: "end_turn"}, nil
}
