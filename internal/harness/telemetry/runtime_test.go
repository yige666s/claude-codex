package telemetry

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseExporterListDedupesAndNormalizes(t *testing.T) {
	got := ParseExporterList(" stdout, jsonl ,stdout,perfetto ")
	want := []string{"stdout", "jsonl", "perfetto"}
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected %v, got %v", want, got)
		}
	}
}

func TestRuntimeBuildsJSONLTracerByDefaultWhenEnabled(t *testing.T) {
	home := t.TempDir()
	runtime, err := NewRuntime(RuntimeOptions{
		Enabled:     true,
		HomeDir:     home,
		ServiceName: "claude-codex",
		Now: func() time.Time {
			return time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
		},
	})
	if err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}
	RecordEvent(runtime.Tracer(), "session-1", "interaction.start", "interaction", map[string]any{"prompt": "hello"})
	if err := runtime.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	files, err := filepath.Glob(filepath.Join(home, "telemetry", "sessions", "*.jsonl"))
	if err != nil {
		t.Fatalf("Glob() error = %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected one trace file, got %v", files)
	}
	data, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !strings.Contains(string(data), `"sequence":0`) {
		t.Fatalf("expected sequenced event in %q", string(data))
	}
}

func TestStdoutSessionTracerWritesJSONLines(t *testing.T) {
	var buf bytes.Buffer
	tracer := NewStdoutSessionTracer(&buf)
	if err := tracer.Record(TraceEvent{Name: "interaction.start"}); err != nil {
		t.Fatalf("Record() error = %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if decoded["name"] != "interaction.start" {
		t.Fatalf("expected interaction.start, got %v", decoded["name"])
	}
}
