package budget

import (
	"testing"
	"time"
)

func TestNewBudgetTracker(t *testing.T) {
	tracker := NewBudgetTracker()

	if tracker.ContinuationCount != 0 {
		t.Errorf("Expected ContinuationCount 0, got %d", tracker.ContinuationCount)
	}
	if tracker.LastDeltaTokens != 0 {
		t.Errorf("Expected LastDeltaTokens 0, got %d", tracker.LastDeltaTokens)
	}
	if tracker.LastGlobalTurnTokens != 0 {
		t.Errorf("Expected LastGlobalTurnTokens 0, got %d", tracker.LastGlobalTurnTokens)
	}
	if tracker.StartedAt.IsZero() {
		t.Error("Expected StartedAt to be set")
	}
}

func TestCheckTokenBudget_NoAgent_NoBudget(t *testing.T) {
	tracker := NewBudgetTracker()
	decision := CheckTokenBudget(tracker, "", 0, 1000)

	if decision.GetAction() != "stop" {
		t.Errorf("Expected stop, got %s", decision.GetAction())
	}

	stopDecision, ok := decision.(*StopDecision)
	if !ok {
		t.Fatal("Expected StopDecision")
	}
	if stopDecision.CompletionEvent != nil {
		t.Error("Expected nil CompletionEvent")
	}
}

func TestCheckTokenBudget_WithAgent(t *testing.T) {
	tracker := NewBudgetTracker()
	decision := CheckTokenBudget(tracker, "agent-123", 10000, 1000)

	if decision.GetAction() != "stop" {
		t.Errorf("Expected stop, got %s", decision.GetAction())
	}

	stopDecision, ok := decision.(*StopDecision)
	if !ok {
		t.Fatal("Expected StopDecision")
	}
	if stopDecision.CompletionEvent != nil {
		t.Error("Expected nil CompletionEvent for agent")
	}
}

func TestCheckTokenBudget_UnderThreshold(t *testing.T) {
	tracker := NewBudgetTracker()
	budget := 10000
	turnTokens := 5000 // 50% of budget

	decision := CheckTokenBudget(tracker, "", budget, turnTokens)

	if decision.GetAction() != "continue" {
		t.Errorf("Expected continue, got %s", decision.GetAction())
	}

	continueDecision, ok := decision.(*ContinueDecision)
	if !ok {
		t.Fatal("Expected ContinueDecision")
	}

	if continueDecision.ContinuationCount != 1 {
		t.Errorf("Expected ContinuationCount 1, got %d", continueDecision.ContinuationCount)
	}
	if continueDecision.Pct != 50 {
		t.Errorf("Expected Pct 50, got %d", continueDecision.Pct)
	}
	if continueDecision.TurnTokens != turnTokens {
		t.Errorf("Expected TurnTokens %d, got %d", turnTokens, continueDecision.TurnTokens)
	}
	if continueDecision.Budget != budget {
		t.Errorf("Expected Budget %d, got %d", budget, continueDecision.Budget)
	}
}

func TestCheckTokenBudget_OverThreshold(t *testing.T) {
	tracker := NewBudgetTracker()
	tracker.ContinuationCount = 1 // Simulate previous continuation
	budget := 10000
	turnTokens := 9500 // 95% of budget

	decision := CheckTokenBudget(tracker, "", budget, turnTokens)

	if decision.GetAction() != "stop" {
		t.Errorf("Expected stop, got %s", decision.GetAction())
	}

	stopDecision, ok := decision.(*StopDecision)
	if !ok {
		t.Fatal("Expected StopDecision")
	}
	if stopDecision.CompletionEvent == nil {
		t.Fatal("Expected CompletionEvent")
	}
	if stopDecision.CompletionEvent.Pct != 95 {
		t.Errorf("Expected Pct 95, got %d", stopDecision.CompletionEvent.Pct)
	}
}

func TestCheckTokenBudget_DiminishingReturns(t *testing.T) {
	tracker := NewBudgetTracker()
	budget := 10000

	// First continuation
	CheckTokenBudget(tracker, "", budget, 1000)
	// Second continuation
	CheckTokenBudget(tracker, "", budget, 1400)
	// Third continuation
	CheckTokenBudget(tracker, "", budget, 1700)

	// Fourth check with small delta (diminishing returns)
	decision := CheckTokenBudget(tracker, "", budget, 1900)

	if decision.GetAction() != "stop" {
		t.Errorf("Expected stop due to diminishing returns, got %s", decision.GetAction())
	}

	stopDecision, ok := decision.(*StopDecision)
	if !ok {
		t.Fatal("Expected StopDecision")
	}
	if stopDecision.CompletionEvent == nil {
		t.Fatal("Expected CompletionEvent")
	}
	if !stopDecision.CompletionEvent.DiminishingReturns {
		t.Error("Expected DiminishingReturns to be true")
	}
}

func TestCheckTokenBudget_DurationTracking(t *testing.T) {
	tracker := NewBudgetTracker()
	tracker.StartedAt = time.Now().Add(-5 * time.Second)
	tracker.ContinuationCount = 1

	budget := 10000
	turnTokens := 9500

	decision := CheckTokenBudget(tracker, "", budget, turnTokens)

	stopDecision, ok := decision.(*StopDecision)
	if !ok {
		t.Fatal("Expected StopDecision")
	}
	if stopDecision.CompletionEvent == nil {
		t.Fatal("Expected CompletionEvent")
	}

	// Should be around 5000ms
	if stopDecision.CompletionEvent.DurationMs < 4900 || stopDecision.CompletionEvent.DurationMs > 5100 {
		t.Errorf("Expected DurationMs around 5000, got %d", stopDecision.CompletionEvent.DurationMs)
	}
}

func TestGetBudgetContinuationMessage(t *testing.T) {
	tests := []struct {
		pct        int
		turnTokens int
		budget     int
		expected   string
	}{
		{50, 5000, 10000, "Stopped at 50% of token target (5,000 / 10,000). Keep working — do not summarize."},
		{75, 750000, 1000000, "Stopped at 75% of token target (750,000 / 1,000,000). Keep working — do not summarize."},
		{10, 100, 1000, "Stopped at 10% of token target (100 / 1,000). Keep working — do not summarize."},
	}

	for _, tt := range tests {
		result := GetBudgetContinuationMessage(tt.pct, tt.turnTokens, tt.budget)
		if result != tt.expected {
			t.Errorf("Expected:\n%s\nGot:\n%s", tt.expected, result)
		}
	}
}

func TestFormatNumber(t *testing.T) {
	tests := []struct {
		input    int
		expected string
	}{
		{0, "0"},
		{999, "999"},
		{1000, "1,000"},
		{1234, "1,234"},
		{999999, "999,999"},
		{1000000, "1,000,000"},
		{1234567, "1,234,567"},
		{123456789, "123,456,789"},
	}

	for _, tt := range tests {
		result := formatNumber(tt.input)
		if result != tt.expected {
			t.Errorf("formatNumber(%d) = %s, expected %s", tt.input, result, tt.expected)
		}
	}
}
