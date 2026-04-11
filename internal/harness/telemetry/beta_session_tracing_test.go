package telemetry

import (
	"testing"
)

type captureTracer struct {
	events []TraceEvent
}

func (c *captureTracer) Record(event TraceEvent) error {
	c.events = append(c.events, event)
	return nil
}

func (c *captureTracer) Close() error { return nil }

func TestBetaSessionTracerRedactsPromptWhenDisabled(t *testing.T) {
	capture := &captureTracer{}
	tracer := NewBetaSessionTracer(capture, BetaOptions{Enabled: true, LogUserPrompts: false})

	if err := tracer.Record(TraceEvent{
		Name: "interaction.start",
		Attrs: map[string]any{
			"prompt": "secret prompt",
		},
	}); err != nil {
		t.Fatalf("Record() error = %v", err)
	}

	got := capture.events[0].Attrs["prompt"]
	if got != "<REDACTED>" {
		t.Fatalf("expected prompt redaction, got %v", got)
	}
}

func TestBetaSessionTracerDedupesSystemPrompt(t *testing.T) {
	capture := &captureTracer{}
	tracer := NewBetaSessionTracer(capture, BetaOptions{Enabled: true, LogUserPrompts: true})

	for i := 0; i < 2; i++ {
		if err := tracer.Record(TraceEvent{
			Name: "planner.turn.start",
			Attrs: map[string]any{
				"system_prompt": "same prompt",
			},
		}); err != nil {
			t.Fatalf("Record() error = %v", err)
		}
	}

	first := capture.events[0].Attrs["system_prompt"]
	second := capture.events[1].Attrs["system_prompt"]
	if first != "same prompt" {
		t.Fatalf("expected first system prompt to pass through, got %v", first)
	}
	if second != "<DEDUPED>" {
		t.Fatalf("expected second system prompt to dedupe, got %v", second)
	}
}

func TestTruncateContent(t *testing.T) {
	got, truncated := TruncateContent("abcdefgh", 5)
	if !truncated || got != "ab..." {
		t.Fatalf("expected truncation, got %q truncated=%t", got, truncated)
	}
}
