package query

import (
	"sync"

	"github.com/ding/claude-code/claude-go/internal/harness/tool"
	"github.com/ding/claude-code/claude-go/internal/public/types"
)

// State management functions for tracking query state across iterations.

var (
	stateMu           sync.RWMutex
	globalQueryStates = make(map[string]*State)
)

// SaveState saves the current query state for a session.
func SaveState(sessionID string, state *State) {
	stateMu.Lock()
	defer stateMu.Unlock()
	globalQueryStates[sessionID] = state
}

// LoadState loads the query state for a session.
func LoadState(sessionID string) (*State, bool) {
	stateMu.RLock()
	defer stateMu.RUnlock()
	state, ok := globalQueryStates[sessionID]
	return state, ok
}

// DeleteState deletes the query state for a session.
func DeleteState(sessionID string) {
	stateMu.Lock()
	defer stateMu.Unlock()
	delete(globalQueryStates, sessionID)
}

// CloneState creates a deep copy of the state.
func CloneState(state *State) *State {
	if state == nil {
		return nil
	}

	newState := &State{
		Messages:                     cloneMessages(state.Messages),
		ToolUseContext:               state.ToolUseContext, // Shallow copy - context is managed separately
		MaxOutputTokensRecoveryCount: state.MaxOutputTokensRecoveryCount,
		HasAttemptedReactiveCompact:  state.HasAttemptedReactiveCompact,
		TurnCount:                    state.TurnCount,
	}

	if state.AutoCompactTracking != nil {
		tracking := *state.AutoCompactTracking
		newState.AutoCompactTracking = &tracking
	}

	if state.MaxOutputTokensOverride != nil {
		override := *state.MaxOutputTokensOverride
		newState.MaxOutputTokensOverride = &override
	}

	if state.StopHookActive != nil {
		active := *state.StopHookActive
		newState.StopHookActive = &active
	}

	if state.Transition != nil {
		transition := *state.Transition
		newState.Transition = &transition
	}

	return newState
}

// cloneMessages creates a deep copy of messages.
func cloneMessages(messages []types.Message) []types.Message {
	if messages == nil {
		return nil
	}

	cloned := make([]types.Message, len(messages))
	copy(cloned, messages)
	return cloned
}

// UpdateStateMessages updates the messages in the state.
func UpdateStateMessages(state *State, messages []types.Message) {
	state.Messages = messages
}

// UpdateStateToolUseContext updates the tool use context in the state.
func UpdateStateToolUseContext(state *State, ctx *tool.ToolUseContext) {
	state.ToolUseContext = ctx
}

// UpdateStateAutoCompactTracking updates the auto-compact tracking in the state.
func UpdateStateAutoCompactTracking(state *State, tracking *AutoCompactTrackingState) {
	state.AutoCompactTracking = tracking
}

// IncrementTurnCount increments the turn count in the state.
func IncrementTurnCount(state *State) {
	state.TurnCount++
}

// ResetRecoveryState resets recovery-related state fields.
func ResetRecoveryState(state *State) {
	state.MaxOutputTokensRecoveryCount = 0
	state.HasAttemptedReactiveCompact = false
	state.MaxOutputTokensOverride = nil
}

// SetTransition sets the transition reason for the state.
func SetTransition(state *State, reason string) {
	state.Transition = &Continue{Reason: reason}
}

// GetTransitionReason returns the current transition reason.
func GetTransitionReason(state *State) string {
	if state.Transition == nil {
		return ""
	}
	return state.Transition.Reason
}

// IsFirstIteration checks if this is the first iteration (no transition).
func IsFirstIteration(state *State) bool {
	return state.Transition == nil
}

// GetTurnCount returns the current turn count.
func GetTurnCount(state *State) int {
	return state.TurnCount
}

// GetMessages returns the current messages.
func GetMessages(state *State) []types.Message {
	return state.Messages
}

// GetToolUseContext returns the current tool use context.
func GetToolUseContext(state *State) *tool.ToolUseContext {
	return state.ToolUseContext
}

// GetAutoCompactTracking returns the auto-compact tracking state.
func GetAutoCompactTracking(state *State) *AutoCompactTrackingState {
	return state.AutoCompactTracking
}

// IsStopHookActive checks if stop hooks are currently active.
func IsStopHookActive(state *State) bool {
	if state.StopHookActive == nil {
		return false
	}
	return *state.StopHookActive
}

// SetStopHookActive sets the stop hook active flag.
func SetStopHookActive(state *State, active bool) {
	state.StopHookActive = &active
}
