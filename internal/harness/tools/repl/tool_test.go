package repl

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"claude-codex/internal/harness/permissions"
	toolkit "claude-codex/internal/harness/tools"
)

type stubTool struct {
	name   string
	output string
	seen   json.RawMessage
}

func (s *stubTool) Name() string                  { return s.name }
func (s *stubTool) Description() string           { return s.name + " description" }
func (s *stubTool) InputSchema() json.RawMessage  { return json.RawMessage(`{"type":"object"}`) }
func (s *stubTool) Permission() permissions.Level { return permissions.LevelRead }
func (s *stubTool) IsConcurrencySafe() bool       { return true }
func (s *stubTool) Execute(_ context.Context, raw json.RawMessage) (toolkit.Result, error) {
	s.seen = append(s.seen[:0], raw...)
	return toolkit.Result{Output: s.output}, nil
}

func TestModeEnabledFromEnv(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]*string
		want bool
	}{
		{
			name: "explicit disable wins",
			env: map[string]*string{
				"CLAUDE_CODE_REPL":       strPtr("false"),
				"CLAUDE_REPL_MODE":       strPtr("true"),
				"USER_TYPE":              strPtr("ant"),
				"CLAUDE_CODE_ENTRYPOINT": strPtr("cli"),
			},
			want: false,
		},
		{
			name: "explicit repl mode enables",
			env: map[string]*string{
				"CLAUDE_REPL_MODE": strPtr("true"),
			},
			want: true,
		},
		{
			name: "ant cli entrypoint enables",
			env: map[string]*string{
				"USER_TYPE":              strPtr("ant"),
				"CLAUDE_CODE_ENTRYPOINT": strPtr("cli"),
			},
			want: true,
		},
		{
			name: "default disabled",
			env:  map[string]*string{},
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			restoreEnv(t, "CLAUDE_CODE_REPL", "CLAUDE_REPL_MODE", "USER_TYPE", "CLAUDE_CODE_ENTRYPOINT")
			for key, value := range tc.env {
				if value == nil {
					_ = os.Unsetenv(key)
					continue
				}
				t.Setenv(key, *value)
			}
			if got := ModeEnabledFromEnv(); got != tc.want {
				t.Fatalf("ModeEnabledFromEnv() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestFilterToolsForModeHidesPrimitiveToolsWhenReplIsPresent(t *testing.T) {
	tools := []toolkit.Tool{
		&stubTool{name: "Read"},
		&stubTool{name: "Write"},
		&stubTool{name: "bash"},
		&stubTool{name: ToolName},
		&stubTool{name: "Config"},
	}

	filtered := FilterToolsForMode(tools)
	names := toolNames(filtered)

	if len(filtered) != 2 || !names[ToolName] || !names["Config"] {
		t.Fatalf("expected only REPL and Config after filtering, got %v", names)
	}
}

func TestFilterToolsForModeKeepsPrimitivesWhenReplIsAbsent(t *testing.T) {
	tools := []toolkit.Tool{
		&stubTool{name: "Read"},
		&stubTool{name: "bash"},
		&stubTool{name: "Config"},
	}

	filtered := FilterToolsForMode(tools)
	if len(filtered) != len(tools) {
		t.Fatalf("expected primitive tools to remain without REPL, got %v", toolNames(filtered))
	}
}

func TestToolExecutesPrimitiveCalls(t *testing.T) {
	read := &stubTool{name: "Read", output: "read ok"}
	tool := NewTool([]toolkit.Tool{
		read,
		&stubTool{name: "Config", output: "config ok"},
	})

	result, err := tool.Execute(context.Background(), json.RawMessage(`{"calls":[{"tool":"file_read","input":{"file_path":"README.md"}}]}`))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var response struct {
		Results []struct {
			Tool   string `json:"tool"`
			Output string `json:"output"`
			Error  string `json:"error,omitempty"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(result.Output), &response); err != nil {
		t.Fatalf("unmarshal output: %v\n%s", err, result.Output)
	}
	if len(response.Results) != 1 || response.Results[0].Tool != "Read" || response.Results[0].Output != "read ok" || response.Results[0].Error != "" {
		t.Fatalf("unexpected response: %+v", response)
	}
	if string(read.seen) != `{"file_path":"README.md"}` {
		t.Fatalf("primitive input = %s", read.seen)
	}
}

func TestToolRejectsNonPrimitiveCalls(t *testing.T) {
	tool := NewTool([]toolkit.Tool{
		&stubTool{name: "Read", output: "read ok"},
		&stubTool{name: "Config", output: "config ok"},
	})

	_, err := tool.Execute(context.Background(), json.RawMessage(`{"calls":[{"tool":"Config","input":{}}]}`))
	if err == nil || err.Error() != `tool "Config" is not allowed in REPL mode` {
		t.Fatalf("Execute() error = %v", err)
	}
}

func strPtr(value string) *string {
	return &value
}

func restoreEnv(t *testing.T, keys ...string) {
	t.Helper()
	for _, key := range keys {
		original, ok := os.LookupEnv(key)
		_ = os.Unsetenv(key)
		if ok {
			t.Cleanup(func() {
				_ = os.Setenv(key, original)
			})
		} else {
			t.Cleanup(func() {
				_ = os.Unsetenv(key)
			})
		}
	}
}

func toolNames(tools []toolkit.Tool) map[string]bool {
	names := make(map[string]bool, len(tools))
	for _, tool := range tools {
		names[tool.Name()] = true
	}
	return names
}
