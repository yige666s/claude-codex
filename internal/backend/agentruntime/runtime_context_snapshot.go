package agentruntime

import (
	"encoding/json"
	"strings"

	"claude-codex/internal/harness/state"
)

const (
	runtimeContextSnapshotVersion = "agentapi_context.v2"
	runtimeContextRecentLimit     = 6
	runtimeContextContentLimit    = 240
)

type appRuntimeContextSnapshot struct {
	Version              string                         `json:"version"`
	SessionID            string                         `json:"session_id,omitempty"`
	AgentMode            string                         `json:"agent_mode,omitempty"`
	ThinkingMode         bool                           `json:"thinking_mode,omitempty"`
	AttachmentIDs        []string                       `json:"attachment_ids,omitempty"`
	AttachmentURLs       []appRuntimeAttachmentURL      `json:"attachment_urls,omitempty"`
	RecentMessages       []appRuntimeContextMessage     `json:"recent_messages,omitempty"`
	ClientCapabilities   appRuntimeClientCapabilities   `json:"client_capabilities"`
	RuntimeState         appRuntimeState                `json:"runtime_state"`
	UserState            appRuntimeUserState            `json:"user_state"`
	QuotaState           appRuntimeQuotaState           `json:"quota_state"`
	BusinessState        map[string]any                 `json:"business_state,omitempty"`
	Extra                map[string]any                 `json:"extra,omitempty"`
	PersonalizationState appRuntimePersonalizationState `json:"personalization_state"`
}

type appRuntimeClientCapabilities struct {
	StructuredOutput bool `json:"structured_output"`
	ArtifactPreview  bool `json:"artifact_preview"`
	EventReplay      bool `json:"event_replay"`
}

type appRuntimeState struct {
	WorkingDir       string   `json:"working_dir,omitempty"`
	ConnectorContext []string `json:"connector_context,omitempty"`
}

type appRuntimeUserState struct {
	Personalization appRuntimePersonalizationState `json:"personalization"`
	MemoryFlags     appRuntimeMemoryFlags          `json:"memory_flags"`
}

type appRuntimeMemoryFlags struct {
	UseSavedMemory   bool `json:"use_saved_memory"`
	UseChatHistory   bool `json:"use_chat_history"`
	UseBrowserMemory bool `json:"use_browser_memory"`
}

type appRuntimeQuotaState struct {
	ToolWritePolicy string `json:"tool_write_policy,omitempty"`
}

type appRuntimeAttachmentURL struct {
	Filename    string `json:"filename,omitempty"`
	ContentType string `json:"content_type,omitempty"`
	URL         string `json:"url,omitempty"`
}

type appRuntimeContextMessage struct {
	Role              string `json:"role"`
	Content           string `json:"content,omitempty"`
	AttachmentCount   int    `json:"attachment_count,omitempty"`
	ContentBlockCount int    `json:"content_block_count,omitempty"`
}

type appRuntimePersonalizationState struct {
	UseSavedMemory            bool   `json:"use_saved_memory"`
	UseChatHistory            bool   `json:"use_chat_history"`
	UseBrowserMemory          bool   `json:"use_browser_memory"`
	CustomInstructionsPresent bool   `json:"custom_instructions_present"`
	Nickname                  string `json:"nickname,omitempty"`
	Occupation                string `json:"occupation,omitempty"`
	StylePreset               string `json:"style_preset,omitempty"`
}

func (r *Runtime) injectAppRuntimeContextSnapshot(req ChatRequest, session *state.Session, personalization PersonalizationSettings) {
	if r == nil || session == nil {
		return
	}
	content := formatAppRuntimeContextSnapshot(buildAppRuntimeContextSnapshot(req, session, personalization))
	if strings.TrimSpace(content) == "" {
		return
	}
	session.AddSystemContext(content)
}

func buildAppRuntimeContextSnapshot(req ChatRequest, session *state.Session, personalization PersonalizationSettings) appRuntimeContextSnapshot {
	var sessionID, workingDir string
	if session != nil {
		sessionID = session.ID
		workingDir = session.WorkingDir
	}
	personalizationState := appRuntimePersonalizationState{
		UseSavedMemory:            personalization.FeatureFlags.UseSavedMemory,
		UseChatHistory:            personalization.FeatureFlags.UseChatHistory,
		UseBrowserMemory:          personalization.FeatureFlags.UseBrowserMemory,
		CustomInstructionsPresent: strings.TrimSpace(personalization.CustomInstructions) != "",
		Nickname:                  strings.TrimSpace(personalization.Profile.Nickname),
		Occupation:                strings.TrimSpace(personalization.Profile.Occupation),
		StylePreset:               strings.TrimSpace(personalization.Style.Preset),
	}
	return appRuntimeContextSnapshot{
		Version:        runtimeContextSnapshotVersion,
		SessionID:      sessionID,
		AgentMode:      firstNonEmptyString(strings.TrimSpace(req.AgentMode), AgentModeChat),
		ThinkingMode:   req.ThinkingMode,
		AttachmentIDs:  compactStringList(req.AttachmentIDs),
		AttachmentURLs: appRuntimeAttachmentURLs(req.AttachmentURLs),
		RecentMessages: appRuntimeRecentMessages(session, runtimeContextRecentLimit),
		ClientCapabilities: appRuntimeClientCapabilities{
			StructuredOutput: true,
			ArtifactPreview:  true,
			EventReplay:      true,
		},
		RuntimeState: appRuntimeState{
			WorkingDir:       workingDir,
			ConnectorContext: normalizeConnectorScopes(req.ConnectorContext),
		},
		UserState: appRuntimeUserState{
			Personalization: personalizationState,
			MemoryFlags: appRuntimeMemoryFlags{
				UseSavedMemory:   personalization.FeatureFlags.UseSavedMemory,
				UseChatHistory:   personalization.FeatureFlags.UseChatHistory,
				UseBrowserMemory: personalization.FeatureFlags.UseBrowserMemory,
			},
		},
		QuotaState: appRuntimeQuotaState{
			ToolWritePolicy: "review",
		},
		BusinessState:        map[string]any{},
		Extra:                map[string]any{"schema_family": "agentapi_context"},
		PersonalizationState: personalizationState,
	}
}

func appRuntimeAttachmentURLs(urls []ChatAttachmentURL) []appRuntimeAttachmentURL {
	if len(urls) == 0 {
		return nil
	}
	out := make([]appRuntimeAttachmentURL, 0, len(urls))
	for _, attachment := range urls {
		url := strings.TrimSpace(attachment.URL)
		if url == "" {
			continue
		}
		out = append(out, appRuntimeAttachmentURL{
			URL:         truncateRuntimeContextString(url, 300),
			Filename:    strings.TrimSpace(attachment.Filename),
			ContentType: strings.TrimSpace(attachment.ContentType),
		})
	}
	return out
}

func appRuntimeRecentMessages(session *state.Session, limit int) []appRuntimeContextMessage {
	if session == nil || limit <= 0 {
		return nil
	}
	out := make([]appRuntimeContextMessage, 0, min(limit, len(session.Messages)))
	for i := len(session.Messages) - 1; i >= 0 && len(out) < limit; i-- {
		message := session.Messages[i]
		if message.Hidden || message.Role == state.MessageRoleTool {
			continue
		}
		out = append(out, appRuntimeContextMessage{
			Role:              strings.TrimSpace(message.Role),
			Content:           truncateRuntimeContextString(strings.TrimSpace(message.Content), runtimeContextContentLimit),
			AttachmentCount:   len(message.Attachments),
			ContentBlockCount: len(message.ContentBlocks),
		})
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

func formatAppRuntimeContextSnapshot(snapshot appRuntimeContextSnapshot) string {
	data, err := json.Marshal(snapshot)
	if err != nil {
		return ""
	}
	return appRuntimeContextMarker + "\n" + string(data) + "\n</app-runtime-context>"
}

func truncateRuntimeContextString(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || len([]rune(value)) <= limit {
		return value
	}
	runes := []rune(value)
	return string(runes[:limit]) + "..."
}
