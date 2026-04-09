package mcp

import "encoding/json"

type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type Event struct {
	Name string          `json:"name"`
	Data json.RawMessage `json:"data,omitempty"`
}

type ToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type ListToolsResult struct {
	Tools []ToolDefinition `json:"tools"`
}

type CallToolParams struct {
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

type CallToolResult struct {
	Output string `json:"output"`
}

// InitializeResult is the response from a server's initialize handshake.
// The optional Instructions field carries per-server system prompt text.
type InitializeResult struct {
	ProtocolVersion string `json:"protocolVersion,omitempty"`
	ServerInfo      struct {
		Name    string `json:"name,omitempty"`
		Version string `json:"version,omitempty"`
	} `json:"serverInfo,omitempty"`
	Instructions string `json:"instructions,omitempty"`
}
