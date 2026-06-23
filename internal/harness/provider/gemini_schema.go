package provider

import "strings"

func geminiCompatibleToolSchema(schema map[string]interface{}) map[string]interface{} {
	if len(schema) == 0 {
		return map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		}
	}
	sanitized, _ := sanitizeGeminiSchema(schema, schema, map[string]bool{}).(map[string]interface{})
	if len(sanitized) == 0 {
		return map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		}
	}
	if _, ok := sanitized["type"]; !ok {
		if _, hasProperties := sanitized["properties"]; hasProperties {
			sanitized["type"] = "object"
		}
	}
	return sanitized
}

func sanitizeGeminiSchema(value interface{}, root map[string]interface{}, resolving map[string]bool) interface{} {
	switch typed := value.(type) {
	case map[string]interface{}:
		if ref, _ := typed["$ref"].(string); strings.HasPrefix(ref, "#/") && !resolving[ref] {
			if resolved, ok := resolveGeminiLocalRef(root, ref); ok {
				resolving[ref] = true
				merged := map[string]interface{}{}
				if resolvedMap, ok := resolved.(map[string]interface{}); ok {
					for k, v := range resolvedMap {
						merged[k] = v
					}
				}
				for k, v := range typed {
					if k != "$ref" {
						merged[k] = v
					}
				}
				out := sanitizeGeminiSchema(merged, root, resolving)
				delete(resolving, ref)
				return out
			}
		}
		out := make(map[string]interface{}, len(typed))
		for key, item := range typed {
			if !geminiSchemaKeyAllowed(key) {
				continue
			}
			if key == "type" {
				if normalized, nullable := normalizeGeminiSchemaType(item); normalized != nil {
					out[key] = normalized
					if nullable {
						out["nullable"] = true
					}
				}
				continue
			}
			if key == "properties" {
				props, ok := item.(map[string]interface{})
				if !ok {
					continue
				}
				cleanProps := map[string]interface{}{}
				for propName, propSchema := range props {
					if strings.TrimSpace(propName) == "" {
						continue
					}
					cleanProps[propName] = sanitizeGeminiSchema(propSchema, root, resolving)
				}
				out[key] = cleanProps
				continue
			}
			if key == "enum" {
				if cleanEnum := sanitizeGeminiStringEnum(item); len(cleanEnum) > 0 {
					out[key] = cleanEnum
				}
				continue
			}
			out[key] = sanitizeGeminiSchema(item, root, resolving)
		}
		return out
	case []interface{}:
		out := make([]interface{}, 0, len(typed))
		for _, item := range typed {
			out = append(out, sanitizeGeminiSchema(item, root, resolving))
		}
		return out
	default:
		return value
	}
}

func sanitizeGeminiStringEnum(value interface{}) []interface{} {
	values, ok := value.([]interface{})
	if !ok {
		return nil
	}
	out := make([]interface{}, 0, len(values))
	for _, item := range values {
		if _, ok := item.(string); ok {
			out = append(out, item)
		}
	}
	return out
}

func resolveGeminiLocalRef(root map[string]interface{}, ref string) (interface{}, bool) {
	parts := strings.Split(strings.TrimPrefix(ref, "#/"), "/")
	var current interface{} = root
	for _, part := range parts {
		part = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
		currentMap, ok := current.(map[string]interface{})
		if !ok {
			return nil, false
		}
		current, ok = currentMap[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

func geminiSchemaKeyAllowed(key string) bool {
	if strings.HasPrefix(key, "$") || strings.HasPrefix(strings.ToLower(key), "x-") {
		return false
	}
	switch key {
	case "type",
		"format",
		"description",
		"nullable",
		"enum",
		"items",
		"properties",
		"propertyOrdering",
		"required",
		"minItems",
		"maxItems",
		"minimum",
		"maximum",
		"minLength",
		"maxLength",
		"pattern",
		"anyOf":
		return true
	default:
		return false
	}
}

func normalizeGeminiSchemaType(value interface{}) (interface{}, bool) {
	switch typed := value.(type) {
	case []interface{}:
		nullable := false
		var firstNonNull string
		for _, item := range typed {
			if s, ok := item.(string); ok {
				if strings.EqualFold(s, "null") {
					nullable = true
					continue
				}
				if firstNonNull == "" {
					firstNonNull = s
				}
			}
		}
		if firstNonNull != "" {
			return firstNonNull, nullable
		}
		return nil, nullable
	default:
		return value, false
	}
}
