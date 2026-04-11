package provider

import (
	"context"
	"testing"

	"claude-codex/internal/harness/state"
	toolkit "claude-codex/internal/harness/tools"
)

type fakeProvider struct {
	response *MessageResponse
}

func (f fakeProvider) CreateMessage(context.Context, MessageRequest) (*MessageResponse, error) {
	return f.response, nil
}

func (f fakeProvider) Name() string { return "fake" }

func (f fakeProvider) SupportedModels() []string { return []string{"fake"} }

func TestPlannerConvertsProviderToolCallsToEnginePlan(t *testing.T) {
	planner := NewPlanner(fakeProvider{
		response: &MessageResponse{
			Model:      "fake",
			Role:       "assistant",
			StopReason: "tool_use",
			ToolCalls: []ToolCall{
				{ID: "call-1", Name: "bash", Input: []byte(`{"command":"ls"}`)},
			},
		},
	}, "fake")

	plan, err := planner.Next(context.Background(), state.NewSession(t.TempDir()), []toolkit.Descriptor{})
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	if len(plan.ToolCalls) != 1 || plan.ToolCalls[0].Name != "bash" {
		t.Fatalf("unexpected plan %#v", plan)
	}
}
