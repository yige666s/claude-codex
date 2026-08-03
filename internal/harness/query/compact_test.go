package query

import (
	"context"
	"strings"
	"testing"

	"claude-codex/internal/public/types"
)

func TestLocalCompactServiceUsesSummaryWhenStructuralCompactionIsInsufficient(t *testing.T) {
	messages := make([]types.Message, 0, 9)
	for i := 0; i < 9; i++ {
		messages = append(messages, types.Message{
			Type:    types.MessageTypeUser,
			UUID:    types.UUID(),
			Content: []types.ContentBlock{{Type: "text", Text: strings.Repeat(string(rune('a'+i)), 100_000)}},
		})
	}
	called := false
	service := NewLocalCompactServiceWithSummarizer("claude-sonnet-4-6", func(_ context.Context, overflow []types.Message) (string, error) {
		called = true
		if len(overflow) == 0 || len(overflow) >= len(messages) {
			t.Fatalf("unexpected summary range: %d", len(overflow))
		}
		return "user goal and completed work", nil
	})

	result, err := service.Compact(context.Background(), messages)
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	if !called {
		t.Fatal("expected full summary generation")
	}
	if len(result.Messages) >= len(messages) {
		t.Fatalf("expected reduced message history, got %d messages", len(result.Messages))
	}
	if len(result.Messages) < 2 || !result.Messages[1].IsCompactSummary {
		t.Fatalf("expected compact summary message, got %#v", result.Messages)
	}
	if got := result.Messages[len(result.Messages)-1].UUID; got != messages[len(messages)-1].UUID {
		t.Fatalf("latest message was not preserved: %q", got)
	}
}

func TestLocalCompactServiceReactiveCompactForcesReductionBelowEstimatedThreshold(t *testing.T) {
	messages := make([]types.Message, 0, 12)
	for i := 0; i < 12; i++ {
		messages = append(messages, types.Message{
			Type:    types.MessageTypeUser,
			UUID:    types.UUID(),
			Content: []types.ContentBlock{{Type: "text", Text: strings.Repeat(string(rune('a'+i)), 4000)}},
		})
	}
	called := false
	service := NewLocalCompactServiceWithSummarizer("claude-sonnet-4-6", func(_ context.Context, overflow []types.Message) (string, error) {
		called = true
		if len(overflow) == 0 {
			t.Fatal("expected an overflow prefix to summarize")
		}
		return "preserved earlier context", nil
	})

	result, err := service.ReactiveCompact(context.Background(), messages)
	if err != nil {
		t.Fatalf("reactive compact: %v", err)
	}
	if !called {
		t.Fatal("provider rejection must force summary generation below the estimated threshold")
	}
	if estimateMessageTokens(result.Messages) >= estimateMessageTokens(messages) {
		t.Fatalf("reactive compact did not reduce context: before=%d after=%d", estimateMessageTokens(messages), estimateMessageTokens(result.Messages))
	}
}

func TestFinalContextTokensFromLastResponseUsesPopulatedContext(t *testing.T) {
	messages := []types.Message{{
		Type:    types.MessageTypeUser,
		Content: []types.ContentBlock{{Type: "text", Text: strings.Repeat("x", 400)}},
	}}
	if got := finalContextTokensFromLastResponse(messages); got <= 0 {
		t.Fatalf("expected positive context token estimate, got %d", got)
	}
}
