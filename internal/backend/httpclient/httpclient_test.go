package httpclient

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestJSONDecodesSuccessAndSendsHeaders(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("Content-Type = %q", r.Header.Get("Content-Type"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	client := New(WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))))
	var out struct {
		OK bool `json:"ok"`
	}
	if err := client.JSON(context.Background(), http.MethodPost, server.URL, map[string]string{"hello": "world"}, &out, WithBearer("token")); err != nil {
		t.Fatal(err)
	}
	if !out.OK {
		t.Fatalf("out.OK = false")
	}
	if gotAuth != "Bearer token" {
		t.Fatalf("Authorization = %q", gotAuth)
	}
}

func TestJSONReturnsStatusErrorBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusBadGateway)
	}))
	defer server.Close()

	client := New(WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))), WithRetry(NoRetry()))
	err := client.JSON(context.Background(), http.MethodGet, server.URL, nil, nil)
	var statusErr *StatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("expected StatusError, got %T", err)
	}
	if statusErr.StatusCode != http.StatusBadGateway {
		t.Fatalf("StatusCode = %d", statusErr.StatusCode)
	}
	if statusErr.Body == "" {
		t.Fatalf("Body is empty")
	}
}

func TestRetryHonorsRetryAfterAndObserver(t *testing.T) {
	var attempts atomic.Int32
	var observed atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) == 1 {
			w.Header().Set("Retry-After", "1")
			http.Error(w, "try again", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	client := New(
		WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))),
		WithRetry(RetryPolicy{
			MaxAttempts: 2,
			BaseDelay:   time.Nanosecond,
			MaxDelay:    time.Nanosecond,
			Methods:     map[string]bool{http.MethodGet: true},
			Statuses:    map[int]bool{http.StatusServiceUnavailable: true},
			Now:         func() time.Time { return time.Now().Add(2 * time.Second) },
		}),
		WithObserver(ObserverFunc(func(metrics RequestMetrics) {
			observed.Add(1)
		})),
	)
	if err := client.JSON(context.Background(), http.MethodGet, server.URL, nil, nil); err != nil {
		t.Fatal(err)
	}
	if attempts.Load() != 2 {
		t.Fatalf("attempts = %d", attempts.Load())
	}
	if observed.Load() != 2 {
		t.Fatalf("observed = %d", observed.Load())
	}
}

func TestTraceHookCanDecorateRequestContext(t *testing.T) {
	type traceKey struct{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	called := false
	client := New(
		WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil))),
		WithTraceHook(func(ctx context.Context, req *http.Request) context.Context {
			called = true
			return context.WithValue(ctx, traceKey{}, req.URL.Host)
		}),
		WithObserver(ObserverFunc(func(metrics RequestMetrics) {
			if metrics.Host == "" {
				t.Fatal("observer did not receive host")
			}
		})),
	)
	if err := client.JSON(context.Background(), http.MethodGet, server.URL, nil, nil); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("trace hook was not called")
	}
}
