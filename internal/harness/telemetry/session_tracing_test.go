package telemetry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestJSONLSessionTracerWritesEvents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "traces", "session.jsonl")

	tracer, err := NewJSONLSessionTracer(path)
	if err != nil {
		t.Fatalf("NewJSONLSessionTracer() error = %v", err)
	}

	err = tracer.Record(TraceEvent{
		Timestamp: time.Date(2026, 4, 9, 13, 0, 0, 0, time.UTC),
		SessionID: "session-1",
		Name:      "interaction.start",
		Kind:      "interaction",
		Attrs: map[string]any{
			"prompt_length": 42,
		},
	})
	if err != nil {
		t.Fatalf("Record() error = %v", err)
	}
	if err := tracer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	text := string(data)
	for _, want := range []string{`"session_id":"session-1"`, `"name":"interaction.start"`, `"prompt_length":42`} {
		if !strings.Contains(text, want) {
			t.Fatalf("trace output missing %q in %q", want, text)
		}
	}
}

func TestJSONLSessionTracerRejectsWritesAfterClose(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trace.jsonl")

	tracer, err := NewJSONLSessionTracer(path)
	if err != nil {
		t.Fatalf("NewJSONLSessionTracer() error = %v", err)
	}
	if err := tracer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	err = tracer.Record(TraceEvent{Name: "after-close"})
	if err == nil {
		t.Fatal("Record() after Close() error = nil, want error")
	}
}
