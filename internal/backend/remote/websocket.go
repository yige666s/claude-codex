package remote

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

// SessionsWebSocket manages WebSocket connection to CCR sessions
type SessionsWebSocket struct {
	sessionID     string
	orgUUID       string
	getAccessToken func() string
	callbacks     SessionsWebSocketCallbacks

	mu                      sync.RWMutex
	conn                    *websocket.Conn
	state                   WebSocketState
	reconnectAttempts       int
	sessionNotFoundRetries  int
	pingTicker              *time.Ticker
	reconnectTimer          *time.Timer
	ctx                     context.Context
	cancel                  context.CancelFunc
	closeOnce               sync.Once
}

// NewSessionsWebSocket creates a new WebSocket client
func NewSessionsWebSocket(
	sessionID string,
	orgUUID string,
	getAccessToken func() string,
	callbacks SessionsWebSocketCallbacks,
) *SessionsWebSocket {
	ctx, cancel := context.WithCancel(context.Background())
	return &SessionsWebSocket{
		sessionID:      sessionID,
		orgUUID:        orgUUID,
		getAccessToken: getAccessToken,
		callbacks:      callbacks,
		state:          WebSocketStateClosed,
		ctx:            ctx,
		cancel:         cancel,
	}
}

// Connect establishes WebSocket connection
func (ws *SessionsWebSocket) Connect() error {
	ws.mu.Lock()
	if ws.state == WebSocketStateConnecting {
		ws.mu.Unlock()
		return nil
	}
	ws.state = WebSocketStateConnecting
	ws.mu.Unlock()

	// Build WebSocket URL
	baseURL := "wss://api.anthropic.com"
	url := fmt.Sprintf("%s/v1/sessions/ws/%s/subscribe?organization_uuid=%s",
		baseURL, ws.sessionID, ws.orgUUID)

	// Get fresh access token
	accessToken := ws.getAccessToken()

	// Set up headers
	header := http.Header{}
	header.Set("Authorization", fmt.Sprintf("Bearer %s", accessToken))
	header.Set("anthropic-version", "2023-06-01")

	// Connect
	conn, _, err := websocket.DefaultDialer.Dial(url, header)
	if err != nil {
		ws.mu.Lock()
		ws.state = WebSocketStateClosed
		ws.mu.Unlock()
		return fmt.Errorf("websocket dial failed: %w", err)
	}

	ws.mu.Lock()
	ws.conn = conn
	ws.state = WebSocketStateConnected
	ws.reconnectAttempts = 0
	ws.sessionNotFoundRetries = 0
	ws.mu.Unlock()

	// Start ping interval
	ws.startPingInterval()

	// Notify connected
	if ws.callbacks.OnConnected != nil {
		ws.callbacks.OnConnected()
	}

	// Start message reader
	go ws.readMessages()

	return nil
}

// readMessages reads messages from WebSocket
func (ws *SessionsWebSocket) readMessages() {
	defer func() {
		ws.mu.RLock()
		conn := ws.conn
		ws.mu.RUnlock()

		if conn != nil {
			conn.Close()
		}
	}()

	for {
		select {
		case <-ws.ctx.Done():
			return
		default:
		}

		ws.mu.RLock()
		conn := ws.conn
		state := ws.state
		ws.mu.RUnlock()

		if conn == nil || state != WebSocketStateConnected {
			return
		}

		_, data, err := conn.ReadMessage()
		if err != nil {
			// Check if it's a close error
			if closeErr, ok := err.(*websocket.CloseError); ok {
				ws.handleClose(closeErr.Code)
			} else {
				// Network error
				ws.handleClose(1006) // Abnormal closure
			}
			return
		}

		ws.handleMessage(data)
	}
}

// handleMessage processes incoming WebSocket messages
func (ws *SessionsWebSocket) handleMessage(data []byte) {
	var msg SessionsMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		if ws.callbacks.OnError != nil {
			ws.callbacks.OnError(fmt.Errorf("failed to parse message: %w", err))
		}
		return
	}

	if ws.callbacks.OnMessage != nil {
		ws.callbacks.OnMessage(&msg)
	}
}

// handleClose handles WebSocket closure
func (ws *SessionsWebSocket) handleClose(code int) {
	ws.mu.Lock()
	ws.state = WebSocketStateClosed
	ws.stopPingInterval()
	ws.mu.Unlock()

	// Check if it's a permanent close code
	if PermanentCloseCodes[code] {
		if ws.callbacks.OnClose != nil {
			ws.callbacks.OnClose()
		}
		return
	}

	// Handle session not found with limited retries
	if code == 4001 {
		ws.mu.Lock()
		ws.sessionNotFoundRetries++
		retries := ws.sessionNotFoundRetries
		ws.mu.Unlock()

		if retries >= MaxSessionNotFoundRetries {
			if ws.callbacks.OnClose != nil {
				ws.callbacks.OnClose()
			}
			return
		}
	}

	// Attempt reconnection
	ws.mu.Lock()
	ws.reconnectAttempts++
	attempts := ws.reconnectAttempts
	ws.mu.Unlock()

	if attempts >= MaxReconnectAttempts {
		if ws.callbacks.OnClose != nil {
			ws.callbacks.OnClose()
		}
		return
	}

	// Schedule reconnect
	if ws.callbacks.OnReconnecting != nil {
		ws.callbacks.OnReconnecting()
	}

	ws.mu.Lock()
	ws.reconnectTimer = time.AfterFunc(ReconnectDelayMS*time.Millisecond, func() {
		ws.Connect()
	})
	ws.mu.Unlock()
}

// startPingInterval starts periodic ping messages
func (ws *SessionsWebSocket) startPingInterval() {
	ws.mu.Lock()
	defer ws.mu.Unlock()

	if ws.pingTicker != nil {
		ws.pingTicker.Stop()
	}

	ws.pingTicker = time.NewTicker(PingIntervalMS * time.Millisecond)
	go func() {
		for {
			select {
			case <-ws.ctx.Done():
				return
			case <-ws.pingTicker.C:
				ws.mu.RLock()
				conn := ws.conn
				state := ws.state
				ws.mu.RUnlock()

				if conn != nil && state == WebSocketStateConnected {
					if err := conn.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(10*time.Second)); err != nil {
						if ws.callbacks.OnError != nil {
							ws.callbacks.OnError(fmt.Errorf("ping failed: %w", err))
						}
					}
				}
			}
		}
	}()
}

// stopPingInterval stops the ping ticker
func (ws *SessionsWebSocket) stopPingInterval() {
	if ws.pingTicker != nil {
		ws.pingTicker.Stop()
		ws.pingTicker = nil
	}
}

// SendControlResponse sends a control response message
func (ws *SessionsWebSocket) SendControlResponse(response *SDKControlPermissionResponse) error {
	ws.mu.RLock()
	conn := ws.conn
	state := ws.state
	ws.mu.RUnlock()

	if conn == nil || state != WebSocketStateConnected {
		return fmt.Errorf("not connected")
	}

	data, err := json.Marshal(response)
	if err != nil {
		return fmt.Errorf("failed to marshal response: %w", err)
	}

	return conn.WriteMessage(websocket.TextMessage, data)
}

// SendControlRequest sends a control request message
func (ws *SessionsWebSocket) SendControlRequest(request *SDKControlRequestInner) error {
	ws.mu.RLock()
	conn := ws.conn
	state := ws.state
	ws.mu.RUnlock()

	if conn == nil || state != WebSocketStateConnected {
		return fmt.Errorf("not connected")
	}

	msg := SDKControlRequest{
		Type:      "control_request",
		RequestID: uuid.New().String(),
		Request:   *request,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	return conn.WriteMessage(websocket.TextMessage, data)
}

// IsConnected returns true if WebSocket is connected
func (ws *SessionsWebSocket) IsConnected() bool {
	ws.mu.RLock()
	defer ws.mu.RUnlock()
	return ws.state == WebSocketStateConnected
}

// Close closes the WebSocket connection
func (ws *SessionsWebSocket) Close() {
	ws.closeOnce.Do(func() {
		ws.mu.Lock()
		ws.state = WebSocketStateClosed
		ws.stopPingInterval()

		if ws.reconnectTimer != nil {
			ws.reconnectTimer.Stop()
			ws.reconnectTimer = nil
		}

		if ws.conn != nil {
			ws.conn.Close()
			ws.conn = nil
		}
		ws.mu.Unlock()

		ws.cancel()
	})
}

// Reconnect forces a reconnection
func (ws *SessionsWebSocket) Reconnect() {
	ws.mu.Lock()
	ws.reconnectAttempts = 0
	ws.sessionNotFoundRetries = 0
	ws.mu.Unlock()

	ws.Close()

	time.Sleep(500 * time.Millisecond)
	ws.Connect()
}
