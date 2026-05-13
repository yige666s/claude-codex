package provider

import (
	"context"
	"testing"

	"claude-codex/internal/harness/state"
	toolkit "claude-codex/internal/harness/tools"
	publictypes "claude-codex/internal/public/types"
)

type fakeProvider struct {
	response *MessageResponse
	request  *MessageRequest
}

func (f *fakeProvider) CreateMessage(_ context.Context, request MessageRequest) (*MessageResponse, error) {
	f.request = &request
	return f.response, nil
}

func (f *fakeProvider) Name() string { return "fake" }

func (f *fakeProvider) SupportedModels() []string { return []string{"fake"} }

func TestPlannerConvertsProviderToolCallsToEnginePlan(t *testing.T) {
	planner := NewPlanner(&fakeProvider{
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

func TestPlannerPreservesUserContentBlocks(t *testing.T) {
	provider := &fakeProvider{
		response: &MessageResponse{
			Model:      "fake",
			Role:       "assistant",
			StopReason: "end_turn",
			Content:    []ContentBlock{{Type: "text", Text: "ok"}},
		},
	}
	planner := NewPlanner(provider, "fake")
	session := state.NewSession(t.TempDir())
	session.Messages = append(session.Messages, state.Message{
		Role:    "user",
		Content: "describe it",
		ContentBlocks: []publictypes.ContentBlock{
			{Type: "text", Text: "describe it"},
			{Type: "image", Source: map[string]interface{}{
				"type":       "base64",
				"media_type": "image/png",
				"data":       "cG5n",
			}},
		},
	})

	if _, err := planner.Next(context.Background(), session, nil); err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	if provider.request == nil || len(provider.request.Messages) != 1 {
		t.Fatalf("missing provider request: %#v", provider.request)
	}
	blocks, ok := provider.request.Messages[0].Content.([]ContentBlock)
	if !ok || len(blocks) != 2 {
		t.Fatalf("expected provider content blocks, got %#v", provider.request.Messages[0].Content)
	}
	if blocks[1].Type != "image" || blocks[1].Source["data"] != "cG5n" {
		t.Fatalf("image block was not preserved: %#v", blocks[1])
	}
}
