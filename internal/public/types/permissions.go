package types

import "context"

// PermissionMode controls how permissions are handled.
type PermissionMode string

const (
	PermissionModeDefault PermissionMode = "default" // Prompt for permission
	PermissionModePlan    PermissionMode = "plan"    // Read-only mode
	PermissionModeBypass  PermissionMode = "bypass"  // Auto-allow all
	PermissionModeAuto    PermissionMode = "auto"    // Auto-allow with rules
)

// PermissionLevel represents the level of permission required.
type PermissionLevel string

const (
	PermissionLevelNone    PermissionLevel = "none"    // No permission needed
	PermissionLevelRead    PermissionLevel = "read"    // Read-only access
	PermissionLevelWrite   PermissionLevel = "write"   // Write access
	PermissionLevelExecute PermissionLevel = "execute" // Execute access
)

// PermissionBehavior indicates how to handle a permission request.
type PermissionBehavior string

const (
	PermissionBehaviorAllow PermissionBehavior = "allow" // Allow the action
	PermissionBehaviorDeny  PermissionBehavior = "deny"  // Deny the action
	PermissionBehaviorAsk   PermissionBehavior = "ask"   // Ask the user
)

// PermissionResult represents the result of a permission check.
type PermissionResult struct {
	Behavior PermissionBehavior `json:"behavior"`
	Reason   string             `json:"reason,omitempty"`
	Cached   bool               `json:"cached,omitempty"`
}

// PermissionRequest represents a request for permission.
type PermissionRequest struct {
	ToolName    string                 `json:"tool_name"`
	Level       PermissionLevel        `json:"level"`
	Input       map[string]interface{} `json:"input,omitempty"`
	Description string                 `json:"description,omitempty"`
	Path        string                 `json:"path,omitempty"`
}

// PermissionResponse represents a response to a permission request.
type PermissionResponse struct {
	Granted bool   `json:"granted"`
	Reason  string `json:"reason,omitempty"`
}

// AdditionalWorkingDirectory represents an additional working directory with permissions.
type AdditionalWorkingDirectory struct {
	Path       string          `json:"path"`
	Permission PermissionLevel `json:"permission"` // "read", "write", "execute"
	Recursive  bool            `json:"recursive"`
}

// OrphanedPermission represents a permission that was granted but not yet used.
type OrphanedPermission struct {
	ToolName  string                 `json:"tool_name"`
	Input     map[string]interface{} `json:"input"`
	GrantedAt int64                  `json:"granted_at"` // Unix timestamp
}

// SDKPermissionDenial records a denied tool permission in SDK mode.
type SDKPermissionDenial struct {
	ToolName  string                 `json:"tool_name"`
	ToolUseID string                 `json:"tool_use_id"`
	ToolInput map[string]interface{} `json:"tool_input"`
	Reason    string                 `json:"reason,omitempty"`
	Timestamp int64                  `json:"timestamp"` // Unix timestamp
}

// PermissionChecker defines the interface for checking permissions.
type PermissionChecker interface {
	// Authorize checks if a tool is authorized to execute with the given level.
	Authorize(ctx context.Context, toolName string, level PermissionLevel) error

	// GetMode returns the current permission mode.
	GetMode() PermissionMode

	// SetMode sets the permission mode.
	SetMode(mode PermissionMode)

	// ClearCache clears the permission cache.
	ClearCache()
}

// PermissionHandler handles permission requests.
type PermissionHandler func(ctx context.Context, request PermissionRequest) (*PermissionResponse, error)

// NewPermissionResult creates a new permission result.
func NewPermissionResult(behavior PermissionBehavior, reason string) *PermissionResult {
	return &PermissionResult{
		Behavior: behavior,
		Reason:   reason,
	}
}

// AllowPermission creates a permission result that allows the action.
func AllowPermission(reason string) *PermissionResult {
	return &PermissionResult{
		Behavior: PermissionBehaviorAllow,
		Reason:   reason,
	}
}

// DenyPermission creates a permission result that denies the action.
func DenyPermission(reason string) *PermissionResult {
	return &PermissionResult{
		Behavior: PermissionBehaviorDeny,
		Reason:   reason,
	}
}

// AskPermission creates a permission result that asks the user.
func AskPermission(reason string) *PermissionResult {
	return &PermissionResult{
		Behavior: PermissionBehaviorAsk,
		Reason:   reason,
	}
}

// IsAllowed returns true if the permission is allowed.
func (pr *PermissionResult) IsAllowed() bool {
	return pr.Behavior == PermissionBehaviorAllow
}

// IsDenied returns true if the permission is denied.
func (pr *PermissionResult) IsDenied() bool {
	return pr.Behavior == PermissionBehaviorDeny
}

// ShouldAsk returns true if the user should be asked.
func (pr *PermissionResult) ShouldAsk() bool {
	return pr.Behavior == PermissionBehaviorAsk
}

// ParsePermissionMode parses a permission mode string.
func ParsePermissionMode(mode string) (PermissionMode, bool) {
	switch mode {
	case string(PermissionModeDefault), "":
		return PermissionModeDefault, true
	case string(PermissionModePlan):
		return PermissionModePlan, true
	case string(PermissionModeBypass):
		return PermissionModeBypass, true
	case string(PermissionModeAuto):
		return PermissionModeAuto, true
	default:
		return PermissionModeDefault, false
	}
}

// ParsePermissionLevel parses a permission level string.
func ParsePermissionLevel(level string) (PermissionLevel, bool) {
	switch level {
	case string(PermissionLevelNone):
		return PermissionLevelNone, true
	case string(PermissionLevelRead):
		return PermissionLevelRead, true
	case string(PermissionLevelWrite):
		return PermissionLevelWrite, true
	case string(PermissionLevelExecute):
		return PermissionLevelExecute, true
	default:
		return PermissionLevelNone, false
	}
}
