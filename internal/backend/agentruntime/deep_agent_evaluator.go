package agentruntime

import (
	"context"
	"strings"
)

type ruleDeepAgentEvaluator struct {
	verifier DeepAgentVerifier
}

func newRuleDeepAgentEvaluator(verifier DeepAgentVerifier) ruleDeepAgentEvaluator {
	if verifier == nil {
		verifier = ruleDeepAgentVerifier{}
	}
	return ruleDeepAgentEvaluator{verifier: verifier}
}

func (e ruleDeepAgentEvaluator) EvaluateProgress(_ context.Context, input DeepAgentEvaluatorInput) (DeepAgentEvaluatorVerdict, error) {
	failed := []string{}
	if input.TraceSummary.Blocker != "" {
		failed = append(failed, input.TraceSummary.Blocker)
	}
	if len(input.TraceSummary.FailedSteps) > 0 {
		failed = append(failed, "failed steps: "+strings.Join(input.TraceSummary.FailedSteps, ", "))
	}
	if len(failed) > 0 {
		return deepAgentEvaluatorRepairVerdict("progress requires repair", failed, "Inspect failed steps and rerun the needed action."), nil
	}
	return deepAgentEvaluatorPassVerdict("progress is on track", nil), nil
}

func (e ruleDeepAgentEvaluator) EvaluateFinal(ctx context.Context, input DeepAgentEvaluatorInput) (DeepAgentEvaluatorVerdict, error) {
	verdict := deepAgentVerdictFromTraceChecks(input.TraceSummary.VerifierChecks)
	if verdict.Verdict == "" {
		state := deepAgentEvaluatorInputState(input)
		final, err := e.verifier.CheckFinal(ctx, state)
		if err != nil {
			return DeepAgentEvaluatorVerdict{}, err
		}
		verdict = deepAgentVerdictFromFinalVerification(final)
	}
	sources, err := e.EvaluateSources(ctx, input)
	if err != nil {
		return DeepAgentEvaluatorVerdict{}, err
	}
	artifact, err := e.EvaluateArtifact(ctx, input)
	if err != nil {
		return DeepAgentEvaluatorVerdict{}, err
	}
	conflicts, err := e.EvaluateConflicts(ctx, input)
	if err != nil {
		return DeepAgentEvaluatorVerdict{}, err
	}
	verdict = mergeDeepAgentEvaluatorVerdicts(verdict, sources, artifact, conflicts)
	return verdict, nil
}

func (e ruleDeepAgentEvaluator) EvaluateSources(_ context.Context, input DeepAgentEvaluatorInput) (DeepAgentEvaluatorVerdict, error) {
	if verdict, ok := deepAgentEvaluatorVerdictFromChecks(input.TraceSummary.VerifierChecks, "source", "citation", "coverage", "entity", "gap"); ok {
		verdict.SourceCoverage = map[string]any{"from_verifier_checks": true}
		return verdict, nil
	}
	required := input.Contract.SourcePolicy.RequiresSources || len(input.Contract.Rubric.RequiredEvidence) > 0 || len(input.Contract.EvaluatorPolicy.EvidenceRequired) > 0
	requiredCount := firstPositiveGateInt(input.Contract.SourcePolicy.MinSourceCount, 1)
	sourceCount := 0
	citationCount := 0
	seen := map[string]struct{}{}
	for _, evidence := range input.Evidence {
		for _, source := range evidence.Sources {
			key := strings.ToLower(strings.TrimSpace(firstNonEmptyString(source.URL, source.Title, source.Snippet)))
			if key != "" {
				if _, ok := seen[key]; !ok {
					seen[key] = struct{}{}
				}
			}
			sourceCount++
		}
		citationCount += deepAgentCitationCount(DeepAgentActionResult{Output: evidence.Output})
	}
	coverage := map[string]any{"required": required, "source_count": sourceCount, "unique_source_count": len(seen), "citation_count": citationCount, "min_source_count": requiredCount}
	if required && sourceCount < requiredCount && citationCount < requiredCount {
		verdict := deepAgentEvaluatorRepairVerdict("source coverage is insufficient", []string{"source evidence below required minimum"}, "Collect traceable source evidence before finalizing.")
		verdict.SourceCoverage = coverage
		return verdict, nil
	}
	verdict := deepAgentEvaluatorPassVerdict("source coverage passed", nil)
	verdict.SourceCoverage = coverage
	return verdict, nil
}

func (e ruleDeepAgentEvaluator) EvaluateArtifact(_ context.Context, input DeepAgentEvaluatorInput) (DeepAgentEvaluatorVerdict, error) {
	if verdict, ok := deepAgentEvaluatorVerdictFromChecks(input.TraceSummary.VerifierChecks, "artifact"); ok {
		return verdict, nil
	}
	required := len(input.Contract.EvaluatorPolicy.ArtifactRequired) > 0 || len(input.Contract.Rubric.RequiredArtifacts) > 0
	if required && len(input.Artifacts) == 0 {
		return deepAgentEvaluatorRepairVerdict("required artifact is missing", []string{"required artifact is missing"}, "Create and attach the required artifact, then rerun final evaluation."), nil
	}
	return deepAgentEvaluatorPassVerdict("artifact coverage passed", nil), nil
}

func (e ruleDeepAgentEvaluator) EvaluateConflicts(_ context.Context, input DeepAgentEvaluatorInput) (DeepAgentEvaluatorVerdict, error) {
	state := deepAgentEvaluatorInputState(input)
	if unresolved := unresolvedDeepAgentConflicts(state); len(unresolved) > 0 {
		return deepAgentEvaluatorReviewVerdict("unresolved conflicts remain", unresolved, "Resolve the conflicts or explicitly document the uncertainty."), nil
	}
	return deepAgentEvaluatorPassVerdict("conflict check passed", nil), nil
}

func deepAgentEvaluatorInputFromState(state *DeepAgentState) DeepAgentEvaluatorInput {
	if state == nil {
		return DeepAgentEvaluatorInput{}
	}
	return DeepAgentEvaluatorInput{
		Contract:  deepAgentStateLoopContract(state),
		Evidence:  deepAgentEvidenceForSummary(state),
		Artifacts: deepAgentStateCurrentArtifactRefs(state),
		TraceSummary: DeepAgentEvaluatorTrace{
			Status:         state.Status,
			CompletedSteps: append([]string(nil), state.CompletedSteps...),
			FailedSteps:    append([]string(nil), state.FailedSteps...),
			ActionCount:    state.ActionCount,
			Blocker:        state.Blocker,
		},
	}
}

func deepAgentEvaluatorInputState(input DeepAgentEvaluatorInput) *DeepAgentState {
	state := &DeepAgentState{
		LoopContract:   input.Contract,
		Rubric:         input.Contract.Rubric,
		Status:         input.TraceSummary.Status,
		CompletedSteps: append([]string(nil), input.TraceSummary.CompletedSteps...),
		FailedSteps:    append([]string(nil), input.TraceSummary.FailedSteps...),
		ActionCount:    input.TraceSummary.ActionCount,
		Blocker:        input.TraceSummary.Blocker,
		WorkingMemory:  map[string]any{},
	}
	for _, evidence := range input.Evidence {
		(StateDeepAgentEvidenceStore{}).PutStepEvidence(state, evidence)
	}
	if len(input.Artifacts) > 0 {
		state.WorkingMemory["final_artifact_refs"] = append([]DeepAgentArtifactRef(nil), input.Artifacts...)
	}
	return state
}

func deepAgentVerdictFromFinalVerification(final DeepAgentFinalVerification) DeepAgentEvaluatorVerdict {
	failed := append([]string(nil), final.Missing...)
	for _, check := range final.Checks {
		if !check.Passed && strings.TrimSpace(check.Reason) != "" && !deepAgentStringSliceContains(failed, check.Reason) {
			failed = append(failed, check.Reason)
		}
	}
	if final.Done {
		verdict := deepAgentEvaluatorPassVerdict(firstNonEmptyString(final.Reason, "final evaluator passed"), final.Checks)
		verdict.Confidence = firstNonEmptyString(final.Confidence, "medium")
		verdict.RubricCoverage = deepAgentRubricCoverage(final.Checks)
		return verdict
	}
	verdict := deepAgentEvaluatorRepairVerdict(firstNonEmptyString(final.Reason, "final evaluator failed"), failed, "Repair the failed criteria and rerun final evaluation.")
	verdict.Checks = append([]DeepAgentVerificationCheck(nil), final.Checks...)
	verdict.Confidence = firstNonEmptyString(final.Confidence, "medium")
	verdict.RubricCoverage = deepAgentRubricCoverage(final.Checks)
	return verdict
}

func deepAgentVerdictFromTraceChecks(checks []DeepAgentVerificationCheck) DeepAgentEvaluatorVerdict {
	if len(checks) == 0 {
		return DeepAgentEvaluatorVerdict{}
	}
	failed := []string{}
	for _, check := range checks {
		if !check.Passed && strings.TrimSpace(check.Reason) != "" {
			failed = append(failed, check.Reason)
		}
	}
	if len(failed) > 0 {
		verdict := deepAgentEvaluatorRepairVerdict(strings.Join(failed, "; "), failed, "Repair the failed criteria and rerun final evaluation.")
		verdict.Checks = append([]DeepAgentVerificationCheck(nil), checks...)
		verdict.RubricCoverage = deepAgentRubricCoverage(checks)
		return verdict
	}
	verdict := deepAgentEvaluatorPassVerdict("all final evaluator checks passed", checks)
	verdict.RubricCoverage = deepAgentRubricCoverage(checks)
	return verdict
}

func deepAgentEvaluatorVerdictFromChecks(checks []DeepAgentVerificationCheck, nameParts ...string) (DeepAgentEvaluatorVerdict, bool) {
	matched := []DeepAgentVerificationCheck{}
	failed := []string{}
	for _, check := range checks {
		name := strings.ToLower(strings.TrimSpace(check.Name))
		if name == "" {
			continue
		}
		match := false
		for _, part := range nameParts {
			if strings.Contains(name, strings.ToLower(strings.TrimSpace(part))) {
				match = true
				break
			}
		}
		if !match {
			continue
		}
		matched = append(matched, check)
		if !check.Passed && strings.TrimSpace(check.Reason) != "" {
			failed = append(failed, check.Reason)
		}
	}
	if len(matched) == 0 {
		return DeepAgentEvaluatorVerdict{}, false
	}
	if len(failed) > 0 {
		verdict := deepAgentEvaluatorRepairVerdict(strings.Join(failed, "; "), failed, "Repair the failed verifier criteria and rerun final evaluation.")
		verdict.Checks = matched
		return verdict, true
	}
	verdict := deepAgentEvaluatorPassVerdict("verifier checks passed", matched)
	return verdict, true
}

func deepAgentEvaluatorPassVerdict(reason string, checks []DeepAgentVerificationCheck) DeepAgentEvaluatorVerdict {
	return DeepAgentEvaluatorVerdict{
		Verdict:    DeepAgentEvaluatorPass,
		Passed:     true,
		Reason:     reason,
		Confidence: "high",
		Checks:     append([]DeepAgentVerificationCheck(nil), checks...),
	}
}

func deepAgentEvaluatorRepairVerdict(reason string, failed []string, repair string) DeepAgentEvaluatorVerdict {
	repairPlan := []string{}
	if strings.TrimSpace(repair) != "" {
		repairPlan = append(repairPlan, repair)
	}
	return DeepAgentEvaluatorVerdict{
		Verdict:        DeepAgentEvaluatorNeedsRepair,
		Passed:         false,
		Reason:         reason,
		FailedCriteria: append([]string(nil), failed...),
		Confidence:     "high",
		RepairPlan:     repairPlan,
	}
}

func deepAgentEvaluatorReviewVerdict(reason string, failed []string, repair string) DeepAgentEvaluatorVerdict {
	verdict := deepAgentEvaluatorRepairVerdict(reason, failed, repair)
	verdict.Verdict = DeepAgentEvaluatorNeedsReview
	return verdict
}

func mergeDeepAgentEvaluatorVerdicts(base DeepAgentEvaluatorVerdict, others ...DeepAgentEvaluatorVerdict) DeepAgentEvaluatorVerdict {
	out := base
	for _, item := range others {
		if item.SourceCoverage != nil {
			out.SourceCoverage = item.SourceCoverage
		}
		if item.RubricCoverage != nil {
			out.RubricCoverage = item.RubricCoverage
		}
		if item.Passed {
			continue
		}
		out.Passed = false
		if out.Verdict == "" || out.Verdict == DeepAgentEvaluatorPass || item.Verdict == DeepAgentEvaluatorNeedsReview {
			out.Verdict = firstNonEmptyString(item.Verdict, DeepAgentEvaluatorNeedsRepair)
		}
		out.FailedCriteria = appendUniqueStrings(out.FailedCriteria, item.FailedCriteria)
		out.RepairPlan = appendUniqueStrings(out.RepairPlan, item.RepairPlan)
		if out.Reason == "" || base.Passed {
			out.Reason = item.Reason
		}
	}
	if out.Verdict == "" {
		out.Verdict = DeepAgentEvaluatorPass
	}
	out.Passed = out.Verdict == DeepAgentEvaluatorPass
	if !out.Passed && len(out.RepairPlan) == 0 {
		out.RepairPlan = []string{"Repair the failed criteria and rerun final evaluation."}
	}
	return out
}

func recordDeepAgentEvaluatorVerdict(state *DeepAgentState, verdict DeepAgentEvaluatorVerdict) {
	if state == nil {
		return
	}
	if state.WorkingMemory == nil {
		state.WorkingMemory = map[string]any{}
	}
	state.WorkingMemory["evaluator_verdict"] = map[string]any{
		"verdict":         verdict.Verdict,
		"passed":          verdict.Passed,
		"failed_criteria": append([]string(nil), verdict.FailedCriteria...),
		"confidence":      verdict.Confidence,
		"repair_plan":     append([]string(nil), verdict.RepairPlan...),
		"reason":          verdict.Reason,
		"checks":          append([]DeepAgentVerificationCheck(nil), verdict.Checks...),
		"source_coverage": cloneWorkflowMap(verdict.SourceCoverage),
		"rubric_coverage": cloneWorkflowMap(verdict.RubricCoverage),
	}
}

func deepAgentEvaluatorVerdictForSummary(state *DeepAgentState) map[string]any {
	if state == nil || state.WorkingMemory == nil {
		return nil
	}
	if raw, ok := state.WorkingMemory["evaluator_verdict"].(map[string]any); ok {
		return cloneWorkflowMap(raw)
	}
	return nil
}

func deepAgentRubricCoverage(checks []DeepAgentVerificationCheck) map[string]any {
	total := 0
	passed := 0
	failed := []string{}
	for _, check := range checks {
		if !strings.HasPrefix(check.Name, "rubric_") && check.Name != "content_verifier" {
			continue
		}
		total++
		if check.Passed {
			passed++
		} else if check.Reason != "" {
			failed = append(failed, check.Reason)
		}
	}
	return map[string]any{"total": total, "passed": passed, "failed": failed}
}

func deepAgentContractTaskNeedsResearch(contract LoopContract) bool {
	taskType := strings.ToLower(strings.TrimSpace(contract.TaskType))
	deliverable := strings.ToLower(strings.TrimSpace(contract.Deliverable.Type + " " + contract.Deliverable.Format))
	return strings.Contains(taskType, "research") || strings.Contains(taskType, "report") || strings.Contains(deliverable, "report")
}
