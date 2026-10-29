package upstreamproxy

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
)

// StartUpstreamProxyRelay starts the CONNECT-over-WebSocket relay.
// Returns the ephemeral port it bound and a stop function.
func StartUpstreamProxyRelay(ctx context.Context, opts *RelayOptions) (*Relay, error) {
	// Create TCP listener on ephemeral port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("failed to create listener: %w", err)
	}

	port := listener.Addr().(*net.TCPAddr).Port
	stopChan := make(chan struct{})
	var wg sync.WaitGroup

	// Start accepting connections
	wg.Add(1)
	go func() {
		defer wg.Done()
		acceptLoop(listener, opts, stopChan)
	}()

	relay := &Relay{
		Port: port,
		Stop: func() {
			close(stopChan)
			listener.Close()
			wg.Wait()
		},
	}

	return relay, nil
}

// acceptLoop accepts incoming CONNECT requests
func acceptLoop(listener net.Listener, opts *RelayOptions, stopChan chan struct{}) {
	for {
		select {
		case <-stopChan:
			return
		default:
		}

		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-stopChan:
				return
			default:
				logWarning(fmt.Sprintf("[upstreamproxy] accept error: %v", err))
				continue
			}
		}

		go handleConnection(conn, opts)
	}
}

// handleConnection handles a single CONNECT tunnel
func handleConnection(conn net.Conn, opts *RelayOptions) {
	defer conn.Close()

	// Read CONNECT request
	reader := bufio.NewReader(conn)
	requestLine, err := reader.ReadString('\n')
	if err != nil {
		return
	}

	// Parse CONNECT request
	parts := strings.Fields(requestLine)
	if len(parts) < 2 || parts[0] != "CONNECT" {
		conn.Write([]byte("HTTP/1.1 400 Bad Request\r\n\r\n"))
		return
	}

	target := parts[1]

	// Read headers until empty line
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		if line == "\r\n" || line == "\n" {
			break
		}
	}

	// TODO: Open WebSocket connection to CCR
	// For now, return 502 as the WebSocket implementation is pending
	conn.Write([]byte("HTTP/1.1 502 Bad Gateway\r\n\r\nWebSocket relay not yet implemented\r\n"))

	logDebug(fmt.Sprintf("[upstreamproxy] CONNECT to %s (WebSocket not yet implemented)", target))
}

// forwardToWS forwards data to the WebSocket, chunking if necessary
func forwardToWS(ws WebSocketLike, data []byte) error {
	if ws.ReadyState() != WSOpen {
		return fmt.Errorf("websocket not open")
	}

	// Chunk data if it exceeds MaxChunkBytes
	for offset := 0; offset < len(data); offset += MaxChunkBytes {
		end := offset + MaxChunkBytes
		if end > len(data) {
			end = len(data)
		}
		chunk := data[offset:end]
		encoded := EncodeChunk(chunk)
		if err := ws.Send(encoded); err != nil {
			return err
		}
	}

	return nil
}

// sendKeepalive sends an empty chunk as keepalive
func sendKeepalive(ws WebSocketLike) error {
	if ws.ReadyState() == WSOpen {
		return ws.Send(EncodeChunk([]byte{}))
	}
	return nil
}

// cleanupConn cleans up connection state
func cleanupConn(state *ConnState) {
	if state == nil {
		return
	}

	// Stop pinger
	select {
	case <-state.Pinger:
	default:
		close(state.Pinger)
	}

	// Close WebSocket
	if state.WS != nil && state.WS.ReadyState() <= WSOpen {
		state.WS.Close()
	}
}
