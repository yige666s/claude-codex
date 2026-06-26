package agentruntime

import (
	"fmt"
	"strings"
	"time"
)

const (
	DeepAgentTemplateResearchReport   = "research_report"
	DeepAgentTemplateCodeFix          = "code_fix"
	DeepAgentTemplateCIFailureFix     = "ci_failure_fix"
	DeepAgentTemplateDocGeneration    = "doc_generation"
	DeepAgentTemplateWebMonitor       = "web_monitor"
	DeepAgentTemplateMemoryRefinement = "memory_refinement"
)

type DeepAgentTaskTemplate struct {
	ID            string               `json:"id"`
	Name          string               `json:"name"`
	Description   string               `json:"description,omitempty"`
	TaskType      string               `json:"task_type,omitempty"`
	Deliverable   string               `json:"deliverable,omitempty"`
	Rubric        DeepAgentRubric      `json:"rubric,omitempty"`
	Budget        DeepAgentPolicy      `json:"budget,omitempty"`
	ExecutorHints []DeepAgentStepRoute `json:"executor_hints,omitempty"`
	Steps         []DeepAgentStep      `json:"steps,omitempty"`
	EvalTags      []string             `json:"eval_tags,omitempty"`
}

func DefaultDeepAgentTaskTemplates() []DeepAgentTaskTemplate {
	return []DeepAgentTaskTemplate{
		{
			ID:          DeepAgentTemplateResearchReport,
			Name:        "Research report",
			Description: "Research, source gathering, synthesis, artifact creation, and verification.",
			TaskType:    DeepAgentTemplateResearchReport,
			Deliverable: "research_report",
			Rubric: DeepAgentRubric{
				AcceptanceCriteria: []string{
					"sources are gathered before synthesis",
					"final report states findings, caveats, and next steps",
					"critical coverage includes company/team, product features, pricing/availability, user reviews, competitors, and risks/uncertainty",
					"citations are traceable to source URLs or titles",
				},
				RequiredEvidence:  []string{"multiple source URLs or citations", "synthesis notes", "final artifact reference", "source quality and coverage verification"},
				RequiredArtifacts: []string{"research report"},
				QualityBar:        "Evidence-backed, clearly structured, and explicit about uncertainty.",
			},
			Budget:        DeepAgentPolicy{MaxSteps: 5, MaxActions: 12, MaxDuration: 45 * time.Minute},
			ExecutorHints: templateRoutes([]templateRouteSpec{{stepID: "gather", mode: DeepAgentToolModeModel, executor: deepAgentRouteExecutorModel, deliverable: "source_pack", allowedTools: webResearchAllowedTools(), searchScope: "web"}, {stepID: "synthesize", mode: DeepAgentToolModeModel, executor: deepAgentRouteExecutorModel, deliverable: "analysis"}, {stepID: "artifact", mode: DeepAgentToolModeModelArtifact, executor: deepAgentRouteExecutorArtifact, artifact: true, deliverable: "research_report"}, {stepID: "verify", mode: DeepAgentToolModeTest, executor: deepAgentRouteExecutorTest, deliverable: "verification"}}),
			Steps: []DeepAgentStep{
				researchGatherTemplateStep(),
				templateStep("synthesize", "综合分析并形成报告结构", "Synthesize evidence into findings, caveats, and report outline.", []string{"gather"}, "已形成清晰结论、大纲和风险说明", DeepAgentToolModeModel),
				templateStep("artifact", "生成调研报告 artifact", "Create the final research report artifact for the requested goal.", []string{"gather", "synthesize"}, "最终报告 artifact 已生成", DeepAgentToolModeModelArtifact),
				researchVerifyTemplateStep(),
			},
			EvalTags: []string{"research", "artifact", "source_quality"},
		},
		{
			ID:          DeepAgentTemplateCodeFix,
			Name:        "Code fix",
			Description: "Reproduce, diagnose, patch, test, and summarize a code issue.",
			TaskType:    DeepAgentTemplateCodeFix,
			Deliverable: "code_change",
			Rubric: DeepAgentRubric{
				AcceptanceCriteria: []string{"failure mode is identified before changes", "patch is scoped to the issue", "relevant tests or diagnostics pass"},
				RequiredEvidence:   []string{"root cause", "changed files", "test output"},
				QualityBar:         "Minimal, reversible, and verified against the reproduced behavior.",
			},
			Budget:        DeepAgentPolicy{MaxSteps: 5, MaxActions: 14, MaxDuration: 60 * time.Minute},
			ExecutorHints: templateRoutes([]templateRouteSpec{{stepID: "reproduce", mode: DeepAgentToolModeTest, executor: deepAgentRouteExecutorTest, deliverable: "failure_evidence"}, {stepID: "diagnose", mode: DeepAgentToolModeModel, executor: deepAgentRouteExecutorModel, deliverable: "root_cause"}, {stepID: "patch", mode: DeepAgentToolModeCodePatch, executor: deepAgentRouteExecutorCodePatch, deliverable: "code_patch"}, {stepID: "test", mode: DeepAgentToolModeTest, executor: deepAgentRouteExecutorTest, deliverable: "verification"}}),
			Steps: []DeepAgentStep{
				templateStep("reproduce", "复现或确认问题", "Run the narrowest available reproduction, diagnostic, or failing test.", nil, "已确认故障表现或当前诊断入口", DeepAgentToolModeTest),
				templateStep("diagnose", "定位根因和影响范围", "Trace the failure to code paths and constraints before editing.", []string{"reproduce"}, "已记录根因和影响范围", DeepAgentToolModeModel),
				templateStep("patch", "实现最小修复", "Apply the smallest code change that addresses the root cause.", []string{"diagnose"}, "修复已完成且范围可解释", DeepAgentToolModeCodePatch),
				templateStep("test", "运行相关验证", "Run focused tests, type checks, or diagnostics that prove the fix.", []string{"patch"}, "相关验证通过或已记录剩余风险", DeepAgentToolModeTest),
			},
			EvalTags: []string{"code", "regression", "verification"},
		},
		{
			ID:          DeepAgentTemplateCIFailureFix,
			Name:        "CI failure fix",
			Description: "Read CI logs, isolate failure, repair, and rerun relevant checks.",
			TaskType:    DeepAgentTemplateCIFailureFix,
			Deliverable: "ci_fix",
			Rubric: DeepAgentRubric{
				AcceptanceCriteria: []string{"CI failure excerpt is captured", "fix targets the failing check", "rerun command or local equivalent passes"},
				RequiredEvidence:   []string{"CI log excerpt", "failing check name", "rerun result"},
				QualityBar:         "Fix the failing check without broad unrelated refactors.",
			},
			Budget:        DeepAgentPolicy{MaxSteps: 4, MaxActions: 12, MaxDuration: 45 * time.Minute},
			ExecutorHints: templateRoutes([]templateRouteSpec{{stepID: "logs", mode: DeepAgentToolModeRAGSearch, executor: deepAgentRouteExecutorRAG, deliverable: "ci_logs"}, {stepID: "fix", mode: DeepAgentToolModeCodePatch, executor: deepAgentRouteExecutorCodePatch, deliverable: "code_patch"}, {stepID: "rerun", mode: DeepAgentToolModeTest, executor: deepAgentRouteExecutorTest, deliverable: "verification"}}),
			Steps: []DeepAgentStep{
				templateStep("logs", "读取失败日志", "Collect the failing check, error excerpt, and relevant environment details.", nil, "已提取失败日志和失败目标", DeepAgentToolModeRAGSearch),
				templateStep("fix", "定位并修复失败原因", "Patch the code, config, or test data causing the CI failure.", []string{"logs"}, "失败原因已被针对性修复", DeepAgentToolModeCodePatch),
				templateStep("rerun", "重跑相关检查", "Run the failing check or closest local equivalent.", []string{"fix"}, "相关检查通过", DeepAgentToolModeTest),
			},
			EvalTags: []string{"ci", "test", "regression"},
		},
		{
			ID:          DeepAgentTemplateDocGeneration,
			Name:        "Document generation",
			Description: "Collect context, generate an artifact, and check formatting.",
			TaskType:    DeepAgentTemplateDocGeneration,
			Deliverable: "document",
			Rubric: DeepAgentRubric{
				AcceptanceCriteria: []string{"source context is reflected", "artifact follows requested structure", "format checks are completed"},
				RequiredEvidence:   []string{"source context summary", "artifact reference", "format check"},
				RequiredArtifacts:  []string{"document artifact"},
				QualityBar:         "Readable, structured, and faithful to the supplied context.",
			},
			Budget:        DeepAgentPolicy{MaxSteps: 4, MaxActions: 10, MaxDuration: 40 * time.Minute},
			ExecutorHints: templateRoutes([]templateRouteSpec{{stepID: "context", mode: DeepAgentToolModeRAGSearch, executor: deepAgentRouteExecutorRAG, deliverable: "context_pack"}, {stepID: "draft", mode: DeepAgentToolModeModelArtifact, executor: deepAgentRouteExecutorArtifact, artifact: true, deliverable: "document"}, {stepID: "format", mode: DeepAgentToolModeTest, executor: deepAgentRouteExecutorTest, deliverable: "format_check"}}),
			Steps: []DeepAgentStep{
				templateStep("context", "收集文档上下文", "Collect relevant source material, constraints, and existing structure.", nil, "上下文和格式约束已明确", DeepAgentToolModeRAGSearch),
				templateStep("draft", "生成文档 artifact", "Generate the requested document artifact using the collected context.", []string{"context"}, "文档 artifact 已生成", DeepAgentToolModeModelArtifact),
				templateStep("format", "检查文档格式", "Check structure, formatting, and requested artifact requirements.", []string{"draft"}, "文档格式和结构通过检查", DeepAgentToolModeTest),
			},
			EvalTags: []string{"document", "artifact", "format"},
		},
		{
			ID:          DeepAgentTemplateWebMonitor,
			Name:        "Web monitor",
			Description: "Observe a web target on schedule, judge changes, and summarize or alert.",
			TaskType:    DeepAgentTemplateWebMonitor,
			Deliverable: "monitor_summary",
			Rubric: DeepAgentRubric{
				AcceptanceCriteria: []string{"target is observed with timestamp", "change decision is explicit", "summary or alert explains impact"},
				RequiredEvidence:   []string{"observed target", "change signal", "summary or alert"},
				QualityBar:         "Concise, timestamped, and actionable.",
			},
			Budget:        DeepAgentPolicy{MaxSteps: 3, MaxActions: 8, MaxDuration: 20 * time.Minute},
			ExecutorHints: templateRoutes([]templateRouteSpec{{stepID: "observe", mode: DeepAgentToolModeWeb, executor: deepAgentRouteExecutorWeb, deliverable: "observation"}, {stepID: "judge", mode: DeepAgentToolModeModel, executor: deepAgentRouteExecutorModel, deliverable: "change_decision"}, {stepID: "summarize", mode: DeepAgentToolModeModel, executor: deepAgentRouteExecutorModel, deliverable: "monitor_summary"}}),
			Steps: []DeepAgentStep{
				templateStep("observe", "观察目标页面或数据源", "Fetch or inspect the configured web target and capture current state.", nil, "已记录观察结果和时间", DeepAgentToolModeWeb),
				templateStep("judge", "判断是否发生关键变化", "Compare current observation with trigger payload or prior state.", []string{"observe"}, "已给出变化判断和依据", DeepAgentToolModeModel),
				templateStep("summarize", "生成摘要或告警", "Produce a concise summary or alert if the change is material.", []string{"judge"}, "摘要或告警已生成", DeepAgentToolModeModel),
			},
			EvalTags: []string{"monitor", "web", "change_detection"},
		},
		{
			ID:          DeepAgentTemplateMemoryRefinement,
			Name:        "Memory refinement",
			Description: "Extract learnings from a session, classify them, and stage for confirmation.",
			TaskType:    DeepAgentTemplateMemoryRefinement,
			Deliverable: "memory_candidates",
			Rubric: DeepAgentRubric{
				AcceptanceCriteria: []string{"candidate learnings are extracted from evidence", "classification is explicit", "write is staged for confirmation when needed"},
				RequiredEvidence:   []string{"source session evidence", "classified candidates", "confirmation status"},
				QualityBar:         "Specific, non-duplicative, and privacy-aware.",
			},
			Budget:        DeepAgentPolicy{MaxSteps: 3, MaxActions: 8, MaxDuration: 25 * time.Minute},
			ExecutorHints: templateRoutes([]templateRouteSpec{{stepID: "extract", mode: DeepAgentToolModeRAGSearch, executor: deepAgentRouteExecutorRAG, deliverable: "learning_candidates"}, {stepID: "classify", mode: DeepAgentToolModeModel, executor: deepAgentRouteExecutorModel, deliverable: "classification"}, {stepID: "stage", mode: DeepAgentToolModeModel, executor: deepAgentRouteExecutorModel, deliverable: "confirmation_queue"}}),
			Steps: []DeepAgentStep{
				templateStep("extract", "提取会话 learning 候选", "Extract candidate learnings from supplied session or workflow evidence.", nil, "已提取候选 learning 和来源", DeepAgentToolModeRAGSearch),
				templateStep("classify", "分类并去重候选", "Classify candidates by category, sensitivity, and duplication risk.", []string{"extract"}, "候选已分类并去重", DeepAgentToolModeModel),
				templateStep("stage", "待确认写入", "Stage approved candidates for confirmation instead of silently committing memory.", []string{"classify"}, "候选已进入待确认状态", DeepAgentToolModeModel),
			},
			EvalTags: []string{"memory", "classification", "privacy"},
		},
	}
}

func DeepAgentTaskTemplateByID(id string) (DeepAgentTaskTemplate, bool) {
	id = normalizeDeepAgentTemplateID(id)
	for _, tmpl := range DefaultDeepAgentTaskTemplates() {
		if tmpl.ID == id {
			return cloneDeepAgentTaskTemplate(tmpl), true
		}
	}
	return DeepAgentTaskTemplate{}, false
}

func applyDeepAgentTaskTemplateToTaskRequest(req DeepAgentTaskRequest) DeepAgentTaskRequest {
	tmpl, ok := selectDeepAgentTaskTemplate(req.Goal, req.State)
	if !ok {
		return req
	}
	req.State = cloneWorkflowMap(req.State)
	if req.State == nil {
		req.State = map[string]any{}
	}
	req.State["template_id"] = tmpl.ID
	req.State["deep_agent_template"] = tmpl
	req.State["task_type"] = firstNonEmptyString(deepAgentWorkflowString(req.State, "task_type"), tmpl.TaskType)
	req.State["deliverable"] = firstNonEmptyString(deepAgentWorkflowString(req.State, "deliverable"), tmpl.Deliverable)
	req.State["template_eval_tags"] = append([]string(nil), tmpl.EvalTags...)
	req.Rubric = mergeDeepAgentRubric(tmpl.Rubric, req.Rubric)
	req.Policy = mergeTemplatePolicy(tmpl.Budget, req.Policy)
	if len(req.Plan.Steps) == 0 && len(tmpl.Steps) > 0 {
		req.Plan = deepAgentPlanFromTemplate(tmpl, req.Goal)
	}
	return req
}

func deepAgentPlanFromTemplate(tmpl DeepAgentTaskTemplate, goal string) DeepAgentPlan {
	steps := make([]DeepAgentStep, 0, len(tmpl.Steps))
	for _, step := range tmpl.Steps {
		copied := step
		copied.DependsOn = append([]string(nil), step.DependsOn...)
		copied.Metadata = cloneWorkflowMap(step.Metadata)
		if copied.Metadata == nil {
			copied.Metadata = map[string]any{}
		}
		copied.Metadata["template_id"] = tmpl.ID
		if _, ok := copied.Metadata["args"]; !ok {
			copied.Metadata["args"] = map[string]any{}
		}
		if args, ok := copied.Metadata["args"].(map[string]any); ok {
			args["template_id"] = tmpl.ID
			args["template_task_type"] = tmpl.TaskType
		}
		steps = append(steps, copied)
	}
	return normalizeDeepAgentPlan(goal, DeepAgentPlan{Goal: goal, Steps: steps})
}

func selectDeepAgentTaskTemplate(goal string, state map[string]any) (DeepAgentTaskTemplate, bool) {
	if id := deepAgentWorkflowString(state, "template_id"); id != "" {
		return DeepAgentTaskTemplateByID(id)
	}
	_ = goal
	return DeepAgentTaskTemplate{}, false
}

func normalizeDeepAgentTemplateID(id string) string {
	return strings.ToLower(strings.TrimSpace(id))
}

func templateStep(id, title, intent string, dependsOn []string, done, tool string) DeepAgentStep {
	route := DeepAgentStepRoute{
		StepID:          id,
		Version:         "template:v1",
		Mode:            tool,
		Executor:        routeExecutorForTool(tool),
		DeliverableType: id,
		SuccessCriteria: []string{done},
		Reason:          "deep-agent template default route",
		Confidence:      "medium",
	}
	return DeepAgentStep{
		ID:            id,
		Title:         title,
		Intent:        intent,
		DependsOn:     append([]string(nil), dependsOn...),
		DoneCondition: done,
		Status:        DeepAgentStepStatusPending,
		Metadata: map[string]any{
			"tool":       tool,
			"args":       map[string]any{"instruction": intent},
			"step_route": deepAgentStepRouteMap(route),
		},
	}
}

func researchGatherTemplateStep() DeepAgentStep {
	intent := PromptResearchGatherIntent
	step := templateStep("gather", "收集来源和关键事实", intent, nil, "已收集可追溯来源，并覆盖 company/team、product features、pricing/availability、user reviews、competitors、risks/uncertainty", DeepAgentToolModeModel)
	route := DeepAgentStepRoute{
		StepID:          "gather",
		Version:         "template:v1",
		Mode:            DeepAgentToolModeModel,
		Executor:        deepAgentRouteExecutorModel,
		DeliverableType: "source_pack",
		AllowedTools:    webResearchAllowedTools(),
		SearchScope:     "web",
		SuccessCriteria: []string{"已收集可追溯来源和关键事实", "已覆盖 company/team、product features、pricing/availability、user reviews、competitors、risks/uncertainty"},
		Reason:          "research report template source gathering route",
		Confidence:      "high",
	}
	step.Metadata["step_route"] = deepAgentStepRouteMap(route)
	if args, ok := step.Metadata["args"].(map[string]any); ok {
		args["allowed_tools"] = webResearchAllowedTools()
		args["search_scope"] = "web"
	}
	return step
}

func researchVerifyTemplateStep() DeepAgentStep {
	step := templateStep("verify", "校验来源、结论和交付物", "Verify source quality, citation traceability, entity disambiguation, coverage completeness, and artifact availability using accumulated DeepAgent evidence.", []string{"artifact"}, "来源质量、引用、实体、覆盖项和 artifact 均通过校验", DeepAgentToolModeTest)
	if args, ok := step.Metadata["args"].(map[string]any); ok {
		args["state_verification"] = true
	}
	return step
}

type templateRouteSpec struct {
	stepID       string
	mode         string
	executor     string
	artifact     bool
	deliverable  string
	allowedTools []string
	searchScope  string
}

func templateRoutes(specs []templateRouteSpec) []DeepAgentStepRoute {
	out := make([]DeepAgentStepRoute, 0, len(specs))
	for _, spec := range specs {
		out = append(out, DeepAgentStepRoute{
			StepID:           spec.stepID,
			Version:          "template:v1",
			Mode:             spec.mode,
			Executor:         spec.executor,
			RequiresArtifact: spec.artifact,
			DeliverableType:  spec.deliverable,
			AllowedTools:     firstNonEmptyStringSlice(spec.allowedTools, allowedToolsForTemplateMode(spec.mode)),
			SearchScope:      spec.searchScope,
			SuccessCriteria:  []string{fmt.Sprintf("complete %s step", spec.stepID)},
			Reason:           "deep-agent template executor hint",
			Confidence:       "medium",
		})
	}
	return out
}

func webResearchAllowedTools() []string {
	return []string{"WebSearch", "WebFetch"}
}

func firstNonEmptyStringSlice(values ...[]string) []string {
	for _, value := range values {
		if len(value) > 0 {
			return append([]string(nil), value...)
		}
	}
	return nil
}

func routeExecutorForTool(tool string) string {
	switch tool {
	case DeepAgentToolModeModelArtifact:
		return deepAgentRouteExecutorArtifact
	case DeepAgentToolModeSkill:
		return deepAgentRouteExecutorSkill
	case DeepAgentToolModeRAGSearch:
		return deepAgentRouteExecutorRAG
	case DeepAgentToolModeTest:
		return deepAgentRouteExecutorTest
	case DeepAgentToolModeWeb:
		return deepAgentRouteExecutorWeb
	case DeepAgentToolModeCodePatch:
		return deepAgentRouteExecutorCodePatch
	default:
		return deepAgentRouteExecutorModel
	}
}

func allowedToolsForTemplateMode(mode string) []string {
	switch mode {
	case DeepAgentToolModeWeb:
		return []string{"web", "search", "browser"}
	case DeepAgentToolModeRAGSearch:
		return []string{"rag_search", "message_search", "context_loader"}
	case DeepAgentToolModeTest:
		return []string{"test", "diagnostics", "shell_readonly"}
	case DeepAgentToolModeCodePatch:
		return []string{"code_patch", "tests", "diagnostics"}
	case DeepAgentToolModeModelArtifact:
		return []string{"model", "artifact"}
	default:
		return []string{"model"}
	}
}

func mergeDeepAgentRubric(defaults, explicit DeepAgentRubric) DeepAgentRubric {
	return DeepAgentRubric{
		AcceptanceCriteria: appendUniqueStrings(defaults.AcceptanceCriteria, explicit.AcceptanceCriteria),
		RequiredEvidence:   appendUniqueStrings(defaults.RequiredEvidence, explicit.RequiredEvidence),
		RequiredArtifacts:  appendUniqueStrings(defaults.RequiredArtifacts, explicit.RequiredArtifacts),
		ForbiddenActions:   appendUniqueStrings(defaults.ForbiddenActions, explicit.ForbiddenActions),
		QualityBar:         firstNonEmptyString(explicit.QualityBar, defaults.QualityBar),
	}
}

func normalizeDeepAgentRubric(rubric DeepAgentRubric) DeepAgentRubric {
	return DeepAgentRubric{
		AcceptanceCriteria: appendUniqueStrings(nil, rubric.AcceptanceCriteria),
		RequiredEvidence:   appendUniqueStrings(nil, rubric.RequiredEvidence),
		RequiredArtifacts:  appendUniqueStrings(nil, rubric.RequiredArtifacts),
		ForbiddenActions:   appendUniqueStrings(nil, rubric.ForbiddenActions),
		QualityBar:         strings.TrimSpace(rubric.QualityBar),
	}
}

func deepAgentRubricEmpty(rubric DeepAgentRubric) bool {
	rubric = normalizeDeepAgentRubric(rubric)
	return len(rubric.AcceptanceCriteria) == 0 &&
		len(rubric.RequiredEvidence) == 0 &&
		len(rubric.RequiredArtifacts) == 0 &&
		len(rubric.ForbiddenActions) == 0 &&
		rubric.QualityBar == ""
}

func mergeTemplatePolicy(defaults DeepAgentPolicy, explicit DeepAgentPolicy) DeepAgentPolicy {
	policy := defaults
	if explicit.MaxSteps > 0 {
		policy.MaxSteps = explicit.MaxSteps
	}
	if explicit.MaxActions > 0 {
		policy.MaxActions = explicit.MaxActions
	}
	if explicit.MaxDuration > 0 {
		policy.MaxDuration = explicit.MaxDuration
	}
	if explicit.StepTimeout > 0 {
		policy.StepTimeout = explicit.StepTimeout
	}
	if explicit.NoProgressLimit > 0 {
		policy.NoProgressLimit = explicit.NoProgressLimit
	}
	return policy
}

func appendUniqueStrings(defaults, explicit []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(defaults)+len(explicit))
	for _, values := range [][]string{defaults, explicit} {
		for _, value := range values {
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			key := strings.ToLower(value)
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, value)
		}
	}
	return out
}

func cloneDeepAgentTaskTemplate(tmpl DeepAgentTaskTemplate) DeepAgentTaskTemplate {
	tmpl.Rubric = mergeDeepAgentRubric(DeepAgentRubric{}, tmpl.Rubric)
	tmpl.ExecutorHints = append([]DeepAgentStepRoute(nil), tmpl.ExecutorHints...)
	tmpl.EvalTags = append([]string(nil), tmpl.EvalTags...)
	steps := make([]DeepAgentStep, 0, len(tmpl.Steps))
	for _, step := range tmpl.Steps {
		copied := step
		copied.DependsOn = append([]string(nil), step.DependsOn...)
		copied.Metadata = cloneWorkflowMap(step.Metadata)
		steps = append(steps, copied)
	}
	tmpl.Steps = steps
	return tmpl
}
