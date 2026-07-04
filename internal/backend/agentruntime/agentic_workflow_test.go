package agentruntime

import (
	"testing"
	"time"

	"claude-codex/internal/harness/skills"
)

func TestAgenticTaskTurnTimeoutExtendsLongRunningSkill(t *testing.T) {
	runtime := NewRuntime(
		RuntimeConfig{TurnTimeout: 2 * time.Minute},
		nil,
		nil,
		fakeSkillCatalog{skills: []*skills.SkillDefinition{{
			Name:          "presentations",
			UserInvocable: true,
			RunAsJob:      true,
			Metadata: map[string]any{
				"agentapi": map[string]any{
					"long_running":       true,
					"produces_artifacts": true,
				},
			},
		}}},
		nil,
	)

	if got := runtime.agenticTaskTurnTimeout(ChatRequest{Content: "/presentations make slides"}); got != longRunningSkillTurnTimeout {
		t.Fatalf("long-running skill timeout = %s, want %s", got, longRunningSkillTurnTimeout)
	}
	if got := runtime.agenticTaskTurnTimeout(ChatRequest{Content: "/presentations make slides"}); got <= 5*time.Minute {
		t.Fatalf("long-running presentation timeout = %s, should exceed historical 5m job deadline", got)
	}
}

func TestAgenticTaskTurnTimeoutKeepsRegularChatBudget(t *testing.T) {
	runtime := NewRuntime(
		RuntimeConfig{TurnTimeout: 2 * time.Minute},
		nil,
		nil,
		fakeSkillCatalog{skills: []*skills.SkillDefinition{{
			Name:          "demo",
			UserInvocable: true,
		}}},
		nil,
	)

	if got := runtime.agenticTaskTurnTimeout(ChatRequest{Content: "hello"}); got != 2*time.Minute {
		t.Fatalf("regular chat timeout = %s, want 2m", got)
	}
	if got := runtime.agenticTaskTurnTimeout(ChatRequest{Content: "/demo hello"}); got != 2*time.Minute {
		t.Fatalf("regular skill timeout = %s, want 2m", got)
	}
}

func TestLLMCallTimeoutExtendsLongRunningSkill(t *testing.T) {
	config := LLMGovernanceConfig{
		ChatTimeout:  45 * time.Second,
		SkillTimeout: 90 * time.Second,
	}.normalized()

	if got := llmCallTimeout(config, LLMScope{}); got != 45*time.Second {
		t.Fatalf("chat llm timeout = %s, want 45s", got)
	}
	if got := llmCallTimeout(config, LLMScope{SkillName: "demo"}); got != 90*time.Second {
		t.Fatalf("regular skill llm timeout = %s, want 90s", got)
	}
	if got := llmCallTimeout(config, LLMScope{SkillName: "spreadsheets", SkillLongRunning: true}); got != longRunningSkillTurnTimeout {
		t.Fatalf("long-running skill llm timeout = %s, want %s", got, longRunningSkillTurnTimeout)
	}
	if got := llmCallTimeout(config, LLMScope{SkillName: "presentations", SkillLongRunning: true}); got <= 5*time.Minute {
		t.Fatalf("long-running presentation llm timeout = %s, should exceed historical 5m job deadline", got)
	}
}
