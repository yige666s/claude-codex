package prompt

import (
	"sync"
)

// SystemPrompt is a branded type for system prompt arrays.
// It represents an immutable collection of prompt strings that form
// the system context for Claude API calls.
type SystemPrompt struct {
	sections []string
	mu       sync.RWMutex
}

// NewSystemPrompt creates a new SystemPrompt from a slice of strings.
// The input slice is copied to ensure immutability.
func NewSystemPrompt(sections []string) *SystemPrompt {
	copied := make([]string, len(sections))
	copy(copied, sections)
	return &SystemPrompt{
		sections: copied,
	}
}

// Sections returns a copy of the prompt sections.
func (sp *SystemPrompt) Sections() []string {
	sp.mu.RLock()
	defer sp.mu.RUnlock()

	result := make([]string, len(sp.sections))
	copy(result, sp.sections)
	return result
}

// Len returns the number of sections in the prompt.
func (sp *SystemPrompt) Len() int {
	sp.mu.RLock()
	defer sp.mu.RUnlock()
	return len(sp.sections)
}

// IsEmpty returns true if the prompt has no sections.
func (sp *SystemPrompt) IsEmpty() bool {
	return sp.Len() == 0
}

// SystemPromptSection represents a single section of the SystemPrompt
// with optional caching behavior.
type SystemPromptSection struct {
	// Name is the unique identifier for this section
	Name string

	// Compute is the function that generates the section content
	Compute ComputeFunc

	// CacheBreak indicates if this section should bypass caching
	// and recompute on every turn (use sparingly as it breaks prompt cache)
	CacheBreak bool
}

// ComputeFunc is a function that computes a SystemPrompt section.
// It may return nil if the section should be omitted.
type ComputeFunc func() (string, error)

// NewSection creates a cached SystemPrompt section.
// The section will be computed once and cached until cleared.
func NewSection(name string, compute ComputeFunc) *SystemPromptSection {
	return &SystemPromptSection{
		Name:       name,
		Compute:    compute,
		CacheBreak: false,
	}
}

// NewUncachedSection creates a volatile SystemPrompt section that recomputes every turn.
// This WILL break the prompt cache when the value changes.
// Use only when absolutely necessary (e.g., dynamic context that must be fresh).
func NewUncachedSection(name string, compute ComputeFunc, reason string) *SystemPromptSection {
	// reason is for documentation purposes - helps developers understand
	// why cache-breaking is necessary for this section
	_ = reason
	return &SystemPromptSection{
		Name:       name,
		Compute:    compute,
		CacheBreak: true,
	}
}
