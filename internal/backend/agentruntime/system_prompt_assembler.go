package agentruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"claude-codex/internal/harness/state"
)

const (
	SystemPromptLayerBase    = "L0"
	SystemPromptLayerTool    = "L1"
	SystemPromptLayerSafety  = "L2"
	SystemPromptLayerSession = "L3"
	SystemPromptLayerDynamic = "L4"

	SystemPromptCacheLong    = "long"
	SystemPromptCacheSession = "session"
	SystemPromptCacheNone    = "none"

	systemPromptSnapshotVersion = "assembled-v1"
	systemPromptSnapshotMarker  = "<system-prompt-snapshot"
)

type SystemPromptSegment struct {
	Layer       string `json:"layer"`
	PromptID    string `json:"prompt_id,omitempty"`
	Version     string `json:"version,omitempty"`
	Hash        string `json:"hash,omitempty"`
	Content     string `json:"content"`
	CachePolicy string `json:"cache_policy"`
	Fallback    bool   `json:"fallback,omitempty"`
}

type SystemPromptSnapshot struct {
	Segments []SystemPromptSegment `json:"segments"`
	Content  string                `json:"content"`
	Hash     string                `json:"hash"`
}

type SystemPromptAssembler struct {
	Resolver    PromptResolver
	Environment string
	RuntimeMode string
}

func (r *Runtime) injectAssembledChatSystemPrompt(ctx context.Context, userID, sessionID string, session *state.Session, connectorLines string) (SystemPromptSnapshot, error) {
	if session == nil {
		return SystemPromptSnapshot{}, nil
	}
	assembler := SystemPromptAssembler{
		Resolver:    r.promptResolver,
		Environment: PromptEnvironmentProduction,
		RuntimeMode: "chat",
	}
	snapshot, err := assembler.BuildChatSnapshot(ctx, ChatSystemPromptInput{
		UserID:         userID,
		SessionID:      sessionID,
		Session:        session,
		ConnectorLines: connectorLines,
		Temporal:       strings.TrimSpace(r.temporalContext()),
		Locale:         strings.TrimSpace(r.localeContext()),
	})
	if err != nil {
		return SystemPromptSnapshot{}, err
	}
	session.Messages = stripAssembledSystemPromptSourceMessages(session.Messages)
	if strings.TrimSpace(snapshot.Content) != "" {
		session.AddSystemContext(snapshot.Content)
	}
	return snapshot, nil
}

type ChatSystemPromptInput struct {
	UserID         string
	SessionID      string
	Session        *state.Session
	ConnectorLines string
	Temporal       string
	Locale         string
}

func (a SystemPromptAssembler) BuildChatSnapshot(ctx context.Context, input ChatSystemPromptInput) (SystemPromptSnapshot, error) {
	segments := make([]SystemPromptSegment, 0, 8)
	if segment, ok, err := a.registrySegment(ctx, input, PromptIDRuntimeChatBaseBehavior, SystemPromptLayerBase, SystemPromptCacheLong, nil); err != nil {
		return SystemPromptSnapshot{}, err
	} else if ok {
		segments = append(segments, segment)
	}
	if strings.TrimSpace(input.ConnectorLines) != "" {
		variables := map[string]any{"connector_context": strings.TrimSpace(input.ConnectorLines)}
		if segment, ok, err := a.registrySegment(ctx, input, PromptIDRuntimeChatConnectorContext, SystemPromptLayerTool, SystemPromptCacheSession, variables); err != nil {
			return SystemPromptSnapshot{}, err
		} else if ok {
			segments = append(segments, segment)
		}
	}
	if segment, ok, err := a.registrySegment(ctx, input, PromptIDRuntimeChatConsumerSecurity, SystemPromptLayerSafety, SystemPromptCacheLong, nil); err != nil {
		return SystemPromptSnapshot{}, err
	} else if ok {
		segments = append(segments, segment)
	}
	if strings.TrimSpace(input.Locale) != "" {
		segment := a.metadataOnlySegment(ctx, input, PromptIDRuntimeChatLocaleContext, SystemPromptLayerSession, SystemPromptCacheSession, input.Locale)
		segments = append(segments, segment)
	}
	segments = append(segments, existingSystemContextSegments(input.Session)...)
	if strings.TrimSpace(input.Temporal) != "" {
		segment := a.metadataOnlySegment(ctx, input, PromptIDRuntimeChatTemporalContext, SystemPromptLayerDynamic, SystemPromptCacheNone, input.Temporal)
		segments = append(segments, segment)
	}
	snapshot := SystemPromptSnapshot{Segments: segments}
	snapshot.Content = renderSystemPromptSnapshot(segments, "")
	snapshot.Hash = systemPromptHash(snapshot.Content)
	snapshot.Content = renderSystemPromptSnapshot(segments, snapshot.Hash)
	return snapshot, nil
}

func (a SystemPromptAssembler) registrySegment(ctx context.Context, input ChatSystemPromptInput, promptID, layer, cachePolicy string, variables map[string]any) (SystemPromptSegment, bool, error) {
	resolution, err := a.resolve(ctx, input, promptID)
	if err != nil {
		return SystemPromptSegment{}, false, err
	}
	rendered, err := RenderPrompt(resolution, variables)
	if err != nil {
		return SystemPromptSegment{}, false, err
	}
	content := strings.TrimSpace(rendered.Content)
	if content == "" {
		return SystemPromptSegment{}, false, nil
	}
	return SystemPromptSegment{
		Layer:       layer,
		PromptID:    rendered.PromptID,
		Version:     rendered.PromptVersion,
		Hash:        rendered.PromptHash,
		Content:     content,
		CachePolicy: cachePolicy,
		Fallback:    resolution.Fallback,
	}, true, nil
}

func (a SystemPromptAssembler) metadataOnlySegment(ctx context.Context, input ChatSystemPromptInput, promptID, layer, cachePolicy, content string) SystemPromptSegment {
	segment := SystemPromptSegment{
		Layer:       layer,
		PromptID:    promptID,
		Content:     strings.TrimSpace(content),
		CachePolicy: cachePolicy,
		Hash:        systemPromptHash(content),
	}
	if resolution, err := a.resolve(ctx, input, promptID); err == nil {
		version := normalizePromptVersion(resolution.Version)
		segment.Version = version.Version
		segment.Hash = firstNonEmptyString(version.ContentHash, segment.Hash)
		segment.Fallback = resolution.Fallback
	}
	return segment
}

func (a SystemPromptAssembler) resolve(ctx context.Context, input ChatSystemPromptInput, promptID string) (PromptResolution, error) {
	resolver := a.Resolver
	if resolver.Fallbacks == nil && resolver.Store == nil {
		resolver = NewPromptResolver(nil, nil)
	}
	req := PromptResolveRequest{
		PromptID:    promptID,
		Environment: firstNonEmptyString(a.Environment, PromptEnvironmentProduction),
		UserID:      input.UserID,
		SessionID:   input.SessionID,
		RuntimeMode: firstNonEmptyString(a.RuntimeMode, "chat"),
	}
	resolution, err := resolver.Resolve(ctx, req)
	if err == nil {
		return resolution, nil
	}
	if normalizePromptEnvironment(req.Environment) == PromptEnvironmentProduction {
		fallbackResolver := NewPromptResolver(nil, nil)
		return fallbackResolver.Resolve(ctx, req)
	}
	return PromptResolution{}, err
}

func existingSystemContextSegments(session *state.Session) []SystemPromptSegment {
	if session == nil {
		return nil
	}
	segments := make([]SystemPromptSegment, 0)
	for _, message := range session.Messages {
		if !message.Hidden {
			continue
		}
		content := strings.TrimSpace(message.Content)
		if content == "" || strings.Contains(content, consumerSecuritySystemContext) || strings.Contains(content, systemPromptSnapshotMarker) {
			continue
		}
		switch {
		case strings.Contains(content, "<personalization>"):
			segments = append(segments, dynamicSystemPromptSegment(SystemPromptLayerSession, SystemPromptCacheSession, content))
		case strings.Contains(content, appRuntimeContextMarker), strings.Contains(content, memoryContextMarker), strings.Contains(content, episodicMemoryContextMarker), strings.Contains(content, browserMemoryContextMarker):
			segments = append(segments, dynamicSystemPromptSegment(SystemPromptLayerDynamic, SystemPromptCacheNone, content))
		}
	}
	return segments
}

func dynamicSystemPromptSegment(layer, cachePolicy, content string) SystemPromptSegment {
	content = strings.TrimSpace(content)
	return SystemPromptSegment{
		Layer:       layer,
		Content:     content,
		CachePolicy: cachePolicy,
		Hash:        systemPromptHash(content),
	}
}

func stripAssembledSystemPromptSourceMessages(messages []state.Message) []state.Message {
	if len(messages) == 0 {
		return messages
	}
	out := messages[:0]
	for _, message := range messages {
		if isAssembledSystemPromptSourceMessage(message) {
			continue
		}
		out = append(out, message)
	}
	return out
}

func isAssembledSystemPromptSourceMessage(message state.Message) bool {
	if !message.Hidden {
		return false
	}
	content := strings.TrimSpace(message.Content)
	if content == "" {
		return false
	}
	return strings.Contains(content, consumerSecuritySystemContext) ||
		strings.Contains(content, "<personalization>") ||
		strings.Contains(content, appRuntimeContextMarker) ||
		strings.Contains(content, memoryContextMarker) ||
		strings.Contains(content, episodicMemoryContextMarker) ||
		strings.Contains(content, browserMemoryContextMarker) ||
		strings.Contains(content, temporalContextMarker) ||
		strings.Contains(content, localeContextMarker) ||
		strings.Contains(content, systemPromptSnapshotMarker)
}

func renderSystemPromptSnapshot(segments []SystemPromptSegment, hash string) string {
	var builder strings.Builder
	if strings.TrimSpace(hash) == "" {
		builder.WriteString("<system-prompt-snapshot>\n")
	} else {
		builder.WriteString(fmt.Sprintf("<system-prompt-snapshot hash=\"%s\">\n", hash))
	}
	for _, segment := range segments {
		content := strings.TrimSpace(segment.Content)
		if content == "" {
			continue
		}
		builder.WriteString(fmt.Sprintf("<segment layer=\"%s\"", segment.Layer))
		if segment.PromptID != "" {
			builder.WriteString(fmt.Sprintf(" prompt_id=\"%s\"", segment.PromptID))
		}
		if segment.Version != "" {
			builder.WriteString(fmt.Sprintf(" version=\"%s\"", segment.Version))
		}
		if segment.Hash != "" {
			builder.WriteString(fmt.Sprintf(" hash=\"%s\"", segment.Hash))
		}
		if segment.CachePolicy != "" {
			builder.WriteString(fmt.Sprintf(" cache=\"%s\"", segment.CachePolicy))
		}
		builder.WriteString(">\n")
		builder.WriteString(content)
		builder.WriteString("\n</segment>\n")
	}
	builder.WriteString("</system-prompt-snapshot>")
	return builder.String()
}

func systemPromptHash(content string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(content)))
	return hex.EncodeToString(sum[:])
}
