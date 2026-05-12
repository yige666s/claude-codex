package mcpresources

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	mcpcore "claude-codex/internal/harness/mcp"
)

type testResources struct{}

func (testResources) ListResources(context.Context) ([]mcpcore.ResourceDefinition, error) {
	return []mcpcore.ResourceDefinition{{URI: "file://notes.md", Name: "notes"}}, nil
}

func (testResources) ReadResource(_ context.Context, uri string) ([]mcpcore.ResourceContent, error) {
	return []mcpcore.ResourceContent{{URI: uri, MimeType: "text/plain", Text: "hello"}}, nil
}

func TestListMcpResourcesListsAllActiveServers(t *testing.T) {
	mcpcore.ClearActiveClients()
	defer mcpcore.ClearActiveClients()
	server := mcpcore.NewServer(nil)
	server.Name = "resource-server"
	server.ResourceProvider = testResources{}
	mcpcore.RegisterActiveClient("resource-server", mcpcore.NewInProcessClient(server))

	result, err := NewListMcpResources("").Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("list resources: %v", err)
	}
	if !strings.Contains(result.Output, `"server": "resource-server"`) || !strings.Contains(result.Output, "file://notes.md") {
		t.Fatalf("unexpected output: %q", result.Output)
	}
}

func TestReadMcpResourceAcceptsServerAlias(t *testing.T) {
	mcpcore.ClearActiveClients()
	defer mcpcore.ClearActiveClients()
	server := mcpcore.NewServer(nil)
	server.Name = "resource-server"
	server.ResourceProvider = testResources{}
	mcpcore.RegisterActiveClient("resource-server", mcpcore.NewInProcessClient(server))

	input := json.RawMessage(`{"server":"resource-server","uri":"file://notes.md"}`)
	result, err := NewReadMcpResource().Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("read resource: %v", err)
	}
	if !strings.Contains(result.Output, `"text": "hello"`) {
		t.Fatalf("unexpected output: %q", result.Output)
	}
}
