package remote

import (
	"claude-codex/internal/harness/anthropic"
)

// SDKMessage types from CCR backend
type SDKMessageType string

const (
	SDKMessageTypeAssistant      SDKMessageType = "assistant"
	SDKMessageTypeUser           SDKMessageType = "user"
	SDKMessageTypePartial        SDKMessageType = "partial_assistant"
	SDKMessageTypeResult         SDKMessageType = "result"
	SDKMessageTypeSystem         SDKMessageType = "system"
	SDKMessageTypeStatus         SDKMessageType = "status"
	SDKMessageTypeToolProgress   SDKMessageType = "tool_progress"
	SDKMessageTypeAuthStatus     SDKMessageType = "auth_status"
	SDKMessageTypeToolUseSummary SDKMessageType = "tool_use_summary"
	SDKMessageTypeRateLimitEvent SDKMessageType = "rate_limit_event"
	SDKMessageTypeCompactBoundary SDKMessageType = "compact_boundary"
)

// SDKMessage is the base interface for all SDK messages
type SDKMessage interface {
	GetType() SDKMessageType
	GetUUID() string
}

// SDKAssistantMessage represents a complete assistant response
type SDKAssistantMessage struct {
	Type      SDKMessageType            `json:"type"`
	UUID      string                    `json:"uuid"`
	Message   anthropic.MessageResponse `json:"message"`
	Error     *string                   `json:"error,omitempty"`
	Timestamp string                    `json:"timestamp,omitempty"`
}

func (m *SDKAssistantMessage) GetType() SDKMessageType { return m.Type }
func (m *SDKAssistantMessage) GetUUID() string         { return m.UUID }

// SDKPartialAssistantMessage represents streaming assistant content
type SDKPartialAssistantMessage struct {
	Type  SDKMessageType      `json:"type"`
	UUID  string              `json:"uuid"`
	Event anthropic.StreamEvent `json:"event"`
}

func (m *SDKPartialAssistantMessage) GetType() SDKMessageType { return m.Type }
func (m *SDKPartialAssistantMessage) GetUUID() string         { return m.UUID }

// SDKUserMessage represents user input
type SDKUserMessage struct {
	Type          SDKMessageType           `json:"type"`
	UUID          string                   `json:"uuid"`
	Message       *anthropic.InputMessage  `json:"message,omitempty"`
	ToolUseResult *ToolUseResult           `json:"tool_use_result,omitempty"`
	Timestamp     string                   `json:"timestamp,omitempty"`
}

func (m *SDKUserMessage) GetType() SDKMessageType { return m.Type }
func (m *SDKUserMessage) GetUUID() string         { return m.UUID }

// ToolUseResult contains metadata about tool execution
type ToolUseResult struct {
	ToolUseID string `json:"tool_use_id"`
	ToolName  string `json:"tool_name"`
	IsError   bool   `json:"is_error"`
}

// SDKResultMessage indicates session completion
type SDKResultMessage struct {
	Type    SDKMessageType `json:"type"`
	UUID    string         `json:"uuid"`
	Subtype string         `json:"subtype"` // "success", "error", "cancelled"
	Result  string         `json:"result,omitempty"`
	Errors  []string       `json:"errors,omitempty"`
}

func (m *SDKResultMessage) GetType() SDKMessageType { return m.Type }
func (m *SDKResultMessage) GetUUID() string         { return m.UUID }

// SDKSystemMessage represents system initialization
type SDKSystemMessage struct {
	Type    SDKMessageType `json:"type"`
	UUID    string         `json:"uuid"`
	Subtype string         `json:"subtype"` // "init"
	Model   string         `json:"model"`
}

func (m *SDKSystemMessage) GetType() SDKMessageType { return m.Type }
func (m *SDKSystemMessage) GetUUID() string         { return m.UUID }

// SDKStatusMessage represents session status updates
type SDKStatusMessage struct {
	Type   SDKMessageType `json:"type"`
	UUID   string         `json:"uuid"`
	Status string         `json:"status,omitempty"` // "compacting", etc.
}

func (m *SDKStatusMessage) GetType() SDKMessageType { return m.Type }
func (m *SDKStatusMessage) GetUUID() string         { return m.UUID }

// SDKToolProgressMessage represents tool execution progress
type SDKToolProgressMessage struct {
	Type               SDKMessageType `json:"type"`
	UUID               string         `json:"uuid"`
	ToolUseID          string         `json:"tool_use_id"`
	ToolName           string         `json:"tool_name"`
	ElapsedTimeSeconds float64        `json:"elapsed_time_seconds"`
}

func (m *SDKToolProgressMessage) GetType() SDKMessageType { return m.Type }
func (m *SDKToolProgressMessage) GetUUID() string         { return m.UUID }

// SDKCompactBoundaryMessage marks conversation compaction
type SDKCompactBoundaryMessage struct {
	Type            SDKMessageType   `json:"type"`
	UUID            string           `json:"uuid"`
	CompactMetadata *CompactMetadata `json:"compact_metadata,omitempty"`
}

func (m *SDKCompactBoundaryMessage) GetType() SDKMessageType { return m.Type }
func (m *SDKCompactBoundaryMessage) GetUUID() string         { return m.UUID }

// CompactMetadata contains compaction details
type CompactMetadata struct {
	MessagesRemoved int    `json:"messages_removed"`
	TokensSaved     int    `json:"tokens_saved"`
	Summary         string `json:"summary,omitempty"`
}

// Control message types for bidirectional communication

// SDKControlRequest is sent from client to server
type SDKControlRequest struct {
	Type      string                   `json:"type"` // "control_request"
	RequestID string                   `json:"request_id"`
	Request   SDKControlRequestInner   `json:"request"`
}

// SDKControlRequestInner contains the actual control request
type SDKControlRequestInner struct {
	Subtype string `json:"subtype"` // "interrupt", etc.
}

// SDKControlResponse is sent from server to client (acknowledgment)
type SDKControlResponse struct {
	Type      string `json:"type"` // "control_response"
	RequestID string `json:"request_id"`
	Status    string `json:"status"` // "ok", "error"
	Error     string `json:"error,omitempty"`
}

// SDKControlPermissionRequest is sent from server requesting permission
type SDKControlPermissionRequest struct {
	Type      string                 `json:"type"` // "control_request"
	RequestID string                 `json:"request_id"`
	Request   PermissionRequestInner `json:"request"`
}

// PermissionRequestInner contains permission request details
type PermissionRequestInner struct {
	Subtype    string                 `json:"subtype"` // "permission"
	ToolUseID  string                 `json:"tool_use_id"`
	ToolName   string                 `json:"tool_name"`
	Input      map[string]interface{} `json:"input"`
	Description string                `json:"description,omitempty"`
}

// SDKControlPermissionResponse is sent from client with permission decision
type SDKControlPermissionResponse struct {
	Type      string                    `json:"type"` // "control_response"
	RequestID string                    `json:"request_id"`
	Response  PermissionResponseInner   `json:"response"`
}

// PermissionResponseInner contains permission decision
type PermissionResponseInner struct {
	Behavior     string                 `json:"behavior"` // "allow", "deny"
	UpdatedInput map[string]interface{} `json:"updated_input,omitempty"`
	Message      string                 `json:"message,omitempty"`
}

// SDKControlCancelRequest is sent from server to cancel a pending permission request
type SDKControlCancelRequest struct {
	Type      string `json:"type"` // "control_cancel_request"
	RequestID string `json:"request_id"`
}

// RemoteSessionConfig configures a remote session connection
type RemoteSessionConfig struct {
	SessionID        string
	GetAccessToken   func() string
	OrgUUID          string
	HasInitialPrompt bool
	ViewerOnly       bool
}

// RemotePermissionResponse represents a permission decision
type RemotePermissionResponse struct {
	Behavior     string                 // "allow" or "deny"
	UpdatedInput map[string]interface{} // Modified input if allowed
	Message      string                 // Denial reason if denied
}

// WebSocket state
type WebSocketState string

const (
	WebSocketStateConnecting WebSocketState = "connecting"
	WebSocketStateConnected  WebSocketState = "connected"
	WebSocketStateClosed     WebSocketState = "closed"
)

// WebSocket configuration
const (
	ReconnectDelayMS              = 2000
	MaxReconnectAttempts          = 5
	PingIntervalMS                = 30000
	MaxSessionNotFoundRetries     = 3
)

// Permanent close codes that stop reconnection
var PermanentCloseCodes = map[int]bool{
	4003: true, // unauthorized
}

// SessionsMessage is a union type for all WebSocket messages
type SessionsMessage struct {
	// Common fields
	Type string `json:"type"`
	UUID string `json:"uuid,omitempty"`

	// SDKAssistantMessage fields
	Message *anthropic.MessageResponse `json:"message,omitempty"`
	Error   *string                    `json:"error,omitempty"`

	// SDKPartialAssistantMessage fields
	Event *anthropic.StreamEvent `json:"event,omitempty"`

	// SDKUserMessage fields
	UserMessage   *anthropic.InputMessage `json:"user_message,omitempty"`
	ToolUseResult *ToolUseResult          `json:"tool_use_result,omitempty"`

	// SDKResultMessage fields
	Subtype string   `json:"subtype,omitempty"`
	Result  string   `json:"result,omitempty"`
	Errors  []string `json:"errors,omitempty"`

	// SDKSystemMessage fields
	Model string `json:"model,omitempty"`

	// SDKStatusMessage fields
	Status string `json:"status,omitempty"`

	// SDKToolProgressMessage fields
	ToolUseID          string  `json:"tool_use_id,omitempty"`
	ToolName           string  `json:"tool_name,omitempty"`
	ElapsedTimeSeconds float64 `json:"elapsed_time_seconds,omitempty"`

	// SDKCompactBoundaryMessage fields
	CompactMetadata *CompactMetadata `json:"compact_metadata,omitempty"`

	// Control message fields
	RequestID string                  `json:"request_id,omitempty"`
	Request   interface{}             `json:"request,omitempty"`
	Response  interface{}             `json:"response,omitempty"`
	Timestamp string                  `json:"timestamp,omitempty"`
}

// ConvertedMessage represents the result of SDK message conversion
type ConvertedMessage struct {
	Type        string                 // "message", "stream_event", "ignored"
	Message     interface{}            // Internal message type
	StreamEvent *anthropic.StreamEvent // Stream event if type is "stream_event"
}

// RemoteSessionCallbacks defines callbacks for remote session events
type RemoteSessionCallbacks struct {
	OnMessage             func(message SDKMessage)
	OnPermissionRequest   func(request *PermissionRequestInner, requestID string)
	OnPermissionCancelled func(requestID string, toolUseID *string)
	OnConnected           func()
	OnDisconnected        func()
	OnReconnecting        func()
	OnError               func(error)
}

// SessionsWebSocketCallbacks defines callbacks for WebSocket events
type SessionsWebSocketCallbacks struct {
	OnMessage      func(message *SessionsMessage)
	OnClose        func()
	OnError        func(error)
	OnConnected    func()
	OnReconnecting func()
}

// AuthMessage is sent after WebSocket connection to authenticate
type AuthMessage struct {
	Type       string         `json:"type"` // "auth"
	Credential AuthCredential `json:"credential"`
}

// AuthCredential contains authentication details
type AuthCredential struct {
	Type  string `json:"type"`  // "oauth"
	Token string `json:"token"` // access token
}

// ConvertOptions configures SDK message conversion behavior
type ConvertOptions struct {
	ConvertToolResults      bool
	ConvertUserTextMessages bool
}
