package agentruntime

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

var ErrDeepAgentPolicyBlocked = errors.New("deep agent action blocked by policy")

type DeepAgentLoopMetrics struct {
	TriggerType         string         `json:"trigger_type,omitempty"`
	TriggerSource       string         `json:"trigger_source,omitempty"`
	TemplateID          string         `json:"template_id,omitempty"`
	TaskType            string         `json:"task_type,omitempty"`
	DurationMS          int64          `json:"duration_ms,omitempty"`
	ActionCount         int            `json:"action_count"`
	NoProgressCount     int            `json:"no_progress_count"`
	CompletedCount      int            `json:"completed_count"`
	FailedCount         int            `json:"failed_count"`
	EvidenceCount       int            `json:"evidence_count"`
	ArtifactCount       int            `json:"artifact_count"`
	VerifierChecks      int            `json:"verifier_checks"`
	VerifierFailed      int            `json:"verifier_failed"`
	TokenEstimate       int            `json:"token_estimate,omitempty"`
	EstimatedCostUSD    float64        `json:"estimated_cost_usd,omitempty"`
	BlockedReason       string         `json:"blocked_reason,omitempty"`
	FinalStatus         string         `json:"final_status,omitempty"`
	Budget              map[string]any `json:"budget,omitempty"`
	SideEffectCount     int            `json:"side_effect_count,omitempty"`
	UserDataAccessCount int            `json:"user_data_access_count,omitempty"`
}

type DeepAgentTimelineItem struct {
	Kind       string         `json:"kind"`
	StepID     string         `json:"step_id,omitempty"`
	Title      string         `json:"title,omitempty"`
	Status     string         `json:"status,omitempty"`
	Tool       string         `json:"tool,omitempty"`
	ActionHash string         `json:"action_hash,omitempty"`
	Summary    string         `json:"summary,omitempty"`
	CreatedAt  time.Time      `json:"created_at,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

type DeepAgentGovernanceState struct {
	KillSwitch           bool                    `json:"kill_switch,omitempty"`
	AllowedHighRiskTools []string                `json:"allowed_high_risk_tools,omitempty"`
	PolicyBlocked        bool                    `json:"policy_blocked,omitempty"`
	PolicyBlockReason    string                  `json:"policy_block_reason,omitempty"`
	HighRiskPolicy       string                  `json:"high_risk_policy,omitempty"`
	SideEffectAudit      []DeepAgentTimelineItem `json:"side_effect_audit,omitempty"`
	UserDataAccessAudit  []DeepAgentTimelineItem `json:"user_data_access_audit,omitempty"`
}

type DeepAgentReplayReport struct {
	RunID             string                       `json:"run_id"`
	Goal              string                       `json:"goal,omitempty"`
	Status            string                       `json:"status,omitempty"`
	TaskType          string                       `json:"task_type,omitempty"`
	TriggerPayload    map[string]any               `json:"trigger_payload,omitempty"`
	PlannerDecisions  []DeepAgentTimelineItem      `json:"planner_decisions,omitempty"`
	RouterDecisions   []map[string]any             `json:"router_decisions,omitempty"`
	ExecutorDecisions []DeepAgentTimelineItem      `json:"executor_decisions,omitempty"`
	VerifierChecks    []DeepAgentVerificationCheck `json:"verifier_checks,omitempty"`
	Metrics           DeepAgentLoopMetrics         `json:"metrics,omitempty"`
	Findings          []EvaluationFinding          `json:"findings,omitempty"`
}

func deepAgentLoopMetricsForRun(run *WorkflowRun, state *DeepAgentState) DeepAgentLoopMetrics {
	if state == nil {
		return DeepAgentLoopMetrics{}
	}
	metrics := DeepAgentLoopMetrics{
		TriggerType:     deepAgentWorkflowString(state.WorkingMemory, "trigger_type"),
		TriggerSource:   deepAgentWorkflowString(state.WorkingMemory, "trigger_source"),
		TemplateID:      deepAgentTemplateID(state),
		TaskType:        deepAgentTaskType(state),
		ActionCount:     state.ActionCount,
		NoProgressCount: state.NoProgressCount,
		CompletedCount:  len(state.CompletedSteps),
		FailedCount:     len(state.FailedSteps),
		EvidenceCount:   len(deepAgentEvidenceForSummary(state)),
		ArtifactCount:   len(deepAgentStateCurrentArtifactRefs(state)),
		BlockedReason:   state.Blocker,
		FinalStatus:     state.Status,
		Budget:          deepAgentBudgetForMetrics(state),
	}
	if run != nil {
		start := firstNonZeroTime(run.StartedAt, state.StartedAt, run.CreatedAt)
		end := firstNonZeroTime(run.FinishedAt, state.UpdatedAt, run.UpdatedAt)
		if !start.IsZero() && !end.IsZero() && end.After(start) {
			metrics.DurationMS = end.Sub(start).Milliseconds()
		}
	}
	if final := deepAgentFinalVerifierForSummary(state); final != nil {
		if checks, ok := final["checks"].([]any); ok {
			metrics.VerifierChecks = len(checks)
			for _, item := range checks {
				record, _ := item.(map[string]any)
				if len(record) > 0 && !deepAgentBool(record, "passed", false) {
					metrics.VerifierFailed++
				}
			}
		}
	}
	if pack, ok := state.WorkingMemory[deepAgentEvidencePackKey].(DeepAgentEvidencePack); ok {
		metrics.TokenEstimate = pack.TokenEstimate
	} else if raw, ok := state.WorkingMemory[deepAgentEvidencePackKey].(map[string]any); ok {
		metrics.TokenEstimate = deepAgentAnyInt(raw["token_estimate"], 0)
	}
	metrics.SideEffectCount, metrics.UserDataAccessCount = deepAgentGovernanceAuditCounts(state)
	return metrics
}

func deepAgentTimelineForState(state *DeepAgentState) []DeepAgentTimelineItem {
	if state == nil {
		return nil
	}
	items := make([]DeepAgentTimelineItem, 0, len(state.Plan.Steps)+len(state.ActionHistory))
	for _, step := range state.Plan.Steps {
		items = append(items, DeepAgentTimelineItem{
			Kind:    "step",
			StepID:  step.ID,
			Title:   step.Title,
			Status:  step.Status,
			Summary: firstNonEmptyString(step.DoneCondition, step.Intent),
		})
	}
	for idx, action := range state.ActionHistory {
		items = append(items, DeepAgentTimelineItem{
			Kind:       "action",
			StepID:     action.StepID,
			Status:     "executed",
			Tool:       action.Tool,
			ActionHash: action.Hash,
			Summary:    firstNonEmptyString(deepAgentWorkflowString(action.Args, "prompt"), deepAgentWorkflowString(action.Args, "query"), action.Tool),
			CreatedAt:  state.StartedAt.Add(time.Duration(idx) * time.Millisecond),
			Metadata: map[string]any{
				"args": cloneWorkflowMap(action.Args),
			},
		})
	}
	for _, evidence := range deepAgentEvidenceForSummary(state) {
		items = append(items, DeepAgentTimelineItem{
			Kind:       "evidence",
			StepID:     firstNonEmptyString(evidence.StepID, evidence.Route.StepID),
			Status:     firstNonEmptyString(deepAgentWorkflowString(evidence.Diagnostics, "result_status"), "recorded"),
			Tool:       evidence.Route.Mode,
			ActionHash: evidence.ActionID,
			Summary:    firstNonEmptyString(evidence.Summary, evidence.Output),
			Metadata: map[string]any{
				"artifact_count": len(evidence.Artifacts),
				"source_count":   len(evidence.Sources),
				"error_class":    evidence.ErrorClass,
				"verified_by":    append([]string(nil), evidence.VerifiedBy...),
			},
		})
	}
	return items
}

func deepAgentGovernanceStateForRun(state *DeepAgentState) DeepAgentGovernanceState {
	if state == nil {
		return DeepAgentGovernanceState{}
	}
	config := deepAgentGovernanceConfig(state)
	governance := DeepAgentGovernanceState{
		KillSwitch:           config.KillSwitch,
		AllowedHighRiskTools: append([]string(nil), config.AllowedHighRiskTools...),
		HighRiskPolicy:       firstNonEmptyString(config.HighRiskPolicy, "review"),
	}
	if state.WorkingMemory != nil {
		if blocked, _ := state.WorkingMemory["governance_policy_block"].(map[string]any); len(blocked) > 0 {
			governance.PolicyBlocked = true
			governance.PolicyBlockReason = deepAgentWorkflowString(blocked, "reason")
		}
	}
	for _, evidence := range deepAgentEvidenceForSummary(state) {
		item := DeepAgentTimelineItem{
			Kind:       "governance",
			StepID:     firstNonEmptyString(evidence.StepID, evidence.Route.StepID),
			Tool:       evidence.Route.Mode,
			ActionHash: evidence.ActionID,
			Summary:    firstNonEmptyString(evidence.SideEffectLevel, evidence.Summary),
		}
		if strings.TrimSpace(evidence.SideEffectLevel) != "" && !strings.EqualFold(evidence.SideEffectLevel, "none") {
			governance.SideEffectAudit = append(governance.SideEffectAudit, item)
		}
		if deepAgentEvidenceAccessesUserData(evidence) {
			governance.UserDataAccessAudit = append(governance.UserDataAccessAudit, item)
		}
	}
	return governance
}

type deepAgentGovernanceSettings struct {
	KillSwitch           bool
	AllowedHighRiskTools []string
	HighRiskPolicy       string
}

func deepAgentGovernanceConfig(state *DeepAgentState) deepAgentGovernanceSettings {
	config := deepAgentGovernanceSettings{HighRiskPolicy: "review"}
	if state == nil || state.WorkingMemory == nil {
		return config
	}
	if deepAgentBool(state.WorkingMemory, "deep_agent_kill_switch", false) {
		config.KillSwitch = true
	}
	raw, _ := state.WorkingMemory["deep_agent_governance"].(map[string]any)
	if len(raw) == 0 {
		raw, _ = state.WorkingMemory["governance"].(map[string]any)
	}
	if len(raw) > 0 {
		config.KillSwitch = config.KillSwitch || deepAgentBool(raw, "kill_switch", false)
		config.AllowedHighRiskTools = deepAgentStringSlice(raw["allowed_high_risk_tools"])
		config.HighRiskPolicy = firstNonEmptyString(deepAgentWorkflowString(raw, "high_risk_policy"), config.HighRiskPolicy)
	}
	return config
}

func deepAgentCheckGovernancePolicy(state *DeepAgentState, action DeepAgentAction) error {
	config := deepAgentGovernanceConfig(state)
	if config.KillSwitch {
		return deepAgentPolicyBlocked(state, "deep agent kill switch is enabled", action)
	}
	if len(config.AllowedHighRiskTools) > 0 && !stringInSlice(action.Tool, config.AllowedHighRiskTools) {
		return deepAgentPolicyBlocked(state, fmt.Sprintf("high-risk tool %q is not allowed by DeepAgent governance policy", action.Tool), action)
	}
	return nil
}

func deepAgentPolicyBlocked(state *DeepAgentState, reason string, action DeepAgentAction) error {
	if state != nil {
		if state.WorkingMemory == nil {
			state.WorkingMemory = map[string]any{}
		}
		state.WorkingMemory["governance_policy_block"] = map[string]any{
			"reason":      reason,
			"tool":        action.Tool,
			"step_id":     action.StepID,
			"action_hash": action.Hash,
		}
	}
	return fmt.Errorf("%w: %s", ErrDeepAgentPolicyBlocked, reason)
}

func deepAgentReplayReportFromRun(run *WorkflowRun, state *DeepAgentState) DeepAgentReplayReport {
	if run == nil || state == nil {
		return DeepAgentReplayReport{}
	}
	report := DeepAgentReplayReport{
		RunID:            run.ID,
		Goal:             state.Goal,
		Status:           state.Status,
		TaskType:         deepAgentTaskType(state),
		TriggerPayload:   deepAgentTriggerPayload(state),
		PlannerDecisions: deepAgentPlannerTimeline(state),
		RouterDecisions:  deepAgentRoutesForSummary(state),
		Metrics:          deepAgentLoopMetricsForRun(run, state),
	}
	for _, item := range deepAgentTimelineForState(state) {
		if item.Kind == "action" || item.Kind == "evidence" {
			report.ExecutorDecisions = append(report.ExecutorDecisions, item)
		}
	}
	if final := deepAgentFinalVerifierForSummary(state); final != nil {
		if checks := deepAgentVerificationChecksFromAny(final["checks"]); len(checks) > 0 {
			report.VerifierChecks = checks
		}
	}
	report.Findings = deepAgentReplayFindings(report)
	return report
}

func deepAgentReplayFindings(report DeepAgentReplayReport) []EvaluationFinding {
	findings := make([]EvaluationFinding, 0)
	if report.Status == DeepAgentRunStatusBlocked || report.Status == DeepAgentRunStatusFailed || report.Status == DeepAgentRunStatusBudgetExceeded {
		findings = append(findings, EvaluationFinding{
			Severity: "error",
			Code:     "deep_agent_not_succeeded",
			Message:  firstNonEmptyString(report.Metrics.BlockedReason, "DeepAgent run did not succeed"),
		})
	}
	if report.Metrics.VerifierFailed > 0 {
		findings = append(findings, EvaluationFinding{
			Severity: "error",
			Code:     "deep_agent_verifier_failed",
			Message:  fmt.Sprintf("%d verifier check(s) failed", report.Metrics.VerifierFailed),
		})
	}
	if report.Metrics.NoProgressCount > 0 {
		findings = append(findings, EvaluationFinding{
			Severity: "warning",
			Code:     "deep_agent_no_progress",
			Message:  fmt.Sprintf("no-progress count reached %d", report.Metrics.NoProgressCount),
		})
	}
	return normalizeEvaluationFindings(findings)
}

func deepAgentPlannerTimeline(state *DeepAgentState) []DeepAgentTimelineItem {
	if state == nil {
		return nil
	}
	out := make([]DeepAgentTimelineItem, 0, len(state.Plan.Steps))
	for _, step := range state.Plan.Steps {
		out = append(out, DeepAgentTimelineItem{
			Kind:    "planner",
			StepID:  step.ID,
			Title:   step.Title,
			Status:  step.Status,
			Summary: firstNonEmptyString(step.Intent, step.DoneCondition),
			Metadata: map[string]any{
				"depends_on":  append([]string(nil), step.DependsOn...),
				"risk_level":  step.RiskLevel,
				"task_type":   deepAgentTaskType(state),
				"deliverable": deepAgentWorkflowString(state.WorkingMemory, "deliverable"),
			},
		})
	}
	return out
}

func deepAgentVerificationChecksFromAny(raw any) []DeepAgentVerificationCheck {
	values, ok := raw.([]any)
	if !ok {
		if typed, ok := raw.([]DeepAgentVerificationCheck); ok {
			return append([]DeepAgentVerificationCheck(nil), typed...)
		}
		return nil
	}
	out := make([]DeepAgentVerificationCheck, 0, len(values))
	for _, item := range values {
		record, _ := item.(map[string]any)
		if len(record) == 0 {
			continue
		}
		out = append(out, DeepAgentVerificationCheck{
			Name:   deepAgentWorkflowString(record, "name"),
			Passed: deepAgentBool(record, "passed", false),
			Reason: deepAgentWorkflowString(record, "reason"),
		})
	}
	return out
}

func deepAgentTaskType(state *DeepAgentState) string {
	if state == nil || state.WorkingMemory == nil {
		return ""
	}
	if value := deepAgentWorkflowString(state.WorkingMemory, "task_type"); value != "" {
		return value
	}
	return ""
}

func deepAgentTemplateID(state *DeepAgentState) string {
	if state == nil || state.WorkingMemory == nil {
		return ""
	}
	if value := deepAgentWorkflowString(state.WorkingMemory, "template_id"); value != "" {
		return normalizeDeepAgentTemplateID(value)
	}
	return ""
}

func deepAgentTriggerPayload(state *DeepAgentState) map[string]any {
	if state == nil || state.WorkingMemory == nil {
		return nil
	}
	if payload, ok := state.WorkingMemory["trigger_payload"].(map[string]any); ok {
		return cloneWorkflowMap(payload)
	}
	return nil
}

func deepAgentBudgetForMetrics(state *DeepAgentState) map[string]any {
	if state == nil || state.WorkingMemory == nil {
		return nil
	}
	if raw, ok := state.WorkingMemory["task_budget"].(map[string]any); ok {
		return cloneWorkflowMap(raw)
	}
	if raw, ok := state.WorkingMemory["resume_policy"].(map[string]any); ok {
		return cloneWorkflowMap(raw)
	}
	return nil
}

func deepAgentGovernanceAuditCounts(state *DeepAgentState) (int, int) {
	sideEffects := 0
	userData := 0
	for _, evidence := range deepAgentEvidenceForSummary(state) {
		if strings.TrimSpace(evidence.SideEffectLevel) != "" && !strings.EqualFold(evidence.SideEffectLevel, "none") {
			sideEffects++
		}
		if deepAgentEvidenceAccessesUserData(evidence) {
			userData++
		}
	}
	return sideEffects, userData
}

func deepAgentEvidenceAccessesUserData(evidence DeepAgentStepEvidence) bool {
	for _, key := range []string{"user_id", "session_id", "memory_item_id", "message_id"} {
		if deepAgentWorkflowString(evidence.Diagnostics, key) != "" {
			return true
		}
	}
	for _, tool := range evidence.ToolCalls {
		name := strings.ToLower(tool.Name)
		if strings.Contains(name, "memory") || strings.Contains(name, "message") || strings.Contains(name, "profile") {
			return true
		}
	}
	return false
}

func stringInSlice(value string, values []string) bool {
	value = strings.TrimSpace(value)
	for _, item := range values {
		if strings.EqualFold(strings.TrimSpace(item), value) {
			return true
		}
	}
	return false
}

func deepAgentMetricsMap(metrics DeepAgentLoopMetrics) map[string]any {
	out := map[string]any{
		"trigger_type":           metrics.TriggerType,
		"trigger_source":         metrics.TriggerSource,
		"task_type":              metrics.TaskType,
		"duration_ms":            metrics.DurationMS,
		"action_count":           metrics.ActionCount,
		"no_progress_count":      metrics.NoProgressCount,
		"completed_count":        metrics.CompletedCount,
		"failed_count":           metrics.FailedCount,
		"evidence_count":         metrics.EvidenceCount,
		"artifact_count":         metrics.ArtifactCount,
		"verifier_checks":        metrics.VerifierChecks,
		"verifier_failed":        metrics.VerifierFailed,
		"token_estimate":         metrics.TokenEstimate,
		"estimated_cost_usd":     metrics.EstimatedCostUSD,
		"blocked_reason":         metrics.BlockedReason,
		"final_status":           metrics.FinalStatus,
		"side_effect_count":      metrics.SideEffectCount,
		"user_data_access_count": metrics.UserDataAccessCount,
	}
	if len(metrics.Budget) > 0 {
		out["budget"] = cloneWorkflowMap(metrics.Budget)
	}
	return out
}

func deepAgentCommonBlockedReasons(runs []*WorkflowRun) []map[string]any {
	counts := map[string]int{}
	for _, run := range runs {
		state, err := deepAgentStateFromWorkflowRun(run)
		if err != nil || state == nil || strings.TrimSpace(state.Blocker) == "" {
			continue
		}
		counts[state.Blocker]++
	}
	type pair struct {
		reason string
		count  int
	}
	pairs := make([]pair, 0, len(counts))
	for reason, count := range counts {
		pairs = append(pairs, pair{reason: reason, count: count})
	}
	sort.SliceStable(pairs, func(i, j int) bool {
		if pairs[i].count == pairs[j].count {
			return pairs[i].reason < pairs[j].reason
		}
		return pairs[i].count > pairs[j].count
	})
	out := make([]map[string]any, 0, len(pairs))
	for _, item := range pairs {
		out = append(out, map[string]any{"reason": item.reason, "count": item.count})
	}
	return out
}

func deepAgentWorkflowListSummary(runs []*WorkflowRun) map[string]any {
	total := 0
	byTask := map[string]map[string]int{}
	byTemplate := map[string]map[string]int{}
	statusCounts := map[string]int{}
	for _, run := range runs {
		if run == nil || run.Name != deepAgentTaskWorkflowName {
			continue
		}
		state, err := deepAgentStateFromWorkflowRun(run)
		if err != nil || state == nil {
			continue
		}
		total++
		taskType := firstNonEmptyString(deepAgentTaskType(state), "unknown")
		templateID := firstNonEmptyString(deepAgentTemplateID(state), "unknown")
		if byTask[taskType] == nil {
			byTask[taskType] = map[string]int{}
		}
		if byTemplate[templateID] == nil {
			byTemplate[templateID] = map[string]int{}
		}
		status := firstNonEmptyString(state.Status, run.Status)
		byTask[taskType][status]++
		byTask[taskType]["total"]++
		byTemplate[templateID][status]++
		byTemplate[templateID]["total"]++
		statusCounts[status]++
	}
	taskRows := make([]map[string]any, 0, len(byTask))
	for taskType, counts := range byTask {
		succeeded := counts[DeepAgentRunStatusSucceeded]
		totalForTask := counts["total"]
		successRate := 0.0
		if totalForTask > 0 {
			successRate = float64(succeeded) / float64(totalForTask)
		}
		taskRows = append(taskRows, map[string]any{
			"task_type":    taskType,
			"total":        totalForTask,
			"succeeded":    succeeded,
			"blocked":      counts[DeepAgentRunStatusBlocked],
			"failed":       counts[DeepAgentRunStatusFailed],
			"review":       counts[DeepAgentRunStatusReviewPending],
			"budget":       counts[DeepAgentRunStatusBudgetExceeded],
			"success_rate": successRate,
		})
	}
	sort.SliceStable(taskRows, func(i, j int) bool {
		return fmt.Sprint(taskRows[i]["task_type"]) < fmt.Sprint(taskRows[j]["task_type"])
	})
	templateRows := make([]map[string]any, 0, len(byTemplate))
	for templateID, counts := range byTemplate {
		succeeded := counts[DeepAgentRunStatusSucceeded]
		totalForTemplate := counts["total"]
		successRate := 0.0
		if totalForTemplate > 0 {
			successRate = float64(succeeded) / float64(totalForTemplate)
		}
		templateRows = append(templateRows, map[string]any{
			"template_id":  templateID,
			"total":        totalForTemplate,
			"succeeded":    succeeded,
			"blocked":      counts[DeepAgentRunStatusBlocked],
			"failed":       counts[DeepAgentRunStatusFailed],
			"review":       counts[DeepAgentRunStatusReviewPending],
			"budget":       counts[DeepAgentRunStatusBudgetExceeded],
			"success_rate": successRate,
		})
	}
	sort.SliceStable(templateRows, func(i, j int) bool {
		return fmt.Sprint(templateRows[i]["template_id"]) < fmt.Sprint(templateRows[j]["template_id"])
	})
	return map[string]any{
		"total":                   total,
		"by_status":               statusCounts,
		"by_task_type":            taskRows,
		"by_template":             templateRows,
		"common_blocked_reasons":  deepAgentCommonBlockedReasons(runs),
		"subject_type":            EvaluationSubjectDeepAgent,
		"supports_eval_replay":    true,
		"supports_run_replay_api": true,
	}
}

func (r *Runtime) ReplayDeepAgentRun(ctx context.Context, runID string) (DeepAgentReplayReport, error) {
	run, err := r.GetWorkflowRun(ctx, runID)
	if err != nil {
		return DeepAgentReplayReport{}, err
	}
	if run == nil || run.Name != deepAgentTaskWorkflowName {
		return DeepAgentReplayReport{}, fmt.Errorf("workflow run %s is not a deep agent task", strings.TrimSpace(runID))
	}
	state, err := deepAgentStateFromWorkflowRun(run)
	if err != nil {
		return DeepAgentReplayReport{}, err
	}
	return deepAgentReplayReportFromRun(run, state), nil
}
