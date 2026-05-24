package provider

import "testing"

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
