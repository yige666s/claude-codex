package query

import (
	"context"

	"claude-codex/internal/harness/tool"
	"claude-codex/internal/public/types"
)

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

	if deps.CompactService == nil {
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
	// TODO: Implement auto-compact decision logic
	// This should check:
	// 1. Token count thresholds
	// 2. Turn counter
	// 3. Consecutive failures
	// 4. Auto-compact enabled flag
	// 5. Query source restrictions

	return false
}

// calculateTokenWarningState calculates the token warning state.
func calculateTokenWarningState(tokenCount int, model string) TokenWarningState {
	// TODO: Implement token warning calculation
	// This should determine if we're at blocking limit or near limit

	return TokenWarningState{
		IsAtBlockingLimit: false,
		IsNearLimit:       false,
	}
}

// isAutoCompactEnabled checks if auto-compaction is enabled.
func isAutoCompactEnabled() bool {
	// TODO: Implement auto-compact enabled check
	return true
}
