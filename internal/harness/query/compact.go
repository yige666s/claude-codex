package query

import (
	"context"
	"fmt"
	"strings"
	"time"

	svccompact "claude-codex/internal/backend/services/compact"
	localcompact "claude-codex/internal/harness/compact"
	"claude-codex/internal/harness/tool"
	"claude-codex/internal/public/types"
)

// LocalCompactService adapts the Go compact package to the query loop's
// CompactService interface.
type LocalCompactService struct {
	model      string
	summarizer CompactSummarizer
}

type CompactSummarizer func(ctx context.Context, messages []types.Message) (string, error)

func NewLocalCompactService(model string) *LocalCompactService {
	return &LocalCompactService{model: model}
}

func NewLocalCompactServiceWithSummarizer(model string, summarizer CompactSummarizer) *LocalCompactService {
	return &LocalCompactService{model: model, summarizer: summarizer}
}

func (s *LocalCompactService) Compact(ctx context.Context, messages []types.Message) (*CompactionResult, error) {
	compactor := localcompact.NewAutoCompactor(&localcompact.AutoCompactConfig{
		Enabled:                true,
		Model:                  s.model,
		ContextWindowSize:      200000,
		CurrentTokenUsage:      estimateMessageTokens(messages),
		MaxConsecutiveFailures: svccompact.MaxConsecutiveAutoCompactFailures,
	})
	result, err := compactor.CompactMessages(ctx, messages)
	if err != nil {
		if s.summarizer != nil {
			return s.fullCompact(ctx, messages)
		}
		snipped, stats := localcompact.SnipMessagesWithStats(messages, localcompact.DefaultSnipConfig())
		if stats.EstimatedTokensSaved <= 0 {
			return nil, err
		}
		return &CompactionResult{Messages: snipped}, nil
	}
	return &CompactionResult{Messages: result.Messages}, nil
}

// ReactiveCompact forces a real context reduction after the provider has
// rejected the prompt. Re-running threshold-based auto-compaction here can
// return the original messages unchanged when provider limits are lower than
// our estimate, which would only repeat the same failing request.
func (s *LocalCompactService) ReactiveCompact(ctx context.Context, messages []types.Message) (*CompactionResult, error) {
	if s.summarizer == nil {
		return nil, fmt.Errorf("reactive compaction requires a summary generator")
	}
	return s.fullCompact(ctx, messages)
}

func (s *LocalCompactService) fullCompact(ctx context.Context, messages []types.Message) (*CompactionResult, error) {
	if len(messages) == 0 {
		return &CompactionResult{}, nil
	}
	start := localcompact.CalculateMessagesToKeepIndex(messages, len(messages)-1, localcompact.DefaultSMCompactConfig)
	if start <= 0 || start >= len(messages) {
		return nil, fmt.Errorf("full compaction could not select a safe history boundary")
	}
	summary, err := s.summarizer(ctx, append([]types.Message(nil), messages[:start]...))
	if err != nil {
		return nil, fmt.Errorf("generate compact summary: %w", err)
	}
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return nil, fmt.Errorf("generate compact summary: empty summary")
	}
	boundary := types.Message{
		Type:      types.MessageTypeSystem,
		Subtype:   "compact_boundary",
		UUID:      types.UUID(),
		Timestamp: time.Now().UTC(),
		Content:   []types.ContentBlock{{Type: "text", Text: "[Context compaction boundary]"}},
	}
	summaryMessage := types.Message{
		Type:             types.MessageTypeUser,
		UUID:             types.UUID(),
		Timestamp:        time.Now().UTC(),
		IsMeta:           true,
		IsCompactSummary: true,
		Content: []types.ContentBlock{{
			Type: "text",
			Text: "<context-summary>\n" + summary + "\n</context-summary>\n\nContinue from this summary and the preserved recent messages.",
		}},
	}
	kept := make([]types.Message, 0, len(messages)-start)
	for _, message := range messages[start:] {
		if message.Subtype != "compact_boundary" {
			kept = append(kept, message)
		}
	}
	compacted := append([]types.Message{boundary, summaryMessage}, kept...)
	if estimateMessageTokens(compacted) >= estimateMessageTokens(messages) {
		return nil, fmt.Errorf("full compaction did not reduce context")
	}
	return &CompactionResult{Messages: compacted}, nil
}

func (s *LocalCompactService) IsAutoCompactEnabled() bool {
	return svccompact.IsAutoCompactEnabled()
}

func (s *LocalCompactService) CalculateTokenWarningState(tokenCount int, model string) TokenWarningState {
	if model == "" {
		model = s.model
	}
	state := svccompact.CalculateTokenWarningState(tokenCount, model, 200000, s.IsAutoCompactEnabled())
	return TokenWarningState{
		IsAtBlockingLimit: state.IsAtBlockingLimit,
		IsNearLimit:       state.IsAboveWarningThreshold || state.IsAboveAutoCompactThreshold || state.IsAboveErrorThreshold,
	}
}

type localAPIService struct{}

func (localAPIService) CreateDumpPromptsFetch(sessionID string) interface{} {
	return map[string]string{"session_id": sessionID}
}

// performCompaction executes message compaction.
func performCompaction(
	ctx context.Context,
	deps *QueryDeps,
	messages []types.Message,
	toolCtx *tool.ToolUseContext,
	eventChan chan<- interface{},
) (*CompactionResult, error) {
	if deps.CompactService == nil || !deps.CompactService.IsAutoCompactEnabled() {
		return nil, nil
	}

	result, err := deps.CompactService.Compact(ctx, messages)
	if err != nil {
		return nil, err
	}
	if result == nil || len(result.Messages) == 0 {
		return nil, fmt.Errorf("auto compaction returned no messages")
	}
	return result, nil
}

// performReactiveCompaction executes reactive compaction on prompt-too-long errors.
func performReactiveCompaction(
	ctx context.Context,
	deps *QueryDeps,
	messages []types.Message,
	toolCtx *tool.ToolUseContext,
	eventChan chan<- interface{},
) (*CompactionResult, error) {
	if deps.CompactService == nil {
		return nil, fmt.Errorf("reactive compaction service is not configured")
	}

	service, ok := deps.CompactService.(interface {
		ReactiveCompact(context.Context, []types.Message) (*CompactionResult, error)
	})
	var result *CompactionResult
	var err error
	if ok {
		result, err = service.ReactiveCompact(ctx, messages)
	} else {
		result, err = deps.CompactService.Compact(ctx, messages)
	}
	if err != nil {
		return nil, err
	}
	if result == nil || len(result.Messages) == 0 {
		return nil, fmt.Errorf("reactive compaction returned no messages")
	}
	if estimateMessageTokens(result.Messages) >= estimateMessageTokens(messages) {
		return nil, fmt.Errorf("reactive compaction did not reduce context")
	}
	return result, nil
}

// shouldAutoCompact determines if auto-compaction should be triggered.
func shouldAutoCompact(
	messages []types.Message,
	tracking *AutoCompactTrackingState,
	ctx *tool.ToolUseContext,
) bool {
	if !isAutoCompactEnabled() {
		return false
	}
	if tracking != nil && tracking.ConsecutiveFailures >= svccompact.MaxConsecutiveAutoCompactFailures {
		return false
	}

	model := ""
	turnID := ""
	if ctx != nil {
		model = ctx.Options.MainLoopModel
		turnID = ctx.ToolUseID
	}
	if model == "" {
		model = "claude-sonnet-4-6"
	}

	converted := (*svccompact.AutoCompactTrackingState)(nil)
	if tracking != nil {
		converted = &svccompact.AutoCompactTrackingState{
			Compacted:           tracking.Compacted,
			TurnCounter:         tracking.TurnCounter,
			TurnID:              tracking.TurnID,
			ConsecutiveFailures: tracking.ConsecutiveFailures,
		}
	}

	return svccompact.ShouldTriggerAutoCompact(
		estimateMessageTokens(messages),
		model,
		200000,
		converted,
		turnID,
	)
}

// calculateTokenWarningState calculates the token warning state.
func calculateTokenWarningState(tokenCount int, model string) TokenWarningState {
	if model == "" {
		model = "claude-sonnet-4-6"
	}

	state := svccompact.CalculateTokenWarningState(tokenCount, model, 200000, isAutoCompactEnabled())
	return TokenWarningState{
		IsAtBlockingLimit: state.IsAtBlockingLimit,
		IsNearLimit:       state.IsAboveWarningThreshold || state.IsAboveAutoCompactThreshold || state.IsAboveErrorThreshold,
	}
}

// isAutoCompactEnabled checks if auto-compaction is enabled.
func isAutoCompactEnabled() bool {
	return svccompact.IsAutoCompactEnabled()
}
