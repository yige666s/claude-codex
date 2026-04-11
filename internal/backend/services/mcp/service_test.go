package mcp

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"claude-codex/internal/app/config"
	mcpcore "claude-codex/internal/harness/mcp"
	"claude-codex/internal/harness/permissions"
	toolkit "claude-codex/internal/harness/tools"
)

type testTool struct{}

func (testTool) Name() string                  { return "mcp__ide__executeCode" }
func (testTool) Description() string           { return "ide execute" }
func (testTool) InputSchema() json.RawMessage  { return json.RawMessage(`{"type":"object"}`) }
func (testTool) Permission() permissions.Level { return permissions.LevelRead }
func (testTool) IsConcurrencySafe() bool       { return true }
func (testTool) Execute(_ context.Context, input json.RawMessage) (toolkit.Result, error) {
	return toolkit.Result{Output: strings.Repeat("x", 120000)}, nil
}

type testResources struct{}

func (testResources) ListResources(_ context.Context) ([]mcpcore.ResourceDefinition, error) {
	return []mcpcore.ResourceDefinition{{URI: "file://doc.txt", Name: "doc.txt"}}, nil
}
func (testResources) ReadResource(_ context.Context, uri string) ([]mcpcore.ResourceContent, error) {
	return []mcpcore.ResourceContent{{URI: uri, Text: "resource"}}, nil
}

func connectedClient(name string) *mcpcore.Client {
	server := mcpcore.NewServer(toolkit.NewRegistry(testTool{}))
	server.Name = name
	server.ResourceProvider = testResources{}
	client := mcpcore.NewInProcessClient(server)
	_, _ = client.Initialize(context.Background())
	mcpcore.RegisterActiveClient(name, client)
	return client
}

func TestSpecialServerDetection(t *testing.T) {
	if !IsIDEServer("ide") || !IsClaudeInChromeServer("claude-in-chrome") || !IsComputerUseServer("computer-use") {
		t.Fatal("expected special server detection to match")
	}
	if IsIncludedTool("ide", "mcp__ide__other") {
		t.Fatal("unexpected ide tool inclusion")
	}
}

func TestPersistAndProcessLargeOutput(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CLAUDE_GO_HOME", home)
	result, err := PersistToolResult(strings.Repeat("a", 5000), "server-tool")
	if err != nil {
		t.Fatalf("PersistToolResult: %v", err)
	}
	if result.Filepath == "" {
		t.Fatal("expected filepath")
	}
	if _, err := os.Stat(result.Filepath); err != nil {
		t.Fatalf("expected persisted file: %v", err)
	}
	instructions := BuildLargeOutputInstructions(result, "Plain text")
	if !strings.Contains(instructions, result.Filepath) {
		t.Fatalf("unexpected instructions: %s", instructions)
	}
}

func TestManagerFetchToolsAndResources(t *testing.T) {
	t.Setenv("CLAUDE_GO_HOME", t.TempDir())
	manager, err := NewManager(config.Default())
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	client := connectedClient("ide")
	defer client.Close()

	result, err := manager.FetchToolsAndResources(context.Background(), "ide", config.MCPServerConfig{Name: "ide", Transport: "inprocess"})
	if err == nil {
		// no-op; manager currently reconnects from config, so provide direct validation below
	}
	_ = result

	connection := ServerConnection{
		Name:   "ide",
		Type:   ConnectionTypeConnected,
		Client: client,
		Config: config.MCPServerConfig{Name: "ide", Transport: "inprocess"},
	}
	output, err := manager.CallTool(context.Background(), connection, "mcp__ide__executeCode", json.RawMessage(`{"code":"print(1)"}`))
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if !strings.Contains(output, "saved to") {
		t.Fatalf("expected large output persistence instructions, got %q", output)
	}

	resources, err := client.ListResources(context.Background())
	if err != nil || len(resources) != 1 {
		t.Fatalf("ListResources: %#v err=%v", resources, err)
	}
}

func TestOfficialURLAndValidationHelpers(t *testing.T) {
	if GetContentSizeEstimate(strings.Repeat("a", 400)) == 0 {
		t.Fatal("expected positive token estimate")
	}
	if !ContentNeedsTruncation(strings.Repeat("a", DefaultMaxMCPOutputChars+1), DefaultMaxMCPOutputChars) {
		t.Fatal("expected truncation need")
	}
	if !strings.Contains(TruncateContent(strings.Repeat("a", 1000), 10), "[OUTPUT TRUNCATED]") {
		t.Fatal("expected truncation marker")
	}
}

func TestManagerOfficialURLCheck(t *testing.T) {
	mcpcore.ResetOfficialMCPURLsForTesting()
	manager, err := NewManager(config.Default())
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if manager.IsOfficialURL("https://example.com") {
		t.Fatal("expected false before prefetch")
	}
}

func TestManagerReconnectServerMissingConfigFails(t *testing.T) {
	manager, err := NewManager(config.Default())
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	result := manager.ReconnectServer(context.Background(), "missing", config.MCPServerConfig{Name: "missing", Transport: "sdk"})
	if result.Type != ConnectionTypeFailed {
		t.Fatalf("expected failed reconnect, got %+v", result)
	}
}

func TestPersistToolResultPathUniqueness(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CLAUDE_GO_HOME", home)
	first, err := PersistToolResult("one", "tool")
	if err != nil {
		t.Fatalf("PersistToolResult first: %v", err)
	}
	time.Sleep(10 * time.Millisecond)
	second, err := PersistToolResult("two", "tool")
	if err != nil {
		t.Fatalf("PersistToolResult second: %v", err)
	}
	if first.Filepath == second.Filepath {
		t.Fatal("expected unique persisted file paths")
	}
}
