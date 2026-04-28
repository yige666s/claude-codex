package permissions

import (
	"sort"
	"strings"
)

// Legacy tool name aliases (TS normalizeLegacyToolName).
var legacyToolNameAliases = map[string]string{
	"Task":            "AgentTool",
	"KillShell":       "TaskStopTool",
	"AgentOutputTool": "TaskOutputTool",
	"BashOutputTool":  "TaskOutputTool",
}

// NormalizeLegacyToolName returns the canonical tool name for legacy aliases.
func NormalizeLegacyToolName(name string) string {
	if canonical, ok := legacyToolNameAliases[name]; ok {
		return canonical
	}
	return name
}

// GetLegacyToolNames returns legacy aliases for a canonical tool name.
func GetLegacyToolNames(canonicalName string) []string {
	var legacy []string
	for alias, canonical := range legacyToolNameAliases {
		if canonical == canonicalName {
			legacy = append(legacy, alias)
		}
	}
	sort.Strings(legacy)
	return legacy
}

// EscapeRuleContent escapes content for use inside "Tool(content)" rule strings.
// Order: \ → \\ then ( → \( then ) → \)
func EscapeRuleContent(content string) string {
	s := strings.ReplaceAll(content, `\`, `\\`)
	s = strings.ReplaceAll(s, "(", `\(`)
	s = strings.ReplaceAll(s, ")", `\)`)
	return s
}

// UnescapeRuleContent unescapes content extracted from a "Tool(content)" rule string.
// Order: \( → (  then \) → )  then \\ → \
func UnescapeRuleContent(content string) string {
	s := strings.ReplaceAll(content, `\\(`, "(")
	s = strings.ReplaceAll(s, `\\)`, ")")
	s = strings.ReplaceAll(s, `\(`, "(")
	s = strings.ReplaceAll(s, `\)`, ")")
	s = strings.ReplaceAll(s, `\\`, `\`)
	return s
}

// findFirstUnescapedChar returns the index of the first unescaped occurrence of char in s,
// or -1 if not found.
func findFirstUnescapedChar(s string, char byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == char {
			// Count preceding backslashes
			count := 0
			for j := i - 1; j >= 0 && s[j] == '\\'; j-- {
				count++
			}
			if count%2 == 0 {
				return i
			}
		}
	}
	return -1
}

// findLastUnescapedChar returns the index of the last unescaped occurrence of char in s,
// or -1 if not found.
func findLastUnescapedChar(s string, char byte) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == char {
			count := 0
			for j := i - 1; j >= 0 && s[j] == '\\'; j-- {
				count++
			}
			if count%2 == 0 {
				return i
			}
		}
	}
	return -1
}

// RuleValueFromString parses a rule string like "Bash(npm:*)" into a RuleValue.
func RuleValueFromString(ruleString string) RuleValue {
	openIdx := findFirstUnescapedChar(ruleString, '(')
	closeIdx := findLastUnescapedChar(ruleString, ')')

	if openIdx < 0 || closeIdx < 0 {
		return RuleValue{ToolName: NormalizeLegacyToolName(ruleString)}
	}
	// Closing paren must be the last character
	if closeIdx != len(ruleString)-1 {
		return RuleValue{ToolName: NormalizeLegacyToolName(ruleString)}
	}

	toolName := ruleString[:openIdx]
	rawContent := ruleString[openIdx+1 : closeIdx]

	if toolName == "" {
		return RuleValue{ToolName: NormalizeLegacyToolName(ruleString)}
	}

	toolName = NormalizeLegacyToolName(toolName)

	// Empty content or bare "*" → tool-wide rule
	if rawContent == "" || rawContent == "*" {
		return RuleValue{ToolName: toolName}
	}

	return RuleValue{ToolName: toolName, RuleContent: UnescapeRuleContent(rawContent)}
}

// RuleValueToString serializes a RuleValue back to its string form.
func RuleValueToString(v RuleValue) string {
	if v.RuleContent == "" {
		return v.ToolName
	}
	return v.ToolName + "(" + EscapeRuleContent(v.RuleContent) + ")"
}
