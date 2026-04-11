package telemetry

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type PerfettoTraceEvent struct {
	Name string         `json:"name"`
	Cat  string         `json:"cat"`
	Ph   string         `json:"ph"`
	Ts   int64          `json:"ts"`
	Pid  int            `json:"pid"`
	Tid  int            `json:"tid"`
	Dur  int64          `json:"dur,omitempty"`
	Args map[string]any `json:"args,omitempty"`
}

type pendingPerfettoSpan struct {
	name      string
	category  string
	startTime int64
	threadID  int
	args      map[string]any
}

type PerfettoSessionTracer struct {
	mu        sync.Mutex
	path      string
	startTime time.Time
	events    []PerfettoTraceEvent
	pending   map[string]pendingPerfettoSpan
	closed    bool
}

func NewPerfettoSessionTracer(path string) (*PerfettoSessionTracer, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	return &PerfettoSessionTracer{
		path:      path,
		startTime: time.Now().UTC(),
		events:    []PerfettoTraceEvent{},
		pending:   map[string]pendingPerfettoSpan{},
	}, nil
}

func (t *PerfettoSessionTracer) Record(event TraceEvent) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return os.ErrClosed
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}

	ts := event.Timestamp.Sub(t.startTime).Microseconds()
	threadID := perfettoThreadID(event.SessionID, event.Name)
	key := perfettoSpanKey(event)
	category := event.Kind
	if category == "" {
		category = "trace"
	}

	switch {
	case strings.HasSuffix(event.Name, ".start"):
		t.pending[key] = pendingPerfettoSpan{
			name:      strings.TrimSuffix(event.Name, ".start"),
			category:  category,
			startTime: ts,
			threadID:  threadID,
			args:      attrsToStringAny(event.Attrs),
		}
	case strings.HasSuffix(event.Name, ".end"):
		pending, ok := t.pending[key]
		if !ok {
			t.events = append(t.events, PerfettoTraceEvent{
				Name: strings.TrimSuffix(event.Name, ".end"),
				Cat:  category,
				Ph:   "i",
				Ts:   ts,
				Pid:  1,
				Tid:  threadID,
				Args: attrsToStringAny(event.Attrs),
			})
			return nil
		}
		delete(t.pending, key)
		merged := pending.args
		for key, value := range event.Attrs {
			merged[key] = value
		}
		t.events = append(t.events, PerfettoTraceEvent{
			Name: pending.name,
			Cat:  pending.category,
			Ph:   "X",
			Ts:   pending.startTime,
			Pid:  1,
			Tid:  pending.threadID,
			Dur:  maxInt64(1, ts-pending.startTime),
			Args: merged,
		})
	default:
		t.events = append(t.events, PerfettoTraceEvent{
			Name: event.Name,
			Cat:  category,
			Ph:   "i",
			Ts:   ts,
			Pid:  1,
			Tid:  threadID,
			Args: attrsToStringAny(event.Attrs),
		})
	}
	return nil
}

func (t *PerfettoSessionTracer) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return nil
	}
	t.closed = true

	for key, pending := range t.pending {
		t.events = append(t.events, PerfettoTraceEvent{
			Name: pending.name,
			Cat:  pending.category,
			Ph:   "X",
			Ts:   pending.startTime,
			Pid:  1,
			Tid:  pending.threadID,
			Dur:  1,
			Args: pending.args,
		})
		delete(t.pending, key)
	}

	file, err := os.Create(t.path)
	if err != nil {
		return err
	}
	defer file.Close()

	payload := map[string]any{
		"traceEvents": t.events,
		"metadata": map[string]any{
			"trace_start_time": t.startTime.Format(time.RFC3339Nano),
			"event_count":      len(t.events),
		},
	}
	return json.NewEncoder(file).Encode(payload)
}

func perfettoSpanKey(event TraceEvent) string {
	if event.Attrs != nil {
		if spanID, ok := event.Attrs["span_id"].(string); ok && strings.TrimSpace(spanID) != "" {
			return spanID
		}
	}
	return fmt.Sprintf("%s:%s", event.SessionID, strings.TrimSuffix(strings.TrimSuffix(event.Name, ".start"), ".end"))
}

func perfettoThreadID(sessionID string, name string) int {
	hasher := fnv.New32a()
	_, _ = hasher.Write([]byte(sessionID + ":" + name))
	return int(hasher.Sum32())
}

func attrsToStringAny(attrs map[string]any) map[string]any {
	if len(attrs) == 0 {
		return nil
	}
	out := make(map[string]any, len(attrs))
	for key, value := range attrs {
		out[key] = value
	}
	return out
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
