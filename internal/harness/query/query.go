package query

import (
	"context"
	"fmt"
	"os"

	"github.com/ding/claude-code/claude-go/internal/public/types"
)

// Global system prompt builder instance
var globalSystemPromptBuilder = NewSystemPromptBuilder()

// Query is the main entry point for query execution.
// It orchestrates the query loop and handles command lifecycle notifications.
func Query(ctx context.Context, params *QueryParams) (<-chan interface{}, <-chan Terminal, error) {
	eventChan := make(chan interface{}, 100)
	terminalChan := make(chan Terminal, 1)

	// Build SystemPrompt if not provided
	if isEmptySystemPrompt(params.SystemPrompt) && params.UserContext != nil && params.SystemContext != nil {
		model := params.FallbackModel
		if model == "" {
			model = "claude-sonnet-4-6" // Default model
		}

		systemPrompt, err := globalSystemPromptBuilder.BuildSystemPrompt(
			ctx,
			params.UserContext,
			params.SystemContext,
			"", // customPrompt
			"", // appendPrompt
			model,
			params.MCPClients...,
		)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to build system prompt: %w", err)
		}
		params.SystemPrompt = systemPrompt
	}

	go func() {
		defer close(eventChan)
		defer close(terminalChan)

		// Wait for any in-progress session memory extraction from a prior turn
		// before starting the query loop, so the loop reads fresh notes.
		WaitForPendingSessionMemoryExtraction()

		consumedCommandUUIDs := []string{}
		terminal, err := queryLoop(ctx, params, &consumedCommandUUIDs, eventChan)

		if err != nil {
			terminal = Terminal{
				Reason: TerminalReasonModelError,
				Error:  err,
			}
		}

		// Only reached if queryLoop returned normally. Skipped on error.
		// This gives the same asymmetric started-without-completed signal
		// as the TypeScript implementation.
		if err == nil {
			for _, uuid := range consumedCommandUUIDs {
				notifyCommandLifecycle(uuid, "completed")
			}
		}

		terminalChan <- terminal
	}()

	return eventChan, terminalChan, nil
}

// notifyCommandLifecycle notifies about command lifecycle events.
func notifyCommandLifecycle(uuid, event string) {
	// TODO: Implement command lifecycle notification
	// This should integrate with the command queue manager
}

// buildQueryConfig creates the query configuration from environment.
func buildQueryConfig() *QueryConfig {
	return &QueryConfig{
		SessionID: getSessionID(),
		Gates: QueryGates{
			StreamingToolExecution: checkStreamingToolExecutionGate(),
			EmitToolUseSummaries:   checkEmitToolUseSummariesGate(),
			IsAnt:                  isAntUser(),
			FastModeEnabled:        isFastModeEnabled(),
		},
	}
}

// Helper functions for configuration (to be implemented)
// getSessionID returns the current session ID.
// Checks CLAUDE_SESSION_ID env var first, then generates a UUID.
func getSessionID() string {
	if id := os.Getenv("CLAUDE_SESSION_ID"); id != "" {
		return id
	}
	return types.UUID()
}

func checkStreamingToolExecutionGate() bool {
	// TODO: Implement feature gate check
	return true
}

func checkEmitToolUseSummariesGate() bool {
	// TODO: Implement environment check
	return false
}

func isAntUser() bool {
	// TODO: Implement user type check
	return false
}

func isFastModeEnabled() bool {
	// TODO: Implement fast mode check
	return true
}

// productionDeps returns the production dependencies.
func productionDeps() *QueryDeps {
	return &QueryDeps{
		CallModel: func(ctx context.Context, params *ModelCallParams) (<-chan types.Message, error) {
			// TODO: Implement actual model calling
			ch := make(chan types.Message)
			close(ch)
			return ch, nil
		},
		UUID: func() string {
			// TODO: Implement UUID generation
			return fmt.Sprintf("uuid-%d", 0)
		},
		CompactService: nil, // TODO: Implement
		APIService:     nil, // TODO: Implement
	}
}
