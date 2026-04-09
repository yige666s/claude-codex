package schemas

import (
	"fmt"
	"strings"
	"unicode"
)

// ValidatePermissionRule validates a permission rule pattern
func ValidatePermissionRule(rule string) error {
	if rule == "" {
		return fmt.Errorf("permission rule cannot be empty")
	}

	// Check for balanced parentheses
	if err := validateParentheses(rule); err != nil {
		return err
	}

	// Check for empty parentheses
	if strings.Contains(rule, "()") {
		return fmt.Errorf("empty parentheses are not allowed")
	}

	// Parse and validate the rule
	return validateRulePattern(rule)
}

// validateParentheses checks for balanced parentheses (escape-aware)
func validateParentheses(rule string) error {
	depth := 0
	escaped := false

	for i, ch := range rule {
		if escaped {
			escaped = false
			continue
		}

		if ch == '\\' {
			escaped = true
			continue
		}

		if ch == '(' {
			depth++
		} else if ch == ')' {
			depth--
			if depth < 0 {
				return fmt.Errorf("unmatched closing parenthesis at position %d", i)
			}
		}
	}

	if depth > 0 {
		return fmt.Errorf("unmatched opening parenthesis")
	}

	return nil
}

// validateRulePattern validates the rule pattern syntax
func validateRulePattern(rule string) error {
	// Check for MCP-specific patterns
	if strings.HasPrefix(rule, "mcp:") {
		return validateMCPPattern(rule)
	}

	// Check for Bash-specific patterns
	if strings.HasPrefix(rule, "Bash:") {
		return validateBashPattern(rule)
	}

	// Check for file patterns
	if strings.HasPrefix(rule, "Read(") || strings.HasPrefix(rule, "Write(") ||
		strings.HasPrefix(rule, "Edit(") || strings.HasPrefix(rule, "Glob(") {
		return validateFilePattern(rule)
	}

	// Check for tool name capitalization
	if err := validateToolNameCapitalization(rule); err != nil {
		return err
	}

	return nil
}

// validateMCPPattern validates MCP-specific patterns
func validateMCPPattern(rule string) error {
	// MCP patterns should not contain wildcards
	if strings.Contains(rule, "*") {
		return fmt.Errorf("MCP patterns do not support wildcards")
	}

	// Format: mcp:serverName or mcp:serverName:toolName
	parts := strings.Split(rule, ":")
	if len(parts) < 2 {
		return fmt.Errorf("invalid MCP pattern format, expected mcp:serverName or mcp:serverName:toolName")
	}

	if len(parts) > 3 {
		return fmt.Errorf("invalid MCP pattern format, too many colons")
	}

	// Server name should not be empty
	if parts[1] == "" {
		return fmt.Errorf("MCP server name cannot be empty")
	}

	// Tool name (if present) should not be empty
	if len(parts) == 3 && parts[2] == "" {
		return fmt.Errorf("MCP tool name cannot be empty")
	}

	return nil
}

// validateBashPattern validates Bash-specific patterns
func validateBashPattern(rule string) error {
	// Format: Bash or Bash:* or Bash:command
	if rule == "Bash" {
		return nil
	}

	if !strings.HasPrefix(rule, "Bash:") {
		return fmt.Errorf("invalid Bash pattern format")
	}

	suffix := strings.TrimPrefix(rule, "Bash:")
	if suffix == "" {
		return fmt.Errorf("Bash pattern cannot end with colon")
	}

	// Bash:* is valid (all commands)
	if suffix == "*" {
		return nil
	}

	// Otherwise it should be a command pattern
	return nil
}

// validateFilePattern validates file operation patterns
func validateFilePattern(rule string) error {
	// Extract the file path pattern from the rule
	// Format: ToolName(pattern)
	openParen := strings.Index(rule, "(")
	closeParen := strings.LastIndex(rule, ")")

	if openParen == -1 || closeParen == -1 {
		return fmt.Errorf("invalid file pattern format, expected ToolName(pattern)")
	}

	if closeParen < openParen {
		return fmt.Errorf("invalid parentheses in file pattern")
	}

	pattern := rule[openParen+1 : closeParen]
	if pattern == "" {
		return fmt.Errorf("file pattern cannot be empty")
	}

	// Validate glob pattern syntax
	return validateGlobPattern(pattern)
}

// validateGlobPattern validates glob pattern syntax
func validateGlobPattern(pattern string) error {
	// Check for invalid characters
	invalidChars := []rune{'\x00', '\n', '\r'}
	for _, ch := range invalidChars {
		if strings.ContainsRune(pattern, ch) {
			return fmt.Errorf("glob pattern contains invalid character: %q", ch)
		}
	}

	// Check for unmatched brackets
	bracketDepth := 0
	for _, ch := range pattern {
		if ch == '[' {
			bracketDepth++
		} else if ch == ']' {
			bracketDepth--
			if bracketDepth < 0 {
				return fmt.Errorf("unmatched closing bracket in glob pattern")
			}
		}
	}

	if bracketDepth > 0 {
		return fmt.Errorf("unmatched opening bracket in glob pattern")
	}

	return nil
}

// validateToolNameCapitalization validates tool name capitalization
func validateToolNameCapitalization(rule string) error {
	// Extract tool name (before first parenthesis or colon)
	toolName := rule
	if idx := strings.IndexAny(rule, "(:"); idx != -1 {
		toolName = rule[:idx]
	}

	// Tool names should start with uppercase
	if len(toolName) > 0 && !unicode.IsUpper(rune(toolName[0])) {
		return fmt.Errorf("tool name should start with uppercase letter: %s", toolName)
	}

	return nil
}

// IsPermissionRuleValid checks if a permission rule is valid
func IsPermissionRuleValid(rule string) bool {
	return ValidatePermissionRule(rule) == nil
}

// FilterInvalidPermissionRules filters out invalid permission rules
func FilterInvalidPermissionRules(rules []PermissionRule) []PermissionRule {
	valid := make([]PermissionRule, 0, len(rules))
	for _, rule := range rules {
		if IsPermissionRuleValid(string(rule)) {
			valid = append(valid, rule)
		}
	}
	return valid
}

// NormalizePermissionRule normalizes a permission rule
func NormalizePermissionRule(rule string) string {
	// Trim whitespace
	rule = strings.TrimSpace(rule)

	// Normalize multiple spaces to single space
	rule = strings.Join(strings.Fields(rule), " ")

	return rule
}
