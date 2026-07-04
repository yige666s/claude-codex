package agentruntime

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

func updateDeepAgentHandoff(state *DeepAgentState, now time.Time) LoopHandoff {
	if state == nil {
		return LoopHandoff{}
	}
	handoff := buildDeepAgentHandoff(state, now)
	if !loopHandoffEmpty(handoff) {
		state.Handoff = handoff
		if state.WorkingMemory == nil {
			state.WorkingMemory = map[string]any{}
		}
		state.WorkingMemory["loop_handoff"] = handoff
	}
	return handoff
}

func buildDeepAgentHandoff(state *DeepAgentState, now time.Time) LoopHandoff {
	if state == nil {
		return LoopHandoff{}
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	kind := deepAgentHandoffType(state)
	handoff := LoopHandoff{
		Type:              kind,
		ResumePoint:       deepAgentHandoffResumePoint(state),
		ResumeAvailable:   state.Status == DeepAgentRunStatusBlocked || state.Status == DeepAgentRunStatusFailed || state.Status == DeepAgentRunStatusBudgetExceeded || state.Status == DeepAgentRunStatusReviewPending,
		ReviewState:       deepAgentHandoffReviewState(state),
		BlockingReason:    state.Blocker,
		RecommendedAction: deepAgentHandoffRecommendedAction(state),
		UpdatedAt:         now.UTC(),
		Metadata: map[string]any{
			"completed_steps": len(state.CompletedSteps),
			"failed_steps":    len(state.FailedSteps),
			"action_count":    state.ActionCount,
		},
	}
	switch kind {
	case "workspace":
		handoff.Workspace = deepAgentWorkspaceHandoff(state)
	case "connector":
		handoff.Connector = deepAgentConnectorHandoff(state)
	default:
		handoff.Artifact = deepAgentArtifactHandoff(state)
	}
	handoff.Summary = deepAgentHandoffSummary(handoff)
	return handoff
}

func mergeDeepAgentHandoffPatch(base, patch LoopHandoff) LoopHandoff {
	if patch.Type != "" {
		base.Type = patch.Type
	}
	if patch.Summary != "" {
		base.Summary = patch.Summary
	}
	if patch.ResumePoint != "" {
		base.ResumePoint = patch.ResumePoint
	}
	if patch.ResumeAvailable {
		base.ResumeAvailable = true
	}
	if patch.Workspace.Repo != "" || patch.Workspace.Branch != "" || patch.Workspace.Worktree != "" || len(patch.Workspace.ChangedFiles) > 0 || len(patch.Workspace.TestCommands) > 0 || patch.Workspace.RollbackPlan != "" || patch.Workspace.BaseCommit != "" {
		base.Workspace = patch.Workspace
	}
	if len(patch.Artifact.SourceArtifacts) > 0 || patch.Artifact.DraftArtifact != nil || patch.Artifact.FinalArtifact != nil || patch.Artifact.ReviewState != "" {
		base.Artifact = patch.Artifact
	}
	if patch.Connector.Provider != "" || len(patch.Connector.Scopes) > 0 || patch.Connector.RiskLevel != "" || len(patch.Connector.PendingWriteActions) > 0 {
		base.Connector = patch.Connector
	}
	if patch.ReviewState != "" {
		base.ReviewState = patch.ReviewState
	}
	if patch.BlockingReason != "" {
		base.BlockingReason = patch.BlockingReason
	}
	if patch.RecommendedAction != "" {
		base.RecommendedAction = patch.RecommendedAction
	}
	if len(patch.Metadata) > 0 {
		if base.Metadata == nil {
			base.Metadata = map[string]any{}
		}
		for key, value := range patch.Metadata {
			base.Metadata[key] = value
		}
	}
	if !patch.UpdatedAt.IsZero() {
		base.UpdatedAt = patch.UpdatedAt
	}
	if base.Summary == "" {
		base.Summary = deepAgentHandoffSummary(base)
	}
	return base
}

func loopHandoffEmpty(handoff LoopHandoff) bool {
	return handoff.Type == "" && handoff.Summary == "" && handoff.ResumePoint == "" && !handoff.ResumeAvailable
}

func loopHandoffFromAny(raw any) LoopHandoff {
	if raw == nil {
		return LoopHandoff{}
	}
	if handoff, ok := raw.(LoopHandoff); ok {
		return handoff
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return LoopHandoff{}
	}
	var handoff LoopHandoff
	if err := json.Unmarshal(data, &handoff); err != nil {
		return LoopHandoff{}
	}
	return handoff
}

func deepAgentHandoffType(state *DeepAgentState) string {
	if state == nil {
		return ""
	}
	text := strings.ToLower(strings.Join([]string{
		state.Goal,
		state.LoopContract.TaskType,
		state.LoopContract.Deliverable.Type,
		state.LoopContract.Deliverable.Format,
	}, " "))
	for _, action := range state.ActionHistory {
		text += " " + strings.ToLower(action.Tool)
		if provider := deepAgentActionConnectorProvider(action); provider != "" {
			return "connector"
		}
		if normalizeDeepAgentRouteMode(action.Tool) == DeepAgentToolModeConnector {
			return "connector"
		}
		if normalizeDeepAgentRouteMode(action.Tool) == DeepAgentToolModeCodePatch {
			return "workspace"
		}
	}
	if deepAgentContainsAny(text, "code", "patch", "repo", "branch", "worktree", "test", "代码", "修复", "仓库") {
		return "workspace"
	}
	if deepAgentContainsAny(text, "connector", "mcp", "github", "gmail", "notion") {
		return "connector"
	}
	return "artifact"
}

func deepAgentHandoffResumePoint(state *DeepAgentState) string {
	if state == nil {
		return ""
	}
	if state.CurrentStepIndex >= 0 && state.CurrentStepIndex < len(state.Plan.Steps) {
		step := state.Plan.Steps[state.CurrentStepIndex]
		return strings.TrimSpace(fmt.Sprintf("%s: %s", step.ID, firstNonEmptyString(step.Title, step.Intent)))
	}
	if len(state.FailedSteps) > 0 {
		return "retry failed step " + state.FailedSteps[len(state.FailedSteps)-1]
	}
	if len(state.CompletedSteps) > 0 {
		return "continue after completed step " + state.CompletedSteps[len(state.CompletedSteps)-1]
	}
	return "start from current loop state"
}

func deepAgentWorkspaceHandoff(state *DeepAgentState) WorkspaceHandoff {
	handoff := WorkspaceHandoff{
		Repo:         deepAgentHandoffStringFromState(state, "repo", "repository", "workspace_repo"),
		Branch:       deepAgentHandoffStringFromState(state, "branch", "workspace_branch"),
		Worktree:     deepAgentHandoffStringFromState(state, "worktree", "workspace", "working_dir", "workspace_dir"),
		BaseCommit:   deepAgentHandoffStringFromState(state, "base_commit", "base_sha", "commit"),
		ChangedFiles: deepAgentHandoffStringSliceFromState(state, "changed_files"),
		TestCommands: deepAgentHandoffStringSliceFromState(state, "test_commands", "verification_commands"),
		RollbackPlan: deepAgentHandoffStringFromState(state, "rollback_plan"),
	}
	for _, evidence := range deepAgentEvidenceForSummary(state) {
		handoff.ChangedFiles = appendUniqueHandoffStrings(handoff.ChangedFiles, deepAgentStringSlice(evidence.Diagnostics["changed_files"])...)
		handoff.TestCommands = appendUniqueHandoffStrings(handoff.TestCommands, deepAgentStringSlice(evidence.Diagnostics["test_commands"])...)
		handoff.RollbackPlan = firstNonEmptyString(handoff.RollbackPlan, evidence.RollbackHint, deepAgentWorkflowString(evidence.Diagnostics, "rollback_plan"))
	}
	for _, action := range state.ActionHistory {
		handoff.ChangedFiles = appendUniqueHandoffStrings(handoff.ChangedFiles, deepAgentStringSlice(action.Args["changed_files"])...)
		handoff.TestCommands = appendUniqueHandoffStrings(handoff.TestCommands, deepAgentStringSlice(action.Args["test_commands"])...)
		handoff.Repo = firstNonEmptyString(handoff.Repo, deepAgentActionString(action, "repo"), deepAgentActionString(action, "repository"))
		handoff.Branch = firstNonEmptyString(handoff.Branch, deepAgentActionString(action, "branch"))
		handoff.Worktree = firstNonEmptyString(handoff.Worktree, deepAgentActionString(action, "worktree"), deepAgentActionString(action, "working_dir"))
		handoff.BaseCommit = firstNonEmptyString(handoff.BaseCommit, deepAgentActionString(action, "base_commit"), deepAgentActionString(action, "base_sha"))
		handoff.RollbackPlan = firstNonEmptyString(handoff.RollbackPlan, deepAgentActionString(action, "rollback_plan"))
	}
	if handoff.RollbackPlan == "" {
		handoff.RollbackPlan = "Revert or discard the listed changed files from the handoff before retrying."
	}
	return handoff
}

func deepAgentArtifactHandoff(state *DeepAgentState) ArtifactHandoff {
	artifacts := deepAgentStateCurrentArtifactRefs(state)
	handoff := ArtifactHandoff{
		SourceArtifacts: artifacts,
		ReviewState:     deepAgentHandoffReviewState(state),
	}
	if len(artifacts) > 0 {
		handoff.DraftArtifact = &artifacts[len(artifacts)-1]
		handoff.FinalArtifact = &artifacts[len(artifacts)-1]
	}
	return handoff
}

func deepAgentConnectorHandoff(state *DeepAgentState) ConnectorHandoff {
	handoff := ConnectorHandoff{
		Provider:  deepAgentHandoffStringFromState(state, "provider", "connector_provider"),
		Scopes:    deepAgentHandoffStringSliceFromState(state, "connector_context", "scopes"),
		RiskLevel: firstNonEmptyString(deepAgentHandoffStringFromState(state, "risk_level"), "medium"),
	}
	for _, action := range state.ActionHistory {
		if provider := deepAgentActionConnectorProvider(action); provider != "" {
			handoff.Provider = firstNonEmptyString(handoff.Provider, provider)
		}
		handoff.Scopes = appendUniqueHandoffStrings(handoff.Scopes, deepAgentStringSlice(action.Args["connector_context"])...)
		if deepAgentActionRequiresReview(action) || strings.EqualFold(deepAgentActionString(action, "side_effect_level"), deepAgentSideEffectWrite) {
			handoff.PendingWriteActions = append(handoff.PendingWriteActions, action)
		}
	}
	return handoff
}

func deepAgentActionRequiresReview(action DeepAgentAction) bool {
	if action.Args == nil {
		return false
	}
	return deepAgentBool(action.Args, "requires_review", false) ||
		deepAgentBool(action.Args, "review_required", false) ||
		strings.EqualFold(deepAgentActionString(action, "permission_policy"), "review_required")
}

func deepAgentHandoffReviewState(state *DeepAgentState) string {
	if state == nil {
		return ""
	}
	if state.Status == DeepAgentRunStatusReviewPending {
		return "review_pending"
	}
	if state.Status == DeepAgentRunStatusSucceeded {
		return "accepted"
	}
	if state.Status == DeepAgentRunStatusBlocked || state.Status == DeepAgentRunStatusFailed || state.Status == DeepAgentRunStatusBudgetExceeded {
		return "needs_resume"
	}
	return "in_progress"
}

func deepAgentHandoffRecommendedAction(state *DeepAgentState) string {
	if state == nil {
		return ""
	}
	recovery := deepAgentRecoveryStateForSummary(state)
	return recovery.RecommendedNext
}

func deepAgentHandoffSummary(handoff LoopHandoff) string {
	switch handoff.Type {
	case "workspace":
		return strings.TrimSpace(fmt.Sprintf("Workspace handoff at %s with %d changed file(s).", firstNonEmptyString(handoff.ResumePoint, "current step"), len(handoff.Workspace.ChangedFiles)))
	case "connector":
		return strings.TrimSpace(fmt.Sprintf("Connector handoff for %s with %d pending write action(s).", firstNonEmptyString(handoff.Connector.Provider, "connector"), len(handoff.Connector.PendingWriteActions)))
	case "artifact":
		fallthrough
	default:
		return strings.TrimSpace(fmt.Sprintf("Artifact handoff at %s with %d artifact(s).", firstNonEmptyString(handoff.ResumePoint, "current step"), len(handoff.Artifact.SourceArtifacts)))
	}
}

func deepAgentHandoffStringFromState(state *DeepAgentState, keys ...string) string {
	if state == nil || state.WorkingMemory == nil {
		return ""
	}
	for _, key := range keys {
		if value := deepAgentWorkflowString(state.WorkingMemory, key); value != "" {
			return value
		}
	}
	return ""
}

func deepAgentHandoffStringSliceFromState(state *DeepAgentState, keys ...string) []string {
	if state == nil || state.WorkingMemory == nil {
		return nil
	}
	var out []string
	for _, key := range keys {
		out = appendUniqueHandoffStrings(out, deepAgentStringSlice(state.WorkingMemory[key])...)
	}
	return out
}

func appendUniqueHandoffStrings(values []string, additions ...string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values)+len(additions))
	for _, value := range append(values, additions...) {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	return out
}
