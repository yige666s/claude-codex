package engine

import (
	"context"
	"testing"

	"claude-codex/internal/harness/permissions"
	"claude-codex/internal/harness/state"
	toolkit "claude-codex/internal/harness/tools"
)

type generatedPromptPlanner struct{}

func (generatedPromptPlanner) Next(context.Context, *state.Session, []toolkit.Descriptor) (Plan, error) {
	return Plan{
		AssistantText: "generated result",
		StopReason:    "end_turn",
	}, nil
}

func TestRunGeneratedPromptDoesNotAppendUserMessage(t *testing.T) {
	session := state.NewSession(t.TempDir())
	session.AddUserMessage("/find-skills demo")

	engine := NewWithDir(generatedPromptPlanner{}, toolkit.NewRegistry(), permissions.NewChecker(permissions.ModeBypass, nil, nil), 2, t.TempDir())
	result, err := engine.RunGeneratedPrompt(context.Background(), session, "internal generated prompt")
	if err != nil {
		t.Fatalf("RunGeneratedPrompt() error = %v", err)
	}
	if result.Output != "generated result" {
		t.Fatalf("unexpected result %#v", result)
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
		t.Fatalf("expected only the original user message to remain, got %#v", session.Messages)
	}
	if hiddenUserCount != 1 {
		t.Fatalf("expected one hidden generated prompt message, got %#v", session.Messages)
	}
}
