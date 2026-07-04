package bootstrap

import (
	"context"
	"testing"

	"claude-codex/internal/backend/agentruntime"
	"claude-codex/internal/harness/state"
)

func TestBuildLLMConfigSupportsSimplePlanner(t *testing.T) {
	cfg, err := BuildLLMConfig("simple", "", "", "", "", 0)
	if err != nil {
		t.Fatalf("build simple config: %v", err)
	}
	if cfg.Provider != "simple" || cfg.Model != "simple" || cfg.Timeout == 0 {
		t.Fatalf("unexpected simple config: %#v", cfg)
	}
	planner, err := newPlanner(cfg)
	if err != nil {
		t.Fatalf("new simple planner: %v", err)
	}
	session := state.NewSession(t.TempDir())
	session.AddUserMessage("hello")
	resp, err := planner.Next(context.Background(), session, nil)
	if err != nil {
		t.Fatalf("simple planner plan: %v", err)
	}
	if resp.AssistantText == "" {
		t.Fatalf("simple planner returned empty assistant text: %#v", resp)
	}
}

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

func TestApplyRoutedModelForScopeRebindsProviderCredentials(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "deepseek-test-key")
	t.Setenv("DEEPSEEK_BASE_URL", "https://deepseek.example/v1")
	got := applyRoutedModelForScope(LLMConfig{
		Provider: "nvidia",
		Model:    "nvidia/nemotron-3-ultra-550b-a55b",
		APIKey:   "nvidia-test-key",
		BaseURL:  "https://nvidia.example/v1",
		Timeout:  30,
	}, "default=nvidia/nemotron-3-ultra-550b-a55b,chat=deepseek-chat", agentruntime.Scope{Prompt: "hello"})
	if got.Provider != "deepseek" {
		t.Fatalf("provider = %q, want deepseek", got.Provider)
	}
	if got.Model != "deepseek-chat" {
		t.Fatalf("model = %q, want deepseek-chat", got.Model)
	}
	if got.APIKey != "deepseek-test-key" {
		t.Fatalf("api key was not rebound for deepseek provider")
	}
	if got.BaseURL != "https://deepseek.example/v1" {
		t.Fatalf("base url = %q, want deepseek base URL", got.BaseURL)
	}
	if got.Timeout != 30 {
		t.Fatalf("timeout = %d, want original timeout", got.Timeout)
	}
}

func TestRoutedModelSupportsEvaluationJudgeRoute(t *testing.T) {
	got := RoutedModel("gemini-2.5-pro", "default=gemini-2.5-pro,judge=gemini-3.5-flash,skill=gemini-3.1-flash-lite", agentruntime.Scope{
		SkillScoped: true,
		SkillName:   "evaluation_judge",
	})
	if got != "gemini-3.5-flash" {
		t.Fatalf("judge routed model = %q, want gemini-3.5-flash", got)
	}
}
