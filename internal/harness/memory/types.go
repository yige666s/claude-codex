package memory

import "time"

// MemoryType represents the type of memory entry
type MemoryType string

const (
	MemoryTypeUser      MemoryType = "user"      // User information and preferences
	MemoryTypeFeedback  MemoryType = "feedback"  // User feedback and guidance
	MemoryTypeProject   MemoryType = "project"   // Project-specific information
	MemoryTypeReference MemoryType = "reference" // External resource references
)

// Memory represents a single memory entry
type Memory struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Type        MemoryType `json:"type"`
	Content     string     `json:"content"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	FilePath    string     `json:"file_path"` // Relative path from memory dir
}

// MemoryIndex represents the MEMORY.md index file
type MemoryIndex struct {
	Entries []MemoryIndexEntry `json:"entries"`
}

// MemoryIndexEntry represents a single line in MEMORY.md
type MemoryIndexEntry struct {
	Title       string `json:"title"`
	FilePath    string `json:"file_path"`
	Description string `json:"description"`
}

// SessionMemoryConfig controls automatic memory extraction.
// Mirrors SessionMemoryConfig / DEFAULT_SESSION_MEMORY_CONFIG in
// src/services/SessionMemory/sessionMemoryUtils.ts.
type SessionMemoryConfig struct {
	// Enabled controls whether session memory extraction is active.
	Enabled bool `json:"enabled"`
	// InitializationThreshold is the context-window token count that must be
	// reached before the first extraction fires (alias for
	// minimumMessageTokensToInit).
	InitializationThreshold int `json:"initialization_threshold"`
	// MinimumTokensBetweenUpdate is the minimum context-window growth (tokens)
	// required between subsequent extractions.
	MinimumTokensBetweenUpdate int `json:"minimum_tokens_between_update"`
	// ToolCallsBetweenUpdates is the minimum number of tool calls required
	// between subsequent extractions.
	ToolCallsBetweenUpdates int `json:"tool_calls_between_updates"`
}

// DefaultSessionMemoryConfig returns the default configuration, mirroring
// DEFAULT_SESSION_MEMORY_CONFIG in sessionMemoryUtils.ts.
func DefaultSessionMemoryConfig() SessionMemoryConfig {
	return SessionMemoryConfig{
		Enabled:                    true,
		InitializationThreshold:    10000, // ~10k tokens (matches TS minimumMessageTokensToInit)
		MinimumTokensBetweenUpdate: 5000,  // ~5k tokens (matches TS minimumTokensBetweenUpdate)
		ToolCallsBetweenUpdates:    3,     // matches TS toolCallsBetweenUpdates
	}
}

// ExtractionState tracks the state of memory extraction
type ExtractionState struct {
	Initialized              bool      `json:"initialized"`
	LastExtractionTokenCount int       `json:"last_extraction_token_count"`
	LastSummarizedMessageID  string    `json:"last_summarized_message_id"`
	ExtractionInProgress     bool      `json:"extraction_in_progress"`
	LastExtractionTime       time.Time `json:"last_extraction_time"`
}

// ExtractionResult represents the result of a memory extraction
type ExtractionResult struct {
	Success        bool   `json:"success"`
	MemoryPath     string `json:"memory_path,omitempty"`
	Error          string `json:"error,omitempty"`
	MemoriesFound  int    `json:"memories_found,omitempty"`
	MemoriesSaved  int    `json:"memories_saved,omitempty"`
	AgentTurnCount int    `json:"agent_turn_count,omitempty"`
}
