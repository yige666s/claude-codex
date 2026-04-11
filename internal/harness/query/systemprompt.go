package query

import (
	"context"
	"fmt"

	harnesscontext "claude-codex/internal/harness/context"
	"claude-codex/internal/harness/mcp"
	"claude-codex/internal/harness/prompt"
	"claude-codex/internal/public/types"
)

// SystemPromptBuilder handles dynamic system prompt construction for queries.
type SystemPromptBuilder struct {
	promptBuilder *prompt.Builder
}

// NewSystemPromptBuilder creates a new system prompt builder.
func NewSystemPromptBuilder() *SystemPromptBuilder {
	return &SystemPromptBuilder{
		promptBuilder: prompt.NewBuilder(),
	}
}

// BuildSystemPrompt constructs the system prompt for a query.
// This integrates context collection and prompt building.
func (b *SystemPromptBuilder) BuildSystemPrompt(
	ctx context.Context,
	userContext map[string]string,
	systemContext map[string]string,
	customPrompt string,
	appendPrompt string,
	model string,
	mcpClients ...*mcp.Client,
) (types.SystemPrompt, error) {
	// Create prompt context
	promptCtx := prompt.NewPromptContext()
	promptCtx.UserContext = userContext
	promptCtx.SystemContext = systemContext
	promptCtx.CustomSystemPrompt = customPrompt
	promptCtx.AppendSystemPrompt = appendPrompt
	promptCtx.MainLoopModel = model

	// Build the system prompt
	systemPrompt, err := b.promptBuilder.BuildSystemPrompt(ctx, promptCtx)
	if err != nil {
		return types.SystemPrompt{}, fmt.Errorf("failed to build system prompt: %w", err)
	}

	sp := convertToTypesSystemPrompt(systemPrompt)

	// Inject MCP server instructions as an additional section (cache-breaking per turn).
	if len(mcpClients) > 0 {
		mcpSection := mcp.GetMCPInstructionsSection(mcpClients)
		if mcpSection != "" {
			if sp.Content != "" {
				sp.Content += "\n\n" + mcpSection
			} else {
				sp.Content = mcpSection
			}
			sp.Parts = append(sp.Parts, types.SystemPromptPart{
				Type:    "text",
				Content: mcpSection,
				Cache:   false, // MCP instructions can change between turns
			})
		}
	}

	return sp, nil
}

// BuildSystemPromptWithSections constructs system prompt from sections.
func (b *SystemPromptBuilder) BuildSystemPromptWithSections(
	ctx context.Context,
	sections []*prompt.SystemPromptSection,
) (types.SystemPrompt, error) {
	systemPrompt, err := b.promptBuilder.BuildFromSections(ctx, sections)
	if err != nil {
		return types.SystemPrompt{}, fmt.Errorf("failed to build from sections: %w", err)
	}

	return convertToTypesSystemPrompt(systemPrompt), nil
}

// CollectContext gathers system and user context for the query.
func (b *SystemPromptBuilder) CollectContext(
	ctx context.Context,
	workingDir string,
	includeGit bool,
) (userContext, systemContext map[string]string, err error) {
	// Collect workspace context using the context package
	opts := harnesscontext.DefaultCollectorOptions()
	opts.IncludeGit = includeGit

	wsCtx := harnesscontext.CollectWithOptions(workingDir, opts)

	// Convert to map format
	systemContext = map[string]string{
		"platform":  wsCtx.Platform,
		"shell":     wsCtx.Shell,
		"osVersion": wsCtx.OSVersion,
		"gitStatus": wsCtx.GitStatus,
	}

	userContext = map[string]string{
		"claudeMd": wsCtx.ClaudeMD,
	}

	// Add git info if available
	if wsCtx.IsGitRepo {
		systemContext["gitBranch"] = wsCtx.GitBranch
	}

	return userContext, systemContext, nil
}

// ClearCache clears the prompt builder cache.
func (b *SystemPromptBuilder) ClearCache() {
	b.promptBuilder.ClearCache()
}

// GetCacheStats returns cache statistics.
func (b *SystemPromptBuilder) GetCacheStats() prompt.CacheStats {
	return b.promptBuilder.GetCacheStats()
}

// convertToTypesSystemPrompt converts internal SystemPrompt to types.SystemPrompt.
func convertToTypesSystemPrompt(sp *prompt.SystemPrompt) types.SystemPrompt {
	if sp == nil || sp.IsEmpty() {
		return types.SystemPrompt{}
	}

	sections := sp.Sections()

	// Convert sections to Parts
	parts := make([]types.SystemPromptPart, len(sections))
	for i, section := range sections {
		parts[i] = types.SystemPromptPart{
			Type:    "text",
			Content: section,
			Cache:   true, // Enable caching by default
		}
	}

	// Join all sections into content
	content := ""
	for i, section := range sections {
		if i > 0 {
			content += "\n\n"
		}
		content += section
	}

	return types.SystemPrompt{
		Content: content,
		Parts:   parts,
	}
}

// getSystemPrompt is the main entry point for building system prompts in queries.
// This is called by QueryEngine to construct the system prompt for each query.
func getSystemPrompt(
	ctx context.Context,
	builder *SystemPromptBuilder,
	params *QueryParams,
	model string,
) (types.SystemPrompt, error) {
	// If SystemPrompt is already provided in params, use it
	if !isEmptySystemPrompt(params.SystemPrompt) {
		return params.SystemPrompt, nil
	}

	// Otherwise, build it dynamically
	return builder.BuildSystemPrompt(
		ctx,
		params.UserContext,
		params.SystemContext,
		"", // customPrompt - can be extended later
		"", // appendPrompt - can be extended later
		model,
	)
}

// isEmptySystemPrompt checks if a SystemPrompt is empty.
func isEmptySystemPrompt(sp types.SystemPrompt) bool {
	return sp.Content == "" && len(sp.Parts) == 0
}
