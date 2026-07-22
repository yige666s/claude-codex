package agentruntime

const (
	PromptIDRuntimeChatSystemPromptSnapshot   = "runtime/chat/system_prompt_snapshot"
	PromptIDRuntimeChatBaseBehavior           = "runtime/chat/base_behavior"
	PromptIDRuntimeChatConsumerSecurity       = "runtime/chat/consumer_security"
	PromptIDRuntimeChatTemporalContext        = "runtime/chat/temporal_context"
	PromptIDRuntimeChatLocaleContext          = "runtime/chat/locale_context"
	PromptIDRuntimeChatConnectorContext       = "runtime/chat/connector_context"
	PromptIDRuntimeDeepAgentRouter            = "runtime/deep_agent/router"
	PromptIDRuntimeDeepAgentModeClassifier    = "runtime/deep_agent/mode_classifier"
	PromptIDRuntimeDeepAgentToolUsageReminder = "runtime/deep_agent/tool_usage_reminder"
	PromptIDRuntimeDeepAgentPlanner           = "runtime/deep_agent/planner"
	PromptIDRuntimeDeepAgentPlanRepair        = "runtime/deep_agent/plan_repair"
	PromptIDRuntimeDeepResearchOrchestrator   = "runtime/deep_research/orchestrator"
	PromptIDRuntimeDeepResearchPlanRepair     = "runtime/deep_research/plan_repair"
	PromptIDMemoryExtractDefault              = "memory/extract/default"
	PromptIDMemoryExtractRepair               = "memory/extract/repair"
	PromptIDMemoryOrganizerDefault            = "memory/organizer/default"
	PromptIDMemoryRecallTrigger               = "memory/recall/trigger"
	PromptIDMemoryEpisodeSummarizeDefault     = "memory/episode_summarize/default"
	PromptIDMemoryAssetTextExtract            = "memory/asset/text_extract"
	PromptIDMemoryImageExtract                = "memory/asset/image_extract"
	PromptIDAssetVisionInsight                = "asset/vision/insight"
	PromptIDStructuredJSONRepair              = "runtime/structured_json/repair"
	PromptIDEvalJudgeDefault                  = "eval/judge/default"
	PromptIDLiveSetupDefault                  = "live/setup/default"
	PromptIDLiveDefaultAssistant              = "live/default_assistant"
	PromptIDLiveRunSkillDescription           = "live/tool/run_skill_description"
	PromptIDLiveWebResearchDescription        = "live/tool/web_research_description"
	PromptIDLiveWebResearchPreamble           = "live/web_research/preamble"
	PromptIDLiveSkillRouter                   = "live/skill_router"
	PromptIDRuntimeFailureRecovery            = "runtime/failure_recovery"
)

type SystemPromptBaseline struct {
	Prompt       PromptTemplate `json:"prompt"`
	Version      PromptVersion  `json:"version"`
	Aliases      []string       `json:"aliases,omitempty"`
	Layer        string         `json:"layer"`
	Priority     string         `json:"priority"`
	Source       string         `json:"source"`
	RenderStyle  string         `json:"render_style"`
	MigrationUse string         `json:"migration_use"`
}

func BuiltinSystemPromptBaselines() []SystemPromptBaseline {
	return []SystemPromptBaseline{
		baseline(PromptIDRuntimeChatBaseBehavior, "Chat Base Behavior", "runtime/chat", "L0", "P0", "go-const", PromptChatBaseBehavior, "internal/backend/agentruntime/prompt_constants.go:PromptChatBaseBehavior", "Base behavior for normal chat turns."),
		baseline(PromptIDRuntimeChatConsumerSecurity, "Consumer Security", "runtime/chat", "L2", "P0", "go-const", PromptConsumerSecuritySystemContext, "internal/backend/agentruntime/prompt_constants.go:PromptConsumerSecuritySystemContext", "Consumer web safety boundary for normal chat."),
		baseline(PromptIDRuntimeChatTemporalContext, "Temporal Context", "runtime/chat", "L4", "P1", "fmt", PromptTemporalContextTemplate, "internal/backend/agentruntime/prompt_constants.go:PromptTemporalContextTemplate", "Dynamic date/time context template."),
		baseline(PromptIDRuntimeChatLocaleContext, "Locale Context", "runtime/chat", "L3", "P1", "fmt", PromptLocaleContextTemplate, "internal/backend/agentruntime/prompt_constants.go:PromptLocaleContextTemplate", "Locale and timezone policy template."),
		baseline(PromptIDRuntimeChatConnectorContext, "Connector Context", "runtime/chat", "L1", "P0", "go-const", PromptConnectorContextHeader+"\n\n{{connector_context}}\n\n"+PromptConnectorContextSuffix, "internal/backend/agentruntime/prompt_constants.go:PromptConnectorContextHeader/Suffix", "Selected connector capability and policy prompt."),
		baseline(PromptIDRuntimeDeepAgentRouter, "DeepAgent Router", "runtime/deep_agent", "L1", "P0", "fmt", PromptDeepAgentRouteTemplate, "internal/backend/agentruntime/prompt_constants.go:PromptDeepAgentRouteTemplate", "Classifies DeepAgent plan steps into execution routes."),
		baseline(PromptIDRuntimeDeepAgentModeClassifier, "DeepAgent Mode Classifier", "runtime/deep_agent", "L1", "P0", "fmt", PromptDeepAgentExecutionModeClassifierTemplate, "internal/backend/agentruntime/prompt_constants.go:PromptDeepAgentExecutionModeClassifierTemplate", "Classifies DeepAgent execution mode."),
		baseline(PromptIDRuntimeDeepAgentToolUsageReminder, "DeepAgent Tool Usage Reminder", "runtime/deep_agent", "L1", "P1", "go-const", PromptDeepAgentToolUsageReminder, "internal/backend/agentruntime/prompt_constants.go:PromptDeepAgentToolUsageReminder", "Tool policy reminder appended to DeepAgent execution context."),
		baseline(PromptIDRuntimeDeepAgentPlanner, "DeepAgent Planner", "runtime/deep_agent", "L0", "P0", "fmt", PromptDeepAgentPlannerTemplate, "internal/backend/agentruntime/prompt_constants.go:PromptDeepAgentPlannerTemplate", "Plans DeepAgent user goals into verifiable steps."),
		baseline(PromptIDRuntimeDeepAgentPlanRepair, "DeepAgent Plan Repair Context", "runtime/deep_agent", "L4", "P1", "fmt", PromptDeepAgentPlanRepairContextTemplate, "internal/backend/agentruntime/prompt_constants.go:PromptDeepAgentPlanRepairContextTemplate", "Context template for repairing DeepAgent plans."),
		baseline(PromptIDRuntimeDeepResearchOrchestrator, "Deep Research Orchestrator", "runtime/deep_research", "L0", "P0", "fmt", PromptDeepResearchOrchestratorTemplate, "internal/backend/agentruntime/prompt_constants.go:PromptDeepResearchOrchestratorTemplate", "Builds a task-specific Deep Research worker DAG."),
		baseline(PromptIDRuntimeDeepResearchPlanRepair, "Deep Research Plan Repair Context", "runtime/deep_research", "L4", "P1", "fmt", PromptDeepResearchPlanRepairContextTemplate, "internal/backend/agentruntime/prompt_constants.go:PromptDeepResearchPlanRepairContextTemplate", "Context template for repairing Deep Research task graphs."),
		baselineWithAliases(PromptIDMemoryExtractDefault, "Memory Extract", "memory", "L0", "P0", "handlebars", memoryExtractionPromptTemplate(), "internal/backend/agentruntime/prompt_constants.go:PromptMemoryExtractionTemplate", "Extracts durable memory candidates from conversation.", []string{PromptIDMemoryExtract}, map[string]any{"required": []any{"conversation_json"}}),
		baseline(PromptIDMemoryExtractRepair, "Memory Extract Repair", "memory", "L0", "P1", "fmt", PromptMemoryExtractionRepairTemplate, "internal/backend/agentruntime/prompt_constants.go:PromptMemoryExtractionRepairTemplate", "Repairs invalid memory extraction JSON."),
		baseline(PromptIDMemoryOrganizerDefault, "Memory Organizer", "memory", "L0", "P1", "fmt", PromptMemoryOrganizerTemplate, "internal/backend/agentruntime/prompt_constants.go:PromptMemoryOrganizerTemplate", "Organizes and maintains memory records."),
		baseline(PromptIDMemoryRecallTrigger, "Memory Recall Trigger", "memory", "L0", "P1", "fmt", PromptMemoryRecallLLMTriggerTemplate, "internal/backend/agentruntime/prompt_constants.go:PromptMemoryRecallLLMTriggerTemplate", "Decides whether a turn needs memory recall."),
		baselineWithAliases(PromptIDMemoryEpisodeSummarizeDefault, "Memory Episode Summarize", "memory", "L0", "P0", "handlebars", memoryEpisodeSummarizePromptTemplate(), "internal/backend/agentruntime/prompt_constants.go:PromptMemoryEpisodeSummarizeTemplate", "Summarizes a conversation as episodic memory.", []string{PromptIDMemoryEpisodeSummarize}, map[string]any{"required": []any{"session_id", "conversation_json", "current_timestamp"}}),
		baseline(PromptIDMemoryAssetTextExtract, "Asset Text Memory Extract", "memory", "L0", "P1", "fmt", PromptAssetMemoryTextTemplate, "internal/backend/agentruntime/prompt_constants.go:PromptAssetMemoryTextTemplate", "Extracts durable memory from text assets."),
		baseline(PromptIDMemoryImageExtract, "Image Memory Extract", "memory", "L0", "P1", "go-const", PromptImageMemoryExtraction, "internal/backend/agentruntime/prompt_constants.go:PromptImageMemoryExtraction", "Extracts durable memory candidates from image input."),
		baseline(PromptIDAssetVisionInsight, "Asset Vision Insight", "asset", "L0", "P1", "go-const", PromptVisionAssetInsight, "internal/backend/agentruntime/prompt_constants.go:PromptVisionAssetInsight", "Analyzes generated image artifacts for retrieval and memory candidates."),
		baseline(PromptIDStructuredJSONRepair, "Structured JSON Repair", "runtime", "L0", "P1", "fmt", PromptStructuredJSONRepairTemplate, "internal/backend/agentruntime/prompt_constants.go:PromptStructuredJSONRepairTemplate", "Repairs invalid structured JSON outputs."),
		baselineWithVersion(PromptIDEvalJudgeDefault, "Evaluation Judge", "eval", "L0", "P0", "go-const", goldenJudgeSystemPrompt(), "internal/backend/agentruntime/prompt_constants.go:PromptGoldenJudgeSystem", "RAG-style evaluator judge prompt.", DefaultGoldenJudgePromptVersion, []string{PromptIDEvalJudge}, nil),
		baselineWithAliases(PromptIDLiveSetupDefault, "Live Setup", "live", "L0", "P1", "handlebars", "{{content}}", "internal/backend/agentruntime/prompt_registry.go:defaultPromptFallbacks", "Wrapper prompt for live system instruction content.", []string{PromptIDLiveSetup}, map[string]any{"required": []any{"content"}}),
		baseline(PromptIDLiveDefaultAssistant, "Live Default Assistant", "live", "L0", "P1", "go-const", PromptLiveDefaultAssistantInstruction, "internal/backend/agentruntime/prompt_constants.go:PromptLiveDefaultAssistantInstruction", "Default live voice assistant instruction."),
		baseline(PromptIDLiveRunSkillDescription, "Live Run Skill Function Description", "live", "L1", "P1", "go-const", PromptLiveRunSkillFunctionDescription, "internal/backend/agentruntime/prompt_constants.go:PromptLiveRunSkillFunctionDescription", "Live function description for running a published skill."),
		baseline(PromptIDLiveWebResearchDescription, "Live Web Research Function Description", "live", "L1", "P1", "go-const", PromptLiveWebResearchFunctionDescription, "internal/backend/agentruntime/prompt_constants.go:PromptLiveWebResearchFunctionDescription", "Live function description for web research."),
		baseline(PromptIDLiveWebResearchPreamble, "Live Web Research Preamble", "live", "L0", "P1", "go-const", PromptLiveWebResearchPreamble, "internal/backend/agentruntime/prompt_constants.go:PromptLiveWebResearchPreamble", "Backend web research subtask prompt for live mode."),
		baseline(PromptIDLiveSkillRouter, "Live Skill Router", "live", "L1", "P1", "fmt", PromptLiveSkillRouterTemplate, "internal/backend/agentruntime/prompt_constants.go:PromptLiveSkillRouterTemplate", "Routes live utterances to published skills."),
		baseline(PromptIDRuntimeFailureRecovery, "Failure Recovery", "runtime", "L0", "P1", "fmt", PromptFailureRecoveryTemplate, "internal/backend/agentruntime/prompt_constants.go:PromptFailureRecoveryTemplate", "User-facing response template for failed tool or skill execution."),
	}
}

func baseline(promptID, name, scope, layer, priority, renderStyle, content, source, use string) SystemPromptBaseline {
	return baselineWithVersion(promptID, name, scope, layer, priority, renderStyle, content, source, use, "builtin-v1", nil, nil)
}

func baselineWithAliases(promptID, name, scope, layer, priority, renderStyle, content, source, use string, aliases []string, variablesSchema map[string]any) SystemPromptBaseline {
	return baselineWithVersion(promptID, name, scope, layer, priority, renderStyle, content, source, use, "builtin-v1", aliases, variablesSchema)
}

func baselineWithVersion(promptID, name, scope, layer, priority, renderStyle, content, source, use, version string, aliases []string, variablesSchema map[string]any) SystemPromptBaseline {
	metadata := map[string]any{
		"layer":         layer,
		"priority":      priority,
		"source":        source,
		"render_style":  renderStyle,
		"migration_use": use,
	}
	if len(aliases) > 0 {
		metadata["aliases"] = append([]string(nil), aliases...)
	}
	return SystemPromptBaseline{
		Prompt: PromptTemplate{
			ID:          promptID,
			Name:        name,
			Description: use,
			Scope:       scope,
			Owner:       "runtime",
			Metadata:    metadata,
		},
		Version: PromptVersion{
			PromptID:        promptID,
			Version:         version,
			Status:          PromptStatusPublished,
			Content:         content,
			VariablesSchema: variablesSchema,
			RenderConfig: map[string]any{
				"render_style": renderStyle,
			},
			Changelog: "Seeded from code fallback during system prompt Phase 0 baseline inventory.",
			CreatedBy: "system-prompt-phase0",
		},
		Aliases:      append([]string(nil), aliases...),
		Layer:        layer,
		Priority:     priority,
		Source:       source,
		RenderStyle:  renderStyle,
		MigrationUse: use,
	}
}
