package agentruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"claude-codex/internal/harness/skills"
	"claude-codex/internal/harness/state"
	toolkit "claude-codex/internal/harness/tools"
)

const (
	deepAgentLoadedContextKey       = "loaded_context"
	deepAgentLoadedContextMaxItems  = 12
	deepAgentLoadedContextMsgLimit  = 8
	deepAgentLoadedContextTextLimit = 600
	deepAgentLoadedMemoryTextLimit  = 2000
	deepAgentLoadedCatalogTextLimit = 240
)

type noopDeepAgentContextLoader struct{}

func (noopDeepAgentContextLoader) LoadDeepAgentContext(_ context.Context, req DeepAgentTaskRequest, agentState *DeepAgentState) (DeepAgentLoadedContext, error) {
	working := stateWorkingMemory(agentState)
	return DeepAgentLoadedContext{
		UserID:    firstNonEmptyString(deepAgentWorkflowString(working, "user_id"), req.UserID),
		SessionID: firstNonEmptyString(deepAgentWorkflowString(working, "session_id"), req.SessionID),
		JobID:     firstNonEmptyString(deepAgentWorkflowString(working, "job_id"), req.JobID),
	}, nil
}

func (r *Runtime) LoadDeepAgentContext(ctx context.Context, req DeepAgentTaskRequest, agentState *DeepAgentState) (DeepAgentLoadedContext, error) {
	working := stateWorkingMemory(agentState)
	userID := firstNonEmptyString(deepAgentWorkflowString(working, "user_id"), req.UserID)
	sessionID := firstNonEmptyString(deepAgentWorkflowString(working, "session_id"), req.SessionID)
	jobID := firstNonEmptyString(deepAgentWorkflowString(working, "job_id"), req.JobID)
	loaded := DeepAgentLoadedContext{UserID: userID, SessionID: sessionID, JobID: jobID}
	if r == nil {
		loaded.Issues = append(loaded.Issues, "runtime is not configured")
		return loaded, nil
	}

	var session *state.Session
	if userID != "" && sessionID != "" && r.sessions != nil {
		var err error
		session, err = r.GetSession(ctx, userID, sessionID)
		if err != nil {
			return loaded, err
		}
		loaded.RecentMessages = deepAgentRecentMessageRefs(session, deepAgentLoadedContextMsgLimit)
		loaded.Attachments = append(loaded.Attachments, deepAgentMessageAttachmentRefs(session)...)
	}

	loaded.Attachments = append(loaded.Attachments, r.deepAgentStoredAttachmentRefs(ctx, userID, sessionID)...)
	loaded.Attachments = append(loaded.Attachments, deepAgentRequestedAttachmentRefs(working)...)
	loaded.Attachments = dedupeDeepAgentAttachmentRefs(loaded.Attachments)
	if len(loaded.Attachments) > deepAgentLoadedContextMaxItems {
		loaded.Attachments = loaded.Attachments[:deepAgentLoadedContextMaxItems]
	}

	loaded.ExistingArtifacts = r.deepAgentExistingArtifactRefs(ctx, userID, sessionID)
	loaded.SkillCatalog = r.deepAgentSkillRefs()
	loaded.ToolCatalog = r.deepAgentToolRefs(ctx, userID, sessionID)
	loaded.MemorySummary = r.deepAgentMemorySummary(ctx, userID, session)
	return loaded, nil
}

func (r *Runtime) deepAgentStoredAttachmentRefs(ctx context.Context, userID, sessionID string) []DeepAgentAttachmentRef {
	if r == nil || userID == "" || sessionID == "" {
		return nil
	}
	attachments, err := r.ListAttachments(ctx, userID, sessionID)
	if err != nil {
		return nil
	}
	out := make([]DeepAgentAttachmentRef, 0, minInt(len(attachments), deepAgentLoadedContextMaxItems))
	for _, attachment := range attachments {
		if attachment == nil || strings.TrimSpace(attachment.ID) == "" {
			continue
		}
		out = append(out, DeepAgentAttachmentRef{
			ID:          attachment.ID,
			Filename:    attachment.Filename,
			ContentType: attachment.ContentType,
			SizeBytes:   attachment.SizeBytes,
			Source:      "asset_store",
		})
	}
	return out
}

func (r *Runtime) deepAgentExistingArtifactRefs(ctx context.Context, userID, sessionID string) []DeepAgentArtifactRef {
	if r == nil || userID == "" || sessionID == "" {
		return nil
	}
	artifacts, err := r.ListArtifacts(ctx, userID, sessionID)
	if err != nil {
		return nil
	}
	out := make([]DeepAgentArtifactRef, 0, minInt(len(artifacts), deepAgentLoadedContextMaxItems))
	for _, artifact := range artifacts {
		if artifact == nil || strings.TrimSpace(artifact.ID) == "" {
			continue
		}
		out = append(out, deepAgentArtifactRefFromArtifact(artifact, "session_artifact"))
	}
	if len(out) > deepAgentLoadedContextMaxItems {
		out = out[:deepAgentLoadedContextMaxItems]
	}
	return out
}

func (r *Runtime) deepAgentSkillRefs() []DeepAgentSkillRef {
	if r == nil || r.skills == nil {
		return nil
	}
	items := r.skills.ListUserInvocableSkills()
	out := make([]DeepAgentSkillRef, 0, len(items))
	for _, skill := range items {
		if skill == nil || !skill.UserInvocable || skill.IsHidden {
			continue
		}
		out = append(out, deepAgentSkillRef(skill))
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	if len(out) > deepAgentLoadedContextMaxItems {
		out = out[:deepAgentLoadedContextMaxItems]
	}
	return out
}

func (r *Runtime) deepAgentToolRefs(ctx context.Context, userID, sessionID string) []DeepAgentToolRef {
	if r == nil || r.engineFactory == nil {
		return nil
	}
	runner := r.runnerForScope(Scope{UserID: userID, SessionID: sessionID})
	describer, ok := runner.(interface {
		Descriptors() []toolkit.Descriptor
	})
	if !ok || describer == nil {
		return nil
	}
	descriptors := describer.Descriptors()
	out := make([]DeepAgentToolRef, 0, len(descriptors))
	for _, descriptor := range descriptors {
		name := strings.TrimSpace(descriptor.Name)
		if name == "" {
			continue
		}
		out = append(out, DeepAgentToolRef{
			Name:        name,
			Description: truncateDeepAgentDiagnosticText(descriptor.Description, deepAgentLoadedCatalogTextLimit),
			Permission:  string(descriptor.Permission),
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	if len(out) > deepAgentLoadedContextMaxItems {
		out = out[:deepAgentLoadedContextMaxItems]
	}
	_ = ctx
	return out
}

func (r *Runtime) deepAgentMemorySummary(ctx context.Context, userID string, session *state.Session) string {
	if r == nil || r.memory == nil || userID == "" {
		return ""
	}
	var (
		summary string
		err     error
	)
	if session != nil {
		summary, err = r.memory.LoadContext(ctx, userID, session)
	} else {
		summary, err = r.memory.LoadUserMemory(ctx, userID)
	}
	if err != nil {
		return ""
	}
	return truncateDeepAgentDiagnosticText(summary, deepAgentLoadedMemoryTextLimit)
}

func deepAgentRecentMessageRefs(session *state.Session, limit int) []DeepAgentMessageRef {
	if session == nil || limit <= 0 {
		return nil
	}
	out := make([]DeepAgentMessageRef, 0, minInt(len(session.Messages), limit))
	for i := len(session.Messages) - 1; i >= 0 && len(out) < limit; i-- {
		message := session.Messages[i]
		if message.Hidden {
			continue
		}
		content := deepAgentMessageContent(message)
		if strings.TrimSpace(content) == "" && len(message.Attachments) == 0 {
			continue
		}
		ref := DeepAgentMessageRef{
			ID:        message.ID,
			Role:      message.Role,
			Content:   truncateDeepAgentDiagnosticText(content, deepAgentLoadedContextTextLimit),
			Snippet:   truncateDeepAgentDiagnosticText(content, 180),
			CreatedAt: message.CreatedAt,
		}
		out = append(out, ref)
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

func deepAgentMessageContent(message state.Message) string {
	if strings.TrimSpace(message.Content) != "" {
		return strings.TrimSpace(message.Content)
	}
	if strings.TrimSpace(message.ToolOutput) != "" {
		return strings.TrimSpace(message.ToolOutput)
	}
	if len(message.ContentParts) > 0 {
		data, _ := json.Marshal(message.ContentParts)
		return string(data)
	}
	if len(message.ContentBlocks) > 0 {
		data, _ := json.Marshal(message.ContentBlocks)
		return string(data)
	}
	return ""
}

func deepAgentMessageAttachmentRefs(session *state.Session) []DeepAgentAttachmentRef {
	if session == nil {
		return nil
	}
	var out []DeepAgentAttachmentRef
	for _, message := range session.Messages {
		for _, attachment := range message.Attachments {
			if strings.TrimSpace(attachment.ID) == "" {
				continue
			}
			out = append(out, DeepAgentAttachmentRef{
				ID:          attachment.ID,
				Filename:    attachment.FileName,
				ContentType: attachment.MimeType,
				SizeBytes:   attachment.FileSize,
				Source:      "message",
			})
		}
	}
	return out
}

func deepAgentRequestedAttachmentRefs(values map[string]any) []DeepAgentAttachmentRef {
	var out []DeepAgentAttachmentRef
	for _, id := range deepAgentStringSlice(values["attachment_ids"]) {
		out = append(out, DeepAgentAttachmentRef{ID: id, Source: "request"})
	}
	for _, item := range deepAgentAttachmentURLs(values["attachment_urls"]) {
		out = append(out, DeepAgentAttachmentRef{
			URL:         item.URL,
			Filename:    item.Filename,
			ContentType: item.ContentType,
			Source:      "request_url",
		})
	}
	return out
}

func deepAgentAttachmentURLs(value any) []ChatAttachmentURL {
	switch typed := value.(type) {
	case []ChatAttachmentURL:
		return append([]ChatAttachmentURL(nil), typed...)
	case []any:
		out := make([]ChatAttachmentURL, 0, len(typed))
		for _, item := range typed {
			switch v := item.(type) {
			case ChatAttachmentURL:
				out = append(out, v)
			case map[string]any:
				out = append(out, ChatAttachmentURL{
					URL:         deepAgentWorkflowString(v, "url"),
					ContentType: deepAgentWorkflowString(v, "content_type"),
					Filename:    deepAgentWorkflowString(v, "filename"),
				})
			}
		}
		return out
	}
	return nil
}

func dedupeDeepAgentAttachmentRefs(items []DeepAgentAttachmentRef) []DeepAgentAttachmentRef {
	if len(items) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]DeepAgentAttachmentRef, 0, len(items))
	for _, item := range items {
		key := firstNonEmptyString(strings.TrimSpace(item.ID), strings.TrimSpace(item.URL), strings.TrimSpace(item.Filename))
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	return out
}

func deepAgentArtifactRefFromArtifact(artifact *Artifact, source string) DeepAgentArtifactRef {
	if artifact == nil {
		return DeepAgentArtifactRef{}
	}
	return DeepAgentArtifactRef{
		ID:          artifact.ID,
		JobID:       artifact.JobID,
		Filename:    artifact.Filename,
		ContentType: artifact.ContentType,
		SizeBytes:   artifact.SizeBytes,
		Source:      source,
		CreatedAt:   artifact.CreatedAt,
	}
}

func deepAgentSkillRef(skill *skills.SkillDefinition) DeepAgentSkillRef {
	if skill == nil {
		return DeepAgentSkillRef{}
	}
	return DeepAgentSkillRef{
		Name:              skill.Name,
		Description:       truncateDeepAgentDiagnosticText(skill.Description, deepAgentLoadedCatalogTextLimit),
		WhenToUse:         truncateDeepAgentDiagnosticText(skill.WhenToUse, deepAgentLoadedCatalogTextLimit),
		ArgumentHint:      truncateDeepAgentDiagnosticText(skill.ArgumentHint, deepAgentLoadedCatalogTextLimit),
		RunAsJob:          skill.RunAsJob || skill.ExecutionContext == skills.ContextFork,
		ProducesArtifacts: skillProducesArtifacts(skill),
	}
}

func deepAgentLoadedContextFromMap(values map[string]any) (DeepAgentLoadedContext, bool) {
	if values == nil {
		return DeepAgentLoadedContext{}, false
	}
	raw, ok := values[deepAgentLoadedContextKey]
	if !ok || raw == nil {
		return DeepAgentLoadedContext{}, false
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return DeepAgentLoadedContext{}, false
	}
	var loaded DeepAgentLoadedContext
	if err := json.Unmarshal(data, &loaded); err != nil {
		return DeepAgentLoadedContext{}, false
	}
	return loaded, true
}

func deepAgentLoadedContextPrompt(values map[string]any) string {
	loaded, ok := deepAgentLoadedContextFromMap(values)
	if !ok {
		return ""
	}
	var sections []string
	if len(loaded.RecentMessages) > 0 {
		var lines []string
		for _, message := range loaded.RecentMessages {
			snippet := firstNonEmptyString(message.Snippet, message.Content)
			if snippet == "" {
				continue
			}
			lines = append(lines, fmt.Sprintf("- %s: %s", firstNonEmptyString(message.Role, "message"), snippet))
		}
		if len(lines) > 0 {
			sections = append(sections, "Recent session messages:\n"+strings.Join(lines, "\n"))
		}
	}
	if len(loaded.Attachments) > 0 {
		var lines []string
		for _, attachment := range loaded.Attachments {
			label := firstNonEmptyString(attachment.Filename, attachment.ID, attachment.URL)
			if label == "" {
				continue
			}
			lines = append(lines, fmt.Sprintf("- %s (%s, %s)", label, firstNonEmptyString(attachment.ContentType, "unknown"), firstNonEmptyString(attachment.Source, "attachment")))
		}
		sections = append(sections, "Attachments:\n"+strings.Join(lines, "\n"))
	}
	if len(loaded.ExistingArtifacts) > 0 {
		var lines []string
		for _, artifact := range loaded.ExistingArtifacts {
			label := firstNonEmptyString(artifact.Filename, artifact.ID)
			if label == "" {
				continue
			}
			lines = append(lines, fmt.Sprintf("- %s (%s)", label, firstNonEmptyString(artifact.ContentType, "unknown")))
		}
		sections = append(sections, "Existing artifacts:\n"+strings.Join(lines, "\n"))
	}
	if strings.TrimSpace(loaded.MemorySummary) != "" {
		sections = append(sections, "Memory summary:\n"+loaded.MemorySummary)
	}
	if len(loaded.ToolCatalog) > 0 {
		var names []string
		for _, tool := range loaded.ToolCatalog {
			if tool.Name != "" {
				names = append(names, tool.Name)
			}
		}
		if len(names) > 0 {
			sections = append(sections, "Available tools: "+strings.Join(names, ", "))
		}
	}
	if len(loaded.SkillCatalog) > 0 {
		var names []string
		for _, skill := range loaded.SkillCatalog {
			if skill.Name != "" {
				names = append(names, skill.Name)
			}
		}
		if len(names) > 0 {
			sections = append(sections, "Available skills: "+strings.Join(names, ", "))
		}
	}
	return strings.TrimSpace(strings.Join(sections, "\n\n"))
}
