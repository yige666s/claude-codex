package bootstrap

import (
	"testing"

	"claude-codex/internal/backend/agentruntime"
)

func TestApplyRoutedModelForScopeRebindsVertexLocation(t *testing.T) {
	got := applyRoutedModelForScope(LLMConfig{
		Provider:       "vertex",
		Model:          "gemini-2.5-pro",
		VertexLocation: "us-central1",
	}, "default=gemini-2.5-pro,chat=gemini-3.1-flash-lite", agentruntime.Scope{Prompt: "hello"})
	if got.Model != "gemini-3.1-flash-lite" {
		t.Fatalf("model = %q, want routed model", got.Model)
	}
	if got.VertexLocation != "global" {
		t.Fatalf("vertex location = %q, want global", got.VertexLocation)
	}
}
