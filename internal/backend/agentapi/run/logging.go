package run

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
)

func runLogger(component string) *slog.Logger {
	component = strings.TrimSpace(component)
	if component == "" {
		component = "agentapi"
	}
	return slog.Default().With(slog.String("component", component))
}

func logInfof(format string, args ...any) {
	logRunf(context.Background(), slog.LevelInfo, "agentapi", format, args...)
}

func logWarnf(format string, args ...any) {
	logRunf(context.Background(), slog.LevelWarn, "agentapi", format, args...)
}

func logErrorf(format string, args ...any) {
	logRunf(context.Background(), slog.LevelError, "agentapi", format, args...)
}

func logFatalf(format string, args ...any) {
	logRunf(context.Background(), slog.LevelError, "agentapi", format, args...)
	os.Exit(1)
}

func logFatal(args ...any) {
	runLogger("agentapi").ErrorContext(context.Background(), fmt.Sprint(args...))
	os.Exit(1)
}

func logRunf(ctx context.Context, level slog.Level, component, format string, args ...any) {
	if ctx == nil {
		ctx = context.Background()
	}
	runLogger(component).Log(ctx, level, fmt.Sprintf(format, args...))
}
