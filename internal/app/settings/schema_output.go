package settings

import (
	"encoding/json"
)

func GenerateSettingsJSONSchema() string {
	schema := map[string]any{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"title":   "Claude Code Settings",
		"type":    "object",
		"properties": map[string]any{
			"permissions": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"allow":       map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"deny":        map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"ask":         map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"defaultMode": map[string]any{"type": "string"},
				},
			},
			"hooks": map[string]any{"type": "object"},
			"mcpServers": map[string]any{
				"type": "object",
			},
			"env":                           map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}},
			"marketplaces":                  map[string]any{"type": "object"},
			"sandbox":                       map[string]any{"type": "object"},
			"keybindings":                   map[string]any{"type": "array"},
			"allowedMcpServers":             map[string]any{"type": "array"},
			"deniedMcpServers":              map[string]any{"type": "array"},
			"model":                         map[string]any{"type": "string"},
			"verbose":                       map[string]any{"type": "boolean"},
			"fastMode":                      map[string]any{"type": "boolean"},
			"autoUpdates":                   map[string]any{"type": "boolean"},
			"telemetry":                     map[string]any{"type": "boolean"},
			"strictPluginOnlyCustomization": map[string]any{"anyOf": []any{map[string]any{"type": "boolean"}, map[string]any{"type": "array", "items": map[string]any{"type": "string", "enum": CustomizationSurfaces}}}},
		},
	}
	data, _ := json.MarshalIndent(schema, "", "  ")
	return string(data)
}
