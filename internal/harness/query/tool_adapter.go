package query

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	htool "claude-codex/internal/harness/tool"
	toolkit "claude-codex/internal/harness/tools"
)

type configuredTool struct {
	*htool.BaseTool
	name    string
	desc    string
	schema  json.RawMessage
	execute func(ctx context.Context, toolUseID, name string, input json.RawMessage) (string, error)
}

func newConfiguredToolFromDescriptor(desc toolkit.Descriptor, execute func(ctx context.Context, toolUseID, name string, input json.RawMessage) (string, error)) htool.Tool {
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
		schema:   append(json.RawMessage(nil), desc.InputSchema...),
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
	if validation, err := t.ValidateInput(args, toolCtx); err != nil {
		return nil, err
	} else if !validation.Valid {
		return nil, fmt.Errorf("%s: %s", t.name, validation.Message)
	}
	input, err := json.Marshal(args)
	if err != nil {
		return nil, err
	}
	toolUseID := ""
	if toolCtx != nil {
		toolUseID = toolCtx.ToolUseID
	}
	output, err := t.execute(ctx, toolUseID, t.name, input)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", t.name, err)
	}
	return &htool.ToolResult{Data: output}, nil
}

func (t *configuredTool) ValidateInput(input map[string]interface{}, toolCtx *htool.ToolUseContext) (htool.ValidationResult, error) {
	if len(t.schema) == 0 {
		return htool.NewValidationSuccess(), nil
	}
	var schema map[string]any
	if err := json.Unmarshal(t.schema, &schema); err != nil {
		return htool.NewValidationError(fmt.Sprintf("invalid input schema: %v", err), 1001), nil
	}
	if err := validateConfiguredToolInput(input, schema, "$"); err != nil {
		return htool.NewValidationError(err.Error(), 1002), nil
	}
	return htool.NewValidationSuccess(), nil
}

func configuredToolsFromDescriptors(descs []toolkit.Descriptor, execute func(ctx context.Context, toolUseID, name string, input json.RawMessage) (string, error)) []htool.Tool {
	out := make([]htool.Tool, 0, len(descs))
	for _, desc := range descs {
		out = append(out, newConfiguredToolFromDescriptor(desc, execute))
	}
	return out
}

func validateConfiguredToolInput(input map[string]interface{}, schema map[string]any, path string) error {
	expectedType := configuredToolSchemaType(schema["type"])
	if expectedType == "object" {
		for _, field := range configuredToolStringSlice(schema["required"]) {
			if _, ok := input[field]; !ok {
				return fmt.Errorf("%s.%s is required", path, field)
			}
		}
		properties, _ := schema["properties"].(map[string]any)
		for key, fieldSchema := range properties {
			value, ok := input[key]
			if !ok {
				continue
			}
			childSchema, ok := fieldSchema.(map[string]any)
			if !ok {
				continue
			}
			if err := validateConfiguredToolValue(value, childSchema, path+"."+key); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateConfiguredToolValue(value any, schema map[string]any, path string) error {
	expectedType := configuredToolSchemaType(schema["type"])
	if expectedType != "" && !configuredToolTypeMatches(value, expectedType) {
		return fmt.Errorf("%s expected %s, got %T", path, expectedType, value)
	}
	if enumValues, ok := schema["enum"]; ok && !configuredToolValueInEnum(value, enumValues) {
		return fmt.Errorf("%s is not in enum", path)
	}
	if expectedType == "object" {
		child, ok := value.(map[string]interface{})
		if !ok {
			return fmt.Errorf("%s expected object, got %T", path, value)
		}
		return validateConfiguredToolInput(child, schema, path)
	}
	return nil
}

func configuredToolSchemaType(value any) string {
	if text, ok := value.(string); ok {
		return strings.ToLower(strings.TrimSpace(text))
	}
	return ""
}

func configuredToolTypeMatches(value any, expected string) bool {
	switch expected {
	case "object":
		_, ok := value.(map[string]interface{})
		return ok
	case "array":
		_, ok := value.([]interface{})
		return ok
	case "string":
		_, ok := value.(string)
		return ok
	case "number", "integer":
		switch value.(type) {
		case int, int64, float64, json.Number:
			return true
		default:
			return false
		}
	case "boolean", "bool":
		_, ok := value.(bool)
		return ok
	default:
		return true
	}
}

func configuredToolStringSlice(value any) []string {
	switch typed := value.(type) {
	case []string:
		return typed
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}

func configuredToolValueInEnum(value any, enumValues any) bool {
	values, ok := enumValues.([]any)
	if !ok {
		return true
	}
	for _, candidate := range values {
		if fmt.Sprint(value) == fmt.Sprint(candidate) {
			return true
		}
	}
	return false
}
