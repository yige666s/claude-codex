package cli

import (
	"context"
	"os"
	"strings"

	"claude-codex/internal/app/config"
	"claude-codex/internal/harness/state"
	"claude-codex/internal/harness/telemetry"
)

func newTelemetryRuntime(cfg config.Config, home string, streams IO) (*telemetry.Runtime, error) {
	return telemetry.NewRuntime(telemetry.RuntimeOptions{
		Enabled:        cfg.Telemetry.Enabled,
		Exporters:      cfg.Telemetry.Exporter,
		Endpoint:       cfg.Telemetry.Endpoint,
		Insecure:       cfg.Telemetry.Insecure,
		ServiceName:    cfg.Telemetry.ServiceNameOrDefault(),
		HomeDir:        home,
		Stdout:         streams.Err,
		BetaEnabled:    telemetry.IsBetaTracingEnabled(),
		LogUserPrompts: telemetryLogUserPrompts(),
	})
}

func recordStreamingInteraction(tracer telemetry.SessionTracer, session *state.Session, prompt string, output string, err error) {
	if tracer == nil || session == nil {
		return
	}
	interactionID := "streaming-interaction"
	telemetry.RecordEvent(tracer, session.ID, "interaction.start", "interaction", map[string]any{
		"span_id":       interactionID,
		"prompt":        prompt,
		"prompt_length": len(prompt),
		"working_dir":   session.WorkingDir,
	})

	attrs := map[string]any{
		"span_id":      interactionID,
		"output_chars": len(output),
	}
	if err != nil {
		attrs["status"] = "error"
		attrs["error"] = err.Error()
	} else {
		attrs["status"] = "ok"
	}
	telemetry.RecordEvent(tracer, session.ID, "interaction.end", "interaction", attrs)
}

func telemetryLogUserPrompts() bool {
	switch strings.TrimSpace(strings.ToLower(os.Getenv("OTEL_LOG_USER_PROMPTS"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func closeTelemetryRuntime(_ context.Context, runtime *telemetry.Runtime) error {
	if runtime == nil {
		return nil
	}
	return runtime.Close()
}
