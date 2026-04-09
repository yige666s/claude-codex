package remote

import (
	"encoding/json"
	"fmt"
	"sync"
)

// RemoteSessionManager manages a remote CCR session
type RemoteSessionManager struct {
	config                     RemoteSessionConfig
	callbacks                  RemoteSessionCallbacks
	websocket                  *SessionsWebSocket
	pendingPermissionRequests  map[string]*PermissionRequestInner
	mu                         sync.RWMutex
}

// NewRemoteSessionManager creates a new remote session manager
func NewRemoteSessionManager(
	config RemoteSessionConfig,
	callbacks RemoteSessionCallbacks,
) *RemoteSessionManager {
	return &RemoteSessionManager{
		config:                    config,
		callbacks:                 callbacks,
		pendingPermissionRequests: make(map[string]*PermissionRequestInner),
	}
}

// Connect establishes connection to the remote session
func (m *RemoteSessionManager) Connect() error {
	wsCallbacks := SessionsWebSocketCallbacks{
		OnMessage: func(message *SessionsMessage) {
			m.handleMessage(message)
		},
		OnConnected: func() {
			if m.callbacks.OnConnected != nil {
				m.callbacks.OnConnected()
			}
		},
		OnClose: func() {
			if m.callbacks.OnDisconnected != nil {
				m.callbacks.OnDisconnected()
			}
		},
		OnReconnecting: func() {
			if m.callbacks.OnReconnecting != nil {
				m.callbacks.OnReconnecting()
			}
		},
		OnError: func(err error) {
			if m.callbacks.OnError != nil {
				m.callbacks.OnError(err)
			}
		},
	}

	m.websocket = NewSessionsWebSocket(
		m.config.SessionID,
		m.config.OrgUUID,
		m.config.GetAccessToken,
		wsCallbacks,
	)

	return m.websocket.Connect()
}

// handleMessage processes incoming WebSocket messages
func (m *RemoteSessionManager) handleMessage(message *SessionsMessage) {
	// Handle control requests (permission prompts from CCR)
	if message.Type == "control_request" {
		m.handleControlRequest(message)
		return
	}

	// Handle control cancel requests
	if message.Type == "control_cancel_request" {
		m.handleControlCancelRequest(message)
		return
	}

	// Handle control responses (acknowledgments)
	if message.Type == "control_response" {
		// Just log, no action needed
		return
	}

	// Forward SDK messages to callback
	if m.callbacks.OnMessage != nil {
		sdkMsg := m.convertToSDKMessage(message)
		if sdkMsg != nil {
			m.callbacks.OnMessage(sdkMsg)
		}
	}
}

// handleControlRequest processes control requests from CCR
func (m *RemoteSessionManager) handleControlRequest(message *SessionsMessage) {
	requestID := message.RequestID
	if requestID == "" {
		return
	}

	// Parse the request field
	requestData, err := json.Marshal(message.Request)
	if err != nil {
		return
	}

	var request PermissionRequestInner
	if err := json.Unmarshal(requestData, &request); err != nil {
		return
	}

	// Only handle permission requests
	if request.Subtype != "permission" {
		return
	}

	// Store pending request
	m.mu.Lock()
	m.pendingPermissionRequests[requestID] = &request
	m.mu.Unlock()

	// Notify callback
	if m.callbacks.OnPermissionRequest != nil {
		m.callbacks.OnPermissionRequest(&request, requestID)
	}
}

// handleControlCancelRequest processes permission cancellation
func (m *RemoteSessionManager) handleControlCancelRequest(message *SessionsMessage) {
	requestID := message.RequestID
	if requestID == "" {
		return
	}

	m.mu.Lock()
	request := m.pendingPermissionRequests[requestID]
	delete(m.pendingPermissionRequests, requestID)
	m.mu.Unlock()

	var toolUseID *string
	if request != nil {
		toolUseID = &request.ToolUseID
	}

	if m.callbacks.OnPermissionCancelled != nil {
		m.callbacks.OnPermissionCancelled(requestID, toolUseID)
	}
}

// convertToSDKMessage converts SessionsMessage to SDKMessage interface
func (m *RemoteSessionManager) convertToSDKMessage(message *SessionsMessage) SDKMessage {
	switch message.Type {
	case string(SDKMessageTypeAssistant):
		return &SDKAssistantMessage{
			Type:      SDKMessageTypeAssistant,
			UUID:      message.UUID,
			Message:   *message.Message,
			Error:     message.Error,
			Timestamp: message.Timestamp,
		}
	case string(SDKMessageTypePartial):
		return &SDKPartialAssistantMessage{
			Type:  SDKMessageTypePartial,
			UUID:  message.UUID,
			Event: *message.Event,
		}
	case string(SDKMessageTypeResult):
		return &SDKResultMessage{
			Type:    SDKMessageTypeResult,
			UUID:    message.UUID,
			Subtype: message.Subtype,
			Result:  message.Result,
			Errors:  message.Errors,
		}
	case string(SDKMessageTypeSystem):
		return &SDKSystemMessage{
			Type:    SDKMessageTypeSystem,
			UUID:    message.UUID,
			Subtype: message.Subtype,
			Model:   message.Model,
		}
	case string(SDKMessageTypeStatus):
		return &SDKStatusMessage{
			Type:   SDKMessageTypeStatus,
			UUID:   message.UUID,
			Status: message.Status,
		}
	case string(SDKMessageTypeToolProgress):
		return &SDKToolProgressMessage{
			Type:               SDKMessageTypeToolProgress,
			UUID:               message.UUID,
			ToolUseID:          message.ToolUseID,
			ToolName:           message.ToolName,
			ElapsedTimeSeconds: message.ElapsedTimeSeconds,
		}
	case string(SDKMessageTypeCompactBoundary):
		return &SDKCompactBoundaryMessage{
			Type:            SDKMessageTypeCompactBoundary,
			UUID:            message.UUID,
			CompactMetadata: message.CompactMetadata,
		}
	default:
		return nil
	}
}

// SendMessage sends a user message to the remote session
func (m *RemoteSessionManager) SendMessage(content string) error {
	// This would typically use HTTP POST to /v1/sessions/{id}/messages
	// For now, we'll just return an error indicating it's not implemented
	return fmt.Errorf("SendMessage not implemented - use HTTP API")
}

// RespondToPermission sends a permission response
func (m *RemoteSessionManager) RespondToPermission(
	requestID string,
	response RemotePermissionResponse,
) error {
	m.mu.Lock()
	request := m.pendingPermissionRequests[requestID]
	delete(m.pendingPermissionRequests, requestID)
	m.mu.Unlock()

	if request == nil {
		return fmt.Errorf("no pending permission request with ID: %s", requestID)
	}

	if m.websocket == nil {
		return fmt.Errorf("websocket not connected")
	}

	permResponse := &SDKControlPermissionResponse{
		Type:      "control_response",
		RequestID: requestID,
		Response: PermissionResponseInner{
			Behavior:     response.Behavior,
			UpdatedInput: response.UpdatedInput,
			Message:      response.Message,
		},
	}

	return m.websocket.SendControlResponse(permResponse)
}

// IsConnected returns true if connected to remote session
func (m *RemoteSessionManager) IsConnected() bool {
	if m.websocket == nil {
		return false
	}
	return m.websocket.IsConnected()
}

// CancelSession sends an interrupt signal to cancel current request
func (m *RemoteSessionManager) CancelSession() error {
	if m.websocket == nil {
		return fmt.Errorf("not connected")
	}

	return m.websocket.SendControlRequest(&SDKControlRequestInner{
		Subtype: "interrupt",
	})
}

// GetSessionID returns the session ID
func (m *RemoteSessionManager) GetSessionID() string {
	return m.config.SessionID
}

// Disconnect closes the connection to the remote session
func (m *RemoteSessionManager) Disconnect() {
	if m.websocket != nil {
		m.websocket.Close()
		m.websocket = nil
	}

	m.mu.Lock()
	m.pendingPermissionRequests = make(map[string]*PermissionRequestInner)
	m.mu.Unlock()
}

// Reconnect forces a reconnection to the remote session
func (m *RemoteSessionManager) Reconnect() {
	if m.websocket != nil {
		m.websocket.Reconnect()
	}
}

// CreateRemoteSessionConfig creates a remote session config
func CreateRemoteSessionConfig(
	sessionID string,
	getAccessToken func() string,
	orgUUID string,
	hasInitialPrompt bool,
	viewerOnly bool,
) RemoteSessionConfig {
	return RemoteSessionConfig{
		SessionID:        sessionID,
		GetAccessToken:   getAccessToken,
		OrgUUID:          orgUUID,
		HasInitialPrompt: hasInitialPrompt,
		ViewerOnly:       viewerOnly,
	}
}
