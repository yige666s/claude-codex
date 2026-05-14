package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"claude-codex/internal/harness/permissions"
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
				ID:    "tool-1",
				Name:  "file_read",
				Input: json.RawMessage(`{"path":"README.md"}`),
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
