package telemetry

import (
	"bytes"
	"io"
	"net/http"
	"testing"
)

type roundTripFunc func(req *http.Request) (*http.Response, error)

func (fn roundTripFunc) Do(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestBigQueryExporterPostsBufferedEvents(t *testing.T) {
	var body []byte
	exporter, err := NewBigQueryExporter(BigQueryExporterOptions{
		Endpoint:    "https://example.com/telemetry",
		ServiceName: "claude-codex",
		BatchSize:   2,
		HTTPClient: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			var readErr error
			body, readErr = io.ReadAll(req.Body)
			if readErr != nil {
				t.Fatalf("ReadAll() error = %v", readErr)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Body:       io.NopCloser(bytes.NewReader(nil)),
			}, nil
		}),
	})
	if err != nil {
		t.Fatalf("NewBigQueryExporter() error = %v", err)
	}

	_ = exporter.Record(TraceEvent{Name: "interaction.start"})
	if err := exporter.Record(TraceEvent{Name: "interaction.end"}); err != nil {
		t.Fatalf("Record() error = %v", err)
	}
	if len(body) == 0 {
		t.Fatal("expected exporter to flush buffered events")
	}
	if !bytes.Contains(body, []byte(`"exporter":"bigquery"`)) {
		t.Fatalf("expected bigquery payload, got %s", string(body))
	}
}
