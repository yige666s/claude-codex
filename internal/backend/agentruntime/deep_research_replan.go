package agentruntime

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

const (
	DeepResearchReplanReasonRequiredFailure  = "required_failure"
	DeepResearchReplanReasonEvidenceGap      = "evidence_gap"
	DeepResearchReplanReasonSchedulerStalled = "scheduler_stalled"
	DeepResearchReplanReasonBatchCompleted   = "batch_completed"
)

// DeepResearchReplan is a complete replacement for the unfinished portion of
// a research plan. Successfully completed nodes are retained by the runtime and
// do not need to be repeated by the model.
type DeepResearchReplan struct {
	Revision int              `json:"revision"`
	Reason   string           `json:"reason"`
	Plan     DeepResearchPlan `json:"plan"`
}

func shouldDeepResearchReplan(run DeepResearchRunState) (string, bool) {
	return shouldDeepResearchReplanForConfig(run, DeepResearchRuntimeConfig{RequireSources: true})
}

func shouldDeepResearchReplanForConfig(run DeepResearchRunState, cfg DeepResearchRuntimeConfig) (string, bool) {
	for _, node := range run.WorkerRuns {
		if node.Required && (node.Status == DeepResearchTaskStatusFailedFinal || node.Status == DeepResearchTaskStatusBlockedByDependency) {
			return DeepResearchReplanReasonRequiredFailure, true
		}
	}
	if !allDeepResearchTasksTerminal(run.WorkerRuns) {
		return "", false
	}
	hasSuccessfulWorker := false
	hasTrustedSource := false
	hasOpenQuestion := false
	for _, node := range run.WorkerRuns {
		if node.Status != DeepResearchTaskStatusSucceeded || node.Result == nil {
			continue
		}
		hasSuccessfulWorker = true
		hasOpenQuestion = hasOpenQuestion || len(node.Result.OpenQuestions) > 0
		hasTrustedSource = hasTrustedSource || len(deepResearchTrustedSources(*node.Result)) > 0
	}
	if hasSuccessfulWorker && !hasTrustedSource && (cfg.RequireSources || hasOpenQuestion) {
		return DeepResearchReplanReasonEvidenceGap, true
	}
	return "", false
}

func applyDeepResearchReplan(run *DeepResearchRunState, proposal DeepResearchReplan, allowedTools map[string]string) error {
	if run == nil {
		return fmt.Errorf("deep research run state is required")
	}
	if strings.TrimSpace(proposal.Plan.Goal) == "" {
		proposal.Plan.Goal = run.Goal
	}
	if strings.TrimSpace(proposal.Plan.Goal) != strings.TrimSpace(run.Goal) {
		return fmt.Errorf("deep research replan cannot change the goal")
	}
	if proposal.Revision <= run.PlanRevision {
		return fmt.Errorf("deep research replan revision %d must be greater than current revision %d", proposal.Revision, run.PlanRevision)
	}

	oldDefinitions := make(map[string]DeepResearchTaskNode, len(run.Plan.Nodes))
	for _, node := range run.Plan.Nodes {
		oldDefinitions[node.ID] = node
	}
	candidateByID := make(map[string]DeepResearchTaskNode, len(proposal.Plan.Nodes))
	for _, node := range proposal.Plan.Nodes {
		if _, exists := candidateByID[node.ID]; exists {
			return fmt.Errorf("duplicate deep research node id: %s", node.ID)
		}
		candidateByID[node.ID] = node
	}

	// A replan may omit completed history, but it may never rewrite it. Retain
	// successful nodes in the active graph because future nodes may depend on
	// their evidence.
	frozen := make([]DeepResearchTaskNode, 0, len(run.WorkerRuns))
	frozenIDs := make(map[string]bool, len(run.WorkerRuns))
	for _, oldPlanNode := range run.Plan.Nodes {
		current, ok := run.WorkerRuns[oldPlanNode.ID]
		if !ok || current.Status != DeepResearchTaskStatusSucceeded {
			continue
		}
		if candidate, included := candidateByID[current.ID]; included {
			if !sameDeepResearchNodeDefinition(oldPlanNode, candidate) {
				return fmt.Errorf("deep research replan cannot mutate completed node %s", current.ID)
			}
			delete(candidateByID, current.ID)
		}
		frozen = append(frozen, oldPlanNode)
		frozenIDs[oldPlanNode.ID] = true
	}

	merged := DeepResearchPlan{
		Goal:           run.Goal,
		MaxConcurrency: proposal.Plan.MaxConcurrency,
		Nodes:          make([]DeepResearchTaskNode, 0, len(frozen)+len(candidateByID)),
	}
	merged.Nodes = append(merged.Nodes, frozen...)
	for _, node := range proposal.Plan.Nodes {
		if _, retained := candidateByID[node.ID]; retained {
			merged.Nodes = append(merged.Nodes, node)
		}
	}
	if err := validateDeepResearchPlan(merged); err != nil {
		return err
	}
	canonicalTools := normalizeDeepResearchAllowedTools(allowedTools)
	for idx := range merged.Nodes {
		if frozenIDs[merged.Nodes[idx].ID] {
			continue
		}
		if len(merged.Nodes[idx].AllowedTools) == 0 {
			return fmt.Errorf("deep research node %s requires at least one allowed tool", merged.Nodes[idx].ID)
		}
		one := DeepResearchPlan{Goal: merged.Goal, MaxConcurrency: merged.MaxConcurrency, Nodes: []DeepResearchTaskNode{merged.Nodes[idx]}}
		if err := canonicalizeDeepResearchPlanAllowedTools(&one, canonicalTools); err != nil {
			return err
		}
		merged.Nodes[idx].AllowedTools = one.Nodes[0].AllowedTools
	}

	nextRuns := make(map[string]DeepResearchTaskNode, len(merged.Nodes))
	for idx := range merged.Nodes {
		definition := merged.Nodes[idx]
		current, existed := run.WorkerRuns[definition.ID]
		oldDefinition, hadDefinition := oldDefinitions[definition.ID]
		if existed && current.Status == DeepResearchTaskStatusSucceeded {
			definition = copyDeepResearchRuntimeFields(definition, current)
		} else if existed && current.Attempt > 0 && hadDefinition && sameDeepResearchNodeDefinition(oldDefinition, definition) {
			definition = copyDeepResearchRuntimeFields(definition, current)
		} else {
			definition = resetDeepResearchNodeRuntime(definition)
		}
		merged.Nodes[idx] = definition
		nextRuns[definition.ID] = definition
	}

	run.Plan = merged
	run.WorkerRuns = nextRuns
	run.PlanRevision = proposal.Revision
	run.LastReplanReason = proposal.Reason
	return nil
}

func normalizeDeepResearchAllowedTools(allowedTools map[string]string) map[string]string {
	out := make(map[string]string, len(allowedTools))
	for key, value := range allowedTools {
		canonical := strings.TrimSpace(value)
		if canonical == "" {
			canonical = strings.TrimSpace(key)
		}
		out[strings.ToLower(strings.TrimSpace(key))] = canonical
		out[strings.ToLower(canonical)] = canonical
	}
	return out
}

func sameDeepResearchNodeDefinition(left, right DeepResearchTaskNode) bool {
	return left.ID == right.ID &&
		strings.TrimSpace(left.Title) == strings.TrimSpace(right.Title) &&
		strings.TrimSpace(left.Description) == strings.TrimSpace(right.Description) &&
		strings.TrimSpace(left.WorkerRole) == strings.TrimSpace(right.WorkerRole) &&
		strings.TrimSpace(left.ExpectedOutput) == strings.TrimSpace(right.ExpectedOutput) &&
		left.Required == right.Required &&
		slices.Equal(left.DependsOn, right.DependsOn) &&
		equalFoldedStringSlices(left.AllowedTools, right.AllowedTools)
}

func equalFoldedStringSlices(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for idx := range left {
		if !strings.EqualFold(strings.TrimSpace(left[idx]), strings.TrimSpace(right[idx])) {
			return false
		}
	}
	return true
}

func resetDeepResearchNodeRuntime(node DeepResearchTaskNode) DeepResearchTaskNode {
	node.Status = DeepResearchTaskStatusPending
	node.Attempt = 0
	node.StartedAt = nil
	node.CompletedAt = nil
	node.AgentRunID = ""
	node.Result = nil
	node.Error = ""
	node.BlockedBy = nil
	node.LastHeartbeatAt = nil
	return node
}

func copyDeepResearchRuntimeFields(definition, runtime DeepResearchTaskNode) DeepResearchTaskNode {
	definition.Status = runtime.Status
	definition.Attempt = runtime.Attempt
	definition.StartedAt = runtime.StartedAt
	definition.CompletedAt = runtime.CompletedAt
	definition.AgentRunID = runtime.AgentRunID
	definition.Result = runtime.Result
	definition.Error = runtime.Error
	definition.BlockedBy = append([]string(nil), runtime.BlockedBy...)
	definition.LastHeartbeatAt = runtime.LastHeartbeatAt
	return definition
}

func equivalentDeepResearchPlans(left, right DeepResearchPlan) bool {
	if strings.TrimSpace(left.Goal) != strings.TrimSpace(right.Goal) || left.MaxConcurrency != right.MaxConcurrency || len(left.Nodes) != len(right.Nodes) {
		return false
	}
	rightByID := make(map[string]DeepResearchTaskNode, len(right.Nodes))
	for _, node := range right.Nodes {
		rightByID[node.ID] = node
	}
	for _, node := range left.Nodes {
		candidate, ok := rightByID[node.ID]
		if !ok || !sameDeepResearchNodeDefinition(node, candidate) {
			return false
		}
	}
	return true
}

func deepResearchHasRetryableFailure(nodes map[string]DeepResearchTaskNode) bool {
	for _, node := range nodes {
		if node.Status == DeepResearchTaskStatusPending && node.Attempt > 0 {
			return true
		}
	}
	return false
}

func deepResearchHasUnfinishedNode(nodes map[string]DeepResearchTaskNode) bool {
	for _, node := range nodes {
		switch node.Status {
		case DeepResearchTaskStatusSucceeded, DeepResearchTaskStatusFailedFinal, DeepResearchTaskStatusBlockedByDependency, DeepResearchTaskStatusSkipped, DeepResearchTaskStatusCancelled:
		default:
			return true
		}
	}
	return false
}

func deepResearchReplanTriggerDescription(reason string, run DeepResearchRunState) string {
	switch reason {
	case DeepResearchReplanReasonRequiredFailure:
		return "a required worker exhausted retries or became blocked by a failed dependency"
	case DeepResearchReplanReasonEvidenceGap:
		return "the completed graph still has unresolved questions without trusted source evidence"
	case DeepResearchReplanReasonSchedulerStalled:
		return "the scheduler has unfinished tasks but no runnable frontier"
	case DeepResearchReplanReasonBatchCompleted:
		return "a worker batch completed and new execution evidence is available"
	default:
		return firstNonEmptyString(strings.TrimSpace(reason), strings.TrimSpace(run.LastReplanReason), "execution checkpoint")
	}
}

func deepResearchReplanTriggerNodeIDs(reason string, run DeepResearchRunState) []string {
	ids := make([]string, 0)
	for id, node := range run.WorkerRuns {
		include := false
		switch reason {
		case DeepResearchReplanReasonRequiredFailure:
			include = node.Required && (node.Status == DeepResearchTaskStatusFailedFinal || node.Status == DeepResearchTaskStatusBlockedByDependency)
		case DeepResearchReplanReasonEvidenceGap:
			include = node.Status == DeepResearchTaskStatusSucceeded && node.Result != nil && len(node.Result.OpenQuestions) > 0
		case DeepResearchReplanReasonSchedulerStalled:
			include = node.Status == DeepResearchTaskStatusPending || node.Status == DeepResearchTaskStatusReady
		case DeepResearchReplanReasonBatchCompleted:
			include = node.CompletedAt != nil
		}
		if include {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}
