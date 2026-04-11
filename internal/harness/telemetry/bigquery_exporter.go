package telemetry

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

type RemoteSessionTracerOptions struct {
	Endpoint    string
	Exporter    string
	ServiceName string
	Insecure    bool
	HTTPClient  HTTPDoer
	BatchSize   int
}

type BigQueryExporterOptions struct {
	Endpoint    string
	ServiceName string
	Insecure    bool
	HTTPClient  HTTPDoer
	BatchSize   int
}

type RemoteSessionTracer struct {
	mu          sync.Mutex
	endpoint    string
	exporter    string
	serviceName string
	client      HTTPDoer
	batchSize   int
	buffer      []TraceEvent
	closed      bool
}

type BigQueryExporter struct {
	remote *RemoteSessionTracer
}

func NewRemoteSessionTracer(options RemoteSessionTracerOptions) (*RemoteSessionTracer, error) {
	if strings.TrimSpace(options.Endpoint) == "" {
		return nil, fmt.Errorf("%s telemetry endpoint is required", options.Exporter)
	}
	if options.HTTPClient == nil {
		options.HTTPClient = &http.Client{Timeout: 5 * time.Second}
	}
	if options.BatchSize <= 0 {
		options.BatchSize = 25
	}
	return &RemoteSessionTracer{
		endpoint:    options.Endpoint,
		exporter:    options.Exporter,
		serviceName: options.ServiceName,
		client:      options.HTTPClient,
		batchSize:   options.BatchSize,
		buffer:      make([]TraceEvent, 0, options.BatchSize),
	}, nil
}

func NewBigQueryExporter(options BigQueryExporterOptions) (*BigQueryExporter, error) {
	remote, err := NewRemoteSessionTracer(RemoteSessionTracerOptions{
		Endpoint:    options.Endpoint,
		Exporter:    "bigquery",
		ServiceName: options.ServiceName,
		Insecure:    options.Insecure,
		HTTPClient:  options.HTTPClient,
		BatchSize:   options.BatchSize,
	})
	if err != nil {
		return nil, err
	}
	return &BigQueryExporter{remote: remote}, nil
}

func (t *RemoteSessionTracer) Record(event TraceEvent) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return http.ErrServerClosed
	}
	t.buffer = append(t.buffer, cloneTraceEvent(event))
	if len(t.buffer) < t.batchSize {
		return nil
	}
	return t.flushLocked()
}

func (t *RemoteSessionTracer) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return nil
	}
	t.closed = true
	return t.flushLocked()
}

func (t *RemoteSessionTracer) flushLocked() error {
	if len(t.buffer) == 0 {
		return nil
	}

	payload := map[string]any{
		"exporter":     t.exporter,
		"service_name": t.serviceName,
		"events":       t.buffer,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, t.endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s telemetry export failed: status %s", t.exporter, resp.Status)
	}

	t.buffer = t.buffer[:0]
	return nil
}

func (e *BigQueryExporter) Record(event TraceEvent) error {
	return e.remote.Record(event)
}

func (e *BigQueryExporter) Close() error {
	if e == nil || e.remote == nil {
		return nil
	}
	return e.remote.Close()
}
