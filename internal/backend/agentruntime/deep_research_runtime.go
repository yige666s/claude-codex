package agentruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
)

type ruleDeepResearchOrchestrator struct{}

func (ruleDeepResearchOrchestrator) Plan(_ context.Context, req DeepAgentTaskRequest, cfg DeepResearchRuntimeConfig) (DeepResearchPlan, error) {
	goal := strings.TrimSpace(req.Goal)
	if goal == "" {
		return DeepResearchPlan{}, fmt.Errorf("deep research goal is required")
	}
	cfg = normalizeDeepResearchRuntimeConfig(cfg)
	category := classifyDeepResearchGoal(goal)
	nodes := ruleDeepResearchNodesForCategory(category)
	nodes = truncateDeepResearchNodesToValidDAG(nodes, cfg.MaxWorkers)
	for idx := range nodes {
		nodes[idx].MaxAttempts = cfg.MaxRetries + 1
		nodes[idx].TimeoutMS = cfg.WorkerTimeout.Milliseconds()
		nodes[idx].Metadata = mergeDeepResearchNodeMetadata(nodes[idx].Metadata, map[string]any{
			"orchestrator":      "rule_v1",
			"goal_category":     category,
			"deliverable_node":  deepResearchNodeLooksDeliverable(nodes[idx]),
			"max_workers_limit": cfg.MaxWorkers,
		})
	}
	return DeepResearchPlan{Goal: goal, MaxConcurrency: cfg.MaxConcurrency, Nodes: nodes}, nil
}

type runtimeDeepResearchWorkerExecutor struct {
	runtime       *Runtime
	inline        DeepAgentExecutor
	harnessRunner DeepResearchHarnessAgentRunner
}

func newRuntimeDeepResearchWorkerExecutor(runtime *Runtime, inline DeepAgentExecutor) *runtimeDeepResearchWorkerExecutor {
	if inline == nil && runtime != nil {
		inline = NewRuntimeDeepAgentExecutor(runtime)
	}
	var harnessRunner DeepResearchHarnessAgentRunner
	if runtime != nil {
		harnessRunner = runtime.deepResearchAgent
	}
	return &runtimeDeepResearchWorkerExecutor{runtime: runtime, inline: inline, harnessRunner: harnessRunner}
}

func (e *runtimeDeepResearchWorkerExecutor) ExecuteWorker(ctx context.Context, input DeepResearchWorkerInput) (DeepResearchWorkerResult, error) {
	if e == nil {
		return DeepResearchWorkerResult{}, fmt.Errorf("deep research worker executor is not configured")
	}
	if strings.EqualFold(strings.TrimSpace(input.Backend), DeepResearchWorkerBackendHarnessAgent) {
		if e.harnessRunner != nil {
			out, err := e.harnessRunner.RunDeepResearchAgent(ctx, input)
			if out.Metadata == nil {
				out.Metadata = map[string]any{}
			}
			out.Metadata["worker_backend"] = DeepResearchWorkerBackendHarnessAgent
			return out, err
		}
		input = cloneDeepResearchWorkerInput(input)
		input.WorkingMemory["worker_backend_fallback"] = "harness_agent_runner_not_configured"
	}
	if e.inline == nil {
		return DeepResearchWorkerResult{}, fmt.Errorf("inline deep research worker backend is not configured")
	}
	action := deepResearchWorkerAction(input)
	agentState := &DeepAgentState{
		Goal:          input.Goal,
		Status:        DeepAgentRunStatusRunning,
		WorkingMemory: cloneWorkflowMap(input.WorkingMemory),
		StartedAt:     time.Now().UTC(),
		UpdatedAt:     time.Now().UTC(),
	}
	result, err := e.inline.ExecuteDeepAgentAction(ctx, action, agentState)
	out := deepResearchWorkerResultFromActionResult(input.Node, result)
	out.AgentRunID = firstNonEmptyString(out.AgentRunID, fmt.Sprintf("inline-%s-attempt-%d", input.Node.ID, maxInt(input.Node.Attempt, 1)))
	if out.Metadata == nil {
		out.Metadata = map[string]any{}
	}
	out.Metadata["worker_backend"] = firstNonEmptyString(input.Backend, DeepResearchWorkerBackendInline)
	if fallback := deepAgentWorkflowString(input.WorkingMemory, "worker_backend_fallback"); fallback != "" {
		out.Metadata["worker_backend_fallback"] = fallback
	}
	return out, err
}

func cloneDeepResearchWorkerInput(input DeepResearchWorkerInput) DeepResearchWorkerInput {
	input.DependencyOutput = append([]DeepResearchWorkerResult(nil), input.DependencyOutput...)
	input.ConnectorContext = append([]string(nil), input.ConnectorContext...)
	input.WorkingMemory = cloneWorkflowMap(input.WorkingMemory)
	if input.WorkingMemory == nil {
		input.WorkingMemory = map[string]any{}
	}
	return input
}

type noopDeepResearchWorker struct{}

func (noopDeepResearchWorker) ExecuteWorker(context.Context, DeepResearchWorkerInput) (DeepResearchWorkerResult, error) {
	return DeepResearchWorkerResult{}, fmt.Errorf("deep research worker is not configured")
}

type ruleDeepResearchAggregator struct {
	requireSources bool
}

func (a ruleDeepResearchAggregator) Aggregate(_ context.Context, run DeepResearchRunState) (DeepResearchAggregateResult, error) {
	results := map[string]DeepResearchWorkerResult{}
	var findings []DeepResearchFinding
	var sources []DeepAgentSourceRef
	var trustedSources []DeepAgentSourceRef
	var artifacts []DeepAgentArtifactRef
	var errors []string
	for _, node := range sortedDeepResearchNodes(run.WorkerRuns) {
		if node.Result == nil {
			if node.Required {
				errors = append(errors, fmt.Sprintf("%s missing result", node.ID))
			}
			continue
		}
		results[node.ID] = *node.Result
		findings = append(findings, node.Result.Findings...)
		sources = append(sources, node.Result.Sources...)
		trustedSources = append(trustedSources, deepResearchTrustedSources(*node.Result)...)
		artifacts = append(artifacts, node.Result.Artifacts...)
		errors = append(errors, node.Result.Errors...)
	}
	sources = dedupeDeepResearchSources(sources)
	trustedSources = dedupeDeepResearchSources(trustedSources)
	findings = deepResearchFindingsWithTrustedCitations(findings, trustedSources)
	artifacts = dedupeDeepResearchArtifacts(artifacts)
	if a.requireSources && len(trustedSources) == 0 {
		return DeepResearchAggregateResult{
			Status:        DeepResearchRunStatusFailed,
			WorkerResults: results,
			Findings:      findings,
			Errors:        append(errors, "trusted source evidence is required but no worker returned verified sources"),
			Partial:       true,
		}, fmt.Errorf("deep research trusted source evidence is missing")
	}
	summary := fmt.Sprintf("Deep research completed with %d worker results and %d trusted sources.", len(results), len(trustedSources))
	final := buildDeepResearchFinalAnswer(run, findings, trustedSources, errors)
	return DeepResearchAggregateResult{
		Status:        DeepResearchRunStatusSucceeded,
		Summary:       summary,
		FinalAnswer:   final,
		Findings:      findings,
		Sources:       trustedSources,
		Artifacts:     artifacts,
		WorkerResults: results,
		Partial:       len(errors) > 0,
		Errors:        compactStringSlice(errors),
	}, nil
}

func normalizeDeepResearchRuntimeConfig(cfg DeepResearchRuntimeConfig) DeepResearchRuntimeConfig {
	cfg.WorkerBackend = strings.TrimSpace(cfg.WorkerBackend)
	if cfg.WorkerBackend == "" {
		cfg.WorkerBackend = DeepResearchWorkerBackendInline
	}
	if cfg.MaxWorkers <= 0 {
		cfg.MaxWorkers = 8
	}
	if cfg.MaxConcurrency <= 0 {
		cfg.MaxConcurrency = 3
	}
	if cfg.MaxConcurrency > cfg.MaxWorkers {
		cfg.MaxConcurrency = cfg.MaxWorkers
	}
	if cfg.WorkerTimeout <= 0 {
		cfg.WorkerTimeout = 5 * time.Minute
	}
	if cfg.TotalTimeout <= 0 {
		cfg.TotalTimeout = 20 * time.Minute
	}
	if cfg.MaxRetries < 0 {
		cfg.MaxRetries = 0
	}
	if cfg.ReplanEnabled {
		if cfg.MaxReplans <= 0 {
			cfg.MaxReplans = 3
		}
		if cfg.ReplanEveryBatches <= 0 {
			cfg.ReplanEveryBatches = 1
		}
	}
	if cfg.MinSuccessfulWorkers <= 0 {
		cfg.MinSuccessfulWorkers = 1
	}
	return cfg
}

func deepResearchConfigSnapshot(cfg DeepResearchRuntimeConfig) DeepResearchRuntimeConfigSnapshot {
	cfg = normalizeDeepResearchRuntimeConfig(cfg)
	return DeepResearchRuntimeConfigSnapshot{
		WorkerBackend:        cfg.WorkerBackend,
		MaxWorkers:           cfg.MaxWorkers,
		MaxConcurrency:       cfg.MaxConcurrency,
		WorkerTimeoutMS:      cfg.WorkerTimeout.Milliseconds(),
		TotalTimeoutMS:       cfg.TotalTimeout.Milliseconds(),
		MaxRetries:           cfg.MaxRetries,
		ReplanEnabled:        cfg.ReplanEnabled,
		MaxReplans:           cfg.MaxReplans,
		ReplanEveryBatches:   cfg.ReplanEveryBatches,
		RequireSources:       cfg.RequireSources,
		MinSuccessfulWorkers: cfg.MinSuccessfulWorkers,
	}
}

func normalizeDeepResearchPlan(plan DeepResearchPlan, goal string, cfg DeepResearchRuntimeConfig) DeepResearchPlan {
	cfg = normalizeDeepResearchRuntimeConfig(cfg)
	plan.Goal = firstNonEmptyString(strings.TrimSpace(plan.Goal), strings.TrimSpace(goal))
	if plan.MaxConcurrency <= 0 {
		plan.MaxConcurrency = cfg.MaxConcurrency
	}
	if plan.MaxConcurrency > cfg.MaxConcurrency {
		plan.MaxConcurrency = cfg.MaxConcurrency
	}
	plan.Nodes = truncateDeepResearchNodesToValidDAG(plan.Nodes, cfg.MaxWorkers)
	for idx := range plan.Nodes {
		node := &plan.Nodes[idx]
		node.ID = normalizeDeepResearchID(firstNonEmptyString(node.ID, node.Title, fmt.Sprintf("worker-%d", idx+1)))
		node.Title = firstNonEmptyString(strings.TrimSpace(node.Title), node.ID)
		node.Description = strings.TrimSpace(node.Description)
		node.WorkerRole = firstNonEmptyString(strings.TrimSpace(node.WorkerRole), "researcher")
		node.ExpectedOutput = firstNonEmptyString(strings.TrimSpace(node.ExpectedOutput), "facts_with_sources")
		if node.MaxAttempts <= 0 {
			node.MaxAttempts = cfg.MaxRetries + 1
		}
		if node.TimeoutMS <= 0 {
			node.TimeoutMS = cfg.WorkerTimeout.Milliseconds()
		}
		if len(node.AllowedTools) == 0 {
			node.AllowedTools = []string{"WebSearch", "WebFetch"}
		}
		node.DependsOn = compactStringSlice(node.DependsOn)
	}
	return plan
}

func validateDeepResearchPlan(plan DeepResearchPlan) error {
	if strings.TrimSpace(plan.Goal) == "" {
		return fmt.Errorf("deep research plan goal is required")
	}
	if len(plan.Nodes) == 0 {
		return fmt.Errorf("deep research plan requires at least one node")
	}
	ids := map[string]struct{}{}
	for _, node := range plan.Nodes {
		if strings.TrimSpace(node.ID) == "" {
			return fmt.Errorf("deep research node id is required")
		}
		if _, ok := ids[node.ID]; ok {
			return fmt.Errorf("duplicate deep research node id: %s", node.ID)
		}
		ids[node.ID] = struct{}{}
	}
	visiting := map[string]bool{}
	visited := map[string]bool{}
	byID := map[string]DeepResearchTaskNode{}
	for _, node := range plan.Nodes {
		byID[node.ID] = node
		for _, dep := range node.DependsOn {
			if _, ok := ids[dep]; !ok {
				return fmt.Errorf("deep research node %s depends on unknown node %s", node.ID, dep)
			}
		}
	}
	var visit func(string) error
	visit = func(id string) error {
		if visiting[id] {
			return fmt.Errorf("deep research task graph has cycle at %s", id)
		}
		if visited[id] {
			return nil
		}
		visiting[id] = true
		for _, dep := range byID[id].DependsOn {
			if err := visit(dep); err != nil {
				return err
			}
		}
		visiting[id] = false
		visited[id] = true
		return nil
	}
	for id := range ids {
		if err := visit(id); err != nil {
			return err
		}
	}
	return nil
}

func deepAgentPlanFromDeepResearchPlan(plan DeepResearchPlan) DeepAgentPlan {
	out := DeepAgentPlan{Goal: plan.Goal, Steps: make([]DeepAgentStep, 0, len(plan.Nodes))}
	for _, node := range plan.Nodes {
		out.Steps = append(out.Steps, DeepAgentStep{
			ID:            node.ID,
			Title:         node.Title,
			Intent:        firstNonEmptyString(node.Description, node.Title),
			DependsOn:     append([]string(nil), node.DependsOn...),
			Status:        DeepAgentStepStatusPending,
			DoneCondition: firstNonEmptyString(node.ExpectedOutput, "worker result is available"),
			Metadata: map[string]any{
				"deep_research_worker": true,
				"worker_role":          node.WorkerRole,
				"required":             node.Required,
				"allowed_tools":        node.AllowedTools,
			},
		})
	}
	return out
}

func deepResearchWorkerAction(input DeepResearchWorkerInput) DeepAgentAction {
	route := DeepAgentStepRoute{
		StepID:          input.Node.ID,
		Mode:            DeepAgentToolModeModel,
		Executor:        deepAgentRouteExecutorModel,
		AllowedTools:    append([]string(nil), input.Node.AllowedTools...),
		SearchScope:     "web",
		SuccessCriteria: []string{input.Node.ExpectedOutput},
		Reason:          "deep research worker node",
	}
	prompt := deepResearchWorkerPrompt(input)
	return DeepAgentAction{
		StepID: input.Node.ID,
		Tool:   DeepAgentToolModeModel,
		Args: map[string]any{
			"prompt":            prompt,
			"goal":              input.Goal,
			"step_id":           input.Node.ID,
			"step_title":        input.Node.Title,
			"done_condition":    input.Node.ExpectedOutput,
			"expected_evidence": input.Node.ExpectedOutput,
			"allowed_tools":     input.Node.AllowedTools,
			"deep_research":     true,
			"worker_role":       input.Node.WorkerRole,
			"step_route":        deepAgentStepRouteMap(route),
			"source_policy":     input.WorkingMemory["source_policy"],
			"connector_context": input.ConnectorContext,
		},
	}
}

func deepResearchWorkerPrompt(input DeepResearchWorkerInput) string {
	var b strings.Builder
	b.WriteString("You are a bounded Deep Research worker.\n")
	b.WriteString("Parent goal: ")
	b.WriteString(input.Goal)
	b.WriteString("\n\nWorker task: ")
	b.WriteString(firstNonEmptyString(input.Node.Description, input.Node.Title))
	b.WriteString("\nWorker role: ")
	b.WriteString(firstNonEmptyString(input.Node.WorkerRole, "researcher"))
	b.WriteString("\nExpected output: ")
	b.WriteString(firstNonEmptyString(input.Node.ExpectedOutput, "facts_with_sources"))
	if input.Node.Attempt > 1 && (input.Node.Result != nil || strings.TrimSpace(input.Node.Error) != "") {
		b.WriteString("\n\nPrevious attempt failed. Change the strategy instead of repeating the same approach.")
		if input.Node.Result != nil && strings.TrimSpace(input.Node.Result.Summary) != "" {
			b.WriteString("\nPrevious failure summary: ")
			b.WriteString(truncateDeepAgentDiagnosticText(input.Node.Result.Summary, 800))
		}
		if strings.TrimSpace(input.Node.Error) != "" {
			b.WriteString("\nPrevious error: ")
			b.WriteString(truncateDeepAgentDiagnosticText(input.Node.Error, 800))
		}
	}
	if len(input.DependencyOutput) > 0 {
		b.WriteString("\n\nDependency outputs:")
		for _, dep := range input.DependencyOutput {
			b.WriteString("\n- ")
			b.WriteString(truncateDeepAgentDiagnosticText(firstNonEmptyString(dep.Summary, dep.Output), 800))
		}
	}
	b.WriteString("\n\nReturn concise findings with source evidence when tools are available. Do not create another multi-agent plan.")
	return b.String()
}

func deepResearchWorkerResultFromActionResult(node DeepResearchTaskNode, result DeepAgentActionResult) DeepResearchWorkerResult {
	status := firstNonEmptyString(result.Status, DeepAgentActionStatusSucceeded)
	out := DeepResearchWorkerResult{
		Status:   status,
		Output:   strings.TrimSpace(result.Output),
		Summary:  truncateDeepAgentDiagnosticText(firstNonEmptyString(result.Output, result.Error, node.Title), 800),
		Metadata: cloneWorkflowMap(result.Metadata),
	}
	if out.Metadata == nil {
		out.Metadata = map[string]any{}
	}
	if result.Error != "" {
		out.Errors = append(out.Errors, result.Error)
	}
	if evidence, ok := deepAgentStepEvidenceFromAny(result.Metadata["step_evidence"]); ok {
		out.Output = firstNonEmptyString(out.Output, evidence.Output, evidence.Summary)
		out.Summary = firstNonEmptyString(evidence.Summary, truncateDeepAgentDiagnosticText(evidence.Output, 800), out.Summary)
		out.Sources = append(out.Sources, evidence.Sources...)
		out.Artifacts = append(out.Artifacts, evidence.Artifacts...)
		out.ToolCalls = append(out.ToolCalls, evidence.ToolCalls...)
	}
	out.Sources = append(out.Sources, deepAgentSourceRefsFromAny(result.Metadata["sources"])...)
	out.Artifacts = append(out.Artifacts, deepAgentArtifactRefsFromMetadata(result.Metadata)...)
	out.ToolCalls = append(out.ToolCalls, deepAgentToolCallRefsFromMetadata(result.Metadata)...)
	out.Sources = dedupeDeepResearchSources(out.Sources)
	out.Artifacts = dedupeDeepResearchArtifacts(out.Artifacts)
	out.Findings = []DeepResearchFinding{{
		Claim:      firstNonEmptyString(out.Summary, node.Title),
		Evidence:   truncateDeepAgentDiagnosticText(firstNonEmptyString(out.Output, out.Summary), 1000),
		Confidence: "medium",
	}}
	if len(out.Sources) > 0 {
		out.Findings[0].SourceURL = out.Sources[0].URL
	}
	return out
}

func deepResearchEvidenceFromWorker(node DeepResearchTaskNode, result DeepResearchWorkerResult) DeepAgentStepEvidence {
	return DeepAgentStepEvidence{
		StepID:    node.ID,
		ActionID:  fmt.Sprintf("%s-attempt-%d", node.ID, node.Attempt),
		Output:    firstNonEmptyString(result.Output, result.Summary),
		Summary:   result.Summary,
		Sources:   result.Sources,
		Artifacts: result.Artifacts,
		ToolCalls: result.ToolCalls,
		Diagnostics: map[string]any{
			"deep_research": true,
			"worker_role":   node.WorkerRole,
			"agent_run_id":  node.AgentRunID,
			"attempt":       node.Attempt,
			"errors":        result.Errors,
		},
	}
}

func emitDeepResearchEvent(ctx context.Context, eventType, sessionID, jobID, content string, data map[string]any) {
	data = enrichDeepResearchEventData(eventType, data)
	emitJobEventFromContext(ctx, Event{
		Type:      eventType,
		SessionID: sessionID,
		JobID:     jobID,
		Role:      "workflow",
		Content:   content,
		Data:      deepAgentEventData(data),
	})
	compat := ""
	switch eventType {
	case "deep_research_plan_created":
		compat = "deep_agent_parallel_group_started"
	case "deep_research_worker_started":
		compat = "deep_agent_parallel_branch_started"
	case "deep_research_worker_succeeded":
		compat = "deep_agent_parallel_branch_succeeded"
	case "deep_research_completed":
		compat = "deep_agent_parallel_group_joined"
	}
	if compat != "" {
		emitJobEventFromContext(ctx, Event{
			Type:      compat,
			SessionID: sessionID,
			JobID:     jobID,
			Role:      "workflow",
			Content:   content,
			Data:      deepAgentEventData(data),
		})
	}
}

func enrichDeepResearchEventData(eventType string, data map[string]any) map[string]any {
	if data == nil {
		data = map[string]any{}
	}
	if _, ok := data["event_group"]; !ok {
		data["event_group"] = "deep_research"
	}
	if strings.HasPrefix(eventType, "deep_research_worker_") {
		workerID := firstNonEmptyString(deepAgentWorkflowString(data, "worker_id"), deepAgentWorkflowString(data, "task_id"))
		if workerID != "" {
			data["branch_id"] = firstNonEmptyString(deepAgentWorkflowString(data, "branch_id"), workerID)
		}
		if title := firstNonEmptyString(deepAgentWorkflowString(data, "worker_title"), deepAgentWorkflowString(data, "branch_title")); title != "" {
			data["branch_title"] = title
		} else if node, ok := data["node"].(DeepResearchTaskNode); ok {
			data["branch_title"] = firstNonEmptyString(node.Title, node.ID)
		}
		data["parallel_group_id"] = firstNonEmptyString(deepAgentWorkflowString(data, "parallel_group_id"), "deep_research")
	}
	if eventType == "deep_research_plan_created" || eventType == "deep_research_completed" {
		data["parallel_group_id"] = firstNonEmptyString(deepAgentWorkflowString(data, "parallel_group_id"), "deep_research")
		if count := deepAgentAnyInt(data["node_count"], 0); count > 0 {
			data["branch_count"] = count
		}
		if count := deepAgentAnyInt(data["worker_count"], 0); count > 0 {
			data["branch_count"] = count
			data["succeeded"] = count
		}
	}
	return data
}

func deepResearchDependenciesSucceeded(nodes map[string]DeepResearchTaskNode, node DeepResearchTaskNode) bool {
	for _, dep := range node.DependsOn {
		if nodes[dep].Status != DeepResearchTaskStatusSucceeded {
			return false
		}
	}
	return true
}

func markDeepResearchDependencyBlocks(run *DeepResearchRunState) int {
	if run == nil {
		return 0
	}
	count := 0
	for id, node := range run.WorkerRuns {
		if node.Status != DeepResearchTaskStatusPending && node.Status != "" {
			continue
		}
		var blocked []string
		for _, dep := range node.DependsOn {
			status := run.WorkerRuns[dep].Status
			if status == DeepResearchTaskStatusFailedFinal || status == DeepResearchTaskStatusBlockedByDependency {
				blocked = append(blocked, dep)
			}
		}
		if len(blocked) > 0 {
			node.Status = DeepResearchTaskStatusBlockedByDependency
			node.BlockedBy = blocked
			run.WorkerRuns[id] = node
			count++
		}
	}
	return count
}

func allDeepResearchTasksTerminal(nodes map[string]DeepResearchTaskNode) bool {
	for _, node := range nodes {
		switch node.Status {
		case DeepResearchTaskStatusSucceeded, DeepResearchTaskStatusFailedFinal, DeepResearchTaskStatusBlockedByDependency, DeepResearchTaskStatusSkipped, DeepResearchTaskStatusCancelled:
		default:
			return false
		}
	}
	return len(nodes) > 0
}

func countDeepResearchWorkerOutcomes(nodes map[string]DeepResearchTaskNode) (successes int, requiredFailures int) {
	for _, node := range nodes {
		if node.Status == DeepResearchTaskStatusSucceeded {
			successes++
		}
		if node.Required && (node.Status == DeepResearchTaskStatusFailedFinal || node.Status == DeepResearchTaskStatusBlockedByDependency) {
			requiredFailures++
		}
	}
	return successes, requiredFailures
}

func deepResearchDependencyResults(run DeepResearchRunState, node DeepResearchTaskNode) []DeepResearchWorkerResult {
	out := []DeepResearchWorkerResult{}
	for _, dep := range node.DependsOn {
		depNode := run.WorkerRuns[dep]
		if depNode.Result != nil {
			out = append(out, *depNode.Result)
		}
	}
	return out
}

func deepResearchActionCount(nodes map[string]DeepResearchTaskNode) int {
	total := 0
	for _, node := range nodes {
		total += node.Attempt
	}
	return total
}

func sortedDeepResearchNodes(nodes map[string]DeepResearchTaskNode) []DeepResearchTaskNode {
	out := make([]DeepResearchTaskNode, 0, len(nodes))
	for _, node := range nodes {
		out = append(out, node)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func normalizeDeepResearchID(text string) string {
	text = strings.ToLower(strings.TrimSpace(text))
	text = strings.ReplaceAll(text, "_", "-")
	var b strings.Builder
	lastDash := false
	for _, r := range text {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if ok {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "worker"
	}
	return out
}

func dedupeDeepResearchSources(in []DeepAgentSourceRef) []DeepAgentSourceRef {
	seen := map[string]struct{}{}
	out := []DeepAgentSourceRef{}
	for _, source := range in {
		key := firstNonEmptyString(source.URL, source.Title, source.Snippet)
		if strings.TrimSpace(key) == "" {
			continue
		}
		if parsed, err := url.Parse(source.URL); err == nil && parsed.Host != "" {
			source.Domain = firstNonEmptyString(source.Domain, parsed.Host)
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, source)
	}
	return out
}

func dedupeDeepResearchArtifacts(in []DeepAgentArtifactRef) []DeepAgentArtifactRef {
	seen := map[string]struct{}{}
	out := []DeepAgentArtifactRef{}
	for _, artifact := range in {
		key := firstNonEmptyString(artifact.ID, artifact.Filename)
		if strings.TrimSpace(key) == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, artifact)
	}
	return out
}

func buildDeepResearchFinalAnswer(run DeepResearchRunState, findings []DeepResearchFinding, sources []DeepAgentSourceRef, errors []string) string {
	var b strings.Builder
	b.WriteString("深入研究结果\n\n")
	b.WriteString("目标：")
	b.WriteString(run.Goal)
	b.WriteString("\n\n结论：")
	for _, finding := range findings {
		claim := strings.TrimSpace(finding.Claim)
		if claim == "" {
			continue
		}
		b.WriteString("\n- ")
		b.WriteString(claim)
		if finding.SourceURL != "" {
			b.WriteString("（")
			b.WriteString(finding.SourceURL)
			b.WriteString("）")
		}
	}
	if len(sources) > 0 {
		b.WriteString("\n\n来源：")
		for _, source := range sources {
			b.WriteString("\n- ")
			b.WriteString(firstNonEmptyString(source.Title, source.URL, source.Snippet))
			if source.URL != "" {
				b.WriteString(" ")
				b.WriteString(source.URL)
			}
		}
	}
	if len(errors) > 0 {
		b.WriteString("\n\n未完全解决：")
		for _, err := range compactStringSlice(errors) {
			b.WriteString("\n- ")
			b.WriteString(err)
		}
	}
	return b.String()
}

func classifyDeepResearchGoal(goal string) string {
	lower := strings.ToLower(strings.TrimSpace(goal))
	switch {
	case deepAgentContainsAny(lower, "repo", "repository", "codebase", "architecture", "implementation", "code review", "代码", "仓库", "项目", "架构", "实现", "测试"):
		return "codebase"
	case deepAgentContainsAny(lower, "competitor", "competitive", "alternatives", "versus", "compare", "竞品", "竞争", "对比", "替代"):
		return "competitive"
	case deepAgentContainsAny(lower, "pricing", "price", "plan", "subscription", "定价", "价格", "套餐"):
		return "pricing"
	default:
		return "product"
	}
}

func ruleDeepResearchNodesForCategory(category string) []DeepResearchTaskNode {
	switch category {
	case "codebase":
		return []DeepResearchTaskNode{
			{
				ID:             "overview",
				Title:          "需求与仓库背景",
				Description:    "确认目标范围、相关模块、调用入口和关键上下文。",
				WorkerRole:     "researcher",
				AllowedTools:   []string{"repo_search"},
				ExpectedOutput: "repo_scope_summary",
				Required:       true,
			},
			{
				ID:             "codebase",
				Title:          "代码与实现路径调研",
				Description:    "分析相关代码路径、模块边界、关键数据流、测试入口和实现风险。",
				WorkerRole:     "code_worker",
				AllowedTools:   []string{"repo_search", "test"},
				ExpectedOutput: "code_findings",
				Required:       true,
			},
			{
				ID:             "details",
				Title:          "运行限制与验证要点",
				Description:    "整理配置、依赖、边界条件、验证方法和剩余风险。",
				WorkerRole:     "researcher",
				AllowedTools:   []string{"repo_search", "test"},
				ExpectedOutput: "verification_notes",
				Required:       true,
			},
			{
				ID:             "synthesis",
				Title:          "代码结论与交付草稿",
				Description:    "基于前置 worker 输出，生成结构化实现结论和交付草稿。",
				DependsOn:      []string{"overview", "codebase", "details"},
				WorkerRole:     "writer",
				AllowedTools:   []string{"model"},
				ExpectedOutput: "markdown_report",
				Required:       true,
			},
		}
	case "pricing":
		return []DeepResearchTaskNode{
			{
				ID:             "overview",
				Title:          "产品与背景调研",
				Description:    "收集目标对象的定位、核心能力、目标用户和背景信息。",
				WorkerRole:     "researcher",
				AllowedTools:   []string{"WebSearch", "WebFetch"},
				ExpectedOutput: "facts_with_sources",
				Required:       true,
			},
			{
				ID:             "details",
				Title:          "价格、功能与关键细节",
				Description:    "收集价格、套餐、限制、功能清单、使用方式和关键细节。",
				WorkerRole:     "researcher",
				AllowedTools:   []string{"WebSearch", "WebFetch"},
				ExpectedOutput: "facts_with_sources",
				Required:       true,
			},
			{
				ID:             "market",
				Title:          "市场对标与用户反馈",
				Description:    "收集竞品、用户反馈、优缺点和风险。",
				WorkerRole:     "researcher",
				AllowedTools:   []string{"WebSearch", "WebFetch"},
				ExpectedOutput: "facts_with_sources",
				Required:       true,
			},
			{
				ID:             "synthesis",
				Title:          "综合分析与报告草稿",
				Description:    "基于前置 worker 输出，生成结构化分析和报告草稿。",
				DependsOn:      []string{"overview", "details", "market"},
				WorkerRole:     "writer",
				AllowedTools:   []string{"model"},
				ExpectedOutput: "markdown_report",
				Required:       true,
			},
		}
	default:
		return []DeepResearchTaskNode{
			{
				ID:             "overview",
				Title:          "产品与背景调研",
				Description:    "收集目标对象的定位、核心能力、目标用户和背景信息。",
				WorkerRole:     "researcher",
				AllowedTools:   []string{"WebSearch", "WebFetch"},
				ExpectedOutput: "facts_with_sources",
				Required:       true,
			},
			{
				ID:             "market",
				Title:          "市场、竞品与用户反馈",
				Description:    "收集市场位置、竞品、用户反馈、优缺点和风险。",
				WorkerRole:     "researcher",
				AllowedTools:   []string{"WebSearch", "WebFetch"},
				ExpectedOutput: "facts_with_sources",
				Required:       true,
			},
			{
				ID:             "details",
				Title:          "价格、功能与关键细节",
				Description:    "收集价格、套餐、限制、功能清单、使用方式和关键细节。",
				WorkerRole:     "researcher",
				AllowedTools:   []string{"WebSearch", "WebFetch"},
				ExpectedOutput: "facts_with_sources",
				Required:       true,
			},
			{
				ID:             "synthesis",
				Title:          "综合分析与报告草稿",
				Description:    "基于前置 worker 输出，生成结构化分析和报告草稿。",
				DependsOn:      []string{"overview", "market", "details"},
				WorkerRole:     "writer",
				AllowedTools:   []string{"model"},
				ExpectedOutput: "markdown_report",
				Required:       true,
			},
		}
	}
}

func truncateDeepResearchNodesToValidDAG(nodes []DeepResearchTaskNode, maxWorkers int) []DeepResearchTaskNode {
	if maxWorkers <= 0 || len(nodes) <= maxWorkers {
		return append([]DeepResearchTaskNode(nil), nodes...)
	}
	if len(nodes) == 0 {
		return nil
	}
	// With a single worker, keep a source-gathering node. Aggregation already
	// produces the final answer; retaining only a writer node would leave it with
	// no evidence and make RequireSources impossible to satisfy.
	if maxWorkers == 1 {
		out := append([]DeepResearchTaskNode(nil), nodes[:1]...)
		return deepResearchTrimNodeDependencies(out)
	}
	if !deepResearchNodeLooksDeliverable(nodes[len(nodes)-1]) {
		out := append([]DeepResearchTaskNode(nil), nodes[:maxWorkers]...)
		return deepResearchTrimNodeDependencies(out)
	}
	deliverable := nodes[len(nodes)-1]
	prereqLimit := maxWorkers - 1
	if prereqLimit < 0 {
		prereqLimit = 0
	}
	if prereqLimit > len(nodes)-1 {
		prereqLimit = len(nodes) - 1
	}
	out := append([]DeepResearchTaskNode(nil), nodes[:prereqLimit]...)
	deliverable.DependsOn = filterDeepResearchDependencies(deliverable.DependsOn, deepResearchNodeIDSet(out))
	out = append(out, deliverable)
	return deepResearchTrimNodeDependencies(out)
}

func deepResearchTrimNodeDependencies(nodes []DeepResearchTaskNode) []DeepResearchTaskNode {
	ids := deepResearchNodeIDSet(nodes)
	out := append([]DeepResearchTaskNode(nil), nodes...)
	for idx := range out {
		out[idx].DependsOn = filterDeepResearchDependencies(out[idx].DependsOn, ids)
	}
	return out
}

func filterDeepResearchDependencies(deps []string, allowed map[string]struct{}) []string {
	if len(deps) == 0 || len(allowed) == 0 {
		if len(allowed) == 0 {
			return nil
		}
		return append([]string(nil), deps...)
	}
	out := make([]string, 0, len(deps))
	for _, dep := range deps {
		if _, ok := allowed[dep]; ok {
			out = append(out, dep)
		}
	}
	return compactStringSlice(out)
}

func deepResearchNodeIDSet(nodes []DeepResearchTaskNode) map[string]struct{} {
	ids := make(map[string]struct{}, len(nodes))
	for _, node := range nodes {
		if strings.TrimSpace(node.ID) == "" {
			continue
		}
		ids[node.ID] = struct{}{}
	}
	return ids
}

func deepResearchNodeLooksDeliverable(node DeepResearchTaskNode) bool {
	expected := strings.ToLower(strings.TrimSpace(node.ExpectedOutput))
	title := strings.ToLower(strings.TrimSpace(node.Title))
	role := strings.ToLower(strings.TrimSpace(node.WorkerRole))
	return role == "writer" ||
		strings.Contains(expected, "report") ||
		strings.Contains(expected, "markdown") ||
		strings.Contains(title, "report") ||
		strings.Contains(title, "总结") ||
		strings.Contains(title, "报告") ||
		strings.Contains(title, "草稿")
}

func mergeDeepResearchNodeMetadata(base map[string]any, values map[string]any) map[string]any {
	out := cloneWorkflowMap(base)
	if out == nil {
		out = map[string]any{}
	}
	for key, value := range values {
		out[key] = value
	}
	return out
}

func deepResearchTrustedSources(result DeepResearchWorkerResult) []DeepAgentSourceRef {
	toolEvidence := deepResearchHasTrustedToolEvidence(result.ToolCalls)
	out := make([]DeepAgentSourceRef, 0, len(result.Sources))
	for _, source := range result.Sources {
		if deepResearchSourceIsTrusted(source, toolEvidence) {
			out = append(out, source)
		}
	}
	return out
}

func deepResearchSourceIsTrusted(source DeepAgentSourceRef, toolEvidence bool) bool {
	provider := strings.ToLower(strings.TrimSpace(source.Provider))
	sourceKind := strings.ToLower(strings.TrimSpace(source.SourceKind))
	if provider == "model_text" || strings.HasPrefix(sourceKind, "unverified_") {
		return false
	}
	if sourceKind == "tool_verified" {
		return true
	}
	if provider != "" && provider != "model" && provider != "output" {
		return true
	}
	if !toolEvidence {
		return false
	}
	return strings.TrimSpace(source.URL) != "" && strings.TrimSpace(source.Title) != ""
}

func deepResearchHasTrustedToolEvidence(calls []DeepAgentToolCallRef) bool {
	for _, call := range calls {
		status := strings.ToLower(strings.TrimSpace(call.Status))
		if status != "" && status != "succeeded" && status != "result" && status != "called" {
			continue
		}
		if deepResearchToolNameCanVerifySources(call.Name) {
			return true
		}
	}
	return false
}

func deepResearchFindingsWithTrustedCitations(findings []DeepResearchFinding, sources []DeepAgentSourceRef) []DeepResearchFinding {
	trustedURLs := make(map[string]struct{}, len(sources))
	for _, source := range sources {
		if raw := strings.TrimSpace(source.URL); raw != "" {
			trustedURLs[raw] = struct{}{}
		}
	}
	out := append([]DeepResearchFinding(nil), findings...)
	for idx := range out {
		raw := strings.TrimSpace(out[idx].SourceURL)
		if raw == "" {
			continue
		}
		if _, ok := trustedURLs[raw]; !ok {
			out[idx].SourceURL = ""
		}
	}
	return out
}

func compactStringSlice(in []string) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, item := range in {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

func deepResearchRunStateFromAny(raw any) (DeepResearchRunState, bool) {
	switch typed := raw.(type) {
	case DeepResearchRunState:
		return typed, true
	case *DeepResearchRunState:
		if typed == nil {
			return DeepResearchRunState{}, false
		}
		return *typed, true
	case map[string]any:
		data, err := json.Marshal(typed)
		if err != nil {
			return DeepResearchRunState{}, false
		}
		var out DeepResearchRunState
		if err := json.Unmarshal(data, &out); err != nil {
			return DeepResearchRunState{}, false
		}
		return out, true
	default:
		return DeepResearchRunState{}, false
	}
}
