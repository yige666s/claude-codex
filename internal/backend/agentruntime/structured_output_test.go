package agentruntime

import "testing"

func TestExtractAndValidateStructuredObjectAllowsJSONInMarkdown(t *testing.T) {
	schema := StructuredSchema{
		Name: "demo",
		Schema: map[string]any{
			"type":     "object",
			"required": []string{"action"},
			"properties": map[string]any{
				"action": map[string]any{"type": "string", "enum": []any{"ok"}},
			},
		},
	}
	result := ExtractAndValidateStructuredObject("```json\n{\"action\":\"ok\"}\n```", schema)
	if !result.Valid() {
		t.Fatalf("expected valid structured output, got %#v", result.Errors)
	}
}

func TestValidateStructuredJSONRejectsRequiredEnumAndTypeErrors(t *testing.T) {
	schema := StructuredSchema{
		Name: "demo",
		Schema: map[string]any{
			"type":                 "object",
			"required":             []string{"action", "count"},
			"additionalProperties": false,
			"properties": map[string]any{
				"action": map[string]any{"type": "string", "enum": []any{"ok"}},
				"count":  map[string]any{"type": "integer"},
			},
		},
	}
	result := ValidateStructuredJSON([]byte(`{"action":"bad","extra":true}`), schema)
	if result.Valid() {
		t.Fatal("expected invalid structured output")
	}
	if len(result.Errors) < 3 {
		t.Fatalf("expected enum, missing field, and additional property errors, got %#v", result.Errors)
	}
}

func TestStructuredOutputValidationTestSet(t *testing.T) {
	schema := StructuredSchema{
		Name: "tool_args",
		Schema: map[string]any{
			"type":                 "object",
			"required":             []string{"action", "query", "limit"},
			"additionalProperties": false,
			"properties": map[string]any{
				"action": map[string]any{"type": "string", "enum": []any{"search", "answer"}},
				"query":  map[string]any{"type": "string"},
				"limit":  map[string]any{"type": "integer"},
				"tags": map[string]any{
					"type":  "array",
					"items": map[string]any{"type": "string"},
				},
			},
		},
	}
	cases := []struct {
		name      string
		output    string
		wantValid bool
		strict    bool
	}{
		{name: "plain valid json", output: `{"action":"search","query":"postgres","limit":3,"tags":["db"]}`, wantValid: true},
		{name: "markdown fenced json", output: "```json\n{\"action\":\"search\",\"query\":\"postgres\",\"limit\":3}\n```", wantValid: true},
		{name: "non json", output: `please search postgres`, wantValid: false},
		{name: "multiple json values", output: `{"action":"search","query":"postgres","limit":3} {"action":"answer","query":"x","limit":1}`, wantValid: false, strict: true},
		{name: "missing required field", output: `{"action":"search","query":"postgres"}`, wantValid: false},
		{name: "enum drift", output: `{"action":"lookup","query":"postgres","limit":3}`, wantValid: false},
		{name: "type drift", output: `{"action":"search","query":"postgres","limit":"3"}`, wantValid: false},
		{name: "additional property", output: `{"action":"search","query":"postgres","limit":3,"unsafe":true}`, wantValid: false},
		{name: "array item type drift", output: `{"action":"search","query":"postgres","limit":3,"tags":["db",7]}`, wantValid: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var result StructuredValidationResult
			if tc.strict {
				result = ValidateStructuredJSON([]byte(tc.output), schema)
			} else {
				result = ExtractAndValidateStructuredObject(tc.output, schema)
			}
			if result.Valid() != tc.wantValid {
				t.Fatalf("Valid() = %v, want %v; errors=%#v", result.Valid(), tc.wantValid, result.Errors)
			}
		})
	}
}
