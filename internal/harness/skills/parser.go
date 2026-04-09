package skills

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

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

	// Parse YAML frontmatter
	var fm Frontmatter
	if err := yaml.Unmarshal([]byte(matches[1]), &fm); err != nil {
		return nil, fmt.Errorf("failed to parse frontmatter: %w", err)
	}

	return &ParsedSkillFile{
		Frontmatter: &fm,
		Content:     strings.TrimSpace(matches[2]),
	}, nil
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
	return ParseStringArray(value)
}

// ParsePaths parses paths field into path patterns
func ParsePaths(value interface{}) []string {
	patterns := ParseStringArray(value)
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

		result = append(result, pattern)
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
			"exhaustive": 5,
		}
		if level, ok := effortMap[lower]; ok {
			return &level
		}
	}

	return nil
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
