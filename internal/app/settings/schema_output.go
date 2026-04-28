package settings

import (
	"encoding/json"
)

func GenerateSettingsJSONSchema() string {
	schema := map[string]any{
		"$schema":              "https://json-schema.org/draft/2020-12/schema",
		"$id":                  SettingsSchemaURL,
		"title":                "Claude Code Settings",
		"type":                 "object",
		"additionalProperties": false,
		"properties":           settingsSchemaProperties(),
	}
	data, _ := json.MarshalIndent(schema, "", "  ")
	return string(data)
}
