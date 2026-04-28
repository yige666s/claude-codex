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

var shellSafeEnvVars = map[string]bool{
	"GOEXPERIMENT": true, "GOOS": true, "GOARCH": true, "CGO_ENABLED": true, "GO111MODULE": true,
	"RUST_BACKTRACE": true, "RUST_LOG": true,
	"NODE_ENV":         true,
	"PYTHONUNBUFFERED": true, "PYTHONDONTWRITEBYTECODE": true,
	"PYTEST_DISABLE_PLUGIN_AUTOLOAD": true, "PYTEST_DEBUG": true,
	"ANTHROPIC_API_KEY": true,
	"LANG":              true, "LANGUAGE": true, "LC_ALL": true, "LC_CTYPE": true, "LC_TIME": true, "CHARSET": true,
	"TERM": true, "COLORTERM": true, "NO_COLOR": true, "FORCE_COLOR": true, "TZ": true,
	"LS_COLORS": true, "LSCOLORS": true, "GREP_COLOR": true, "GREP_COLORS": true, "GCC_COLORS": true,
	"TIME_STYLE": true, "BLOCK_SIZE": true, "BLOCKSIZE": true,
}

var shellSubcommandNames = map[string]bool{
	"run": true, "exec": true, "test": true, "build": true, "start": true, "dev": true, "install": true,
	"commit": true, "checkout": true, "status": true, "diff": true, "log": true, "show": true, "push": true,
	"pull": true, "add": true, "reset": true, "restore": true, "switch": true, "merge": true, "rebase": true,
	"publish": true, "pack": true, "lint": true, "format": true, "generate": true, "migrate": true,
}

var shellBareWrapperPrefixes = map[string]bool{
	"sh": true, "bash": true, "zsh": true, "fish": true, "dash": true, "ksh": true, "env": true,
	"xargs": true, "nice": true, "nohup": true, "time": true, "timeout": true, "stdbuf": true,
	"sudo": true, "su": true, "doas": true,
}

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
			strings.HasPrefix(command, rule.Prefix+"\t") ||
			command == "xargs "+rule.Prefix ||
			strings.HasPrefix(command, "xargs "+rule.Prefix+" ") ||
			strings.HasPrefix(command, "xargs "+rule.Prefix+"\t")
	case ShellRuleWildcard:
		if isCompound {
			return false
		}
		return MatchWildcardPattern(rule.Pattern, command, false)
	}
	return false
}

// MatchesRuleForBehavior applies the same normalization strategy used by the
// TS Bash permission matcher. Allow rules only strip safe env vars/wrappers,
// while deny/ask rules also strip arbitrary leading env assignments so they
// cannot be bypassed with FOO=bar rm -rf ...
func MatchesRuleForBehavior(rule ShellPermissionRule, command string, behavior Behavior, isCompound bool) bool {
	stripAllEnv := behavior == BehaviorDeny || behavior == BehaviorAsk
	for _, candidate := range shellRuleMatchCandidates(command, stripAllEnv) {
		compoundForRule := false
		if behavior == BehaviorAllow {
			compoundForRule = isCompound || isShellCompoundCommand(candidate)
		}
		if MatchesRule(rule, candidate, compoundForRule) {
			return true
		}
	}
	return false
}

func shellRuleMatchCandidates(command string, stripAllEnv bool) []string {
	add := func(values []string, value string) []string {
		value = strings.TrimSpace(value)
		if value == "" {
			return values
		}
		for _, existing := range values {
			if existing == value {
				return values
			}
		}
		return append(values, value)
	}

	var candidates []string
	work := []string{command, stripOutputRedirections(command)}
	for _, value := range work {
		candidates = add(candidates, value)
		candidates = add(candidates, stripSafeShellWrappers(value))
		if stripAllEnv {
			candidates = add(candidates, stripAllLeadingShellEnvVars(value))
			candidates = add(candidates, stripSafeShellWrappers(stripAllLeadingShellEnvVars(value)))
		}
	}

	previousLen := -1
	for stripAllEnv && previousLen != len(candidates) {
		previousLen = len(candidates)
		snapshot := append([]string(nil), candidates...)
		for _, value := range snapshot {
			candidates = add(candidates, stripOutputRedirections(stripAllLeadingShellEnvVars(stripSafeShellWrappers(value))))
		}
	}
	return candidates
}

func stripCommentLines(command string) string {
	lines := strings.Split(command, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		kept = append(kept, line)
	}
	if len(kept) == 0 {
		return command
	}
	return strings.Join(kept, "\n")
}

func stripSafeShellWrappers(command string) string {
	stripped := strings.TrimSpace(command)
	for {
		next := stripSafeShellWrapperOnce(stripCommentLines(stripped))
		next = strings.TrimSpace(next)
		if next == stripped {
			return stripped
		}
		stripped = next
	}
}

func stripSafeShellWrapperOnce(command string) string {
	tokens := shellFields(command)
	if len(tokens) == 0 {
		return command
	}

	i := 0
	for i < len(tokens) && isEnvAssignment(tokens[i]) && shellSafeEnvVars[envAssignmentName(tokens[i])] {
		i++
	}
	if i > 0 {
		return strings.Join(tokens[i:], " ")
	}

	switch tokens[0] {
	case "time", "nohup", "command", "builtin":
		i = 1
		if i < len(tokens) && tokens[i] == "--" {
			i++
		}
		return strings.Join(tokens[i:], " ")
	case "env":
		i = 1
		for i < len(tokens) && isEnvAssignment(tokens[i]) && shellSafeEnvVars[envAssignmentName(tokens[i])] {
			i++
		}
		if i < len(tokens) && tokens[i] == "--" {
			i++
		}
		return strings.Join(tokens[i:], " ")
	case "timeout":
		i = 1
		for i < len(tokens) && strings.HasPrefix(tokens[i], "-") {
			flag := tokens[i]
			i++
			if (flag == "-k" || flag == "-s" || flag == "--kill-after" || flag == "--signal") && i < len(tokens) {
				i++
			}
		}
		if i < len(tokens) && tokens[i] == "--" {
			i++
		}
		if i < len(tokens) {
			i++
		}
		return strings.Join(tokens[i:], " ")
	case "nice":
		i = 1
		if i < len(tokens) && tokens[i] == "-n" {
			i += 2
		} else if i < len(tokens) && regexp.MustCompile(`^-\d+$`).MatchString(tokens[i]) {
			i++
		}
		if i < len(tokens) && tokens[i] == "--" {
			i++
		}
		return strings.Join(tokens[i:], " ")
	case "stdbuf":
		i = 1
		for i < len(tokens) && regexp.MustCompile(`^-[ioe][LN0-9]+$`).MatchString(tokens[i]) {
			i++
		}
		if i < len(tokens) && tokens[i] == "--" {
			i++
		}
		return strings.Join(tokens[i:], " ")
	default:
		return command
	}
}

func stripAllLeadingShellEnvVars(command string) string {
	tokens := shellFields(strings.TrimSpace(command))
	i := 0
	for i < len(tokens) && isEnvAssignment(tokens[i]) {
		i++
	}
	if i == 0 {
		return command
	}
	return strings.Join(tokens[i:], " ")
}

func stripOutputRedirections(command string) string {
	command = strings.TrimSpace(command)
	if command == "" {
		return command
	}

	var out strings.Builder
	inSingle, inDouble := false, false
	last := 0
	for i := 0; i < len(command); i++ {
		c := command[i]
		switch {
		case c == '\\':
			i++
			continue
		case c == '\'' && !inDouble:
			inSingle = !inSingle
			continue
		case c == '"' && !inSingle:
			inDouble = !inDouble
			continue
		case inSingle || inDouble:
			continue
		}

		start := -1
		if c == '>' {
			start = i
		} else if c == '&' && i+1 < len(command) && command[i+1] == '>' {
			start = i
		} else if c >= '0' && c <= '9' && i+1 < len(command) && command[i+1] == '>' {
			start = i
		}
		if start < 0 {
			continue
		}

		end := start
		if command[end] == '&' {
			end++
		} else if command[end] >= '0' && command[end] <= '9' {
			for end < len(command) && command[end] >= '0' && command[end] <= '9' {
				end++
			}
		}
		for end < len(command) && command[end] == '>' {
			end++
		}
		if end < len(command) && command[end] == '|' {
			end++
		}
		for end < len(command) && (command[end] == ' ' || command[end] == '\t') {
			end++
		}
		inTargetSingle, inTargetDouble := false, false
		for end < len(command) {
			tc := command[end]
			if tc == '\\' {
				end += 2
				continue
			}
			if tc == '\'' && !inTargetDouble {
				inTargetSingle = !inTargetSingle
				end++
				continue
			}
			if tc == '"' && !inTargetSingle {
				inTargetDouble = !inTargetDouble
				end++
				continue
			}
			if !inTargetSingle && !inTargetDouble && (tc == ' ' || tc == '\t' || tc == '\n' || tc == ';' || tc == '&' || tc == '|') {
				break
			}
			end++
		}
		out.WriteString(command[last:start])
		last = end
		i = end - 1
	}
	out.WriteString(command[last:])
	return strings.TrimSpace(strings.Join(strings.Fields(out.String()), " "))
}

func shellFields(command string) []string {
	var tokens []string
	var current strings.Builder
	inSingle, inDouble, escaped := false, false, false
	flush := func() {
		if current.Len() > 0 {
			tokens = append(tokens, current.String())
			current.Reset()
		}
	}
	for i := 0; i < len(command); i++ {
		c := command[i]
		if escaped {
			current.WriteByte(c)
			escaped = false
			continue
		}
		switch {
		case c == '\\' && !inSingle:
			escaped = true
		case c == '\'' && !inDouble:
			inSingle = !inSingle
		case c == '"' && !inSingle:
			inDouble = !inDouble
		case (c == ' ' || c == '\t' || c == '\n' || c == '\r') && !inSingle && !inDouble:
			flush()
		default:
			current.WriteByte(c)
		}
	}
	flush()
	return tokens
}

func isEnvAssignment(token string) bool {
	name := envAssignmentName(token)
	if name == "" {
		return false
	}
	return isShellIdentifier(name)
}

func envAssignmentName(token string) string {
	idx := strings.IndexByte(token, '=')
	if idx <= 0 {
		return ""
	}
	name := token[:idx]
	if strings.HasSuffix(name, "+") {
		name = strings.TrimSuffix(name, "+")
	}
	return name
}

func isShellIdentifier(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if i == 0 {
			if !((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || c == '_') {
				return false
			}
			continue
		}
		if !((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_') {
			return false
		}
	}
	return true
}

func isShellCompoundCommand(command string) bool {
	inSingle, inDouble, escaped := false, false, false
	for i := 0; i < len(command); i++ {
		c := command[i]
		if escaped {
			escaped = false
			continue
		}
		if c == '\\' && !inSingle {
			escaped = true
			continue
		}
		if c == '\'' && !inDouble {
			inSingle = !inSingle
			continue
		}
		if c == '"' && !inSingle {
			inDouble = !inDouble
			continue
		}
		if inSingle || inDouble {
			continue
		}
		if c == ';' || c == '\n' {
			return true
		}
		if (c == '&' || c == '|') && i+1 < len(command) && command[i+1] == c {
			return true
		}
		if c == '|' && !(i > 0 && command[i-1] == '>') {
			return true
		}
	}
	return false
}

// SuggestionForShellCommand builds a TS-style reusable Bash rule suggestion.
func SuggestionForShellCommand(toolName, command string) PermissionUpdate {
	command = strings.TrimSpace(command)
	if prefix := prefixBeforeHeredoc(command); prefix != "" {
		return SuggestionForPrefix(toolName, prefix)
	}
	if strings.Contains(command, "\n") {
		if firstLine := strings.TrimSpace(strings.Split(command, "\n")[0]); firstLine != "" {
			return SuggestionForPrefix(toolName, firstLine)
		}
	}
	if prefix := SimpleShellCommandPrefix(command); prefix != "" {
		return SuggestionForPrefix(toolName, prefix)
	}
	return SuggestionForExactCommand(toolName, command)
}

func prefixBeforeHeredoc(command string) string {
	idx := strings.Index(command, "<<")
	if idx <= 0 {
		return ""
	}
	before := strings.TrimSpace(command[:idx])
	if before == "" {
		return ""
	}
	if prefix := SimpleShellCommandPrefix(before); prefix != "" {
		return prefix
	}
	tokens := shellFields(before)
	i := 0
	for i < len(tokens) && isEnvAssignment(tokens[i]) {
		if !shellSafeEnvVars[envAssignmentName(tokens[i])] {
			return ""
		}
		i++
	}
	if i >= len(tokens) {
		return ""
	}
	end := i + 2
	if end > len(tokens) {
		end = len(tokens)
	}
	return strings.Join(tokens[i:end], " ")
}

// SimpleShellCommandPrefix returns a reusable two-word command prefix when the
// second word is a recognizable subcommand, after skipping safe env vars and
// wrappers. It deliberately avoids broad shell/wrapper prefixes such as bash:*.
func SimpleShellCommandPrefix(command string) string {
	stripped := stripSafeShellWrappers(command)
	tokens := shellFields(stripped)
	i := 0
	for i < len(tokens) && isEnvAssignment(tokens[i]) {
		if !shellSafeEnvVars[envAssignmentName(tokens[i])] {
			return ""
		}
		i++
	}
	if i >= len(tokens) {
		return ""
	}
	first := tokens[i]
	if shellBareWrapperPrefixes[first] || !isCommandWord(first) {
		return ""
	}
	if i+1 < len(tokens) && shellSubcommandNames[tokens[i+1]] {
		return first + " " + tokens[i+1]
	}
	return ""
}

func isCommandWord(value string) bool {
	if value == "" {
		return false
	}
	for i := 0; i < len(value); i++ {
		c := value[i]
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.') {
			return false
		}
	}
	return value[0] >= 'a' && value[0] <= 'z'
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
