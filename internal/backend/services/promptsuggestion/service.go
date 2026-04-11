package promptsuggestion

type SuppressReason string

const (
	SuppressDisabled          SuppressReason = "disabled"
	SuppressPendingPermission SuppressReason = "pending_permission"
	SuppressElicitation       SuppressReason = "elicitation_active"
	SuppressPlanMode          SuppressReason = "plan_mode"
	SuppressRateLimit         SuppressReason = "rate_limit"
)

type AppState struct {
	PromptSuggestionEnabled bool
	PendingWorkerRequest    bool
	PendingSandboxRequest   bool
	ElicitationQueueLength  int
	PermissionMode          string
	RateLimitStatus         string
}

func GetSuggestionSuppressReason(state AppState) *SuppressReason {
	switch {
	case !state.PromptSuggestionEnabled:
		r := SuppressDisabled
		return &r
	case state.PendingWorkerRequest || state.PendingSandboxRequest:
		r := SuppressPendingPermission
		return &r
	case state.ElicitationQueueLength > 0:
		r := SuppressElicitation
		return &r
	case state.PermissionMode == "plan":
		r := SuppressPlanMode
		return &r
	case state.RateLimitStatus != "" && state.RateLimitStatus != "allowed":
		r := SuppressRateLimit
		return &r
	default:
		return nil
	}
}

func ShouldEnablePromptSuggestion(nonInteractive bool, teammate bool, settingsEnabled bool) bool {
	if nonInteractive || teammate {
		return false
	}
	return settingsEnabled
}
