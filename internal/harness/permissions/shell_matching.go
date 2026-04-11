package permissions

import (
	"regexp"
	"strings"
)

// ShellRuleType identifies the matching strategy for a shell permission rule.
type ShellRuleType string

const (
	ShellRuleExact    ShellRuleType = "exact"
	ShellRulePrefix   ShellRuleType = "prefix"   // legacy "cmd:*" syntax
	ShellRuleWildcard ShellRuleType = "wildcard" // glob * pattern
)

// ShellPermissionRule is a parsed representation of a shell command permission rule.
type ShellPermissionRule struct {
	Type    ShellRuleType
	Command string // for exact
	Prefix  string // for prefix
	Pattern string // for wildcard
}

var prefixRuleRE = regexp.MustCompile(`^(.+):\*$`)

// hasWildcards returns true if the pattern contains an unescaped * that is not a legacy prefix.
func hasWildcards(pattern string) bool {
	// Legacy "cmd:*" → prefix, not wildcard
	if prefixRuleRE.MatchString(pattern) {
		return false
	}
	for i := 0; i < len(pattern); i++ {
		if pattern[i] == '*' {
			count := 0
			for j := i - 1; j >= 0 && pattern[j] == '\\'; j-- {
				count++
			}
			if count%2 == 0 {
				return true
			}
		}
	}
	return false
}

// ParseShellPermissionRule converts a rule string into a typed ShellPermissionRule.
func ParseShellPermissionRule(rule string) ShellPermissionRule {
	if m := prefixRuleRE.FindStringSubmatch(rule); m != nil {
		return ShellPermissionRule{Type: ShellRulePrefix, Prefix: m[1]}
	}
	if hasWildcards(rule) {
		return ShellPermissionRule{Type: ShellRuleWildcard, Pattern: rule}
	}
	return ShellPermissionRule{Type: ShellRuleExact, Command: rule}
}

const (
	escapedStarPlaceholder      = "\x00ESCAPED_STAR\x00"
	escapedBackslashPlaceholder = "\x00ESCAPED_BACKSLASH\x00"
)

// MatchWildcardPattern tests command against a glob wildcard pattern.
// The pattern can use * as a wildcard; \* is a literal asterisk.
func MatchWildcardPattern(pattern, command string, caseInsensitive bool) bool {
	pattern = strings.TrimSpace(pattern)

	// Escape backslash sequences first
	var processed strings.Builder
	i := 0
	for i < len(pattern) {
		if pattern[i] == '\\' && i+1 < len(pattern) {
			switch pattern[i+1] {
			case '*':
				processed.WriteString(escapedStarPlaceholder)
				i += 2
				continue
			case '\\':
				processed.WriteString(escapedBackslashPlaceholder)
				i += 2
				continue
			}
		}
		processed.WriteByte(pattern[i])
		i++
	}
	p := processed.String()

	// Escape regex special chars (except our placeholders which use \x00)
	reSpecial := regexp.MustCompile(`[.+?^${}()\[\]|'"]`)
	p = reSpecial.ReplaceAllStringFunc(p, func(s string) string { return `\` + s })

	// Count unescaped wildcards (after placeholder substitution, unescaped * are the originals)
	wildcardCount := strings.Count(p, "*")

	// Special rule: if pattern ends with " .*" (space + wildcard) and there's exactly one wildcard,
	// make trailing args optional so "git *" matches bare "git".
	if wildcardCount == 1 && strings.HasSuffix(p, ` *`) {
		// Replace trailing " *" with optional group
		p = p[:len(p)-2] + `( .*)?`
	} else {
		// Convert remaining * to .*
		p = strings.ReplaceAll(p, "*", ".*")
	}

	// Restore placeholders
	p = strings.ReplaceAll(p, escapedStarPlaceholder, `\*`)
	p = strings.ReplaceAll(p, escapedBackslashPlaceholder, `\\`)

	flags := "(?s)"
	if caseInsensitive {
		flags = "(?si)"
	}
	re, err := regexp.Compile(flags + "^" + p + "$")
	if err != nil {
		return false
	}
	return re.MatchString(command)
}

// MatchesRule checks if a command matches a ShellPermissionRule.
// isCompound should be true when the command contains multiple subcommands (split by ;|& etc.).
func MatchesRule(rule ShellPermissionRule, command string, isCompound bool) bool {
	switch rule.Type {
	case ShellRuleExact:
		return rule.Command == command
	case ShellRulePrefix:
		if isCompound {
			return false // prefix/wildcard allow rules must not match compound commands
		}
		return command == rule.Prefix ||
			strings.HasPrefix(command, rule.Prefix+" ") ||
			strings.HasPrefix(command, rule.Prefix+"\t")
	case ShellRuleWildcard:
		if isCompound {
			return false
		}
		return MatchWildcardPattern(rule.Pattern, command, false)
	}
	return false
}

// SuggestionForExactCommand builds a PermissionUpdate to add an exact allow rule.
func SuggestionForExactCommand(toolName, command string) PermissionUpdate {
	return PermissionUpdate{
		Type:        UpdateAddRules,
		Destination: SourceLocalSettings,
		Rules:       []RuleValue{{ToolName: toolName, RuleContent: command}},
		Behavior:    BehaviorAllow,
	}
}

// SuggestionForPrefix builds a PermissionUpdate to add a prefix allow rule.
func SuggestionForPrefix(toolName, prefix string) PermissionUpdate {
	return PermissionUpdate{
		Type:        UpdateAddRules,
		Destination: SourceLocalSettings,
		Rules:       []RuleValue{{ToolName: toolName, RuleContent: prefix + ":*"}},
		Behavior:    BehaviorAllow,
	}
}
