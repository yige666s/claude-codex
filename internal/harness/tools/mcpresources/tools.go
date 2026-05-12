// Package mcpresources implements ListMcpResources and ReadMcpResource tools.
package mcpresources

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	mcpcore "claude-codex/internal/harness/mcp"
	"claude-codex/internal/harness/permissions"
	toolkit "claude-codex/internal/harness/tools"
)

// ---- ListMcpResources ----

type listTool struct {
	ServerName string
}

func NewListMcpResources(serverName string) toolkit.Tool { return &listTool{ServerName: serverName} }

func (t *listTool) Name() string { return "ListMcpResourcesTool" }
func (t *listTool) Description() string {
	return `List available resources from an MCP server.

Resources are data objects exposed by MCP servers (files, database records, etc.).
Use this to discover what resources are available before reading them with ReadMcpResource.`
}
func (t *listTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "server": {"type": "string", "description": "Name of the MCP server to list resources from"},
    "server_name": {"type": "string", "description": "Legacy alias for server"}
  }
}`)
}
func (t *listTool) Permission() permissions.Level { return permissions.LevelRead }
func (t *listTool) IsConcurrencySafe() bool       { return true }

func (t *listTool) Execute(_ context.Context, raw json.RawMessage) (toolkit.Result, error) {
	var in struct {
		Server     string `json:"server"`
		ServerName string `json:"server_name"`
	}
	_ = json.Unmarshal(raw, &in)

	name := in.Server
	if name == "" {
		name = in.ServerName
	}
	if name == "" {
		name = t.ServerName
	}
	if name == "" {
		return listAllResources()
	}
	client, ok := mcpcore.GetActiveClient(name)
	if !ok {
		return toolkit.Result{Output: fmt.Sprintf("MCP server '%s' is not connected.", name)}, nil
	}
	resources, err := client.ListResources(context.Background())
	if err != nil {
		return toolkit.Result{}, err
	}
	if len(resources) == 0 {
		return toolkit.Result{Output: fmt.Sprintf("No MCP resources available for server '%s'.", name)}, nil
	}
	data, err := json.MarshalIndent(resources, "", "  ")
	if err != nil {
		return toolkit.Result{}, err
	}
	return toolkit.Result{Output: string(data)}, nil
}

// ---- ReadMcpResource ----

type readTool struct{}

func NewReadMcpResource() toolkit.Tool { return &readTool{} }

func (t *readTool) Name() string { return "ReadMcpResourceTool" }
func (t *readTool) Description() string {
	return `Read a specific resource from an MCP server by URI.

Resources expose data from MCP servers. Use ListMcpResources first to discover available URIs.`
}
func (t *readTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "server": {"type": "string", "description": "Name of the MCP server"},
    "server_name": {"type": "string", "description": "Legacy alias for server"},
    "uri": {"type": "string", "description": "URI of the resource to read"}
  },
  "required": ["server", "uri"]
}`)
}
func (t *readTool) Permission() permissions.Level { return permissions.LevelRead }
func (t *readTool) IsConcurrencySafe() bool       { return true }

func (t *readTool) Execute(_ context.Context, raw json.RawMessage) (toolkit.Result, error) {
	var in struct {
		Server     string `json:"server"`
		ServerName string `json:"server_name"`
		URI        string `json:"uri"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return toolkit.Result{}, fmt.Errorf("ReadMcpResource: %w", err)
	}
	name := in.Server
	if name == "" {
		name = in.ServerName
	}
	client, ok := mcpcore.GetActiveClient(name)
	if !ok {
		return toolkit.Result{Output: fmt.Sprintf("MCP server '%s' is not connected.", name)}, nil
	}
	contents, err := client.ReadResource(context.Background(), in.URI)
	if err != nil {
		return toolkit.Result{}, err
	}
	data, err := json.MarshalIndent(contents, "", "  ")
	if err != nil {
		return toolkit.Result{}, err
	}
	return toolkit.Result{Output: string(data)}, nil
}

func listAllResources() (toolkit.Result, error) {
	clients := mcpcore.ListActiveClients()
	if len(clients) == 0 {
		return toolkit.Result{Output: "No MCP servers are connected."}, nil
	}
	names := make([]string, 0, len(clients))
	for name := range clients {
		names = append(names, name)
	}
	sort.Strings(names)

	var resources []map[string]any
	for _, name := range names {
		definitions, err := clients[name].ListResources(context.Background())
		if err != nil {
			return toolkit.Result{}, err
		}
		for _, definition := range definitions {
			encoded, err := json.Marshal(definition)
			if err != nil {
				return toolkit.Result{}, err
			}
			var object map[string]any
			if err := json.Unmarshal(encoded, &object); err != nil {
				return toolkit.Result{}, err
			}
			object["server"] = name
			resources = append(resources, object)
		}
	}
	if len(resources) == 0 {
		return toolkit.Result{Output: "No MCP resources available from connected servers."}, nil
	}
	data, err := json.MarshalIndent(resources, "", "  ")
	if err != nil {
		return toolkit.Result{}, err
	}
	return toolkit.Result{Output: string(data)}, nil
}
