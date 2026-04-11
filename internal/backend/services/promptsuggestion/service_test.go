package promptsuggestion

import "testing"

func TestSuppressReasonAndEnablement(t *testing.T) {
	if ShouldEnablePromptSuggestion(true, false, true) {
		t.Fatal("non-interactive should disable prompt suggestion")
	}
	reason := GetSuggestionSuppressReason(AppState{PromptSuggestionEnabled: true, PermissionMode: "plan"})
	if reason == nil || *reason != SuppressPlanMode {
		t.Fatalf("unexpected suppress reason: %v", reason)
	}
}
