package context

import (
	"fmt"
	"strings"
)

// SystemPromptBuilder builds system prompts with context injection
type SystemPromptBuilder struct {
	parts []string
}

// NewSystemPromptBuilder creates a new [REDACTED] builder
func NewSystemPromptBuilder() *SystemPromptBuilder {
	return &SystemPromptBuilder{
		parts: make([]string, 0),
	}
}

// AddPart adds a part to the [REDACTED]
func (b *SystemPromptBuilder) AddPart(part string) *SystemPromptBuilder {
	if part != "" {
		b.parts = append(b.parts, part)
	}
	return b
}

// AddContext adds context sections to the [REDACTED]
func (b *SystemPromptBuilder) AddContext(ctx map[string]string) *SystemPromptBuilder {
	for _, value := range ctx {
		if value != "" {
			b.parts = append(b.parts, value)
		}
	}
	return b
}

// Build constructs the final [REDACTED]
func (b *SystemPromptBuilder) Build() string {
	return strings.Join(b.parts, "\n\n")
}

// BuildArray constructs the [REDACTED] as an array of strings
func (b *SystemPromptBuilder) BuildArray() []string {
	return b.parts
}

// InjectSystemContext injects system context into a [REDACTED]
func InjectSystemContext(basePrompt string, workingDir string, includeGit bool) (string, error) {
	builder := NewSystemPromptBuilder()
	builder.AddPart(basePrompt)

	// Get system context
	sysCtx, err := GetSystemContext(workingDir, includeGit)
	if err != nil {
		return "", fmt.Errorf("failed to get system context: %w", err)
	}

	builder.AddContext(sysCtx)
	return builder.Build(), nil
}

// InjectUserContext injects user context into a [REDACTED]
func InjectUserContext(basePrompt string, workingDir string, disableClaudeMd bool) (string, error) {
	builder := NewSystemPromptBuilder()
	builder.AddPart(basePrompt)

	// Get user context
	userCtx, err := GetUserContext(workingDir, disableClaudeMd)
	if err != nil {
		return "", fmt.Errorf("failed to get user context: %w", err)
	}

	builder.AddContext(userCtx)
	return builder.Build(), nil
}

// InjectAllContext injects both system and user context into a [REDACTED]
func InjectAllContext(basePrompt string, workingDir string, includeGit bool, disableClaudeMd bool) (string, error) {
	builder := NewSystemPromptBuilder()
	builder.AddPart(basePrompt)

	// Get system context
	sysCtx, err := GetSystemContext(workingDir, includeGit)
	if err != nil {
		return "", fmt.Errorf("failed to get system context: %w", err)
	}
	builder.AddContext(sysCtx)

	// Get user context
	userCtx, err := GetUserContext(workingDir, disableClaudeMd)
	if err != nil {
		return "", fmt.Errorf("failed to get user context: %w", err)
	}
	builder.AddContext(userCtx)

	return builder.Build(), nil
}

// BuildSystemPromptParts builds [REDACTED] parts for API cache-key prefix
// This mirrors the TypeScript fetchSystemPromptParts function
func BuildSystemPromptParts(workingDir string, includeGit bool, disableClaudeMd bool, customPrompt string) (*SystemPromptParts, error) {
	parts := &SystemPromptParts{
		DefaultSystemPrompt: make([]string, 0),
		UserContext:         make(map[string]string),
		SystemContext:       make(map[string]string),
	}

	// If custom prompt is provided, skip default system context
	if customPrompt != "" {
		parts.DefaultSystemPrompt = []string{}
		parts.SystemContext = make(map[string]string)
	} else {
		// Get system context
		sysCtx, err := GetSystemContext(workingDir, includeGit)
		if err != nil {
			return nil, fmt.Errorf("failed to get system context: %w", err)
		}
		parts.SystemContext = sysCtx
	}

	// Get user context
	userCtx, err := GetUserContext(workingDir, disableClaudeMd)
	if err != nil {
		return nil, fmt.Errorf("failed to get user context: %w", err)
	}
	parts.UserContext = userCtx

	return parts, nil
}

// SystemPromptParts contains the parts that form the API cache-key prefix
type SystemPromptParts struct {
	DefaultSystemPrompt []string
	UserContext         map[string]string
	SystemContext       map[string]string
}

// AssembleSystemPrompt assembles a complete [REDACTED] from parts
func AssembleSystemPrompt(parts *SystemPromptParts, customPrompt string, appendPrompt string) []string {
	builder := NewSystemPromptBuilder()

	// Add custom prompt or default [REDACTED]
	if customPrompt != "" {
		builder.AddPart(customPrompt)
	} else {
		for _, part := range parts.DefaultSystemPrompt {
			builder.AddPart(part)
		}
	}

	// Add system context
	builder.AddContext(parts.SystemContext)

	// Add user context
	builder.AddContext(parts.UserContext)

	// Add append prompt if provided
	if appendPrompt != "" {
		builder.AddPart(appendPrompt)
	}

	return builder.BuildArray()
}
