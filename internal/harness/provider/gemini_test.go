package provider

import (
	"strings"
	"testing"
)

func TestGeminiThinkingConfigForRequest(t *testing.T) {
	flash := geminiThinkingConfigForRequest(MessageRequest{
		Model:          "gemini-2.5-flash",
		ThinkingConfig: &ThinkingConfig{Enabled: true},
	})
	if flash == nil || flash.ThinkingBudget == nil || *flash.ThinkingBudget != -1 {
		t.Fatalf("flash thinking config = %#v", flash)
	}

	pro := geminiThinkingConfigForRequest(MessageRequest{
		Model:          "projects/p/locations/us-central1/publishers/google/models/gemini-2.5-pro",
		ThinkingConfig: &ThinkingConfig{Enabled: true, BudgetTokens: 4096},
	})
	if pro == nil || pro.ThinkingBudget == nil || *pro.ThinkingBudget != 4096 {
		t.Fatalf("pro thinking config = %#v", pro)
	}

	gemini3 := geminiThinkingConfigForRequest(MessageRequest{
		Model:          "gemini-3.1-flash-lite",
		ThinkingConfig: &ThinkingConfig{Enabled: true},
	})
	if gemini3 == nil || gemini3.ThinkingLevel != "HIGH" {
		t.Fatalf("gemini 3 thinking config = %#v", gemini3)
	}

	unsupported := geminiThinkingConfigForRequest(MessageRequest{
		Model:          "gemini-1.5-flash",
		ThinkingConfig: &ThinkingConfig{Enabled: true},
	})
	if unsupported != nil {
		t.Fatalf("unsupported model should not receive thinking config: %#v", unsupported)
	}
}

func TestGeminiUnifiedResponseRejectsEmptyCandidate(t *testing.T) {
	_, err := geminiUnifiedResponse("gemini-test", "vertex", geminiResponse{
		Candidates: []geminiCandidate{{
			FinishReason:  "MAX_TOKENS",
			SafetyRatings: []interface{}{map[string]interface{}{"category": "HARM_CATEGORY_DANGEROUS_CONTENT"}},
		}},
		UsageMetadata: geminiUsageMetadata{
			PromptTokenCount:     11,
			CandidatesTokenCount: 0,
		},
	})
	if err == nil {
		t.Fatal("expected empty candidate to fail")
	}
	text := err.Error()
	for _, want := range []string{"empty response", "finish_reason=MAX_TOKENS", "prompt_tokens=11", "output_tokens=0", "safety_ratings=1"} {
		if !strings.Contains(text, want) {
			t.Fatalf("error %q does not contain %q", text, want)
		}
	}
}
