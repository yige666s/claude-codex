package tool

import (
	"context"
	"encoding/json"
	"testing"
)

func TestToolInputJSONSchema_MarshalJSON(t *testing.T) {
	schema := &ToolInputJSONSchema{
		Type: "object",
		Properties: map[string]interface{}{
			"name": map[string]interface{}{
				"type": "string",
			},
			"age": map[string]interface{}{
				"type": "number",
			},
		},
		Required: []string{"name"},
		Additional: map[string]interface{}{
			"additionalProperties": false,
		},
	}

	data, err := json.Marshal(schema)
	if err != nil {
		t.Fatalf("Failed to marshal schema: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	if result["type"] != "object" {
		t.Errorf("Expected type 'object', got %v", result["type"])
	}

	if result["additionalProperties"] != false {
		t.Errorf("Expected additionalProperties false, got %v", result["additionalProperties"])
	}
}

func TestToolInputJSONSchema_UnmarshalJSON(t *testing.T) {
	data := []byte(`{
		"type": "object",
		"properties": {
			"name": {"type": "string"},
			"age": {"type": "number"}
		},
		"required": ["name"],
		"additionalProperties": false
	}`)

	var schema ToolInputJSONSchema
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatalf("Failed to unmarshal schema: %v", err)
	}

	if schema.Type != "object" {
		t.Errorf("Expected type 'object', got %s", schema.Type)
	}

	if len(schema.Required) != 1 || schema.Required[0] != "name" {
		t.Errorf("Expected required ['name'], got %v", schema.Required)
	}

	if schema.Additional["additionalProperties"] != false {
		t.Errorf("Expected additionalProperties false, got %v", schema.Additional["additionalProperties"])
	}
}

func TestValidationResult(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		result := NewValidationSuccess()
		if !result.Valid {
			t.Error("Expected valid result")
		}
		if result.Message != "" {
			t.Errorf("Expected empty message, got %s", result.Message)
		}
	})

	t.Run("Error", func(t *testing.T) {
		result := NewValidationError("Invalid input", 400)
		if result.Valid {
			t.Error("Expected invalid result")
		}
		if result.Message != "Invalid input" {
			t.Errorf("Expected message 'Invalid input', got %s", result.Message)
		}
		if result.ErrorCode != 400 {
			t.Errorf("Expected error code 400, got %d", result.ErrorCode)
		}
	})
}

func TestToolMatchesName(t *testing.T) {
	tool := NewToolBuilder("test-tool").
		WithAliases("alias1", "alias2").
		Build()

	tests := []struct {
		name     string
		expected bool
	}{
		{"test-tool", true},
		{"alias1", true},
		{"alias2", true},
		{"other-tool", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ToolMatchesName(tool, tt.name)
			if result != tt.expected {
				t.Errorf("ToolMatchesName(%s) = %v, expected %v", tt.name, result, tt.expected)
			}
		})
	}
}

func TestFindToolByName(t *testing.T) {
	tool1 := NewToolBuilder("tool1").WithAliases("t1").Build()
	tool2 := NewToolBuilder("tool2").WithAliases("t2").Build()
	tool3 := NewToolBuilder("tool3").Build()

	tools := []Tool{tool1, tool2, tool3}

	tests := []struct {
		name     string
		expected Tool
	}{
		{"tool1", tool1},
		{"t1", tool1},
		{"tool2", tool2},
		{"t2", tool2},
		{"tool3", tool3},
		{"nonexistent", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FindToolByName(tools, tt.name)
			if result != tt.expected {
				t.Errorf("FindToolByName(%s) = %v, expected %v", tt.name, result, tt.expected)
			}
		})
	}
}

func TestToolBuilder(t *testing.T) {
	t.Run("Basic tool", func(t *testing.T) {
		tool := NewToolBuilder("test-tool").
			WithAliases("alias1", "alias2").
			WithSearchHint("test search hint").
			WithMaxResultSizeChars(50000).
			Build()

		if tool.Name() != "test-tool" {
			t.Errorf("Expected name 'test-tool', got %s", tool.Name())
		}

		aliases := tool.Aliases()
		if len(aliases) != 2 || aliases[0] != "alias1" || aliases[1] != "alias2" {
			t.Errorf("Expected aliases [alias1, alias2], got %v", aliases)
		}

		if tool.SearchHint() != "test search hint" {
			t.Errorf("Expected search hint 'test search hint', got %s", tool.SearchHint())
		}

		if tool.MaxResultSizeChars() != 50000 {
			t.Errorf("Expected max result size 50000, got %d", tool.MaxResultSizeChars())
		}
	})

	t.Run("MCP tool", func(t *testing.T) {
		tool := NewToolBuilder("mcp-tool").
			WithMCP("server1", "tool1").
			Build()

		if !tool.IsMCP() {
			t.Error("Expected MCP tool")
		}

		mcpInfo := tool.MCPInfo()
		if mcpInfo == nil {
			t.Fatal("Expected MCP info")
		}

		if mcpInfo.ServerName != "server1" {
			t.Errorf("Expected server name 'server1', got %s", mcpInfo.ServerName)
		}

		if mcpInfo.ToolName != "tool1" {
			t.Errorf("Expected tool name 'tool1', got %s", mcpInfo.ToolName)
		}
	})

	t.Run("LSP tool", func(t *testing.T) {
		tool := NewToolBuilder("lsp-tool").
			WithLSP().
			Build()

		if !tool.IsLSP() {
			t.Error("Expected LSP tool")
		}
	})

	t.Run("Deferred tool", func(t *testing.T) {
		tool := NewToolBuilder("deferred-tool").
			WithDefer().
			Build()

		if !tool.ShouldDefer() {
			t.Error("Expected deferred tool")
		}
	})

	t.Run("Always load tool", func(t *testing.T) {
		tool := NewToolBuilder("always-load-tool").
			WithAlwaysLoad().
			Build()

		if !tool.AlwaysLoad() {
			t.Error("Expected always load tool")
		}
	})

	t.Run("Strict tool", func(t *testing.T) {
		tool := NewToolBuilder("strict-tool").
			WithStrict().
			Build()

		if !tool.Strict() {
			t.Error("Expected strict tool")
		}
	})
}

func TestBaseToolDefaults(t *testing.T) {
	tool := NewToolBuilder("test-tool").Build()
	input := map[string]interface{}{"key": "value"}

	if !tool.IsEnabled() {
		t.Error("Expected tool to be enabled by default")
	}

	if tool.IsConcurrencySafe(input) {
		t.Error("Expected tool to not be concurrency safe by default")
	}

	if tool.IsReadOnly(input) {
		t.Error("Expected tool to not be read-only by default")
	}

	if tool.IsDestructive(input) {
		t.Error("Expected tool to not be destructive by default")
	}

	if tool.IsOpenWorld(input) {
		t.Error("Expected tool to not be open world by default")
	}

	if tool.RequiresUserInteraction() {
		t.Error("Expected tool to not require user interaction by default")
	}

	if tool.InterruptBehavior() != InterruptBlock {
		t.Errorf("Expected interrupt behavior 'block', got %s", tool.InterruptBehavior())
	}

	searchInfo := tool.IsSearchOrReadCommand(input)
	if searchInfo.IsSearch || searchInfo.IsRead || searchInfo.IsList {
		t.Error("Expected tool to not be search/read/list by default")
	}

	if tool.UserFacingName(input) != "test-tool" {
		t.Errorf("Expected user facing name 'test-tool', got %s", tool.UserFacingName(input))
	}

	if tool.GetActivityDescription(input) != "" {
		t.Errorf("Expected empty activity description, got %s", tool.GetActivityDescription(input))
	}

	if tool.ToAutoClassifierInput(input) != "" {
		t.Errorf("Expected empty classifier input, got %v", tool.ToAutoClassifierInput(input))
	}

	if tool.GetPath(input) != "" {
		t.Errorf("Expected empty path, got %s", tool.GetPath(input))
	}

	if tool.InputsEquivalent(input, input) {
		t.Error("Expected inputs to not be equivalent by default")
	}

	if tool.IsTransparentWrapper() {
		t.Error("Expected tool to not be transparent wrapper by default")
	}

	if tool.IsMCP() {
		t.Error("Expected tool to not be MCP by default")
	}

	if tool.IsLSP() {
		t.Error("Expected tool to not be LSP by default")
	}

	if tool.ShouldDefer() {
		t.Error("Expected tool to not be deferred by default")
	}

	if tool.AlwaysLoad() {
		t.Error("Expected tool to not always load by default")
	}

	if tool.Strict() {
		t.Error("Expected tool to not be strict by default")
	}
}

func TestBaseToolValidateInput(t *testing.T) {
	tool := NewToolBuilder("test-tool").Build()
	ctx := NewToolUseContext(context.Background())
	input := map[string]interface{}{"key": "value"}

	result, err := tool.ValidateInput(input, ctx)
	if err != nil {
		t.Fatalf("ValidateInput failed: %v", err)
	}

	if !result.Valid {
		t.Error("Expected validation to succeed by default")
	}
}

func TestBaseToolCheckPermissions(t *testing.T) {
	tool := NewToolBuilder("test-tool").Build()
	ctx := NewToolUseContext(context.Background())
	input := map[string]interface{}{"key": "value"}

	result, err := tool.CheckPermissions(input, ctx)
	if err != nil {
		t.Fatalf("CheckPermissions failed: %v", err)
	}

	if result.Behavior != PermissionAllow {
		t.Errorf("Expected permission behavior 'allow', got %s", result.Behavior)
	}

	if result.UpdatedInput == nil {
		t.Error("Expected updated input to be set")
	}
}

func TestToolResult(t *testing.T) {
	result := &ToolResult{
		Data: "test data",
		NewMessages: []interface{}{
			map[string]interface{}{"type": "user", "content": "test"},
		},
		MCPMeta: &MCPMeta{
			Meta: map[string]interface{}{"key": "value"},
		},
	}

	if result.Data != "test data" {
		t.Errorf("Expected data 'test data', got %v", result.Data)
	}

	if len(result.NewMessages) != 1 {
		t.Errorf("Expected 1 new message, got %d", len(result.NewMessages))
	}

	if result.MCPMeta == nil {
		t.Fatal("Expected MCP meta")
	}

	if result.MCPMeta.Meta["key"] != "value" {
		t.Errorf("Expected meta key 'value', got %v", result.MCPMeta.Meta["key"])
	}
}

func TestPermissionResult(t *testing.T) {
	t.Run("Allow", func(t *testing.T) {
		result := &PermissionResult{
			Behavior:     PermissionAllow,
			UpdatedInput: map[string]interface{}{"key": "value"},
		}

		if result.Behavior != PermissionAllow {
			t.Errorf("Expected behavior 'allow', got %s", result.Behavior)
		}
	})

	t.Run("Deny", func(t *testing.T) {
		result := &PermissionResult{
			Behavior: PermissionDeny,
			Reason:   "Access denied",
		}

		if result.Behavior != PermissionDeny {
			t.Errorf("Expected behavior 'deny', got %s", result.Behavior)
		}

		if result.Reason != "Access denied" {
			t.Errorf("Expected reason 'Access denied', got %s", result.Reason)
		}
	})

	t.Run("Ask", func(t *testing.T) {
		result := &PermissionResult{
			Behavior: PermissionAsk,
		}

		if result.Behavior != PermissionAsk {
			t.Errorf("Expected behavior 'ask', got %s", result.Behavior)
		}
	})
}
