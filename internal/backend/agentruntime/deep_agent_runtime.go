package agentruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"claude-codex/internal/harness/engine"
	"claude-codex/internal/harness/skills"
	"claude-codex/internal/harness/state"
	skilltool "claude-codex/internal/harness/tools/skill"
)

type RuntimeDeepAgentPlanner struct {
	runtime *Runtime
}

func NewRuntimeDeepAgentPlanner(runtime *Runtime) *RuntimeDeepAgentPlanner {
	return &RuntimeDeepAgentPlanner{runtime: runtime}
}

func (p *RuntimeDeepAgentPlanner) CreatePlan(ctx context.Context, req DeepAgentTaskRequest) (DeepAgentPlan, error) {
	if p == nil || p.runtime == nil {
		return DeepAgentPlan{}, fmt.Errorf("runtime deep agent planner is not configured")
	}
	runner := p.runtime.runnerForScope(ctx, Scope{
		UserID:    req.UserID,
		SessionID: req.SessionID,
		Prompt:    req.Goal,
	})
	plannerSession := state.NewSession("")
	renderedPrompt := p.deepAgentPlannerPromptForRequest(ctx, req)
	prompt := renderedPrompt.Content
	callCtx := WithPromptMetadata(ctx, renderedPrompt.Metadata)
	result, err := runner.RunGeneratedPrompt(callCtx, plannerSession, prompt)
	if err != nil {
		if isDeepAgentEmptyModelResponseError(err) {
			schema := deepAgentPlanStructuredSchema()
			fallbackPlan, fallbackErr := ruleDeepAgentPlanner{}.CreatePlan(ctx, req)
			emitStructuredOutputFallbackEvent(ctx, schema, "deep_agent_planner", structuredFallbackRulePlanner, fallbackErr)
			if fallbackErr != nil {
				return DeepAgentPlan{}, fmt.Errorf("deep agent planner empty response and fallback failed: %w", fallbackErr)
			}
			plan := normalizeDeepAgentPlan(req.Goal, fallbackPlan)
			for _, step := range plan.Steps {
				if strings.TrimSpace(step.DoneCondition) == "" {
					return DeepAgentPlan{}, fmt.Errorf("deep agent planner returned step %q without done_condition", firstNonEmptyString(step.ID, step.Title))
				}
			}
			return plan, nil
		}
		return DeepAgentPlan{}, err
	}
	plan, err := parseDeepAgentPlan(result.Output)
	if err != nil {
		schema := deepAgentPlanStructuredSchema()
		emitStructuredOutputValidationFailure(ctx, schema, "deep_agent_planner", ExtractAndValidateStructuredObject(result.Output, schema))
		repairContext := p.deepAgentPlanRepairContext(ctx, req)
		repaired, repairErr := repairStructuredJSONWithRunner(WithPromptMetadata(ctx, repairContext.Metadata), runner, schema, result.Output, err, repairContext.Content)
		if repairErr == nil {
			plan, err = parseDeepAgentPlan(string(repaired))
		}
		if err != nil {
			fallbackPlan, fallbackErr := ruleDeepAgentPlanner{}.CreatePlan(ctx, req)
			if fallbackErr != nil {
				emitStructuredOutputFallbackEvent(ctx, schema, "deep_agent_planner", structuredFallbackRulePlanner, fallbackErr)
				return DeepAgentPlan{}, fmt.Errorf("deep agent planner output invalid after %s and %s: %w; repair failed: %v; fallback failed: %v", structuredFallbackRepairRetry, structuredFallbackRulePlanner, err, repairErr, fallbackErr)
			}
			emitStructuredOutputFallbackEvent(ctx, schema, "deep_agent_planner", structuredFallbackRulePlanner, nil)
			plan = fallbackPlan
		}
	}
	plan = normalizeDeepAgentPlan(req.Goal, plan)
	for _, step := range plan.Steps {
		if strings.TrimSpace(step.DoneCondition) == "" {
			return DeepAgentPlan{}, fmt.Errorf("deep agent planner returned step %q without done_condition", firstNonEmptyString(step.ID, step.Title))
		}
	}
	return plan, nil
}

func isDeepAgentEmptyModelResponseError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "empty response")
}

func (p *RuntimeDeepAgentPlanner) deepAgentPlannerPrompt(req DeepAgentTaskRequest) string {
	return p.deepAgentPlannerPromptForRequest(context.Background(), req).Content
}

func (p *RuntimeDeepAgentPlanner) deepAgentPlannerPromptForRequest(ctx context.Context, req DeepAgentTaskRequest) deepAgentRenderedPrompt {
	skillCatalog := p.deepAgentSkillCatalogPrompt()
	if strings.TrimSpace(skillCatalog) == "" {
		skillCatalog = "(none)"
	}
	maxSteps := normalizeDeepAgentPolicy(req.Policy).MaxSteps
	loadedContext := deepAgentLoadedContextPrompt(req.State)
	if loadedContext == "" {
		loadedContext = "(none)"
	}
	rubric := deepAgentRubricPrompt(req.Rubric)
	if rubric == "" {
		rubric = "(none)"
	}
	return p.renderDeepAgentPromptForScope(ctx, PromptIDRuntimeDeepAgentPlanner, req.UserID, req.SessionID, maxSteps, rubric, skillCatalog, loadedContext, strings.TrimSpace(req.Goal))
}

func (p *RuntimeDeepAgentPlanner) deepAgentSkillCatalogPrompt() string {
	if p == nil || p.runtime == nil || p.runtime.skills == nil {
		return "(none)"
	}
	items := p.runtime.skills.ListUserInvocableSkills()
	if len(items) == 0 {
		return "(none)"
	}
	var out strings.Builder
	for _, skill := range items {
		if skill == nil || !skill.UserInvocable || skill.IsHidden {
			continue
		}
		out.WriteString("- name: ")
		out.WriteString(skill.Name)
		if strings.TrimSpace(skill.Description) != "" {
			out.WriteString("\n  description: ")
			out.WriteString(skill.Description)
		}
		if strings.TrimSpace(skill.WhenToUse) != "" {
			out.WriteString("\n  when_to_use: ")
			out.WriteString(skill.WhenToUse)
		}
		if strings.TrimSpace(skill.ArgumentHint) != "" {
			out.WriteString("\n  args_hint: ")
			out.WriteString(skill.ArgumentHint)
		}
		if skill.RunAsJob || skill.ExecutionContext == skills.ContextFork {
			out.WriteString("\n  run_mode: job")
		}
		if skillProducesArtifacts(skill) {
			out.WriteString("\n  produces_artifacts: true")
		}
		out.WriteString("\n")
	}
	if strings.TrimSpace(out.String()) == "" {
		return "(none)"
	}
	return strings.TrimSpace(out.String())
}

func (p *RuntimeDeepAgentPlanner) validatePlanSkillReferences(plan DeepAgentPlan) error {
	if p == nil || p.runtime == nil || p.runtime.skills == nil {
		return nil
	}
	for idx, step := range plan.Steps {
		if deepAgentWorkflowString(step.Metadata, "tool") != "skill" {
			continue
		}
		args, _ := step.Metadata["args"].(map[string]any)
		skillName := strings.TrimPrefix(strings.TrimSpace(deepAgentWorkflowString(args, "skill_name")), "/")
		if skillName == "" {
			return fmt.Errorf("deep agent plan step %d skill args.skill_name is required", idx)
		}
		skill, ok := p.runtime.skills.GetSkill(skillName)
		if !ok || skill == nil || !skill.UserInvocable || skill.IsHidden {
			return fmt.Errorf("deep agent plan step %d references unavailable skill: %s", idx, skillName)
		}
	}
	return nil
}

func (p *RuntimeDeepAgentPlanner) NextAction(ctx context.Context, state *DeepAgentState, step DeepAgentStep) (DeepAgentAction, error) {
	return p.routeStepAction(ctx, state, step)
}

func (p *RuntimeDeepAgentPlanner) routeStepAction(ctx context.Context, state *DeepAgentState, step DeepAgentStep) (DeepAgentAction, error) {
	router := NewRuntimeDeepAgentStepRouter(p.runtime)
	route, err := router.RouteStep(ctx, state, step)
	if err != nil {
		return DeepAgentAction{}, err
	}
	return p.actionForRoute(state, step, route)
}

func (p *RuntimeDeepAgentPlanner) actionForRoute(state *DeepAgentState, step DeepAgentStep, route DeepAgentStepRoute) (DeepAgentAction, error) {
	mode := normalizeDeepAgentRouteMode(route.Mode)
	args := map[string]any{}
	if rawArgs, ok := step.Metadata["args"].(map[string]any); ok {
		args = cloneWorkflowMap(rawArgs)
	}
	switch mode {
	case DeepAgentToolModeSkill:
		skillName := strings.TrimPrefix(strings.TrimSpace(route.SkillName), "/")
		if skillName == "" {
			if skill, ok := p.selectSkillForStep(step); ok && skill != nil {
				skillName = skill.Name
			}
		}
		if skillName == "" {
			mode = DeepAgentToolModeModel
			args["prompt"] = p.modelPromptForStep(state, step)
			break
		}
		args["skill_name"] = skillName
		args["args"] = p.skillArgsForStep(state, step)
	case DeepAgentToolModeRAGSearch, "tool_use":
		mode = DeepAgentToolModeRAGSearch
		args["query"] = firstNonEmptyString(step.Intent, step.Title, stateGoal(state))
		args["limit"] = DeepAgentDefaultRAGSearchLimit
	case DeepAgentToolModeModelArtifact:
		args["prompt"] = p.modelPromptForStep(state, step)
	case DeepAgentToolModeTest:
		args["prompt"] = p.modelPromptForStep(state, step)
		args["expected_evidence"] = "test results, exit status, and failure excerpt if any"
	case DeepAgentToolModeWeb:
		if explicitURL := firstNonEmptyString(deepAgentArgsExplicitHTTPURL(args), deepAgentStepExplicitHTTPURL(step)); explicitURL != "" {
			args["url"] = explicitURL
			args["prompt"] = p.modelPromptForStep(state, step)
			args["expected_evidence"] = "URL, screenshot or DOM assertion evidence, and source refs if applicable"
		} else {
			mode = DeepAgentToolModeModel
			route.Mode = mode
			route.Executor = deepAgentRouteExecutorModel
			route.AllowedTools = webResearchAllowedTools()
			route.SearchScope = "web"
			args["prompt"] = p.modelPromptForStep(state, step)
			args["expected_evidence"] = "WebSearch/WebFetch source URLs, fetched page evidence, and key facts"
		}
	case DeepAgentToolModeCodePatch:
		args["prompt"] = p.modelPromptForStep(state, step)
		args["expected_evidence"] = "diff summary, changed files, and verification hints"
	case DeepAgentToolModeConnector:
		args["query"] = firstNonEmptyString(step.Intent, step.Title, stateGoal(state))
		args["provider"] = firstNonEmptyString(deepAgentWorkflowString(args, "provider"), "github")
	case DeepAgentToolModeMulti:
		args["task"] = p.modelPromptForStep(state, step)
		branchBudget := p.deepAgentParallelBranchBudget()
		maxBranches := p.deepAgentParallelMaxBranches()
		if deepAgentStepRequestsCoverageFanout(step) {
			args["branch_specs"] = deepAgentParallelCoverageBranchSpecs(firstNonEmptyString(step.Intent, step.Title, stateGoal(state)), maxBranches)
			args["max_concurrency"] = maxBranches
			args["min_successful_branches"] = 1
		} else {
			args["branch_specs"] = singleDeepAgentParallelBranchSpec(
				firstNonEmptyString(step.ID, "primary"),
				firstNonEmptyString(step.Title, step.Intent, "Primary objective"),
				firstNonEmptyString(step.Intent, step.Title, stateGoal(state)),
			)
			args["max_concurrency"] = 1
			args["min_successful_branches"] = 1
		}
		args["max_branches"] = maxBranches
		args["branch_timeout_ms"] = branchBudget.TimeoutMS
		args["branch_max_tool_calls"] = branchBudget.MaxToolCalls
		args["branch_max_sources"] = branchBudget.MaxSources
		args["branch_max_tokens"] = branchBudget.MaxTokens
		if len(route.AllowedTools) == 0 {
			route.AllowedTools = []string{"WebSearch", "WebFetch"}
		}
	default:
		mode = DeepAgentToolModeModel
		args["prompt"] = p.modelPromptForStep(state, step)
	}
	if len(route.AllowedTools) > 0 {
		args["allowed_tools"] = append([]string(nil), route.AllowedTools...)
	}
	if strings.TrimSpace(route.DeliverableType) != "" {
		args["deliverable_type"] = route.DeliverableType
	}
	if strings.TrimSpace(route.FilenameHint) != "" {
		args["filename_hint"] = route.FilenameHint
	}
	if strings.TrimSpace(route.SearchScope) != "" {
		args["search_scope"] = route.SearchScope
	}
	if state != nil && state.WorkingMemory != nil {
		if userID := deepAgentWorkflowString(state.WorkingMemory, "user_id"); userID != "" {
			args["user_id"] = firstNonEmptyString(deepAgentWorkflowString(args, "user_id"), userID)
		}
		if sessionID := deepAgentWorkflowString(state.WorkingMemory, "session_id"); sessionID != "" {
			args["session_id"] = firstNonEmptyString(deepAgentWorkflowString(args, "session_id"), sessionID)
		}
		if jobID := deepAgentWorkflowString(state.WorkingMemory, "job_id"); jobID != "" {
			args["job_id"] = firstNonEmptyString(deepAgentWorkflowString(args, "job_id"), jobID)
		}
		if args["source_policy"] == nil {
			if raw := state.WorkingMemory["source_policy"]; raw != nil {
				args["source_policy"] = normalizeLoopContractSourcePolicy(deepAgentSourcePolicyFromAny(raw), stateGoal(state))
			}
		}
	}
	if args["source_policy"] == nil && state != nil && state.LoopContract.ID != "" {
		args["source_policy"] = normalizeLoopContractSourcePolicy(state.LoopContract.SourcePolicy, stateGoal(state))
	}
	if normalizeDeepAgentRouteMode(mode) == DeepAgentToolModeModel && strings.EqualFold(strings.TrimSpace(route.SearchScope), "web") {
		sourcePolicy := deepAgentSourcePolicyFromAny(args["source_policy"])
		sourcePolicy = normalizeLoopContractSourcePolicy(sourcePolicy, stateGoal(state))
		args["source_policy"] = sourcePolicy
		if prompt := deepAgentWorkflowString(args, "prompt"); prompt != "" && !strings.Contains(prompt, "Source policy:") {
			args["prompt"] = strings.TrimSpace(prompt + "\n\n" + deepAgentSourcePolicyPrompt(sourcePolicy))
		}
	}
	attempt := deepAgentStepAttemptCount(state, step.ID) + 1
	if attempt > 1 {
		args["attempt"] = attempt
		args["retry_instruction"] = fmt.Sprintf("Previous attempt %d for step %q did not satisfy the success criteria. Use a different strategy and produce evidence for: %s", attempt-1, firstNonEmptyString(step.Title, step.ID), step.DoneCondition)
		if mode == DeepAgentToolModeModel || mode == DeepAgentToolModeModelArtifact {
			args["prompt"] = strings.TrimSpace(deepAgentWorkflowString(args, "prompt") + "\n\nRetry instruction: " + deepAgentWorkflowString(args, "retry_instruction"))
		}
	}
	route.Mode = mode
	route.Executor = firstNonEmptyString(route.Executor, deepAgentExecutorForMode(mode))
	route.RequiresArtifact = route.RequiresArtifact || mode == DeepAgentToolModeModelArtifact
	route.Version = firstNonEmptyString(route.Version, "v1")
	routeMap := deepAgentStepRouteMap(route)
	return DeepAgentAction{
		StepID: step.ID,
		Tool:   mode,
		Args: mergeDeepAgentActionArgs(args, map[string]any{
			"goal":              stateGoal(state),
			"step_id":           step.ID,
			"step_title":        step.Title,
			"step_intent":       step.Intent,
			"done_condition":    step.DoneCondition,
			"success_criteria":  step.DoneCondition,
			"requires_artifact": route.RequiresArtifact,
			"step_route":        routeMap,
			"route_version":     route.Version,
		}),
	}, nil
}

func (p *RuntimeDeepAgentPlanner) deepAgentParallelMaxBranches() int {
	if p != nil && p.runtime != nil && p.runtime.config.LLMGovernanceProvider != nil {
		cfg := p.runtime.config.LLMGovernanceProvider().normalized()
		if cfg.MaxBranchCount > 0 {
			return cfg.MaxBranchCount
		}
	}
	return deepAgentParallelDefaultPrimaryBranches
}

func (p *RuntimeDeepAgentPlanner) deepAgentParallelBranchBudget() DeepAgentParallelBranchBudget {
	budget := DeepAgentParallelBranchBudget{
		TimeoutMS:    deepAgentParallelDefaultBranchTimeout.Milliseconds(),
		MaxToolCalls: deepAgentParallelDefaultMaxToolCalls,
		MaxSources:   deepAgentParallelMaxSourcesPerBranch,
		MaxTokens:    deepAgentParallelDefaultMaxTokens,
	}
	if p != nil && p.runtime != nil && p.runtime.config.LLMGovernanceProvider != nil {
		cfg := p.runtime.config.LLMGovernanceProvider().normalized()
		budget.TimeoutMS = cfg.ParallelBranchTimeout.Milliseconds()
		budget.MaxToolCalls = cfg.ParallelMaxToolCalls
		budget.MaxSources = cfg.MaxSourcesPerBranch
		budget.MaxTokens = cfg.ParallelMaxTokens
	}
	return budget
}

func deepAgentStepRequestsCoverageFanout(step DeepAgentStep) bool {
	text := strings.ToLower(strings.TrimSpace(strings.Join([]string{step.Title, step.Intent, step.DoneCondition}, "\n")))
	if !deepAgentContainsAny(text,
		"多方向", "多路", "多分支", "多个子任务", "拆成多个子任务", "拆分子任务",
		"fan-out", "fan out", "parallel branches", "multiple branches", "multiple subtopics", "coverage dimensions",
	) {
		return false
	}
	return len(deepAgentParallelCoverageDimensionsForText(text)) > 1
}

func (p *RuntimeDeepAgentPlanner) keywordRouteStep(step DeepAgentStep) string {
	text := strings.ToLower(strings.TrimSpace(strings.Join([]string{step.Intent, step.Title, step.DoneCondition}, "\n")))
	if text == "" {
		return ""
	}
	if deepAgentContainsAny(text,
		"获取历史", "历史消息", "上下文检索", "会话检索", "记忆检索", "previous conversation", "prior conversation",
		"rag", "search history", "message search", "conversation search", "session context",
	) {
		return DeepAgentToolModeRAGSearch
	}
	if deepAgentStepRequiresArtifact(step) {
		return DeepAgentToolModeModelArtifact
	}
	if deepAgentContainsAny(text,
		"搜索", "查询", "检索", "查找", "调研", "研究", "外部", "联网", "互联网", "官网", "产品", "竞品", "新闻",
		"web", "internet", "external", "current", "latest", "research",
	) {
		return DeepAgentToolModeModel
	}
	return ""
}

func deepAgentStepLooksLikeImageGeneration(step DeepAgentStep) bool {
	text := strings.ToLower(strings.TrimSpace(strings.Join([]string{step.Intent, step.Title, step.DoneCondition}, "\n")))
	if text == "" {
		return false
	}
	return deepAgentContainsAny(text,
		"生成图片", "生成图像", "画一", "画张", "画一张", "画一个", "生图", "绘制", "插画", "图片", "图像",
		"generate image", "generate an image", "generate a picture", "create image", "create an image",
		"render image", "draw ", "paint ", "illustration", "picture",
	)
}

func (p *RuntimeDeepAgentPlanner) llmRouteStep(ctx context.Context, agentState *DeepAgentState, step DeepAgentStep) string {
	if p == nil || p.runtime == nil {
		return ""
	}
	userID := deepAgentWorkflowString(stateWorkingMemory(agentState), "user_id")
	sessionID := deepAgentWorkflowString(stateWorkingMemory(agentState), "session_id")
	renderedPrompt := p.renderDeepAgentPromptForScope(ctx, PromptIDRuntimeDeepAgentModeClassifier, userID, sessionID,
		DeepAgentToolModeModel, DeepAgentToolModeModelArtifact, DeepAgentToolModeSkill, DeepAgentToolModeRAGSearch, DeepAgentToolModeTest, DeepAgentToolModeWeb, DeepAgentToolModeCodePatch, DeepAgentToolModeConnector, DeepAgentToolModeMulti,
		DeepAgentToolModeModel, DeepAgentToolModeModelArtifact, DeepAgentToolModeSkill, DeepAgentToolModeRAGSearch, DeepAgentToolModeTest, DeepAgentToolModeWeb, DeepAgentToolModeCodePatch, DeepAgentToolModeConnector, DeepAgentToolModeMulti, DeepAgentToolModeModel,
		strings.TrimSpace(firstNonEmptyString(step.Intent, step.Title)), strings.TrimSpace(step.DoneCondition), p.stepContextSummary(agentState, step))
	prompt := renderedPrompt.Content
	runner := p.runtime.runnerForScope(ctx, Scope{UserID: userID, SessionID: sessionID, Prompt: prompt})
	result, err := runner.RunGeneratedPrompt(WithPromptMetadata(ctx, renderedPrompt.Metadata), state.NewSession(""), prompt)
	if err != nil {
		return ""
	}
	mode := strings.ToLower(strings.TrimSpace(result.Output))
	switch {
	case strings.Contains(mode, DeepAgentToolModeModelArtifact):
		return DeepAgentToolModeModelArtifact
	case strings.Contains(mode, DeepAgentToolModeSkill):
		return DeepAgentToolModeSkill
	case strings.Contains(mode, DeepAgentToolModeRAGSearch) || strings.Contains(mode, "search"):
		return DeepAgentToolModeRAGSearch
	case strings.Contains(mode, DeepAgentToolModeTest) || strings.Contains(mode, "lint") || strings.Contains(mode, "typecheck"):
		return DeepAgentToolModeTest
	case strings.Contains(mode, DeepAgentToolModeWeb) || strings.Contains(mode, "browser") || strings.Contains(mode, "screenshot"):
		return DeepAgentToolModeWeb
	case strings.Contains(mode, DeepAgentToolModeCodePatch) || strings.Contains(mode, "patch") || strings.Contains(mode, "diff"):
		return DeepAgentToolModeCodePatch
	case strings.Contains(mode, DeepAgentToolModeConnector) || strings.Contains(mode, "github"):
		return DeepAgentToolModeConnector
	case strings.Contains(mode, DeepAgentToolModeMulti):
		if deepAgentStepRequiresArtifact(step) {
			return DeepAgentToolModeModelArtifact
		}
		return DeepAgentToolModeMulti
	case strings.Contains(mode, DeepAgentToolModeModel):
		return DeepAgentToolModeModel
	default:
		return ""
	}
}

func (p *RuntimeDeepAgentPlanner) selectSkillForStep(step DeepAgentStep) (*skills.SkillDefinition, bool) {
	if p == nil || p.runtime == nil || p.runtime.skills == nil {
		return nil, false
	}
	text := strings.TrimSpace(strings.Join([]string{step.Intent, step.Title, step.DoneCondition}, "\n"))
	if skill, ok := p.runtime.skillForPrompt(text); ok && skill != nil && skill.UserInvocable && !skill.IsHidden {
		return skill, true
	}
	var best *skills.SkillDefinition
	bestScore := 0
	for _, skill := range p.runtime.skills.ListUserInvocableSkills() {
		if skill == nil || skill.IsHidden {
			continue
		}
		score := deepAgentStepSkillScore(text, skill)
		if score > bestScore {
			best = skill
			bestScore = score
		}
	}
	if best == nil || bestScore <= 0 {
		return nil, false
	}
	return best, true
}

func (p *RuntimeDeepAgentPlanner) modelPromptForStep(agentState *DeepAgentState, step DeepAgentStep) string {
	return p.modelPromptForStepContext(context.Background(), agentState, step)
}

func (p *RuntimeDeepAgentPlanner) modelPromptForStepContext(ctx context.Context, agentState *DeepAgentState, step DeepAgentStep) string {
	var b strings.Builder
	b.WriteString(p.deepAgentToolUsageReminder(ctx, agentState))
	b.WriteString("\n\n")
	if goal := stateGoal(agentState); goal != "" {
		b.WriteString("User goal:\n")
		b.WriteString(goal)
		b.WriteString("\n\n")
	}
	if loadedContext := deepAgentLoadedContextPrompt(stateWorkingMemory(agentState)); strings.TrimSpace(loadedContext) != "" {
		b.WriteString("Loaded task context:\n")
		b.WriteString(loadedContext)
		b.WriteString("\n\n")
	}
	if contextSummary := p.stepContextSummary(agentState, step); strings.TrimSpace(contextSummary) != "" {
		b.WriteString("Prior step context:\n")
		b.WriteString(contextSummary)
		b.WriteString("\n\n")
	}
	b.WriteString("Current step intent:\n")
	b.WriteString(firstNonEmptyString(step.Intent, step.Title))
	if strings.TrimSpace(step.DoneCondition) != "" {
		b.WriteString("\n\nSuccess criteria:\n")
		b.WriteString(step.DoneCondition)
	}
	if !deepAgentStepRequiresArtifact(step) {
		b.WriteString("\n\nStep boundary:\n")
		b.WriteString("This is not a deliverable-file step. Do not create, apologize for, or discuss report/document/Skill generation here; only complete the current step intent.")
	}
	return b.String()
}

func deepAgentToolUsageReminder() string {
	return PromptDeepAgentToolUsageReminder
}

func (p *RuntimeDeepAgentPlanner) deepAgentToolUsageReminder(ctx context.Context, agentState *DeepAgentState) string {
	userID := deepAgentWorkflowString(stateWorkingMemory(agentState), "user_id")
	sessionID := deepAgentWorkflowString(stateWorkingMemory(agentState), "session_id")
	rendered := p.renderDeepAgentPromptForScope(ctx, PromptIDRuntimeDeepAgentToolUsageReminder, userID, sessionID)
	if strings.TrimSpace(rendered.Content) == "" {
		return PromptDeepAgentToolUsageReminder
	}
	return rendered.Content
}

func deepAgentStepRequiresArtifact(step DeepAgentStep) bool {
	text := strings.Join([]string{step.Title, step.Intent, step.DoneCondition}, "\n")
	return deepAgentTextRequiresArtifact(text)
}

func deepAgentTextRequiresArtifact(text string) bool {
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" {
		return false
	}
	if deepAgentContainsAny(text,
		"artifact", "download", "downloadable", "file", ".md", ".docx", ".xlsx", ".pptx", "markdown",
		"可下载", "产物", "文件", "导出",
	) {
		return true
	}
	hasDeliverableNoun := deepAgentContainsAny(text, "report", "document", "文档", "报告")
	if !hasDeliverableNoun {
		return false
	}
	if deepAgentContainsAny(text,
		"后续", "支撑", "下一步", "下一阶段", "后面",
		"用于", "用来", "以便", "为了后续", "为撰写", "作为素材", "提供素材", "提供资料", "提供材料", "参考资料",
		"later", "subsequent", "next step", "next-step", "next phase", "support later", "support subsequent",
		"for generating", "for creating", "for writing", "can be used to", "used to generate", "used to create",
		"provide material", "provide materials", "provide input", "material for", "materials for", "input for",
	) &&
		!deepAgentContainsAny(text, "artifact", "download", "downloadable", "可下载", "产物", "导出", "保存") {
		return false
	}
	return deepAgentContainsAny(text,
		"generate", "generated", "create", "created", "write", "written", "produce", "produced", "deliver", "delivered", "save", "saved", "final",
		"生成", "创建", "撰写", "编写", "写作", "输出", "交付", "保存", "制作", "最终",
	)
}

func (p *RuntimeDeepAgentPlanner) skillArgsForStep(agentState *DeepAgentState, step DeepAgentStep) string {
	var parts []string
	if goal := stateGoal(agentState); goal != "" {
		parts = append(parts, "用户目标：\n"+goal)
	}
	if contextSummary := p.stepContextSummary(agentState, step); strings.TrimSpace(contextSummary) != "" {
		parts = append(parts, "前置步骤输出：\n"+contextSummary)
	}
	parts = append(parts, "当前步骤意图：\n"+firstNonEmptyString(step.Intent, step.Title))
	if strings.TrimSpace(step.DoneCondition) != "" {
		parts = append(parts, "成功标准：\n"+step.DoneCondition)
	}
	return strings.Join(parts, "\n\n")
}

func (p *RuntimeDeepAgentPlanner) stepContextSummary(agentState *DeepAgentState, step DeepAgentStep) string {
	if agentState == nil || agentState.WorkingMemory == nil {
		return ""
	}
	store, _ := agentState.WorkingMemory["step_context"].(map[string]any)
	if len(store) == 0 {
		return ""
	}
	var ids []string
	if len(step.DependsOn) > 0 {
		ids = append(ids, step.DependsOn...)
	} else {
		for _, prior := range agentState.Plan.Steps {
			if prior.ID == step.ID {
				break
			}
			if _, ok := store[prior.ID]; ok {
				ids = append(ids, prior.ID)
			}
		}
	}
	var parts []string
	for _, id := range ids {
		record, _ := store[id].(map[string]any)
		if len(record) == 0 {
			continue
		}
		summary := firstNonEmptyString(deepAgentWorkflowString(record, "summary"), deepAgentWorkflowString(record, "output"))
		if summary == "" {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s: %s", id, summary))
	}
	return strings.Join(parts, "\n\n")
}

func deepAgentStepSkillScore(text string, skill *skills.SkillDefinition) int {
	if skill == nil {
		return 0
	}
	text = strings.ToLower(text)
	haystack := strings.ToLower(strings.Join([]string{skill.Name, skill.DisplayName, skill.Description, skill.WhenToUse, skill.ArgumentHint}, "\n"))
	score := 0
	for _, token := range []string{
		"docx", "word", "document", "文档", "报告", "report", "markdown", ".md", "file", "artifact", "文件",
		"image", "picture", "draw", "render", "illustration", "图片", "图像", "画", "生图", "绘制",
	} {
		if strings.Contains(text, token) && strings.Contains(haystack, token) {
			score += 4
		}
	}
	for _, token := range strings.Fields(strings.NewReplacer("\n", " ", "，", " ", "。", " ", ",", " ", ".", " ").Replace(text)) {
		if len([]rune(token)) >= 2 && strings.Contains(haystack, token) {
			score++
		}
	}
	if skillProducesArtifacts(skill) && deepAgentContainsAny(text,
		"artifact", "download", "file", "markdown", "docx", "image", "picture", "render", "illustration",
		"文档", "报告", "文件", "图片", "图像", "可下载", "产物",
	) {
		score += 5
	}
	return score
}

func deepAgentContainsAny(text string, needles ...string) bool {
	text = strings.ToLower(strings.TrimSpace(text))
	for _, needle := range needles {
		if strings.Contains(text, strings.ToLower(strings.TrimSpace(needle))) {
			return true
		}
	}
	return false
}

func stateWorkingMemory(agentState *DeepAgentState) map[string]any {
	if agentState == nil {
		return nil
	}
	return agentState.WorkingMemory
}

func stateGoal(agentState *DeepAgentState) string {
	if agentState == nil {
		return ""
	}
	return strings.TrimSpace(agentState.Goal)
}

type RuntimeDeepAgentExecutor struct {
	runtime *Runtime
}

func NewRuntimeDeepAgentExecutor(runtime *Runtime) *RuntimeDeepAgentExecutor {
	return &RuntimeDeepAgentExecutor{runtime: runtime}
}

func (e *RuntimeDeepAgentExecutor) ExecuteDeepAgentAction(ctx context.Context, action DeepAgentAction, agentState *DeepAgentState) (DeepAgentActionResult, error) {
	if e == nil || e.runtime == nil {
		return DeepAgentActionResult{Status: DeepAgentActionStatusFailed, Error: "runtime deep agent executor is not configured"}, fmt.Errorf("runtime deep agent executor is not configured")
	}
	route, ok := deepAgentStepRouteFromMap(action.Args)
	if !ok {
		route = DeepAgentStepRoute{
			StepID:           firstNonEmptyString(action.StepID, deepAgentActionString(action, "step_id")),
			Mode:             normalizeDeepAgentRouteMode(action.Tool),
			Executor:         deepAgentExecutorForMode(action.Tool),
			SkillName:        strings.TrimPrefix(firstNonEmptyString(deepAgentActionString(action, "skill_name"), deepAgentActionString(action, "skill")), "/"),
			RequiresArtifact: deepAgentActionRequiresArtifact(action),
			DeliverableType:  firstNonEmptyString(deepAgentActionString(action, "deliverable_type"), deepAgentDeliverableNone),
			AllowedTools:     deepAgentStringSlice(action.Args["allowed_tools"]),
			SearchScope:      deepAgentActionString(action, "search_scope"),
			SuccessCriteria:  deepAgentStringSlice(action.Args["success_criteria"]),
			Reason:           "legacy action route inference",
			Confidence:       "medium",
		}
	}
	return NewRuntimeDeepAgentExecutorRegistry(e.runtime, e).ExecuteAction(ctx, route, action, agentState)
}

func (e *RuntimeDeepAgentExecutor) executeModelAction(ctx context.Context, action DeepAgentAction, agentState *DeepAgentState, forceArtifact bool) (DeepAgentActionResult, error) {
	userID := deepAgentActionString(action, "user_id")
	sessionID := deepAgentActionString(action, "session_id")
	if agentState != nil && agentState.WorkingMemory != nil {
		userID = firstNonEmptyString(userID, deepAgentWorkflowString(agentState.WorkingMemory, "user_id"))
		sessionID = firstNonEmptyString(sessionID, deepAgentWorkflowString(agentState.WorkingMemory, "session_id"))
	}
	prompt := firstNonEmptyString(
		deepAgentActionString(action, "prompt"),
		deepAgentActionString(action, "instruction"),
		deepAgentActionString(action, "query"),
		deepAgentActionString(action, "step_title"),
		action.StepID,
	)
	if strings.TrimSpace(prompt) == "" {
		return DeepAgentActionResult{Status: DeepAgentActionStatusFailed, Error: "model action prompt is required"}, fmt.Errorf("model action prompt is required")
	}
	requiresArtifact := forceArtifact || deepAgentActionRequiresArtifact(action)
	allowedTools := deepAgentModelActionAllowedTools(action, agentState, forceArtifact)
	session, err := e.deepAgentSession(ctx, userID, sessionID)
	if err != nil {
		return DeepAgentActionResult{Status: DeepAgentActionStatusFailed, Error: err.Error()}, err
	}
	if err := e.runtime.injectSessionRuntimeContexts(ctx, userID, session); err != nil {
		return DeepAgentActionResult{Status: DeepAgentActionStatusFailed, Error: err.Error()}, err
	}
	if strings.TrimSpace(sessionID) == "" && userID != "" && strings.TrimSpace(session.ID) != "" {
		if saveErr := e.runtime.sessions.Save(ctx, userID, session); saveErr != nil {
			return DeepAgentActionResult{Status: DeepAgentActionStatusFailed, Error: saveErr.Error(), Retryable: true}, saveErr
		}
	}
	priorArtifactRefs := e.deepAgentPriorArtifactRefs(ctx, userID, session.ID, agentState, action)
	if deepAgentPriorArtifactSatisfiesGenericDocument(agentState, requiresArtifact, priorArtifactRefs) {
		return DeepAgentActionResult{
			Status:    DeepAgentActionStatusSucceeded,
			Output:    "A prior DeepAgent step already created an artifact that satisfies this deliverable.",
			Completed: true,
			Metadata: map[string]any{
				"tool":                             firstNonEmptyString(strings.TrimSpace(action.Tool), DeepAgentToolModeModel),
				"session_id":                       session.ID,
				"artifact_count":                   len(priorArtifactRefs),
				"artifact_refs":                    priorArtifactRefs,
				"artifact_satisfied_by_prior_step": true,
				"tool_result_valid":                true,
				"allowed_tools":                    append([]string(nil), allowedTools...),
			},
		}, nil
	}
	scope := Scope{
		UserID:     userID,
		SessionID:  session.ID,
		WorkingDir: session.WorkingDir,
		Prompt:     prompt,
	}
	if len(allowedTools) > 0 {
		scope.AllowedTools = allowedTools
	}
	runner := e.runtime.runnerForScope(ctx, scope)
	beforeArtifacts := e.deepAgentArtifactIDSet(ctx, userID, session.ID)
	startMessageCount := len(session.Messages)
	result, hiddenPromptCount, err := runDeepAgentExecutionPrompt(ctx, runner, session, prompt, startMessageCount)
	if err != nil && !errors.Is(err, skilltool.ErrRunAsJobRequired) {
		return DeepAgentActionResult{Status: DeepAgentActionStatusFailed, Error: err.Error(), Retryable: true}, err
	}
	resultSession := result.Session
	if resultSession == nil {
		resultSession = session
	}
	if selected, ok := selectedRunAsJobSkill(resultSession, startMessageCount); ok {
		metadata := map[string]any{
			"tool":                firstNonEmptyString(strings.TrimSpace(action.Tool), DeepAgentToolModeModel),
			"session_id":          resultSession.ID,
			"artifact_count":      0,
			"tool_result_valid":   true,
			"skill_name":          selected.Name,
			"run_as_job_marker":   true,
			"prompt_mode":         "hidden_user_turn",
			"hidden_user_prompts": hiddenPromptCount,
		}
		if resultSession != nil && userID != "" && strings.TrimSpace(resultSession.ID) != "" {
			if saveErr := e.runtime.sessions.Save(ctx, userID, resultSession); saveErr != nil {
				return DeepAgentActionResult{Status: DeepAgentActionStatusFailed, Error: saveErr.Error(), Retryable: true, Metadata: metadata}, saveErr
			}
		}
		job, jobErr := e.runtime.createSelectedSkillJob(ctx, ChatRequest{
			UserID:    userID,
			SessionID: resultSession.ID,
			Content:   prompt,
		}, resultSession.ID, selected)
		if jobErr != nil {
			return DeepAgentActionResult{Status: DeepAgentActionStatusFailed, Error: jobErr.Error(), Retryable: true, Metadata: metadata}, jobErr
		}
		metadata["job_started"] = true
		metadata["child_job_id"] = job.ID
		metadata["child_job_type"] = job.Type
		e.runtime.markJobUserMessageHidden(job.ID)
		childResult, childErr := e.runDeepAgentChildJob(ctx, job, metadata, beforeArtifacts, action)
		if childErr != nil {
			return childResult, childErr
		}
		return childResult, nil
	}
	if errors.Is(err, skilltool.ErrRunAsJobRequired) {
		err := fmt.Errorf("model selected run-as-job skill but no valid skill marker was found")
		return DeepAgentActionResult{Status: DeepAgentActionStatusFailed, Error: err.Error(), Retryable: true}, err
	}
	diagnostics := collectSkillExecutionDiagnostics(resultSession, startMessageCount)
	newArtifactRefs := e.deepAgentNewArtifactRefs(ctx, userID, resultSession.ID, beforeArtifacts, action, "artifact_tool")
	storeArtifactCount := e.deepAgentNewArtifactCount(ctx, userID, resultSession.ID, beforeArtifacts)
	artifactCount := diagnostics.ArtifactCount
	if storeArtifactCount > artifactCount {
		artifactCount = storeArtifactCount
	}
	if len(newArtifactRefs) > int(artifactCount) {
		artifactCount = int64(len(newArtifactRefs))
	}
	metadata := map[string]any{
		"tool":                firstNonEmptyString(strings.TrimSpace(action.Tool), DeepAgentToolModeModel),
		"session_id":          resultSession.ID,
		"artifact_count":      artifactCount,
		"tool_result_valid":   diagnostics.ErrorKind == "" && diagnostics.SkillError == "",
		"prompt_mode":         "hidden_user_turn",
		"hidden_user_prompts": hiddenPromptCount,
	}
	sourcePolicy := deepAgentSourcePolicyFromAction(action)
	for key, value := range deepAgentModelActionEvidenceMetadata(result.Output, resultSession, startMessageCount, sourcePolicy) {
		metadata[key] = value
	}
	if len(newArtifactRefs) > 0 {
		metadata["artifact_refs"] = newArtifactRefs
	}
	if len(allowedTools) > 0 {
		metadata["allowed_tools"] = append([]string(nil), allowedTools...)
	}
	if storeArtifactCount > 0 && diagnostics.ArtifactCount == 0 {
		metadata["artifact_detected_from_store"] = true
	}
	fallbackOutput := deepAgentModelArtifactFallbackOutput(result.Output, resultSession, startMessageCount)
	invalidFallbackOutput := requiresArtifact && deepAgentModelArtifactFallbackLooksInvalid(fallbackOutput)
	if invalidFallbackOutput {
		metadata["artifact_fallback_invalid"] = true
	}
	hiddenAssistantMessages := hideDeepAgentExecutionAssistantMessages(resultSession, startMessageCount)
	if hiddenAssistantMessages > 0 {
		metadata["hidden_assistant_messages"] = hiddenAssistantMessages
	}
	artifactSatisfiedOutput := ""
	priorArtifactSatisfies := false
	if artifactCount == 0 {
		priorArtifactRefs = e.deepAgentPriorArtifactRefs(ctx, userID, resultSession.ID, agentState, action)
		if satisfied := deepAgentPriorArtifactSatisfiesGenericDocument(agentState, requiresArtifact, priorArtifactRefs); satisfied {
			priorArtifactSatisfies = true
		}
	}
	priorArtifactCount := len(priorArtifactRefs)
	if artifactCount == 0 && priorArtifactSatisfies && priorArtifactCount > 0 {
		artifactCount = int64(priorArtifactCount)
		metadata["artifact_count"] = priorArtifactCount
		metadata["artifact_refs"] = priorArtifactRefs
		metadata["artifact_satisfied_by_prior_step"] = true
		artifactSatisfiedOutput = "A prior DeepAgent step already created an artifact that satisfies this deliverable."
	}
	if artifactCount == 0 && requiresArtifact && fallbackOutput != "" && !invalidFallbackOutput {
		if resultSession != nil && userID != "" && strings.TrimSpace(resultSession.ID) != "" {
			if saveErr := e.runtime.sessions.Save(ctx, userID, resultSession); saveErr != nil {
				return DeepAgentActionResult{Status: DeepAgentActionStatusFailed, Error: saveErr.Error(), Retryable: true, Metadata: metadata}, saveErr
			}
		}
		artifact, artifactErr := e.createDeepAgentModelArtifact(ctx, userID, resultSession.ID, action, fallbackOutput)
		if artifactErr != nil {
			return DeepAgentActionResult{Status: DeepAgentActionStatusFailed, Error: artifactErr.Error(), Retryable: true, Metadata: metadata}, artifactErr
		}
		artifactCount = 1
		metadata["artifact_count"] = 1
		metadata["artifact_id"] = artifact.ID
		metadata["artifact_filename"] = artifact.Filename
		metadata["artifact_content_type"] = artifact.ContentType
		metadata["artifact_refs"] = []DeepAgentArtifactRef{deepAgentArtifactRefFromActionArtifact(artifact, action, "fallback")}
		metadata["artifact_fallback"] = true
		recordDeepAgentArtifactToolResult(resultSession, action, artifact)
	}
	if artifactCount == 0 && requiresArtifact {
		modelDetails := deepAgentModelActionDiagnostics(result.Output, resultSession, startMessageCount)
		diagnosticDetails := map[string]any{
			"diagnostics_artifact_count":  diagnostics.ArtifactCount,
			"store_artifact_count":        storeArtifactCount,
			"has_fallback_output":         fallbackOutput != "",
			"fallback_output_invalid":     invalidFallbackOutput,
			"prior_artifact_count":        priorArtifactCount,
			"fallback_output_length":      len(fallbackOutput),
			"artifact_service_configured": e.runtime.artifacts != nil,
			"session_messages_count":      len(resultSession.Messages),
			"user_id_present":             strings.TrimSpace(userID) != "",
			"session_id_present":          strings.TrimSpace(resultSession.ID) != "",
			"requires_artifact":           requiresArtifact,
			"force_artifact":              forceArtifact,
			"allowed_tools":               append([]string(nil), allowedTools...),
			"route":                       action.Args["step_route"],
			"output_preview":              truncateDeepAgentDiagnosticText(firstNonEmptyString(result.Output, fallbackOutput), 240),
		}
		for key, value := range modelDetails {
			diagnosticDetails[key] = value
		}
		metadata["diagnostic_details"] = diagnosticDetails

		err := fmt.Errorf("model_artifact action produced no artifact or report content: diagnostics_count=%d, store_count=%d, has_fallback=%v, artifact_service=%v, output_len=%d, assistant_messages=%d, tool_calls=%d, tool_results=%d, artifact_tool_results=%d",
			diagnostics.ArtifactCount,
			storeArtifactCount,
			fallbackOutput != "",
			e.runtime.artifacts != nil,
			modelDetails["output_length"],
			modelDetails["assistant_messages"],
			modelDetails["tool_calls"],
			modelDetails["tool_results"],
			modelDetails["artifact_tool_results"],
		)
		return DeepAgentActionResult{Status: DeepAgentActionStatusFailed, Error: err.Error(), Retryable: true, Metadata: metadata}, err
	}
	if resultSession != nil && userID != "" && strings.TrimSpace(resultSession.ID) != "" {
		if saveErr := e.runtime.sessions.Save(ctx, userID, resultSession); saveErr != nil {
			return DeepAgentActionResult{Status: DeepAgentActionStatusFailed, Error: saveErr.Error(), Retryable: true}, saveErr
		}
	}
	return DeepAgentActionResult{
		Status:    DeepAgentActionStatusSucceeded,
		Output:    deepAgentModelActionUserOutput(artifactSatisfiedOutput, result.Output, fallbackOutput, metadata, artifactCount),
		Completed: true,
		Metadata:  metadata,
	}, nil
}

func deepAgentModelActionUserOutput(artifactSatisfiedOutput, resultOutput, fallbackOutput string, metadata map[string]any, artifactCount int64) string {
	if ready := deepAgentArtifactReadyOutput(metadata, artifactCount); ready != "" {
		return ready
	}
	return firstNonEmptyString(artifactSatisfiedOutput, resultOutput, fallbackOutput)
}

func deepAgentArtifactReadyOutput(metadata map[string]any, artifactCount int64) string {
	if artifactCount <= 0 {
		return ""
	}
	refs := deepAgentArtifactRefsFromAny(metadata["artifact_refs"])
	names := make([]string, 0, len(refs))
	seen := map[string]struct{}{}
	for _, ref := range refs {
		name := strings.TrimSpace(ref.Filename)
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		names = append(names, name)
	}
	switch {
	case len(names) == 1:
		return fmt.Sprintf("已生成 artifact %s，可在 Artifacts 面板查看。", names[0])
	case len(names) > 1:
		return fmt.Sprintf("已生成 %d 个 artifacts（%s），可在 Artifacts 面板查看。", len(names), strings.Join(names, ", "))
	case artifactCount == 1:
		return "已生成 artifact，可在 Artifacts 面板查看。"
	default:
		return fmt.Sprintf("已生成 %d 个 artifacts，可在 Artifacts 面板查看。", artifactCount)
	}
}

func runDeepAgentExecutionPrompt(ctx context.Context, runner Runner, session *state.Session, prompt string, startMessageCount int) (engine.Result, int, error) {
	result, err := runner.Run(ctx, session, prompt)
	resultSession := result.Session
	if resultSession == nil {
		resultSession = session
		result.Session = session
	}
	hiddenCount := hideDeepAgentExecutionUserPrompts(resultSession, startMessageCount, prompt)
	return result, hiddenCount, err
}

func deepAgentModelActionEvidenceMetadata(output string, session *state.Session, startIndex int, sourcePolicy LoopContractSourcePolicy) map[string]any {
	metadata := map[string]any{}
	details := deepAgentModelActionDiagnostics(output, session, startIndex)
	metadata["diagnostic_details"] = details
	if names := deepAgentStringSlice(details["assistant_tool_names"]); len(names) > 0 {
		metadata["assistant_tool_names"] = names
	}
	if names := deepAgentStringSlice(details["tool_result_names"]); len(names) > 0 {
		metadata["tool_result_names"] = names
	}
	sources, sourcePolicyReport := deepAgentModelActionSourceRefs(output, session, startIndex, sourcePolicy)
	metadata["source_policy"] = normalizeLoopContractSourcePolicy(sourcePolicy, "")
	metadata["source_policy_report"] = sourcePolicyReport
	if len(sources) > 0 {
		metadata["sources"] = sources
	}
	return metadata
}

func deepAgentModelActionSourceRefs(output string, session *state.Session, startIndex int, sourcePolicy LoopContractSourcePolicy) ([]DeepAgentSourceRef, deepAgentSourcePolicyReport) {
	seen := map[string]struct{}{}
	out := make([]DeepAgentSourceRef, 0)
	appendRefs := func(refs []DeepAgentSourceRef) {
		for _, ref := range refs {
			key := strings.ToLower(strings.TrimSpace(firstNonEmptyString(ref.URL, ref.Title+"|"+ref.Provider)))
			if key == "" {
				continue
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, ref)
		}
	}
	// Prefer sources actually returned by a web tool. Appending them first also
	// prevents a duplicate URL echoed by the model from downgrading provenance.
	if session != nil {
		if startIndex < 0 || startIndex > len(session.Messages) {
			startIndex = 0
		}
		for _, message := range session.Messages[startIndex:] {
			if message.Role != state.MessageRoleTool {
				continue
			}
			if !strings.EqualFold(message.ToolName, "WebSearch") && !strings.EqualFold(message.ToolName, "WebFetch") {
				continue
			}
			appendRefs(deepAgentSourceRefsFromToolOutput(message.ToolOutput, message.ToolName))
		}
	}
	appendRefs(deepAgentSourceRefsFromText(output))
	return curateDeepAgentSourceRefsWithPolicy(out, deepAgentModelActionMaxSources, sourcePolicy)
}

func hideDeepAgentExecutionUserPrompts(session *state.Session, startMessageCount int, prompt string) int {
	if session == nil {
		return 0
	}
	if startMessageCount < 0 || startMessageCount > len(session.Messages) {
		startMessageCount = 0
	}
	prompt = strings.TrimSpace(prompt)
	hiddenCount := 0
	for i := startMessageCount; i < len(session.Messages); i++ {
		message := &session.Messages[i]
		if message.Role != state.MessageRoleUser {
			continue
		}
		if prompt != "" && strings.TrimSpace(message.Content) != prompt {
			continue
		}
		if !message.Hidden {
			message.Hidden = true
		}
		hiddenCount++
	}
	return hiddenCount
}

func hideDeepAgentExecutionAssistantMessages(session *state.Session, startMessageCount int) int {
	if session == nil {
		return 0
	}
	if startMessageCount < 0 || startMessageCount > len(session.Messages) {
		startMessageCount = 0
	}
	hiddenCount := 0
	for i := startMessageCount; i < len(session.Messages); i++ {
		message := &session.Messages[i]
		if message.Role != state.MessageRoleAssistant {
			continue
		}
		if !message.Hidden {
			message.Hidden = true
		}
		hiddenCount++
	}
	return hiddenCount
}

func (e *RuntimeDeepAgentExecutor) deepAgentArtifactIDSet(ctx context.Context, userID, sessionID string) map[string]struct{} {
	if e == nil || e.runtime == nil || strings.TrimSpace(userID) == "" || strings.TrimSpace(sessionID) == "" {
		return nil
	}
	artifacts, err := e.runtime.ListArtifacts(ctx, userID, sessionID)
	if err != nil {
		return nil
	}
	out := make(map[string]struct{}, len(artifacts))
	for _, artifact := range artifacts {
		if artifact == nil || strings.TrimSpace(artifact.ID) == "" {
			continue
		}
		out[artifact.ID] = struct{}{}
	}
	return out
}

func deepAgentModelActionAllowedTools(action DeepAgentAction, agentState *DeepAgentState, forceArtifact bool) []string {
	if tools := deepAgentStringSlice(action.Args["allowed_tools"]); len(tools) > 0 {
		return tools
	}
	goal := stateGoal(agentState)
	if goal == "" {
		goal = deepAgentActionString(action, "goal")
	}
	if !deepAgentGenericDocumentArtifactGoal(goal) || deepAgentExplicitDocxText(goal) {
		return nil
	}
	if !forceArtifact {
		return []string{"WebSearch", "WebFetch"}
	}
	return []string{"WebSearch", "WebFetch", ArtifactToolName}
}

func deepAgentGenericDocumentArtifactGoal(text string) bool {
	return deepAgentContainsAny(text,
		"report", "document", "markdown", ".md",
		"报告", "文档", "调研", "调查", "研究报告", "调研报告", "调研文档",
	)
}

func (e *RuntimeDeepAgentExecutor) deepAgentPriorArtifactRefs(ctx context.Context, userID, sessionID string, agentState *DeepAgentState, action DeepAgentAction) []DeepAgentArtifactRef {
	refs := deepAgentStateCurrentArtifactRefs(agentState)
	if e != nil {
		refs = append(refs, e.deepAgentNewArtifactRefs(ctx, userID, sessionID, map[string]struct{}{}, action, "prior")...)
	}
	expectedJobID := deepAgentActionString(action, "job_id")
	expectedRunID := deepAgentActionString(action, "run_id")
	seen := map[string]struct{}{}
	out := make([]DeepAgentArtifactRef, 0, len(refs))
	for _, ref := range refs {
		key := firstNonEmptyString(ref.ID, ref.Filename)
		if strings.TrimSpace(key) == "" {
			continue
		}
		if !deepAgentPriorArtifactRefMatchesScope(ref, expectedJobID, expectedRunID) {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, ref)
	}
	return out
}

func deepAgentPriorArtifactRefMatchesScope(ref DeepAgentArtifactRef, expectedJobID, expectedRunID string) bool {
	expectedJobID = strings.TrimSpace(expectedJobID)
	expectedRunID = strings.TrimSpace(expectedRunID)
	if expectedJobID != "" && strings.TrimSpace(ref.JobID) != "" && strings.TrimSpace(ref.JobID) != expectedJobID {
		return false
	}
	if expectedRunID != "" && strings.TrimSpace(ref.RunID) != "" && strings.TrimSpace(ref.RunID) != expectedRunID {
		return false
	}
	return true
}

func deepAgentPriorArtifactSatisfiesGenericDocument(agentState *DeepAgentState, requiresArtifact bool, refs []DeepAgentArtifactRef) bool {
	if !requiresArtifact {
		return false
	}
	goal := stateGoal(agentState)
	if !deepAgentGenericDocumentArtifactGoal(goal) || deepAgentExplicitDocxText(goal) {
		return false
	}
	return len(refs) > 0
}

func deepAgentExplicitDocxText(text string) bool {
	return deepAgentTextRequestsDocx(text)
}

func (e *RuntimeDeepAgentExecutor) deepAgentNewArtifactCount(ctx context.Context, userID, sessionID string, before map[string]struct{}) int64 {
	return int64(len(e.deepAgentNewArtifactRefs(ctx, userID, sessionID, before, DeepAgentAction{}, "")))
}

func (e *RuntimeDeepAgentExecutor) deepAgentNewArtifactRefs(ctx context.Context, userID, sessionID string, before map[string]struct{}, action DeepAgentAction, source string) []DeepAgentArtifactRef {
	if e == nil || e.runtime == nil || strings.TrimSpace(userID) == "" || strings.TrimSpace(sessionID) == "" {
		return nil
	}
	artifacts, err := e.runtime.ListArtifacts(ctx, userID, sessionID)
	if err != nil {
		return nil
	}
	refs := make([]DeepAgentArtifactRef, 0)
	for _, artifact := range artifacts {
		if artifact == nil || strings.TrimSpace(artifact.ID) == "" {
			continue
		}
		if _, seen := before[artifact.ID]; !seen {
			refs = append(refs, deepAgentArtifactRefFromActionArtifact(artifact, action, source))
		}
	}
	return refs
}

func deepAgentArtifactRefFromActionArtifact(artifact *Artifact, action DeepAgentAction, source string) DeepAgentArtifactRef {
	ref := deepAgentArtifactRefFromArtifact(artifact, source)
	ref.StepID = firstNonEmptyString(ref.StepID, action.StepID, deepAgentActionString(action, "step_id"))
	return ref
}

func deepAgentModelArtifactFallbackOutput(output string, session *state.Session, startIndex int) string {
	if text := strings.TrimSpace(output); text != "" {
		return deepAgentUsableArtifactFallbackText(text)
	}
	if session == nil || len(session.Messages) == 0 {
		return ""
	}
	if startIndex < 0 || startIndex > len(session.Messages) {
		startIndex = 0
	}
	for i := len(session.Messages) - 1; i >= startIndex; i-- {
		message := session.Messages[i]
		if message.Hidden || message.Role != state.MessageRoleAssistant {
			continue
		}
		if text := strings.TrimSpace(message.Content); text != "" {
			return deepAgentUsableArtifactFallbackText(text)
		}
	}
	return ""
}

func deepAgentUsableArtifactFallbackText(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	text = deepAgentNormalizeArtifactFallbackMarkdown(text)
	if idx := deepAgentFirstMarkdownHeadingIndex(text); idx > 0 {
		return strings.TrimSpace(text[idx:])
	}
	return text
}

func deepAgentNormalizeArtifactFallbackMarkdown(text string) string {
	text = strings.TrimSpace(deepAgentDecodeLiteralEscapes(text))
	if text == "" {
		return ""
	}
	text = deepAgentStripToolResultPreamble(text)
	text = deepAgentStripPageTagEnvelope(text)
	title, body := deepAgentExtractLeadingTitleJSON(text)
	body = strings.TrimSpace(body)
	if title != "" && !deepAgentMarkdownStartsWithTitle(body, title) {
		body = "# " + title + "\n\n" + body
	}
	return body
}

func deepAgentMarkdownStartsWithTitle(text, title string) bool {
	text = strings.TrimSpace(text)
	title = strings.TrimSpace(title)
	return title != "" && (text == "# "+title || strings.HasPrefix(text, "# "+title+"\n"))
}

func deepAgentDecodeLiteralEscapes(text string) string {
	if strings.Count(text, `\n`) <= strings.Count(text, "\n") {
		return text
	}
	replacer := strings.NewReplacer(
		`\r\n`, "\n",
		`\n`, "\n",
		`\t`, "\t",
		`\"`, `"`,
		`\\`, `\`,
	)
	return replacer.Replace(text)
}

func deepAgentStripToolResultPreamble(text string) string {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "Here is the result of ") {
		return text
	}
	for _, marker := range []string{":\n<", ":\n\n<", ":\r\n<"} {
		if idx := strings.Index(text, marker); idx >= 0 {
			return strings.TrimSpace(text[idx+2:])
		}
	}
	return text
}

func deepAgentStripPageTagEnvelope(text string) string {
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "<page") {
		if idx := strings.Index(text, ">"); idx >= 0 {
			text = strings.TrimSpace(text[idx+1:])
		}
	}
	text = strings.TrimSpace(text)
	if idx := strings.LastIndex(text, "</page>"); idx >= 0 {
		text = strings.TrimSpace(text[:idx] + text[idx+len("</page>"):])
	}
	return text
}

func deepAgentExtractLeadingTitleJSON(text string) (string, string) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "{") {
		return "", text
	}
	lines := strings.SplitN(text, "\n", 2)
	firstLine := strings.TrimSpace(lines[0])
	var payload struct {
		Title string `json:"title"`
	}
	if err := json.Unmarshal([]byte(firstLine), &payload); err != nil || strings.TrimSpace(payload.Title) == "" {
		return "", text
	}
	rest := ""
	if len(lines) > 1 {
		rest = lines[1]
	}
	return strings.TrimSpace(payload.Title), strings.TrimSpace(rest)
}

func deepAgentFirstMarkdownHeadingIndex(text string) int {
	offset := 0
	for _, line := range strings.SplitAfter(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# ") || strings.HasPrefix(trimmed, "## ") || strings.HasPrefix(trimmed, "### ") {
			return offset + strings.Index(line, strings.TrimLeft(line, " \t"))
		}
		offset += len(line)
	}
	return -1
}

func deepAgentModelArtifactFallbackLooksInvalid(output string) bool {
	text := strings.ToLower(strings.TrimSpace(output))
	if text == "" {
		return false
	}
	if deepAgentFirstMarkdownHeadingIndex(strings.TrimSpace(output)) == 0 && len([]rune(output)) >= 200 {
		return false
	}
	if deepAgentContainsAny(text,
		"tool not found", "unknown tool", "工具未找到", "未找到工具", "技能未找到",
		"technical issue", "technical error", "encountered technical",
	) {
		return true
	}
	if deepAgentContainsAny(text, "skill", "技能", "docx") &&
		deepAgentContainsAny(text, "暂时无法使用", "无法使用", "不可用", "unavailable", "not available", "temporarily unavailable") {
		return true
	}
	if deepAgentContainsAny(text, "抱歉", "sorry", "apolog") &&
		deepAgentContainsAny(text, "无法", "不能", "失败", "cannot", "can't", "failed", "unable") {
		return true
	}
	return false
}

func deepAgentStatePriorArtifactCount(agentState *DeepAgentState) int {
	return len(deepAgentStateCurrentArtifactRefs(agentState))
}

func deepAgentModelActionDiagnostics(output string, session *state.Session, startIndex int) map[string]any {
	details := map[string]any{
		"output_length":          len(strings.TrimSpace(output)),
		"new_messages":           0,
		"assistant_messages":     0,
		"visible_assistant_text": 0,
		"tool_calls":             0,
		"tool_results":           0,
		"artifact_tool_results":  0,
		"assistant_tool_names":   []string{},
		"tool_result_names":      []string{},
		"last_assistant_preview": "",
	}
	if session == nil || len(session.Messages) == 0 {
		return details
	}
	if startIndex < 0 || startIndex > len(session.Messages) {
		startIndex = 0
	}
	messages := session.Messages[startIndex:]
	details["new_messages"] = len(messages)
	assistantToolNames := make([]string, 0)
	toolResultNames := make([]string, 0)
	for _, message := range messages {
		switch message.Role {
		case state.MessageRoleAssistant:
			details["assistant_messages"] = details["assistant_messages"].(int) + 1
			if text := strings.TrimSpace(message.Content); text != "" && !message.Hidden {
				details["visible_assistant_text"] = details["visible_assistant_text"].(int) + len(text)
				details["last_assistant_preview"] = truncateDeepAgentDiagnosticText(text, 240)
			}
			for _, call := range message.ToolCalls {
				details["tool_calls"] = details["tool_calls"].(int) + 1
				if name := strings.TrimSpace(call.Name); name != "" {
					assistantToolNames = append(assistantToolNames, name)
				}
			}
		case state.MessageRoleTool:
			details["tool_results"] = details["tool_results"].(int) + 1
			if name := strings.TrimSpace(message.ToolName); name != "" {
				toolResultNames = append(toolResultNames, name)
				if strings.EqualFold(name, ArtifactToolName) {
					details["artifact_tool_results"] = details["artifact_tool_results"].(int) + 1
				}
			}
		}
	}
	details["assistant_tool_names"] = assistantToolNames
	details["tool_result_names"] = toolResultNames
	return details
}

func truncateDeepAgentDiagnosticText(text string, limit int) string {
	text = strings.TrimSpace(text)
	if limit <= 0 || len([]rune(text)) <= limit {
		return text
	}
	runes := []rune(text)
	return string(runes[:limit]) + "..."
}

func (e *RuntimeDeepAgentExecutor) createDeepAgentModelArtifact(ctx context.Context, userID, sessionID string, action DeepAgentAction, output string) (*Artifact, error) {
	if e == nil || e.runtime == nil || e.runtime.artifacts == nil {
		return nil, fmt.Errorf("artifact service is not configured")
	}
	if strings.TrimSpace(userID) == "" || strings.TrimSpace(sessionID) == "" {
		return nil, fmt.Errorf("artifact fallback requires user_id and session_id")
	}
	if deepAgentActionDeliverableType(action) == deepAgentDeliverableDocx {
		return nil, fmt.Errorf("docx deliverable requires the documents/docx skill artifact; refusing markdown fallback")
	}
	filename := deepAgentModelArtifactFilename(action)
	return e.runtime.CreateArtifact(ctx, userID, sessionID, filename, "text/markdown", []byte(output))
}

func deepAgentActionDeliverableType(action DeepAgentAction) string {
	deliverable := strings.ToLower(strings.TrimSpace(deepAgentActionString(action, "deliverable_type")))
	if deliverable != "" && deliverable != deepAgentDeliverableNone {
		return deliverable
	}
	text := strings.Join([]string{
		deepAgentActionString(action, "filename_hint"),
		deepAgentActionString(action, "step_title"),
		deepAgentActionString(action, "step_intent"),
		deepAgentActionString(action, "done_condition"),
		deepAgentActionString(action, "success_criteria"),
	}, "\n")
	if deepAgentTextRequestsDocx(text) {
		return deepAgentDeliverableDocx
	}
	return ""
}

func deepAgentActionRequiresArtifact(action DeepAgentAction) bool {
	if action.Args != nil {
		for _, key := range []string{"requires_artifact", "require_artifact"} {
			if _, ok := action.Args[key]; ok {
				return deepAgentBool(action.Args, key, false)
			}
		}
	}
	text := strings.Join([]string{
		deepAgentActionString(action, "step_title"),
		deepAgentActionString(action, "step_intent"),
		deepAgentActionString(action, "done_condition"),
		deepAgentActionString(action, "success_criteria"),
	}, "\n")
	return deepAgentTextRequiresArtifact(text)
}

func deepAgentModelArtifactFilename(action DeepAgentAction) string {
	if hint := strings.TrimSpace(deepAgentActionString(action, "filename_hint")); hint != "" {
		return sanitizeDeepAgentFilename(strings.TrimSuffix(strings.TrimSuffix(hint, ".markdown"), ".md"), ".md")
	}
	stepID := strings.TrimSpace(action.StepID)
	if stepID == "" {
		stepID = strings.TrimSpace(deepAgentActionString(action, "step_id"))
	}
	stepID = strings.ToLower(stepID)
	stepID = strings.NewReplacer("/", "-", "\\", "-", " ", "-", "_", "-", ".", "-").Replace(stepID)
	stepID = strings.Trim(stepID, "-")
	if stepID == "" {
		stepID = "result"
	}
	if !strings.HasSuffix(stepID, ".md") {
		stepID += ".md"
	}
	return stepID
}

func recordDeepAgentArtifactToolResult(session *state.Session, action DeepAgentAction, artifact *Artifact) {
	if session == nil || artifact == nil {
		return
	}
	callID := "deep-agent-artifact-" + firstNonEmptyString(strings.TrimSpace(action.StepID), "result")
	recordArtifactToolResult(session, callID, artifact, "deep_agent_model_fallback")
}

func (e *RuntimeDeepAgentExecutor) executeSkillAction(ctx context.Context, action DeepAgentAction, agentState *DeepAgentState) (DeepAgentActionResult, error) {
	userID := deepAgentActionString(action, "user_id")
	sessionID := deepAgentActionString(action, "session_id")
	if agentState != nil && agentState.WorkingMemory != nil {
		userID = firstNonEmptyString(userID, deepAgentWorkflowString(agentState.WorkingMemory, "user_id"))
		sessionID = firstNonEmptyString(sessionID, deepAgentWorkflowString(agentState.WorkingMemory, "session_id"))
	}
	skillName := strings.TrimPrefix(firstNonEmptyString(deepAgentActionString(action, "skill"), deepAgentActionString(action, "skill_name")), "/")
	if skillName == "" {
		return DeepAgentActionResult{Status: DeepAgentActionStatusFailed, Error: "skill action skill_name is required"}, fmt.Errorf("skill action skill_name is required")
	}
	args := firstNonEmptyString(deepAgentActionString(action, "args"), deepAgentActionString(action, "input"))
	session, err := e.deepAgentSession(ctx, userID, sessionID)
	if err != nil {
		return DeepAgentActionResult{Status: DeepAgentActionStatusFailed, Error: err.Error()}, err
	}
	content := "/" + skillName
	if strings.TrimSpace(args) != "" {
		content += " " + args
	}
	startMessageCount := len(session.Messages)
	beforeArtifacts := e.deepAgentArtifactIDSet(ctx, userID, session.ID)
	result, err := e.runtime.runSkillCommand(withHiddenUserMessage(ctx), ChatRequest{
		UserID:    userID,
		SessionID: session.ID,
		Content:   content,
	}, userID, session, content, nil)
	diagnostics := collectSkillExecutionDiagnostics(result.Session, startMessageCount)
	if err != nil {
		return DeepAgentActionResult{Status: DeepAgentActionStatusFailed, Error: err.Error(), Retryable: true, Metadata: deepAgentSkillActionMetadata(skillName, session.ID, diagnostics, result.Job)}, err
	}
	resultSession := result.Session
	if resultSession == nil {
		resultSession = session
	}
	metadata := deepAgentSkillActionMetadata(skillName, session.ID, diagnostics, result.Job)
	if hiddenAssistantMessages := hideDeepAgentExecutionAssistantMessages(resultSession, startMessageCount); hiddenAssistantMessages > 0 {
		metadata["hidden_assistant_messages"] = hiddenAssistantMessages
	}
	if resultSession != nil && userID != "" && strings.TrimSpace(resultSession.ID) != "" {
		if saveErr := e.runtime.sessions.Save(ctx, userID, resultSession); saveErr != nil {
			return DeepAgentActionResult{Status: DeepAgentActionStatusFailed, Error: saveErr.Error(), Retryable: true, Metadata: metadata}, saveErr
		}
	}
	newArtifactRefs := e.deepAgentNewArtifactRefs(ctx, userID, resultSession.ID, beforeArtifacts, action, "skill")
	if len(newArtifactRefs) > 0 {
		metadata["artifact_refs"] = newArtifactRefs
		metadata["artifact_count"] = len(newArtifactRefs)
		metadata["tool_result_valid"] = true
	}
	if result.Job != nil {
		childResult, childErr := e.runDeepAgentChildJob(ctx, result.Job, metadata, beforeArtifacts, action)
		if childErr != nil {
			return childResult, childErr
		}
		return childResult, nil
	}
	output := result.Output
	if ready := deepAgentArtifactReadyOutput(metadata, int64(deepAgentAnyInt(metadata["artifact_count"], 0))); ready != "" {
		output = ready
	}
	return DeepAgentActionResult{
		Status:    DeepAgentActionStatusSucceeded,
		Output:    output,
		Completed: true,
		Metadata:  metadata,
	}, nil
}

func (e *RuntimeDeepAgentExecutor) runDeepAgentChildJob(ctx context.Context, job *Job, metadata map[string]any, beforeArtifacts map[string]struct{}, action DeepAgentAction) (DeepAgentActionResult, error) {
	if e == nil || e.runtime == nil || job == nil {
		return DeepAgentActionResult{Status: DeepAgentActionStatusFailed, Error: "child job is not configured", Metadata: metadata}, fmt.Errorf("child job is not configured")
	}
	if err := e.runtime.StartJob(ctx, job); err != nil {
		return DeepAgentActionResult{Status: DeepAgentActionStatusFailed, Error: err.Error(), Retryable: true, Metadata: metadata}, err
	}
	ticker := time.NewTicker(time.Duration(DeepAgentDefaultChildJobPollMS) * time.Millisecond)
	defer ticker.Stop()
	for {
		current, err := e.runtime.GetJob(ctx, job.UserID, job.ID)
		if err != nil {
			return DeepAgentActionResult{Status: DeepAgentActionStatusFailed, Error: err.Error(), Retryable: true, Metadata: metadata}, err
		}
		metadata["child_job_status"] = current.Status
		if isTerminalJobStatus(current.Status) {
			if current.Status == JobStatusSucceeded {
				artifactRefs := e.deepAgentNewArtifactRefs(ctx, current.UserID, current.SessionID, beforeArtifacts, action, "skill_job")
				if len(artifactRefs) == 0 {
					artifactRefs = e.deepAgentChildJobArtifactRefs(ctx, current)
				}
				if len(artifactRefs) > 0 {
					metadata["artifact_count"] = len(artifactRefs)
					metadata["artifact_refs"] = artifactRefs
					metadata["tool_result_valid"] = true
				}
				return DeepAgentActionResult{
					Status:    DeepAgentActionStatusSucceeded,
					Output:    fmt.Sprintf("skill job %s succeeded", current.ID),
					Completed: true,
					Metadata:  metadata,
				}, nil
			}
			err := fmt.Errorf("skill job %s ended with status %s: %s", current.ID, current.Status, current.Error)
			return DeepAgentActionResult{Status: DeepAgentActionStatusFailed, Error: err.Error(), Retryable: current.Status != JobStatusCancelled, Metadata: metadata}, err
		}
		select {
		case <-ctx.Done():
			err := fmt.Errorf("waiting for skill job %s: %w", job.ID, ctx.Err())
			return DeepAgentActionResult{Status: DeepAgentActionStatusFailed, Error: err.Error(), Retryable: true, Metadata: metadata}, err
		case <-ticker.C:
		}
	}
}

func (e *RuntimeDeepAgentExecutor) deepAgentChildJobArtifactCount(ctx context.Context, job *Job) int {
	return len(e.deepAgentChildJobArtifactRefs(ctx, job))
}

func (e *RuntimeDeepAgentExecutor) deepAgentChildJobArtifactRefs(ctx context.Context, job *Job) []DeepAgentArtifactRef {
	if e == nil || e.runtime == nil || job == nil {
		return nil
	}
	artifacts, err := e.runtime.ListArtifacts(ctx, job.UserID, job.SessionID)
	if err != nil {
		return nil
	}
	refs := make([]DeepAgentArtifactRef, 0)
	for _, artifact := range artifacts {
		if artifact == nil {
			continue
		}
		if strings.TrimSpace(artifact.JobID) != "" && artifact.JobID == job.ID {
			refs = append(refs, deepAgentArtifactRefFromArtifact(artifact, "skill_job"))
		}
	}
	return refs
}

func deepAgentSkillActionMetadata(skillName, sessionID string, diagnostics skillExecutionDiagnostics, job *Job) map[string]any {
	metadata := map[string]any{
		"tool":              DeepAgentToolModeSkill,
		"skill_name":        skillName,
		"session_id":        sessionID,
		"artifact_count":    diagnostics.ArtifactCount,
		"tool_result_valid": diagnostics.SkillError == "" && diagnostics.ErrorKind == "",
	}
	if diagnostics.ErrorKind != "" {
		metadata["error_kind"] = diagnostics.ErrorKind
	}
	if diagnostics.Provider != "" {
		metadata["provider"] = diagnostics.Provider
	}
	if diagnostics.Model != "" {
		metadata["model"] = diagnostics.Model
	}
	if diagnostics.JSON != nil {
		metadata["diagnostics"] = diagnostics.JSON
	}
	if job != nil {
		metadata["job_started"] = true
		metadata["child_job_id"] = job.ID
		metadata["child_job_type"] = job.Type
	} else {
		metadata["job_started"] = false
	}
	return metadata
}

func (e *RuntimeDeepAgentExecutor) executeRAGSearchAction(ctx context.Context, action DeepAgentAction, agentState *DeepAgentState) (DeepAgentActionResult, error) {
	userID := deepAgentActionString(action, "user_id")
	if agentState != nil && agentState.WorkingMemory != nil {
		userID = firstNonEmptyString(userID, deepAgentWorkflowString(agentState.WorkingMemory, "user_id"))
	}
	query := firstNonEmptyString(deepAgentActionString(action, "query"), deepAgentActionString(action, "prompt"), deepAgentActionString(action, "input"))
	if query == "" {
		return DeepAgentActionResult{Status: DeepAgentActionStatusFailed, Error: "rag_search action query is required"}, fmt.Errorf("rag_search action query is required")
	}
	limit := deepAgentActionInt(action, "limit", DeepAgentDefaultRAGSearchLimit)
	results, err := e.runtime.SearchMessages(ctx, userID, query, limit, 0)
	if err != nil {
		return DeepAgentActionResult{Status: DeepAgentActionStatusFailed, Error: err.Error(), Retryable: true}, err
	}
	data, _ := json.Marshal(results)
	return DeepAgentActionResult{
		Status:    DeepAgentActionStatusSucceeded,
		Output:    string(data),
		Completed: true,
		Metadata: map[string]any{
			"tool":         DeepAgentToolModeRAGSearch,
			"query":        query,
			"top_k":        limit,
			"result_count": len(results),
			"sources":      deepAgentSourceRefsFromMessageSearch(results),
		},
	}, nil
}

func deepAgentSourceRefsFromMessageSearch(results []MessageSearchResult) []DeepAgentSourceRef {
	if len(results) == 0 {
		return nil
	}
	refs := make([]DeepAgentSourceRef, 0, len(results))
	for _, result := range results {
		refs = append(refs, DeepAgentSourceRef{
			ID:       firstNonEmptyString(result.MessageID, fmt.Sprintf("%s:%d", result.SessionID, result.MessageIndex)),
			Title:    firstNonEmptyString(result.SessionTitle, result.SessionID),
			Snippet:  truncateDeepAgentDiagnosticText(firstNonEmptyString(result.Snippet, result.Content), 240),
			Provider: firstNonEmptyString(result.Source, "session"),
		})
	}
	return refs
}

func (e *RuntimeDeepAgentExecutor) deepAgentSession(ctx context.Context, userID, sessionID string) (*state.Session, error) {
	if strings.TrimSpace(sessionID) == "" {
		return state.NewSession(e.runtime.config.DefaultWorkingDir), nil
	}
	if e.runtime.sessions == nil {
		return nil, fmt.Errorf("session store is not configured")
	}
	session, err := e.runtime.sessions.Get(ctx, userID, sessionID)
	if err != nil {
		return nil, err
	}
	return session, nil
}

func deepAgentPlannerPrompt(req DeepAgentTaskRequest) string {
	return deepAgentPlannerPromptWithSkills(req, "(none)")
}

func deepAgentRubricPrompt(rubric DeepAgentRubric) string {
	rubric = normalizeDeepAgentRubric(rubric)
	if deepAgentRubricEmpty(rubric) {
		return ""
	}
	var b strings.Builder
	writeList := func(title string, values []string) {
		if len(values) == 0 {
			return
		}
		b.WriteString(title)
		b.WriteString(":\n")
		for _, value := range values {
			b.WriteString("- ")
			b.WriteString(value)
			b.WriteString("\n")
		}
	}
	writeList("Acceptance criteria", rubric.AcceptanceCriteria)
	writeList("Required evidence", rubric.RequiredEvidence)
	writeList("Required artifacts", rubric.RequiredArtifacts)
	writeList("Forbidden actions", rubric.ForbiddenActions)
	if strings.TrimSpace(rubric.QualityBar) != "" {
		b.WriteString("Quality bar: ")
		b.WriteString(strings.TrimSpace(rubric.QualityBar))
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

func deepAgentPlannerPromptWithSkills(req DeepAgentTaskRequest, skillCatalog string) string {
	if strings.TrimSpace(skillCatalog) == "" {
		skillCatalog = "(none)"
	}
	maxSteps := normalizeDeepAgentPolicy(req.Policy).MaxSteps
	loadedContext := deepAgentLoadedContextPrompt(req.State)
	if loadedContext == "" {
		loadedContext = "(none)"
	}
	rubric := deepAgentRubricPrompt(req.Rubric)
	if rubric == "" {
		rubric = "(none)"
	}
	return fmt.Sprintf(PromptDeepAgentPlannerTemplate, maxSteps, rubric, skillCatalog, loadedContext, strings.TrimSpace(req.Goal))
}

func deepAgentPlanRepairContext(req DeepAgentTaskRequest) string {
	return fmt.Sprintf(PromptDeepAgentPlanRepairContextTemplate, strings.TrimSpace(req.Goal), normalizeDeepAgentPolicy(req.Policy).MaxSteps)
}

func (p *RuntimeDeepAgentPlanner) deepAgentPlanRepairContext(ctx context.Context, req DeepAgentTaskRequest) deepAgentRenderedPrompt {
	return p.renderDeepAgentPromptForScope(ctx, PromptIDRuntimeDeepAgentPlanRepair, req.UserID, req.SessionID, strings.TrimSpace(req.Goal), normalizeDeepAgentPolicy(req.Policy).MaxSteps)
}

func parseDeepAgentPlan(output string) (DeepAgentPlan, error) {
	result := ExtractAndValidateStructuredObject(output, deepAgentPlanStructuredSchema())
	if !result.Valid() {
		return DeepAgentPlan{}, result.Error()
	}
	jsonText, err := json.Marshal(result.Value)
	if err != nil {
		return DeepAgentPlan{}, err
	}
	var plan DeepAgentPlan
	if err := json.Unmarshal(jsonText, &plan); err != nil {
		return DeepAgentPlan{}, err
	}
	if err := validateDeepAgentPlanSemantics(plan); err != nil {
		return DeepAgentPlan{}, err
	}
	return plan, nil
}

func deepAgentPlanStructuredSchema() StructuredSchema {
	return StructuredSchema{
		Name:    "deep_agent_plan",
		Version: "v1",
		Schema: map[string]any{
			"type":     "object",
			"required": []string{"goal", "steps"},
			"properties": map[string]any{
				"goal": map[string]any{"type": "string"},
				"steps": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type":     "object",
						"required": []string{"id", "title", "intent", "done_condition"},
						"properties": map[string]any{
							"id":             map[string]any{"type": "string"},
							"title":          map[string]any{"type": "string"},
							"intent":         map[string]any{"type": "string"},
							"depends_on":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
							"status":         map[string]any{"type": "string"},
							"done_condition": map[string]any{"type": "string"},
							"risk_level":     map[string]any{"type": "string", "enum": []any{"", "low", "medium", "high"}},
							"metadata":       map[string]any{"type": "object"},
						},
					},
				},
			},
		},
	}
}

func validateDeepAgentPlanSemantics(plan DeepAgentPlan) error {
	if strings.TrimSpace(plan.Goal) == "" {
		return fmt.Errorf("deep agent plan goal is required")
	}
	if len(plan.Steps) == 0 {
		return fmt.Errorf("deep agent plan has no steps")
	}
	for idx, step := range plan.Steps {
		prefix := fmt.Sprintf("deep agent plan step %d", idx)
		if strings.TrimSpace(step.ID) == "" {
			return fmt.Errorf("%s id is required", prefix)
		}
		if strings.TrimSpace(step.Title) == "" {
			return fmt.Errorf("%s title is required", prefix)
		}
		if strings.TrimSpace(step.Intent) == "" {
			return fmt.Errorf("%s intent is required", prefix)
		}
		if strings.TrimSpace(step.DoneCondition) == "" {
			return fmt.Errorf("%s done_condition is required", prefix)
		}
		if tool := deepAgentWorkflowString(step.Metadata, "tool"); tool != "" {
			return fmt.Errorf("%s must not select metadata.tool during planning: %s", prefix, tool)
		}
	}
	return nil
}

func extractDeepAgentJSONObject(output string) (string, error) {
	output = strings.TrimSpace(output)
	if output == "" {
		return "", fmt.Errorf("deep agent planner returned empty output")
	}
	decoder := json.NewDecoder(strings.NewReader(output))
	var raw json.RawMessage
	if err := decoder.Decode(&raw); err == nil && len(raw) > 0 {
		return string(raw), nil
	}
	start := strings.Index(output, "{")
	end := strings.LastIndex(output, "}")
	if start < 0 || end <= start {
		return "", fmt.Errorf("deep agent planner did not return a JSON object")
	}
	return output[start : end+1], nil
}

func deepAgentActionString(action DeepAgentAction, key string) string {
	if action.Args == nil {
		return ""
	}
	value, ok := action.Args[key]
	if !ok {
		return ""
	}
	if str, ok := value.(string); ok {
		return strings.TrimSpace(str)
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func deepAgentActionInt(action DeepAgentAction, key string, fallback int) int {
	if action.Args == nil {
		return fallback
	}
	switch value := action.Args[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	case json.Number:
		if n, err := value.Int64(); err == nil {
			return int(n)
		}
	}
	return fallback
}

func deepAgentWorkflowString(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	value, ok := values[key]
	if !ok {
		return ""
	}
	if str, ok := value.(string); ok {
		return strings.TrimSpace(str)
	}
	return strings.TrimSpace(fmt.Sprint(value))
}
