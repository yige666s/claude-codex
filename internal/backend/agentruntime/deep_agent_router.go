package agentruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"claude-codex/internal/harness/skills"
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
	deepAgentRouteExecutorConnector = "connector"

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
		"github issue", "github repo", "github repository", "github pull request", "github pr",
		"github issue", "github 仓库", "github 议题", "github issue", "github repo",
	) {
		return DeepAgentStepRoute{
			StepID:       step.ID,
			Mode:         DeepAgentToolModeConnector,
			Executor:     deepAgentRouteExecutorConnector,
			AllowedTools: []string{"github_repo_reader", "github_issue_reader"},
			SearchScope:  "github",
			Reason:       "deterministic connector guard",
			Confidence:   "high",
		}, true
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
		"网页验证", "截图", "浏览器", "打开页面", "页面验证",
	) || deepAgentContainsWebVerificationToken(text) {
		if deepAgentStepExplicitHTTPURL(step) == "" {
			return DeepAgentStepRoute{
				StepID:          step.ID,
				Mode:            DeepAgentToolModeModel,
				Executor:        deepAgentRouteExecutorModel,
				DeliverableType: deepAgentDeliverableNone,
				AllowedTools:    webResearchAllowedTools(),
				SearchScope:     "web",
				Reason:          "deterministic web research fallback without URL",
				Confidence:      "high",
			}, true
		}
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
	if deepAgentStepLooksParallelizable(step) {
		return DeepAgentStepRoute{
			StepID:          step.ID,
			Mode:            DeepAgentToolModeMulti,
			Executor:        deepAgentRouteExecutorSubPlan,
			DeliverableType: deepAgentDeliverableNone,
			AllowedTools:    []string{"WebSearch", "WebFetch"},
			SearchScope:     "web",
			Reason:          "deterministic parallel research guard",
			Confidence:      "high",
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

func deepAgentContainsWebVerificationToken(text string) bool {
	text = strings.ToLower(strings.TrimSpace(text))
	if strings.Contains(text, "web page") {
		return true
	}
	for _, token := range []string{"browser", "screenshot", "dom"} {
		if deepAgentContainsASCIIWord(text, token) {
			return true
		}
	}
	return false
}

func deepAgentStepExplicitHTTPURL(step DeepAgentStep) string {
	if args, ok := step.Metadata["args"].(map[string]any); ok {
		if url := deepAgentArgsExplicitHTTPURL(args); url != "" {
			return url
		}
	}
	return deepAgentExtractHTTPURL(strings.Join([]string{step.Intent, step.Title, step.DoneCondition}, "\n"))
}

func deepAgentArgsExplicitHTTPURL(args map[string]any) string {
	for _, key := range []string{"url", "target_url", "input"} {
		if url := deepAgentExtractHTTPURL(deepAgentWorkflowString(args, key)); url != "" {
			return url
		}
	}
	return ""
}

func deepAgentExtractHTTPURL(text string) string {
	for _, field := range strings.Fields(strings.TrimSpace(text)) {
		candidate := strings.Trim(field, " \t\r\n\"'`.,;:()[]{}<>，。；：、）】》")
		lower := strings.ToLower(candidate)
		if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
			continue
		}
		parsed, err := url.Parse(candidate)
		if err != nil || parsed == nil {
			continue
		}
		scheme := strings.ToLower(parsed.Scheme)
		if (scheme == "http" || scheme == "https") && parsed.Host != "" {
			return parsed.String()
		}
	}
	return ""
}

func deepAgentContainsASCIIWord(text, token string) bool {
	token = strings.ToLower(strings.TrimSpace(token))
	if text == "" || token == "" {
		return false
	}
	start := 0
	for {
		idx := strings.Index(text[start:], token)
		if idx < 0 {
			return false
		}
		idx += start
		beforeOK := idx == 0 || !deepAgentIsASCIIWordByte(text[idx-1])
		afterIdx := idx + len(token)
		afterOK := afterIdx >= len(text) || !deepAgentIsASCIIWordByte(text[afterIdx])
		if beforeOK && afterOK {
			return true
		}
		start = idx + len(token)
	}
}

func deepAgentIsASCIIWordByte(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '_'
}

func (r *RuntimeDeepAgentStepRouter) skillRouteForStep(step DeepAgentStep) (DeepAgentStepRoute, bool) {
	if r == nil || r.planner == nil {
		return DeepAgentStepRoute{}, false
	}
	text := deepAgentRouteText(step)
	deliverable := deepAgentDeliverableTypeForStep(step)
	var skill *skills.SkillDefinition
	var ok bool
	if deliverable == deepAgentDeliverableDocx {
		skill, ok = r.preferredDocxSkillForStep()
	}
	if !ok || skill == nil {
		skill, ok = r.planner.selectSkillForStep(step)
	}
	if !ok || skill == nil {
		return DeepAgentStepRoute{}, false
	}
	name := strings.ToLower(strings.TrimSpace(skill.Name))
	explicitSkill := strings.Contains(text, name) || deepAgentContainsAny(text, " skill", "skill ", "技能")
	matchesSpecializedDeliverable := false
	switch deliverable {
	case deepAgentDeliverableDocx:
		matchesSpecializedDeliverable = deepAgentSkillMatchesDocxDeliverable(name)
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

func (r *RuntimeDeepAgentStepRouter) preferredDocxSkillForStep() (*skills.SkillDefinition, bool) {
	if r == nil || r.runtime == nil || r.runtime.skills == nil {
		return nil, false
	}
	for _, preferred := range []string{"documents", "docx"} {
		if skill, ok := r.runtime.skills.GetSkill(preferred); ok && skill != nil && skill.UserInvocable && !skill.IsHidden {
			return skill, true
		}
	}
	for _, skill := range r.runtime.skills.ListUserInvocableSkills() {
		if skill == nil || skill.IsHidden {
			continue
		}
		if deepAgentSkillMatchesDocxDeliverable(skill.Name) {
			return skill, true
		}
	}
	return nil, false
}

func deepAgentSkillMatchesDocxDeliverable(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	return name == "documents" ||
		name == "docx" ||
		strings.Contains(name, "docx") ||
		strings.Contains(name, "document")
}

func (r *RuntimeDeepAgentStepRouter) llmRouteStep(ctx context.Context, agentState *DeepAgentState, step DeepAgentStep) (DeepAgentStepRoute, error) {
	if r == nil || r.runtime == nil {
		return DeepAgentStepRoute{}, fmt.Errorf("runtime is not configured")
	}
	userID := deepAgentWorkflowString(stateWorkingMemory(agentState), "user_id")
	sessionID := deepAgentWorkflowString(stateWorkingMemory(agentState), "session_id")
	renderedPrompt := r.deepAgentRoutePrompt(ctx, agentState, step, userID, sessionID)
	prompt := renderedPrompt.Content
	runner := r.runtime.runnerForScope(Scope{
		UserID:    userID,
		SessionID: sessionID,
		Prompt:    prompt,
	})
	result, err := runner.RunGeneratedPrompt(WithPromptMetadata(ctx, renderedPrompt.Metadata), state.NewSession(""), prompt)
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
	case "external_connector", "connector_tool", DeepAgentToolModeConnector:
		return DeepAgentToolModeConnector
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
	case DeepAgentToolModeConnector:
		return deepAgentRouteExecutorConnector
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
	case deepAgentTextRequestsDocx(text):
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

func deepAgentTextRequestsDocx(text string) bool {
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" {
		return false
	}
	if deepAgentContainsAny(text,
		".docx", "docx", "word document", "word 文档", "word文档", "word 文件", "word文件",
		"word 报告", "word报告", "word 调研", "word调研", "word 版", "word版",
		"微软 word", "microsoft word",
	) {
		return true
	}
	return deepAgentContainsASCIIWord(text, "word") && deepAgentContainsAny(text,
		"文档", "报告", "调研", "文件", "document", "report", "deliverable",
	)
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
	return fmt.Sprintf(PromptDeepAgentRouteTemplate, stateGoal(agentState), step.ID, step.Title, step.Intent, step.DoneCondition, contextSummary)
}

func (r *RuntimeDeepAgentStepRouter) deepAgentRoutePrompt(ctx context.Context, agentState *DeepAgentState, step DeepAgentStep, userID, sessionID string) deepAgentRenderedPrompt {
	contextSummary := ""
	if planner := NewRuntimeDeepAgentPlanner(r.runtime); planner != nil {
		contextSummary = planner.stepContextSummary(agentState, step)
	}
	return r.renderDeepAgentPromptForScope(ctx, PromptIDRuntimeDeepAgentRouter, userID, sessionID, stateGoal(agentState), step.ID, step.Title, step.Intent, step.DoneCondition, contextSummary)
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
				"mode":              map[string]any{"type": "string", "enum": []any{DeepAgentToolModeModel, DeepAgentToolModeModelArtifact, DeepAgentToolModeSkill, DeepAgentToolModeRAGSearch, DeepAgentToolModeTest, DeepAgentToolModeWeb, DeepAgentToolModeCodePatch, DeepAgentToolModeMulti, DeepAgentToolModeConnector, "artifact"}},
				"executor":          map[string]any{"type": "string", "enum": []any{deepAgentRouteExecutorModel, deepAgentRouteExecutorArtifact, deepAgentRouteExecutorSkill, deepAgentRouteExecutorRAG, deepAgentRouteExecutorTest, deepAgentRouteExecutorWeb, deepAgentRouteExecutorCodePatch, deepAgentRouteExecutorSubPlan, deepAgentRouteExecutorConnector}},
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
