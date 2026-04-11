package telemetry

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type RuntimeOptions struct {
	Enabled        bool
	Exporters      string
	Endpoint       string
	Insecure       bool
	ServiceName    string
	HomeDir        string
	Stdout         io.Writer
	TracePath      string
	PerfettoPath   string
	BetaEnabled    bool
	LogUserPrompts bool
	Now            func() time.Time
	HTTPClient     HTTPDoer
}

type Runtime struct {
	tracer  SessionTracer
	enabled bool
}

type MultiSessionTracer struct {
	tracers []SessionTracer
}

type StdoutSessionTracer struct {
	mu      sync.Mutex
	writer  io.Writer
	encoder *json.Encoder
	closed  bool
}

type SequencedSessionTracer struct {
	next     SessionTracer
	mu       sync.Mutex
	sequence int64
	now      func() time.Time
}

func NewRuntime(options RuntimeOptions) (*Runtime, error) {
	if !options.Enabled {
		return &Runtime{tracer: NoopSessionTracer{}, enabled: false}, nil
	}

	if strings.TrimSpace(options.ServiceName) == "" {
		options.ServiceName = "claude-codex"
	}
	if options.Now == nil {
		options.Now = func() time.Time { return time.Now().UTC() }
	}

	exporters := ParseExporterList(options.Exporters)
	if len(exporters) == 0 {
		exporters = []string{"jsonl"}
	}

	tracers := make([]SessionTracer, 0, len(exporters))
	for _, exporter := range exporters {
		tracer, err := newExporterTracer(exporter, options)
		if err != nil {
			return nil, err
		}
		tracers = append(tracers, tracer)
	}

	var combined SessionTracer = MultiSessionTracer{tracers: tracers}
	combined = SequencedSessionTracer{
		next: combined,
		now:  options.Now,
	}
	if options.BetaEnabled || IsBetaTracingEnabled() {
		combined = NewBetaSessionTracer(combined, BetaOptions{
			Enabled:        true,
			LogUserPrompts: options.LogUserPrompts,
		})
	}

	return &Runtime{tracer: combined, enabled: true}, nil
}

func (r *Runtime) Tracer() SessionTracer {
	if r == nil {
		return NoopSessionTracer{}
	}
	if r.tracer == nil {
		return NoopSessionTracer{}
	}
	return r.tracer
}

func (r *Runtime) IsEnabled() bool {
	return r != nil && r.enabled
}

func (r *Runtime) Close() error {
	if r == nil || r.tracer == nil {
		return nil
	}
	return r.tracer.Close()
}

func ParseExporterList(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	rawParts := strings.Split(value, ",")
	result := make([]string, 0, len(rawParts))
	seen := map[string]bool{}
	for _, part := range rawParts {
		exporter := normalizeExporter(part)
		if exporter == "" || seen[exporter] {
			continue
		}
		seen[exporter] = true
		result = append(result, exporter)
	}
	return result
}

func NewStdoutSessionTracer(writer io.Writer) *StdoutSessionTracer {
	if writer == nil {
		writer = os.Stdout
	}
	return &StdoutSessionTracer{
		writer:  writer,
		encoder: json.NewEncoder(writer),
	}
}

func (t *StdoutSessionTracer) Record(event TraceEvent) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return os.ErrClosed
	}
	return t.encoder.Encode(event)
}

func (t *StdoutSessionTracer) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.closed = true
	return nil
}

func (t MultiSessionTracer) Record(event TraceEvent) error {
	var errs []error
	for _, tracer := range t.tracers {
		if tracer == nil {
			continue
		}
		if err := tracer.Record(cloneTraceEvent(event)); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (t MultiSessionTracer) Close() error {
	var errs []error
	for _, tracer := range t.tracers {
		if tracer == nil {
			continue
		}
		if err := tracer.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (t SequencedSessionTracer) Record(event TraceEvent) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if event.Timestamp.IsZero() {
		event.Timestamp = t.now()
	}
	if event.Attrs == nil {
		event.Attrs = map[string]any{}
	}
	event.Attrs["sequence"] = t.sequence
	t.sequence++
	return t.next.Record(event)
}

func (t SequencedSessionTracer) Close() error {
	return t.next.Close()
}

func RecordEvent(tracer SessionTracer, sessionID string, name string, kind string, attrs map[string]any) {
	if tracer == nil {
		return
	}
	_ = tracer.Record(TraceEvent{
		SessionID: sessionID,
		Name:      name,
		Kind:      kind,
		Attrs:     attrs,
	})
}

func newExporterTracer(exporter string, options RuntimeOptions) (SessionTracer, error) {
	switch exporter {
	case "stdout":
		return NewStdoutSessionTracer(options.Stdout), nil
	case "jsonl":
		path := strings.TrimSpace(options.TracePath)
		if path == "" {
			path = filepath.Join(options.HomeDir, "telemetry", "sessions", fmt.Sprintf("trace-%d.jsonl", options.Now().UnixNano()))
		}
		return NewJSONLSessionTracer(path)
	case "perfetto":
		path := strings.TrimSpace(options.PerfettoPath)
		if path == "" {
			path = filepath.Join(options.HomeDir, "traces", fmt.Sprintf("trace-%d.json", options.Now().UnixNano()))
		}
		return NewPerfettoSessionTracer(path)
	case "bigquery":
		return NewBigQueryExporter(BigQueryExporterOptions{
			Endpoint:    options.Endpoint,
			ServiceName: options.ServiceName,
			HTTPClient:  options.HTTPClient,
			Insecure:    options.Insecure,
		})
	case "otlp", "jaeger":
		return NewRemoteSessionTracer(RemoteSessionTracerOptions{
			Endpoint:    options.Endpoint,
			Exporter:    exporter,
			ServiceName: options.ServiceName,
			HTTPClient:  options.HTTPClient,
			Insecure:    options.Insecure,
		})
	default:
		return nil, fmt.Errorf("unsupported telemetry exporter %q", exporter)
	}
}

func normalizeExporter(value string) string {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "", "none":
		return ""
	case "stdout", "jsonl", "perfetto", "bigquery", "otlp", "jaeger":
		return strings.TrimSpace(strings.ToLower(value))
	default:
		return strings.TrimSpace(strings.ToLower(value))
	}
}

func cloneTraceEvent(event TraceEvent) TraceEvent {
	clone := event
	if len(event.Attrs) > 0 {
		clone.Attrs = make(map[string]any, len(event.Attrs))
		for key, value := range event.Attrs {
			clone.Attrs[key] = value
		}
	}
	return clone
}
