package engine

import (
	"context"
	"testing"

	"claude-codex/internal/harness/permissions"
	"claude-codex/internal/harness/state"
	"claude-codex/internal/harness/telemetry"
	toolkit "claude-codex/internal/harness/tools"
)

type telemetryPlanner struct{}

func (telemetryPlanner) Next(context.Context, *state.Session, []toolkit.Descriptor) (Plan, error) {
	return Plan{AssistantText: "done", StopReason: "end_turn"}, nil
}

type captureSessionTracer struct {
	events []telemetry.TraceEvent
}

func (c *captureSessionTracer) Record(event telemetry.TraceEvent) error {
	c.events = append(c.events, event)
	return nil
}

func (c *captureSessionTracer) Close() error { return nil }

func TestEngineRecordsInteractionTelemetry(t *testing.T) {
	registry := toolkit.NewRegistry()
	engine := NewWithDir(telemetryPlanner{}, registry, permissions.NewChecker(permissions.ModeBypass, nil, nil), 2, t.TempDir())
	capture := &captureSessionTracer{}
	engine.SetTelemetryTracer(capture)

	session := state.NewSession(t.TempDir())
	result, err := engine.Run(context.Background(), session, "hello")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Output != "done" {
		t.Fatalf("expected done output, got %#v", result)
	}
	if len(capture.events) == 0 {
		t.Fatal("expected telemetry events to be recorded")
	}

	var sawStart bool
	var sawEnd bool
	for _, event := range capture.events {
		if event.Name == "interaction.start" {
			sawStart = true
		}
		if event.Name == "interaction.end" {
			sawEnd = true
		}
	}
	if !sawStart || !sawEnd {
		t.Fatalf("expected start/end telemetry events, got %#v", capture.events)
	}
}
