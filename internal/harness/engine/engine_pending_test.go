package engine

import (
	"context"
	"strings"
	"testing"

	"claude-codex/internal/harness/permissions"
	"claude-codex/internal/harness/state"
	toolkit "claude-codex/internal/harness/tools"
)

type pendingPlanner struct {
	seen string
}

func (p *pendingPlanner) Next(_ context.Context, session *state.Session, _ []toolkit.Descriptor) (Plan, error) {
	p.seen = session.LastUserMessage()
	return Plan{AssistantText: p.seen, StopReason: "end_turn"}, nil
}

func TestEngineInjectsPendingMessagesBeforePlannerTurn(t *testing.T) {
	planner := &pendingPlanner{}
	eng := New(planner, toolkit.NewRegistry(), permissions.NewChecker(permissions.ModeBypass, nil, nil), 2)
	eng.UseLegacyRuntime()
	drained := false
	eng.SetPendingMessageProvider(func(context.Context) []string {
		if drained {
			return nil
		}
		drained = true
		return []string{"continue with auth tests"}
	})

	session := state.NewSession(t.TempDir())
	result, err := eng.Run(context.Background(), session, "initial request")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !strings.Contains(result.Output, "continue with auth tests") {
		t.Fatalf("pending message was not injected, output=%q seen=%q", result.Output, planner.seen)
	}
	if !strings.Contains(result.Output, "<agent-follow-up>") {
		t.Fatalf("agent pending provider should wrap follow-up message, got %q", result.Output)
	}
}

func TestEngineAddsRawPendingMessageProviders(t *testing.T) {
	planner := &pendingPlanner{}
	eng := New(planner, toolkit.NewRegistry(), permissions.NewChecker(permissions.ModeBypass, nil, nil), 2)
	eng.UseLegacyRuntime()
	eng.AddPendingMessageProvider(func(context.Context) []string {
		return []string{"<task-notification><status>completed</status></task-notification>"}
	})

	session := state.NewSession(t.TempDir())
	result, err := eng.Run(context.Background(), session, "initial request")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !strings.Contains(result.Output, "<task-notification>") || strings.Contains(result.Output, "<agent-follow-up>") {
		t.Fatalf("raw pending provider not injected as-is: %q", result.Output)
	}
}
