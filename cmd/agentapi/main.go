package main

import (
	"log/slog"
	"os"

	agentapirun "claude-codex/internal/backend/agentapi/run"
)

func main() {
	command := agentapirun.NewCommand()
	command.SetArgs(agentapirun.NormalizeLegacyFlagArgs(os.Args[1:], command))
	if err := command.Execute(); err != nil {
		slog.Default().With("component", "agentapi").Error(err.Error())
		os.Exit(1)
	}
}
