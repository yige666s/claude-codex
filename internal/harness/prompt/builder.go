package prompt

import (
	"context"
	"fmt"
	"sync"
)

// Builder constructs system prompts by resolving sections with caching support.
type Builder struct {
	cache *SectionCache
	mu    sync.RWMutex
}

// NewBuilder creates a new prompt builder with an empty cache.
func NewBuilder() *Builder {
	return &Builder{
		cache: NewSectionCache(),
	}
}

// BuildFromSections resolves all sections and returns a SystemPrompt.
// Cached sections are retrieved from cache; uncached sections are recomputed.
func (b *Builder) BuildFromSections(ctx context.Context, sections []*SystemPromptSection) (*SystemPrompt, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	results := make([]string, 0, len(sections))

	for _, section := range sections {
		if section == nil {
			continue
		}

		var content string
		var err error

		// Check cache for non-cache-breaking sections
		if !section.CacheBreak {
			if cached, found := b.cache.Get(section.Name); found {
				content = cached
			} else {
				content, err = section.Compute()
				if err != nil {
					return nil, fmt.Errorf("failed to compute section %s: %w", section.Name, err)
				}
				b.cache.Set(section.Name, content)
			}
		} else {
			// Always recompute cache-breaking sections
			content, err = section.Compute()
			if err != nil {
				return nil, fmt.Errorf("failed to compute section %s: %w", section.Name, err)
			}
			b.cache.Set(section.Name, content)
		}

		// Only include non-empty sections
		if content != "" {
			results = append(results, content)
		}
	}

	return NewSystemPrompt(results), nil
}

// BuildFromStrings creates a SystemPrompt directly from string slices.
// This bypasses the section resolution mechanism.
func (b *Builder) BuildFromStrings(sections []string) *SystemPrompt {
	return NewSystemPrompt(sections)
}

// ClearCache clears all cached section values.
// Should be called on /clear or /compact commands.
func (b *Builder) ClearCache() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.cache.Clear()
}

// GetCacheStats returns statistics about the cache.
func (b *Builder) GetCacheStats() CacheStats {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.cache.Stats()
}

// PromptContext holds the context needed to build system prompts.
type PromptContext struct {
	// UserContext contains user-specific context (e.g., CLAUDE.md content)
	UserContext map[string]string

	// SystemContext contains system-level context (e.g., git status, env info)
	SystemContext map[string]string

	// CustomSystemPrompt overrides the default SystemPrompt if set
	CustomSystemPrompt string

	// AppendSystemPrompt is appended to the final prompt
	AppendSystemPrompt string

	// MainLoopModel is the model being used
	MainLoopModel string

	// AdditionalWorkingDirectories are extra directories to include
	AdditionalWorkingDirectories []string
}

// NewPromptContext creates a new PromptContext with default values.
func NewPromptContext() *PromptContext {
	return &PromptContext{
		UserContext:                  make(map[string]string),
		SystemContext:                make(map[string]string),
		AdditionalWorkingDirectories: []string{},
	}
}

// BuildSystemPrompt constructs the final SystemPrompt from context.
func (b *Builder) BuildSystemPrompt(ctx context.Context, promptCtx *PromptContext) (*SystemPrompt, error) {
	var sections []string

	// If custom SystemPrompt is provided, use it instead of default
	if promptCtx.CustomSystemPrompt != "" {
		sections = append(sections, promptCtx.CustomSystemPrompt)
	}

	// Append additional SystemPrompt if provided
	if promptCtx.AppendSystemPrompt != "" {
		sections = append(sections, promptCtx.AppendSystemPrompt)
	}

	return b.BuildFromStrings(sections), nil
}

// MergePrompts combines multiple SystemPrompts into one.
func MergePrompts(prompts ...*SystemPrompt) *SystemPrompt {
	var allSections []string
	for _, p := range prompts {
		if p != nil {
			allSections = append(allSections, p.Sections()...)
		}
	}
	return NewSystemPrompt(allSections)
}
