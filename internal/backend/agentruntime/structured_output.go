package agentruntime

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strings"
)

type StructuredSchema struct {
	Name    string
	Version string
	Schema  map[string]any
}

type StructuredValidationError struct {
	Field      string `json:"field"`
	Expected   string `json:"expected,omitempty"`
	Actual     string `json:"actual,omitempty"`
	Message    string `json:"message"`
	Repairable bool   `json:"repairable"`
}

func (e StructuredValidationError) Error() string {
	if e.Field == "" {
		return e.Message
	}
	return e.Field + ": " + e.Message
}

type StructuredValidationResult struct {
	Value  any
	Errors []StructuredValidationError
}

func (r StructuredValidationResult) Valid() bool {
	return len(r.Errors) == 0
}

func (r StructuredValidationResult) Error() error {
	if r.Valid() {
		return nil
	}
	parts := make([]string, 0, len(r.Errors))
	for _, item := range r.Errors {
		parts = append(parts, item.Error())
	}
	return fmt.Errorf("structured output validation failed: %s", strings.Join(parts, "; "))
}

func ValidateStructuredJSON(raw []byte, schema StructuredSchema) StructuredValidationResult {
	var value any
	decoder := json.NewDecoder(strings.NewReader(strings.TrimSpace(string(raw))))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return StructuredValidationResult{Errors: []StructuredValidationError{{
			Field:      "$",
			Expected:   "valid JSON",
			Actual:     "invalid JSON",
			Message:    err.Error(),
			Repairable: true,
		}}}
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return StructuredValidationResult{Value: value, Errors: []StructuredValidationError{{
			Field:      "$",
			Expected:   "single JSON value",
			Actual:     "multiple JSON values",
			Message:    "output must contain exactly one JSON value",
			Repairable: true,
		}}}
	}
	return StructuredValidationResult{Value: value, Errors: validateStructuredValue(value, schema.Schema, "$")}
}

func ExtractAndValidateStructuredObject(output string, schema StructuredSchema) StructuredValidationResult {
	output = strings.TrimSpace(output)
	if output == "" {
		return StructuredValidationResult{Errors: []StructuredValidationError{{
			Field:      "$",
			Expected:   "JSON object",
			Actual:     "empty output",
			Message:    "output is empty",
			Repairable: true,
		}}}
	}
	if extracted := extractFirstJSONValue(output); extracted != "" {
		output = extracted
	}
	return ValidateStructuredJSON([]byte(output), schema)
}

func validateStructuredValue(value any, schema map[string]any, path string) []StructuredValidationError {
	if schema == nil {
		return nil
	}
	var errors []StructuredValidationError
	if enumValues, ok := schema["enum"]; ok {
		if !structuredValueInEnum(value, enumValues) {
			errors = append(errors, StructuredValidationError{
				Field:      path,
				Expected:   "one of " + structuredEnumDescription(enumValues),
				Actual:     structuredTypeName(value),
				Message:    "value is not in enum",
				Repairable: true,
			})
			return errors
		}
	}

	expectedType := structuredSchemaType(schema["type"])
	if expectedType != "" && !structuredTypeMatches(value, expectedType) {
		errors = append(errors, StructuredValidationError{
			Field:      path,
			Expected:   expectedType,
			Actual:     structuredTypeName(value),
			Message:    "invalid type",
			Repairable: true,
		})
		return errors
	}

	switch strings.ToLower(expectedType) {
	case "object":
		object, _ := value.(map[string]any)
		errors = append(errors, validateStructuredObject(object, schema, path)...)
	case "array":
		items, _ := value.([]any)
		if itemSchema, ok := structuredMap(schema["items"]); ok {
			for idx, item := range items {
				errors = append(errors, validateStructuredValue(item, itemSchema, fmt.Sprintf("%s[%d]", path, idx))...)
			}
		}
	default:
		if expectedType == "" {
			if object, ok := value.(map[string]any); ok {
				errors = append(errors, validateStructuredObject(object, schema, path)...)
			}
		}
	}
	return errors
}

func validateStructuredObject(object map[string]any, schema map[string]any, path string) []StructuredValidationError {
	var errors []StructuredValidationError
	for _, field := range structuredStringSlice(schema["required"]) {
		if _, ok := object[field]; !ok {
			errors = append(errors, StructuredValidationError{
				Field:      path + "." + field,
				Expected:   "required field",
				Actual:     "missing",
				Message:    "required field is missing",
				Repairable: true,
			})
		}
	}
	properties, _ := structuredMap(schema["properties"])
	for key, fieldSchema := range properties {
		childSchema, ok := structuredMap(fieldSchema)
		if !ok {
			continue
		}
		value, exists := object[key]
		if !exists {
			continue
		}
		errors = append(errors, validateStructuredValue(value, childSchema, path+"."+key)...)
	}
	if additional, ok := schema["additionalProperties"].(bool); ok && !additional {
		for key := range object {
			if _, ok := properties[key]; !ok {
				errors = append(errors, StructuredValidationError{
					Field:      path + "." + key,
					Expected:   "declared property",
					Actual:     "additional property",
					Message:    "additional property is not allowed",
					Repairable: true,
				})
			}
		}
	}
	return errors
}

func structuredSchemaType(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.ToLower(strings.TrimSpace(typed))
	case []any:
		if len(typed) > 0 {
			if text, ok := typed[0].(string); ok {
				return strings.ToLower(strings.TrimSpace(text))
			}
		}
	case []string:
		if len(typed) > 0 {
			return strings.ToLower(strings.TrimSpace(typed[0]))
		}
	}
	return ""
}

func structuredTypeMatches(value any, expected string) bool {
	switch strings.ToLower(expected) {
	case "object":
		_, ok := value.(map[string]any)
		return ok
	case "array":
		_, ok := value.([]any)
		return ok
	case "string":
		_, ok := value.(string)
		return ok
	case "number":
		switch value.(type) {
		case float64, int, int64, json.Number:
			return true
		default:
			return false
		}
	case "integer":
		return structuredIsInteger(value)
	case "boolean", "bool":
		_, ok := value.(bool)
		return ok
	case "":
		return true
	default:
		return true
	}
}

func structuredIsInteger(value any) bool {
	switch typed := value.(type) {
	case int, int64:
		return true
	case float64:
		return math.Trunc(typed) == typed
	case json.Number:
		_, err := typed.Int64()
		return err == nil
	default:
		return false
	}
}

func structuredTypeName(value any) string {
	switch value.(type) {
	case map[string]any:
		return "object"
	case []any:
		return "array"
	case string:
		return "string"
	case bool:
		return "boolean"
	case float64, int, int64, json.Number:
		return "number"
	case nil:
		return "null"
	default:
		return fmt.Sprintf("%T", value)
	}
}

func structuredValueInEnum(value any, enumValues any) bool {
	for _, candidate := range structuredAnySlice(enumValues) {
		if structuredEnumValueEqual(value, candidate) {
			return true
		}
	}
	return false
}

func structuredEnumValueEqual(left, right any) bool {
	if ls, ok := left.(string); ok {
		if rs, ok := right.(string); ok {
			return ls == rs
		}
	}
	if lb, ok := left.(bool); ok {
		if rb, ok := right.(bool); ok {
			return lb == rb
		}
	}
	ln, lok := structuredNumberString(left)
	rn, rok := structuredNumberString(right)
	return lok && rok && ln == rn
}

func structuredNumberString(value any) (string, bool) {
	switch typed := value.(type) {
	case json.Number:
		return typed.String(), true
	case float64:
		return fmt.Sprintf("%g", typed), true
	case int:
		return fmt.Sprintf("%d", typed), true
	case int64:
		return fmt.Sprintf("%d", typed), true
	default:
		return "", false
	}
}

func structuredEnumDescription(values any) string {
	parts := make([]string, 0)
	for _, value := range structuredAnySlice(values) {
		parts = append(parts, fmt.Sprint(value))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func structuredMap(value any) (map[string]any, bool) {
	switch typed := value.(type) {
	case map[string]any:
		return typed, true
	default:
		return nil, false
	}
}

func structuredStringSlice(value any) []string {
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

func structuredAnySlice(value any) []any {
	switch typed := value.(type) {
	case []any:
		return typed
	case []string:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, item)
		}
		return out
	default:
		return nil
	}
}
