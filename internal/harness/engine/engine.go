package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	coreagent "claude-codex/internal/harness/agent"
	"claude-codex/internal/harness/messages"
	"claude-codex/internal/harness/permissions"
	"claude-codex/internal/harness/skills"
	"claude-codex/internal/harness/state"
	"claude-codex/internal/harness/telemetry"
	toolkit "claude-codex/internal/harness/tools"
	bashtool "claude-codex/internal/harness/tools/bash"
	skilltool "claude-codex/internal/harness/tools/skill"
	publictypes "claude-codex/internal/public/types"
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
	telemetryTracer     telemetry.SessionTracer
	runner              engineRuntime
	pendingProviders    []func(context.Context) []string
}

type Result struct {
	Output  string
	Session *state.Session
}

type StreamingPlanner interface {
	Planner
	StreamNext(ctx context.Context, session *state.Session, tools []toolkit.Descriptor, onChunk func(string)) (Plan, error)
}

func New(planner Planner, registry *toolkit.Registry, checker *permissions.Checker, maxTurns int) *Engine {
	if maxTurns < 0 {
		maxTurns = 0
	}
	engine := &Engine{
		planner:     planner,
		registry:    registry,
		permissions: checker,
		maxTurns:    maxTurns,
	}
	engine.runner = newQueryRuntime(engine)
	return engine
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

func (e *Engine) SetTelemetryTracer(tracer telemetry.SessionTracer) {
	if tracer == nil {
		e.telemetryTracer = telemetry.NoopSessionTracer{}
		return
	}
	e.telemetryTracer = tracer
}

// SetPendingMessageProvider configures a drain function for follow-up
// instructions queued while this engine is running as a background agent.
func (e *Engine) SetPendingMessageProvider(provider func(context.Context) []string) {
	if provider == nil {
		e.pendingProviders = nil
		return
	}
	e.pendingProviders = []func(context.Context) []string{wrapAgentFollowUps(provider)}
}

// AddPendingMessageProvider appends an independent source of synthetic
// user-role messages, such as coordinator task notifications.
func (e *Engine) AddPendingMessageProvider(provider func(context.Context) []string) {
	if provider == nil {
		return
	}
	e.pendingProviders = append(e.pendingProviders, provider)
}

func wrapAgentFollowUps(provider func(context.Context) []string) func(context.Context) []string {
	return func(ctx context.Context) []string {
		messages := provider(ctx)
		if len(messages) == 0 {
			return nil
		}
		out := make([]string, 0, len(messages))
		for _, message := range messages {
			message = strings.TrimSpace(message)
			if message == "" {
				continue
			}
			out = append(out, "<agent-follow-up>\n"+message+"\n</agent-follow-up>")
		}
		return out
	}
}

// SetSkillManager sets the skill manager for the engine
func (e *Engine) SetSkillManager(sm *skills.SkillManager) {
	e.skillManager = sm
}

// UseLegacyRuntime keeps Engine on the existing planner-driven runtime.
func (e *Engine) UseLegacyRuntime() {
	e.runner = newLegacyRuntime(e)
}

// UseQueryRuntime switches Engine onto the TS-aligned queryengine -> query chain.
// This is opt-in while the query runtime is still being brought to feature parity.
func (e *Engine) UseQueryRuntime() {
	e.runner = newQueryRuntime(e)
}

func (e *Engine) Descriptors() []toolkit.Descriptor {
	return e.runner.Descriptors()
}

func (e *Engine) ExecuteTool(ctx context.Context, name string, input json.RawMessage) (toolkit.Result, error) {
	return e.runner.ExecuteTool(ctx, name, input)
}

func (e *Engine) Run(ctx context.Context, session *state.Session, prompt string) (Result, error) {
	return e.runner.Run(ctx, session, prompt, true)
}

func (e *Engine) RunContent(ctx context.Context, session *state.Session, prompt []publictypes.ContentBlock) (Result, error) {
	return e.runner.Run(ctx, session, prompt, true)
}

func (e *Engine) RunGeneratedPrompt(ctx context.Context, session *state.Session, prompt string) (Result, error) {
	return e.runner.Run(ctx, session, prompt, false)
}

func (e *Engine) RunStream(ctx context.Context, session *state.Session, prompt string, onToken func(string)) (Result, error) {
	return e.runStream(ctx, session, prompt, true, onToken)
}

func (e *Engine) RunGeneratedPromptStream(ctx context.Context, session *state.Session, prompt string, onToken func(string)) (Result, error) {
	return e.runStream(ctx, session, prompt, false, onToken)
}

func (e *Engine) runStream(ctx context.Context, session *state.Session, prompt string, recordUserMessage bool, onToken func(string)) (Result, error) {
	streamingPlanner, ok := e.planner.(StreamingPlanner)
	if !ok {
		if recordUserMessage {
			return e.Run(ctx, session, prompt)
		}
		return e.RunGeneratedPrompt(ctx, session, prompt)
	}
	if session == nil {
		return Result{}, fmt.Errorf("session is required")
	}
	interactionID := fmt.Sprintf("interaction-%d", time.Now().UnixNano())
	e.recordTrace(session.ID, "interaction.start", "interaction", map[string]any{
		"span_id":       interactionID,
		"prompt":        prompt,
		"prompt_length": len(prompt),
		"prompt_source": promptSource(recordUserMessage),
		"working_dir":   session.WorkingDir,
		"runtime":       "streaming",
	})

	if recordUserMessage {
		e.ensureInitialModelContext(session)
	}

	if recordUserMessage {
		if last := session.LastUserMessage(); last != prompt {
			session.AddUserMessage(prompt)
		}
	} else if strings.TrimSpace(prompt) != "" {
		session.AddSystemContext(prompt)
	}

	var output strings.Builder
	for turn := 0; e.maxTurns <= 0 || turn < e.maxTurns; turn++ {
		e.injectPendingMessages(ctx, session)
		turnSpanID := fmt.Sprintf("%s:turn:%d", interactionID, turn)
		e.recordTrace(session.ID, "planner.turn.start", "planner", map[string]any{
			"span_id": turnSpanID,
			"turn":    turn,
			"tools":   len(e.registry.Descriptors()),
		})
		plan, err := streamingPlanner.StreamNext(ctx, session, e.registry.Descriptors(), func(token string) {
			if token == "" {
				return
			}
			output.WriteString(token)
			if onToken != nil {
				onToken(token)
			}
		})
		if err != nil {
			e.recordTrace(session.ID, "interaction.end", "interaction", map[string]any{
				"span_id": interactionID,
				"status":  "error",
				"error":   err.Error(),
				"runtime": "streaming",
			})
			return Result{}, err
		}
		e.recordTrace(session.ID, "planner.turn.end", "planner", map[string]any{
			"span_id":         turnSpanID,
			"turn":            turn,
			"status":          "ok",
			"tool_call_count": len(plan.ToolCalls),
			"assistant_chars": len(plan.AssistantText),
			"stop_reason":     plan.StopReason,
		})

		if len(plan.ToolCalls) == 0 {
			if plan.AssistantText != "" {
				session.AddAssistantMessage(plan.AssistantText)
			}
			e.recordTrace(session.ID, "interaction.end", "interaction", map[string]any{
				"span_id":         interactionID,
				"status":          "ok",
				"tool_call_count": 0,
				"output_chars":    len(plan.AssistantText),
				"runtime":         "streaming",
			})
			return Result{Output: plan.AssistantText, Session: session}, nil
		}

		stateToolCalls := make([]state.ToolCall, len(plan.ToolCalls))
		for i, tc := range plan.ToolCalls {
			stateToolCalls[i] = state.ToolCall{ID: tc.ID, Name: tc.Name, Input: tc.Input, ThoughtSignature: tc.ThoughtSignature}
		}
		session.AddAssistantMessageWithTools(plan.AssistantText, stateToolCalls)
		if err := e.executeToolCalls(ctx, session, plan.ToolCalls, interactionID); err != nil {
			return Result{Output: output.String(), Session: session}, err
		}
	}
	return Result{}, fmt.Errorf("planner exceeded max turns (%d)", e.maxTurns)
}

func toolFailureMessage(call ToolCall, output string) state.Message {
	return state.Message{
		Role:       "tool",
		ToolCallID: call.ID,
		ToolName:   call.Name,
		ToolInput:  call.Input,
		ToolOutput: output,
	}
}

func formatToolExecutionError(call ToolCall, err error) string {
	if err == nil {
		return ""
	}
	if call.Name == "" {
		return err.Error()
	}
	return fmt.Sprintf("%s: %v", call.Name, err)
}

func contextWithSessionAgent(ctx context.Context, session *state.Session) context.Context {
	if session == nil {
		return ctx
	}
	if current, ok := coreagent.AgentContextFrom(ctx); ok {
		if current.ParentSessionID == "" {
			current.ParentSessionID = session.ID
		}
		if current.SessionMetadata == nil {
			current.SessionMetadata = cloneSessionMetadata(session.Metadata)
		}
		if len(current.RecentMessages) == 0 {
			current.RecentMessages = recentSessionMessages(session, 12)
		}
		return coreagent.WithAgentContext(ctx, current)
	}
	return coreagent.WithAgentContext(ctx, coreagent.AgentContext{
		AgentID:         session.AgentID,
		ParentSessionID: session.ID,
		InvocationKind:  coreagent.InvocationMain,
		SessionMetadata: cloneSessionMetadata(session.Metadata),
		RecentMessages:  recentSessionMessages(session, 12),
	})
}

func cloneSessionMetadata(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func recentSessionMessages(session *state.Session, limit int) []string {
	if session == nil || limit <= 0 {
		return nil
	}
	start := len(session.Messages) - limit
	if start < 0 {
		start = 0
	}
	out := make([]string, 0, len(session.Messages)-start)
	for _, message := range session.Messages[start:] {
		if strings.TrimSpace(message.Content) == "" {
			continue
		}
		role := message.Role
		if role == "" {
			role = "message"
		}
		out = append(out, role+": "+strings.TrimSpace(message.Content))
	}
	return out
}

func (e *Engine) executeToolCall(
	ctx context.Context,
	session *state.Session,
	interactionID string,
	call ToolCall,
	progressReporter toolkit.ProgressReporter,
) state.Message {
	tool, err := e.registry.Get(call.Name)
	if err != nil {
		output := formatToolExecutionError(call, err)
		e.recordToolTrace(session.ID, interactionID, call, permissions.Request{}, "error", map[string]any{"error": err.Error()})
		progressReporter.Report(toolkit.ProgressEvent{
			ToolName: call.Name,
			Status:   "failed",
			Message:  err.Error(),
		})
		return toolFailureMessage(call, output)
	}

	request := buildPermissionRequest(tool.Name(), tool.Permission(), call.Input)
	if e.permissions != nil {
		if err := e.permissions.AuthorizeRequest(ctx, request); err != nil {
			output := formatToolExecutionError(call, err)
			e.recordToolTrace(session.ID, interactionID, call, request, "error", map[string]any{"error": err.Error()})
			progressReporter.Report(toolkit.ProgressEvent{
				ToolName: call.Name,
				Status:   "failed",
				Message:  err.Error(),
				Metadata: cloneStringMap(request.Metadata),
			})
			return toolFailureMessage(call, output)
		}
	}

	e.recordToolTrace(session.ID, interactionID, call, request, "start", nil)
	progressReporter.Report(toolkit.ProgressEvent{
		ToolName: call.Name,
		Status:   "started",
		Message:  request.Summary,
		Metadata: cloneStringMap(request.Metadata),
	})

	var result toolkit.Result
	toolCtx := contextWithSessionAgent(ctx, session)
	if progressTool, ok := tool.(toolkit.ProgressAwareTool); ok {
		result, err = progressTool.ExecuteWithProgress(toolCtx, call.Input, progressReporter)
	} else {
		result, err = tool.Execute(toolCtx, call.Input)
	}

	if err != nil {
		output := formatToolExecutionError(call, err)
		e.recordToolTrace(session.ID, interactionID, call, request, "error", map[string]any{"error": err.Error()})
		progressReporter.Report(toolkit.ProgressEvent{
			ToolName: call.Name,
			Status:   "failed",
			Message:  err.Error(),
			Metadata: cloneStringMap(request.Metadata),
		})
		return toolFailureMessage(call, output)
	}

	e.recordToolTrace(session.ID, interactionID, call, request, "end", map[string]any{
		"status":       "ok",
		"output_chars": len(result.Output),
	})
	progressReporter.Report(toolkit.ProgressEvent{
		ToolName: call.Name,
		Status:   "completed",
		Message:  request.Summary,
		Progress: 1.0,
		Metadata: cloneStringMap(request.Metadata),
	})

	return state.Message{
		Role:       "tool",
		ToolCallID: call.ID,
		ToolName:   call.Name,
		ToolInput:  call.Input,
		ToolOutput: result.Output,
	}
}

func (e *Engine) executeToolCalls(ctx context.Context, session *state.Session, calls []ToolCall, interactionID string) error {
	// Partition tool calls into concurrent-safe and non-concurrent-safe groups
	var safeCalls []ToolCall
	var unsafeCalls []ToolCall

	for _, call := range calls {
		tool, err := e.registry.Get(call.Name)
		if err != nil {
			unsafeCalls = append(unsafeCalls, call)
			continue
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
				idx := callIndex[call.ID]
				results[idx] = e.executeToolCall(runCtx, session, interactionID, call, progressReporter)
				return nil
			})
		}

		if err := group.Wait(); err != nil {
			return err
		}
	}

	// Execute non-concurrent-safe tools sequentially
	for _, call := range unsafeCalls {
		idx := callIndex[call.ID]
		results[idx] = e.executeToolCall(ctx, session, interactionID, call, progressReporter)
	}

	for _, message := range results {
		session.AddToolResult(message.ToolCallID, message.ToolName, message.ToolInput, message.ToolOutput)
		if message.ToolName == skilltool.ToolName && skilltool.IsRunAsJobMarker(message.ToolOutput) {
			return skilltool.ErrRunAsJobRequired
		}
	}

	return nil
}

func buildPermissionRequest(toolName string, level permissions.Level, input json.RawMessage) permissions.Request {
	request := permissions.Request{
		ToolName: toolName,
		Level:    level,
	}

	if len(input) == 0 {
		return request
	}

	var payload map[string]any
	if err := json.Unmarshal(input, &payload); err != nil {
		request.Summary = summarizeText(string(input), 120)
		return request
	}

	metadata := map[string]string{}
	switch toolName {
	case "Bash", "bash":
		if command, ok := payload["command"].(string); ok {
			request.Summary = summarizeText(command, 120)
			metadata["command"] = summarizeText(command, 240)
			if prefix := permissions.SimpleShellCommandPrefix(command); prefix != "" {
				metadata["command_prefix"] = prefix
			} else if fields := strings.Fields(command); len(fields) > 0 {
				metadata["command_prefix"] = fields[0]
			}
			analysis := bashtool.AnalyzeCommand(command)
			if bashtool.IsCommandReadOnly(command) {
				metadata["access"] = "read-only"
			} else {
				metadata["access"] = "write-or-exec"
			}
			if analysis.CompoundStructure.HasCompoundOperators {
				metadata["compound"] = strings.Join(analysis.CompoundStructure.Operators, " ")
			}
			if analysis.CompoundStructure.HasPipeline {
				metadata["pipeline"] = "true"
			}
			if analysis.DangerousPatterns.HasCommandSubstitution {
				metadata["command_substitution"] = "true"
			}
			if analysis.DangerousPatterns.HasParameterExpansion {
				metadata["parameter_expansion"] = "true"
			}
			if analysis.DangerousPatterns.HasHeredoc {
				metadata["heredoc"] = "true"
			}
			if risk := destructiveBashHint(command); risk != "" {
				metadata["risk"] = risk
			}
		}
	case "Read", "Write", "Edit", "file_read", "file_write", "file_edit":
		if path, ok := payload["file_path"].(string); ok {
			request.Summary = path
			metadata["path"] = path
			metadata["filename"] = filepath.Base(path)
		} else if path, ok := payload["path"].(string); ok {
			request.Summary = path
			metadata["path"] = path
			metadata["filename"] = filepath.Base(path)
		}
		if oldString, ok := payload["old_string"].(string); ok {
			metadata["old"] = summarizeText(oldString, 80)
		}
		if newString, ok := payload["new_string"].(string); ok {
			metadata["new"] = summarizeText(newString, 80)
		}
	case "WebFetch", "web_fetch":
		if url, ok := payload["url"].(string); ok {
			request.Summary = summarizeText(url, 120)
			metadata["url"] = url
		}
	case "Agent", "agent":
		if prompt, ok := payload["prompt"].(string); ok {
			request.Summary = summarizeText(prompt, 120)
		}
		if description, ok := payload["description"].(string); ok && strings.TrimSpace(description) != "" {
			metadata["description"] = summarizeText(description, 120)
		}
		if subagentType, ok := payload["subagent_type"].(string); ok && strings.TrimSpace(subagentType) != "" {
			metadata["subagent_type"] = subagentType
		}
		if model, ok := payload["model"].(string); ok && strings.TrimSpace(model) != "" {
			metadata["model"] = model
		}
		if teamName, ok := payload["team_name"].(string); ok && strings.TrimSpace(teamName) != "" {
			metadata["team_name"] = teamName
		}
	case "TeamCreate", "TeamDelete", "team_create", "team_delete":
		if teamName, ok := payload["name"].(string); ok && strings.TrimSpace(teamName) != "" {
			action := teamOperation(toolName)
			request.Summary = fmt.Sprintf("%s team %s", action, teamName)
			metadata["team_name"] = teamName
			metadata["operation"] = action
		}
	default:
		if strings.HasPrefix(toolName, "mcp__") {
			serverName, remoteTool := parseMCPToolName(toolName)
			if serverName != "" {
				metadata["server"] = serverName
			}
			if remoteTool != "" {
				metadata["mcp_tool"] = remoteTool
			}
			if serverName != "" || remoteTool != "" {
				request.Summary = strings.Trim(strings.Join([]string{serverName, remoteTool}, "/"), "/")
			}
			if uri, ok := payload["uri"].(string); ok && strings.TrimSpace(uri) != "" {
				metadata["uri"] = uri
			}
			if path, ok := payload["path"].(string); ok && strings.TrimSpace(path) != "" {
				metadata["path"] = path
			}
			if url, ok := payload["url"].(string); ok && strings.TrimSpace(url) != "" {
				metadata["url"] = url
			}
			break
		}
		if path, ok := payload["path"].(string); ok {
			request.Summary = path
			metadata["path"] = path
		}
	}

	if request.Summary == "" {
		if text, err := json.Marshal(payload); err == nil {
			request.Summary = summarizeText(string(text), 120)
		}
	}
	if len(metadata) > 0 {
		request.Metadata = metadata
	}
	return request
}

func summarizeText(value string, limit int) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\n", " "))
	if limit <= 0 || len(value) <= limit {
		return value
	}
	if limit <= 3 {
		return value[:limit]
	}
	return value[:limit-3] + "..."
}

func teamOperation(toolName string) string {
	switch toolName {
	case "TeamCreate", "team_create":
		return "create"
	case "TeamDelete", "team_delete":
		return "delete"
	default:
		return strings.TrimPrefix(toolName, "team_")
	}
}

func destructiveBashHint(command string) string {
	command = strings.TrimSpace(strings.ToLower(command))
	switch {
	case strings.Contains(command, "rm -rf"), strings.Contains(command, "rm -r "), strings.Contains(command, "rmdir "):
		return "destructive removal"
	case strings.Contains(command, "mv "), strings.Contains(command, "cp "):
		return "filesystem mutation"
	case strings.Contains(command, "chmod "), strings.Contains(command, "chown "):
		return "permission mutation"
	case strings.Contains(command, "git push"), strings.Contains(command, "git commit"), strings.Contains(command, "git reset"):
		return "git state mutation"
	case strings.Contains(command, "curl ") && (strings.Contains(command, "| sh") || strings.Contains(command, "| bash")):
		return "network script execution"
	case strings.Contains(command, "sudo "):
		return "privileged execution"
	default:
		return ""
	}
}

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func parseMCPToolName(toolName string) (string, string) {
	parts := strings.Split(toolName, "__")
	if len(parts) < 3 || parts[0] != "mcp" {
		return "", ""
	}
	return parts[1], strings.Join(parts[2:], "__")
}

func (e *Engine) recordTrace(sessionID string, name string, kind string, attrs map[string]any) {
	if e == nil || e.telemetryTracer == nil {
		return
	}
	telemetry.RecordEvent(e.telemetryTracer, sessionID, name, kind, attrs)
}

func (e *Engine) recordToolTrace(sessionID string, interactionID string, call ToolCall, request permissions.Request, phase string, extra map[string]any) {
	attrs := map[string]any{
		"span_id":      fmt.Sprintf("%s:tool:%s", interactionID, call.ID),
		"tool_name":    call.Name,
		"tool_call_id": call.ID,
	}
	if request.Summary != "" {
		attrs["summary"] = request.Summary
	}
	for key, value := range request.Metadata {
		attrs[key] = value
	}
	for key, value := range extra {
		attrs[key] = value
	}
	name := "tool.end"
	switch phase {
	case "start":
		name = "tool.start"
	case "error":
		name = "tool.end"
		attrs["status"] = "error"
	}
	e.recordTrace(sessionID, name, "tool", attrs)
}

func promptSource(recordUserMessage bool) string {
	if recordUserMessage {
		return "user"
	}
	return "generated"
}
