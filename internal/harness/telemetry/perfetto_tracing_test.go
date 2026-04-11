package telemetry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPerfettoSessionTracerWritesCompleteEvents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trace.json")
	tracer, err := NewPerfettoSessionTracer(path)
	if err != nil {
		t.Fatalf("NewPerfettoSessionTracer() error = %v", err)
	}

	start := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	end := start.Add(75 * time.Millisecond)
	for _, event := range []TraceEvent{
		{
			Timestamp: start,
			SessionID: "session-1",
			Name:      "interaction.start",
			Kind:      "interaction",
			Attrs:     map[string]any{"span_id": "i1", "prompt_length": 3},
		},
		{
			Timestamp: end,
			SessionID: "session-1",
			Name:      "interaction.end",
			Kind:      "interaction",
			Attrs:     map[string]any{"span_id": "i1", "status": "ok"},
		},
	} {
		if err := tracer.Record(event); err != nil {
			t.Fatalf("Record() error = %v", err)
		}
	}
	if err := tracer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	var payload struct {
		TraceEvents []PerfettoTraceEvent `json:"traceEvents"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if len(payload.TraceEvents) != 1 {
		t.Fatalf("expected one complete trace event, got %d", len(payload.TraceEvents))
	}
	if payload.TraceEvents[0].Dur <= 0 {
		t.Fatalf("expected duration to be set, got %#v", payload.TraceEvents[0])
	}
}
