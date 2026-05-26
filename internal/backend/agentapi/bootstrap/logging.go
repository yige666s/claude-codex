package bootstrap

import (
	"context"
	"fmt"
	"log/slog"
	"os"
)

func logInfof(format string, args ...any) {
	slog.Default().With("component", "agentapi_bootstrap").InfoContext(context.Background(), fmt.Sprintf(format, args...))
}

func logFatalf(format string, args ...any) {
	slog.Default().With("component", "agentapi_bootstrap").ErrorContext(context.Background(), fmt.Sprintf(format, args...))
	os.Exit(1)
}
