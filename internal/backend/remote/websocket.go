package remote

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	backendretry "claude-codex/internal/backend/retry"
	"claude-codex/internal/backend/workers"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

// SessionsWebSocket manages WebSocket connection to CCR sessions
type SessionsWebSocket struct {
	sessionID      string
	orgUUID        string
	getAccessToken func() string
	callbacks      SessionsWebSocketCallbacks

	mu                     sync.RWMutex
	conn                   *websocket.Conn
	state                  WebSocketState
	reconnectAttempts      int
	sessionNotFoundRetries int
	reconnectScheduled     bool
	ctx                    context.Context
	cancel                 context.CancelFunc
	workers                *workers.Group
	reconnectPolicy        backendretry.Policy
	closeOnce              sync.Once
}

// NewSessionsWebSocket creates a new WebSocket client
func NewSessionsWebSocket(
	sessionID string,
	orgUUID string,
	getAccessToken func() string,
	callbacks SessionsWebSocketCallbacks,
) *SessionsWebSocket {
	ctx, cancel := context.WithCancel(context.Background())
	logger := slog.Default().With(
		slog.String("component", "remote_websocket"),
		slog.String("session_id", sessionID),
	)
	return &SessionsWebSocket{
		sessionID:      sessionID,
		orgUUID:        orgUUID,
		getAccessToken: getAccessToken,
		callbacks:      callbacks,
		state:          WebSocketStateClosed,
		ctx:            ctx,
		cancel:         cancel,
		workers:        workers.New(ctx, logger),
		reconnectPolicy: backendretry.Policy{
			MaxAttempts: MaxReconnectAttempts,
			BaseDelay:   ReconnectDelayMS * time.Millisecond,
			MaxDelay:    ReconnectDelayMS * time.Millisecond,
		},
	}
}

// Connect establishes WebSocket connection
func (ws *SessionsWebSocket) Connect() error {
	ws.mu.Lock()
	if err := ws.ctx.Err(); err != nil {
		ws.mu.Unlock()
		return err
	}
	if ws.state == WebSocketStateConnecting {
		ws.mu.Unlock()
		return nil
	}
	if ws.state == WebSocketStateConnected {
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
	conn, _, err := websocket.DefaultDialer.DialContext(ws.ctx, url, header)
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
	ws.reconnectScheduled = false
	ws.mu.Unlock()

	// Notify connected
	if ws.callbacks.OnConnected != nil {
		ws.callbacks.OnConnected()
	}

	ws.startConnectionWorkers(conn)

	return nil
}

func (ws *SessionsWebSocket) startConnectionWorkers(conn *websocket.Conn) {
	if ws == nil || conn == nil || ws.workers == nil {
		return
	}
	ws.workers.Start("remote_websocket_reader", func(ctx context.Context) error {
		return ws.readMessages(ctx, conn)
	})
	ws.workers.Start("remote_websocket_ping", func(ctx context.Context) error {
		return ws.pingLoop(ctx, conn)
	})
}

// readMessages reads messages from WebSocket.
func (ws *SessionsWebSocket) readMessages(ctx context.Context, conn *websocket.Conn) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		if !ws.isCurrentConnection(conn) {
			return nil
		}

		_, data, err := conn.ReadMessage()
		if err != nil {
			// Check if it's a close error
			if closeErr, ok := err.(*websocket.CloseError); ok {
				ws.handleClose(closeErr.Code, conn)
			} else {
				// Network error
				ws.handleClose(1006, conn) // Abnormal closure
			}
			return nil
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
func (ws *SessionsWebSocket) handleClose(code int, conn *websocket.Conn) {
	ws.mu.Lock()
	if conn != nil && ws.conn != conn {
		ws.mu.Unlock()
		return
	}
	ws.state = WebSocketStateClosed
	if ws.conn == conn {
		ws.conn = nil
	}
	if ws.ctx.Err() != nil {
		ws.mu.Unlock()
		return
	}
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
	if ws.reconnectScheduled {
		ws.mu.Unlock()
		return
	}
	ws.reconnectAttempts++
	attempts := ws.reconnectAttempts
	maxAttempts := ws.reconnectPolicy.Attempts()
	if maxAttempts <= 0 {
		maxAttempts = MaxReconnectAttempts
	}
	if attempts >= maxAttempts {
		ws.mu.Unlock()
		if ws.callbacks.OnClose != nil {
			ws.callbacks.OnClose()
		}
		return
	}
	ws.reconnectScheduled = true
	ws.mu.Unlock()

	if ws.callbacks.OnReconnecting != nil {
		ws.callbacks.OnReconnecting()
	}

	ws.scheduleReconnect(attempts)
}

func (ws *SessionsWebSocket) scheduleReconnect(attempt int) {
	if ws == nil || ws.workers == nil {
		return
	}
	name := fmt.Sprintf("remote_websocket_reconnect_%d", attempt)
	ws.workers.Start(name, func(ctx context.Context) error {
		currentAttempt := attempt
		for {
			if err := ws.reconnectPolicy.Sleep(ctx, currentAttempt, nil); err != nil {
				return nil
			}
			if err := ws.Connect(); err == nil {
				return nil
			} else if ws.callbacks.OnError != nil {
				ws.callbacks.OnError(err)
			}
			nextAttempt, shouldContinue, notifyClose := ws.nextReconnectAttempt()
			if !shouldContinue {
				if notifyClose && ws.callbacks.OnClose != nil {
					ws.callbacks.OnClose()
				}
				return nil
			}
			if ws.callbacks.OnReconnecting != nil {
				ws.callbacks.OnReconnecting()
			}
			currentAttempt = nextAttempt
		}
	})
}

func (ws *SessionsWebSocket) nextReconnectAttempt() (int, bool, bool) {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	if ws.ctx.Err() != nil || ws.state == WebSocketStateConnected {
		ws.reconnectScheduled = false
		return 0, false, false
	}
	ws.reconnectAttempts++
	attempts := ws.reconnectAttempts
	maxAttempts := ws.reconnectPolicy.Attempts()
	if maxAttempts <= 0 {
		maxAttempts = MaxReconnectAttempts
	}
	if attempts >= maxAttempts {
		ws.reconnectScheduled = false
		return attempts, false, true
	}
	return attempts, true, false
}

func (ws *SessionsWebSocket) pingLoop(ctx context.Context, conn *websocket.Conn) error {
	ticker := time.NewTicker(PingIntervalMS * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if !ws.isCurrentConnection(conn) {
				return nil
			}
			if err := conn.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(10*time.Second)); err != nil {
				if ws.callbacks.OnError != nil {
					ws.callbacks.OnError(fmt.Errorf("ping failed: %w", err))
				}
			}
		}
	}
}

func (ws *SessionsWebSocket) isCurrentConnection(conn *websocket.Conn) bool {
	ws.mu.RLock()
	defer ws.mu.RUnlock()
	return conn != nil && ws.conn == conn && ws.state == WebSocketStateConnected
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

		if ws.conn != nil {
			ws.conn.Close()
			ws.conn = nil
		}
		ws.mu.Unlock()

		ws.cancel()
		stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = ws.workers.Stop(stopCtx)
	})
}

// Reconnect forces a reconnection
func (ws *SessionsWebSocket) Reconnect() {
	ws.mu.Lock()
	ws.reconnectAttempts = 0
	ws.sessionNotFoundRetries = 0
	ws.reconnectScheduled = false
	conn := ws.conn
	ws.conn = nil
	ws.state = WebSocketStateClosed
	ws.mu.Unlock()

	if conn != nil {
		_ = conn.Close()
	}
	if err := backendretry.Sleep(ws.ctx, 500*time.Millisecond); err != nil {
		return
	}
	if err := ws.Connect(); err != nil && ws.callbacks.OnError != nil {
		ws.callbacks.OnError(err)
	}
}
