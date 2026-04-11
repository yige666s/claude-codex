package mcp

import (
	"errors"
	"sync"
)

type JSONRPCMessage struct {
	ID      int64     `json:"id,omitempty"`
	Method  string    `json:"method,omitempty"`
	Params  jsonRaw   `json:"params,omitempty"`
	Result  jsonRaw   `json:"result,omitempty"`
	Error   *RPCError `json:"error,omitempty"`
	JSONRPC string    `json:"jsonrpc,omitempty"`
}

type jsonRaw = []byte

type InProcessTransport struct {
	mu        sync.RWMutex
	peer      *InProcessTransport
	closed    bool
	OnClose   func()
	OnError   func(error)
	OnMessage func(JSONRPCMessage)
}

func CreateLinkedTransportPair() (*InProcessTransport, *InProcessTransport) {
	a := &InProcessTransport{}
	b := &InProcessTransport{}
	a.peer = b
	b.peer = a
	return a, b
}

func (t *InProcessTransport) Start() error { return nil }

func (t *InProcessTransport) Send(message JSONRPCMessage) error {
	t.mu.RLock()
	if t.closed {
		t.mu.RUnlock()
		return errors.New("transport is closed")
	}
	peer := t.peer
	t.mu.RUnlock()
	if peer == nil {
		return nil
	}
	go func() {
		peer.mu.RLock()
		handler := peer.OnMessage
		peer.mu.RUnlock()
		if handler != nil {
			handler(message)
		}
	}()
	return nil
}

func (t *InProcessTransport) Close() error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil
	}
	t.closed = true
	onClose := t.OnClose
	peer := t.peer
	t.mu.Unlock()

	if onClose != nil {
		onClose()
	}
	if peer != nil {
		peer.mu.Lock()
		if !peer.closed {
			peer.closed = true
			onPeerClose := peer.OnClose
			peer.mu.Unlock()
			if onPeerClose != nil {
				onPeerClose()
			}
		} else {
			peer.mu.Unlock()
		}
	}
	return nil
}
