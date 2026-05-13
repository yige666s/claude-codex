package skills

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const maxPromptShellOutputBytes = 1 << 20

// Frontmatter represents parsed YAML frontmatter from a skill file
type Frontmatter struct {
	Name                   string                 `yaml:"name"`
	Description            interface{}            `yaml:"description"` // string or array
	WhenToUse              string                 `yaml:"when_to_use"`
	ArgumentHint           string                 `yaml:"argument-hint"`
	Arguments              interface{}            `yaml:"arguments"` // string or array
	AllowedTools           interface{}            `yaml:"allowed-tools"`
	Model                  string                 `yaml:"model"`
	DisableModelInvocation interface{}            `yaml:"disable-model-invocation"`
	UserInvocable          interface{}            `yaml:"user-invocable"`
	Context                string                 `yaml:"context"`
	Agent                  string                 `yaml:"agent"`
	Effort                 interface{}            `yaml:"effort"`
	Version                string                 `yaml:"version"`
	Paths                  interface{}            `yaml:"paths"`
	Metadata               interface{}            `yaml:"metadata"`
	Hooks                  map[string]interface{} `yaml:"hooks"`
	Shell                  interface{}            `yaml:"shell"`
}

// ParsedSkillFile represents a parsed skill markdown file
type ParsedSkillFile struct {
	Frontmatter *Frontmatter
	Content     string // Markdown content after frontmatter
}

// ParseSkillFile parses a skill markdown file with YAML frontmatter
func ParseSkillFile(content string) (*ParsedSkillFile, error) {
	// Match frontmatter block (--- ... ---)
	re := regexp.MustCompile(`(?s)^---\s*\n(.*?)\n---\s*\n(.*)$`)
	matches := re.FindStringSubmatch(content)

	if matches == nil {
		// No frontmatter, treat entire content as markdown
		return &ParsedSkillFile{
			Frontmatter: &Frontmatter{},
			Content:     content,
		}, nil
	}

	// Parse YAML frontmatter. Claude Code tolerates common unquoted glob
	// frontmatter values, so retry with problematic scalar values quoted.
	var fm Frontmatter
	if err := yaml.Unmarshal([]byte(matches[1]), &fm); err != nil {
		quoted := quoteProblematicFrontmatterValues(matches[1])
		if quoted == matches[1] {
			return nil, fmt.Errorf("failed to parse frontmatter: %w", err)
		}
		if retryErr := yaml.Unmarshal([]byte(quoted), &fm); retryErr != nil {
			return nil, fmt.Errorf("failed to parse frontmatter: %w", err)
		}
	}

	return &ParsedSkillFile{
		Frontmatter: &fm,
		Content:     strings.TrimSpace(matches[2]),
	}, nil
}

func quoteProblematicFrontmatterValues(input string) string {
	var changed bool
	lines := strings.Split(input, "\n")
	keyValuePattern := regexp.MustCompile(`^([A-Za-z_-][A-Za-z0-9_-]*):\s+(.+)$`)
	for i, line := range lines {
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") || strings.HasPrefix(strings.TrimSpace(line), "-") {
			continue
		}
		matches := keyValuePattern.FindStringSubmatch(line)
		if len(matches) != 3 {
			continue
		}
		value := strings.TrimSpace(matches[2])
		if value == "" || strings.HasPrefix(value, "\"") || strings.HasPrefix(value, "'") || strings.HasPrefix(value, "[") || strings.HasPrefix(value, "{") {
			continue
		}
		if !isProblematicYAMLScalar(value) {
			continue
		}
		lines[i] = matches[1] + ": " + strconv.Quote(value)
		changed = true
	}
	if !changed {
		return input
	}
	return strings.Join(lines, "\n")
}

func isProblematicYAMLScalar(value string) bool {
	if strings.Contains(value, ": ") {
		return true
	}
	return strings.ContainsAny(value, "{}[]*&#!|>%@`")
}

// CoerceDescriptionToString converts description to string
func CoerceDescriptionToString(desc interface{}) string {
	if desc == nil {
		return ""
	}

	switch v := desc.(type) {
	case string:
		return strings.TrimSpace(v)
	case []interface{}:
		// Join array elements
		parts := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				parts = append(parts, strings.TrimSpace(s))
			}
		}
		return strings.Join(parts, " ")
	default:
		return fmt.Sprintf("%v", v)
	}
}

// ParseBooleanFrontmatter parses a boolean value from frontmatter
func ParseBooleanFrontmatter(value interface{}) bool {
	if value == nil {
		return false
	}

	switch v := value.(type) {
	case bool:
		return v
	case string:
		lower := strings.ToLower(strings.TrimSpace(v))
		return lower == "true" || lower == "yes" || lower == "1"
	case int:
		return v != 0
	default:
		return false
	}
}

// ParseStringArray parses a string or array into []string
func ParseStringArray(value interface{}) []string {
	if value == nil {
		return nil
	}

	switch v := value.(type) {
	case string:
		// Split by comma or newline
		parts := strings.FieldsFunc(v, func(r rune) bool {
			return r == ',' || r == '\n'
		})
		result := make([]string, 0, len(parts))
		for _, part := range parts {
			trimmed := strings.TrimSpace(part)
			if trimmed != "" {
				result = append(result, trimmed)
			}
		}
		return result
	case []interface{}:
		result := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				trimmed := strings.TrimSpace(s)
				if trimmed != "" {
					result = append(result, trimmed)
				}
			}
		}
		return result
	case []string:
		result := make([]string, 0, len(v))
		for _, s := range v {
			trimmed := strings.TrimSpace(s)
			if trimmed != "" {
				result = append(result, trimmed)
			}
		}
		return result
	default:
		return nil
	}
}

// ParseAllowedTools parses allowed-tools field
func ParseAllowedTools(value interface{}) []string {
	tools := ParseStringArray(value)
	if tools == nil {
		return []string{}
	}
	return tools
}

// ParseArgumentNames parses arguments field into argument names
func ParseArgumentNames(value interface{}) []string {
	items := make([]string, 0)
	switch v := value.(type) {
	case string:
		items = strings.Fields(v)
	case []interface{}:
		for _, item := range v {
			if s, ok := item.(string); ok {
				items = append(items, strings.Fields(s)...)
			}
		}
	case []string:
		for _, item := range v {
			items = append(items, strings.Fields(item)...)
		}
	default:
		return nil
	}

	result := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" || isNumericOnly(item) {
			continue
		}
		result = append(result, item)
	}
	return result
}

// ParsePaths parses paths field into path patterns
func ParsePaths(value interface{}) []string {
	patterns := parsePathPatterns(value)
	if patterns == nil {
		return nil
	}

	// Remove /** suffix and filter empty patterns
	result := make([]string, 0, len(patterns))
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}

		// Remove /** suffix
		pattern = strings.TrimSuffix(pattern, "/**")

		result = append(result, expandBracePattern(pattern)...)
	}

	// If all patterns are ** (match-all), return nil
	if len(result) > 0 {
		allMatchAll := true
		for _, p := range result {
			if p != "**" {
				allMatchAll = false
				break
			}
		}
		if allMatchAll {
			return nil
		}
	}

	return result
}

func isNumericOnly(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func parsePathPatterns(value interface{}) []string {
	switch v := value.(type) {
	case nil:
		return nil
	case string:
		return splitPathFrontmatter(v)
	case []interface{}:
		var result []string
		for _, item := range v {
			if s, ok := item.(string); ok {
				result = append(result, splitPathFrontmatter(s)...)
			}
		}
		return result
	case []string:
		var result []string
		for _, item := range v {
			result = append(result, splitPathFrontmatter(item)...)
		}
		return result
	default:
		return nil
	}
}

func splitPathFrontmatter(value string) []string {
	var result []string
	var current strings.Builder
	braceDepth := 0
	for _, r := range value {
		switch r {
		case '{':
			braceDepth++
			current.WriteRune(r)
		case '}':
			if braceDepth > 0 {
				braceDepth--
			}
			current.WriteRune(r)
		case ',':
			if braceDepth == 0 {
				appendTrimmedPath(&result, current.String())
				current.Reset()
			} else {
				current.WriteRune(r)
			}
		default:
			current.WriteRune(r)
		}
	}
	appendTrimmedPath(&result, current.String())
	return result
}

func appendTrimmedPath(result *[]string, value string) {
	value = strings.TrimSpace(value)
	if value != "" {
		*result = append(*result, value)
	}
}

func expandBracePattern(pattern string) []string {
	start := strings.Index(pattern, "{")
	if start == -1 {
		return []string{pattern}
	}
	end := findMatchingBrace(pattern, start)
	if end == -1 {
		return []string{pattern}
	}
	options := splitBraceOptions(pattern[start+1 : end])
	if len(options) == 0 {
		return []string{pattern}
	}
	result := make([]string, 0, len(options))
	prefix := pattern[:start]
	suffix := pattern[end+1:]
	for _, option := range options {
		result = append(result, expandBracePattern(prefix+option+suffix)...)
	}
	return result
}

func findMatchingBrace(pattern string, start int) int {
	depth := 0
	for i := start; i < len(pattern); i++ {
		switch pattern[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func splitBraceOptions(value string) []string {
	var result []string
	var current strings.Builder
	depth := 0
	for _, r := range value {
		switch r {
		case '{':
			depth++
			current.WriteRune(r)
		case '}':
			if depth > 0 {
				depth--
			}
			current.WriteRune(r)
		case ',':
			if depth == 0 {
				appendTrimmedPath(&result, current.String())
				current.Reset()
			} else {
				current.WriteRune(r)
			}
		default:
			current.WriteRune(r)
		}
	}
	appendTrimmedPath(&result, current.String())
	return result
}

// ParseEffort parses effort field (string or int)
func ParseEffort(value interface{}) *int {
	if value == nil {
		return nil
	}

	switch v := value.(type) {
	case int:
		return &v
	case string:
		// Try to parse as integer
		if i, err := strconv.Atoi(v); err == nil {
			return &i
		}

		// Try to parse as effort level name
		lower := strings.ToLower(strings.TrimSpace(v))
		effortMap := map[string]int{
			"minimal":    1,
			"low":        2,
			"medium":     3,
			"high":       4,
			"max":        5,
			"exhaustive": 5,
		}
		if level, ok := effortMap[lower]; ok {
			return &level
		}
	}

	return nil
}

func ParseSkillMetadataEnv(value interface{}) ([]string, string) {
	meta, ok := value.(map[string]interface{})
	if !ok {
		return nil, ""
	}
	openclaw, ok := meta["openclaw"].(map[string]interface{})
	if !ok {
		return nil, ""
	}
	primaryEnv, _ := openclaw["primaryEnv"].(string)
	requires, ok := openclaw["requires"].(map[string]interface{})
	if !ok {
		return uniqueNonEmpty([]string{primaryEnv}), strings.TrimSpace(primaryEnv)
	}
	envList := ParseStringArray(requires["env"])
	if strings.TrimSpace(primaryEnv) != "" {
		envList = append(envList, primaryEnv)
	}
	return uniqueNonEmpty(envList), strings.TrimSpace(primaryEnv)
}

func ParseSkillMetadata(value interface{}) map[string]any {
	meta, ok := value.(map[string]interface{})
	if !ok {
		return nil
	}
	out := make(map[string]any, len(meta))
	for key, item := range meta {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		out[key] = item
	}
	return out
}

func ParseSkillMetadataRunAsJob(value interface{}) bool {
	meta, ok := value.(map[string]interface{})
	if !ok {
		return false
	}
	if truthyMetadata(meta["job"]) || truthyMetadata(meta["run_as_job"]) || truthyMetadata(meta["long_running"]) || truthyMetadata(meta["produces_artifacts"]) {
		return true
	}
	for _, key := range []string{"agentapi", "runtime", "openclaw"} {
		nested, ok := meta[key].(map[string]interface{})
		if !ok {
			continue
		}
		if truthyMetadata(nested["job"]) || truthyMetadata(nested["run_as_job"]) || truthyMetadata(nested["long_running"]) || truthyMetadata(nested["produces_artifacts"]) {
			return true
		}
		execution := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", nested["execution"])))
		if execution == "job" || execution == "durable_job" || execution == "background" {
			return true
		}
	}
	return false
}

func truthyMetadata(value interface{}) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "1", "true", "yes", "y", "on", "job", "durable_job", "background", "long", "long_running":
			return true
		}
	case int:
		return v != 0
	case int64:
		return v != 0
	case float64:
		return v != 0
	}
	return false
}

func uniqueNonEmpty(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func ParseShellFrontmatter(value interface{}) FrontmatterShell {
	if value == nil {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", value))) {
	case "bash":
		return ShellBash
	case "powershell":
		return ShellPowerShell
	default:
		return ""
	}
}

var (
	blockPattern         = regexp.MustCompile("(?s)```!\\s*\\n?(.*?)\\n?```")
	inlinePattern        = regexp.MustCompile("(?m)(^|\\s)!`([^`]+)`")
	lookPath             = exec.LookPath
	pythonCommandPattern = regexp.MustCompile(`(^|\&\&\s*|\|\|\s*|;\s*|\(\s*|\n\s*)((?:[A-Za-z_][A-Za-z0-9_]*=[^\s]+\s+)*)python(\s)`)
)

func ExecuteShellCommandsInPrompt(text string, shell FrontmatterShell, workingDir string, environment map[string]string, allowedTools []string, runtime PromptShellRuntime) (string, error) {
	return ExecuteShellCommandsInPromptWithTimeout(text, shell, workingDir, environment, allowedTools, runtime, 0)
}

func ExecuteShellCommandsInPromptWithTimeout(text string, shell FrontmatterShell, workingDir string, environment map[string]string, allowedTools []string, runtime PromptShellRuntime, timeout time.Duration) (string, error) {
	result := text

	blockMatches := blockPattern.FindAllStringSubmatch(result, -1)
	for _, match := range blockMatches {
		if len(match) < 2 {
			continue
		}
		command := applyPython3Fallback(strings.TrimSpace(match[1]))
		output, err := executePromptShellCommand(command, shell, workingDir, environment, allowedTools, runtime, timeout)
		if err != nil {
			return "", err
		}
		result = strings.Replace(result, match[0], output, 1)
	}

	inlineMatches := inlinePattern.FindAllStringSubmatch(result, -1)
	for _, match := range inlineMatches {
		if len(match) < 3 {
			continue
		}
		command := applyPython3Fallback(strings.TrimSpace(match[2]))
		output, err := executePromptShellCommand(command, shell, workingDir, environment, allowedTools, runtime, timeout)
		if err != nil {
			return "", err
		}
		result = strings.Replace(result, match[0], match[1]+output, 1)
	}

	return result, nil
}

func executePromptShellCommand(command string, shell FrontmatterShell, workingDir string, environment map[string]string, allowedTools []string, runtime PromptShellRuntime, timeout time.Duration) (string, error) {
	if strings.TrimSpace(command) == "" {
		return "", nil
	}
	command = applyPython3Fallback(command)
	ctx := context.Background()
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	if runtime != nil && !(reflect.ValueOf(runtime).Kind() == reflect.Ptr && reflect.ValueOf(runtime).IsNil()) {
		if err := runtime.ValidateCommand(command); err != nil {
			return "", err
		}
		return runtime.ExecuteCommand(ctx, command)
	}
	if err := validateLocalPromptShellCommand(command, shell, allowedTools); err != nil {
		return "", err
	}

	exe := "bash"
	args := []string{"-lc", command}
	if shell == ShellPowerShell {
		exe = "pwsh"
		args = []string{"-NoProfile", "-Command", command}
	}
	cmd := exec.CommandContext(ctx, exe, args...)
	if strings.TrimSpace(workingDir) != "" {
		cmd.Dir = workingDir
	}
	cmd.Env = os.Environ()
	for key, value := range environment {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	var output limitBuffer
	output.limit = maxPromptShellOutputBytes
	cmd.Stdout = &output
	cmd.Stderr = &output
	err := cmd.Run()
	outputText := strings.TrimSpace(output.String())
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("shell command timed out after %s: %q", timeout, command)
		}
		if outputText == "" {
			return "", fmt.Errorf("shell command failed for %q: %v", command, err)
		}
		return "", fmt.Errorf("shell command failed for %q: %s", command, outputText)
	}
	if output.exceeded {
		return "", fmt.Errorf("shell command output exceeds max size of %d bytes", maxPromptShellOutputBytes)
	}
	return outputText, nil
}

type limitBuffer struct {
	bytes.Buffer
	limit    int
	exceeded bool
}

func (b *limitBuffer) Write(p []byte) (int, error) {
	if b.limit <= 0 {
		return len(p), nil
	}
	remaining := b.limit - b.Buffer.Len()
	if remaining <= 0 {
		b.exceeded = true
		return len(p), nil
	}
	if len(p) > remaining {
		b.exceeded = true
		_, _ = b.Buffer.Write(p[:remaining])
		return len(p), nil
	}
	_, _ = b.Buffer.Write(p)
	return len(p), nil
}

func validateLocalPromptShellCommand(command string, shell FrontmatterShell, allowedTools []string) error {
	if len(allowedTools) == 0 {
		return nil
	}
	toolName := "Bash"
	if shell == ShellPowerShell {
		toolName = "PowerShell"
	}
	for _, allowed := range allowedTools {
		allowed = strings.TrimSpace(allowed)
		if strings.EqualFold(allowed, toolName) {
			return nil
		}
		prefix := toolName + "("
		if !strings.HasPrefix(strings.ToLower(allowed), strings.ToLower(prefix)) || !strings.HasSuffix(allowed, ")") {
			continue
		}
		pattern := strings.TrimSpace(allowed[len(prefix) : len(allowed)-1])
		if promptShellPatternMatches(command, pattern) {
			return nil
		}
	}
	return fmt.Errorf("shell command %q is not allowed by skill allowed-tools", command)
}

func ValidatePromptShellCommand(command string, shell FrontmatterShell, allowedTools []string) error {
	return validateLocalPromptShellCommand(command, shell, allowedTools)
}

func promptShellPatternMatches(command, pattern string) bool {
	command = strings.TrimSpace(command)
	pattern = strings.TrimSpace(strings.ReplaceAll(pattern, ":", " "))
	if pattern == "" || pattern == "*" {
		return true
	}
	if strings.HasSuffix(pattern, "*") {
		return strings.HasPrefix(command, strings.TrimSpace(strings.TrimSuffix(pattern, "*")))
	}
	return command == pattern
}

func applyPython3Fallback(command string) string {
	if strings.TrimSpace(command) == "" {
		return command
	}
	if _, err := lookPath("python"); err == nil {
		return command
	}
	if _, err := lookPath("python3"); err != nil {
		return command
	}
	return pythonCommandPattern.ReplaceAllString(command, `${1}${2}python3${3}`)
}

// ExtractDescriptionFromMarkdown extracts description from markdown content
// Uses the first paragraph or heading as description
func ExtractDescriptionFromMarkdown(content string) string {
	lines := strings.Split(content, "\n")

	var description strings.Builder
	inParagraph := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Skip empty lines before first paragraph
		if !inParagraph && trimmed == "" {
			continue
		}

		// Skip headings
		if strings.HasPrefix(trimmed, "#") {
			continue
		}

		// Start of paragraph
		if !inParagraph && trimmed != "" {
			inParagraph = true
		}

		// End of paragraph
		if inParagraph && trimmed == "" {
			break
		}

		// Add line to description
		if inParagraph {
			if description.Len() > 0 {
				description.WriteString(" ")
			}
			description.WriteString(trimmed)
		}
	}

	result := description.String()
	if result == "" {
		return "No description available"
	}

	// Limit length
	if len(result) > 200 {
		result = result[:197] + "..."
	}

	return result
}

// EstimateTokenCount estimates token count for text (rough approximation)
func EstimateTokenCount(text string) int {
	// Rough estimate: ~4 characters per token
	return len(text) / 4
}
