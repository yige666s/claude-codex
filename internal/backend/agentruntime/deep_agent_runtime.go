package agentruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"claude-codex/internal/harness/skills"
	"claude-codex/internal/harness/state"
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
	runner := p.runtime.runnerForScope(Scope{
		UserID:    req.UserID,
		SessionID: req.SessionID,
		Prompt:    req.Goal,
	})
	plannerSession := state.NewSession("")
	prompt := p.deepAgentPlannerPrompt(req)
	result, err := runner.RunGeneratedPrompt(ctx, plannerSession, prompt)
	if err != nil {
		return DeepAgentPlan{}, err
	}
	plan, err := parseDeepAgentPlan(result.Output)
	if err != nil {
		schema := deepAgentPlanStructuredSchema()
		emitStructuredOutputValidationFailure(ctx, schema, "deep_agent_planner", ExtractAndValidateStructuredObject(result.Output, schema))
		repaired, repairErr := repairStructuredJSONWithRunner(ctx, runner, schema, result.Output, err, deepAgentPlanRepairContext(req))
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

func (p *RuntimeDeepAgentPlanner) deepAgentPlannerPrompt(req DeepAgentTaskRequest) string {
	return deepAgentPlannerPromptWithSkills(req, p.deepAgentSkillCatalogPrompt())
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
	if deepAgentWorkflowString(step.Metadata, "tool") != "" {
		return ruleDeepAgentPlanner{}.NextAction(ctx, state, step)
	}
	mode := p.keywordRouteStep(step)
	if mode == "" {
		mode = p.llmRouteStep(ctx, state, step)
	}
	if mode == "" {
		mode = "model"
	}
	args := map[string]any{}
	switch mode {
	case "skill":
		skill, ok := p.selectSkillForStep(step)
		if !ok {
			mode = "model"
			args["prompt"] = p.modelPromptForStep(state, step)
			break
		}
		args["skill_name"] = skill.Name
		args["args"] = p.skillArgsForStep(state, step)
	case "rag_search", "tool_use":
		mode = "rag_search"
		args["query"] = firstNonEmptyString(step.Intent, step.Title, stateGoal(state))
		args["limit"] = 5
	default:
		mode = "model"
		args["prompt"] = p.modelPromptForStep(state, step)
	}
	if state != nil && state.WorkingMemory != nil {
		if userID := deepAgentWorkflowString(state.WorkingMemory, "user_id"); userID != "" {
			args["user_id"] = firstNonEmptyString(deepAgentWorkflowString(args, "user_id"), userID)
		}
		if sessionID := deepAgentWorkflowString(state.WorkingMemory, "session_id"); sessionID != "" {
			args["session_id"] = firstNonEmptyString(deepAgentWorkflowString(args, "session_id"), sessionID)
		}
	}
	attempt := deepAgentStepAttemptCount(state, step.ID) + 1
	if attempt > 1 {
		args["attempt"] = attempt
		args["retry_instruction"] = fmt.Sprintf("Previous attempt %d for step %q did not satisfy the success criteria. Use a different strategy and produce evidence for: %s", attempt-1, firstNonEmptyString(step.Title, step.ID), step.DoneCondition)
		if mode == "model" {
			args["prompt"] = strings.TrimSpace(deepAgentWorkflowString(args, "prompt") + "\n\nRetry instruction: " + deepAgentWorkflowString(args, "retry_instruction"))
		}
	}
	return DeepAgentAction{
		StepID: step.ID,
		Tool:   mode,
		Args: mergeDeepAgentActionArgs(args, map[string]any{
			"goal":             stateGoal(state),
			"step_id":          step.ID,
			"step_title":       step.Title,
			"step_intent":      step.Intent,
			"done_condition":   step.DoneCondition,
			"success_criteria": step.DoneCondition,
		}),
	}, nil
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
		return "rag_search"
	}
	if deepAgentContainsAny(text,
		"artifact", "download", "file", ".md", "markdown", "report", "文档", "报告", "文件", "可下载", "产物", "导出",
		"搜索", "查询", "检索", "查找", "调研", "研究", "外部", "联网", "互联网", "官网", "产品", "竞品", "新闻",
		"web", "internet", "external", "current", "latest", "research",
	) {
		return "model"
	}
	return ""
}

func (p *RuntimeDeepAgentPlanner) llmRouteStep(ctx context.Context, agentState *DeepAgentState, step DeepAgentStep) string {
	if p == nil || p.runtime == nil {
		return ""
	}
	prompt := fmt.Sprintf(`Classify the next DeepAgent step execution mode.

Return exactly one word: model, skill, rag_search, or multi.

Definitions:
- model: general step execution. The model may use provider tools such as WebSearch, WebFetch, Artifact, and Skill when needed.
- skill: force a published skill only when the step explicitly requires a specific specialized skill.
- rag_search: search prior conversation/session context only. Do not use this for external web/product research.
- multi: broad step that should be decomposed; choose model if unsure.

Step intent: %s
Success criteria: %s
Prior step context:
%s`, strings.TrimSpace(firstNonEmptyString(step.Intent, step.Title)), strings.TrimSpace(step.DoneCondition), p.stepContextSummary(agentState, step))
	runner := p.runtime.runnerForScope(Scope{UserID: deepAgentWorkflowString(stateWorkingMemory(agentState), "user_id"), SessionID: deepAgentWorkflowString(stateWorkingMemory(agentState), "session_id"), Prompt: prompt})
	result, err := runner.RunGeneratedPrompt(ctx, state.NewSession(""), prompt)
	if err != nil {
		return ""
	}
	mode := strings.ToLower(strings.TrimSpace(result.Output))
	switch {
	case strings.Contains(mode, "skill"):
		return "skill"
	case strings.Contains(mode, "rag_search") || strings.Contains(mode, "search"):
		return "rag_search"
	case strings.Contains(mode, "multi"):
		return "model"
	case strings.Contains(mode, "model"):
		return "model"
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
	var b strings.Builder
	b.WriteString(deepAgentToolUsageReminder())
	b.WriteString("\n\n")
	if goal := stateGoal(agentState); goal != "" {
		b.WriteString("User goal:\n")
		b.WriteString(goal)
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
	return b.String()
}

func deepAgentToolUsageReminder() string {
	return `DeepAgent tool policy:
- Use WebSearch and WebFetch for current, external, internet, product, company, market, or competitor research.
- Use Artifact directly when the step must create a Markdown/text/CSV/JSON/HTML file or another downloadable artifact.
- Use Skill when a published skill is clearly the best specialized executor.
- Do not claim you cannot browse the web, perform real-time research, or create files when an appropriate tool is available. If a tool fails, report the tool error and continue with any partial evidence.`
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
	for _, token := range []string{"docx", "word", "document", "文档", "报告", "report", "markdown", ".md", "file", "artifact", "文件"} {
		if strings.Contains(text, token) && strings.Contains(haystack, token) {
			score += 4
		}
	}
	for _, token := range strings.Fields(strings.NewReplacer("\n", " ", "，", " ", "。", " ", ",", " ", ".", " ").Replace(text)) {
		if len([]rune(token)) >= 2 && strings.Contains(haystack, token) {
			score++
		}
	}
	if skillProducesArtifacts(skill) && deepAgentContainsAny(text, "artifact", "download", "file", "markdown", "docx", "文档", "报告", "文件", "可下载", "产物") {
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
	tool := strings.ToLower(strings.TrimSpace(action.Tool))
	switch tool {
	case "", "model", "answer", "llm":
		return e.executeModelAction(ctx, action, agentState)
	case "skill":
		return e.executeSkillAction(ctx, action, agentState)
	case "rag_search", "search", "message_search":
		return e.executeRAGSearchAction(ctx, action, agentState)
	default:
		return DeepAgentActionResult{
			Status: DeepAgentActionStatusFailed,
			Error:  fmt.Sprintf("unsupported deep agent tool: %s", action.Tool),
		}, fmt.Errorf("unsupported deep agent tool: %s", action.Tool)
	}
}

func (e *RuntimeDeepAgentExecutor) executeModelAction(ctx context.Context, action DeepAgentAction, agentState *DeepAgentState) (DeepAgentActionResult, error) {
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
	session, err := e.deepAgentSession(ctx, userID, sessionID)
	if err != nil {
		return DeepAgentActionResult{Status: DeepAgentActionStatusFailed, Error: err.Error()}, err
	}
	if err := e.runtime.injectSessionRuntimeContexts(ctx, userID, session); err != nil {
		return DeepAgentActionResult{Status: DeepAgentActionStatusFailed, Error: err.Error()}, err
	}
	runner := e.runtime.runnerForScope(Scope{
		UserID:     userID,
		SessionID:  session.ID,
		WorkingDir: session.WorkingDir,
		Prompt:     prompt,
	})
	result, err := runner.RunGeneratedPrompt(ctx, session, prompt)
	if err != nil {
		return DeepAgentActionResult{Status: DeepAgentActionStatusFailed, Error: err.Error(), Retryable: true}, err
	}
	if result.Session != nil && userID != "" && sessionID != "" {
		if saveErr := e.runtime.sessions.Save(ctx, userID, result.Session); saveErr != nil {
			return DeepAgentActionResult{Status: DeepAgentActionStatusFailed, Error: saveErr.Error(), Retryable: true}, saveErr
		}
	}
	return DeepAgentActionResult{
		Status:    DeepAgentActionStatusSucceeded,
		Output:    result.Output,
		Completed: true,
		Metadata: map[string]any{
			"tool":       "model",
			"session_id": session.ID,
		},
	}, nil
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
	result, err := e.runtime.runSkillCommand(withHiddenUserMessage(ctx), ChatRequest{
		UserID:    userID,
		SessionID: session.ID,
		Content:   content,
	}, userID, session, content, nil)
	diagnostics := collectSkillExecutionDiagnostics(result.Session, startMessageCount)
	if err != nil {
		return DeepAgentActionResult{Status: DeepAgentActionStatusFailed, Error: err.Error(), Retryable: true, Metadata: deepAgentSkillActionMetadata(skillName, session.ID, diagnostics, result.Job)}, err
	}
	if result.Session != nil && userID != "" && sessionID != "" {
		if saveErr := e.runtime.sessions.Save(ctx, userID, result.Session); saveErr != nil {
			return DeepAgentActionResult{Status: DeepAgentActionStatusFailed, Error: saveErr.Error(), Retryable: true, Metadata: deepAgentSkillActionMetadata(skillName, session.ID, diagnostics, result.Job)}, saveErr
		}
	}
	metadata := deepAgentSkillActionMetadata(skillName, session.ID, diagnostics, result.Job)
	if result.Job != nil {
		childResult, childErr := e.runDeepAgentChildJob(ctx, result.Job, metadata)
		if childErr != nil {
			return childResult, childErr
		}
		return childResult, nil
	}
	return DeepAgentActionResult{
		Status:    DeepAgentActionStatusSucceeded,
		Output:    result.Output,
		Completed: true,
		Metadata:  metadata,
	}, nil
}

func (e *RuntimeDeepAgentExecutor) runDeepAgentChildJob(ctx context.Context, job *Job, metadata map[string]any) (DeepAgentActionResult, error) {
	if e == nil || e.runtime == nil || job == nil {
		return DeepAgentActionResult{Status: DeepAgentActionStatusFailed, Error: "child job is not configured", Metadata: metadata}, fmt.Errorf("child job is not configured")
	}
	if err := e.runtime.StartJob(ctx, job); err != nil {
		return DeepAgentActionResult{Status: DeepAgentActionStatusFailed, Error: err.Error(), Retryable: true, Metadata: metadata}, err
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		current, err := e.runtime.GetJob(ctx, job.UserID, job.ID)
		if err != nil {
			return DeepAgentActionResult{Status: DeepAgentActionStatusFailed, Error: err.Error(), Retryable: true, Metadata: metadata}, err
		}
		metadata["child_job_status"] = current.Status
		if isTerminalJobStatus(current.Status) {
			if current.Status == JobStatusSucceeded {
				if artifactCount := e.deepAgentChildJobArtifactCount(ctx, current); artifactCount > 0 {
					metadata["artifact_count"] = artifactCount
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
	if e == nil || e.runtime == nil || job == nil {
		return 0
	}
	artifacts, err := e.runtime.ListArtifacts(ctx, job.UserID, job.SessionID)
	if err != nil {
		return 0
	}
	count := 0
	for _, artifact := range artifacts {
		if artifact == nil {
			continue
		}
		if strings.TrimSpace(artifact.JobID) == "" || artifact.JobID == job.ID {
			count++
		}
	}
	return count
}

func deepAgentSkillActionMetadata(skillName, sessionID string, diagnostics skillExecutionDiagnostics, job *Job) map[string]any {
	metadata := map[string]any{
		"tool":              "skill",
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
	limit := deepAgentActionInt(action, "limit", 5)
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
			"tool":         "rag_search",
			"query":        query,
			"result_count": len(results),
		},
	}, nil
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

func deepAgentPlannerPromptWithSkills(req DeepAgentTaskRequest, skillCatalog string) string {
	if strings.TrimSpace(skillCatalog) == "" {
		skillCatalog = "(none)"
	}
	return fmt.Sprintf(`You are the planner for a production DeepAgent controller.

Split the user goal into a small intent plan. Return JSON only, with no markdown.

Rules:
- Use 1 to %d steps.
- Every step must have id, title, intent, depends_on, and done_condition.
- Plan steps describe what should be achieved, not how to execute it.
- Do not choose execution mode, tool, skill, provider, API, or command in this plan.
- Do not put metadata.tool, metadata.args, skill_name, or rag_search query in plan steps.
- Use depends_on to express required prior step outputs by step id.
- Each done_condition is the success_criteria and must be concrete and verifiable.
- Do not include risky external side effects unless the goal explicitly requires them.

Published skills are available later to the Step Router. Use this only to phrase deliverable intents clearly, not to select a skill in the plan:
%s

JSON shape:
{
  "goal": "string",
  "steps": [
    {
      "id": "step-1",
      "title": "string",
      "intent": "string",
      "depends_on": [],
      "done_condition": "string",
      "risk_level": "low|medium|high"
    }
  ]
}

User goal:
%s`, normalizeDeepAgentPolicy(req.Policy).MaxSteps, skillCatalog, strings.TrimSpace(req.Goal))
}

func deepAgentPlanRepairContext(req DeepAgentTaskRequest) string {
	return fmt.Sprintf("User goal: %s\nMax steps: %d", strings.TrimSpace(req.Goal), normalizeDeepAgentPolicy(req.Policy).MaxSteps)
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
