package agentruntime

import (
	"bufio"
	"context"
	"log"
	"log/slog"
	"net"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5/middleware"
)

type contextKey string

const requestIDContextKey contextKey = "request_id"
const requestLogContextKey contextKey = "request_log"

type requestLogState struct {
	userID string
}

func init() {
	middleware.RequestIDHeader = "X-Request-ID"
}

func requestIDFromContext(ctx context.Context) string {
	if id := middleware.GetReqID(ctx); id != "" {
		return id
	}
	id, _ := ctx.Value(requestIDContextKey).(string)
	return id
}

func withRequestID(ctx context.Context, id string) context.Context {
	ctx = context.WithValue(ctx, requestIDContextKey, id)
	return context.WithValue(ctx, middleware.RequestIDKey, id)
}

func setRequestLogUserID(ctx context.Context, userID string) {
	state, _ := ctx.Value(requestLogContextKey).(*requestLogState)
	if state != nil {
		state.userID = strings.TrimSpace(userID)
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *statusRecorder) WriteHeader(status int) {
	if r.status == 0 {
		r.status = status
	}
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(data []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(data)
	r.bytes += n
	return n, err
}

func (r *statusRecorder) Flush() {
	if flusher, ok := r.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (r *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := r.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}
	return hijacker.Hijack()
}

func newStructuredLogger(logger *log.Logger) *slog.Logger {
	if logger == nil {
		return slog.Default()
	}
	return slog.New(slog.NewJSONHandler(logger.Writer(), &slog.HandlerOptions{
		ReplaceAttr: func(_ []string, attr slog.Attr) slog.Attr {
			if attr.Key == slog.TimeKey {
				attr.Key = "ts"
			}
			return attr
		},
	}))
}

func structuredLogger(logger any) *slog.Logger {
	switch typed := logger.(type) {
	case nil:
		return slog.Default()
	case *slog.Logger:
		if typed == nil {
			return slog.Default()
		}
		return typed
	case *log.Logger:
		return newStructuredLogger(typed)
	default:
		return slog.Default()
	}
}

func componentLogger(logger *slog.Logger, component string) *slog.Logger {
	if logger == nil {
		logger = slog.Default()
	}
	component = strings.TrimSpace(component)
	if component == "" {
		return logger
	}
	return logger.With(slog.String("component", component))
}

func contextLogAttrs(ctx context.Context, userID, sessionID, jobID string) []slog.Attr {
	attrs := make([]slog.Attr, 0, 4)
	if requestID := strings.TrimSpace(requestIDFromContext(ctx)); requestID != "" {
		attrs = append(attrs, slog.String("request_id", requestID))
	}
	if userID = strings.TrimSpace(userID); userID != "" {
		attrs = append(attrs, slog.String("user_id", userID))
	}
	if sessionID = strings.TrimSpace(sessionID); sessionID != "" {
		attrs = append(attrs, slog.String("session_id", sessionID))
	}
	if jobID = strings.TrimSpace(firstNonEmptyString(jobID, jobIDFromContext(ctx))); jobID != "" {
		attrs = append(attrs, slog.String("job_id", jobID))
	}
	return attrs
}

func logWarn(ctx context.Context, logger *slog.Logger, message string, attrs ...slog.Attr) {
	logWithLevel(ctx, logger, slog.LevelWarn, message, attrs...)
}

func logError(ctx context.Context, logger *slog.Logger, message string, err error, attrs ...slog.Attr) {
	if err != nil {
		attrs = append(attrs, slog.Any("error", err))
	}
	logWithLevel(ctx, logger, slog.LevelError, message, attrs...)
}

func logWithLevel(ctx context.Context, logger *slog.Logger, level slog.Level, message string, attrs ...slog.Attr) {
	if logger == nil {
		logger = slog.Default()
	}
	if ctx == nil {
		ctx = context.Background()
	}
	logger.LogAttrs(ctx, level, message, attrs...)
}

func logFields(logger *slog.Logger, fields map[string]any) {
	if logger == nil {
		return
	}
	attrs := make([]slog.Attr, 0, len(fields))
	message := "event"
	for key, value := range fields {
		if key == "event" {
			if event, ok := value.(string); ok && strings.TrimSpace(event) != "" {
				message = event
			}
		}
		attrs = append(attrs, slog.Any(key, value))
	}
	logger.LogAttrs(context.Background(), slog.LevelInfo, message, attrs...)
}
