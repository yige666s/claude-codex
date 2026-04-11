package compact

import (
	"context"
	"testing"
	"time"

	"claude-codex/internal/public/types"
)

func TestNewAutoCompactor(t *testing.T) {
	ac := NewAutoCompactor(nil)

	if ac == nil {
		t.Fatal("Expected non-nil auto-compactor")
	}

	if !ac.config.Enabled {
		t.Error("Should be enabled by default")
	}

	if ac.trackingState == nil {
		t.Error("Tracking state should be initialized")
	}
}

func TestShouldTriggerAutoCompact_BelowThreshold(t *testing.T) {
	config := &AutoCompactConfig{
		Enabled:           true,
		Model:             "claude-sonnet-4-6",
		ContextWindowSize: 200000,
		CurrentTokenUsage: 100000, // Below threshold
	}

	ac := NewAutoCompactor(config)
	should := ac.ShouldTriggerAutoCompact()

	if should {
		t.Error("Should not trigger when below threshold")
	}
}

func TestShouldTriggerAutoCompact_AboveThreshold(t *testing.T) {
	config := &AutoCompactConfig{
		Enabled:                true,
		Model:                  "claude-sonnet-4-6",
		ContextWindowSize:      200000,
		CurrentTokenUsage:      180000, // Above threshold
		MaxConsecutiveFailures: 3,
	}

	ac := NewAutoCompactor(config)
	should := ac.ShouldTriggerAutoCompact()

	if !should {
		t.Error("Should trigger when above threshold")
	}
}

func TestShouldTriggerAutoCompact_Disabled(t *testing.T) {
	config := &AutoCompactConfig{
		Enabled:           false,
		Model:             "claude-sonnet-4-6",
		ContextWindowSize: 200000,
		CurrentTokenUsage: 180000,
	}

	ac := NewAutoCompactor(config)
	should := ac.ShouldTriggerAutoCompact()

	if should {
		t.Error("Should not trigger when disabled")
	}
}

func TestShouldTriggerAutoCompact_TooManyFailures(t *testing.T) {
	config := &AutoCompactConfig{
		Enabled:                true,
		Model:                  "claude-sonnet-4-6",
		ContextWindowSize:      200000,
		CurrentTokenUsage:      180000,
		MaxConsecutiveFailures: 3,
	}

	ac := NewAutoCompactor(config)

	// Record failures
	ac.RecordCompactionFailure()
	ac.RecordCompactionFailure()
	ac.RecordCompactionFailure()

	should := ac.ShouldTriggerAutoCompact()

	if should {
		t.Error("Should not trigger after max failures")
	}
}

func TestCalculateTokenWarningState(t *testing.T) {
	config := &AutoCompactConfig{
		Enabled:           true,
		Model:             "claude-sonnet-4-6",
		ContextWindowSize: 200000,
		CurrentTokenUsage: 180000,
	}

	ac := NewAutoCompactor(config)
	state := ac.CalculateTokenWarningState()

	if state == nil {
		t.Fatal("Expected non-nil state")
	}

	if !state.IsAboveAutoCompactThreshold {
		t.Error("Should be above auto-compact threshold")
	}

	if state.PercentLeft < 0 || state.PercentLeft > 100 {
		t.Errorf("Invalid percent left: %d", state.PercentLeft)
	}
}

func TestRecordCompactionSuccess(t *testing.T) {
	ac := NewAutoCompactor(nil)

	// Record some failures first
	ac.RecordCompactionFailure()
	ac.RecordCompactionFailure()

	if ac.trackingState.ConsecutiveFailures != 2 {
		t.Error("Should have 2 failures")
	}

	// Record success
	ac.RecordCompactionSuccess()

	if ac.trackingState.ConsecutiveFailures != 0 {
		t.Error("Failures should be reset to 0")
	}

	if !ac.trackingState.Compacted {
		t.Error("Compacted flag should be true")
	}
}

func TestRecordCompactionFailure(t *testing.T) {
	ac := NewAutoCompactor(nil)

	ac.RecordCompactionFailure()
	if ac.trackingState.ConsecutiveFailures != 1 {
		t.Error("Should have 1 failure")
	}

	ac.RecordCompactionFailure()
	if ac.trackingState.ConsecutiveFailures != 2 {
		t.Error("Should have 2 failures")
	}
}

func TestIncrementTurn(t *testing.T) {
	ac := NewAutoCompactor(nil)

	ac.trackingState.Compacted = true
	ac.IncrementTurn("turn-1")

	if ac.trackingState.TurnCounter != 1 {
		t.Error("Turn counter should be 1")
	}

	if ac.trackingState.TurnID != "turn-1" {
		t.Error("Turn ID should be set")
	}

	if ac.trackingState.Compacted {
		t.Error("Compacted flag should be reset")
	}

	ac.IncrementTurn("turn-2")
	if ac.trackingState.TurnCounter != 2 {
		t.Error("Turn counter should be 2")
	}
}

func TestUpdateTokenUsage(t *testing.T) {
	ac := NewAutoCompactor(nil)

	ac.UpdateTokenUsage(50000)
	if ac.config.CurrentTokenUsage != 50000 {
		t.Error("Token usage should be updated")
	}

	ac.UpdateTokenUsage(100000)
	if ac.config.CurrentTokenUsage != 100000 {
		t.Error("Token usage should be updated")
	}
}

func TestCompactMessages_NoCompactionNeeded(t *testing.T) {
	config := &AutoCompactConfig{
		Enabled:           true,
		Model:             "claude-sonnet-4-6",
		ContextWindowSize: 200000,
		CurrentTokenUsage: 50000, // Below threshold
	}

	ac := NewAutoCompactor(config)

	messages := []types.Message{
		{
			Type: types.MessageTypeUser,
			Content: []types.ContentBlock{
				{Type: "text", Text: "Hello"},
			},
		},
	}

	ctx := context.Background()
	result, err := ac.CompactMessages(ctx, messages)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if result.CompactedCount != 0 {
		t.Error("Should not compact when below threshold")
	}

	if result.TokensSaved != 0 {
		t.Error("Should not save tokens")
	}
}

func TestCompactMessages_Microcompact(t *testing.T) {
	config := &AutoCompactConfig{
		Enabled:                true,
		Model:                  "claude-sonnet-4-6",
		ContextWindowSize:      200000,
		CurrentTokenUsage:      180000, // Above threshold
		MaxConsecutiveFailures: 3,
	}

	ac := NewAutoCompactor(config)

	oldTime := time.Now().Add(-10 * time.Minute)
	messages := []types.Message{
		{
			Type:      types.MessageTypeAssistant,
			Timestamp: oldTime,
			Content: []types.ContentBlock{
				{Type: "tool_use", ID: "tool-1", Name: "Read"},
			},
		},
		{
			Type: types.MessageTypeUser,
			Content: []types.ContentBlock{
				{Type: "tool_result", ToolUseID: "tool-1", Content: "large content here"},
			},
		},
	}

	ctx := context.Background()

	t.Logf("Should trigger: %v", ac.ShouldTriggerAutoCompact())
	t.Logf("Threshold: %d, Current: %d", ac.GetAutoCompactThreshold(), ac.config.CurrentTokenUsage)

	result, err := ac.CompactMessages(ctx, messages)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	t.Logf("CompactedCount: %d, TokensSaved: %d", result.CompactedCount, result.TokensSaved)

	if result.CompactedCount == 0 {
		t.Error("Should compact messages")
	}

	if result.TokensSaved == 0 {
		t.Error("Should save tokens")
	}

	if ac.trackingState.ConsecutiveFailures != 0 {
		t.Error("Should reset failures on success")
	}
}

func TestGetEffectiveContextWindowSize(t *testing.T) {
	config := &AutoCompactConfig{
		Model:             "claude-sonnet-4-6",
		ContextWindowSize: 200000,
	}

	ac := NewAutoCompactor(config)
	size := ac.GetEffectiveContextWindowSize()

	if size <= 0 {
		t.Error("Effective size should be positive")
	}

	if size >= config.ContextWindowSize {
		t.Error("Effective size should be less than total")
	}
}

func TestGetAutoCompactThreshold(t *testing.T) {
	config := &AutoCompactConfig{
		Model:             "claude-sonnet-4-6",
		ContextWindowSize: 200000,
	}

	ac := NewAutoCompactor(config)
	threshold := ac.GetAutoCompactThreshold()

	if threshold <= 0 {
		t.Error("Threshold should be positive")
	}

	effectiveSize := ac.GetEffectiveContextWindowSize()
	if threshold >= effectiveSize {
		t.Error("Threshold should be less than effective size")
	}
}

func TestSetEnabled(t *testing.T) {
	ac := NewAutoCompactor(nil)

	if !ac.IsEnabled() {
		t.Error("Should be enabled by default")
	}

	ac.SetEnabled(false)
	if ac.IsEnabled() {
		t.Error("Should be disabled")
	}

	ac.SetEnabled(true)
	if !ac.IsEnabled() {
		t.Error("Should be enabled")
	}
}

func TestReset(t *testing.T) {
	ac := NewAutoCompactor(nil)

	ac.IncrementTurn("turn-1")
	ac.RecordCompactionFailure()
	ac.RecordCompactionSuccess()

	ac.Reset()

	if ac.trackingState.TurnCounter != 0 {
		t.Error("Turn counter should be reset")
	}

	if ac.trackingState.ConsecutiveFailures != 0 {
		t.Error("Failures should be reset")
	}

	if ac.trackingState.Compacted {
		t.Error("Compacted flag should be reset")
	}
}

func TestFormatWarningMessage(t *testing.T) {
	ac := NewAutoCompactor(nil)

	tests := []struct {
		name     string
		state    *TokenWarningState
		contains string
	}{
		{
			name: "blocking limit",
			state: &TokenWarningState{
				PercentLeft:       5,
				IsAtBlockingLimit: true,
			},
			contains: "full",
		},
		{
			name: "error threshold",
			state: &TokenWarningState{
				PercentLeft:           10,
				IsAboveErrorThreshold: true,
			},
			contains: "nearly full",
		},
		{
			name: "warning threshold",
			state: &TokenWarningState{
				PercentLeft:             20,
				IsAboveWarningThreshold: true,
			},
			contains: "filling up",
		},
		{
			name: "auto-compact threshold",
			state: &TokenWarningState{
				PercentLeft:                 30,
				IsAboveAutoCompactThreshold: true,
			},
			contains: "Auto-compaction",
		},
		{
			name: "no warning",
			state: &TokenWarningState{
				PercentLeft: 80,
			},
			contains: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := ac.FormatWarningMessage(tt.state)

			if tt.contains == "" {
				if msg != "" {
					t.Errorf("Expected empty message, got %q", msg)
				}
			} else {
				if msg == "" {
					t.Error("Expected non-empty message")
				}
				// Simple contains check
				found := false
				for i := 0; i <= len(msg)-len(tt.contains); i++ {
					if msg[i:i+len(tt.contains)] == tt.contains {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Expected message to contain %q, got %q", tt.contains, msg)
				}
			}
		})
	}
}
