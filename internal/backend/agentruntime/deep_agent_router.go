package agentruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"claude-codex/internal/harness/state"
)

const (
	deepAgentRouteExecutorModel     = "model"
	deepAgentRouteExecutorArtifact  = "artifact"
	deepAgentRouteExecutorSkill     = "skill"
	deepAgentRouteExecutorRAG       = "rag_search"
	deepAgentRouteExecutorTest      = "test"
	deepAgentRouteExecutorWeb       = "web"
	deepAgentRouteExecutorCodePatch = "code_patch"
	deepAgentRouteExecutorSubPlan   = "subplan"

	deepAgentDeliverableNone     = "none"
	deepAgentDeliverableMarkdown = "markdown"
	deepAgentDeliverableDocx     = "docx"
	deepAgentDeliverableImage    = "image"
	deepAgentDeliverableSVG      = "svg"
)

type RuntimeDeepAgentStepRouter struct {
	runtime *Runtime
	planner *RuntimeDeepAgentPlanner
}

func NewRuntimeDeepAgentStepRouter(runtime *Runtime) *RuntimeDeepAgentStepRouter {
	return &RuntimeDeepAgentStepRouter{runtime: runtime, planner: NewRuntimeDeepAgentPlanner(runtime)}
}

func (r *RuntimeDeepAgentStepRouter) RouteStep(ctx context.Context, agentState *DeepAgentState, step DeepAgentStep) (DeepAgentStepRoute, error) {
	if r == nil {
		return DeepAgentStepRoute{}, fmt.Errorf("deep agent step router is not configured")
	}
	if route, ok := r.explicitRoute(step); ok {
		return r.finalizeRouteForExecution(ctx, agentState, step, route), nil
	}
	if route, ok := r.deterministicRoute(step); ok {
		return r.finalizeRouteForExecution(ctx, agentState, step, route), nil
	}
	route, err := r.llmRouteStep(ctx, agentState, step)
	if err == nil && strings.TrimSpace(route.Mode) != "" {
		return r.finalizeRouteForExecution(ctx, agentState, step, route), nil
	}
	fallback := DeepAgentStepRoute{
		StepID:     step.ID,
		Mode:       DeepAgentToolModeModel,
		Executor:   deepAgentRouteExecutorModel,
		Reason:     "deterministic fallback",
		Confidence: "low",
	}
	if deepAgentStepRequiresArtifact(step) {
		fallback.Mode = DeepAgentToolModeModelArtifact
		fallback.Executor = deepAgentRouteExecutorArtifact
		fallback.RequiresArtifact = true
		fallback.DeliverableType = deepAgentDeliverableTypeForStep(step)
	}
	if err != nil {
		fallback.Reason = "llm router failed: " + err.Error() + "; deterministic fallback"
	}
	return r.finalizeRouteForExecution(ctx, agentState, step, fallback), nil
}

func (r *RuntimeDeepAgentStepRouter) explicitRoute(step DeepAgentStep) (DeepAgentStepRoute, bool) {
	tool := strings.TrimSpace(deepAgentWorkflowString(step.Metadata, "tool"))
	if tool == "" {
		return DeepAgentStepRoute{}, false
	}
	args, _ := step.Metadata["args"].(map[string]any)
	route := DeepAgentStepRoute{
		StepID:          step.ID,
		Mode:            normalizeDeepAgentRouteMode(tool),
		Executor:        deepAgentExecutorForMode(tool),
		SkillName:       strings.TrimPrefix(firstNonEmptyString(deepAgentWorkflowString(args, "skill_name"), deepAgentWorkflowString(args, "skill")), "/"),
		DeliverableType: deepAgentDeliverableTypeForStep(step),
		SearchScope:     deepAgentSearchScopeForMode(tool),
		Reason:          "explicit step metadata",
		Confidence:      "high",
	}
	if route.Mode == DeepAgentToolModeModelArtifact || deepAgentStepRequiresArtifact(step) {
		route.RequiresArtifact = true
	}
	return route, true
}

func (r *RuntimeDeepAgentStepRouter) deterministicRoute(step DeepAgentStep) (DeepAgentStepRoute, bool) {
	text := deepAgentRouteText(step)
	if text == "" {
		return DeepAgentStepRoute{}, false
	}
	if deepAgentContainsAny(text,
		"运行测试", "执行测试", "单元测试", "集成测试", "静态检查", "类型检查", "构建验证", "go test", "npm test", "pnpm test",
		"test", "lint", "typecheck", "build check",
	) {
		return DeepAgentStepRoute{
			StepID:     step.ID,
			Mode:       DeepAgentToolModeTest,
			Executor:   deepAgentRouteExecutorTest,
			Reason:     "deterministic test verification guard",
			Confidence: "high",
		}, true
	}
	if deepAgentContainsAny(text,
		"修改代码", "修复代码", "应用补丁", "生成补丁", "代码补丁", "diff", "patch", "code edit", "code fix",
	) {
		return DeepAgentStepRoute{
			StepID:     step.ID,
			Mode:       DeepAgentToolModeCodePatch,
			Executor:   deepAgentRouteExecutorCodePatch,
			Reason:     "deterministic code patch guard",
			Confidence: "high",
		}, true
	}
	if deepAgentContainsAny(text,
		"网页验证", "截图", "浏览器", "打开页面", "页面验证", "screenshot", "browser", "web page", "dom",
	) {
		return DeepAgentStepRoute{
			StepID:      step.ID,
			Mode:        DeepAgentToolModeWeb,
			Executor:    deepAgentRouteExecutorWeb,
			SearchScope: "web",
			Reason:      "deterministic web verification guard",
			Confidence:  "high",
		}, true
	}
	if deepAgentContainsAny(text,
		"获取历史", "历史消息", "上下文检索", "会话检索", "记忆检索", "previous conversation", "prior conversation",
		"rag", "search history", "message search", "conversation search", "session context",
	) {
		return DeepAgentStepRoute{
			StepID:      step.ID,
			Mode:        DeepAgentToolModeRAGSearch,
			Executor:    deepAgentRouteExecutorRAG,
			SearchScope: "session",
			Reason:      "deterministic session or memory search guard",
			Confidence:  "high",
		}, true
	}
	if route, ok := r.skillRouteForStep(step); ok {
		return route, true
	}
	if deepAgentStepLooksLikeImageGeneration(step) {
		route := DeepAgentStepRoute{
			StepID:           step.ID,
			Mode:             DeepAgentToolModeModelArtifact,
			Executor:         deepAgentRouteExecutorArtifact,
			RequiresArtifact: true,
			DeliverableType:  deepAgentDeliverableImage,
			Reason:           "deterministic image artifact guard",
			Confidence:       "high",
		}
		if r != nil && r.planner != nil {
			if skill, ok := r.planner.selectSkillForStep(step); ok && skill != nil {
				name := strings.ToLower(strings.TrimSpace(skill.Name))
				if strings.Contains(name, "image") || strings.Contains(name, "imagen") {
					route.Mode = DeepAgentToolModeSkill
					route.Executor = deepAgentRouteExecutorSkill
					route.SkillName = skill.Name
					route.Reason = "deterministic image skill guard"
				}
			}
		}
		return route, true
	}
	if deepAgentStepRequiresArtifact(step) {
		return DeepAgentStepRoute{
			StepID:           step.ID,
			Mode:             DeepAgentToolModeModelArtifact,
			Executor:         deepAgentRouteExecutorArtifact,
			RequiresArtifact: true,
			DeliverableType:  deepAgentDeliverableTypeForStep(step),
			AllowedTools:     []string{"WebSearch", "WebFetch", ArtifactToolName},
			Reason:           "deterministic deliverable artifact guard",
			Confidence:       "high",
		}, true
	}
	if deepAgentContainsAny(text,
		"搜索", "查询", "检索", "查找", "调研", "研究", "外部", "联网", "互联网", "官网", "产品", "竞品", "新闻",
		"web", "internet", "external", "current", "latest", "research",
	) {
		return DeepAgentStepRoute{
			StepID:          step.ID,
			Mode:            DeepAgentToolModeModel,
			Executor:        deepAgentRouteExecutorModel,
			DeliverableType: deepAgentDeliverableNone,
			AllowedTools:    []string{"WebSearch", "WebFetch"},
			SearchScope:     "web",
			Reason:          "deterministic research guard",
			Confidence:      "high",
		}, true
	}
	return DeepAgentStepRoute{}, false
}

func (r *RuntimeDeepAgentStepRouter) skillRouteForStep(step DeepAgentStep) (DeepAgentStepRoute, bool) {
	if r == nil || r.planner == nil {
		return DeepAgentStepRoute{}, false
	}
	skill, ok := r.planner.selectSkillForStep(step)
	if !ok || skill == nil {
		return DeepAgentStepRoute{}, false
	}
	text := deepAgentRouteText(step)
	name := strings.ToLower(strings.TrimSpace(skill.Name))
	deliverable := deepAgentDeliverableTypeForStep(step)
	explicitSkill := strings.Contains(text, name) || deepAgentContainsAny(text, " skill", "skill ", "技能")
	matchesSpecializedDeliverable := false
	switch deliverable {
	case deepAgentDeliverableDocx:
		matchesSpecializedDeliverable = name == "docx" || strings.Contains(name, "docx")
	case deepAgentDeliverableImage:
		matchesSpecializedDeliverable = strings.Contains(name, "image") || strings.Contains(name, "imagen")
	case deepAgentDeliverableSVG:
		matchesSpecializedDeliverable = strings.Contains(name, "graph") || strings.Contains(name, "diagram") || strings.Contains(name, "svg")
	}
	if !explicitSkill && !matchesSpecializedDeliverable {
		return DeepAgentStepRoute{}, false
	}
	return DeepAgentStepRoute{
		StepID:           step.ID,
		Mode:             DeepAgentToolModeSkill,
		Executor:         deepAgentRouteExecutorSkill,
		SkillName:        skill.Name,
		RequiresArtifact: skillProducesArtifacts(skill) || deepAgentStepRequiresArtifact(step),
		DeliverableType:  deliverable,
		Reason:           "deterministic skill selection guard",
		Confidence:       "high",
	}, true
}

func (r *RuntimeDeepAgentStepRouter) llmRouteStep(ctx context.Context, agentState *DeepAgentState, step DeepAgentStep) (DeepAgentStepRoute, error) {
	if r == nil || r.runtime == nil {
		return DeepAgentStepRoute{}, fmt.Errorf("runtime is not configured")
	}
	prompt := deepAgentRoutePrompt(agentState, step)
	runner := r.runtime.runnerForScope(Scope{
		UserID:    deepAgentWorkflowString(stateWorkingMemory(agentState), "user_id"),
		SessionID: deepAgentWorkflowString(stateWorkingMemory(agentState), "session_id"),
		Prompt:    prompt,
	})
	result, err := runner.RunGeneratedPrompt(ctx, state.NewSession(""), prompt)
	if err != nil {
		return DeepAgentStepRoute{}, err
	}
	route, err := parseDeepAgentStepRoute(result.Output)
	if err != nil {
		emitStructuredOutputValidationFailure(ctx, deepAgentRouteStructuredSchema(), "deep_agent_router", ExtractAndValidateStructuredObject(result.Output, deepAgentRouteStructuredSchema()))
		return DeepAgentStepRoute{}, err
	}
	route.Reason = firstNonEmptyString(route.Reason, "llm router json")
	route.Confidence = firstNonEmptyString(route.Confidence, "medium")
	return route, nil
}

func (r *RuntimeDeepAgentStepRouter) finalizeRoute(agentState *DeepAgentState, step DeepAgentStep, route DeepAgentStepRoute) DeepAgentStepRoute {
	route.StepID = firstNonEmptyString(route.StepID, step.ID)
	route.Mode = normalizeDeepAgentRouteMode(firstNonEmptyString(route.Mode, DeepAgentToolModeModel))
	route.Executor = firstNonEmptyString(route.Executor, deepAgentExecutorForMode(route.Mode))
	if route.Executor == "" {
		route.Executor = deepAgentRouteExecutorModel
	}
	route.RequiresArtifact = route.RequiresArtifact || route.Mode == DeepAgentToolModeModelArtifact
	if route.RequiresArtifact && strings.TrimSpace(route.DeliverableType) == "" {
		route.DeliverableType = deepAgentDeliverableTypeForStep(step)
	}
	route.DeliverableType = firstNonEmptyString(route.DeliverableType, deepAgentDeliverableNone)
	if len(route.SuccessCriteria) == 0 && strings.TrimSpace(step.DoneCondition) != "" {
		route.SuccessCriteria = []string{strings.TrimSpace(step.DoneCondition)}
	}
	if route.RequiresArtifact && len(route.AllowedTools) == 0 && route.Mode == DeepAgentToolModeModelArtifact {
		route.AllowedTools = []string{"WebSearch", "WebFetch", ArtifactToolName}
	}
	if route.Mode == DeepAgentToolModeModel && len(route.AllowedTools) == 0 && route.SearchScope == "web" {
		route.AllowedTools = []string{"WebSearch", "WebFetch"}
	}
	if strings.TrimSpace(route.FilenameHint) == "" && route.RequiresArtifact {
		route.FilenameHint = deepAgentRouteFilenameHint(agentState, step, route)
	}
	route.Reason = firstNonEmptyString(route.Reason, "fallback route")
	route.Confidence = firstNonEmptyString(route.Confidence, "medium")
	return route
}

func (r *RuntimeDeepAgentStepRouter) finalizeRouteForExecution(ctx context.Context, agentState *DeepAgentState, step DeepAgentStep, route DeepAgentStepRoute) DeepAgentStepRoute {
	final := r.finalizeRoute(agentState, step, route)
	final.Version = r.deepAgentRouteVersion()
	if r == nil || r.runtime == nil || !r.runtime.config.DeepAgent.V2ShadowRoute {
		return final
	}
	shadow := r.legacyRoute(ctx, agentState, step)
	final.ShadowRoute = deepAgentStepRouteMap(shadow)
	final.ShadowDiff = diffDeepAgentRoutes(shadow, final)
	return final
}

func (r *RuntimeDeepAgentStepRouter) deepAgentRouteVersion() string {
	if r != nil && r.runtime != nil && r.runtime.config.DeepAgent.V2Enabled {
		return "v2"
	}
	return "v1"
}

func (r *RuntimeDeepAgentStepRouter) legacyRoute(ctx context.Context, agentState *DeepAgentState, step DeepAgentStep) DeepAgentStepRoute {
	planner := r.planner
	if planner == nil {
		planner = NewRuntimeDeepAgentPlanner(r.runtime)
	}
	mode := planner.keywordRouteStep(step)
	if mode == "" {
		mode = planner.llmRouteStep(ctx, agentState, step)
	}
	if mode == "" {
		mode = DeepAgentToolModeModel
	}
	route := DeepAgentStepRoute{
		StepID:           step.ID,
		Version:          "legacy",
		Mode:             normalizeDeepAgentRouteMode(mode),
		Executor:         deepAgentExecutorForMode(mode),
		RequiresArtifact: normalizeDeepAgentRouteMode(mode) == DeepAgentToolModeModelArtifact,
		DeliverableType:  deepAgentDeliverableNone,
		Reason:           "legacy keyword/one-word router shadow",
		Confidence:       "medium",
	}
	if route.RequiresArtifact {
		route.DeliverableType = deepAgentDeliverableTypeForStep(step)
	}
	return r.finalizeRoute(agentState, step, route)
}

func diffDeepAgentRoutes(oldRoute, newRoute DeepAgentStepRoute) []string {
	var diff []string
	add := func(field, oldValue, newValue string) {
		if strings.TrimSpace(oldValue) != strings.TrimSpace(newValue) {
			diff = append(diff, fmt.Sprintf("%s:%s->%s", field, strings.TrimSpace(oldValue), strings.TrimSpace(newValue)))
		}
	}
	add("mode", oldRoute.Mode, newRoute.Mode)
	add("executor", oldRoute.Executor, newRoute.Executor)
	add("skill_name", oldRoute.SkillName, newRoute.SkillName)
	add("deliverable_type", oldRoute.DeliverableType, newRoute.DeliverableType)
	add("search_scope", oldRoute.SearchScope, newRoute.SearchScope)
	if oldRoute.RequiresArtifact != newRoute.RequiresArtifact {
		diff = append(diff, fmt.Sprintf("requires_artifact:%t->%t", oldRoute.RequiresArtifact, newRoute.RequiresArtifact))
	}
	if strings.Join(oldRoute.AllowedTools, ",") != strings.Join(newRoute.AllowedTools, ",") {
		diff = append(diff, fmt.Sprintf("allowed_tools:%s->%s", strings.Join(oldRoute.AllowedTools, ","), strings.Join(newRoute.AllowedTools, ",")))
	}
	return diff
}

func normalizeDeepAgentRouteMode(mode string) string {
	mode = strings.ToLower(strings.TrimSpace(mode))
	switch mode {
	case "", "answer", "llm":
		return DeepAgentToolModeModel
	case "artifact", "file", "deliverable", DeepAgentToolModeModelArtifact:
		return DeepAgentToolModeModelArtifact
	case "search", "message_search", "session_search", DeepAgentToolModeRAGSearch:
		return DeepAgentToolModeRAGSearch
	case DeepAgentToolModeSkill:
		return DeepAgentToolModeSkill
	case "browser", "web_fetch", "web_search", DeepAgentToolModeWeb:
		return DeepAgentToolModeWeb
	case "tests", "lint", "typecheck", "build", DeepAgentToolModeTest:
		return DeepAgentToolModeTest
	case "patch", "edit", "diff", DeepAgentToolModeCodePatch:
		return DeepAgentToolModeCodePatch
	case "subplan", DeepAgentToolModeMulti:
		return DeepAgentToolModeMulti
	default:
		return mode
	}
}

func deepAgentExecutorForMode(mode string) string {
	switch normalizeDeepAgentRouteMode(mode) {
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
	case DeepAgentToolModeMulti:
		return deepAgentRouteExecutorSubPlan
	default:
		return deepAgentRouteExecutorModel
	}
}

func deepAgentSearchScopeForMode(mode string) string {
	if normalizeDeepAgentRouteMode(mode) == DeepAgentToolModeRAGSearch {
		return "session"
	}
	return ""
}

func deepAgentDeliverableTypeForStep(step DeepAgentStep) string {
	text := deepAgentRouteText(step)
	switch {
	case deepAgentContainsAny(text, ".docx", "docx", "word document", "word 文档", "word文档"):
		return deepAgentDeliverableDocx
	case deepAgentContainsAny(text, ".svg", "svg", "流程图", "架构图", "技术图", "diagram", "flowchart", "architecture diagram"):
		return deepAgentDeliverableSVG
	case deepAgentStepLooksLikeImageGeneration(step):
		return deepAgentDeliverableImage
	case deepAgentContainsAny(text, "json"):
		return "json"
	case deepAgentContainsAny(text, ".md", "markdown", "md格式", "markdown格式"):
		return deepAgentDeliverableMarkdown
	case deepAgentStepRequiresArtifact(step):
		return deepAgentDeliverableMarkdown
	default:
		return deepAgentDeliverableNone
	}
}

func deepAgentRouteText(step DeepAgentStep) string {
	return strings.ToLower(strings.TrimSpace(strings.Join([]string{step.Intent, step.Title, step.DoneCondition}, "\n")))
}

func deepAgentRouteFilenameHint(agentState *DeepAgentState, step DeepAgentStep, route DeepAgentStepRoute) string {
	base := firstNonEmptyString(step.Title, step.Intent, stateGoal(agentState), step.ID, "result")
	base = strings.TrimSpace(base)
	ext := ".md"
	switch route.DeliverableType {
	case deepAgentDeliverableDocx:
		ext = ".docx"
	case deepAgentDeliverableImage:
		ext = ".png"
	case deepAgentDeliverableSVG:
		ext = ".svg"
	case "json":
		ext = ".json"
	}
	return sanitizeDeepAgentFilename(base, ext)
}

func sanitizeDeepAgentFilename(base, ext string) string {
	base = strings.ToLower(strings.TrimSpace(base))
	replacer := strings.NewReplacer("/", "-", "\\", "-", " ", "-", "_", "-", ".", "-", "\n", "-", "\t", "-")
	base = replacer.Replace(base)
	base = strings.Trim(base, "-")
	if base == "" {
		base = "result"
	}
	if len([]rune(base)) > 80 {
		base = string([]rune(base)[:80])
		base = strings.Trim(base, "-")
	}
	if ext != "" && !strings.HasSuffix(base, ext) {
		base += ext
	}
	return base
}

func deepAgentRoutePrompt(agentState *DeepAgentState, step DeepAgentStep) string {
	contextSummary := ""
	if planner := NewRuntimeDeepAgentPlanner(nil); planner != nil {
		contextSummary = planner.stepContextSummary(agentState, step)
	}
	return fmt.Sprintf(`Classify the next DeepAgent step into one execution route.

Return JSON only. Do not explain.

Allowed mode values: "model", "model_artifact", "skill", "rag_search", "multi".
Rules:
- Use "model" for research, analysis, outline, and normal reasoning. External web/product research should set search_scope="web" and allowed_tools=["WebSearch","WebFetch"].
- Use "model_artifact" only when this exact step must create a downloadable deliverable/file/artifact.
- Use "skill" only when a specific published skill is clearly required.
- Use "rag_search" only for prior session/history/memory search, not public web research.
- Use "multi" only if the step must be decomposed.

JSON shape:
{
  "mode": "model",
  "executor": "model",
  "skill_name": "",
  "requires_artifact": false,
  "deliverable_type": "none",
  "filename_hint": "",
  "allowed_tools": [],
  "search_scope": "",
  "success_criteria": [],
  "reason": "short reason",
  "confidence": "medium"
}

User goal:
%s

Step:
ID: %s
Title: %s
Intent: %s
Success criteria: %s

Prior step context:
%s`, stateGoal(agentState), step.ID, step.Title, step.Intent, step.DoneCondition, contextSummary)
}

func parseDeepAgentStepRoute(output string) (DeepAgentStepRoute, error) {
	result := ExtractAndValidateStructuredObject(output, deepAgentRouteStructuredSchema())
	if !result.Valid() {
		return DeepAgentStepRoute{}, fmt.Errorf("deep agent route output invalid: %w", result.Error())
	}
	data, err := json.Marshal(result.Value)
	if err != nil {
		return DeepAgentStepRoute{}, err
	}
	var route DeepAgentStepRoute
	if err := json.Unmarshal(data, &route); err != nil {
		return DeepAgentStepRoute{}, err
	}
	return route, nil
}

func deepAgentRouteStructuredSchema() StructuredSchema {
	return StructuredSchema{
		Name:    "deep_agent_step_route",
		Version: "v1",
		Schema: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"required":             []any{"mode", "executor", "requires_artifact", "deliverable_type", "allowed_tools", "success_criteria", "reason", "confidence"},
			"properties": map[string]any{
				"step_id":           map[string]any{"type": "string"},
				"mode":              map[string]any{"type": "string", "enum": []any{DeepAgentToolModeModel, DeepAgentToolModeModelArtifact, DeepAgentToolModeSkill, DeepAgentToolModeRAGSearch, DeepAgentToolModeTest, DeepAgentToolModeWeb, DeepAgentToolModeCodePatch, DeepAgentToolModeMulti, "artifact"}},
				"executor":          map[string]any{"type": "string", "enum": []any{deepAgentRouteExecutorModel, deepAgentRouteExecutorArtifact, deepAgentRouteExecutorSkill, deepAgentRouteExecutorRAG, deepAgentRouteExecutorTest, deepAgentRouteExecutorWeb, deepAgentRouteExecutorCodePatch, deepAgentRouteExecutorSubPlan}},
				"skill_name":        map[string]any{"type": "string"},
				"requires_artifact": map[string]any{"type": "boolean"},
				"deliverable_type":  map[string]any{"type": "string"},
				"filename_hint":     map[string]any{"type": "string"},
				"allowed_tools":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"search_scope":      map[string]any{"type": "string"},
				"success_criteria":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"reason":            map[string]any{"type": "string"},
				"confidence":        map[string]any{"type": "string", "enum": []any{"low", "medium", "high"}},
			},
		},
	}
}

func deepAgentStepRouteMap(route DeepAgentStepRoute) map[string]any {
	data, err := json.Marshal(route)
	if err != nil {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil
	}
	return out
}

func deepAgentStepRouteFromMap(values map[string]any) (DeepAgentStepRoute, bool) {
	if values == nil {
		return DeepAgentStepRoute{}, false
	}
	raw, ok := values["step_route"]
	if !ok || raw == nil {
		raw, ok = values["route"]
	}
	if !ok || raw == nil {
		return DeepAgentStepRoute{}, false
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return DeepAgentStepRoute{}, false
	}
	var route DeepAgentStepRoute
	if err := json.Unmarshal(data, &route); err != nil {
		return DeepAgentStepRoute{}, false
	}
	return route, true
}
