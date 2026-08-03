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
	Name        string           `json:"name"`
	Description string           `json:"description"`
	InputSchema json.RawMessage  `json:"input_schema"`
	Annotations *ToolAnnotations `json:"annotations,omitempty"`
}

// ToolAnnotations carries the MCP tool-behavior hints needed by the runtime's
// permission boundary. A missing readOnlyHint remains conservative (execute).
type ToolAnnotations struct {
	Title        string `json:"title,omitempty"`
	ReadOnlyHint bool   `json:"readOnlyHint,omitempty"`
}

func (t *ToolDefinition) UnmarshalJSON(data []byte) error {
	type wireToolDefinition struct {
		Name             string           `json:"name"`
		Description      string           `json:"description"`
		InputSchema      json.RawMessage  `json:"input_schema"`
		CamelInputSchema json.RawMessage  `json:"inputSchema"`
		Annotations      *ToolAnnotations `json:"annotations,omitempty"`
	}
	var wire wireToolDefinition
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	t.Name = wire.Name
	t.Description = wire.Description
	t.InputSchema = wire.InputSchema
	t.Annotations = wire.Annotations
	if len(t.InputSchema) == 0 {
		t.InputSchema = wire.CamelInputSchema
	}
	return nil
}

type ResourceDefinition struct {
	URI         string `json:"uri"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mime_type,omitempty"`
}

type ListToolsResult struct {
	Tools []ToolDefinition `json:"tools"`
}

type ListResourcesResult struct {
	Resources []ResourceDefinition `json:"resources"`
}

type CallToolParams struct {
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

type CallToolResult struct {
	Output string `json:"output"`
}

type ReadResourceParams struct {
	URI string `json:"uri"`
}

type ReadResourceResult struct {
	Contents []ResourceContent `json:"contents"`
}

type ResourceContent struct {
	URI      string `json:"uri"`
	MimeType string `json:"mime_type,omitempty"`
	Text     string `json:"text,omitempty"`
	Blob     string `json:"blob,omitempty"`
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
