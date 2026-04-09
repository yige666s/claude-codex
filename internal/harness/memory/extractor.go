package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ding/claude-code/claude-go/internal/harness/agent"
)

// Extractor handles automatic memory extraction from conversations
type Extractor struct {
	storage      *Storage
	config       SessionMemoryConfig
	state        ExtractionState
	mu           sync.RWMutex
	agentManager *agent.Manager // Agent manager for extraction
}

// NewExtractor creates a new memory extractor
func NewExtractor(memoryDir string, config SessionMemoryConfig) *Extractor {
	return &Extractor{
		storage:      NewStorage(memoryDir),
		config:       config,
		agentManager: nil, // Set via SetAgentManager
		state: ExtractionState{
			Initialized:              false,
			LastExtractionTokenCount: 0,
			ExtractionInProgress:     false,
		},
	}
}

// SetAgentManager sets the agent manager for extraction
func (e *Extractor) SetAgentManager(manager *agent.Manager) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.agentManager = manager
}

// ShouldExtract determines if memory extraction should be triggered
func (e *Extractor) ShouldExtract(currentTokenCount int, toolCallsSinceLastExtraction int) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if !e.config.Enabled {
		return false
	}

	if e.state.ExtractionInProgress {
		return false
	}

	// First extraction: wait for initialization threshold
	if !e.state.Initialized {
		return currentTokenCount >= e.config.InitializationThreshold
	}

	// Subsequent extractions: check both token and tool call thresholds
	tokensSinceLastExtraction := currentTokenCount - e.state.LastExtractionTokenCount
	return tokensSinceLastExtraction >= e.config.MinimumTokensBetweenUpdate &&
		toolCallsSinceLastExtraction >= e.config.ToolCallsBetweenUpdates
}

// MarkExtractionStart marks the beginning of an extraction
func (e *Extractor) MarkExtractionStart() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.state.ExtractionInProgress = true
}

// MarkExtractionComplete marks the completion of an extraction
func (e *Extractor) MarkExtractionComplete(currentTokenCount int, messageID string) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.state.Initialized = true
	e.state.ExtractionInProgress = false
	e.state.LastExtractionTokenCount = currentTokenCount
	e.state.LastSummarizedMessageID = messageID
	e.state.LastExtractionTime = time.Now()
}

// ExtractMemories performs memory extraction using a forked agent
func (e *Extractor) ExtractMemories(ctx context.Context, conversationHistory string) (*ExtractionResult, error) {
	e.MarkExtractionStart()
	defer func() {
		// Ensure we clear the in-progress flag even on error
		e.mu.Lock()
		e.state.ExtractionInProgress = false
		e.mu.Unlock()
	}()

	// Check if agent manager is available
	e.mu.RLock()
	manager := e.agentManager
	e.mu.RUnlock()

	if manager == nil {
		return &ExtractionResult{
			Success: false,
			Error:   "agent manager not set",
		}, fmt.Errorf("agent manager not set")
	}

	// Build extraction prompt
	prompt := buildExtractionPrompt(conversationHistory)

	// Run extraction agent
	result, err := manager.RunAgentByType(ctx, "general-purpose", prompt, "claude-sonnet-4")
	if err != nil {
		return &ExtractionResult{
			Success: false,
			Error:   fmt.Sprintf("agent execution failed: %v", err),
		}, err
	}

	if !result.Success {
		return &ExtractionResult{
			Success: false,
			Error:   fmt.Sprintf("agent failed: %v", result.Error),
		}, result.Error
	}

	// Parse agent response to extract memories
	memories, err := e.parseExtractionResponse(result)
	if err != nil {
		return &ExtractionResult{
			Success: false,
			Error:   fmt.Sprintf("failed to parse response: %v", err),
		}, err
	}

	// Save extracted memories
	savedCount := 0
	for _, mem := range memories {
		if err := e.storage.SaveMemory(mem); err != nil {
			// Log error but continue with other memories
			continue
		}
		savedCount++
	}

	return &ExtractionResult{
		Success:        true,
		MemoriesFound:  len(memories),
		MemoriesSaved:  savedCount,
		AgentTurnCount: result.TurnCount,
	}, nil
}

// buildExtractionPrompt creates the prompt for memory extraction
func buildExtractionPrompt(conversationHistory string) string {
	return fmt.Sprintf(`Analyze the following conversation and extract important memories.

Conversation:
%s

Extract memories in the following categories:
1. **user** - Information about the user's role, preferences, responsibilities, or knowledge
2. **feedback** - Guidance about how to approach work (what to avoid or keep doing)
3. **project** - Information about ongoing work, goals, initiatives, bugs, or incidents
4. **reference** - Pointers to where information can be found in external systems

For each memory, provide:
- name: Short identifier (e.g., "user_role", "feedback_testing")
- description: One-line description
- type: One of: user, feedback, project, reference
- content: The actual memory content

Output your response as a JSON array of memory objects:
[
  {
    "name": "memory_name",
    "description": "Brief description",
    "type": "user",
    "content": "Memory content here"
  }
]

Only extract memories that are:
- Non-obvious (not derivable from code)
- Persistent (relevant beyond this conversation)
- Actionable (will inform future behavior)

If no significant memories are found, return an empty array: []`, conversationHistory)
}

// parseExtractionResponse parses the agent's response to extract memories
func (e *Extractor) parseExtractionResponse(result *agent.AgentResult) ([]*Memory, error) {
	if result.FinalMessage == nil {
		return nil, fmt.Errorf("no final message from agent")
	}

	// Extract text content from final message
	var responseText string
	for _, block := range result.FinalMessage.Content {
		if block.Type == "text" {
			responseText += block.Text
		}
	}

	if responseText == "" {
		return nil, fmt.Errorf("no text content in response")
	}

	// Try to find JSON array in response
	startIdx := strings.Index(responseText, "[")
	endIdx := strings.LastIndex(responseText, "]")

	if startIdx == -1 || endIdx == -1 || startIdx >= endIdx {
		// No JSON found, return empty
		return []*Memory{}, nil
	}

	jsonStr := responseText[startIdx : endIdx+1]

	// Parse JSON
	var rawMemories []struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Type        string `json:"type"`
		Content     string `json:"content"`
	}

	if err := json.Unmarshal([]byte(jsonStr), &rawMemories); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	// Convert to Memory objects
	memories := make([]*Memory, 0, len(rawMemories))
	for _, raw := range rawMemories {
		// Validate type
		memType := MemoryType(raw.Type)
		if memType != MemoryTypeUser && memType != MemoryTypeFeedback &&
		   memType != MemoryTypeProject && memType != MemoryTypeReference {
			continue // Skip invalid types
		}

		now := time.Now()
		memories = append(memories, &Memory{
			Name:        raw.Name,
			Description: raw.Description,
			Type:        memType,
			Content:     raw.Content,
			CreatedAt:   now,
			UpdatedAt:   now,
		})
	}

	return memories, nil
}

// GetState returns the current extraction state
func (e *Extractor) GetState() ExtractionState {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.state
}

// GetStorage returns the storage instance
func (e *Extractor) GetStorage() *Storage {
	return e.storage
}
