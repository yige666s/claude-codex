package mcp

import "errors"

type SendMCPMessageCallback func(serverName string, message JSONRPCMessage) (JSONRPCMessage, error)

type SDKControlClientTransport struct {
	serverName string
	send       SendMCPMessageCallback
	closed     bool
	OnClose    func()
	OnError    func(error)
	OnMessage  func(JSONRPCMessage)
}

func NewSDKControlClientTransport(serverName string, send SendMCPMessageCallback) *SDKControlClientTransport {
	return &SDKControlClientTransport{serverName: serverName, send: send}
}

func (t *SDKControlClientTransport) Start() error { return nil }

func (t *SDKControlClientTransport) Send(message JSONRPCMessage) error {
	if t.closed {
		return errors.New("transport is closed")
	}
	response, err := t.send(t.serverName, message)
	if err != nil {
		if t.OnError != nil {
			t.OnError(err)
		}
		return err
	}
	if t.OnMessage != nil {
		t.OnMessage(response)
	}
	return nil
}

func (t *SDKControlClientTransport) Close() error {
	if t.closed {
		return nil
	}
	t.closed = true
	if t.OnClose != nil {
		t.OnClose()
	}
	return nil
}

type SDKControlServerTransport struct {
	send      func(JSONRPCMessage)
	closed    bool
	OnClose   func()
	OnError   func(error)
	OnMessage func(JSONRPCMessage)
}

func NewSDKControlServerTransport(send func(JSONRPCMessage)) *SDKControlServerTransport {
	return &SDKControlServerTransport{send: send}
}

func (t *SDKControlServerTransport) Start() error { return nil }

func (t *SDKControlServerTransport) Send(message JSONRPCMessage) error {
	if t.closed {
		return errors.New("transport is closed")
	}
	if t.send != nil {
		t.send(message)
	}
	return nil
}

func (t *SDKControlServerTransport) Close() error {
	if t.closed {
		return nil
	}
	t.closed = true
	if t.OnClose != nil {
		t.OnClose()
	}
	return nil
}
