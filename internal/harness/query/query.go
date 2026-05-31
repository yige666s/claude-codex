package query

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"claude-codex/internal/harness/provider"
	"claude-codex/internal/harness/tool"
	"claude-codex/internal/public/types"
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
	notifyQueuedCommand(uuid, event)
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
		CallModel: newProviderModelCaller(nil),
		UUID: func() string {
			return types.UUID()
		},
		CompactService: NewLocalCompactService(""),
		APIService:     localAPIService{},
	}
}

func newProviderModelCaller(modelProvider provider.Provider) ModelCaller {
	return func(ctx context.Context, params *ModelCallParams) (<-chan types.Message, error) {
		p := modelProvider
		if p == nil {
			var err error
			p, err = providerFromEnv(params.Model)
			if err != nil {
				return nil, err
			}
		}
		resp, err := p.CreateMessage(ctx, provider.MessageRequest{
			Model:                 params.Model,
			Messages:              toProviderMessages(params.Messages),
			MaxTokens:             params.MaxTokens,
			Temperature:           params.Temperature,
			Tools:                 toProviderTools(params.Tools),
			System:                params.SystemPrompt.Content,
			GoogleSearchGrounding: provider.GoogleSearchGroundingAuto,
		})
		if err != nil {
			return nil, err
		}
		ch := make(chan types.Message, 1)
		ch <- providerResponseToMessage(resp)
		close(ch)
		return ch, nil
	}
}

func providerFromEnv(model string) (provider.Provider, error) {
	name := os.Getenv("CLAUDE_CODE_PROVIDER")
	if name == "" {
		name = os.Getenv("CLAUDE_PROVIDER")
	}
	if name == "" {
		switch {
		case os.Getenv("ANTHROPIC_API_KEY") != "":
			name = "anthropic"
		case os.Getenv("OPENAI_API_KEY") != "":
			name = "openai"
		case os.Getenv("GEMINI_API_KEY") != "":
			name = "gemini"
		default:
			name = "anthropic"
		}
	}
	cfg, err := provider.NewFactory().DefaultConfig(name)
	if err != nil {
		return nil, err
	}
	if model != "" {
		cfg.Model = model
	}
	switch cfg.Provider {
	case "anthropic":
		cfg.APIKey = firstNonEmpty(os.Getenv("ANTHROPIC_API_KEY"), os.Getenv("CLAUDE_API_KEY"))
	case "openai":
		cfg.APIKey = os.Getenv("OPENAI_API_KEY")
	case "gemini":
		cfg.APIKey = os.Getenv("GEMINI_API_KEY")
	}
	if baseURL := os.Getenv("CLAUDE_CODE_API_BASE_URL"); baseURL != "" {
		cfg.BaseURL = baseURL
	}
	cfg.GoogleSearchGrounding = firstNonEmpty(
		os.Getenv("AGENT_API_GOOGLE_SEARCH_GROUNDING"),
		os.Getenv("GOOGLE_SEARCH_GROUNDING"),
	)
	return provider.NewFactory().CreateProvider(cfg)
}

func toProviderMessages(messages []types.Message) []provider.Message {
	out := make([]provider.Message, 0, len(messages))
	for _, msg := range messages {
		role := "user"
		if msg.Type == types.MessageTypeAssistant {
			role = "assistant"
		}
		if msg.Type == types.MessageTypeTool {
			role = "tool"
		}
		out = append(out, provider.Message{
			Role:       role,
			Content:    providerContent(msg.Content),
			ToolCallID: msg.ToolUseID,
		})
	}
	return out
}

func providerContent(blocks []types.ContentBlock) interface{} {
	if len(blocks) == 1 && blocks[0].Type == "text" {
		return blocks[0].Text
	}
	out := make([]provider.ContentBlock, 0, len(blocks))
	for _, block := range blocks {
		out = append(out, provider.ContentBlock{
			Type:   block.Type,
			Text:   firstNonEmpty(block.Text, block.Content),
			Source: block.Source,
		})
	}
	return out
}

func toProviderTools(tools []tool.Tool) []provider.Tool {
	out := make([]provider.Tool, 0, len(tools))
	for _, toolDef := range tools {
		if toolDef == nil || !toolDef.IsEnabled() {
			continue
		}
		description := toolDef.Name()
		if desc, err := toolDef.Description(nil, tool.DescriptionOptions{}); err == nil && desc != "" {
			description = desc
		}
		out = append(out, provider.Tool{
			Name:        toolDef.Name(),
			Description: description,
			InputSchema: toolSchemaMap(toolDef.InputSchema()),
		})
	}
	return out
}

func toolSchemaMap(schema *tool.ToolInputJSONSchema) map[string]interface{} {
	if schema == nil {
		return nil
	}
	data, err := json.Marshal(schema)
	if err != nil {
		return nil
	}
	var out map[string]interface{}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil
	}
	return out
}

func providerResponseToMessage(resp *provider.MessageResponse) types.Message {
	msg := types.Message{
		Type:       types.MessageTypeAssistant,
		UUID:       types.UUID(),
		StopReason: resp.StopReason,
	}
	for _, block := range resp.Content {
		msg.Content = append(msg.Content, types.ContentBlock{Type: block.Type, Text: block.Text})
	}
	for _, call := range resp.ToolCalls {
		msg.Content = append(msg.Content, types.ContentBlock{
			Type:  "tool_use",
			ID:    call.ID,
			Name:  call.Name,
			Input: rawProviderInput(call.Input),
		})
	}
	return msg
}

func rawProviderInput(raw []byte) map[string]interface{} {
	if len(raw) == 0 {
		return nil
	}
	var out map[string]interface{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
