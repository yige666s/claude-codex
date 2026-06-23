package agentruntime

import (
	"strings"
	"time"
)

type DeepAgentWorkflowSummary struct {
	Present        bool                         `json:"present"`
	Goal           string                       `json:"goal,omitempty"`
	Status         string                       `json:"status,omitempty"`
	Blocker        string                       `json:"blocker,omitempty"`
	Recovery       DeepAgentRecoveryState       `json:"recovery,omitempty"`
	FinalAnswer    DeepAgentFinalAnswerEvidence `json:"final_answer,omitempty"`
	Metrics        DeepAgentLoopMetrics         `json:"metrics,omitempty"`
	Timeline       []DeepAgentTimelineItem      `json:"timeline,omitempty"`
	Governance     DeepAgentGovernanceState     `json:"governance,omitempty"`
	CurrentStepID  string                       `json:"current_step_id,omitempty"`
	CurrentStep    *DeepAgentStep               `json:"current_step,omitempty"`
	Plan           DeepAgentPlan                `json:"plan,omitempty"`
	StepContext    map[string]any               `json:"step_context,omitempty"`
	Routes         []map[string]any             `json:"routes,omitempty"`
	Evidence       []DeepAgentStepEvidence      `json:"evidence,omitempty"`
	ArtifactRefs   []DeepAgentArtifactRef       `json:"artifact_refs,omitempty"`
	FinalVerifier  map[string]any               `json:"final_verifier,omitempty"`
	ActionHistory  []DeepAgentAction            `json:"action_history,omitempty"`
	Learnings      []DeepAgentLearningCandidate `json:"learnings,omitempty"`
	CompletedCount int                          `json:"completed_count"`
	FailedCount    int                          `json:"failed_count"`
	ActionCount    int                          `json:"action_count"`
	NoProgress     int                          `json:"no_progress_count"`
}

type DeepAgentRecoveryState struct {
	BlockedReason        string                `json:"blocked_reason,omitempty"`
	BlockedCategory      string                `json:"blocked_category,omitempty"`
	UserFacingReason     string                `json:"user_facing_reason,omitempty"`
	LastAction           *DeepAgentAction      `json:"last_action,omitempty"`
	MissingInfo          []string              `json:"missing_info,omitempty"`
	RecommendedNext      string                `json:"recommended_next_action,omitempty"`
	ResumeAvailable      bool                  `json:"resume_available"`
	ReviewPending        bool                  `json:"review_pending,omitempty"`
	BudgetExceeded       bool                  `json:"budget_exceeded,omitempty"`
	ReviewActionHash     string                `json:"review_action_hash,omitempty"`
	ReviewStepID         string                `json:"review_step_id,omitempty"`
	AdditionalBudgetHint DeepAgentResumeBudget `json:"additional_budget_hint,omitempty"`
}

type DeepAgentFinalAnswerEvidence struct {
	Artifacts       []DeepAgentArtifactRef          `json:"artifacts,omitempty"`
	Sources         []DeepAgentSourceRef            `json:"sources,omitempty"`
	Tests           []map[string]any                `json:"tests,omitempty"`
	KnownGaps       []string                        `json:"known_gaps,omitempty"`
	ResearchQuality *DeepAgentResearchQualityReport `json:"research_quality,omitempty"`
}

func DeepAgentSummaryFromWorkflowRun(run *WorkflowRun) (*DeepAgentWorkflowSummary, bool) {
	if run == nil || run.Name != deepAgentTaskWorkflowName {
		return nil, false
	}
	state, err := deepAgentStateFromWorkflowRun(run)
	if err != nil || state == nil {
		return &DeepAgentWorkflowSummary{Present: false}, true
	}
	summary := &DeepAgentWorkflowSummary{
		Present:        true,
		Goal:           state.Goal,
		Status:         state.Status,
		Blocker:        state.Blocker,
		Recovery:       deepAgentRecoveryStateForSummary(state),
		FinalAnswer:    deepAgentFinalAnswerEvidenceForSummary(state),
		Metrics:        deepAgentLoopMetricsForRun(run, state),
		Timeline:       deepAgentTimelineForState(state),
		Governance:     deepAgentGovernanceStateForRun(state),
		Plan:           state.Plan,
		StepContext:    deepAgentStepContextForSummary(state),
		Routes:         deepAgentRoutesForSummary(state),
		Evidence:       deepAgentEvidenceForSummary(state),
		ArtifactRefs:   deepAgentStateCurrentArtifactRefs(state),
		FinalVerifier:  deepAgentFinalVerifierForSummary(state),
		ActionHistory:  state.ActionHistory,
		Learnings:      state.Learnings,
		CompletedCount: len(state.CompletedSteps),
		FailedCount:    len(state.FailedSteps),
		ActionCount:    state.ActionCount,
		NoProgress:     state.NoProgressCount,
	}
	if state.CurrentStepIndex >= 0 && state.CurrentStepIndex < len(state.Plan.Steps) {
		step := state.Plan.Steps[state.CurrentStepIndex]
		summary.CurrentStepID = step.ID
		summary.CurrentStep = &step
	}
	return summary, true
}

func deepAgentRecoveryStateForSummary(state *DeepAgentState) DeepAgentRecoveryState {
	if state == nil {
		return DeepAgentRecoveryState{}
	}
	recovery := DeepAgentRecoveryState{
		BlockedReason:  state.Blocker,
		MissingInfo:    deepAgentMissingInfoForRecovery(state),
		ReviewPending:  state.Status == DeepAgentRunStatusReviewPending,
		BudgetExceeded: state.Status == DeepAgentRunStatusBudgetExceeded,
	}
	if len(state.ActionHistory) > 0 {
		last := state.ActionHistory[len(state.ActionHistory)-1]
		recovery.LastAction = &last
	}
	if state.WorkingMemory != nil {
		if pending, _ := state.WorkingMemory["pending_review_action"].(map[string]any); len(pending) > 0 {
			recovery.ReviewPending = true
			recovery.ReviewActionHash = deepAgentWorkflowString(pending, "action_hash")
			recovery.ReviewStepID = deepAgentWorkflowString(pending, "step_id")
			recovery.BlockedReason = firstNonEmptyString(recovery.BlockedReason, deepAgentWorkflowString(pending, "reason"))
		}
	}
	recovery.ResumeAvailable = recovery.ReviewPending ||
		recovery.BudgetExceeded ||
		state.Status == DeepAgentRunStatusBlocked ||
		state.Status == DeepAgentRunStatusFailed
	recovery.BlockedCategory = deepAgentBlockedCategory(state, recovery)
	recovery.UserFacingReason = deepAgentUserFacingBlockedReason(state, recovery)
	recovery.AdditionalBudgetHint = deepAgentAdditionalBudgetHint(state, recovery)
	recovery.RecommendedNext = deepAgentRecommendedNextAction(state, recovery)
	return recovery
}

func deepAgentMissingInfoForRecovery(state *DeepAgentState) []string {
	if state == nil || state.WorkingMemory == nil {
		return nil
	}
	if raw, ok := state.WorkingMemory["final_verification"].(map[string]any); ok {
		if missing := deepAgentStringSlice(raw["missing"]); len(missing) > 0 {
			return missing
		}
	}
	if strings.TrimSpace(state.Blocker) == "" {
		return nil
	}
	if deepAgentMissingSourceEvidenceReason(state.Blocker) {
		return []string{"source evidence for the current research step"}
	}
	if strings.Contains(strings.ToLower(state.Blocker), "unmet dependencies") {
		return []string{"completed dependency step evidence"}
	}
	return []string{state.Blocker}
}

func deepAgentAdditionalBudgetHint(state *DeepAgentState, recovery DeepAgentRecoveryState) DeepAgentResumeBudget {
	if state == nil || !recovery.BudgetExceeded {
		return DeepAgentResumeBudget{}
	}
	if strings.Contains(strings.ToLower(state.Blocker), "duration") {
		return DeepAgentResumeBudget{MaxDurationMS: int64((5 * time.Minute).Milliseconds())}
	}
	return DeepAgentResumeBudget{MaxActions: 4}
}

func deepAgentRecommendedNextAction(state *DeepAgentState, recovery DeepAgentRecoveryState) string {
	if recovery.ReviewPending {
		return "Resolve the pending risk review with review_decision=approve, reject, or edit, then resume this workflow."
	}
	if recovery.BudgetExceeded {
		return "Resume with additional_budget.max_actions or additional_budget.max_duration_ms so action count and duration audit continue from the existing run."
	}
	blocker := strings.ToLower(firstNonEmptyString(state.Blocker, recovery.BlockedReason))
	switch {
	case deepAgentMissingSourceEvidenceReason(blocker):
		return "Provide source evidence or adjust the research step in state_patch, then resume."
	case strings.Contains(blocker, "unmet dependencies"):
		return "Complete, reset, or patch the unmet dependency steps before resuming."
	case strings.TrimSpace(blocker) != "":
		return "Provide the missing information in state_patch and resume this workflow."
	default:
		return ""
	}
}

func deepAgentBlockedCategory(state *DeepAgentState, recovery DeepAgentRecoveryState) string {
	if recovery.ReviewPending || (state != nil && state.Status == DeepAgentRunStatusReviewPending) {
		return "waiting_review"
	}
	if recovery.BudgetExceeded || (state != nil && state.Status == DeepAgentRunStatusBudgetExceeded) {
		return "budget_exhausted"
	}
	blocker := strings.ToLower(firstNonEmptyString(recovery.BlockedReason, func() string {
		if state != nil {
			return state.Blocker
		}
		return ""
	}()))
	switch {
	case blocker == "":
		return ""
	case deepAgentMissingSourceEvidenceReason(blocker),
		strings.Contains(blocker, "missing"),
		strings.Contains(blocker, "required"),
		strings.Contains(blocker, "provide"):
		return "missing_user_info"
	default:
		return "technical"
	}
}

func deepAgentUserFacingBlockedReason(state *DeepAgentState, recovery DeepAgentRecoveryState) string {
	switch deepAgentBlockedCategory(state, recovery) {
	case "waiting_review":
		return "等待人工 review。你可以批准、拒绝，或编辑这一步高风险动作后继续。"
	case "budget_exhausted":
		return "本次 loop 已达到预算限制。你可以追加 action 或时间预算后继续。"
	case "missing_user_info":
		if len(recovery.MissingInfo) > 0 {
			return "缺少继续执行所需的信息：" + strings.Join(recovery.MissingInfo, "、")
		}
		return "缺少继续执行所需的信息。补充后可以恢复 loop。"
	case "technical":
		return firstNonEmptyString(strings.TrimSpace(recovery.BlockedReason), "执行遇到技术阻塞，需要调整后恢复。")
	default:
		return ""
	}
}

func deepAgentFinalAnswerEvidenceForSummary(state *DeepAgentState) DeepAgentFinalAnswerEvidence {
	if state == nil {
		return DeepAgentFinalAnswerEvidence{}
	}
	final := DeepAgentFinalAnswerEvidence{
		Artifacts: deepAgentStateCurrentArtifactRefs(state),
	}
	sourceSeen := map[string]struct{}{}
	for _, evidence := range deepAgentEvidenceForSummary(state) {
		for _, source := range evidence.Sources {
			key := firstNonEmptyString(source.URL, source.Title, source.Snippet)
			if strings.TrimSpace(key) == "" {
				continue
			}
			if _, ok := sourceSeen[key]; ok {
				continue
			}
			sourceSeen[key] = struct{}{}
			final.Sources = append(final.Sources, source)
		}
		if test := deepAgentEvidenceTestSummary(evidence); len(test) > 0 {
			final.Tests = append(final.Tests, test)
		}
		if gap := deepAgentEvidenceKnownGap(evidence); gap != "" {
			final.KnownGaps = appendUniqueString(final.KnownGaps, gap)
		}
	}
	if verifier := deepAgentFinalVerifierForSummary(state); len(verifier) > 0 {
		if quality, ok := deepAgentResearchQualityFromAny(verifier["research_quality"]); ok {
			final.ResearchQuality = quality
		}
		if reason := strings.TrimSpace(deepAgentWorkflowString(verifier, "reason")); reason != "" && !deepAgentBool(verifier, "done", false) {
			final.KnownGaps = appendUniqueString(final.KnownGaps, reason)
		}
		for _, check := range deepAgentVerificationChecksFromAny(verifier["checks"]) {
			if !check.Passed && strings.TrimSpace(check.Reason) != "" {
				final.KnownGaps = appendUniqueString(final.KnownGaps, check.Reason)
			}
		}
	}
	return final
}

func deepAgentEvidenceTestSummary(evidence DeepAgentStepEvidence) map[string]any {
	if len(evidence.Diagnostics) == 0 {
		return nil
	}
	command := firstNonEmptyString(
		deepAgentWorkflowString(evidence.Diagnostics, "command"),
		deepAgentWorkflowString(evidence.Diagnostics, "test_command"),
	)
	status := firstNonEmptyString(
		deepAgentWorkflowString(evidence.Diagnostics, "result_status"),
		deepAgentWorkflowString(evidence.Diagnostics, "status"),
	)
	exitCode := deepAgentAnyInt(evidence.Diagnostics["exit_code"], 0)
	if command == "" && status == "" && evidence.Diagnostics["exit_code"] == nil {
		return nil
	}
	out := map[string]any{
		"step_id": evidence.StepID,
	}
	if command != "" {
		out["command"] = command
	}
	if status != "" {
		out["status"] = status
	}
	if evidence.Diagnostics["exit_code"] != nil {
		out["exit_code"] = exitCode
	}
	if excerpt := firstNonEmptyString(deepAgentWorkflowString(evidence.Diagnostics, "failure_excerpt"), deepAgentWorkflowString(evidence.Diagnostics, "stdout_stderr_excerpt")); excerpt != "" {
		out["excerpt"] = excerpt
	}
	return out
}

func deepAgentEvidenceKnownGap(evidence DeepAgentStepEvidence) string {
	if len(evidence.Diagnostics) == 0 {
		return ""
	}
	return firstNonEmptyString(
		deepAgentWorkflowString(evidence.Diagnostics, "not_tested"),
		deepAgentWorkflowString(evidence.Diagnostics, "not_tested_reason"),
		deepAgentWorkflowString(evidence.Diagnostics, "known_gap"),
	)
}

func deepAgentStepContextForSummary(state *DeepAgentState) map[string]any {
	if state == nil || state.WorkingMemory == nil {
		return nil
	}
	store, _ := state.WorkingMemory["step_context"].(map[string]any)
	return cloneWorkflowMap(store)
}

func deepAgentFinalVerifierForSummary(state *DeepAgentState) map[string]any {
	if state == nil || state.WorkingMemory == nil {
		return nil
	}
	if raw, ok := state.WorkingMemory["final_verification"].(map[string]any); ok {
		return cloneWorkflowMap(raw)
	}
	return nil
}

func deepAgentRoutesForSummary(state *DeepAgentState) []map[string]any {
	if state == nil {
		return nil
	}
	out := make([]map[string]any, 0, len(state.ActionHistory))
	for _, action := range state.ActionHistory {
		route, ok := deepAgentStepRouteFromMap(action.Args)
		if !ok {
			continue
		}
		item := deepAgentStepRouteMap(route)
		item["action_hash"] = action.Hash
		item["action_tool"] = action.Tool
		out = append(out, item)
	}
	return out
}

func deepAgentEvidenceForSummary(state *DeepAgentState) []DeepAgentStepEvidence {
	if state == nil || state.WorkingMemory == nil {
		return nil
	}
	out := (StateDeepAgentEvidenceStore{}).ListStepEvidence(state)
	seen := map[string]struct{}{}
	for _, evidence := range out {
		key := firstNonEmptyString(evidence.ActionID, evidence.StepID, evidence.Route.StepID)
		if key != "" {
			seen[key] = struct{}{}
		}
	}
	store, _ := state.WorkingMemory["step_context"].(map[string]any)
	for _, raw := range store {
		record, _ := raw.(map[string]any)
		if len(record) == 0 {
			continue
		}
		if metadata, _ := record["metadata"].(map[string]any); len(metadata) > 0 {
			if evidence, ok := deepAgentStepEvidenceFromAny(metadata["step_evidence"]); ok {
				key := firstNonEmptyString(evidence.ActionID, evidence.StepID, evidence.Route.StepID)
				if key != "" {
					if _, ok := seen[key]; ok {
						continue
					}
					seen[key] = struct{}{}
				}
				out = append(out, evidence)
			}
		}
		if evidence, ok := deepAgentStepEvidenceFromAny(record["step_evidence"]); ok {
			key := firstNonEmptyString(evidence.ActionID, evidence.StepID, evidence.Route.StepID)
			if key != "" {
				if _, ok := seen[key]; ok {
					continue
				}
				seen[key] = struct{}{}
			}
			out = append(out, evidence)
		}
	}
	return out
}
