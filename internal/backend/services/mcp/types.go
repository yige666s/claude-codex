package mcp

import (
	"encoding/json"

	"claude-codex/internal/app/config"
	mcpcore "claude-codex/internal/harness/mcp"
	toolkit "claude-codex/internal/harness/tools"
)

type ConnectionType string

const (
	ConnectionTypeConnected ConnectionType = "connected"
	ConnectionTypeFailed    ConnectionType = "failed"
	ConnectionTypeNeedsAuth ConnectionType = "needs-auth"
	ConnectionTypePending   ConnectionType = "pending"
	ConnectionTypeDisabled  ConnectionType = "disabled"
)

type ServerConnection struct {
	Name         string
	Type         ConnectionType
	Config       config.MCPServerConfig
	Client       *mcpcore.Client
	Instructions string
	Error        string
}

type ToolDescriptor struct {
	Name            string
	ServerName      string
	Description     string
	InputSchema     json.RawMessage
	IsResourceTool  bool
	IsSpecialClient bool
}

type FetchResult struct {
	Connection ServerConnection
	Tools      []toolkit.Tool
	Resources  []mcpcore.ResourceDefinition
}

type PersistedToolResult struct {
	Filepath     string
	OriginalSize int
	Preview      string
	HasMore      bool
}
