package agentruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"claude-codex/internal/harness/state"
)

const deepResearchLLMOrchestratorVersion = "llm_v1"

// RuntimeDeepResearchOrchestrator uses the configured model to build a native
// DeepResearchPlan. The deterministic orchestrator is retained only as a
// fail-closed fallback when the model is unavailable or cannot produce a valid
// task graph after one structured-output repair attempt.
type RuntimeDeepResearchOrchestrator struct {
	runtime  *Runtime
	fallback DeepResearchOrchestrator
}

func NewRuntimeDeepResearchOrchestrator(runtime *Runtime) *RuntimeDeepResearchOrchestrator {
	return &RuntimeDeepResearchOrchestrator{
		runtime:  runtime,
		fallback: ruleDeepResearchOrchestrator{},
	}
}

func (o *RuntimeDeepResearchOrchestrator) Plan(ctx context.Context, req DeepAgentTaskRequest, cfg DeepResearchRuntimeConfig) (DeepResearchPlan, error) {
	if o == nil || o.runtime == nil {
		return DeepResearchPlan{}, fmt.Errorf("runtime deep research orchestrator is not configured")
	}
	cfg = normalizeDeepResearchRuntimeConfig(cfg)
	runner := o.runtime.runnerForScope(ctx, Scope{
		UserID:    req.UserID,
		SessionID: req.SessionID,
		Prompt:    req.Goal,
	})
	if runner == nil {
		return o.fallbackPlan(ctx, req, cfg, fmt.Errorf("deep research orchestrator model runner is not configured"))
	}

	allowedTools, toolPrompt := deepResearchOrchestratorAllowedTools(req.State)
	rendered := o.renderPlanPrompt(ctx, req, cfg, toolPrompt)
	callCtx := WithPromptMetadata(ctx, rendered.Metadata)
	result, err := runner.RunGeneratedPrompt(callCtx, state.NewSession(""), rendered.Content)
	if err != nil {
		return o.fallbackPlan(ctx, req, cfg, err)
	}

	plan, parseErr := parseRuntimeDeepResearchPlan(result.Output, req.Goal, cfg, allowedTools)
	if parseErr == nil {
		return annotateRuntimeDeepResearchPlan(plan, rendered.Metadata), nil
	}

	schema := deepResearchPlanStructuredSchema()
	validation := ExtractAndValidateStructuredObject(result.Output, schema)
	if validation.Valid() {
		validation.Errors = []StructuredValidationError{{
			Field:      "$.nodes",
			Expected:   "bounded acyclic task graph with permitted tools",
			Actual:     "semantically invalid task graph",
			Message:    parseErr.Error(),
			Repairable: true,
		}}
	}
	emitStructuredOutputValidationFailure(ctx, schema, "deep_research_orchestrator", validation)
	repairContext := o.renderRepairContext(ctx, req, cfg, toolPrompt)
	repaired, repairErr := repairStructuredJSONWithRunner(
		WithPromptMetadata(ctx, repairContext.Metadata),
		runner,
		schema,
		result.Output,
		parseErr,
		repairContext.Content,
	)
	if repairErr == nil {
		plan, parseErr = parseRuntimeDeepResearchPlan(string(repaired), req.Goal, cfg, allowedTools)
		if parseErr == nil {
			return annotateRuntimeDeepResearchPlan(plan, rendered.Metadata), nil
		}
	}

	fallbackCause := parseErr
	if repairErr != nil {
		fallbackCause = fmt.Errorf("deep research plan invalid: %v; repair failed: %w", parseErr, repairErr)
	}
	return o.fallbackPlan(ctx, req, cfg, fallbackCause)
}

// Replan reviews execution evidence and returns a replacement for the
// unfinished graph. Unlike initial planning, it has no deterministic full-plan
// fallback: retaining the current graph is safer than overwriting executed
// history when the model or structured repair fails.
func (o *RuntimeDeepResearchOrchestrator) Replan(ctx context.Context, req DeepAgentTaskRequest, run DeepResearchRunState, trigger DeepResearchReplanTrigger, cfg DeepResearchRuntimeConfig) (DeepResearchPlan, error) {
	if o == nil || o.runtime == nil {
		return DeepResearchPlan{}, fmt.Errorf("runtime deep research replanner is not configured")
	}
	cfg = normalizeDeepResearchRuntimeConfig(cfg)
	runner := o.runtime.runnerForScope(ctx, Scope{
		UserID:    req.UserID,
		SessionID: req.SessionID,
		Prompt:    req.Goal,
	})
	if runner == nil {
		return DeepResearchPlan{}, fmt.Errorf("deep research replanner model runner is not configured")
	}
	allowedTools, toolPrompt := deepResearchOrchestratorAllowedTools(req.State)
	rendered := o.renderReplanPrompt(ctx, req, run, trigger, cfg, toolPrompt)
	result, err := runner.RunGeneratedPrompt(WithPromptMetadata(ctx, rendered.Metadata), state.NewSession(""), rendered.Content)
	if err != nil {
		return DeepResearchPlan{}, err
	}
	plan, parseErr := parseRuntimeDeepResearchReplanPlan(result.Output, req.Goal, run, cfg, allowedTools)
	if parseErr == nil {
		return annotateRuntimeDeepResearchPlan(plan, rendered.Metadata), nil
	}

	schema := deepResearchPlanStructuredSchema()
	validation := ExtractAndValidateStructuredObject(result.Output, schema)
	if validation.Valid() {
		validation.Errors = []StructuredValidationError{{
			Field:      "$.nodes",
			Expected:   "valid unfinished DAG preserving successful execution history",
			Actual:     "semantically invalid replacement graph",
			Message:    parseErr.Error(),
			Repairable: true,
		}}
	}
	emitStructuredOutputValidationFailure(ctx, schema, "deep_research_replanner", validation)
	repaired, repairErr := repairStructuredJSONWithRunner(
		WithPromptMetadata(ctx, rendered.Metadata),
		runner,
		schema,
		result.Output,
		parseErr,
		rendered.Content,
	)
	if repairErr != nil {
		return DeepResearchPlan{}, fmt.Errorf("deep research replan invalid: %v; repair failed: %w", parseErr, repairErr)
	}
	plan, parseErr = parseRuntimeDeepResearchReplanPlan(string(repaired), req.Goal, run, cfg, allowedTools)
	if parseErr != nil {
		return DeepResearchPlan{}, parseErr
	}
	return annotateRuntimeDeepResearchPlan(plan, rendered.Metadata), nil
}

func (o *RuntimeDeepResearchOrchestrator) renderPlanPrompt(ctx context.Context, req DeepAgentTaskRequest, cfg DeepResearchRuntimeConfig, toolPrompt string) deepAgentRenderedPrompt {
	rubric := deepAgentRubricPrompt(req.Rubric)
	if strings.TrimSpace(rubric) == "" {
		rubric = "(none)"
	}
	connectors := strings.Join(normalizeConnectorScopes(req.ConnectorContext), "\n- ")
	if connectors == "" {
		connectors = "(none)"
	} else {
		connectors = "- " + connectors
	}
	loadedContext := deepAgentLoadedContextPrompt(req.State)
	if strings.TrimSpace(loadedContext) == "" {
		loadedContext = "(none)"
	}
	return renderRuntimeDeepAgentPrompt(
		ctx,
		o.runtime,
		PromptIDRuntimeDeepResearchOrchestrator,
		req.UserID,
		req.SessionID,
		cfg.MaxWorkers,
		cfg.MaxConcurrency,
		cfg.RequireSources,
		toolPrompt,
		rubric,
		connectors,
		loadedContext,
		strings.TrimSpace(req.Goal),
	)
}

func (o *RuntimeDeepResearchOrchestrator) renderRepairContext(ctx context.Context, req DeepAgentTaskRequest, cfg DeepResearchRuntimeConfig, toolPrompt string) deepAgentRenderedPrompt {
	return renderRuntimeDeepAgentPrompt(
		ctx,
		o.runtime,
		PromptIDRuntimeDeepResearchPlanRepair,
		req.UserID,
		req.SessionID,
		strings.TrimSpace(req.Goal),
		cfg.MaxWorkers,
		cfg.MaxConcurrency,
		toolPrompt,
	)
}

func (o *RuntimeDeepResearchOrchestrator) renderReplanPrompt(ctx context.Context, req DeepAgentTaskRequest, run DeepResearchRunState, trigger DeepResearchReplanTrigger, cfg DeepResearchRuntimeConfig, toolPrompt string) deepAgentRenderedPrompt {
	triggerJSON, _ := json.Marshal(trigger)
	return renderRuntimeDeepAgentPrompt(
		ctx,
		o.runtime,
		PromptIDRuntimeDeepResearchReplanner,
		req.UserID,
		req.SessionID,
		cfg.MaxWorkers,
		cfg.MaxConcurrency,
		cfg.RequireSources,
		toolPrompt,
		strings.TrimSpace(req.Goal),
		string(triggerJSON),
		deepResearchReplanStateJSON(run),
	)
}

func (o *RuntimeDeepResearchOrchestrator) fallbackPlan(ctx context.Context, req DeepAgentTaskRequest, cfg DeepResearchRuntimeConfig, cause error) (DeepResearchPlan, error) {
	fallback := o.fallback
	if fallback == nil {
		fallback = ruleDeepResearchOrchestrator{}
	}
	plan, err := fallback.Plan(ctx, req, cfg)
	emitStructuredOutputFallbackEvent(ctx, deepResearchPlanStructuredSchema(), "deep_research_orchestrator", structuredFallbackRulePlanner, err)
	if err != nil {
		return DeepResearchPlan{}, fmt.Errorf("deep research LLM orchestrator failed: %v; rule fallback failed: %w", cause, err)
	}
	for idx := range plan.Nodes {
		plan.Nodes[idx].Metadata = mergeDeepResearchNodeMetadata(plan.Nodes[idx].Metadata, map[string]any{
			"orchestrator_fallback": true,
			"fallback_error_class":  classifyDeepAgentError(cause, DeepAgentActionResult{}),
		})
	}
	return plan, nil
}

func parseRuntimeDeepResearchPlan(output, goal string, cfg DeepResearchRuntimeConfig, allowedTools map[string]string) (DeepResearchPlan, error) {
	validation := ExtractAndValidateStructuredObject(output, deepResearchPlanStructuredSchema())
	if !validation.Valid() {
		return DeepResearchPlan{}, validation.Error()
	}
	data, err := json.Marshal(validation.Value)
	if err != nil {
		return DeepResearchPlan{}, err
	}
	var plan DeepResearchPlan
	if err := json.Unmarshal(data, &plan); err != nil {
		return DeepResearchPlan{}, err
	}
	cfg = normalizeDeepResearchRuntimeConfig(cfg)
	if len(plan.Nodes) > cfg.MaxWorkers {
		return DeepResearchPlan{}, fmt.Errorf("deep research plan has %d nodes, maximum is %d", len(plan.Nodes), cfg.MaxWorkers)
	}
	if plan.MaxConcurrency <= 0 || plan.MaxConcurrency > cfg.MaxConcurrency {
		return DeepResearchPlan{}, fmt.Errorf("deep research max_concurrency must be between 1 and %d", cfg.MaxConcurrency)
	}
	for idx := range plan.Nodes {
		node := &plan.Nodes[idx]
		id := strings.TrimSpace(node.ID)
		if id == "" || normalizeDeepResearchID(id) != id {
			return DeepResearchPlan{}, fmt.Errorf("deep research node %d id %q must use lowercase letters, digits, and hyphens", idx, node.ID)
		}
		if strings.TrimSpace(node.Title) == "" || strings.TrimSpace(node.Description) == "" {
			return DeepResearchPlan{}, fmt.Errorf("deep research node %s requires title and description", id)
		}
		if strings.TrimSpace(node.WorkerRole) == "" {
			return DeepResearchPlan{}, fmt.Errorf("deep research node %s requires worker_role", id)
		}
		if strings.TrimSpace(node.ExpectedOutput) == "" {
			return DeepResearchPlan{}, fmt.Errorf("deep research node %s requires expected_output", id)
		}
	}
	plan = normalizeDeepResearchPlan(plan, goal, cfg)
	if err := canonicalizeDeepResearchPlanAllowedTools(&plan, allowedTools); err != nil {
		return DeepResearchPlan{}, err
	}
	if err := validateDeepResearchPlan(plan); err != nil {
		return DeepResearchPlan{}, err
	}
	return plan, nil
}

func parseRuntimeDeepResearchReplanPlan(output, goal string, run DeepResearchRunState, cfg DeepResearchRuntimeConfig, allowedTools map[string]string) (DeepResearchPlan, error) {
	validation := ExtractAndValidateStructuredObject(output, deepResearchPlanStructuredSchema())
	if !validation.Valid() {
		return DeepResearchPlan{}, validation.Error()
	}
	data, err := json.Marshal(validation.Value)
	if err != nil {
		return DeepResearchPlan{}, err
	}
	var plan DeepResearchPlan
	if err := json.Unmarshal(data, &plan); err != nil {
		return DeepResearchPlan{}, err
	}
	cfg = normalizeDeepResearchRuntimeConfig(cfg)
	if strings.TrimSpace(plan.Goal) != strings.TrimSpace(goal) {
		return DeepResearchPlan{}, fmt.Errorf("deep research replan cannot change the goal")
	}
	if plan.MaxConcurrency <= 0 || plan.MaxConcurrency > cfg.MaxConcurrency {
		return DeepResearchPlan{}, fmt.Errorf("deep research max_concurrency must be between 1 and %d", cfg.MaxConcurrency)
	}
	for idx := range plan.Nodes {
		node := &plan.Nodes[idx]
		id := strings.TrimSpace(node.ID)
		if id == "" || normalizeDeepResearchID(id) != id {
			return DeepResearchPlan{}, fmt.Errorf("deep research node %d id %q must use lowercase letters, digits, and hyphens", idx, node.ID)
		}
		if strings.TrimSpace(node.Title) == "" || strings.TrimSpace(node.Description) == "" {
			return DeepResearchPlan{}, fmt.Errorf("deep research node %s requires title and description", id)
		}
		if strings.TrimSpace(node.WorkerRole) == "" || strings.TrimSpace(node.ExpectedOutput) == "" {
			return DeepResearchPlan{}, fmt.Errorf("deep research node %s requires worker_role and expected_output", id)
		}
		if len(node.AllowedTools) == 0 {
			return DeepResearchPlan{}, fmt.Errorf("deep research node %s requires at least one allowed tool", id)
		}
		node.MaxAttempts = cfg.MaxRetries + 1
		node.TimeoutMS = cfg.WorkerTimeout.Milliseconds()
	}
	if err := canonicalizeDeepResearchPlanAllowedTools(&plan, allowedTools); err != nil {
		return DeepResearchPlan{}, err
	}
	testRun := run
	testRun.WorkerRuns = make(map[string]DeepResearchTaskNode, len(run.WorkerRuns))
	for id, node := range run.WorkerRuns {
		testRun.WorkerRuns[id] = node
	}
	if testRun.PlanRevision <= 0 {
		testRun.PlanRevision = 1
	}
	if err := applyDeepResearchReplan(&testRun, DeepResearchReplan{
		Revision: testRun.PlanRevision + 1,
		Plan:     plan,
	}, allowedTools); err != nil {
		return DeepResearchPlan{}, err
	}
	if len(testRun.Plan.Nodes) > cfg.MaxWorkers {
		return DeepResearchPlan{}, fmt.Errorf("deep research replan has %d active nodes, maximum is %d", len(testRun.Plan.Nodes), cfg.MaxWorkers)
	}
	return plan, nil
}

func deepResearchReplanStateJSON(run DeepResearchRunState) string {
	type nodeState struct {
		ID             string               `json:"id"`
		Title          string               `json:"title,omitempty"`
		Description    string               `json:"description,omitempty"`
		DependsOn      []string             `json:"depends_on,omitempty"`
		WorkerRole     string               `json:"worker_role,omitempty"`
		AllowedTools   []string             `json:"allowed_tools,omitempty"`
		ExpectedOutput string               `json:"expected_output,omitempty"`
		Required       bool                 `json:"required"`
		Status         string               `json:"status,omitempty"`
		Attempt        int                  `json:"attempt,omitempty"`
		Error          string               `json:"error,omitempty"`
		Summary        string               `json:"summary,omitempty"`
		Output         string               `json:"output,omitempty"`
		Sources        []DeepAgentSourceRef `json:"sources,omitempty"`
		OpenQuestions  []string             `json:"open_questions,omitempty"`
	}
	nodes := make([]nodeState, 0, len(run.WorkerRuns))
	for _, node := range sortedDeepResearchNodes(run.WorkerRuns) {
		item := nodeState{
			ID:             node.ID,
			Title:          node.Title,
			Description:    node.Description,
			DependsOn:      append([]string(nil), node.DependsOn...),
			WorkerRole:     node.WorkerRole,
			AllowedTools:   append([]string(nil), node.AllowedTools...),
			ExpectedOutput: node.ExpectedOutput,
			Required:       node.Required,
			Status:         node.Status,
			Attempt:        node.Attempt,
			Error:          truncateDeepAgentDiagnosticText(node.Error, 1200),
		}
		if node.Result != nil {
			item.Summary = truncateDeepAgentDiagnosticText(node.Result.Summary, 1600)
			item.Output = truncateDeepAgentDiagnosticText(node.Result.Output, 2400)
			item.Sources = append([]DeepAgentSourceRef(nil), node.Result.Sources...)
			item.OpenQuestions = append([]string(nil), node.Result.OpenQuestions...)
		}
		nodes = append(nodes, item)
	}
	payload := map[string]any{
		"plan_revision":   run.PlanRevision,
		"replan_attempts": run.ReplanAttempts,
		"replan_count":    run.ReplanCount,
		"nodes":           nodes,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "{}"
	}
	return string(encoded)
}

func canonicalizeDeepResearchPlanAllowedTools(plan *DeepResearchPlan, allowedTools map[string]string) error {
	if plan == nil {
		return fmt.Errorf("deep research plan is required")
	}
	for idx := range plan.Nodes {
		node := &plan.Nodes[idx]
		if len(node.AllowedTools) == 0 {
			return fmt.Errorf("deep research node %s requires at least one allowed tool", node.ID)
		}
		canonicalTools := make([]string, 0, len(node.AllowedTools))
		seen := map[string]bool{}
		for _, tool := range node.AllowedTools {
			key := strings.ToLower(strings.TrimSpace(tool))
			canonical, ok := allowedTools[key]
			if !ok {
				return fmt.Errorf("deep research node %s references unavailable tool %q", node.ID, tool)
			}
			canonicalKey := strings.ToLower(canonical)
			if !seen[canonicalKey] {
				seen[canonicalKey] = true
				canonicalTools = append(canonicalTools, canonical)
			}
		}
		node.AllowedTools = canonicalTools
	}
	return nil
}

func annotateRuntimeDeepResearchPlan(plan DeepResearchPlan, metadata PromptMetadata) DeepResearchPlan {
	for idx := range plan.Nodes {
		plan.Nodes[idx].Metadata = mergeDeepResearchNodeMetadata(plan.Nodes[idx].Metadata, map[string]any{
			"orchestrator":        deepResearchLLMOrchestratorVersion,
			"prompt_id":           metadata.PromptID,
			"prompt_version":      metadata.PromptVersion,
			"prompt_content_hash": metadata.PromptHash,
		})
	}
	return plan
}

func deepResearchOrchestratorAllowedTools(values map[string]any) (map[string]string, string) {
	type toolDescription struct {
		name        string
		description string
	}
	tools := map[string]toolDescription{}
	add := func(name, description string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		key := strings.ToLower(name)
		if existing, ok := tools[key]; ok && existing.description != "" && description == "" {
			return
		}
		tools[key] = toolDescription{name: name, description: strings.TrimSpace(description)}
	}
	add("model", "Reason or synthesize without external tools.")
	add("WebSearch", "Search current public web information and return traceable sources.")
	add("WebFetch", "Fetch a selected public source for detailed evidence.")
	add("repo_search", "Read-only repository search through bounded file tools.")
	add("test", "Run bounded tests or verification commands.")
	add("artifact", "Create an approved user-visible artifact.")
	if loaded, ok := deepAgentLoadedContextFromMap(values); ok {
		for _, tool := range loaded.ToolCatalog {
			description := tool.Description
			if strings.TrimSpace(tool.Permission) != "" {
				description = strings.TrimSpace(description + " Permission: " + tool.Permission + ".")
			}
			add(tool.Name, description)
		}
	}
	keys := make([]string, 0, len(tools))
	for key := range tools {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	allowed := make(map[string]string, len(keys))
	lines := make([]string, 0, len(keys))
	for _, key := range keys {
		tool := tools[key]
		allowed[key] = tool.name
		line := "- " + tool.name
		if tool.description != "" {
			line += ": " + truncateDeepAgentDiagnosticText(tool.description, 300)
		}
		lines = append(lines, line)
	}
	return allowed, strings.Join(lines, "\n")
}

func deepResearchPlanStructuredSchema() StructuredSchema {
	stringArray := map[string]any{
		"type":  "array",
		"items": map[string]any{"type": "string"},
	}
	return StructuredSchema{
		Name:    "deep_research_plan",
		Version: "v1",
		Schema: map[string]any{
			"type":                 "object",
			"required":             []any{"goal", "max_concurrency", "nodes"},
			"additionalProperties": false,
			"properties": map[string]any{
				"goal":            map[string]any{"type": "string"},
				"max_concurrency": map[string]any{"type": "integer"},
				"nodes": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type":                 "object",
						"required":             []any{"id", "title", "description", "depends_on", "worker_role", "allowed_tools", "expected_output", "required"},
						"additionalProperties": false,
						"properties": map[string]any{
							"id":              map[string]any{"type": "string"},
							"title":           map[string]any{"type": "string"},
							"description":     map[string]any{"type": "string"},
							"depends_on":      stringArray,
							"worker_role":     map[string]any{"type": "string"},
							"allowed_tools":   stringArray,
							"expected_output": map[string]any{"type": "string"},
							"required":        map[string]any{"type": "boolean"},
						},
					},
				},
			},
		},
	}
}
