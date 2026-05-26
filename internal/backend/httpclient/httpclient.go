package httpclient

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"

	"claude-codex/internal/backend/retry"
)

const (
	defaultTimeout      = 10 * time.Second
	defaultMaxBodyBytes = 1 << 20
)

type Client struct {
	client       *http.Client
	timeout      time.Duration
	retry        retry.Policy
	logger       *slog.Logger
	component    string
	observer     Observer
	traceHook    TraceHook
	maxBodyBytes int64
}

type Option func(*Client)

type RequestOption func(*requestOptions)

type requestOptions struct {
	headers    http.Header
	okStatuses map[int]bool
	anyStatus  bool
}

type RetryPolicy = retry.Policy

type Observer interface {
	ObserveHTTPClientRequest(RequestMetrics)
}

type ObserverFunc func(RequestMetrics)

type TraceHook func(context.Context, *http.Request) context.Context

func (f ObserverFunc) ObserveHTTPClientRequest(metrics RequestMetrics) {
	if f != nil {
		f(metrics)
	}
}

type RequestMetrics struct {
	Component  string
	Method     string
	URL        string
	Host       string
	Path       string
	StatusCode int
	Attempt    int
	Duration   time.Duration
	Error      error
}

type StatusError struct {
	Method     string
	URL        string
	StatusCode int
	Status     string
	Body       string
	Header     http.Header
}

func (e *StatusError) Error() string {
	if e == nil {
		return ""
	}
	body := strings.TrimSpace(e.Body)
	if body == "" {
		return fmt.Sprintf("%s %s failed: %s", e.Method, e.URL, e.Status)
	}
	return fmt.Sprintf("%s %s failed: %s: %s", e.Method, e.URL, e.Status, body)
}

func (e *StatusError) RetryAfterHeader() string {
	if e == nil || e.Header == nil {
		return ""
	}
	return e.Header.Get("Retry-After")
}

func New(options ...Option) *Client {
	c := &Client{
		timeout:      defaultTimeout,
		retry:        DefaultRetryPolicy(),
		logger:       slog.Default(),
		maxBodyBytes: defaultMaxBodyBytes,
	}
	for _, option := range options {
		if option != nil {
			option(c)
		}
	}
	if c.client == nil {
		c.client = &http.Client{Timeout: c.timeout}
	} else if c.client.Timeout <= 0 && c.timeout > 0 {
		clone := *c.client
		clone.Timeout = c.timeout
		c.client = &clone
	}
	if c.logger == nil {
		c.logger = slog.Default()
	}
	return c
}

func WithHTTPClient(client *http.Client) Option {
	return func(c *Client) {
		c.client = client
	}
}

func WithTimeout(timeout time.Duration) Option {
	return func(c *Client) {
		if timeout > 0 {
			c.timeout = timeout
		}
	}
}

func WithRetry(policy RetryPolicy) Option {
	return func(c *Client) {
		c.retry = policy
	}
}

func WithLogger(logger *slog.Logger) Option {
	return func(c *Client) {
		c.logger = logger
	}
}

func WithComponent(component string) Option {
	return func(c *Client) {
		c.component = strings.TrimSpace(component)
	}
}

func WithObserver(observer Observer) Option {
	return func(c *Client) {
		c.observer = observer
	}
}

func WithTraceHook(hook TraceHook) Option {
	return func(c *Client) {
		c.traceHook = hook
	}
}

func WithMaxBodyBytes(limit int64) Option {
	return func(c *Client) {
		if limit > 0 {
			c.maxBodyBytes = limit
		}
	}
}

func WithHeader(key, value string) RequestOption {
	return func(opts *requestOptions) {
		if opts.headers == nil {
			opts.headers = make(http.Header)
		}
		opts.headers.Set(key, value)
	}
}

func WithHeaders(headers http.Header) RequestOption {
	return func(opts *requestOptions) {
		if opts.headers == nil {
			opts.headers = make(http.Header)
		}
		for key, values := range headers {
			for _, value := range values {
				opts.headers.Add(key, value)
			}
		}
	}
}

func WithBearer(token string) RequestOption {
	token = strings.TrimSpace(token)
	return WithHeader("Authorization", "Bearer "+token)
}

func WithBasicAuth(username, password string) RequestOption {
	return func(opts *requestOptions) {
		if opts.headers == nil {
			opts.headers = make(http.Header)
		}
		opts.headers.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(username+":"+password)))
	}
}

func WithOKStatuses(statuses ...int) RequestOption {
	return func(opts *requestOptions) {
		if opts.okStatuses == nil {
			opts.okStatuses = make(map[int]bool, len(statuses))
		}
		for _, status := range statuses {
			opts.okStatuses[status] = true
		}
	}
}

func WithAnyStatus() RequestOption {
	return func(opts *requestOptions) {
		opts.anyStatus = true
	}
}

func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		MaxAttempts: 3,
		BaseDelay:   250 * time.Millisecond,
		MaxDelay:    2 * time.Second,
		Jitter:      0.2,
		Methods: map[string]bool{
			http.MethodGet:     true,
			http.MethodHead:    true,
			http.MethodPut:     true,
			http.MethodDelete:  true,
			http.MethodOptions: true,
		},
		Statuses: map[int]bool{
			http.StatusRequestTimeout:      true,
			http.StatusTooManyRequests:     true,
			http.StatusInternalServerError: true,
			http.StatusBadGateway:          true,
			http.StatusServiceUnavailable:  true,
			http.StatusGatewayTimeout:      true,
		},
	}
}

func NoRetry() RetryPolicy {
	return retry.NoRetry()
}

func (c *Client) Do(ctx context.Context, method, url string, body io.Reader, options ...RequestOption) (*http.Response, error) {
	requestOptions := applyRequestOptions(options...)
	return c.do(ctx, method, url, func() (io.Reader, error) { return body, nil }, requestOptions, false)
}

func (c *Client) JSON(ctx context.Context, method, url string, in any, out any, options ...RequestOption) error {
	requestOptions := applyRequestOptions(options...)
	requestOptions.headers.Set("Content-Type", "application/json")
	requestOptions.headers.Set("Accept", "application/json")
	resp, err := c.do(ctx, method, url, jsonReader(in), requestOptions, true)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if out == nil {
		io.Copy(io.Discard, resp.Body)
		return nil
	}
	if resp.Body == nil {
		return io.ErrUnexpectedEOF
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *Client) Bytes(ctx context.Context, method, url string, in any, options ...RequestOption) (int, []byte, http.Header, error) {
	requestOptions := applyRequestOptions(options...)
	if in != nil {
		requestOptions.headers.Set("Content-Type", "application/json")
	}
	resp, err := c.do(ctx, method, url, jsonReader(in), requestOptions, true)
	if err != nil {
		return 0, nil, nil, err
	}
	defer resp.Body.Close()
	data, err := readLimited(resp.Body, c.maxBodyBytes)
	if err != nil {
		return resp.StatusCode, nil, resp.Header.Clone(), err
	}
	return resp.StatusCode, data, resp.Header.Clone(), nil
}

func (c *Client) Data(ctx context.Context, method, url string, data []byte, options ...RequestOption) (int, []byte, http.Header, error) {
	requestOptions := applyRequestOptions(options...)
	resp, err := c.do(ctx, method, url, func() (io.Reader, error) {
		if data == nil {
			return nil, nil
		}
		return bytes.NewReader(data), nil
	}, requestOptions, true)
	if err != nil {
		return 0, nil, nil, err
	}
	defer resp.Body.Close()
	body, err := readLimited(resp.Body, c.maxBodyBytes)
	if err != nil {
		return resp.StatusCode, nil, resp.Header.Clone(), err
	}
	return resp.StatusCode, body, resp.Header.Clone(), nil
}

func (c *Client) do(ctx context.Context, method, url string, bodyFactory func() (io.Reader, error), options requestOptions, closeBody bool) (*http.Response, error) {
	if c == nil {
		c = New()
	}
	method = strings.ToUpper(strings.TrimSpace(method))
	if method == "" {
		method = http.MethodGet
	}
	policy := c.retry
	attempts := policy.AttemptsForMethod(method)
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		start := time.Now()
		body, err := bodyFactory()
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, method, url, body)
		if err != nil {
			return nil, err
		}
		for key, values := range options.headers {
			for _, value := range values {
				req.Header.Add(key, value)
			}
		}
		if requestID := middleware.GetReqID(ctx); requestID != "" && req.Header.Get("X-Request-ID") == "" {
			req.Header.Set("X-Request-ID", requestID)
		}
		if c.traceHook != nil {
			if tracedCtx := c.traceHook(req.Context(), req); tracedCtx != nil {
				req = req.WithContext(tracedCtx)
			}
		}
		resp, err := c.client.Do(req)
		if err != nil {
			c.observe(ctx, req, 0, attempt, time.Since(start), err)
			lastErr = err
			if attempt < attempts && policy.ShouldRetry(method, 0, err) {
				if sleepErr := policy.Sleep(ctx, attempt, err); sleepErr != nil {
					return nil, sleepErr
				}
				continue
			}
			return nil, err
		}
		if isOKStatus(resp.StatusCode, options) {
			c.observe(ctx, req, resp.StatusCode, attempt, time.Since(start), nil)
			return resp, nil
		}
		bodyText := ""
		if resp.Body != nil {
			data, _ := readLimited(resp.Body, c.maxBodyBytes)
			bodyText = string(data)
			_ = resp.Body.Close()
		}
		statusErr := &StatusError{
			Method:     method,
			URL:        url,
			StatusCode: resp.StatusCode,
			Status:     resp.Status,
			Body:       bodyText,
			Header:     resp.Header.Clone(),
		}
		c.observe(ctx, req, resp.StatusCode, attempt, time.Since(start), statusErr)
		lastErr = statusErr
		if attempt < attempts && policy.ShouldRetry(method, resp.StatusCode, statusErr) {
			if sleepErr := policy.Sleep(ctx, attempt, statusErr); sleepErr != nil {
				return nil, sleepErr
			}
			continue
		}
		return nil, statusErr
	}
	return nil, lastErr
}

func (c *Client) observe(ctx context.Context, req *http.Request, status, attempt int, duration time.Duration, err error) {
	if c == nil || req == nil {
		return
	}
	metrics := RequestMetrics{
		Component:  c.component,
		Method:     req.Method,
		URL:        req.URL.String(),
		Host:       req.URL.Host,
		Path:       req.URL.Path,
		StatusCode: status,
		Attempt:    attempt,
		Duration:   duration,
		Error:      err,
	}
	if c.observer != nil {
		c.observer.ObserveHTTPClientRequest(metrics)
	}
	if c.logger == nil {
		return
	}
	level := slog.LevelDebug
	if err != nil {
		level = slog.LevelWarn
	}
	attrs := []slog.Attr{
		slog.String("component", firstNonEmpty(c.component, "httpclient")),
		slog.String("method", req.Method),
		slog.String("host", req.URL.Host),
		slog.String("path", req.URL.Path),
		slog.Int("status", status),
		slog.Int("attempt", attempt),
		slog.Int64("duration_ms", duration.Milliseconds()),
	}
	if requestID := middleware.GetReqID(ctx); requestID != "" {
		attrs = append(attrs, slog.String("request_id", requestID))
	}
	if err != nil {
		attrs = append(attrs, slog.Any("error", err))
	}
	c.logger.LogAttrs(ctx, level, "outbound http request", attrs...)
}

func applyRequestOptions(options ...RequestOption) requestOptions {
	out := requestOptions{headers: make(http.Header)}
	for _, option := range options {
		if option != nil {
			option(&out)
		}
	}
	return out
}

func jsonReader(value any) func() (io.Reader, error) {
	return func() (io.Reader, error) {
		if value == nil {
			return nil, nil
		}
		data, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		return bytes.NewReader(data), nil
	}
}

func isOKStatus(status int, options requestOptions) bool {
	if options.anyStatus {
		return true
	}
	okStatuses := options.okStatuses
	if len(okStatuses) > 0 {
		return okStatuses[status]
	}
	return status >= 200 && status < 300
}

func readLimited(body io.Reader, limit int64) ([]byte, error) {
	if body == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = defaultMaxBodyBytes
	}
	return io.ReadAll(io.LimitReader(body, limit))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
