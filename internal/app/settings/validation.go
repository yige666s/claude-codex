package settings

import (
	"encoding/json"
	"fmt"

	"claude-codex/internal/public/schemas"
)

type FileValidationResult struct {
	IsValid    bool
	Error      string
	FullSchema string
}

func ValidateSettingsFileContent(content string) FileValidationResult {
	return validateSettingsFileContent(content, true)
}

func validateSettingsFileContent(content string, strict bool) FileValidationResult {
	var raw any
	if err := json.Unmarshal([]byte(content), &raw); err != nil {
		return FileValidationResult{
			IsValid:    false,
			Error:      "Invalid JSON: " + err.Error(),
			FullSchema: GenerateSettingsJSONSchema(),
		}
	}

	filtered, warnings := FilterInvalidPermissionRules(raw, "settings")
	doc, ok := filtered.(map[string]any)
	if !ok {
		return FileValidationResult{
			IsValid:    false,
			Error:      "Settings validation failed:\n- root: Expected object",
			FullSchema: GenerateSettingsJSONSchema(),
		}
	}

	settingsErrs := ValidateSettingsDocument(Document(doc), strict)
	payload, err := json.Marshal(filtered)
	if err != nil {
		return FileValidationResult{
			IsValid:    false,
			Error:      err.Error(),
			FullSchema: GenerateSettingsJSONSchema(),
		}
	}

	result := schemas.ValidateSettingsJSON(payload)
	if result.Valid && len(settingsErrs) == 0 {
		return FileValidationResult{IsValid: true}
	}

	errMsg := "Settings validation failed:"
	for _, warning := range warnings {
		errMsg += "\n- " + warning.Path + ": " + warning.Message
	}
	for _, validationErr := range result.Errors {
		errMsg += "\n- " + validationErr.Path + ": " + validationErr.Message
	}
	for _, validationErr := range settingsErrs {
		errMsg += "\n- " + validationErr.Path + ": " + validationErr.Message
	}
	return FileValidationResult{
		IsValid:    false,
		Error:      errMsg,
		FullSchema: GenerateSettingsJSONSchema(),
	}
}

func FilterInvalidPermissionRules(data any, filePath string) (any, []ValidationError) {
	root, ok := data.(map[string]any)
	if !ok {
		return data, nil
	}
	perms, ok := root["permissions"].(map[string]any)
	if !ok {
		return data, nil
	}

	var warnings []ValidationError
	for _, key := range []string{"allow", "deny", "ask"} {
		rules, ok := perms[key].([]any)
		if !ok {
			continue
		}
		filtered := make([]any, 0, len(rules))
		for _, rule := range rules {
			text, ok := rule.(string)
			if !ok {
				warnings = append(warnings, ValidationError{
					File:         filePath,
					Path:         "permissions." + key,
					Message:      "non-string permission rule was removed",
					InvalidValue: rule,
				})
				continue
			}
			if err := schemas.ValidatePermissionRule(text); err != nil {
				warnings = append(warnings, ValidationError{
					File:         filePath,
					Path:         "permissions." + key,
					Message:      fmt.Sprintf("invalid permission rule %q was skipped: %s", text, err.Error()),
					InvalidValue: text,
				})
				continue
			}
			filtered = append(filtered, text)
		}
		perms[key] = filtered
	}
	return root, warnings
}

func validateStrictPluginOnlyCustomization(data any) []ValidationError {
	root, ok := data.(map[string]any)
	if !ok {
		return nil
	}
	value, ok := root["strictPluginOnlyCustomization"]
	if !ok {
		return nil
	}

	switch v := value.(type) {
	case bool:
		return nil
	case []any:
		var errs []ValidationError
		allowed := make(map[string]bool, len(CustomizationSurfaces))
		for _, item := range CustomizationSurfaces {
			allowed[item] = true
		}
		for idx, item := range v {
			text, ok := item.(string)
			if !ok || !allowed[text] {
				errs = append(errs, ValidationError{
					Path:         fmt.Sprintf("strictPluginOnlyCustomization[%d]", idx),
					Message:      "must be one of skills, agents, hooks, mcp",
					InvalidValue: item,
				})
			}
		}
		return errs
	default:
		return []ValidationError{{
			Path:         "strictPluginOnlyCustomization",
			Message:      "must be a boolean or string array",
			InvalidValue: value,
		}}
	}
}
