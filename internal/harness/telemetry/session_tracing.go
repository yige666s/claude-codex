package telemetry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type TraceEvent struct {
	Timestamp time.Time      `json:"timestamp"`
	SessionID string         `json:"session_id,omitempty"`
	Name      string         `json:"name"`
	Kind      string         `json:"kind,omitempty"`
	Attrs     map[string]any `json:"attrs,omitempty"`
}

type SessionTracer interface {
	Record(event TraceEvent) error
	Close() error
}

type NoopSessionTracer struct{}

func (NoopSessionTracer) Record(TraceEvent) error { return nil }
func (NoopSessionTracer) Close() error            { return nil }

type JSONLSessionTracer struct {
	mu      sync.Mutex
	file    *os.File
	encoder *json.Encoder
	closed  bool
}

func NewJSONLSessionTracer(path string) (*JSONLSessionTracer, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}

	return &JSONLSessionTracer{
		file:    file,
		encoder: json.NewEncoder(file),
	}, nil
}

func (t *JSONLSessionTracer) Record(event TraceEvent) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return os.ErrClosed
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}
	return t.encoder.Encode(event)
}

func (t *JSONLSessionTracer) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return nil
	}
	t.closed = true
	return t.file.Close()
}
