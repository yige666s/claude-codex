package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	workctx "claude-codex/internal/harness/context"
	"claude-codex/internal/harness/messages"
	"claude-codex/internal/harness/state"
	toolkit "claude-codex/internal/harness/tools"
	publictypes "claude-codex/internal/public/types"
)

type engineRuntime interface {
	Descriptors() []toolkit.Descriptor
	ExecuteTool(ctx context.Context, name string, input json.RawMessage) (toolkit.Result, error)
	Run(ctx context.Context, session *state.Session, prompt interface{}, recordUserMessage bool) (Result, error)
}

type legacyRuntime struct {
	engine *Engine
}

const (
	workspaceContextInjectedKey    = "engine.workspace_context_injected"
	skillCatalogInjectedKey        = "engine.skill_catalog_injected"
	skillCatalogInjectedVersionKey = "engine.skill_catalog_injected_version"
)

func newLegacyRuntime(engine *Engine) engineRuntime {
	return &legacyRuntime{engine: engine}
}

func (e *Engine) ensureInitialModelContext(session *state.Session) {
	if e == nil || session == nil {
		return
	}
	if session.Metadata == nil {
		session.Metadata = map[string]string{}
	}
	if e.workingDir != "" && session.Metadata[workspaceContextInjectedKey] != "true" && !sessionHasHiddenContent(session, "workspace context") {
		wsCtx := workctx.Collect(e.workingDir)
		session.AddSystemContext(wsCtx.SystemPrompt())
		session.AddHiddenAssistantMessage("Understood. I have the workspace context.")
		session.Metadata[workspaceContextInjectedKey] = "true"
	}
	if e.skillManager == nil || session.Metadata[skillCatalogInjectedVersionKey] == messages.SkillSelectionPolicyVersion || sessionHasHiddenContent(session, messages.SkillSelectionPolicyMarker()) {
		if e.skillManager != nil {
			session.Metadata[skillCatalogInjectedKey] = "true"
			session.Metadata[skillCatalogInjectedVersionKey] = messages.SkillSelectionPolicyVersion
		}
		return
	}
	allSkills := e.skillManager.ListUserInvocableSkills()
	content := messages.FormatAllSkillDescriptions(allSkills)
	if strings.TrimSpace(content) == "" {
		session.Metadata[skillCatalogInjectedKey] = "true"
		session.Metadata[skillCatalogInjectedVersionKey] = messages.SkillSelectionPolicyVersion
		return
	}
	attachment := &messages.SkillListingAttachment{
		Content:    content,
		SkillCount: len(allSkills),
		IsInitial:  true,
	}
	session.AddSystemContext(attachment.ToSystemReminder())
	session.Metadata[skillCatalogInjectedKey] = "true"
	session.Metadata[skillCatalogInjectedVersionKey] = messages.SkillSelectionPolicyVersion
}

func sessionHasHiddenContent(session *state.Session, needle string) bool {
	if session == nil || strings.TrimSpace(needle) == "" {
		return false
	}
	for _, message := range session.Messages {
		if message.Hidden && strings.Contains(message.Content, needle) {
			return true
		}
	}
	return false
}

func (r *legacyRuntime) Descriptors() []toolkit.Descriptor {
	return r.engine.registry.Descriptors()
}

func (r *legacyRuntime) ExecuteTool(ctx context.Context, name string, input json.RawMessage) (toolkit.Result, error) {
	tool, err := r.engine.registry.Get(name)
	if err != nil {
		return toolkit.Result{}, err
	}
	if r.engine.permissions != nil {
		if err := r.engine.permissions.AuthorizeRequest(ctx, buildPermissionRequest(tool.Name(), tool.Permission(), input)); err != nil {
			return toolkit.Result{}, err
		}
	}
	return tool.Execute(ctx, input)
}

func (r *legacyRuntime) Run(ctx context.Context, session *state.Session, prompt interface{}, recordUserMessage bool) (Result, error) {
	if session == nil {
		return Result{}, fmt.Errorf("session is required")
	}
	promptText := promptToText(prompt)
	interactionID := fmt.Sprintf("interaction-%d", time.Now().UnixNano())
	r.engine.recordTrace(session.ID, "interaction.start", "interaction", map[string]any{
		"span_id":       interactionID,
		"prompt":        promptText,
		"prompt_length": len(promptText),
		"prompt_source": promptSource(recordUserMessage),
		"working_dir":   session.WorkingDir,
	})

	if recordUserMessage {
		r.engine.ensureInitialModelContext(session)
	}

	if recordUserMessage {
		if last := session.LastUserMessage(); last != promptText {
			session.AddUserMessage(promptText)
		}
	} else if strings.TrimSpace(promptText) != "" {
		session.AddSystemContext(promptText)
	}

	compressionConfig := state.DefaultCompressionConfig()
	if session.NeedsCompression(compressionConfig) {
		r.engine.recordTrace(session.ID, "session.compression", "session", map[string]any{
			"span_id":  interactionID + ":compression:pre",
			"messages": len(session.Messages),
		})
		if err := session.Compress(compressionConfig); err != nil {
			r.engine.recordTrace(session.ID, "interaction.end", "interaction", map[string]any{
				"span_id": interactionID,
				"status":  "error",
				"error":   err.Error(),
			})
			return Result{}, fmt.Errorf("failed to compress session: %w", err)
		}
	}

	for turn := 0; r.engine.maxTurns <= 0 || turn < r.engine.maxTurns; turn++ {
		turnSpanID := fmt.Sprintf("%s:turn:%d", interactionID, turn)
		r.engine.recordTrace(session.ID, "planner.turn.start", "planner", map[string]any{
			"span_id": turnSpanID,
			"turn":    turn,
			"tools":   len(r.engine.registry.Descriptors()),
		})
		plan, err := r.engine.planner.Next(ctx, session, r.engine.registry.Descriptors())
		if err != nil {
			r.engine.recordTrace(session.ID, "planner.turn.end", "planner", map[string]any{
				"span_id": turnSpanID,
				"turn":    turn,
				"status":  "error",
				"error":   err.Error(),
			})
			r.engine.recordTrace(session.ID, "interaction.end", "interaction", map[string]any{
				"span_id": interactionID,
				"status":  "error",
				"error":   err.Error(),
			})
			return Result{}, err
		}
		r.engine.recordTrace(session.ID, "planner.turn.end", "planner", map[string]any{
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
			r.engine.recordTrace(session.ID, "interaction.end", "interaction", map[string]any{
				"span_id":         interactionID,
				"status":          "ok",
				"tool_call_count": 0,
				"output_chars":    len(plan.AssistantText),
			})
			return Result{
				Output:  plan.AssistantText,
				Session: session,
			}, nil
		}

		stateToolCalls := make([]state.ToolCall, len(plan.ToolCalls))
		for i, tc := range plan.ToolCalls {
			stateToolCalls[i] = state.ToolCall{
				ID:    tc.ID,
				Name:  tc.Name,
				Input: tc.Input,
			}
		}
		session.AddAssistantMessageWithTools(plan.AssistantText, stateToolCalls)

		if err := r.engine.executeToolCalls(ctx, session, plan.ToolCalls, interactionID); err != nil {
			r.engine.recordTrace(session.ID, "interaction.end", "interaction", map[string]any{
				"span_id": interactionID,
				"status":  "error",
				"error":   err.Error(),
			})
			return Result{Session: session}, err
		}

		if session.NeedsCompression(compressionConfig) {
			r.engine.recordTrace(session.ID, "session.compression", "session", map[string]any{
				"span_id":  fmt.Sprintf("%s:compression:post:%d", interactionID, turn),
				"messages": len(session.Messages),
			})
			if err := session.Compress(compressionConfig); err != nil {
				r.engine.recordTrace(session.ID, "interaction.end", "interaction", map[string]any{
					"span_id": interactionID,
					"status":  "error",
					"error":   err.Error(),
				})
				return Result{}, fmt.Errorf("failed to compress session: %w", err)
			}
		}
	}

	r.engine.recordTrace(session.ID, "interaction.end", "interaction", map[string]any{
		"span_id": interactionID,
		"status":  "error",
		"error":   fmt.Sprintf("planner exceeded max turns (%d)", r.engine.maxTurns),
	})
	return Result{}, fmt.Errorf("planner exceeded max turns (%d)", r.engine.maxTurns)
}

func promptToText(prompt interface{}) string {
	switch typed := prompt.(type) {
	case string:
		return typed
	case []publictypes.ContentBlock:
		parts := make([]string, 0, len(typed))
		for _, block := range typed {
			if block.Type == "text" && strings.TrimSpace(block.Text) != "" {
				parts = append(parts, block.Text)
			}
		}
		return strings.Join(parts, "\n")
	default:
		return fmt.Sprint(prompt)
	}
}
