package main

import (
	"log/slog"
	"os"

	agentapirun "claude-codex/internal/backend/agentapi/run"
)

func main() {
	if err := agentapirun.NewCommand().Execute(); err != nil {
		slog.Default().With("component", "agentapi").Error(err.Error())
		os.Exit(1)
	}
}
