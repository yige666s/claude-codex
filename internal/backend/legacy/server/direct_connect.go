package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// DirectConnectConfig holds configuration for a direct connect session
type DirectConnectConfig struct {
	ServerURL  string
	SessionID  string
	WsURL      string
	AuthToken  string
}

// SDKMessage represents a message from the SDK
type SDKMessage struct {
	Type    string          `json:"type"`
	Message json.RawMessage `json:"message,omitempty"`
}

// SDKControlPermissionRequest represents a permission request
type SDKControlPermissionRequest struct {
	Subtype   string          `json:"subtype"`
	RequestID string          `json:"request_id,omitempty"`
	Data      json.RawMessage `json:"data,omitempty"`
}

// RemotePermissionResponse represents a permission response
type RemotePermissionResponse struct {
	Behavior     string          `json:"behavior"`
	UpdatedInput json.RawMessage `json:"updatedInput,omitempty"`
	Message      string          `json:"message,omitempty"`
}

// DirectConnectCallbacks defines callbacks for direct connect events
type DirectConnectCallbacks struct {
	OnMessage            func(message SDKMessage)
	OnPermissionRequest  func(request SDKControlPermissionRequest, requestID string)
	OnConnected          func()
	OnDisconnected       func()
	OnError              func(error)
}

// DirectConnectSessionManager manages a direct connect WebSocket session
type DirectConnectSessionManager struct {
	config    DirectConnectConfig
	callbacks DirectConnectCallbacks
	conn      *websocket.Conn
	mu        sync.RWMutex
	done      chan struct{}
}

// NewDirectConnectSessionManager creates a new direct connect session manager
func NewDirectConnectSessionManager(config DirectConnectConfig, callbacks DirectConnectCallbacks) *DirectConnectSessionManager {
	return &DirectConnectSessionManager{
		config:    config,
		callbacks: callbacks,
		done:      make(chan struct{}),
	}
}

// Connect establishes the WebSocket connection
func (m *DirectConnectSessionManager) Connect() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	header := http.Header{}
	if m.config.AuthToken != "" {
		header.Set("Authorization", "Bearer "+m.config.AuthToken)
	}

	conn, _, err := websocket.DefaultDialer.Dial(m.config.WsURL, header)
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}

	m.conn = conn

	if m.callbacks.OnConnected != nil {
		m.callbacks.OnConnected()
	}

	go m.readLoop()

	return nil
}

// readLoop reads messages from the WebSocket
func (m *DirectConnectSessionManager) readLoop() {
	defer func() {
		close(m.done)
		if m.callbacks.OnDisconnected != nil {
			m.callbacks.OnDisconnected()
		}
	}()

	for {
		_, message, err := m.conn.ReadMessage()
		if err != nil {
			if m.callbacks.OnError != nil {
				m.callbacks.OnError(err)
			}
			return
		}

		m.handleMessage(message)
	}
}

// handleMessage processes incoming messages
func (m *DirectConnectSessionManager) handleMessage(data []byte) {
	var msg map[string]interface{}
	if err := json.Unmarshal(data, &msg); err != nil {
		return
	}

	msgType, ok := msg["type"].(string)
	if !ok {
		return
	}

	switch msgType {
	case "control_request":
		m.handleControlRequest(msg)
	case "control_response", "keep_alive", "control_cancel_request", "streamlined_text", "streamlined_tool_use_summary":
		// Skip these message types
		return
	case "system":
		if subtype, ok := msg["subtype"].(string); ok && subtype == "post_turn_summary" {
			return
		}
		fallthrough
	default:
		// Forward SDK messages
		if m.callbacks.OnMessage != nil {
			var sdkMsg SDKMessage
			if err := json.Unmarshal(data, &sdkMsg); err == nil {
				m.callbacks.OnMessage(sdkMsg)
			}
		}
	}
}

// handleControlRequest processes control requests
func (m *DirectConnectSessionManager) handleControlRequest(msg map[string]interface{}) {
	request, ok := msg["request"].(map[string]interface{})
	if !ok {
		return
	}

	subtype, ok := request["subtype"].(string)
	if !ok {
		return
	}

	requestID, _ := msg["request_id"].(string)

	if subtype == "can_use_tool" {
		if m.callbacks.OnPermissionRequest != nil {
			reqData, _ := json.Marshal(request)
			var permReq SDKControlPermissionRequest
			if err := json.Unmarshal(reqData, &permReq); err == nil {
				m.callbacks.OnPermissionRequest(permReq, requestID)
			}
		}
	} else {
		// Send error response for unsupported subtypes
		m.sendErrorResponse(requestID, fmt.Sprintf("Unsupported control request subtype: %s", subtype))
	}
}

// SendMessage sends a user message to the server
func (m *DirectConnectSessionManager) SendMessage(content interface{}) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.conn == nil {
		return fmt.Errorf("not connected")
	}

	message := map[string]interface{}{
		"type": "user",
		"message": map[string]interface{}{
			"role":    "user",
			"content": content,
		},
		"parent_tool_use_id": nil,
		"session_id":         "",
	}

	return m.conn.WriteJSON(message)
}

// RespondToPermissionRequest sends a permission response
func (m *DirectConnectSessionManager) RespondToPermissionRequest(requestID string, result RemotePermissionResponse) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.conn == nil {
		return fmt.Errorf("not connected")
	}

	response := map[string]interface{}{
		"type": "control_response",
		"response": map[string]interface{}{
			"subtype":    "success",
			"request_id": requestID,
			"response": map[string]interface{}{
				"behavior": result.Behavior,
			},
		},
	}

	respData := response["response"].(map[string]interface{})["response"].(map[string]interface{})
	if result.Behavior == "allow" {
		respData["updatedInput"] = result.UpdatedInput
	} else {
		respData["message"] = result.Message
	}

	return m.conn.WriteJSON(response)
}

// SendInterrupt sends an interrupt signal to cancel the current request
func (m *DirectConnectSessionManager) SendInterrupt() error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.conn == nil {
		return fmt.Errorf("not connected")
	}

	request := map[string]interface{}{
		"type":       "control_request",
		"request_id": generateUUID(),
		"request": map[string]interface{}{
			"subtype": "interrupt",
		},
	}

	return m.conn.WriteJSON(request)
}

// sendErrorResponse sends an error response for a control request
func (m *DirectConnectSessionManager) sendErrorResponse(requestID, errorMsg string) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.conn == nil {
		return fmt.Errorf("not connected")
	}

	response := map[string]interface{}{
		"type": "control_response",
		"response": map[string]interface{}{
			"subtype":    "error",
			"request_id": requestID,
			"error":      errorMsg,
		},
	}

	return m.conn.WriteJSON(response)
}

// Disconnect closes the WebSocket connection
func (m *DirectConnectSessionManager) Disconnect() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.conn == nil {
		return nil
	}

	err := m.conn.Close()
	m.conn = nil
	return err
}

// IsConnected returns whether the connection is active
func (m *DirectConnectSessionManager) IsConnected() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.conn != nil
}

// Wait blocks until the connection is closed
func (m *DirectConnectSessionManager) Wait() {
	<-m.done
}

// generateUUID generates a simple UUID (placeholder - use proper UUID library in production)
func generateUUID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}
