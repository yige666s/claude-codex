package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	ToolLedgerStatusRunning        = "running"
	ToolLedgerStatusSucceeded      = "succeeded"
	ToolLedgerStatusFailed         = "failed"
	ToolLedgerStatusRequiresReview = "requires_review"
)

type ToolLedgerEntry struct {
	ID                     string          `json:"id,omitempty"`
	UserID                 string          `json:"user_id,omitempty"`
	SessionID              string          `json:"session_id,omitempty"`
	JobID                  string          `json:"job_id,omitempty"`
	WorkflowRunID          string          `json:"workflow_run_id,omitempty"`
	WorkflowStepID         string          `json:"workflow_step_id,omitempty"`
	WorkflowStepIndex      int             `json:"workflow_step_index,omitempty"`
	ToolCallID             string          `json:"tool_call_id,omitempty"`
	ToolName               string          `json:"tool_name,omitempty"`
	ArgsHash               string          `json:"args_hash,omitempty"`
	IdempotencyKey         string          `json:"idempotency_key,omitempty"`
	Status                 string          `json:"status,omitempty"`
	Input                  json.RawMessage `json:"input,omitempty"`
	Output                 string          `json:"output,omitempty"`
	Error                  string          `json:"error,omitempty"`
	ExternalIdempotencyKey string          `json:"external_idempotency_key,omitempty"`
	Attempt                int             `json:"attempt,omitempty"`
	Metadata               map[string]any  `json:"metadata,omitempty"`
	StartedAt              time.Time       `json:"started_at,omitempty"`
	CompletedAt            *time.Time      `json:"completed_at,omitempty"`
}

type ToolLedger interface {
	BeginToolCall(ctx context.Context, entry ToolLedgerEntry) (ToolLedgerEntry, bool, error)
	CompleteToolCall(ctx context.Context, idempotencyKey, output string, metadata map[string]any) error
	FailToolCall(ctx context.Context, idempotencyKey, errText string, metadata map[string]any) error
}

type ToolExecutionScope struct {
	UserID            string
	SessionID         string
	JobID             string
	RequestID         string
	WorkflowRunID     string
	WorkflowStepID    string
	WorkflowStepIndex int
}

type toolExecutionScopeContextKey struct{}

func WithToolExecutionScope(ctx context.Context, scope ToolExecutionScope) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	current := ToolExecutionScopeFromContext(ctx)
	scope = mergeToolExecutionScope(current, scope)
	return context.WithValue(ctx, toolExecutionScopeContextKey{}, scope)
}

func ToolExecutionScopeFromContext(ctx context.Context) ToolExecutionScope {
	if ctx == nil {
		return ToolExecutionScope{}
	}
	scope, _ := ctx.Value(toolExecutionScopeContextKey{}).(ToolExecutionScope)
	return scope
}

func mergeToolExecutionScope(base, overlay ToolExecutionScope) ToolExecutionScope {
	if strings.TrimSpace(overlay.UserID) != "" {
		base.UserID = strings.TrimSpace(overlay.UserID)
	}
	if strings.TrimSpace(overlay.SessionID) != "" {
		base.SessionID = strings.TrimSpace(overlay.SessionID)
	}
	if strings.TrimSpace(overlay.JobID) != "" {
		base.JobID = strings.TrimSpace(overlay.JobID)
	}
	if strings.TrimSpace(overlay.RequestID) != "" {
		base.RequestID = strings.TrimSpace(overlay.RequestID)
	}
	if strings.TrimSpace(overlay.WorkflowRunID) != "" {
		base.WorkflowRunID = strings.TrimSpace(overlay.WorkflowRunID)
	}
	if strings.TrimSpace(overlay.WorkflowStepID) != "" {
		base.WorkflowStepID = strings.TrimSpace(overlay.WorkflowStepID)
	}
	if overlay.WorkflowStepIndex > 0 {
		base.WorkflowStepIndex = overlay.WorkflowStepIndex
	}
	return base
}

func toolArgsHash(input json.RawMessage) string {
	normalized := input
	var value any
	if len(input) > 0 && json.Unmarshal(input, &value) == nil {
		if data, err := json.Marshal(value); err == nil {
			normalized = data
		}
	}
	sum := sha256.Sum256(normalized)
	return hex.EncodeToString(sum[:])
}

func toolCallIdempotencyKey(scope ToolExecutionScope, sessionID, interactionID string, call ToolCall, argsHash string) string {
	toolName := strings.TrimSpace(call.Name)
	if strings.TrimSpace(scope.WorkflowStepID) != "" {
		return fmt.Sprintf("%s:%s:%s", scope.WorkflowStepID, toolName, argsHash)
	}
	if strings.TrimSpace(scope.WorkflowRunID) != "" {
		return fmt.Sprintf("%s:%d:%s:%s", scope.WorkflowRunID, scope.WorkflowStepIndex, toolName, argsHash)
	}
	return fmt.Sprintf("%s:%s:%s:%s:%s", strings.TrimSpace(sessionID), strings.TrimSpace(interactionID), strings.TrimSpace(call.ID), toolName, argsHash)
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}
