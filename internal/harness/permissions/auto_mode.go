package permissions

import "sync"

// autoModeState holds module-level auto mode flags (mirrors TS autoModeState.ts).
var autoModeState struct {
	mu           sync.Mutex
	active       bool
	flagCli      bool
	circuitBroken bool
}

// SetAutoModeActive sets whether auto mode is currently active.
func SetAutoModeActive(v bool) {
	autoModeState.mu.Lock()
	autoModeState.active = v
	autoModeState.mu.Unlock()
}

// IsAutoModeActive returns whether auto mode is currently active.
func IsAutoModeActive() bool {
	autoModeState.mu.Lock()
	defer autoModeState.mu.Unlock()
	return autoModeState.active
}

// SetAutoModeFlagCli sets whether auto mode was requested via CLI flag.
func SetAutoModeFlagCli(v bool) {
	autoModeState.mu.Lock()
	autoModeState.flagCli = v
	autoModeState.mu.Unlock()
}

// GetAutoModeFlagCli returns whether auto mode was requested via CLI flag.
func GetAutoModeFlagCli() bool {
	autoModeState.mu.Lock()
	defer autoModeState.mu.Unlock()
	return autoModeState.flagCli
}

// SetAutoModeCircuitBroken sets whether the auto mode circuit breaker has been tripped.
func SetAutoModeCircuitBroken(v bool) {
	autoModeState.mu.Lock()
	autoModeState.circuitBroken = v
	autoModeState.mu.Unlock()
}

// IsAutoModeCircuitBroken returns whether the auto mode circuit breaker has been tripped.
func IsAutoModeCircuitBroken() bool {
	autoModeState.mu.Lock()
	defer autoModeState.mu.Unlock()
	return autoModeState.circuitBroken
}

// ResetAutoModeStateForTesting resets all auto mode state to defaults. Test use only.
func ResetAutoModeStateForTesting() {
	autoModeState.mu.Lock()
	autoModeState.active = false
	autoModeState.flagCli = false
	autoModeState.circuitBroken = false
	autoModeState.mu.Unlock()
}

// DenialTrackingState tracks consecutive and total permission denials for auto mode fallback.
type DenialTrackingState struct {
	ConsecutiveDenials int
	TotalDenials       int
}

const (
	maxConsecutiveDenials = 3
	maxTotalDenials       = 20
)

// NewDenialTrackingState creates a fresh tracking state.
func NewDenialTrackingState() DenialTrackingState { return DenialTrackingState{} }

// RecordDenial increments both counters and returns the updated state.
func RecordDenial(s DenialTrackingState) DenialTrackingState {
	return DenialTrackingState{
		ConsecutiveDenials: s.ConsecutiveDenials + 1,
		TotalDenials:       s.TotalDenials + 1,
	}
}

// RecordSuccess resets the consecutive counter and returns the updated state.
func RecordSuccess(s DenialTrackingState) DenialTrackingState {
	return DenialTrackingState{
		ConsecutiveDenials: 0,
		TotalDenials:       s.TotalDenials,
	}
}

// ShouldFallbackToPrompting returns true when the denial thresholds are exceeded.
func ShouldFallbackToPrompting(s DenialTrackingState) bool {
	return s.ConsecutiveDenials >= maxConsecutiveDenials || s.TotalDenials >= maxTotalDenials
}
