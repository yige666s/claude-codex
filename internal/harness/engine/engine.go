package engine

import (
	"context"
	"encoding/json"
	"fmt"

	"golang.org/x/sync/errgroup"

	workctx "github.com/ding/claude-code/claude-go/internal/harness/context"
	"github.com/ding/claude-code/claude-go/internal/harness/messages"
	"github.com/ding/claude-code/claude-go/internal/harness/permissions"
	"github.com/ding/claude-code/claude-go/internal/harness/skills"
	"github.com/ding/claude-code/claude-go/internal/harness/state"
	toolkit "github.com/ding/claude-code/claude-go/internal/harness/tools"
)

type Engine struct {
	planner             Planner
	registry            *toolkit.Registry
	permissions         *permissions.Checker
	maxTurns            int
	workingDir          string
	skillManager        *skills.SkillManager
	skillListingManager *messages.SkillListingManager
	progressCallback    func(toolkit.ProgressEvent)
}

type Result struct {
	Output  string
	Session *state.Session
}

func New(planner Planner, registry *toolkit.Registry, checker *permissions.Checker, maxTurns int) *Engine {
	if maxTurns <= 0 {
		maxTurns = 8
	}
	return &Engine{
		planner:     planner,
		registry:    registry,
		permissions: checker,
		maxTurns:    maxTurns,
	}
}

func NewWithDir(planner Planner, registry *toolkit.Registry, checker *permissions.Checker, maxTurns int, workingDir string) *Engine {
	e := New(planner, registry, checker, maxTurns)
	e.workingDir = workingDir
	return e
}

// SetProgressCallback sets a callback to receive progress events during tool execution
func (e *Engine) SetProgressCallback(callback func(toolkit.ProgressEvent)) {
	e.progressCallback = callback
}

// SetSkillManager sets the skill manager for the engine
func (e *Engine) SetSkillManager(sm *skills.SkillManager) {
	e.skillManager = sm
}

func (e *Engine) Descriptors() []toolkit.Descriptor {
	return e.registry.Descriptors()
}

func (e *Engine) ExecuteTool(ctx context.Context, name string, input json.RawMessage) (toolkit.Result, error) {
	tool, err := e.registry.Get(name)
	if err != nil {
		return toolkit.Result{}, err
	}
	if e.permissions != nil {
		if err := e.permissions.Authorize(ctx, tool.Name(), tool.Permission()); err != nil {
			return toolkit.Result{}, err
		}
	}
	return tool.Execute(ctx, input)
}

func (e *Engine) Run(ctx context.Context, session *state.Session, prompt string) (Result, error) {
	if session == nil {
		return Result{}, fmt.Errorf("session is required")
	}

	// Inject workspace context as first system message if session is new
	if len(session.Messages) == 0 && e.workingDir != "" {
		wsCtx := workctx.Collect(e.workingDir)
		session.AddSystemContext(wsCtx.SystemPrompt())
		session.AddAssistantMessage("Understood. I have the workspace context.")

		// Inject skill listing if skill manager is available
		if e.skillManager != nil {
			if e.skillListingManager == nil {
				e.skillListingManager = messages.NewSkillListingManager()
			}

			allSkills := e.skillManager.ListUserInvocableSkills()
			attachment := e.skillListingManager.GetSkillListingAttachment(allSkills, 200000)

			if attachment != nil {
				systemReminder := attachment.ToSystemReminder()
				session.AddSystemContext(systemReminder)
			}
		}
	}

	// Only add the user message if it hasn't already been added by the caller (e.g. TUI pre-adds it for immediate display)
	if last := session.LastUserMessage(); last != prompt {
		session.AddUserMessage(prompt)
	}

	// Check if compression is needed before starting the loop
	compressionConfig := state.DefaultCompressionConfig()
	if session.NeedsCompression(compressionConfig) {
		if err := session.Compress(compressionConfig); err != nil {
			return Result{}, fmt.Errorf("failed to compress session: %w", err)
		}
	}

	for turn := 0; turn < e.maxTurns; turn++ {
		plan, err := e.planner.Next(ctx, session, e.registry.Descriptors())
		if err != nil {
			return Result{}, err
		}

		if len(plan.ToolCalls) == 0 {
			if plan.AssistantText != "" {
				session.AddAssistantMessage(plan.AssistantText)
			}
			return Result{
				Output:  plan.AssistantText,
				Session: session,
			}, nil
		}

		// Save the assistant's tool-use intent before executing so the conversation
		// history is complete when we call Next again.
		// Convert engine.ToolCall to state.ToolCall
		stateToolCalls := make([]state.ToolCall, len(plan.ToolCalls))
		for i, tc := range plan.ToolCalls {
			stateToolCalls[i] = state.ToolCall{
				ID:    tc.ID,
				Name:  tc.Name,
				Input: tc.Input,
			}
		}
		session.AddAssistantMessageWithTools(plan.AssistantText, stateToolCalls)

		if err := e.executeToolCalls(ctx, session, plan.ToolCalls); err != nil {
			return Result{}, err
		}

		// Check if compression is needed after tool execution
		if session.NeedsCompression(compressionConfig) {
			if err := session.Compress(compressionConfig); err != nil {
				return Result{}, fmt.Errorf("failed to compress session: %w", err)
			}
		}
	}

	return Result{}, fmt.Errorf("planner exceeded max turns (%d)", e.maxTurns)
}

func (e *Engine) executeToolCalls(ctx context.Context, session *state.Session, calls []ToolCall) error {
	// Partition tool calls into concurrent-safe and non-concurrent-safe groups
	var safeCalls []ToolCall
	var unsafeCalls []ToolCall

	for _, call := range calls {
		tool, err := e.registry.Get(call.Name)
		if err != nil {
			return err
		}
		if tool.IsConcurrencySafe() {
			safeCalls = append(safeCalls, call)
		} else {
			unsafeCalls = append(unsafeCalls, call)
		}
	}

	results := make([]state.Message, len(calls))
	callIndex := make(map[string]int) // map call.ID to results index
	for i, call := range calls {
		callIndex[call.ID] = i
	}

	// Create progress reporter if callback is set
	var progressCh chan toolkit.ProgressEvent
	var progressReporter toolkit.ProgressReporter = toolkit.NoOpProgressReporter{}
	if e.progressCallback != nil {
		progressCh = make(chan toolkit.ProgressEvent, 100)
		progressReporter = toolkit.NewChannelProgressReporter(progressCh)

		// Start goroutine to forward progress events to callback
		go func() {
			for event := range progressCh {
				e.progressCallback(event)
			}
		}()
		defer close(progressCh)
	}

	// Execute concurrent-safe tools in parallel
	if len(safeCalls) > 0 {
		group, runCtx := errgroup.WithContext(ctx)
		for _, call := range safeCalls {
			call := call
			group.Go(func() error {
				tool, err := e.registry.Get(call.Name)
				if err != nil {
					return err
				}

				if e.permissions != nil {
					if err := e.permissions.Authorize(runCtx, tool.Name(), tool.Permission()); err != nil {
						return err
					}
				}

				// Report start
				progressReporter.Report(toolkit.ProgressEvent{
					ToolName: call.Name,
					Status:   "started",
				})

				var result toolkit.Result
				// Check if tool supports progress reporting
				if progressTool, ok := tool.(toolkit.ProgressAwareTool); ok {
					result, err = progressTool.ExecuteWithProgress(runCtx, call.Input, progressReporter)
				} else {
					result, err = tool.Execute(runCtx, call.Input)
				}

				if err != nil {
					progressReporter.Report(toolkit.ProgressEvent{
						ToolName: call.Name,
						Status:   "failed",
						Message:  err.Error(),
					})
					return err
				}

				progressReporter.Report(toolkit.ProgressEvent{
					ToolName: call.Name,
					Status:   "completed",
					Progress: 1.0,
				})

				idx := callIndex[call.ID]
				results[idx] = state.Message{
					Role:       "tool",
					ToolCallID: call.ID,
					ToolName:   call.Name,
					ToolInput:  call.Input,
					ToolOutput: result.Output,
				}
				return nil
			})
		}

		if err := group.Wait(); err != nil {
			return err
		}
	}

	// Execute non-concurrent-safe tools sequentially
	for _, call := range unsafeCalls {
		tool, err := e.registry.Get(call.Name)
		if err != nil {
			return err
		}

		if e.permissions != nil {
			if err := e.permissions.Authorize(ctx, tool.Name(), tool.Permission()); err != nil {
				return err
			}
		}

		// Report start
		progressReporter.Report(toolkit.ProgressEvent{
			ToolName: call.Name,
			Status:   "started",
		})

		var result toolkit.Result
		// Check if tool supports progress reporting
		if progressTool, ok := tool.(toolkit.ProgressAwareTool); ok {
			result, err = progressTool.ExecuteWithProgress(ctx, call.Input, progressReporter)
		} else {
			result, err = tool.Execute(ctx, call.Input)
		}

		if err != nil {
			progressReporter.Report(toolkit.ProgressEvent{
				ToolName: call.Name,
				Status:   "failed",
				Message:  err.Error(),
			})
			return err
		}

		progressReporter.Report(toolkit.ProgressEvent{
			ToolName: call.Name,
			Status:   "completed",
			Progress: 1.0,
		})

		idx := callIndex[call.ID]
		results[idx] = state.Message{
			Role:       "tool",
			ToolCallID: call.ID,
			ToolName:   call.Name,
			ToolInput:  call.Input,
			ToolOutput: result.Output,
		}
	}

	for _, message := range results {
		session.AddToolResult(message.ToolCallID, message.ToolName, message.ToolInput, message.ToolOutput)
	}

	return nil
}
