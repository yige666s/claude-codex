package agentruntime

import (
	"context"
	"fmt"
	"strings"
)

const (
	DeepAgentGatePlan      = "plan"
	DeepAgentGateExecution = "execution"
	DeepAgentGateVerify    = "verify"
)

func allowGateDecision(gate string, refs ...string) GateDecision {
	return GateDecision{Gate: gate, Allow: true, EvidenceRefs: cleanStringSlice(refs)}
}

func blockGateDecision(gate, category, reason, hint string, refs ...string) GateDecision {
	return GateDecision{
		Gate:         gate,
		Allow:        false,
		Category:     strings.TrimSpace(category),
		BlockReason:  strings.TrimSpace(reason),
		RepairHint:   strings.TrimSpace(hint),
		EvidenceRefs: cleanStringSlice(refs),
	}
}

func reviewGateDecision(gate, category, reason, hint string, refs ...string) GateDecision {
	decision := blockGateDecision(gate, category, reason, hint, refs...)
	decision.RequiresReview = true
	return decision
}

func (c *DeepAgentController) planGateDecision(state *DeepAgentState, plan DeepAgentPlan, policy DeepAgentPolicy) GateDecision {
	contract := deepAgentStateLoopContract(state)
	if len(plan.Steps) == 0 {
		return blockGateDecision(DeepAgentGatePlan, "plan", "plan has no executable steps", "Regenerate the plan with at least one concrete step.")
	}
	maxSteps := firstPositiveGateInt(contract.Budget.MaxSteps, policy.MaxSteps)
	if maxSteps > 0 && len(plan.Steps) > maxSteps {
		return blockGateDecision(
			DeepAgentGatePlan,
			"budget",
			fmt.Sprintf("plan has %d steps, max is %d", len(plan.Steps), maxSteps),
			"Reduce the plan scope or increase the step budget.",
			"loop_contract.budget.max_steps",
		)
	}
	allowedModes := cleanStringSlice(contract.ToolPolicy.AllowedModes)
	for _, step := range plan.Steps {
		mode := deepAgentStepPlannedMode(step)
		if mode == "" {
			continue
		}
		if len(allowedModes) > 0 && !deepAgentStringSliceContains(allowedModes, mode) {
			return blockGateDecision(
				DeepAgentGatePlan,
				"tool",
				fmt.Sprintf("plan step %s uses tool mode %s outside the loop contract", firstNonEmptyString(step.ID, step.Title), mode),
				"Choose an allowed tool mode or update the loop contract tool policy.",
				firstNonEmptyString(step.ID, step.Title),
			)
		}
	}
	if missing := planMissingContractRubric(plan, contract); len(missing) > 0 {
		return GateDecision{
			Gate:         DeepAgentGatePlan,
			Allow:        true,
			Category:     "rubric",
			RepairHint:   "Plan coverage is partial; verifier must check the missing rubric items before completion.",
			EvidenceRefs: missing,
		}
	}
	if contract.RiskPolicy.RequiresReview {
		return reviewGateDecision(
			DeepAgentGatePlan,
			"review",
			"loop contract requires human review before execution",
			"Review and approve the plan before continuing.",
			"loop_contract.risk_policy.requires_review",
		)
	}
	return allowGateDecision(DeepAgentGatePlan, "loop_contract", "plan")
}

func (c *DeepAgentController) executionGateDecision(state *DeepAgentState, step DeepAgentStep, action DeepAgentAction, policy DeepAgentPolicy) GateDecision {
	contract := deepAgentStateLoopContract(state)
	if action.Args == nil {
		return blockGateDecision(DeepAgentGateExecution, "tool", "tool arguments are missing", "Regenerate the action with structured tool arguments.", firstNonEmptyString(step.ID, action.StepID))
	}
	mode := normalizeDeepAgentRouteMode(firstNonEmptyString(action.Tool, DeepAgentToolModeModel))
	allowedModes := cleanStringSlice(contract.ToolPolicy.AllowedModes)
	_, hasStructuredRoute := deepAgentStepRouteFromMap(action.Args)
	if hasStructuredRoute && len(allowedModes) > 0 && !deepAgentStringSliceContains(allowedModes, mode) {
		return blockGateDecision(
			DeepAgentGateExecution,
			"tool",
			fmt.Sprintf("tool mode %s is not allowed by the loop contract", mode),
			"Use an allowed tool mode or update the loop contract tool policy.",
			firstNonEmptyString(step.ID, action.StepID),
		)
	}
	if provider := deepAgentActionConnectorProvider(action); provider != "" && !deepAgentStringSliceContains(contract.ToolPolicy.ConnectorContext, provider) {
		return reviewGateDecision(
			DeepAgentGateExecution,
			"auth",
			fmt.Sprintf("%s connector is not authorized for this loop", provider),
			fmt.Sprintf("Reconnect or authorize %s, then retry this step.", provider),
			firstNonEmptyString(step.ID, action.StepID),
		)
	}
	if forbidden := matchingForbiddenAction(action, contract.RiskPolicy.ForbiddenActions); forbidden != "" {
		return blockGateDecision(
			DeepAgentGateExecution,
			"risk",
			fmt.Sprintf("action matches forbidden risk policy: %s", forbidden),
			"Choose a lower-risk action that stays inside the loop contract.",
			firstNonEmptyString(step.ID, action.StepID),
		)
	}
	if deepAgentResearchStepNeedsSources(contract, step, action) && !deepAgentActionCanCollectSources(action) {
		return blockGateDecision(
			DeepAgentGateExecution,
			"source",
			"research step does not satisfy the loop source policy",
			"Use WebSearch/WebFetch or an authorized connector before synthesizing.",
			firstNonEmptyString(step.ID, action.StepID),
		)
	}
	return allowGateDecision(DeepAgentGateExecution, firstNonEmptyString(step.ID, action.StepID))
}

func (c *DeepAgentController) verifyGateDecision(state *DeepAgentState, verification DeepAgentFinalVerification) GateDecision {
	contract := deepAgentStateLoopContract(state)
	refs := []string{"final_verification"}
	for _, check := range verification.Checks {
		if check.Name != "" {
			refs = append(refs, check.Name)
		}
	}
	if !verification.Done {
		category := verifyGateCategory(verification)
		return blockGateDecision(
			DeepAgentGateVerify,
			category,
			firstNonEmptyString(verification.Reason, "final verification did not pass"),
			verifyGateRepairHint(category),
			refs...,
		)
	}
	summary := deepAgentStateEvidenceSummary(state)
	if contract.SourcePolicy.RequiresSources && verifyGateShouldEnforceSources(contract, verification) {
		required := firstPositiveGateInt(contract.SourcePolicy.MinSourceCount, 1)
		if summary.sourceCount < required && summary.citationCount < required {
			return blockGateDecision(
				DeepAgentGateVerify,
				"source",
				fmt.Sprintf("source evidence is insufficient: got %d sources and %d citations, want at least %d", summary.sourceCount, summary.citationCount, required),
				"Collect more traceable source evidence before finalizing.",
				refs...,
			)
		}
	}
	if len(contract.EvaluatorPolicy.ArtifactRequired) > 0 && summary.artifactCount == 0 {
		return blockGateDecision(
			DeepAgentGateVerify,
			"artifact",
			"required artifact is missing",
			"Create and attach the required artifact, then rerun verification.",
			refs...,
		)
	}
	if unresolved := unresolvedDeepAgentConflicts(state); len(unresolved) > 0 {
		return blockGateDecision(
			DeepAgentGateVerify,
			"conflict",
			"unresolved conflicts remain: "+strings.Join(unresolved, "; "),
			"Resolve the conflicts or explicitly mark them as uncertainty in the final answer.",
			refs...,
		)
	}
	return allowGateDecision(DeepAgentGateVerify, refs...)
}

func verifyGateShouldEnforceSources(contract LoopContract, verification DeepAgentFinalVerification) bool {
	if len(contract.Rubric.RequiredEvidence) > 0 || len(contract.EvaluatorPolicy.EvidenceRequired) > 0 {
		return true
	}
	return verification.ResearchQuality != nil && verification.ResearchQuality.Required
}

func deepAgentStateLoopContract(state *DeepAgentState) LoopContract {
	if state == nil {
		return LoopContract{}
	}
	contract := state.LoopContract
	if contract.ID == "" && state.WorkingMemory != nil {
		contract = loopContractFromWorkflowValue(state.WorkingMemory["loop_contract"])
	}
	return contract
}

func deepAgentStepPlannedMode(step DeepAgentStep) string {
	if route, ok := deepAgentStepRouteFromMap(step.Metadata); ok {
		return normalizeDeepAgentRouteMode(route.Mode)
	}
	return normalizeDeepAgentRouteMode(firstNonEmptyString(deepAgentWorkflowString(step.Metadata, "tool"), deepAgentWorkflowString(step.Metadata, "mode")))
}

func planMissingContractRubric(plan DeepAgentPlan, contract LoopContract) []string {
	var missing []string
	text := strings.ToLower(plan.Goal + "\n" + deepAgentPlanText(plan))
	for _, item := range append(append([]string{}, contract.Rubric.AcceptanceCriteria...), contract.Rubric.RequiredEvidence...) {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if !deepAgentPlanCoversRubricText(text, item) {
			missing = append(missing, item)
		}
	}
	for _, item := range contract.Rubric.RequiredArtifacts {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if !strings.Contains(text, "artifact") && !strings.Contains(text, "文档") && !strings.Contains(text, strings.ToLower(item)) {
			missing = append(missing, item)
		}
	}
	return missing
}

func deepAgentPlanText(plan DeepAgentPlan) string {
	var b strings.Builder
	for _, step := range plan.Steps {
		b.WriteString(step.ID)
		b.WriteByte('\n')
		b.WriteString(step.Title)
		b.WriteByte('\n')
		b.WriteString(step.Intent)
		b.WriteByte('\n')
		b.WriteString(step.DoneCondition)
		b.WriteByte('\n')
		if len(step.Metadata) > 0 {
			for key, value := range step.Metadata {
				b.WriteString(key)
				b.WriteByte(' ')
				b.WriteString(fmt.Sprint(value))
				b.WriteByte('\n')
			}
		}
	}
	return b.String()
}

func deepAgentPlanCoversRubricText(planText, rubric string) bool {
	needle := strings.ToLower(strings.TrimSpace(rubric))
	if needle == "" {
		return true
	}
	if strings.Contains(planText, needle) {
		return true
	}
	tokens := strings.FieldsFunc(needle, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == ',' || r == ';' || r == ':' || r == '.' || r == '，' || r == '。'
	})
	meaningful := 0
	covered := 0
	for _, token := range tokens {
		token = strings.TrimSpace(token)
		if len([]rune(token)) < 4 && !containsHan(token) {
			continue
		}
		meaningful++
		if strings.Contains(planText, token) {
			covered++
		}
	}
	if meaningful == 0 {
		return true
	}
	return covered >= meaningful
}

func deepAgentActionConnectorProvider(action DeepAgentAction) string {
	mode := normalizeDeepAgentRouteMode(action.Tool)
	route, _ := deepAgentStepRouteFromMap(action.Args)
	scope := strings.ToLower(firstNonEmptyString(route.SearchScope, deepAgentActionString(action, "search_scope"), deepAgentActionString(action, "provider")))
	if mode == DeepAgentToolModeConnector {
		return firstNonEmptyString(scope, "github")
	}
	if scope == "github" {
		return "github"
	}
	for _, tool := range route.AllowedTools {
		if strings.Contains(strings.ToLower(tool), "github") {
			return "github"
		}
	}
	return ""
}

func matchingForbiddenAction(action DeepAgentAction, forbidden []string) string {
	if len(forbidden) == 0 {
		return ""
	}
	text := strings.ToLower(strings.Join([]string{action.Tool, fmt.Sprint(action.Args)}, "\n"))
	for _, item := range forbidden {
		item = strings.ToLower(strings.TrimSpace(item))
		if item != "" && strings.Contains(text, item) {
			return item
		}
	}
	return ""
}

func deepAgentResearchStepNeedsSources(contract LoopContract, step DeepAgentStep, action DeepAgentAction) bool {
	if !contract.SourcePolicy.RequiresSources {
		return false
	}
	route, _ := deepAgentStepRouteFromMap(action.Args)
	route.Mode = firstNonEmptyString(route.Mode, action.Tool)
	return deepAgentRouteLooksLikeResearch(route, step)
}

func deepAgentActionCanCollectSources(action DeepAgentAction) bool {
	mode := normalizeDeepAgentRouteMode(action.Tool)
	if mode == DeepAgentToolModeWeb || mode == DeepAgentToolModeMulti || mode == DeepAgentToolModeConnector {
		return true
	}
	route, _ := deepAgentStepRouteFromMap(action.Args)
	if route.SearchScope == "web" || route.SearchScope == "github" {
		return true
	}
	allowed := append([]string{}, route.AllowedTools...)
	allowed = append(allowed, deepAgentStringSlice(action.Args["allowed_tools"])...)
	for _, tool := range allowed {
		tool = strings.ToLower(strings.TrimSpace(tool))
		if tool == "websearch" || tool == "webfetch" || strings.Contains(tool, "github") {
			return true
		}
	}
	return false
}

func verifyGateCategory(verification DeepAgentFinalVerification) string {
	text := strings.ToLower(strings.Join(append([]string{verification.Reason}, verification.Missing...), "\n"))
	switch {
	case strings.Contains(text, "artifact"):
		return "artifact"
	case strings.Contains(text, "source") || strings.Contains(text, "citation"):
		return "source"
	case strings.Contains(text, "conflict"):
		return "conflict"
	case strings.Contains(text, "budget"):
		return "budget"
	case strings.Contains(text, "auth") || strings.Contains(text, "connect"):
		return "auth"
	default:
		return "rubric"
	}
}

func verifyGateRepairHint(category string) string {
	switch category {
	case "artifact":
		return "Create and attach the required artifact, then rerun verification."
	case "source":
		return "Collect enough traceable sources before finalizing."
	case "conflict":
		return "Resolve the conflicts or explicitly preserve them as uncertainty."
	case "budget":
		return "Increase budget or reduce the remaining scope."
	case "auth":
		return "Reconnect or authorize the required connector, then retry."
	default:
		return "Address the failed rubric item and rerun verification."
	}
}

func unresolvedDeepAgentConflicts(state *DeepAgentState) []string {
	if state == nil || state.WorkingMemory == nil {
		return nil
	}
	var out []string
	collect := func(values map[string]any) {
		if len(values) == 0 {
			return
		}
		if deepAgentBool(values, "unresolved_conflicts", false) {
			out = append(out, firstNonEmptyString(deepAgentWorkflowString(values, "summary"), "unresolved conflicts"))
		}
		if count := deepAgentAnyInt(values["conflict_count"], 0); count > 0 {
			out = append(out, fmt.Sprintf("%d unresolved conflict(s)", count))
		}
	}
	collect(state.WorkingMemory)
	store, _ := state.WorkingMemory["step_context"].(map[string]any)
	for _, raw := range store {
		record, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		collect(record)
		if metadata, _ := record["metadata"].(map[string]any); len(metadata) > 0 {
			collect(metadata)
		}
	}
	return cleanStringSlice(out)
}

func firstPositiveGateInt(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func (c *DeepAgentController) recordGateDecision(ctx context.Context, run *WorkflowRun, state *DeepAgentState, decision GateDecision) {
	if state != nil {
		state.GateDecisions = append(state.GateDecisions, decision)
		if state.WorkingMemory == nil {
			state.WorkingMemory = map[string]any{}
		}
		state.WorkingMemory["last_gate_decision"] = decision
		state.WorkingMemory["gate_decisions"] = append([]GateDecision(nil), state.GateDecisions...)
	}
	if run != nil {
		if run.State == nil {
			run.State = map[string]any{}
		}
		run.State["last_gate_decision"] = decision
		if state != nil {
			run.State["gate_decisions"] = append([]GateDecision(nil), state.GateDecisions...)
		}
	}
	if c == nil || run == nil {
		return
	}
	content := "Gate allowed"
	if !decision.Allow {
		content = firstNonEmptyString(decision.BlockReason, "Gate blocked execution")
	}
	payload := map[string]any{
		"type":            "deep_agent_gate_decision",
		"event_group":     "gate",
		"workflow_name":   run.Name,
		"run_id":          run.ID,
		"job_id":          run.JobID,
		"session_id":      run.SessionID,
		"user_id":         run.UserID,
		"gate_decision":   decision,
		"gate":            decision.Gate,
		"allow":           decision.Allow,
		"block_reason":    decision.BlockReason,
		"requires_review": decision.RequiresReview,
		"repair_hint":     decision.RepairHint,
		"evidence_refs":   decision.EvidenceRefs,
		"category":        decision.Category,
	}
	emitJobEventFromContext(ctx, Event{
		Type:      "deep_agent_gate_decision",
		SessionID: run.SessionID,
		JobID:     run.JobID,
		Role:      "workflow",
		Content:   content,
		Error:     gateDecisionErrorText(decision),
		Data:      deepAgentEventData(payload),
	})
}

func gateDecisionErrorText(decision GateDecision) string {
	if decision.Allow {
		return ""
	}
	return firstNonEmptyString(decision.BlockReason, "gate blocked execution")
}
