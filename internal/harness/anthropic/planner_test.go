package anthropic

import (
	"strings"
	"testing"

	"claude-codex/internal/harness/plannerapi"
)

func TestValidatePlanRejectsEmptyAssistantResponse(t *testing.T) {
	_, err := validatePlan("claude-test", plannerapi.Plan{StopReason: "end_turn"})
	if err == nil {
		t.Fatal("expected empty response error")
	}
	if !strings.Contains(err.Error(), "empty response") {
		t.Fatalf("error = %v, want empty response marker", err)
	}
}

func TestValidatePlanAllowsTextOrToolCalls(t *testing.T) {
	if _, err := validatePlan("claude-test", plannerapi.Plan{AssistantText: "ok"}); err != nil {
		t.Fatalf("text plan rejected: %v", err)
	}
	if _, err := validatePlan("claude-test", plannerapi.Plan{ToolCalls: []plannerapi.ToolCall{{ID: "tool-1", Name: "Read"}}}); err != nil {
		t.Fatalf("tool plan rejected: %v", err)
	}
}
