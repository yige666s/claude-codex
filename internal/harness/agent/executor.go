package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"claude-codex/internal/harness/anthropic"
	"claude-codex/internal/harness/tools"
)

// Executor handles agent execution
type Executor struct {
	client       *anthropic.Client
	toolRegistry *tools.Registry
	instances    map[AgentID]*AgentInstance
	mu           sync.RWMutex

	// Progress tracking
	progressListeners []ProgressListener
}

// ProgressListener receives progress updates from agents
type ProgressListener func(update ProgressUpdate)

// NewExecutor creates a new agent executor
func NewExecutor(client *anthropic.Client) *Executor {
	return &Executor{
		client:            client,
		toolRegistry:      nil, // Will be set via SetToolRegistry
		instances:         make(map[AgentID]*AgentInstance),
		progressListeners: []ProgressListener{},
	}
}

// SetToolRegistry sets the tool registry for the executor
func (e *Executor) SetToolRegistry(registry *tools.Registry) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.toolRegistry = registry
}

// AddProgressListener registers a progress listener
func (e *Executor) AddProgressListener(listener ProgressListener) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.progressListeners = append(e.progressListeners, listener)
}

// notifyProgress sends a progress update to all listeners
func (e *Executor) notifyProgress(update ProgressUpdate) {
	e.mu.RLock()
	listeners := make([]ProgressListener, len(e.progressListeners))
	copy(listeners, e.progressListeners)
	e.mu.RUnlock()

	for _, listener := range listeners {
		listener(update)
	}
}

// Execute runs an agent with the given configuration
func (e *Executor) Execute(ctx context.Context, config AgentConfig) (*AgentResult, error) {
	// Create agent instance
	instance := e.createInstance(config)

	// Register instance
	e.mu.Lock()
	e.instances[instance.ID] = instance
	e.mu.Unlock()

	// Cleanup on exit
	defer func() {
		e.mu.Lock()
		delete(e.instances, instance.ID)
		e.mu.Unlock()
	}()

	// Notify start
	e.notifyProgress(ProgressUpdate{
		AgentID:    instance.ID,
		TurnNumber: 0,
		Status:     StatusStarting,
		Summary:    fmt.Sprintf("Starting agent: %s", instance.Type),
		Timestamp:  time.Now(),
	})

	// Update status
	instance.Status = StatusRunning

	// Execute agent loop
	result, err := e.executeLoop(ctx, instance, config)

	// Update final status
	if err != nil {
		instance.Status = StatusFailed
	} else {
		instance.Status = StatusCompleted
	}

	now := time.Now()
	instance.EndTime = &now

	return result, err
}

// createInstance creates a new agent instance from config
func (e *Executor) createInstance(config AgentConfig) *AgentInstance {
	agentID := AgentID(generateAgentID())

	// Resolve model
	model := config.ParentModel
	if config.Definition.Model != ModelInherit {
		model = string(config.Definition.Model)
	}

	// Resolve max turns
	maxTurns := config.Definition.MaxTurns
	if config.MaxTurns != nil {
		maxTurns = *config.MaxTurns
	}
	if maxTurns == 0 {
		maxTurns = 200 // Default
	}

	instance := &AgentInstance{
		ID:         agentID,
		Type:       config.Definition.AgentType,
		ParentID:   config.ParentID,
		Model:      model,
		StartTime:  time.Now(),
		Status:     StatusStarting,
		TurnCount:  0,
		MaxTurns:   maxTurns,
		WorkingDir: config.WorkingDir,
		Tools:      config.Definition.Tools,
		Messages:   []Message{},
	}

	// If fork, inherit parent messages
	if config.IsFork && config.InheritContext {
		instance.Messages = append(instance.Messages, config.ParentMessages...)
	}

	return instance
}

// executeLoop runs the main agent conversation loop
func (e *Executor) executeLoop(ctx context.Context, instance *AgentInstance, config AgentConfig) (*AgentResult, error) {
	startTime := time.Now()

	// Build initial messages
	messages := instance.Messages
	if config.InitialPrompt != "" {
		messages = append(messages, Message{
			ID:        generateMessageID(),
			Role:      "user",
			Content:   []ContentBlock{{Type: "text", Text: config.InitialPrompt}},
			Timestamp: time.Now(),
		})
	}

	// Build system prompt
	systemPrompt := config.Definition.SystemPrompt
	if config.SystemPrompt != nil {
		systemPrompt = *config.SystemPrompt
	}

	// Build tool list for API
	var apiTools []anthropic.Tool
	if e.toolRegistry != nil {
		apiTools = e.buildAPIToolsForDefinition(config.Definition, config.Definition.Background, false)
	}

	var lastMessage *Message

	// Main conversation loop
	for instance.TurnCount < instance.MaxTurns {
		// Check context cancellation
		select {
		case <-ctx.Done():
			instance.Status = StatusAborted
			return &AgentResult{
				AgentID:   instance.ID,
				Success:   false,
				Error:     ctx.Err(),
				TurnCount: instance.TurnCount,
				Duration:  time.Since(startTime),
			}, ctx.Err()
		default:
		}

		// Convert messages to API format
		apiMessages := e.convertMessagesToAPI(messages)

		// Determine if we should use streaming
		useStreaming := config.StreamCallback != nil

		var resp *anthropic.MessageResponse
		var err error

		if useStreaming {
			// Use streaming API
			resp, err = e.executeWithStreaming(ctx, instance, systemPrompt, apiMessages, apiTools, config.StreamCallback)
		} else {
			// Use non-streaming API
			req := anthropic.MessageRequest{
				Model:     instance.Model,
				MaxTokens: 8192,
				System:    systemPrompt,
				Messages:  apiMessages,
				Tools:     apiTools,
				Stream:    false,
			}

			resp, err = e.client.CreateMessage(ctx, req)
		}

		if err != nil {
			return &AgentResult{
				AgentID:   instance.ID,
				Success:   false,
				Error:     fmt.Errorf("API call failed: %w", err),
				TurnCount: instance.TurnCount,
				Duration:  time.Since(startTime),
			}, err
		}

		// Convert response to message
		assistantMsg := e.convertAPIResponse(resp)
		messages = append(messages, assistantMsg)
		lastMessage = &assistantMsg

		instance.TurnCount++

		// Notify progress
		e.notifyProgress(ProgressUpdate{
			AgentID:    instance.ID,
			TurnNumber: instance.TurnCount,
			Status:     StatusRunning,
			Summary:    fmt.Sprintf("Turn %d/%d", instance.TurnCount, instance.MaxTurns),
			Timestamp:  time.Now(),
		})

		// Check stop reason
		if resp.StopReason == "end_turn" || resp.StopReason == "stop_sequence" {
			break
		}

		// If there are tool uses, execute them and continue
		toolUseBlocks := e.extractToolUseBlocks(assistantMsg.Content)
		if len(toolUseBlocks) == 0 {
			// No tool uses, conversation is done
			break
		}

		// Execute tools and create tool results
		toolResults, err := e.executeTools(ctx, toolUseBlocks)
		if err != nil {
			return &AgentResult{
				AgentID:   instance.ID,
				Success:   false,
				Error:     fmt.Errorf("tool execution failed: %w", err),
				TurnCount: instance.TurnCount,
				Duration:  time.Since(startTime),
			}, err
		}

		// Add user message with tool results
		userMsg := Message{
			ID:        generateMessageID(),
			Role:      "user",
			Content:   toolResults,
			Timestamp: time.Now(),
		}
		messages = append(messages, userMsg)
	}

	return &AgentResult{
		AgentID:      instance.ID,
		Success:      true,
		TurnCount:    instance.TurnCount,
		Duration:     time.Since(startTime),
		FinalMessage: lastMessage,
	}, nil
}

// convertMessagesToAPI converts internal messages to API format
func (e *Executor) convertMessagesToAPI(messages []Message) []anthropic.InputMessage {
	apiMessages := make([]anthropic.InputMessage, 0, len(messages))

	for _, msg := range messages {
		contentBlocks := make([]anthropic.ContentBlock, len(msg.Content))
		for i, block := range msg.Content {
			cb := anthropic.ContentBlock{
				Type: block.Type,
				Text: block.Text,
			}
			// Handle tool_use blocks (from assistant)
			if block.Type == "tool_use" {
				cb.ID = block.ToolID
				cb.Name = block.ToolName
				cb.Input = block.ToolInput.(json.RawMessage)
			}
			// Handle tool_result blocks (from user)
			if block.Type == "tool_result" {
				cb.ToolUseID = block.ToolUseID
				if resultStr, ok := block.Result.(string); ok {
					cb.Content = resultStr
				} else {
					// Marshal to JSON string
					data, _ := json.Marshal(block.Result)
					cb.Content = string(data)
				}
			}
			contentBlocks[i] = cb
		}

		apiMessages = append(apiMessages, anthropic.InputMessage{
			Role:    msg.Role,
			Content: contentBlocks,
		})
	}

	return apiMessages
}

// convertAPIResponse converts API response to internal message
func (e *Executor) convertAPIResponse(resp *anthropic.MessageResponse) Message {
	contentBlocks := make([]ContentBlock, len(resp.Content))
	for i, block := range resp.Content {
		cb := ContentBlock{
			Type: block.Type,
			Text: block.Text,
		}
		// Handle tool_use blocks
		if block.Type == "tool_use" {
			cb.ToolID = block.ID
			cb.ToolName = block.Name
			cb.ToolInput = block.Input
		}
		contentBlocks[i] = cb
	}

	return Message{
		ID:        generateMessageID(),
		Role:      resp.Role,
		Content:   contentBlocks,
		Timestamp: time.Now(),
	}
}

// buildAPITools converts tool definitions to API format
func (e *Executor) buildAPITools(toolNames []string) []anthropic.Tool {
	if e.toolRegistry == nil {
		return nil
	}

	// Handle wildcard
	if len(toolNames) == 1 && toolNames[0] == "*" {
		descriptors := e.toolRegistry.Descriptors()
		apiTools := make([]anthropic.Tool, len(descriptors))
		for i, desc := range descriptors {
			apiTools[i] = anthropic.Tool{
				Name:        desc.Name,
				Description: desc.Description,
				InputSchema: desc.InputSchema,
			}
		}
		return apiTools
	}

	// Build specific tools
	apiTools := make([]anthropic.Tool, 0, len(toolNames))
	for _, name := range toolNames {
		tool, err := e.toolRegistry.Get(name)
		if err != nil {
			continue // Skip unavailable tools
		}
		apiTools = append(apiTools, anthropic.Tool{
			Name:        tool.Name(),
			Description: tool.Description(),
			InputSchema: tool.InputSchema(),
		})
	}
	return apiTools
}

func (e *Executor) buildAPIToolsForDefinition(def *AgentDefinition, isAsync bool, isMainThread bool) []anthropic.Tool {
	if e.toolRegistry == nil || def == nil {
		return nil
	}
	descriptors := e.toolRegistry.Descriptors()
	available := make([]string, 0, len(descriptors))
	for _, desc := range descriptors {
		available = append(available, desc.Name)
	}
	resolved := resolveAgentTools(def, available, isAsync, isMainThread)
	return e.buildAPITools(resolved.ResolvedTools)
}

// extractToolUseBlocks extracts tool_use blocks from content
func (e *Executor) extractToolUseBlocks(content []ContentBlock) []ContentBlock {
	var toolUses []ContentBlock
	for _, block := range content {
		if block.Type == "tool_use" {
			toolUses = append(toolUses, block)
		}
	}
	return toolUses
}

// executeTools executes tool_use blocks and returns tool_result blocks
func (e *Executor) executeTools(ctx context.Context, toolUseBlocks []ContentBlock) ([]ContentBlock, error) {
	if e.toolRegistry == nil {
		return nil, fmt.Errorf("tool registry not set")
	}

	results := make([]ContentBlock, len(toolUseBlocks))
	for i, toolUse := range toolUseBlocks {
		// Get tool from registry
		tool, err := e.toolRegistry.Get(toolUse.ToolName)
		if err != nil {
			// Tool not found, return error result
			results[i] = ContentBlock{
				Type:      "tool_result",
				ToolUseID: toolUse.ToolID,
				Result:    fmt.Sprintf("Error: %v", err),
				IsError:   true,
			}
			continue
		}

		// Convert input to JSON
		var inputJSON json.RawMessage
		switch v := toolUse.ToolInput.(type) {
		case json.RawMessage:
			inputJSON = v
		case []byte:
			inputJSON = v
		default:
			// Marshal to JSON
			data, err := json.Marshal(v)
			if err != nil {
				results[i] = ContentBlock{
					Type:      "tool_result",
					ToolUseID: toolUse.ToolID,
					Result:    fmt.Sprintf("Error marshaling input: %v", err),
					IsError:   true,
				}
				continue
			}
			inputJSON = data
		}

		// Execute tool
		result, err := tool.Execute(ctx, inputJSON)
		if err != nil {
			results[i] = ContentBlock{
				Type:      "tool_result",
				ToolUseID: toolUse.ToolID,
				Result:    fmt.Sprintf("Error: %v", err),
				IsError:   true,
			}
			continue
		}

		// Success result
		results[i] = ContentBlock{
			Type:      "tool_result",
			ToolUseID: toolUse.ToolID,
			Result:    result.Output,
			IsError:   false,
		}
	}

	return results, nil
}

// GetInstance returns an agent instance by ID
func (e *Executor) GetInstance(id AgentID) (*AgentInstance, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	instance, ok := e.instances[id]
	return instance, ok
}

// ListInstances returns all active agent instances
func (e *Executor) ListInstances() []*AgentInstance {
	e.mu.RLock()
	defer e.mu.RUnlock()

	instances := make([]*AgentInstance, 0, len(e.instances))
	for _, instance := range e.instances {
		instances = append(instances, instance)
	}
	return instances
}

// Abort stops a running agent
func (e *Executor) Abort(id AgentID) error {
	e.mu.Lock()
	instance, ok := e.instances[id]
	e.mu.Unlock()

	if !ok {
		return fmt.Errorf("agent not found: %s", id)
	}

	if instance.AbortSignal != nil {
		instance.AbortSignal()
	}

	instance.Status = StatusAborted
	return nil
}

// Helper functions
func generateAgentID() string {
	return fmt.Sprintf("agent-%d", time.Now().UnixNano())
}

func generateMessageID() string {
	return fmt.Sprintf("msg-%d", time.Now().UnixNano())
}

// executeWithStreaming executes an API call with streaming enabled
func (e *Executor) executeWithStreaming(
	ctx context.Context,
	instance *AgentInstance,
	systemPrompt string,
	apiMessages []anthropic.InputMessage,
	apiTools []anthropic.Tool,
	callback StreamCallback,
) (*anthropic.MessageResponse, error) {
	req := anthropic.MessageRequest{
		Model:     instance.Model,
		MaxTokens: 8192,
		System:    systemPrompt,
		Messages:  apiMessages,
		Tools:     apiTools,
		Stream:    true,
	}

	events, errs := e.client.StreamMessages(ctx, req)

	// Accumulate response
	var responseID string
	var responseModel string
	var responseRole string
	var stopReason string
	var contentBlocks []anthropic.ContentBlock
	var currentTextBlock *anthropic.ContentBlock
	var currentToolUse *anthropic.ContentBlock

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case err := <-errs:
			if err != nil {
				return nil, err
			}
		case event, ok := <-events:
			if !ok {
				// Stream closed
				goto done
			}

			// Parse event data
			switch event.Event {
			case "message_start":
				var data struct {
					Message struct {
						ID    string `json:"id"`
						Model string `json:"model"`
						Role  string `json:"role"`
					} `json:"message"`
				}
				if err := json.Unmarshal(event.Data, &data); err == nil {
					responseID = data.Message.ID
					responseModel = data.Message.Model
					responseRole = data.Message.Role
				}

			case "content_block_start":
				var data struct {
					Index        int                    `json:"index"`
					ContentBlock anthropic.ContentBlock `json:"content_block"`
				}
				if err := json.Unmarshal(event.Data, &data); err == nil {
					if data.ContentBlock.Type == "text" {
						currentTextBlock = &anthropic.ContentBlock{
							Type: "text",
							Text: "",
						}
						contentBlocks = append(contentBlocks, *currentTextBlock)
						currentTextBlock = &contentBlocks[len(contentBlocks)-1]
					} else if data.ContentBlock.Type == "tool_use" {
						currentToolUse = &anthropic.ContentBlock{
							Type:  "tool_use",
							ID:    data.ContentBlock.ID,
							Name:  data.ContentBlock.Name,
							Input: json.RawMessage("{}"),
						}
						contentBlocks = append(contentBlocks, *currentToolUse)
						currentToolUse = &contentBlocks[len(contentBlocks)-1]

						// Notify callback
						if callback != nil {
							callback(StreamEvent{
								Type:      "tool_use_start",
								ToolName:  data.ContentBlock.Name,
								ToolID:    data.ContentBlock.ID,
								Timestamp: time.Now(),
							})
						}
					}
				}

			case "content_block_delta":
				var data struct {
					Index int `json:"index"`
					Delta struct {
						Type        string `json:"type"`
						Text        string `json:"text,omitempty"`
						PartialJSON string `json:"partial_json,omitempty"`
					} `json:"delta"`
				}
				if err := json.Unmarshal(event.Data, &data); err == nil {
					if data.Delta.Type == "text_delta" && currentTextBlock != nil {
						currentTextBlock.Text += data.Delta.Text

						// Notify callback
						if callback != nil {
							callback(StreamEvent{
								Type:      "text_delta",
								Content:   data.Delta.Text,
								Timestamp: time.Now(),
							})
						}
					} else if data.Delta.Type == "input_json_delta" && currentToolUse != nil {
						// Accumulate tool input JSON
						var currentInput string
						if len(currentToolUse.Input) > 0 {
							currentInput = string(currentToolUse.Input)
						}
						currentInput += data.Delta.PartialJSON
						currentToolUse.Input = json.RawMessage(currentInput)
					}
				}

			case "content_block_stop":
				if currentToolUse != nil {
					// Notify callback
					if callback != nil {
						callback(StreamEvent{
							Type:      "tool_use_end",
							ToolName:  currentToolUse.Name,
							ToolID:    currentToolUse.ID,
							Timestamp: time.Now(),
						})
					}
				}
				currentTextBlock = nil
				currentToolUse = nil

			case "message_delta":
				var data struct {
					Delta struct {
						StopReason string `json:"stop_reason,omitempty"`
					} `json:"delta"`
				}
				if err := json.Unmarshal(event.Data, &data); err == nil {
					if data.Delta.StopReason != "" {
						stopReason = data.Delta.StopReason
					}
				}

			case "message_stop":
				goto done
			}
		}
	}

done:
	// Build final response
	return &anthropic.MessageResponse{
		ID:         responseID,
		Model:      responseModel,
		Role:       responseRole,
		Content:    contentBlocks,
		StopReason: stopReason,
	}, nil
}
