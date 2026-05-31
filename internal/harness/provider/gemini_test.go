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

func TestSupportsGoogleSearchGroundingModels(t *testing.T) {
	cases := []struct {
		provider string
		model    string
		want     bool
	}{
		{"vertex", "gemini-2.0-flash", true},
		{"vertex", "gemini-2.5-pro", true},
		{"vertex", "gemini-2.5-flash-lite", true},
		{"vertex", "projects/p/locations/us-central1/publishers/google/models/gemini-3.1-pro-preview", true},
		{"vertex", "gemini-live-2.5-flash-preview-native-audio-09-2025", true},
		{"gemini", "models/gemini-2.5-flash", true},
		{"vertex", "gemini-1.5-pro", false},
		{"openai", "gemini-2.5-flash", false},
	}
	for _, tc := range cases {
		if got := SupportsGoogleSearchGrounding(tc.provider, tc.model); got != tc.want {
			t.Fatalf("SupportsGoogleSearchGrounding(%q, %q) = %v, want %v", tc.provider, tc.model, got, tc.want)
		}
	}
}

func TestGoogleSearchGroundingModeHonorsConfigOff(t *testing.T) {
	mode := googleSearchGroundingMode(
		MessageRequest{GoogleSearchGrounding: GoogleSearchGroundingAlways},
		Config{GoogleSearchGrounding: GoogleSearchGroundingOff},
	)
	if mode != GoogleSearchGroundingOff {
		t.Fatalf("mode = %q, want off", mode)
	}
	mode = googleSearchGroundingMode(
		MessageRequest{GoogleSearchGrounding: GoogleSearchGroundingOff},
		Config{GoogleSearchGrounding: GoogleSearchGroundingAlways},
	)
	if mode != GoogleSearchGroundingOff {
		t.Fatalf("request off should override config always, got %q", mode)
	}
	if mode := googleSearchGroundingMode(MessageRequest{}, Config{}); mode != GoogleSearchGroundingOff {
		t.Fatalf("empty mode = %q, want off", mode)
	}
}
