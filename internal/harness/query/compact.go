package query

import (
	"context"

	svccompact "claude-codex/internal/backend/services/compact"
	localcompact "claude-codex/internal/harness/compact"
	"claude-codex/internal/harness/tool"
	"claude-codex/internal/public/types"
)

// LocalCompactService adapts the Go compact package to the query loop's
// CompactService interface.
type LocalCompactService struct {
	model string
}

func NewLocalCompactService(model string) *LocalCompactService {
	return &LocalCompactService{model: model}
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
		snipped, stats := localcompact.SnipMessagesWithStats(messages, localcompact.DefaultSnipConfig())
		if stats.EstimatedTokensSaved <= 0 {
			return nil, err
		}
		return &CompactionResult{Messages: snipped}, nil
	}
	return &CompactionResult{Messages: result.Messages}, nil
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
	// TODO: Implement compaction logic
	// This should:
	// 1. Call the compact service
	// 2. Build post-compact messages
	// 3. Emit compaction events
	// 4. Return the compaction result

	if deps.CompactService == nil || !deps.CompactService.IsAutoCompactEnabled() {
		return nil, nil
	}

	result, err := deps.CompactService.Compact(ctx, messages)
	if err != nil {
		return nil, err
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
	// TODO: Implement reactive compaction logic
	// This should:
	// 1. Attempt context collapse drain first (cheap, keeps granular context)
	// 2. Fall back to reactive compact (full summary)
	// 3. Handle media-size rejections via strip-retry
	// 4. Return the compaction result

	if deps.CompactService == nil {
		return nil, nil
	}

	// Try reactive compaction
	result, err := deps.CompactService.Compact(ctx, messages)
	if err != nil {
		return nil, err
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
