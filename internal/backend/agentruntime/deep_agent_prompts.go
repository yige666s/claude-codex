package agentruntime

import (
	"context"
	"fmt"
	"strings"
)

type deepAgentRenderedPrompt struct {
	Content  string
	Metadata PromptMetadata
}

func (p *RuntimeDeepAgentPlanner) renderDeepAgentPrompt(ctx context.Context, promptID string, args ...any) deepAgentRenderedPrompt {
	return p.renderDeepAgentPromptForScope(ctx, promptID, "", "", args...)
}

func (p *RuntimeDeepAgentPlanner) renderDeepAgentPromptForScope(ctx context.Context, promptID, userID, sessionID string, args ...any) deepAgentRenderedPrompt {
	return renderRuntimeDeepAgentPrompt(ctx, p.runtime, promptID, userID, sessionID, args...)
}

func (r *RuntimeDeepAgentStepRouter) renderDeepAgentPrompt(ctx context.Context, promptID string, args ...any) deepAgentRenderedPrompt {
	return r.renderDeepAgentPromptForScope(ctx, promptID, "", "", args...)
}

func (r *RuntimeDeepAgentStepRouter) renderDeepAgentPromptForScope(ctx context.Context, promptID, userID, sessionID string, args ...any) deepAgentRenderedPrompt {
	if r == nil {
		return renderRuntimeDeepAgentPrompt(ctx, nil, promptID, userID, sessionID, args...)
	}
	return renderRuntimeDeepAgentPrompt(ctx, r.runtime, promptID, userID, sessionID, args...)
}

func renderRuntimeDeepAgentPrompt(ctx context.Context, runtime *Runtime, promptID, userID, sessionID string, args ...any) deepAgentRenderedPrompt {
	resolver := NewPromptResolver(nil, nil)
	if runtime != nil {
		resolver = runtime.promptResolver
	}
	if resolver.Store == nil && resolver.Fallbacks == nil {
		resolver = NewPromptResolver(nil, nil)
	}
	resolution, err := resolver.Resolve(ctx, PromptResolveRequest{
		PromptID:    promptID,
		Environment: PromptEnvironmentProduction,
		UserID:      userID,
		SessionID:   sessionID,
		RuntimeMode: "deep_agent",
	})
	if err != nil {
		resolution, err = NewPromptResolver(nil, nil).Resolve(ctx, PromptResolveRequest{
			PromptID:    promptID,
			Environment: PromptEnvironmentProduction,
			UserID:      userID,
			SessionID:   sessionID,
			RuntimeMode: "deep_agent",
		})
	}
	if err != nil {
		return deepAgentRenderedPrompt{Content: fmt.Sprintf(deepAgentPromptFallbackTemplate(promptID), args...)}
	}
	version := normalizePromptVersion(resolution.Version)
	content := version.Content
	if len(args) > 0 {
		content = fmt.Sprintf(content, args...)
	}
	metadata := PromptMetadata{
		PromptID:      firstNonEmptyString(version.PromptID, resolution.PromptID),
		PromptVersion: version.Version,
		PromptHash:    version.ContentHash,
	}
	if resolution.Assignment != nil {
		metadata.ExperimentID = strings.TrimSpace(resolution.Assignment.ExperimentID)
		metadata.VariantID = strings.TrimSpace(resolution.Assignment.VariantID)
	}
	return deepAgentRenderedPrompt{
		Content:  content,
		Metadata: metadata,
	}
}

func deepAgentPromptFallbackTemplate(promptID string) string {
	switch strings.TrimSpace(promptID) {
	case PromptIDRuntimeDeepAgentPlanner:
		return PromptDeepAgentPlannerTemplate
	case PromptIDRuntimeDeepAgentRouter:
		return PromptDeepAgentRouteTemplate
	case PromptIDRuntimeDeepAgentModeClassifier:
		return PromptDeepAgentExecutionModeClassifierTemplate
	case PromptIDRuntimeDeepAgentToolUsageReminder:
		return PromptDeepAgentToolUsageReminder
	case PromptIDRuntimeDeepAgentPlanRepair:
		return PromptDeepAgentPlanRepairContextTemplate
	case PromptIDRuntimeDeepResearchOrchestrator:
		return PromptDeepResearchOrchestratorTemplate
	case PromptIDRuntimeDeepResearchPlanRepair:
		return PromptDeepResearchPlanRepairContextTemplate
	default:
		return ""
	}
}
