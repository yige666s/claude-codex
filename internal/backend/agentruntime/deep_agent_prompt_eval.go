package agentruntime

const DeepAgentPromptEvalSetVersion = "phase3-v1"

func DefaultDeepAgentPromptGoldenSets() []GoldenSet {
	return []GoldenSet{
		normalizeGoldenSet(GoldenSet{
			ID:          "deep_agent_prompt_planner",
			Name:        "DeepAgent planner prompt regression",
			Description: "Phase 3 golden set for DeepAgent plan decomposition and artifact intent preservation.",
			Version:     DeepAgentPromptEvalSetVersion,
			Metadata: map[string]any{
				"phase":      "phase3",
				"prompt_ids": []any{PromptIDRuntimeDeepAgentPlanner},
			},
			Cases: []GoldenCase{
				{
					ID:             "planner-research-report",
					Query:          "帮我调研 Tolan AI，并生成完整调研报告文档",
					ExpectedAnswer: "Plan should include research and final deliverable steps without embedding concrete tool metadata in plan steps.",
					ExpectedFacts: []string{
						"research step",
						"final deliverable step",
						"no metadata.tool in planner output",
					},
					Tags: []string{"deep_agent", "planner", "artifact"},
				},
				{
					ID:             "planner-code-fix",
					Query:          "修复消息持久化时机错误，跑测试并说明风险",
					ExpectedAnswer: "Plan should separate diagnosis, implementation, and verification with concrete done conditions.",
					ExpectedFacts: []string{
						"diagnosis",
						"implementation",
						"verification",
						"done_condition",
					},
					Tags: []string{"deep_agent", "planner", "code"},
				},
			},
		}),
		normalizeGoldenSet(GoldenSet{
			ID:          "deep_agent_prompt_router",
			Name:        "DeepAgent router prompt regression",
			Description: "Phase 3 golden set for DeepAgent step routing and artifact/tool boundaries.",
			Version:     DeepAgentPromptEvalSetVersion,
			Metadata: map[string]any{
				"phase":      "phase3",
				"prompt_ids": []any{PromptIDRuntimeDeepAgentRouter, PromptIDRuntimeDeepAgentModeClassifier, PromptIDRuntimeDeepAgentToolUsageReminder},
			},
			Cases: []GoldenCase{
				{
					ID:             "router-web-research",
					Query:          "Step intent: collect current pricing from reliable web sources",
					ExpectedAnswer: "Route should allow web research through WebSearch and WebFetch, not local filesystem claims.",
					ExpectedFacts: []string{
						"model or web route",
						"WebSearch",
						"WebFetch",
						"source evidence",
					},
					Tags: []string{"deep_agent", "router", "web"},
				},
				{
					ID:             "router-deliverable",
					Query:          "Step intent: write the final markdown report artifact",
					ExpectedAnswer: "Route should require an artifact-capable execution path and preserve deliverable type.",
					ExpectedFacts: []string{
						"requires_artifact",
						"markdown",
						"Artifact",
					},
					Tags: []string{"deep_agent", "router", "artifact"},
				},
			},
		}),
	}
}

func DefaultDeepAgentGoldenSets() []GoldenSet {
	sets := DefaultDeepAgentTemplateGoldenSets()
	sets = append(sets, DefaultDeepAgentPromptGoldenSets()...)
	return sets
}

func builtinDeepAgentGoldenSetVersion(id, version string) (GoldenSet, bool) {
	for _, set := range DefaultDeepAgentGoldenSets() {
		if set.ID != id {
			continue
		}
		if version == "" || set.Version == version {
			return set, true
		}
	}
	return GoldenSet{}, false
}
