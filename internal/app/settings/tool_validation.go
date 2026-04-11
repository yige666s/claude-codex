package settings

import "strings"

type ToolValidationResult struct {
	Valid      bool
	Error      string
	Suggestion string
	Examples   []string
}

type ToolValidationConfig struct {
	FilePatternTools map[string]bool
	BashPrefixTools  map[string]bool
	CustomValidation map[string]func(string) ToolValidationResult
}

func DefaultToolValidationConfig() ToolValidationConfig {
	return ToolValidationConfig{
		FilePatternTools: map[string]bool{
			"Read":         true,
			"Write":        true,
			"Edit":         true,
			"Glob":         true,
			"NotebookRead": true,
			"NotebookEdit": true,
		},
		BashPrefixTools: map[string]bool{
			"Bash": true,
		},
		CustomValidation: map[string]func(string) ToolValidationResult{
			"WebSearch": func(content string) ToolValidationResult {
				if strings.ContainsAny(content, "*?") {
					return ToolValidationResult{
						Valid:      false,
						Error:      "WebSearch does not support wildcards",
						Suggestion: "Use exact search terms without * or ?",
						Examples:   []string{"WebSearch(claude ai)", "WebSearch(typescript tutorial)"},
					}
				}
				return ToolValidationResult{Valid: true}
			},
			"WebFetch": func(content string) ToolValidationResult {
				if strings.Contains(content, "://") || strings.HasPrefix(strings.ToLower(content), "http") {
					return ToolValidationResult{
						Valid:      false,
						Error:      "WebFetch permissions use domain format, not URLs",
						Suggestion: "Use domain:hostname format",
						Examples:   []string{"WebFetch(domain:example.com)", "WebFetch(domain:github.com)"},
					}
				}
				if !strings.HasPrefix(content, "domain:") {
					return ToolValidationResult{
						Valid:      false,
						Error:      "WebFetch permissions must use domain: prefix",
						Suggestion: "Use domain:hostname format",
						Examples:   []string{"WebFetch(domain:example.com)", "WebFetch(domain:*.google.com)"},
					}
				}
				return ToolValidationResult{Valid: true}
			},
		},
	}
}

func IsFilePatternTool(toolName string) bool {
	return DefaultToolValidationConfig().FilePatternTools[toolName]
}

func IsBashPrefixTool(toolName string) bool {
	return DefaultToolValidationConfig().BashPrefixTools[toolName]
}

func CustomToolValidation(toolName, content string) ToolValidationResult {
	validator := DefaultToolValidationConfig().CustomValidation[toolName]
	if validator == nil {
		return ToolValidationResult{Valid: true}
	}
	return validator(content)
}
