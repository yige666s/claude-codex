package query

import (
	"context"
	"encoding/json"
	"fmt"

	htool "claude-codex/internal/harness/tool"
	toolkit "claude-codex/internal/harness/tools"
)

type configuredTool struct {
	*htool.BaseTool
	name    string
	desc    string
	execute func(ctx context.Context, name string, input json.RawMessage) (string, error)
}

func newConfiguredToolFromDescriptor(desc toolkit.Descriptor, execute func(ctx context.Context, name string, input json.RawMessage) (string, error)) htool.Tool {
	builder := htool.NewToolBuilder(desc.Name)
	if len(desc.InputSchema) > 0 {
		var schema htool.ToolInputJSONSchema
		if err := json.Unmarshal(desc.InputSchema, &schema); err == nil {
			builder = builder.WithInputSchema(&schema)
		}
	}
	return &configuredTool{
		BaseTool: builder.Build(),
		name:     desc.Name,
		desc:     desc.Description,
		execute:  execute,
	}
}

func (t *configuredTool) Description(map[string]interface{}, htool.DescriptionOptions) (string, error) {
	if t.desc != "" {
		return t.desc, nil
	}
	return t.name, nil
}

func (t *configuredTool) Call(ctx context.Context, args map[string]interface{}, toolCtx *htool.ToolUseContext) (*htool.ToolResult, error) {
	if t.execute == nil {
		return nil, fmt.Errorf("tool executor not configured for %s", t.name)
	}
	input, err := json.Marshal(args)
	if err != nil {
		return nil, err
	}
	output, err := t.execute(ctx, t.name, input)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", t.name, err)
	}
	return &htool.ToolResult{Data: output}, nil
}

func configuredToolsFromDescriptors(descs []toolkit.Descriptor, execute func(ctx context.Context, name string, input json.RawMessage) (string, error)) []htool.Tool {
	out := make([]htool.Tool, 0, len(descs))
	for _, desc := range descs {
		out = append(out, newConfiguredToolFromDescriptor(desc, execute))
	}
	return out
}
