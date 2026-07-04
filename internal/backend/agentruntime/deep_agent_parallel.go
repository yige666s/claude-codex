package agentruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	deepAgentParallelDefaultMaxBranches     = 6
	deepAgentParallelDefaultPrimaryBranches = 4
	deepAgentParallelDefaultMinSuccess      = 1
	deepAgentParallelDefaultBranchTimeout   = 90 * time.Second
	deepAgentParallelMaxBranchTimeout       = 30 * time.Minute
	deepAgentParallelDefaultMaxConcurrency  = 4
	deepAgentParallelDefaultMaxSupplement   = 1
	deepAgentParallelMaxSourcesPerBranch    = 12
	deepAgentParallelMaxSourcesPerGroup     = 40
	deepAgentParallelDefaultMaxToolCalls    = 8
	deepAgentParallelDefaultMaxTokens       = 4000
	deepAgentModelActionMaxSources          = 24
	deepAgentSourceMaxPerHost               = 2
)

type deepAgentParallelCoverageDimension struct {
	ID       string
	Label    string
	Keywords []string
}

type deepAgentParallelCoverageReport struct {
	Score    float64  `json:"score"`
	Required []string `json:"required"`
	Covered  []string `json:"covered"`
	Missing  []string `json:"missing"`
}

type deepAgentParallelConflict struct {
	Field      string   `json:"field"`
	Subject    string   `json:"subject,omitempty"`
	Kind       string   `json:"kind,omitempty"`
	Values     []string `json:"values"`
	Branches   []string `json:"branches,omitempty"`
	Evidence   []string `json:"evidence,omitempty"`
	Confidence float64  `json:"confidence,omitempty"`
	Reason     string   `json:"reason,omitempty"`
}

type deepAgentParallelClaim struct {
	Field      string  `json:"field"`
	Subject    string  `json:"subject,omitempty"`
	Value      string  `json:"value"`
	Branch     string  `json:"branch,omitempty"`
	Evidence   string  `json:"evidence,omitempty"`
	Confidence float64 `json:"confidence,omitempty"`
}

type deepAgentParallelQualityReport struct {
	Coverage     deepAgentParallelCoverageReport `json:"coverage"`
	Claims       []deepAgentParallelClaim        `json:"claims,omitempty"`
	Conflicts    []deepAgentParallelConflict     `json:"conflicts,omitempty"`
	Uncertainty  []string                        `json:"uncertainty_notes,omitempty"`
	Partial      bool                            `json:"partial_synthesis"`
	Supplemental []DeepAgentParallelBranchSpec   `json:"supplemental_branch_specs,omitempty"`
}

var (
	deepAgentParallelClaimValueRE = regexp.MustCompile(`(?i)(?:[$¥€£]\s*)?\d+(?:[.,]\d+)*(?:\s*(?:%|k|m|b|万|亿|million|billion|stars?|星|分|usd|美元|元|月|month|mo|year|yr|年))?`)
	deepAgentParallelTextValueRE  = regexp.MustCompile(`(?i)\b(free|paid|custom|contact sales|enterprise|unavailable|available|waitlist|beta|public|private)\b|免费|付费|自定义|联系销售|企业版|不可用|可用|候补|内测|公测|公开|私有`)
)

func (e *runtimeDeepAgentSubplanExecutor) executeParallelStep(ctx context.Context, route DeepAgentStepRoute, action DeepAgentAction, agentState *DeepAgentState) (DeepAgentStepEvidence, error) {
	route = finalizeDeepAgentActionRoute(route, action)
	specs := deepAgentParallelBranchSpecsFromAction(action, agentState)
	if len(specs) == 0 {
		err := fmt.Errorf("subplan executor requires branch_specs or task")
		return deepAgentDedicatedEvidence(route, action, DeepAgentActionStatusFailed, err.Error(), map[string]any{
			"error_class":       DeepAgentErrorValidation,
			"side_effect_level": deepAgentSideEffectReadonly,
		}), err
	}
	maxBranches := e.deepAgentParallelMaxBranches(action)
	action = e.withDeepAgentParallelBudgetDefaults(action)
	specs = deepAgentParallelPlanBranchSpecs(specs, action, maxBranches)
	if direct := deepAgentParallelBranchResultsFromAction(action); len(direct) > 0 {
		return deepAgentParallelJoinEvidence(ctx, route, action, specs, direct, nil)
	}
	if e == nil || e.runtime == nil {
		err := fmt.Errorf("subplan executor runtime is not configured")
		return deepAgentDedicatedEvidence(route, action, DeepAgentActionStatusFailed, err.Error(), map[string]any{
			"error_class":       DeepAgentErrorConfig,
			"side_effect_level": deepAgentSideEffectReadonly,
		}), err
	}
	parent := e.parent
	if parent == nil {
		parent = NewRuntimeDeepAgentExecutor(e.runtime)
	}
	groupID := firstNonEmptyString(deepAgentActionString(action, "parallel_group_id"), action.Hash, action.StepID, newSortableID())
	emitDeepAgentParallelEvent(ctx, "deep_agent_parallel_group_started", action, route, map[string]any{
		"parallel_group_id": groupID,
		"branch_count":      len(specs),
		"max_branches":      maxBranches,
	})
	results := make([]DeepAgentParallelBranchResult, len(specs))
	concurrency := deepAgentActionInt(action, "max_concurrency", deepAgentParallelDefaultMaxConcurrency)
	if concurrency <= 0 || concurrency > deepAgentParallelDefaultMaxBranches {
		concurrency = deepAgentParallelDefaultMaxConcurrency
	}
	if concurrency > maxBranches {
		concurrency = maxBranches
	}
	if concurrency > len(specs) {
		concurrency = len(specs)
	}
	branchTimeout := e.deepAgentParallelBranchTimeout(action)
	var wg sync.WaitGroup
	sem := make(chan struct{}, concurrency)
	for idx, spec := range specs {
		idx, spec := idx, spec
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			branchCtx, cancel := context.WithTimeout(ctx, branchTimeout)
			defer cancel()
			results[idx] = e.executeParallelBranchWithEvents(branchCtx, parent, route, action, spec, agentState, groupID, idx+1, false)
		}()
	}
	wg.Wait()
	quality := deepAgentParallelQualityReportFor(specs, results)
	emitDeepAgentParallelCoverageEvent(ctx, action, route, groupID, quality)
	var supplemental []DeepAgentParallelBranchSpec
	if supplemental = e.deepAgentParallelSupplementalBranchSpecs(action, specs, results, quality); len(supplemental) > 0 {
		quality.Supplemental = supplemental
		for _, spec := range supplemental {
			branchCtx, cancel := context.WithTimeout(ctx, branchTimeout)
			result := e.executeParallelBranchWithEvents(branchCtx, parent, route, action, spec, agentState, groupID, len(specs)+1, true)
			cancel()
			specs = append(specs, spec)
			results = append(results, result)
		}
		quality = deepAgentParallelQualityReportFor(specs, results)
		quality.Supplemental = supplemental
		emitDeepAgentParallelCoverageEvent(ctx, action, route, groupID, quality)
	}
	extra := map[string]any{"parallel_group_id": groupID}
	if len(supplemental) > 0 {
		extra["supplemental_branch_specs"] = supplemental
		extra["supplemental_branch_count"] = len(supplemental)
	}
	return deepAgentParallelJoinEvidence(ctx, route, action, specs, results, extra)
}

func (e *runtimeDeepAgentSubplanExecutor) executeParallelBranchWithEvents(ctx context.Context, parent *RuntimeDeepAgentExecutor, route DeepAgentStepRoute, action DeepAgentAction, spec DeepAgentParallelBranchSpec, agentState *DeepAgentState, groupID string, branchIndex int, supplemental bool) DeepAgentParallelBranchResult {
	startedAt := time.Now()
	startEvent := "deep_agent_parallel_branch_started"
	successEvent := "deep_agent_parallel_branch_succeeded"
	failedEvent := "deep_agent_parallel_branch_failed"
	if supplemental {
		startEvent = "deep_agent_parallel_supplemental_branch_started"
		successEvent = "deep_agent_parallel_supplemental_branch_succeeded"
		failedEvent = "deep_agent_parallel_supplemental_branch_failed"
	}
	emitDeepAgentParallelEvent(ctx, startEvent, action, route, map[string]any{
		"parallel_group_id":  groupID,
		"branch_id":          spec.ID,
		"branch_title":       spec.Title,
		"branch_kind":        deepAgentParallelSpecKind(spec),
		"coverage_dimension": spec.CoverageDimension,
		"branch_budget":      spec.Budget,
		"branch_index":       branchIndex,
		"objective":          spec.Task,
		"supplemental":       supplemental,
	})
	result, err := e.executeParallelBranch(ctx, parent, route, action, spec, agentState)
	if err != nil {
		result.Status = DeepAgentActionStatusFailed
		result.Error = firstNonEmptyString(result.Error, err.Error())
	}
	eventType := successEvent
	errorText := ""
	if result.Status == DeepAgentActionStatusFailed {
		eventType = failedEvent
		errorText = result.Error
	}
	timedOut := deepAgentParallelErrorTimedOut(errorText)
	if result.Metadata != nil {
		if value, ok := deepAgentMetadataBool(result.Metadata, "timed_out"); ok && value {
			timedOut = true
		}
	}
	emitDeepAgentParallelEvent(ctx, eventType, action, route, map[string]any{
		"parallel_group_id":  groupID,
		"branch_id":          result.ID,
		"branch_title":       result.Title,
		"branch_kind":        deepAgentParallelSpecKind(spec),
		"coverage_dimension": spec.CoverageDimension,
		"branch_budget":      spec.Budget,
		"result_status":      result.Status,
		"source_count":       len(result.Sources),
		"artifact_count":     len(result.Artifacts),
		"tool_call_count":    len(result.ToolCalls),
		"duration_ms":        time.Since(startedAt).Milliseconds(),
		"error":              errorText,
		"timed_out":          timedOut,
		"supplemental":       supplemental,
	})
	return result
}

func (e *runtimeDeepAgentSubplanExecutor) executeParallelBranch(ctx context.Context, parent *RuntimeDeepAgentExecutor, route DeepAgentStepRoute, action DeepAgentAction, spec DeepAgentParallelBranchSpec, agentState *DeepAgentState) (DeepAgentParallelBranchResult, error) {
	if deepAgentParallelSpecUsesInlineConflictReconciliation(spec) && !deepAgentParallelDeepConflictReconciliationEnabled(action) {
		return deepAgentParallelInlineConflictResult(spec), nil
	}
	branchState := cloneDeepAgentStateForParallelBranch(agentState)
	branchTool := deepAgentParallelBranchTool(spec)
	branchAllowedTools := deepAgentParallelBranchAllowedTools(spec, action)
	branchRoute := deepAgentParallelBranchRoute(firstNonEmptyString(action.StepID, route.StepID)+"/"+spec.ID, branchTool, branchAllowedTools)
	sourcePolicy := deepAgentSourcePolicyFromAction(action)
	if action.Args == nil || action.Args["source_policy"] == nil {
		if raw := stateWorkingMemory(agentState)["source_policy"]; raw != nil {
			sourcePolicy = normalizeLoopContractSourcePolicy(deepAgentSourcePolicyFromAny(raw), deepAgentActionString(action, "goal"))
		}
	}
	branchAction := DeepAgentAction{
		StepID: firstNonEmptyString(action.StepID, route.StepID) + "/" + spec.ID,
		Tool:   branchTool,
		Args: mergeDeepAgentActionArgs(cloneWorkflowMap(spec.Metadata), map[string]any{
			"goal":               deepAgentActionString(action, "goal"),
			"prompt":             deepAgentParallelPromptForBranch(action, spec),
			"step_id":            firstNonEmptyString(action.StepID, route.StepID) + "/" + spec.ID,
			"step_title":         spec.Title,
			"done_condition":     strings.Join(spec.SuccessCriteria, "\n"),
			"success_criteria":   spec.SuccessCriteria,
			"allowed_tools":      branchAllowedTools,
			"parallel_branch":    true,
			"branch_kind":        deepAgentParallelSpecKind(spec),
			"coverage_dimension": spec.CoverageDimension,
			"branch_budget":      spec.Budget,
			"source_policy":      sourcePolicy,
		}),
	}
	if spec.Budget.TimeoutMS > 0 {
		branchAction.Args["timeout_ms"] = spec.Budget.TimeoutMS
	}
	if spec.Budget.MaxToolCalls > 0 {
		branchAction.Args["max_tool_calls"] = spec.Budget.MaxToolCalls
	}
	if spec.Budget.MaxSources > 0 {
		branchAction.Args["max_sources"] = spec.Budget.MaxSources
	}
	if spec.Budget.MaxTokens > 0 {
		branchAction.Args["max_tokens"] = spec.Budget.MaxTokens
	}
	branchAction.Args["step_route"] = deepAgentStepRouteMap(branchRoute)
	branchAction.Args["route_version"] = branchRoute.Version
	if userID := firstNonEmptyString(deepAgentActionString(action, "user_id"), deepAgentWorkflowString(stateWorkingMemory(agentState), "user_id")); userID != "" {
		branchAction.Args["user_id"] = userID
	}
	result, err := parent.ExecuteDeepAgentAction(ctx, branchAction, branchState)
	if result.Status == "" {
		result.Status = DeepAgentActionStatusSucceeded
	}
	branchSources, sourcePolicyReport := curateDeepAgentSourceRefsWithPolicy(deepAgentSourceRefsFromAny(result.Metadata["sources"]), deepAgentParallelMaxSourcesForSpec(action, spec), sourcePolicy)
	branchToolCalls := limitDeepAgentToolCallRefs(deepAgentToolCallRefsFromMetadata(result.Metadata), deepAgentParallelMaxToolCallsForSpec(action, spec))
	branchMetadata := cloneWorkflowMap(result.Metadata)
	if branchMetadata == nil {
		branchMetadata = map[string]any{}
	}
	branchMetadata["branch_kind"] = deepAgentParallelSpecKind(spec)
	branchMetadata["coverage_dimension"] = spec.CoverageDimension
	branchMetadata["branch_budget"] = spec.Budget
	branchMetadata["source_policy"] = sourcePolicy
	branchMetadata["source_policy_report"] = sourcePolicyReport
	if deepAgentParallelErrorTimedOut(result.Error) {
		branchMetadata["timed_out"] = true
	}
	if len(branchSources) > 0 {
		branchMetadata["sources"] = branchSources
	} else if branchMetadata != nil {
		delete(branchMetadata, "sources")
	}
	out := DeepAgentParallelBranchResult{
		ID:        spec.ID,
		Title:     spec.Title,
		Status:    result.Status,
		Output:    result.Output,
		Error:     result.Error,
		Sources:   branchSources,
		Artifacts: deepAgentArtifactRefsFromMetadata(result.Metadata),
		ToolCalls: branchToolCalls,
		Metadata:  branchMetadata,
	}
	if supplemental, ok := deepAgentMetadataBool(spec.Metadata, "supplemental"); ok && supplemental {
		if out.Metadata == nil {
			out.Metadata = map[string]any{}
		}
		out.Metadata["supplemental"] = true
	}
	if err != nil {
		out.Status = DeepAgentActionStatusFailed
		out.Error = firstNonEmptyString(out.Error, err.Error())
		if fallback, ok := deepAgentParallelConflictFallbackResult(spec, out.Error); ok {
			return fallback, nil
		}
	}
	return out, err
}

func (e *runtimeDeepAgentSubplanExecutor) deepAgentParallelBranchTimeout(action DeepAgentAction) time.Duration {
	timeout := deepAgentParallelDefaultBranchTimeout
	if e != nil && e.runtime != nil && e.runtime.config.LLMGovernanceProvider != nil {
		cfg := e.runtime.config.LLMGovernanceProvider().normalized()
		if cfg.ParallelBranchTimeout > 0 {
			timeout = cfg.ParallelBranchTimeout
		} else {
			if cfg.SkillTimeout > timeout {
				timeout = cfg.SkillTimeout
			}
			if cfg.ChatTimeout > timeout {
				timeout = cfg.ChatTimeout
			}
		}
	}
	if requested := deepAgentActionDurationMS(action, "branch_timeout_ms"); requested > 0 {
		timeout = requested
	}
	if timeout <= 0 {
		return deepAgentParallelDefaultBranchTimeout
	}
	if timeout > deepAgentParallelMaxBranchTimeout {
		return deepAgentParallelMaxBranchTimeout
	}
	return timeout
}

func deepAgentActionDurationMS(action DeepAgentAction, key string) time.Duration {
	if action.Args == nil {
		return 0
	}
	value, ok := action.Args[key]
	if !ok {
		return 0
	}
	switch typed := value.(type) {
	case int:
		return time.Duration(typed) * time.Millisecond
	case int64:
		return time.Duration(typed) * time.Millisecond
	case float64:
		return time.Duration(typed) * time.Millisecond
	case json.Number:
		if n, err := typed.Int64(); err == nil {
			return time.Duration(n) * time.Millisecond
		}
	case string:
		text := strings.TrimSpace(typed)
		if text == "" {
			return 0
		}
		if n, err := strconv.ParseInt(text, 10, 64); err == nil {
			return time.Duration(n) * time.Millisecond
		}
		if f, err := strconv.ParseFloat(text, 64); err == nil {
			return time.Duration(f) * time.Millisecond
		}
	}
	return 0
}

func (e *runtimeDeepAgentSubplanExecutor) deepAgentParallelMaxBranches(action DeepAgentAction) int {
	if requested := deepAgentActionInt(action, "max_branches", 0); requested > 0 {
		if requested > deepAgentParallelDefaultMaxBranches {
			return deepAgentParallelDefaultMaxBranches
		}
		return requested
	}
	if e != nil && e.runtime != nil && e.runtime.config.LLMGovernanceProvider != nil {
		cfg := e.runtime.config.LLMGovernanceProvider().normalized()
		if cfg.MaxBranchConcurrency > 0 {
			return cfg.MaxBranchConcurrency
		}
	}
	return deepAgentParallelDefaultPrimaryBranches
}

func (e *runtimeDeepAgentSubplanExecutor) withDeepAgentParallelBudgetDefaults(action DeepAgentAction) DeepAgentAction {
	if action.Args == nil {
		action.Args = map[string]any{}
	}
	budget := DeepAgentParallelBranchBudget{
		TimeoutMS:    deepAgentParallelDefaultBranchTimeout.Milliseconds(),
		MaxToolCalls: deepAgentParallelDefaultMaxToolCalls,
		MaxSources:   deepAgentParallelMaxSourcesPerBranch,
		MaxTokens:    deepAgentParallelDefaultMaxTokens,
	}
	if e != nil && e.runtime != nil && e.runtime.config.LLMGovernanceProvider != nil {
		cfg := e.runtime.config.LLMGovernanceProvider().normalized()
		budget.TimeoutMS = cfg.ParallelBranchTimeout.Milliseconds()
		budget.MaxToolCalls = cfg.ParallelMaxToolCalls
		budget.MaxSources = cfg.MaxSourcesPerBranch
		budget.MaxTokens = cfg.ParallelMaxTokens
	}
	if _, ok := action.Args["branch_timeout_ms"]; !ok {
		action.Args["branch_timeout_ms"] = budget.TimeoutMS
	}
	if _, ok := action.Args["branch_max_tool_calls"]; !ok {
		action.Args["branch_max_tool_calls"] = budget.MaxToolCalls
	}
	if _, ok := action.Args["branch_max_sources"]; !ok {
		action.Args["branch_max_sources"] = budget.MaxSources
	}
	if _, ok := action.Args["branch_max_tokens"]; !ok {
		action.Args["branch_max_tokens"] = budget.MaxTokens
	}
	return action
}

func deepAgentParallelPlanBranchSpecs(specs []DeepAgentParallelBranchSpec, action DeepAgentAction, maxBranches int) []DeepAgentParallelBranchSpec {
	specs = normalizeDeepAgentParallelBranchSpecs(specs)
	if maxBranches <= 0 {
		maxBranches = deepAgentParallelDefaultPrimaryBranches
	}
	if maxBranches > deepAgentParallelDefaultMaxBranches {
		maxBranches = deepAgentParallelDefaultMaxBranches
	}
	budget := deepAgentParallelBranchBudgetFromAction(action)
	out := make([]DeepAgentParallelBranchSpec, 0, len(specs))
	seenPrimaryDimension := map[string]int{}
	for _, spec := range specs {
		spec = deepAgentParallelSpecWithBudget(spec, budget)
		if !deepAgentParallelSpecIsSupplemental(spec) {
			dim := firstNonEmptyString(spec.CoverageDimension, "primary")
			if existingIdx, ok := seenPrimaryDimension[dim]; ok {
				out[existingIdx] = mergeDeepAgentParallelBranchSpecs(out[existingIdx], spec)
				continue
			}
			seenPrimaryDimension[dim] = len(out)
		}
		out = append(out, spec)
	}
	if len(out) <= maxBranches {
		return out
	}
	trimmed := make([]DeepAgentParallelBranchSpec, 0, maxBranches)
	for _, spec := range out {
		if len(trimmed) >= maxBranches {
			break
		}
		trimmed = append(trimmed, spec)
	}
	return trimmed
}

func deepAgentParallelSpecWithBudget(spec DeepAgentParallelBranchSpec, fallback DeepAgentParallelBranchBudget) DeepAgentParallelBranchSpec {
	if spec.Budget.TimeoutMS <= 0 {
		spec.Budget.TimeoutMS = fallback.TimeoutMS
	}
	if spec.Budget.MaxToolCalls <= 0 {
		spec.Budget.MaxToolCalls = fallback.MaxToolCalls
	}
	if spec.Budget.MaxSources <= 0 {
		spec.Budget.MaxSources = fallback.MaxSources
	}
	if spec.Budget.MaxTokens <= 0 {
		spec.Budget.MaxTokens = fallback.MaxTokens
	}
	return spec
}

func mergeDeepAgentParallelBranchSpecs(base, extra DeepAgentParallelBranchSpec) DeepAgentParallelBranchSpec {
	if strings.TrimSpace(extra.Task) != "" && !strings.Contains(base.Task, extra.Task) {
		base.Task = strings.TrimSpace(base.Task + "\n\nMerged related branch objective:\n" + extra.Task)
	}
	base.SuccessCriteria = appendUniqueDeepAgentParallelStrings(base.SuccessCriteria, extra.SuccessCriteria...)
	base.AllowedTools = appendUniqueDeepAgentParallelStrings(base.AllowedTools, extra.AllowedTools...)
	if base.Metadata == nil {
		base.Metadata = map[string]any{}
	}
	merged := deepAgentStringSlice(base.Metadata["merged_branch_ids"])
	merged = appendUniqueDeepAgentParallelStrings(merged, extra.ID)
	base.Metadata["merged_branch_ids"] = merged
	base.Metadata["merged_branch_count"] = len(merged) + 1
	return base
}

func appendUniqueDeepAgentParallelStrings(base []string, values ...string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(base)+len(values))
	for _, item := range append(append([]string(nil), base...), values...) {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		key := strings.ToLower(item)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	return out
}

func deepAgentParallelBranchBudgetFromAction(action DeepAgentAction) DeepAgentParallelBranchBudget {
	budget := DeepAgentParallelBranchBudget{
		TimeoutMS:    deepAgentParallelDefaultBranchTimeout.Milliseconds(),
		MaxToolCalls: deepAgentParallelDefaultMaxToolCalls,
		MaxSources:   deepAgentParallelMaxSourcesPerBranch,
		MaxTokens:    deepAgentParallelDefaultMaxTokens,
	}
	if timeout := deepAgentActionDurationMS(action, "branch_timeout_ms"); timeout > 0 {
		budget.TimeoutMS = timeout.Milliseconds()
	}
	if value := deepAgentActionInt(action, "branch_max_tool_calls", 0); value > 0 {
		budget.MaxToolCalls = value
	}
	if value := deepAgentActionInt(action, "branch_max_sources", 0); value > 0 {
		budget.MaxSources = value
	}
	if value := deepAgentActionInt(action, "branch_max_tokens", 0); value > 0 {
		budget.MaxTokens = value
	}
	return budget
}

func deepAgentParallelMaxSourcesForSpec(action DeepAgentAction, spec DeepAgentParallelBranchSpec) int {
	if spec.Budget.MaxSources > 0 {
		return spec.Budget.MaxSources
	}
	if value := deepAgentActionInt(action, "branch_max_sources", 0); value > 0 {
		return value
	}
	return deepAgentParallelMaxSourcesPerBranch
}

func deepAgentParallelMaxToolCallsForSpec(action DeepAgentAction, spec DeepAgentParallelBranchSpec) int {
	if spec.Budget.MaxToolCalls > 0 {
		return spec.Budget.MaxToolCalls
	}
	if value := deepAgentActionInt(action, "branch_max_tool_calls", 0); value > 0 {
		return value
	}
	return deepAgentParallelDefaultMaxToolCalls
}

func limitDeepAgentToolCallRefs(refs []DeepAgentToolCallRef, max int) []DeepAgentToolCallRef {
	if max <= 0 || len(refs) <= max {
		return refs
	}
	return append([]DeepAgentToolCallRef(nil), refs[:max]...)
}

func deepAgentParallelErrorTimedOut(text string) bool {
	text = strings.ToLower(strings.TrimSpace(text))
	return strings.Contains(text, "context deadline exceeded") ||
		strings.Contains(text, "deadline") ||
		strings.Contains(text, "timeout") ||
		strings.Contains(text, "timed out")
}

func deepAgentParallelJoinEvidence(ctx context.Context, route DeepAgentStepRoute, action DeepAgentAction, specs []DeepAgentParallelBranchSpec, results []DeepAgentParallelBranchResult, extra map[string]any) (DeepAgentStepEvidence, error) {
	succeeded := 0
	failed := 0
	primaryCount := 0
	primarySucceeded := 0
	primaryFailed := 0
	var sources []DeepAgentSourceRef
	var artifacts []DeepAgentArtifactRef
	var toolCalls []DeepAgentToolCallRef
	var parts []string
	for _, result := range results {
		title := firstNonEmptyString(result.Title, result.ID)
		supplemental := deepAgentParallelResultIsSupplemental(result)
		if !supplemental {
			primaryCount++
		}
		if result.Status == DeepAgentActionStatusFailed {
			failed++
			if !supplemental {
				primaryFailed++
			}
			parts = append(parts, fmt.Sprintf("## %s\nStatus: failed\nError: %s", title, firstNonEmptyString(result.Error, "unknown error")))
			continue
		}
		succeeded++
		if !supplemental {
			primarySucceeded++
		}
		parts = append(parts, fmt.Sprintf("## %s\nStatus: succeeded\n%s", title, strings.TrimSpace(result.Output)))
		sources = append(sources, result.Sources...)
		artifacts = append(artifacts, result.Artifacts...)
		toolCalls = append(toolCalls, result.ToolCalls...)
	}
	minSuccess := deepAgentActionInt(action, "min_successful_branches", deepAgentParallelDefaultMinSuccess)
	if minSuccess <= 0 {
		minSuccess = deepAgentParallelDefaultMinSuccess
	}
	if primaryCount == 0 {
		primaryCount = len(results)
		primarySucceeded = succeeded
		primaryFailed = failed
	}
	if minSuccess > primaryCount {
		minSuccess = primaryCount
	}
	quality := deepAgentParallelQualityReportFor(specs, results)
	contributions := deepAgentParallelBranchContributions(specs, results, quality)
	for idx := range results {
		if idx < len(contributions) {
			results[idx].Contribution = contributions[idx]
		}
	}
	output := fmt.Sprintf("Parallel group completed: %d/%d primary branch(es) succeeded; %d/%d total branch(es) succeeded.\n\n%s", primarySucceeded, primaryCount, succeeded, len(results), strings.Join(parts, "\n\n"))
	if coverageText := deepAgentParallelQualitySummary(quality); coverageText != "" {
		output += "\n\n" + coverageText
	}
	minCoverage := deepAgentParallelMinCoverage(action)
	toolResultValid := primarySucceeded >= minSuccess && quality.Coverage.Score >= minCoverage
	if len(results) == 0 {
		toolResultValid = false
	}
	groupSourcePolicy := deepAgentSourcePolicyFromAction(action)
	if groupSourcePolicy.MaxSourcesPerBranch < deepAgentParallelMaxSourcesPerGroup {
		groupSourcePolicy.MaxSourcesPerBranch = deepAgentParallelMaxSourcesPerGroup
	}
	curatedSources, sourcePolicyReport := curateDeepAgentSourceRefsWithPolicy(sources, deepAgentParallelMaxSourcesPerGroup, groupSourcePolicy)
	metadata := map[string]any{
		"parallel":                  true,
		"branch_count":              len(results),
		"succeeded_branch_count":    succeeded,
		"failed_branch_count":       failed,
		"primary_branch_count":      primaryCount,
		"primary_succeeded_count":   primarySucceeded,
		"primary_failed_count":      primaryFailed,
		"min_successful_branches":   minSuccess,
		"branch_specs":              specs,
		"branch_results":            results,
		"branch_contributions":      contributions,
		"sources":                   curatedSources,
		"source_policy":             groupSourcePolicy,
		"source_policy_report":      sourcePolicyReport,
		"artifact_refs":             artifacts,
		"tool_calls":                toolCalls,
		"side_effect_level":         deepAgentSideEffectReadonly,
		"dedicated_executor":        deepAgentRouteExecutorSubPlan,
		"tool_result_valid":         toolResultValid,
		"parallel_join_completed":   true,
		"parallel_context_isolated": true,
		"coverage_score":            quality.Coverage.Score,
		"coverage_required":         quality.Coverage.Required,
		"coverage_covered":          quality.Coverage.Covered,
		"missing_coverage":          quality.Coverage.Missing,
		"coverage_complete":         len(quality.Coverage.Missing) == 0,
		"min_coverage_score":        minCoverage,
		"parallel_claims":           quality.Claims,
		"parallel_conflicts":        quality.Conflicts,
		"conflict_count":            len(quality.Conflicts),
		"uncertainty_notes":         quality.Uncertainty,
		"partial_synthesis":         quality.Partial || primaryFailed > 0 || len(quality.Coverage.Missing) > 0,
	}
	for key, value := range extra {
		metadata[key] = value
	}
	status := DeepAgentActionStatusSucceeded
	var err error
	if primarySucceeded < minSuccess {
		status = DeepAgentActionStatusFailed
		err = fmt.Errorf("parallel group only completed %d/%d required primary branch(es)", primarySucceeded, minSuccess)
		metadata["error_class"] = DeepAgentErrorTransient
	} else if quality.Coverage.Score < minCoverage {
		status = DeepAgentActionStatusFailed
		err = fmt.Errorf("parallel group coverage score %.2f below required %.2f", quality.Coverage.Score, minCoverage)
		metadata["error_class"] = DeepAgentErrorTransient
	}
	emitDeepAgentParallelEvent(ctx, "deep_agent_parallel_group_joined", action, route, map[string]any{
		"branch_count":            len(results),
		"succeeded_branch_count":  succeeded,
		"failed_branch_count":     failed,
		"primary_branch_count":    primaryCount,
		"primary_succeeded_count": primarySucceeded,
		"primary_failed_count":    primaryFailed,
		"min_successful_branches": minSuccess,
		"result_status":           status,
		"coverage_score":          quality.Coverage.Score,
		"coverage_required":       quality.Coverage.Required,
		"coverage_covered":        quality.Coverage.Covered,
		"missing_coverage":        quality.Coverage.Missing,
		"parallel_claims":         quality.Claims,
		"parallel_conflicts":      quality.Conflicts,
		"branch_contributions":    contributions,
		"conflict_count":          len(quality.Conflicts),
		"uncertainty_notes":       quality.Uncertainty,
		"partial_synthesis":       metadata["partial_synthesis"],
	})
	evidence := deepAgentDedicatedEvidence(route, action, status, output, metadata)
	evidence.Sources = curatedSources
	evidence.Artifacts = artifacts
	evidence.ToolCalls = toolCalls
	evidence.SideEffectLevel = deepAgentSideEffectReadonly
	return evidence, err
}

func deepAgentParallelMinCoverage(action DeepAgentAction) float64 {
	if action.Args == nil {
		return 0
	}
	for _, key := range []string{"min_coverage_score", "required_coverage_score"} {
		if value, ok := action.Args[key]; ok {
			return deepAgentAnyFloat(value, 0)
		}
	}
	return 0
}

func deepAgentAnyFloat(value any, fallback float64) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case float32:
		return float64(typed)
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case json.Number:
		if n, err := typed.Float64(); err == nil {
			return n
		}
	case string:
		var n float64
		if _, err := fmt.Sscanf(strings.TrimSpace(typed), "%f", &n); err == nil {
			return n
		}
	}
	return fallback
}

func (e *runtimeDeepAgentSubplanExecutor) deepAgentParallelSupplementalBranchSpecs(action DeepAgentAction, specs []DeepAgentParallelBranchSpec, results []DeepAgentParallelBranchResult, quality deepAgentParallelQualityReport) []DeepAgentParallelBranchSpec {
	if action.Args != nil {
		if disabled, ok := deepAgentMetadataBool(action.Args, "disable_supplemental_branches"); ok && disabled {
			return nil
		}
		if disabled, ok := deepAgentMetadataBool(action.Args, "budget_exceeded"); ok && disabled {
			return nil
		}
	}
	if len(quality.Coverage.Missing) == 0 && len(quality.Conflicts) == 0 {
		return nil
	}
	primarySucceeded := 0
	for _, result := range results {
		if deepAgentParallelResultIsSupplemental(result) {
			continue
		}
		if result.Status != DeepAgentActionStatusFailed {
			primarySucceeded++
		}
	}
	if primarySucceeded == 0 {
		return nil
	}
	maxBranches := deepAgentActionInt(action, "max_branches", deepAgentParallelDefaultMaxBranches)
	if maxBranches <= 0 || maxBranches > deepAgentParallelDefaultMaxBranches {
		maxBranches = deepAgentParallelDefaultMaxBranches
	}
	remaining := maxBranches - len(specs)
	if remaining <= 0 {
		return nil
	}
	limit := deepAgentActionInt(action, "max_supplemental_branches", deepAgentParallelDefaultMaxSupplement)
	if limit <= 0 {
		return nil
	}
	if limit > remaining {
		limit = remaining
	}
	parentTask := firstNonEmptyString(deepAgentActionString(action, "task"), deepAgentActionString(action, "prompt"), deepAgentActionString(action, "query"), deepAgentActionString(action, "goal"))
	out := make([]DeepAgentParallelBranchSpec, 0, limit)
	remainingLimit := limit
	if len(quality.Conflicts) > 0 && remainingLimit > 0 {
		conflictLines := deepAgentParallelConflictSummaryLines(quality.Conflicts, 8, 5)
		compactTask := truncateDeepAgentDiagnosticText(parentTask, 900)
		out = append(out, DeepAgentParallelBranchSpec{
			ID:           "supplement-conflict-reconciliation",
			Title:        "Conflict reconciliation",
			Task:         fmt.Sprintf("Resolve conflicting factual claims for this multi-agent research task.\n\nUser goal:\n%s\n\nConflicts:\n- %s", compactTask, strings.Join(conflictLines, "\n- ")),
			Tool:         DeepAgentToolModeModel,
			AllowedTools: []string{"WebSearch", "WebFetch"},
			SuccessCriteria: []string{
				"Re-check conflicting claims against primary or high-quality sources.",
				"Return a concise adjudication with source URLs or titles.",
				"State remaining uncertainty when evidence does not resolve the conflict.",
			},
			Metadata: map[string]any{
				"supplemental":       true,
				"conflict_reconcile": true,
				"conflict_lines":     conflictLines,
				"supplemental_index": len(out) + 1,
			},
		})
		remainingLimit--
	}
	coverageLimit := remainingLimit
	missing := append([]string(nil), quality.Coverage.Missing...)
	if len(missing) > coverageLimit {
		missing = missing[:coverageLimit]
	}
	for idx, item := range missing {
		id := "supplement-" + deepAgentParallelDimensionSlug(item)
		out = append(out, DeepAgentParallelBranchSpec{
			ID:           id,
			Title:        "Coverage supplement: " + item,
			Task:         fmt.Sprintf("%s\n\nFill the missing research coverage for: %s. Use independent source evidence, label uncertainty, and cite source URLs or titles.", truncateDeepAgentDiagnosticText(parentTask, 1600), item),
			Tool:         DeepAgentToolModeModel,
			AllowedTools: []string{"WebSearch", "WebFetch"},
			SuccessCriteria: []string{
				"Return concise facts for the missing coverage dimension.",
				"Include source URLs or source titles.",
				"Call out conflicts or uncertainty instead of choosing silently.",
			},
			Metadata: map[string]any{
				"supplemental":       true,
				"missing_coverage":   item,
				"supplemental_index": idx + 1,
			},
		})
		remainingLimit--
	}
	return out
}

func deepAgentParallelConflictSummaryLines(conflicts []deepAgentParallelConflict, maxConflicts, maxValues int) []string {
	if maxConflicts <= 0 {
		maxConflicts = 1
	}
	if maxValues <= 0 {
		maxValues = 3
	}
	capacity := len(conflicts)
	if capacity > maxConflicts {
		capacity = maxConflicts
	}
	lines := make([]string, 0, capacity)
	for idx, conflict := range conflicts {
		if idx >= maxConflicts {
			break
		}
		values := append([]string(nil), conflict.Values...)
		if len(values) > maxValues {
			values = append(values[:maxValues], fmt.Sprintf("+%d more", len(conflict.Values)-maxValues))
		}
		for valueIdx, value := range values {
			values[valueIdx] = truncateDeepAgentDiagnosticText(value, 120)
		}
		subject := firstNonEmptyString(conflict.Subject, "default")
		lines = append(lines, fmt.Sprintf("%s/%s: %s", conflict.Field, subject, strings.Join(values, " vs ")))
	}
	return lines
}

func deepAgentParallelQualityReportFor(specs []DeepAgentParallelBranchSpec, results []DeepAgentParallelBranchResult) deepAgentParallelQualityReport {
	claims := deepAgentParallelClaims(results)
	report := deepAgentParallelQualityReport{
		Coverage:    deepAgentParallelCoverageFor(specs, results),
		Claims:      claims,
		Conflicts:   deepAgentParallelConflicts(claims),
		Uncertainty: deepAgentParallelUncertaintyNotes(results),
	}
	report.Partial = len(report.Coverage.Missing) > 0 || len(report.Conflicts) > 0
	return report
}

func deepAgentParallelCoverageFor(specs []DeepAgentParallelBranchSpec, results []DeepAgentParallelBranchResult) deepAgentParallelCoverageReport {
	dimensions := deepAgentParallelCoverageDimensions()
	required := make([]string, 0, len(dimensions))
	coveredSet := map[string]struct{}{}
	for _, dim := range dimensions {
		required = append(required, dim.ID)
	}
	specByID := map[string]DeepAgentParallelBranchSpec{}
	for _, spec := range specs {
		specByID[spec.ID] = spec
	}
	for _, result := range results {
		if result.Status == DeepAgentActionStatusFailed {
			continue
		}
		if strings.TrimSpace(result.Output) == "" && len(result.Sources) == 0 && len(result.ToolCalls) == 0 {
			continue
		}
		spec := specByID[result.ID]
		corpus := strings.ToLower(strings.Join([]string{result.ID, result.Title, result.Output, spec.Title, spec.Task}, "\n"))
		for _, source := range result.Sources {
			corpus += "\n" + strings.ToLower(strings.Join([]string{source.Title, source.Snippet, source.URL, source.Provider}, "\n"))
		}
		for _, dim := range dimensions {
			if deepAgentContainsAny(corpus, dim.Keywords...) {
				coveredSet[dim.ID] = struct{}{}
			}
		}
	}
	covered := make([]string, 0, len(coveredSet))
	missing := make([]string, 0)
	for _, dim := range dimensions {
		if _, ok := coveredSet[dim.ID]; ok {
			covered = append(covered, dim.ID)
		} else {
			missing = append(missing, dim.ID)
		}
	}
	sort.Strings(covered)
	score := 0.0
	if len(required) > 0 {
		score = float64(len(covered)) / float64(len(required))
	}
	return deepAgentParallelCoverageReport{Score: score, Required: required, Covered: covered, Missing: missing}
}

func deepAgentParallelCoverageDimensions() []deepAgentParallelCoverageDimension {
	return []deepAgentParallelCoverageDimension{
		{ID: "company_team", Label: "company/team", Keywords: []string{"company", "team", "founder", "about", "公司", "团队", "创始", "开发者"}},
		{ID: "product_features", Label: "product/features", Keywords: []string{"feature", "capability", "function", "product", "功能", "产品", "能力", "特性"}},
		{ID: "pricing_availability", Label: "pricing/availability", Keywords: []string{"pricing", "price", "availability", "available", "定价", "价格", "可用", "上线"}},
		{ID: "user_reviews", Label: "user reviews", Keywords: []string{"review", "feedback", "user", "rating", "评价", "用户", "反馈", "口碑"}},
		{ID: "competitors", Label: "competitors", Keywords: []string{"competitor", "alternative", "versus", "compare", "竞品", "竞争", "替代", "对比"}},
		{ID: "risks_uncertainty", Label: "risks/uncertainty", Keywords: []string{"risk", "uncertain", "caveat", "limitation", "contradiction", "conflict", "风险", "不确定", "限制", "注意", "矛盾", "冲突"}},
	}
}

func deepAgentParallelClaimFields() []struct {
	ID       string
	Keywords []string
} {
	return []struct {
		ID       string
		Keywords []string
	}{
		{ID: "pricing", Keywords: []string{"pricing", "price", "cost", "plan", "subscription", "tier", "定价", "价格", "费用", "订阅", "套餐"}},
		{ID: "revenue", Keywords: []string{"revenue", "arr", "mrr", "income", "营收", "收入"}},
		{ID: "users", Keywords: []string{"users", "downloads", "customers", "active users", "installs", "用户", "下载", "客户", "安装"}},
		{ID: "rating", Keywords: []string{"rating", "score", "stars", "评分", "星级", "星"}},
		{ID: "funding", Keywords: []string{"funding", "raised", "investor", "valuation", "融资", "投资", "估值"}},
		{ID: "availability", Keywords: []string{"available", "availability", "launched", "beta", "waitlist", "unavailable", "可用", "上线", "内测", "候补", "不可用"}},
	}
}

func deepAgentParallelClaims(results []DeepAgentParallelBranchResult) []deepAgentParallelClaim {
	var claims []deepAgentParallelClaim
	for _, result := range results {
		if result.Status == DeepAgentActionStatusFailed {
			continue
		}
		branch := firstNonEmptyString(result.ID, result.Title)
		for _, sentence := range deepAgentParallelSentences(result.Output) {
			lower := strings.ToLower(sentence)
			for _, field := range deepAgentParallelClaimFields() {
				if !deepAgentContainsAny(lower, field.Keywords...) {
					continue
				}
				subject := deepAgentParallelClaimSubject(field.ID, lower)
				values := deepAgentParallelClaimValueRE.FindAllString(sentence, 4)
				if field.ID == "pricing" || field.ID == "availability" {
					values = append(values, deepAgentParallelTextValueRE.FindAllString(sentence, 4)...)
				}
				for _, value := range values {
					value = deepAgentParallelNormalizeClaimValueForField(field.ID, value, sentence)
					if value == "" {
						continue
					}
					claims = append(claims, deepAgentParallelClaim{
						Field:      field.ID,
						Subject:    subject,
						Value:      value,
						Branch:     branch,
						Evidence:   truncateDeepAgentDiagnosticText(strings.TrimSpace(sentence), 220),
						Confidence: deepAgentParallelClaimConfidence(value),
					})
				}
			}
		}
	}
	sort.Slice(claims, func(i, j int) bool {
		if claims[i].Field != claims[j].Field {
			return claims[i].Field < claims[j].Field
		}
		if claims[i].Subject != claims[j].Subject {
			return claims[i].Subject < claims[j].Subject
		}
		if claims[i].Value != claims[j].Value {
			return claims[i].Value < claims[j].Value
		}
		return claims[i].Branch < claims[j].Branch
	})
	return claims
}

func deepAgentParallelConflicts(claims []deepAgentParallelClaim) []deepAgentParallelConflict {
	type claimBucket struct {
		field    string
		subject  string
		values   map[string]map[string]struct{}
		evidence map[string]string
		maxConf  float64
	}
	buckets := map[string]*claimBucket{}
	for _, claim := range claims {
		key := claim.Field + "\x00" + claim.Subject
		bucket := buckets[key]
		if bucket == nil {
			bucket = &claimBucket{
				field:    claim.Field,
				subject:  claim.Subject,
				values:   map[string]map[string]struct{}{},
				evidence: map[string]string{},
			}
			buckets[key] = bucket
		}
		if bucket.values[claim.Value] == nil {
			bucket.values[claim.Value] = map[string]struct{}{}
		}
		bucket.values[claim.Value][claim.Branch] = struct{}{}
		if bucket.evidence[claim.Value] == "" {
			bucket.evidence[claim.Value] = claim.Evidence
		}
		if claim.Confidence > bucket.maxConf {
			bucket.maxConf = claim.Confidence
		}
	}
	conflicts := make([]deepAgentParallelConflict, 0)
	for _, bucket := range buckets {
		if len(bucket.values) < 2 {
			continue
		}
		valueList := make([]string, 0, len(bucket.values))
		branchSet := map[string]struct{}{}
		evidenceList := make([]string, 0, len(bucket.values))
		for value, branches := range bucket.values {
			valueList = append(valueList, value)
			if bucket.evidence[value] != "" {
				evidenceList = append(evidenceList, bucket.evidence[value])
			}
			for branch := range branches {
				branchSet[branch] = struct{}{}
			}
		}
		if len(branchSet) < 2 {
			continue
		}
		branchList := make([]string, 0, len(branchSet))
		for branch := range branchSet {
			branchList = append(branchList, branch)
		}
		sort.Strings(valueList)
		sort.Strings(branchList)
		conflicts = append(conflicts, deepAgentParallelConflict{
			Field:      bucket.field,
			Subject:    bucket.subject,
			Kind:       "claim_value_mismatch",
			Values:     valueList,
			Branches:   branchList,
			Evidence:   evidenceList,
			Confidence: bucket.maxConf,
			Reason:     "Different branches asserted incompatible values for the same fact subject.",
		})
	}
	sort.Slice(conflicts, func(i, j int) bool {
		if conflicts[i].Field != conflicts[j].Field {
			return conflicts[i].Field < conflicts[j].Field
		}
		return conflicts[i].Subject < conflicts[j].Subject
	})
	return conflicts
}

func deepAgentParallelUncertaintyNotes(results []DeepAgentParallelBranchResult) []string {
	seen := map[string]struct{}{}
	var notes []string
	for _, result := range results {
		if result.Status == DeepAgentActionStatusFailed {
			notes = append(notes, fmt.Sprintf("%s failed: %s", firstNonEmptyString(result.Title, result.ID), firstNonEmptyString(result.Error, "unknown error")))
			continue
		}
		for _, sentence := range deepAgentParallelSentences(result.Output) {
			lower := strings.ToLower(sentence)
			if !deepAgentContainsAny(lower, "uncertain", "unknown", "conflict", "contradict", "not verified", "不确定", "未知", "冲突", "矛盾", "未验证") {
				continue
			}
			note := truncateDeepAgentDiagnosticText(strings.TrimSpace(sentence), 180)
			if note == "" {
				continue
			}
			if _, ok := seen[note]; ok {
				continue
			}
			seen[note] = struct{}{}
			notes = append(notes, note)
		}
	}
	if len(notes) > 6 {
		notes = notes[:6]
	}
	return notes
}

func deepAgentParallelQualitySummary(report deepAgentParallelQualityReport) string {
	var b strings.Builder
	b.WriteString("## Parallel verification\n")
	b.WriteString(fmt.Sprintf("Coverage score: %.2f\n", report.Coverage.Score))
	if len(report.Coverage.Covered) > 0 {
		b.WriteString("Covered: ")
		b.WriteString(strings.Join(report.Coverage.Covered, ", "))
		b.WriteString("\n")
	}
	if len(report.Coverage.Missing) > 0 {
		b.WriteString("Missing coverage: ")
		b.WriteString(strings.Join(report.Coverage.Missing, ", "))
		b.WriteString("\n")
	}
	if len(report.Conflicts) > 0 {
		b.WriteString("Conflicts detected:\n")
		for _, conflict := range report.Conflicts {
			b.WriteString("- ")
			b.WriteString(conflict.Field)
			if conflict.Subject != "" {
				b.WriteString("/")
				b.WriteString(conflict.Subject)
			}
			b.WriteString(": ")
			b.WriteString(strings.Join(conflict.Values, " vs "))
			if len(conflict.Branches) > 0 {
				b.WriteString(" (branches: ")
				b.WriteString(strings.Join(conflict.Branches, ", "))
				b.WriteString(")")
			}
			if conflict.Confidence > 0 {
				b.WriteString(fmt.Sprintf(" confidence=%.2f", conflict.Confidence))
			}
			b.WriteString("\n")
		}
	}
	if len(report.Uncertainty) > 0 {
		b.WriteString("Uncertainty notes:\n")
		for _, note := range report.Uncertainty {
			b.WriteString("- ")
			b.WriteString(note)
			b.WriteString("\n")
		}
	}
	return strings.TrimSpace(b.String())
}

func deepAgentParallelBranchContributions(specs []DeepAgentParallelBranchSpec, results []DeepAgentParallelBranchResult, quality deepAgentParallelQualityReport) []DeepAgentBranchContribution {
	specByID := map[string]DeepAgentParallelBranchSpec{}
	for _, spec := range specs {
		specByID[spec.ID] = spec
	}
	out := make([]DeepAgentBranchContribution, 0, len(results))
	for _, result := range results {
		spec := specByID[result.ID]
		contribution := DeepAgentBranchContribution{
			BranchID:          result.ID,
			Title:             firstNonEmptyString(result.Title, spec.Title, result.ID),
			Kind:              firstNonEmptyString(deepAgentWorkflowString(result.Metadata, "branch_kind"), deepAgentParallelSpecKind(spec)),
			CoverageDimension: firstNonEmptyString(deepAgentWorkflowString(result.Metadata, "coverage_dimension"), spec.CoverageDimension),
			Status:            result.Status,
			Findings:          deepAgentParallelContributionFindings(result.Output),
			Sources:           result.Sources,
			Confidence:        deepAgentParallelContributionConfidence(result),
			Conflicts:         deepAgentParallelContributionConflicts(result, quality.Conflicts),
		}
		if contribution.CoverageDimension != "" && containsDeepAgentParallelString(quality.Coverage.Missing, contribution.CoverageDimension) {
			contribution.MissingCoverage = []string{contribution.CoverageDimension}
		}
		contribution.RecommendedNextAction = deepAgentParallelContributionNextAction(contribution, result)
		out = append(out, contribution)
	}
	return out
}

func deepAgentParallelContributionFindings(output string) []string {
	sentences := deepAgentParallelSentences(output)
	if len(sentences) > 4 {
		sentences = sentences[:4]
	}
	out := make([]string, 0, len(sentences))
	for _, sentence := range sentences {
		sentence = truncateDeepAgentDiagnosticText(strings.TrimSpace(sentence), 180)
		if sentence != "" {
			out = append(out, sentence)
		}
	}
	return out
}

func deepAgentParallelContributionConfidence(result DeepAgentParallelBranchResult) string {
	if result.Status == DeepAgentActionStatusFailed {
		return "low"
	}
	if len(result.Sources) >= 2 {
		return "high"
	}
	if len(result.Sources) == 1 || strings.TrimSpace(result.Output) != "" {
		return "medium"
	}
	return "low"
}

func deepAgentParallelContributionConflicts(result DeepAgentParallelBranchResult, conflicts []deepAgentParallelConflict) []string {
	if len(conflicts) == 0 {
		return nil
	}
	branchID := firstNonEmptyString(result.ID, result.Title)
	var out []string
	for _, conflict := range conflicts {
		if !containsDeepAgentParallelString(conflict.Branches, branchID) {
			continue
		}
		out = append(out, fmt.Sprintf("%s/%s: %s", conflict.Field, firstNonEmptyString(conflict.Subject, "default"), strings.Join(conflict.Values, " vs ")))
	}
	return out
}

func deepAgentParallelContributionNextAction(contribution DeepAgentBranchContribution, result DeepAgentParallelBranchResult) string {
	if result.Status == DeepAgentActionStatusFailed {
		if deepAgentParallelErrorTimedOut(result.Error) {
			return "Retry this branch with a larger timeout or narrower source budget."
		}
		return "Retry or replace this branch before trusting the joined synthesis."
	}
	if len(contribution.MissingCoverage) > 0 {
		return "Launch one supplemental branch for the missing coverage dimension."
	}
	if len(contribution.Conflicts) > 0 {
		return "Reconcile conflicting claims against primary or high-quality sources."
	}
	if contribution.Confidence == "low" {
		return "Collect at least one stronger source before final synthesis."
	}
	return "Use this contribution in the final synthesis."
}

func containsDeepAgentParallelString(values []string, target string) bool {
	target = strings.TrimSpace(target)
	if target == "" {
		return false
	}
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func emitDeepAgentParallelCoverageEvent(ctx context.Context, action DeepAgentAction, route DeepAgentStepRoute, groupID string, quality deepAgentParallelQualityReport) {
	emitDeepAgentParallelEvent(ctx, "deep_agent_parallel_coverage_checked", action, route, map[string]any{
		"parallel_group_id":  groupID,
		"coverage_score":     quality.Coverage.Score,
		"coverage_required":  quality.Coverage.Required,
		"coverage_covered":   quality.Coverage.Covered,
		"missing_coverage":   quality.Coverage.Missing,
		"parallel_claims":    quality.Claims,
		"parallel_conflicts": quality.Conflicts,
		"conflict_count":     len(quality.Conflicts),
		"uncertainty_notes":  quality.Uncertainty,
		"partial_synthesis":  quality.Partial,
	})
}

func deepAgentParallelSentences(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	parts := strings.FieldsFunc(text, func(r rune) bool {
		switch r {
		case '\n', '.', '。', '!', '！', '?', '？', ';', '；':
			return true
		default:
			return false
		}
	})
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func deepAgentParallelNormalizeClaimValue(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.Join(strings.Fields(value), " ")
	return strings.Trim(value, ",，。.;；")
}

func deepAgentParallelNormalizeClaimValueForField(field, value, sentence string) string {
	value = deepAgentParallelNormalizeClaimValue(value)
	if value == "" {
		return ""
	}
	lowerSentence := strings.ToLower(sentence)
	switch value {
	case "免费", "free":
		return "free"
	case "付费", "paid":
		return "paid"
	case "自定义", "custom":
		return "custom"
	case "联系销售", "contact sales":
		return "contact_sales"
	case "企业版", "enterprise":
		return "enterprise"
	case "不可用", "unavailable":
		return "unavailable"
	case "可用", "available":
		return "available"
	case "候补", "waitlist":
		return "waitlist"
	case "内测", "beta":
		return "beta"
	case "公测", "public", "公开":
		return "public"
	case "private", "私有":
		return "private"
	}
	if field == "pricing" {
		value = strings.ReplaceAll(value, " per ", "/")
		value = strings.ReplaceAll(value, "每", "/")
		if deepAgentContainsAny(lowerSentence, "month", "/mo", "monthly", "per month", "月") && !strings.Contains(value, "/month") && !strings.Contains(value, "/mo") && !strings.Contains(value, "月") {
			value += "/month"
		}
		if deepAgentContainsAny(lowerSentence, "year", "annual", "annually", "per year", "年") && !strings.Contains(value, "/year") && !strings.Contains(value, "年") {
			value += "/year"
		}
	}
	return value
}

func deepAgentParallelClaimSubject(field, sentence string) string {
	switch field {
	case "pricing":
		if deepAgentContainsAny(sentence, "enterprise", "企业") {
			return "enterprise_pricing"
		}
		if deepAgentContainsAny(sentence, "team", "pro", "团队", "专业") {
			return "team_pricing"
		}
		if deepAgentContainsAny(sentence, "free", "免费") {
			return "free_plan"
		}
		return "base_pricing"
	case "revenue":
		if deepAgentContainsAny(sentence, "arr") {
			return "arr"
		}
		if deepAgentContainsAny(sentence, "mrr") {
			return "mrr"
		}
		return "revenue"
	case "users":
		if deepAgentContainsAny(sentence, "download", "下载", "installs", "安装") {
			return "downloads"
		}
		if deepAgentContainsAny(sentence, "customer", "客户") {
			return "customers"
		}
		return "users"
	case "rating":
		return "rating"
	case "funding":
		if deepAgentContainsAny(sentence, "valuation", "估值") {
			return "valuation"
		}
		return "funding"
	case "availability":
		return "availability"
	default:
		return field
	}
}

func deepAgentParallelClaimConfidence(value string) float64 {
	if deepAgentParallelClaimValueRE.MatchString(value) {
		return 0.82
	}
	return 0.68
}

func deepAgentParallelDimensionSlug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	replacer := strings.NewReplacer("_", "-", " ", "-", "/", "-", "：", "-", ":", "-")
	value = replacer.Replace(value)
	value = strings.Trim(value, "-")
	if value == "" {
		return "coverage"
	}
	return value
}

func deepAgentParallelBranchSpecsFromAction(action DeepAgentAction, agentState *DeepAgentState) []DeepAgentParallelBranchSpec {
	for _, key := range []string{"branch_specs", "branches"} {
		if specs := decodeDeepAgentParallelBranchSpecs(action.Args[key]); len(specs) > 0 {
			return normalizeDeepAgentParallelBranchSpecs(specs)
		}
	}
	task := firstNonEmptyString(deepAgentActionString(action, "task"), deepAgentActionString(action, "prompt"), deepAgentActionString(action, "query"), deepAgentActionString(action, "step_intent"), deepAgentActionString(action, "step_title"), stateGoal(agentState))
	if strings.TrimSpace(task) == "" {
		return nil
	}
	return normalizeDeepAgentParallelBranchSpecs(singleDeepAgentParallelBranchSpec("primary", "Primary objective", task))
}

func decodeDeepAgentParallelBranchSpecs(raw any) []DeepAgentParallelBranchSpec {
	if raw == nil {
		return nil
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var specs []DeepAgentParallelBranchSpec
	if err := json.Unmarshal(data, &specs); err == nil && len(specs) > 0 {
		return specs
	}
	var tasks []string
	if err := json.Unmarshal(data, &tasks); err != nil {
		return nil
	}
	out := make([]DeepAgentParallelBranchSpec, 0, len(tasks))
	for idx, task := range tasks {
		out = append(out, DeepAgentParallelBranchSpec{
			ID:    fmt.Sprintf("branch-%d", idx+1),
			Title: fmt.Sprintf("Branch %d", idx+1),
			Task:  task,
		})
	}
	return out
}

func deepAgentParallelBranchResultsFromAction(action DeepAgentAction) []DeepAgentParallelBranchResult {
	raw := action.Args["branch_results"]
	if raw == nil {
		return nil
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var results []DeepAgentParallelBranchResult
	if err := json.Unmarshal(data, &results); err != nil {
		return nil
	}
	return results
}

func normalizeDeepAgentParallelBranchSpecs(specs []DeepAgentParallelBranchSpec) []DeepAgentParallelBranchSpec {
	out := make([]DeepAgentParallelBranchSpec, 0, len(specs))
	for idx, spec := range specs {
		spec.ID = strings.TrimSpace(spec.ID)
		if spec.ID == "" {
			spec.ID = fmt.Sprintf("branch-%d", idx+1)
		}
		spec.Title = firstNonEmptyString(spec.Title, strings.ReplaceAll(spec.ID, "-", " "))
		spec.Task = strings.TrimSpace(spec.Task)
		if spec.Task == "" {
			spec.Task = spec.Title
		}
		spec.Tool = normalizeDeepAgentRouteMode(firstNonEmptyString(spec.Tool, DeepAgentToolModeModel))
		if len(spec.AllowedTools) == 0 && spec.Tool == DeepAgentToolModeModel {
			spec.AllowedTools = []string{"WebSearch", "WebFetch"}
		}
		spec.Kind = firstNonEmptyString(spec.Kind, deepAgentWorkflowString(spec.Metadata, "branch_kind"), deepAgentWorkflowString(spec.Metadata, "kind"))
		if spec.Kind == "" {
			if deepAgentParallelSpecIsSupplemental(spec) {
				spec.Kind = "supplemental"
			} else {
				spec.Kind = "primary"
			}
		}
		spec.CoverageDimension = firstNonEmptyString(spec.CoverageDimension, deepAgentWorkflowString(spec.Metadata, "coverage_dimension"), deepAgentParallelInferCoverageDimension(strings.Join([]string{spec.ID, spec.Title, spec.Task}, "\n")))
		if spec.Metadata == nil {
			spec.Metadata = map[string]any{}
		}
		spec.Metadata["branch_kind"] = spec.Kind
		if spec.CoverageDimension != "" {
			spec.Metadata["coverage_dimension"] = spec.CoverageDimension
		}
		out = append(out, spec)
	}
	return out
}

func singleDeepAgentParallelBranchSpec(id, title, task string) []DeepAgentParallelBranchSpec {
	id = strings.TrimSpace(id)
	if id == "" {
		id = "primary"
	}
	title = firstNonEmptyString(title, "Primary objective")
	task = strings.TrimSpace(task)
	if task == "" {
		task = title
	}
	return []DeepAgentParallelBranchSpec{{
		ID:                id,
		Title:             title,
		Task:              task,
		Kind:              "primary",
		CoverageDimension: deepAgentParallelInferCoverageDimension(strings.Join([]string{id, title, task}, "\n")),
		SuccessCriteria: []string{
			"Complete this scoped objective with concise evidence and source URLs.",
		},
	}}
}

func defaultDeepAgentParallelBranchSpecs(task string) []DeepAgentParallelBranchSpec {
	return deepAgentParallelCoverageBranchSpecs(task, deepAgentParallelDefaultPrimaryBranches)
}

func deepAgentParallelCoverageBranchSpecs(task string, maxBranches int) []DeepAgentParallelBranchSpec {
	task = strings.TrimSpace(task)
	if task == "" {
		return nil
	}
	if maxBranches <= 0 {
		maxBranches = deepAgentParallelDefaultPrimaryBranches
	}
	dimensions := deepAgentParallelCoverageDimensionsForText(task)
	if len(dimensions) == 0 {
		dimensions = deepAgentParallelCoverageDimensions()
	}
	if len(dimensions) > maxBranches {
		dimensions = dimensions[:maxBranches]
	}
	out := make([]DeepAgentParallelBranchSpec, 0, len(dimensions))
	for _, dim := range dimensions {
		out = append(out, DeepAgentParallelBranchSpec{
			ID:                deepAgentParallelDimensionSlug(dim.ID),
			Title:             dim.Label,
			Task:              fmt.Sprintf("%s\n\nFocus only on coverage dimension: %s. Avoid repeating other branch scopes unless needed for context.", task, dim.Label),
			Kind:              "primary",
			CoverageDimension: dim.ID,
			AllowedTools:      []string{"WebSearch", "WebFetch"},
			SuccessCriteria: []string{
				"Return concise findings for this coverage dimension.",
				"Include source URLs or source titles when available.",
				"Call out conflicts, gaps, and uncertainty instead of over-claiming.",
			},
			Metadata: map[string]any{
				"branch_kind":        "primary",
				"coverage_dimension": dim.ID,
			},
		})
	}
	return out
}

func deepAgentParallelCoverageDimensionsForText(text string) []deepAgentParallelCoverageDimension {
	corpus := strings.ToLower(strings.TrimSpace(text))
	if corpus == "" {
		return nil
	}
	var out []deepAgentParallelCoverageDimension
	for _, dim := range deepAgentParallelCoverageDimensions() {
		if deepAgentContainsAny(corpus, dim.Keywords...) || strings.Contains(corpus, strings.ToLower(dim.ID)) {
			out = append(out, dim)
		}
	}
	return out
}

func deepAgentParallelInferCoverageDimension(text string) string {
	dimensions := deepAgentParallelCoverageDimensionsForText(text)
	if len(dimensions) == 0 {
		return ""
	}
	return dimensions[0].ID
}

func deepAgentStepLooksParallelizable(step DeepAgentStep) bool {
	text := strings.ToLower(strings.TrimSpace(strings.Join([]string{step.Title, step.Intent, step.DoneCondition}, "\n")))
	if text == "" || deepAgentStepRequiresArtifact(step) {
		return false
	}
	mutating := deepAgentContainsAny(text, "实现", "修改", "修复", "写代码", "编码", "重构", "生成文件", "创建文件", "implement", "modify", "fix", "refactor", "write file")
	readonly := deepAgentContainsAny(text, "只读", "不修改", "不产生代码变更", "read-only", "readonly", "without modifying", "no code changes")
	if mutating && !readonly {
		return false
	}
	if deepAgentContainsAny(text,
		"并行", "多方向", "多路", "多分支", "多子任务", "多个子任务", "多智能体", "multi-agent", "multi agent",
		"parallel", "fan-out", "fan out", "branch executor", "parallel branches",
	) {
		return true
	}
	if deepAgentContainsAny(text, "大纲", "整合", "汇总", "总结", "最终", "撰写", "形成报告", "outline", "synthesize", "summarize", "final") {
		return false
	}
	if deepAgentContainsAny(text,
		"调研", "研究", "分析", "审查", "评估", "对比", "排查", "只读", "报告", "资料收集",
		"research", "investigate", "analysis", "audit", "review", "compare", "evaluate", "read-only", "report",
	) {
		return true
	}
	return false
}

func deepAgentParallelBranchTool(spec DeepAgentParallelBranchSpec) string {
	mode := normalizeDeepAgentRouteMode(spec.Tool)
	if mode == DeepAgentToolModeWeb {
		return DeepAgentToolModeModel
	}
	if mode == "" || mode == DeepAgentToolModeMulti || mode == DeepAgentToolModeModelArtifact {
		return DeepAgentToolModeModel
	}
	return mode
}

func deepAgentParallelBranchRoute(stepID, mode string, allowedTools []string) DeepAgentStepRoute {
	mode = normalizeDeepAgentRouteMode(firstNonEmptyString(mode, DeepAgentToolModeModel))
	if mode == DeepAgentToolModeMulti || mode == DeepAgentToolModeWeb || mode == DeepAgentToolModeModelArtifact {
		mode = DeepAgentToolModeModel
	}
	route := DeepAgentStepRoute{
		StepID:          stepID,
		Version:         "parallel-v1",
		Mode:            mode,
		Executor:        deepAgentExecutorForMode(mode),
		DeliverableType: deepAgentDeliverableNone,
		AllowedTools:    append([]string(nil), allowedTools...),
		SearchScope:     "web",
		Reason:          "parallel branch forced single-branch execution",
		Confidence:      "high",
	}
	if len(route.AllowedTools) == 0 && route.Mode == DeepAgentToolModeModel {
		route.AllowedTools = []string{"WebSearch", "WebFetch"}
	}
	return route
}

func deepAgentParallelBranchAllowedTools(spec DeepAgentParallelBranchSpec, action DeepAgentAction) []string {
	if len(spec.AllowedTools) > 0 {
		return append([]string(nil), spec.AllowedTools...)
	}
	if tools := deepAgentStringSlice(action.Args["allowed_tools"]); len(tools) > 0 {
		return tools
	}
	return []string{"WebSearch", "WebFetch"}
}

func deepAgentParallelPromptForBranch(action DeepAgentAction, spec DeepAgentParallelBranchSpec) string {
	if deepAgentParallelSpecIsConflictReconciliation(spec) {
		return deepAgentParallelConflictBranchPrompt(action, spec)
	}
	return deepAgentParallelBranchPrompt(action, spec)
}

func deepAgentParallelSpecIsConflictReconciliation(spec DeepAgentParallelBranchSpec) bool {
	if reconcile, ok := deepAgentMetadataBool(spec.Metadata, "conflict_reconcile"); ok && reconcile {
		return true
	}
	id := strings.ToLower(strings.TrimSpace(spec.ID))
	title := strings.ToLower(strings.TrimSpace(spec.Title))
	return strings.Contains(id, "conflict") || strings.Contains(title, "conflict reconciliation")
}

func deepAgentParallelSpecUsesInlineConflictReconciliation(spec DeepAgentParallelBranchSpec) bool {
	if reconcile, ok := deepAgentMetadataBool(spec.Metadata, "conflict_reconcile"); ok && reconcile {
		return true
	}
	id := strings.ToLower(strings.TrimSpace(spec.ID))
	title := strings.ToLower(strings.TrimSpace(spec.Title))
	return id == "supplement-conflict-reconciliation" || strings.Contains(title, "conflict reconciliation")
}

func deepAgentParallelDeepConflictReconciliationEnabled(action DeepAgentAction) bool {
	return deepAgentBool(action.Args, "run_conflict_reconciliation_branch", false) ||
		deepAgentBool(action.Args, "deep_conflict_reconciliation", false)
}

func deepAgentParallelBranchPrompt(action DeepAgentAction, spec DeepAgentParallelBranchSpec) string {
	var b strings.Builder
	if goal := deepAgentActionString(action, "goal"); goal != "" {
		b.WriteString("Parent goal:\n")
		b.WriteString(goal)
		b.WriteString("\n\n")
	}
	b.WriteString("Parallel branch task:\n")
	b.WriteString(spec.Task)
	if len(spec.SuccessCriteria) > 0 {
		b.WriteString("\n\nBranch success criteria:\n")
		for _, item := range spec.SuccessCriteria {
			b.WriteString("- ")
			b.WriteString(item)
			b.WriteString("\n")
		}
	}
	b.WriteString("\n\nThis is already one isolated branch inside a parallel research group. Do not create another multi-agent plan, do not fan out into more branches, and do not defer to a separate parallel executor. Complete only this branch task directly.")
	b.WriteString("\n\nSource budget and quality:\n")
	budget := spec.Budget
	if budget.MaxSources <= 0 {
		budget.MaxSources = deepAgentParallelMaxSourcesPerBranch
	}
	if budget.MaxToolCalls <= 0 {
		budget.MaxToolCalls = deepAgentParallelDefaultMaxToolCalls
	}
	if budget.MaxTokens <= 0 {
		budget.MaxTokens = deepAgentParallelDefaultMaxTokens
	}
	sourcePolicy := deepAgentSourcePolicyFromAction(action)
	if budget.MaxSources > 0 && sourcePolicy.MaxSourcesPerBranch > budget.MaxSources {
		sourcePolicy.MaxSourcesPerBranch = budget.MaxSources
	}
	b.WriteString("- Start with WebSearch; use WebFetch only for the best source URLs when snippets are insufficient.\n")
	b.WriteString("- Use at most 3 search queries and fetch at most 4 pages for this branch.\n")
	b.WriteString(fmt.Sprintf("- Keep at most %d tool calls for this branch.\n", budget.MaxToolCalls))
	b.WriteString(fmt.Sprintf("- Keep the branch answer within roughly %d tokens.\n", budget.MaxTokens))
	b.WriteString(deepAgentSourcePolicyPrompt(sourcePolicy))
	b.WriteString("\n")
	b.WriteString("- Stop searching once branch criteria are covered; summarize low-confidence gaps instead of collecting more links.")
	b.WriteString("\n\nReturn concise evidence with source URLs or titles when available. Extract key factual claims as field/value evidence when possible, call out contradictions, and mark weak or unverifiable evidence. Do not create final deliverable artifacts in this branch and do not perform writes.")
	return b.String()
}

func deepAgentParallelConflictBranchPrompt(action DeepAgentAction, spec DeepAgentParallelBranchSpec) string {
	var b strings.Builder
	b.WriteString("You are a conflict reconciliation branch inside an already-running multi-agent research group.\n")
	if goal := truncateDeepAgentDiagnosticText(deepAgentActionString(action, "goal"), 700); goal != "" {
		b.WriteString("\nUser goal summary:\n")
		b.WriteString(goal)
		b.WriteString("\n")
	}
	b.WriteString("\nConflict reconciliation task:\n")
	b.WriteString(truncateDeepAgentDiagnosticText(spec.Task, 2200))
	if len(spec.SuccessCriteria) > 0 {
		b.WriteString("\n\nSuccess criteria:\n")
		for _, item := range spec.SuccessCriteria {
			b.WriteString("- ")
			b.WriteString(item)
			b.WriteString("\n")
		}
	}
	b.WriteString("\n\nInstructions:\n")
	b.WriteString("- Verify only the listed conflicting claims; do not restart the full research task.\n")
	b.WriteString("- Use WebSearch and WebFetch for primary or high-quality independent sources when needed; do not browse broadly.\n")
	b.WriteString("- Use at most 2 search queries, fetch at most 3 pages, and keep at most 6 unique sources.\n")
	b.WriteString("- Return a concise adjudication for each conflict: best_supported, confidence, evidence, and remaining_uncertainty.\n")
	b.WriteString("- If the evidence cannot resolve a conflict, preserve it as uncertainty instead of guessing.\n")
	b.WriteString("- Keep the response under 900 words and do not create artifacts or writes.\n")
	return b.String()
}

func deepAgentParallelConflictFallbackResult(spec DeepAgentParallelBranchSpec, errorText string) (DeepAgentParallelBranchResult, bool) {
	if !deepAgentParallelSpecIsConflictReconciliation(spec) || !deepAgentParallelConflictFallbackAllowed(errorText) {
		return DeepAgentParallelBranchResult{}, false
	}
	output := "Conflict reconciliation did not complete within the model/tool budget. Treat the listed conflicts as unresolved uncertainty and do not silently choose one claim.\n\n"
	if strings.TrimSpace(spec.Task) != "" {
		output += truncateDeepAgentDiagnosticText(spec.Task, 1800)
	}
	return DeepAgentParallelBranchResult{
		ID:     spec.ID,
		Title:  spec.Title,
		Status: DeepAgentActionStatusSucceeded,
		Output: output,
		Metadata: map[string]any{
			"supplemental":             true,
			"conflict_reconcile":       true,
			"fallback_reconciliation":  true,
			"unresolved_conflicts":     true,
			"original_error":           truncateDeepAgentDiagnosticText(errorText, 600),
			"side_effect_level":        deepAgentSideEffectReadonly,
			"parallel_branch_fallback": true,
		},
	}, true
}

func deepAgentParallelInlineConflictResult(spec DeepAgentParallelBranchSpec) DeepAgentParallelBranchResult {
	lines := deepAgentStringSlice(spec.Metadata["conflict_lines"])
	var b strings.Builder
	b.WriteString("Conflict reconciliation completed using existing parallel branch evidence without starting another model/tool research pass.\n\n")
	if len(lines) > 0 {
		b.WriteString("Unresolved or conflicting claims:\n")
		for _, line := range lines {
			line = truncateDeepAgentDiagnosticText(strings.TrimSpace(line), 220)
			if line == "" {
				continue
			}
			b.WriteString("- ")
			b.WriteString(line)
			b.WriteString("\n")
		}
	} else if task := strings.TrimSpace(spec.Task); task != "" {
		b.WriteString(truncateDeepAgentDiagnosticText(task, 1200))
		b.WriteString("\n")
	} else {
		b.WriteString("No compact conflict details were provided by the parent branch group.\n")
	}
	b.WriteString("\nTreat unresolved conflicts as uncertainty in the final answer unless a cited primary source clearly resolves them.")
	return DeepAgentParallelBranchResult{
		ID:     spec.ID,
		Title:  spec.Title,
		Status: DeepAgentActionStatusSucceeded,
		Output: strings.TrimSpace(b.String()),
		Metadata: map[string]any{
			"supplemental":                       true,
			"conflict_reconcile":                 true,
			"inline_conflict_reconciliation":     true,
			"unresolved_conflicts":               true,
			"side_effect_level":                  deepAgentSideEffectReadonly,
			"parallel_branch_inline_resolution":  true,
			"parallel_branch_skipped_deep_tools": true,
		},
	}
}

func deepAgentParallelConflictFallbackAllowed(errorText string) bool {
	text := strings.ToLower(strings.TrimSpace(errorText))
	if text == "" {
		return false
	}
	return strings.Contains(text, "context deadline exceeded") ||
		strings.Contains(text, "deadline") ||
		strings.Contains(text, "timeout") ||
		strings.Contains(text, "empty response") ||
		strings.Contains(text, "no assistant text")
}

func deepAgentParallelResultIsSupplemental(result DeepAgentParallelBranchResult) bool {
	if supplemental, ok := deepAgentMetadataBool(result.Metadata, "supplemental"); ok && supplemental {
		return true
	}
	id := strings.ToLower(strings.TrimSpace(result.ID))
	title := strings.ToLower(strings.TrimSpace(result.Title))
	return strings.HasPrefix(id, "supplement-") || strings.Contains(title, "supplement") || strings.Contains(title, "reconciliation")
}

func deepAgentParallelSpecIsSupplemental(spec DeepAgentParallelBranchSpec) bool {
	if strings.EqualFold(strings.TrimSpace(spec.Kind), "supplemental") {
		return true
	}
	if supplemental, ok := deepAgentMetadataBool(spec.Metadata, "supplemental"); ok && supplemental {
		return true
	}
	id := strings.ToLower(strings.TrimSpace(spec.ID))
	title := strings.ToLower(strings.TrimSpace(spec.Title))
	return strings.HasPrefix(id, "supplement-") || strings.Contains(title, "supplement") || strings.Contains(title, "reconciliation")
}

func deepAgentParallelSpecKind(spec DeepAgentParallelBranchSpec) string {
	if kind := strings.ToLower(strings.TrimSpace(spec.Kind)); kind != "" {
		return kind
	}
	if deepAgentParallelSpecIsSupplemental(spec) {
		return "supplemental"
	}
	return "primary"
}

func cloneDeepAgentStateForParallelBranch(state *DeepAgentState) *DeepAgentState {
	if state == nil {
		return &DeepAgentState{WorkingMemory: map[string]any{}}
	}
	data, err := json.Marshal(state)
	if err != nil {
		return &DeepAgentState{Goal: state.Goal, WorkingMemory: cloneWorkflowMap(state.WorkingMemory)}
	}
	var out DeepAgentState
	if err := json.Unmarshal(data, &out); err != nil {
		return &DeepAgentState{Goal: state.Goal, WorkingMemory: cloneWorkflowMap(state.WorkingMemory)}
	}
	if out.WorkingMemory == nil {
		out.WorkingMemory = map[string]any{}
	}
	delete(out.WorkingMemory, "session_id")
	return &out
}

func dedupeDeepAgentSourceRefs(refs []DeepAgentSourceRef) []DeepAgentSourceRef {
	seen := map[string]struct{}{}
	out := make([]DeepAgentSourceRef, 0, len(refs))
	for _, ref := range refs {
		key := deepAgentSourceRefKey(ref)
		if key == "" {
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

func deepAgentSourceRefKey(ref DeepAgentSourceRef) string {
	if key := normalizeDeepAgentSourceURL(ref.URL); key != "" {
		return key
	}
	return strings.ToLower(strings.TrimSpace(firstNonEmptyString(ref.Title+"|"+ref.Provider, ref.Title, ref.Provider)))
}

func normalizeDeepAgentSourceURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return strings.ToLower(strings.TrimRight(raw, "/"))
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	parsed.Fragment = ""
	values := parsed.Query()
	for key := range values {
		lower := strings.ToLower(key)
		if strings.HasPrefix(lower, "utm_") ||
			lower == "fbclid" ||
			lower == "gclid" ||
			lower == "msclkid" ||
			lower == "mc_cid" ||
			lower == "mc_eid" ||
			lower == "igshid" ||
			lower == "ref" ||
			lower == "source" {
			values.Del(key)
		}
	}
	parsed.RawQuery = values.Encode()
	normalized := parsed.String()
	if parsed.RawQuery == "" {
		normalized = strings.TrimRight(normalized, "/")
	}
	return strings.ToLower(normalized)
}

func emitDeepAgentParallelEvent(ctx context.Context, eventType string, action DeepAgentAction, route DeepAgentStepRoute, payload map[string]any) {
	payload = cloneWorkflowMap(payload)
	payload["type"] = eventType
	payload["event_group"] = "parallel_workflow"
	payload["step_id"] = firstNonEmptyString(route.StepID, action.StepID)
	payload["tool"] = DeepAgentToolModeMulti
	payload["route"] = deepAgentStepRouteMap(route)
	emitJobEventFromContext(ctx, Event{
		Type:    eventType,
		Role:    "workflow",
		Content: strings.TrimSpace(fmt.Sprintf("%s %s", firstNonEmptyString(route.StepID, action.StepID), eventType)),
		Error:   deepAgentWorkflowString(payload, "error"),
		Data:    deepAgentEventData(payload),
	})
}
