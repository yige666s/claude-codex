// Package mcpresources implements ListMcpResources and ReadMcpResource tools.
package mcpresources

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ding/claude-code/claude-go/internal/harness/permissions"
	toolkit "github.com/ding/claude-code/claude-go/internal/harness/tools"
)

// ---- ListMcpResources ----

type listTool struct {
	ServerName string
}

func NewListMcpResources(serverName string) toolkit.Tool { return &listTool{ServerName: serverName} }

func (t *listTool) Name() string { return "ListMcpResources" }
func (t *listTool) Description() string {
	return `List available resources from an MCP server.

Resources are data objects exposed by MCP servers (files, database records, etc.).
Use this to discover what resources are available before reading them with ReadMcpResource.`
}
func (t *listTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "server_name": {"type": "string", "description": "Name of the MCP server to list resources from"}
  }
}`)
}
func (t *listTool) Permission() permissions.Level { return permissions.LevelRead }
func (t *listTool) IsConcurrencySafe() bool       { return true }

func (t *listTool) Execute(_ context.Context, raw json.RawMessage) (toolkit.Result, error) {
	var in struct {
		ServerName string `json:"server_name"`
	}
	_ = json.Unmarshal(raw, &in)

	name := in.ServerName
	if name == "" {
		name = t.ServerName
	}
	if name == "" {
		return toolkit.Result{Output: "No server_name specified. Provide a server_name to list its resources."}, nil
	}

	return toolkit.Result{Output: fmt.Sprintf(
		"MCP resource listing for server '%s': (MCP resource listing requires an active MCP connection)", name,
	)}, nil
}

// ---- ReadMcpResource ----

type readTool struct{}

func NewReadMcpResource() toolkit.Tool { return &readTool{} }

func (t *readTool) Name() string { return "ReadMcpResource" }
func (t *readTool) Description() string {
	return `Read a specific resource from an MCP server by URI.

Resources expose data from MCP servers. Use ListMcpResources first to discover available URIs.`
}
func (t *readTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "server_name": {"type": "string", "description": "Name of the MCP server"},
    "uri": {"type": "string", "description": "URI of the resource to read"}
  },
  "required": ["server_name", "uri"]
}`)
}
func (t *readTool) Permission() permissions.Level { return permissions.LevelRead }
func (t *readTool) IsConcurrencySafe() bool       { return true }

func (t *readTool) Execute(_ context.Context, raw json.RawMessage) (toolkit.Result, error) {
	var in struct {
		ServerName string `json:"server_name"`
		URI        string `json:"uri"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return toolkit.Result{}, fmt.Errorf("ReadMcpResource: %w", err)
	}
	return toolkit.Result{Output: fmt.Sprintf(
		"Read MCP resource '%s' from server '%s': (MCP resource reading requires an active MCP connection)",
		in.URI, in.ServerName,
	)}, nil
}
