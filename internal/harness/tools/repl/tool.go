package repl

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"claude-codex/internal/harness/permissions"
	toolkit "claude-codex/internal/harness/tools"
)

const ToolName = "REPL"

const maxCalls = 20

var primitiveAliases = map[string]string{
	"file_read":     "Read",
	"Read":          "Read",
	"FileRead":      "Read",
	"file_write":    "Write",
	"Write":         "Write",
	"FileWrite":     "Write",
	"file_edit":     "Edit",
	"Edit":          "Edit",
	"FileEdit":      "Edit",
	"glob":          "Glob",
	"Glob":          "Glob",
	"grep":          "Grep",
	"Grep":          "Grep",
	"bash":          "Bash",
	"Bash":          "Bash",
	"notebook_edit": "NotebookEdit",
	"NotebookEdit":  "NotebookEdit",
	"agent":         "Agent",
	"Agent":         "Agent",
}

type Tool struct {
	primitives map[string]toolkit.Tool
}

type input struct {
	Calls []call `json:"calls"`
}

type call struct {
	Tool  string          `json:"tool"`
	Input json.RawMessage `json:"input"`
}

type callResult struct {
	Tool   string `json:"tool"`
	Output string `json:"output,omitempty"`
	Error  string `json:"error,omitempty"`
}

type output struct {
	Results []callResult `json:"results"`
}

func NewTool(tools []toolkit.Tool) *Tool {
	primitives := make(map[string]toolkit.Tool)
	for _, tool := range tools {
		name := tool.Name()
		canonical, ok := primitiveAliases[name]
		if !ok || canonical != name {
			continue
		}
		primitives[name] = tool
	}
	return &Tool{primitives: primitives}
}

func (t *Tool) Name() string {
	return ToolName
}

func (t *Tool) Description() string {
	return "Run a bounded batch of primitive tool calls through the REPL tool surface."
}

func (t *Tool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"calls":{"type":"array","items":{"type":"object","properties":{"tool":{"type":"string"},"input":{"type":"object"}},"required":["tool","input"]},"minItems":1,"maxItems":20}},"required":["calls"]}`)
}

func (t *Tool) Permission() permissions.Level {
	return permissions.LevelExecute
}

func (t *Tool) IsConcurrencySafe() bool {
	return false
}

func (t *Tool) Execute(ctx context.Context, raw json.RawMessage) (toolkit.Result, error) {
	var in input
	if err := json.Unmarshal(raw, &in); err != nil {
		return toolkit.Result{}, err
	}
	if len(in.Calls) == 0 {
		return toolkit.Result{}, fmt.Errorf("calls is required")
	}
	if len(in.Calls) > maxCalls {
		return toolkit.Result{}, fmt.Errorf("calls exceeds maximum of %d", maxCalls)
	}

	out := output{Results: make([]callResult, 0, len(in.Calls))}
	for _, requested := range in.Calls {
		canonical, ok := primitiveAliases[requested.Tool]
		if !ok {
			return toolkit.Result{}, fmt.Errorf("tool %q is not allowed in REPL mode", requested.Tool)
		}
		primitive, ok := t.primitives[canonical]
		if !ok {
			return toolkit.Result{}, fmt.Errorf("tool %q is not registered for REPL mode", canonical)
		}
		payload := requested.Input
		if len(payload) == 0 {
			payload = json.RawMessage(`{}`)
		}
		result, err := primitive.Execute(ctx, payload)
		entry := callResult{Tool: canonical, Output: result.Output}
		if err != nil {
			entry.Error = err.Error()
		}
		out.Results = append(out.Results, entry)
	}

	encoded, err := json.Marshal(out)
	if err != nil {
		return toolkit.Result{}, err
	}
	return toolkit.Result{Output: string(encoded)}, nil
}

func ModeEnabledFromEnv() bool {
	if value, ok := os.LookupEnv("CLAUDE_CODE_REPL"); ok && !envBool(value) {
		return false
	}
	if envBool(os.Getenv("CLAUDE_REPL_MODE")) {
		return true
	}
	return os.Getenv("USER_TYPE") == "ant" && os.Getenv("CLAUDE_CODE_ENTRYPOINT") == "cli"
}

func FilterToolsForMode(tools []toolkit.Tool) []toolkit.Tool {
	hasRepl := false
	for _, tool := range tools {
		if tool.Name() == ToolName {
			hasRepl = true
			break
		}
	}
	if !hasRepl {
		return tools
	}

	filtered := make([]toolkit.Tool, 0, len(tools))
	for _, tool := range tools {
		if tool.Name() != ToolName && IsPrimitiveToolName(tool.Name()) {
			continue
		}
		filtered = append(filtered, tool)
	}
	return filtered
}

func IsPrimitiveToolName(name string) bool {
	_, ok := primitiveAliases[name]
	return ok
}

func envBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "t", "yes", "y", "on":
		return true
	default:
		return false
	}
}
