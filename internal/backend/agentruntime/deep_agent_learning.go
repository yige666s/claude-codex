package agentruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

const (
	deepAgentLearningTypeSuccessPath = "success_path"
	deepAgentLearningTypeBadcase     = "badcase"
	deepAgentLearningStatusCandidate = "candidate"
	deepAgentLearningStatusPending   = "pending"
	deepAgentLearningStatusAccepted  = "accepted"
	deepAgentLearningStatusRejected  = "rejected"
	deepAgentLearningStatusExpired   = "expired"
	deepAgentLearningStatusRollback  = "rolled_back"

	deepAgentLearningRiskLow        = "low"
	deepAgentLearningRiskMedium     = "medium"
	deepAgentLearningSensitivityLow = "low"
)

func (c *DeepAgentController) buildLearningCandidates(run *WorkflowRun, state *DeepAgentState) []DeepAgentLearningCandidate {
	if state == nil {
		return nil
	}
	runID := ""
	if run != nil {
		runID = run.ID
	}
	now := c.now()
	userID := deepAgentWorkflowString(state.WorkingMemory, "user_id")
	sessionID := deepAgentWorkflowString(state.WorkingMemory, "session_id")
	stepID := deepAgentLearningStepID(state)
	evidenceID := deepAgentLearningEvidenceID(runID, stepID, state)
	candidates := make([]DeepAgentLearningCandidate, 0, 2)
	if state.Status == DeepAgentRunStatusSucceeded && len(state.CompletedSteps) > 0 {
		candidates = append(candidates, DeepAgentLearningCandidate{
			ID:          newDeepAgentLearningID(),
			Type:        deepAgentLearningTypeSuccessPath,
			Status:      deepAgentLearningStatusCandidate,
			Source:      deepAgentTaskWorkflowName,
			UserID:      userID,
			SessionID:   sessionID,
			RunID:       runID,
			StepID:      stepID,
			EvidenceID:  evidenceID,
			RiskLevel:   deepAgentLearningRiskMedium,
			Sensitivity: deepAgentLearningSensitivityLow,
			Visibility:  MemoryVisibilityUser,
			Content:     deepAgentSuccessLearningContent(state),
			Metadata: map[string]any{
				"goal":            state.Goal,
				"completed_steps": state.CompletedSteps,
				"action_count":    state.ActionCount,
				"confidence":      0.72,
			},
			CreatedAt: now,
		})
	}
	if state.Status == DeepAgentRunStatusBlocked || state.Status == DeepAgentRunStatusBudgetExceeded || state.Status == DeepAgentRunStatusReviewPending {
		candidates = append(candidates, DeepAgentLearningCandidate{
			ID:          newDeepAgentLearningID(),
			Type:        deepAgentLearningTypeBadcase,
			Status:      deepAgentLearningStatusCandidate,
			Source:      deepAgentTaskWorkflowName,
			UserID:      userID,
			SessionID:   sessionID,
			RunID:       runID,
			StepID:      stepID,
			EvidenceID:  evidenceID,
			RiskLevel:   deepAgentLearningRiskMedium,
			Sensitivity: deepAgentLearningSensitivityLow,
			Visibility:  MemoryVisibilityUser,
			Content:     deepAgentBadcaseLearningContent(state),
			Metadata: map[string]any{
				"goal":              state.Goal,
				"status":            state.Status,
				"blocker":           state.Blocker,
				"failed_steps":      state.FailedSteps,
				"no_progress_count": state.NoProgressCount,
				"confidence":        0.68,
			},
			CreatedAt: now,
		})
	}
	return candidates
}

func (c *DeepAgentController) persistLearnings(ctx context.Context, run *WorkflowRun, state *DeepAgentState, candidates []DeepAgentLearningCandidate) error {
	if state == nil || len(candidates) == 0 {
		return nil
	}
	governed := make([]DeepAgentLearningCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		governed = append(governed, governDeepAgentLearningCandidate(candidate))
	}
	candidates = governed
	state.Learnings = append(state.Learnings, candidates...)
	if run != nil {
		if run.State == nil {
			run.State = map[string]any{}
		}
		run.State["deep_agent_learnings"] = state.Learnings
		run.State["deep_agent_learning_candidate_count"] = len(state.Learnings)
	}
	if c != nil && c.learningSink != nil {
		if err := c.learningSink.PersistDeepAgentLearnings(ctx, run, state, candidates); err != nil {
			return err
		}
	}
	c.persistState(ctx, run, state)
	return nil
}

func (c *DeepAgentController) persistFailedLearnings(ctx context.Context, run *WorkflowRun, state *DeepAgentState) {
	if state == nil {
		return
	}
	candidates := c.buildLearningCandidates(run, state)
	_ = c.persistLearnings(ctx, run, state, candidates)
}

func deepAgentSuccessLearningContent(state *DeepAgentState) string {
	steps := make([]string, 0, len(state.CompletedSteps))
	for _, step := range state.Plan.Steps {
		if step.Status == DeepAgentStepStatusSucceeded || containsString(state.CompletedSteps, step.ID) {
			steps = append(steps, firstNonEmptyString(step.Title, step.ID))
		}
	}
	return fmt.Sprintf("DeepAgent success path for goal %q: completed steps: %s.", state.Goal, strings.Join(steps, " -> "))
}

func deepAgentBadcaseLearningContent(state *DeepAgentState) string {
	return fmt.Sprintf("DeepAgent badcase for goal %q: status=%s, blocker=%s.", state.Goal, state.Status, firstNonEmptyString(state.Blocker, "unknown"))
}

func newDeepAgentLearningID() string {
	return "dal-" + newSortableID()
}

func containsString(values []string, value string) bool {
	for _, existing := range values {
		if existing == value {
			return true
		}
	}
	return false
}

type RuntimeDeepAgentLearningSink struct {
	runtime *Runtime
}

func NewRuntimeDeepAgentLearningSink(runtime *Runtime) *RuntimeDeepAgentLearningSink {
	return &RuntimeDeepAgentLearningSink{runtime: runtime}
}

func (s *RuntimeDeepAgentLearningSink) PersistDeepAgentLearnings(ctx context.Context, run *WorkflowRun, state *DeepAgentState, candidates []DeepAgentLearningCandidate) error {
	if s == nil || s.runtime == nil || len(candidates) == 0 {
		return nil
	}
	service, ok := s.runtime.memory.(MemoryItemService)
	if !ok || service == nil {
		return nil
	}
	for _, candidate := range candidates {
		candidate = governDeepAgentLearningCandidate(candidate)
		if candidate.Status == deepAgentLearningStatusRejected || candidate.Status == deepAgentLearningStatusExpired {
			continue
		}
		hash := deepAgentLearningCandidateHash(candidate)
		duplicates, err := service.ListMemoryItems(ctx, candidate.UserID, MemoryItemFilter{})
		if err == nil && deepAgentLearningDuplicateSeen(duplicates, hash) {
			continue
		}
		item := newConversationMemoryItem(candidate.UserID, candidate.SessionID, candidate.Content)
		item.ID = deepAgentLearningMemoryItemID(candidate.ID)
		item.Namespace = MemoryNamespaceDefault
		item.Level = MemoryLevelAtomic
		item.Category = MemoryCategoryEvent
		if candidate.Type == deepAgentLearningTypeSuccessPath {
			item.Category = MemoryCategorySkill
		} else if strings.EqualFold(candidate.Type, MemoryCategoryFact) {
			item.Category = MemoryCategoryFact
		} else if deepAgentLearningCandidateIsPreference(candidate) {
			item.Category = MemoryCategoryPreference
		}
		item.Source = MemorySourceSystem
		item.Visibility = firstNonEmptyString(candidate.Visibility, MemoryVisibilityUser)
		item.Status = MemoryStatusPendingConfirm
		if candidate.Status == deepAgentLearningStatusAccepted {
			item.Status = MemoryStatusActive
		}
		item.RawHash = hash
		item.Confidence = deepAgentLearningCandidateConfidence(candidate)
		item.Weight = 0.55
		item.Tags = []string{"deep_agent", candidate.Type, "candidate", candidate.Status}
		item.SourceRefs = deepAgentLearningSourceRefs(candidate)
		item.ExpiresAt = candidate.ExpiresAt
		item.Metadata = map[string]any{
			"deep_agent_learning":        true,
			"learning_id":                candidate.ID,
			"learning_type":              candidate.Type,
			"workflow_run_id":            candidate.RunID,
			"step_id":                    candidate.StepID,
			"evidence_id":                candidate.EvidenceID,
			"source":                     candidate.Source,
			"review_status":              candidate.Status,
			"review_required":            candidate.RequiresUserConfirmation,
			"user_confirmation_required": candidate.RequiresUserConfirmation,
			"user_confirmed":             candidate.UserConfirmed,
			"dedupe_hash":                hash,
			"gate_reason":                candidate.PolicyReason,
			"risk_level":                 candidate.RiskLevel,
			"sensitivity":                candidate.Sensitivity,
			"memory_level_target":        MemoryLevelAtomic,
			"memory_refinement_template": "memory_refinement",
			"l3_profile_allowed":         false,
		}
		for key, value := range candidate.Metadata {
			item.Metadata[key] = value
		}
		item.CreatedAt = firstNonZeroDeepAgentTime(candidate.CreatedAt, time.Now().UTC())
		item.UpdatedAt = item.CreatedAt
		if _, err := service.UpdateMemoryItem(ctx, candidate.UserID, item); err != nil {
			return err
		}
	}
	return nil
}

func deepAgentLearningCandidateGate(candidate DeepAgentLearningCandidate) (bool, string) {
	if strings.TrimSpace(candidate.UserID) == "" {
		return false, "missing user id"
	}
	if strings.TrimSpace(candidate.Content) == "" {
		return false, "empty content"
	}
	if deepAgentLearningCandidateConfidence(candidate) < 0.65 {
		return false, "confidence below threshold"
	}
	if deepAgentLearningContentLooksSensitive(candidate.Content) {
		return false, "sensitive content filtered"
	}
	return true, "pending user-visible review"
}

func governDeepAgentLearningCandidate(candidate DeepAgentLearningCandidate) DeepAgentLearningCandidate {
	candidate.MemoryItemID = deepAgentLearningMemoryItemID(candidate.ID)
	candidate.Visibility = firstNonEmptyString(candidate.Visibility, MemoryVisibilityUser)
	candidate.RiskLevel = firstNonEmptyString(candidate.RiskLevel, deepAgentLearningRiskMedium)
	candidate.Sensitivity = firstNonEmptyString(candidate.Sensitivity, deepAgentLearningSensitivityLow)
	if candidate.Metadata == nil {
		candidate.Metadata = map[string]any{}
	}
	approved, reason := deepAgentLearningCandidateGate(candidate)
	candidate.PolicyReason = reason
	if !approved {
		candidate.Status = deepAgentLearningStatusRejected
		candidate.RequiresUserConfirmation = false
		candidate.Metadata["review_status"] = candidate.Status
		candidate.Metadata["gate_reason"] = reason
		return candidate
	}
	if deepAgentLearningCanAutoWrite(candidate) {
		candidate.Status = deepAgentLearningStatusAccepted
		candidate.RequiresUserConfirmation = false
		candidate.UserConfirmed = false
		candidate.PolicyReason = "auto accepted low-risk user-visible factual memory"
	} else {
		candidate.Status = deepAgentLearningStatusPending
		candidate.RequiresUserConfirmation = true
	}
	candidate.Metadata["review_status"] = candidate.Status
	candidate.Metadata["gate_reason"] = candidate.PolicyReason
	candidate.Metadata["risk_level"] = candidate.RiskLevel
	candidate.Metadata["sensitivity"] = candidate.Sensitivity
	candidate.Metadata["visibility"] = candidate.Visibility
	candidate.Metadata["memory_item_id"] = candidate.MemoryItemID
	candidate.Metadata["requires_user_confirmation"] = candidate.RequiresUserConfirmation
	candidate.Metadata["user_confirmed"] = candidate.UserConfirmed
	return candidate
}

func deepAgentLearningCanAutoWrite(candidate DeepAgentLearningCandidate) bool {
	if !strings.EqualFold(candidate.Visibility, MemoryVisibilityUser) {
		return false
	}
	if !strings.EqualFold(candidate.RiskLevel, deepAgentLearningRiskLow) || !strings.EqualFold(candidate.Sensitivity, deepAgentLearningSensitivityLow) {
		return false
	}
	if deepAgentLearningCandidateIsPreference(candidate) {
		return false
	}
	return strings.EqualFold(candidate.Type, MemoryCategoryFact) || strings.EqualFold(deepAgentWorkflowString(candidate.Metadata, "memory_category"), MemoryCategoryFact)
}

func deepAgentLearningCandidateIsPreference(candidate DeepAgentLearningCandidate) bool {
	if strings.EqualFold(candidate.Type, MemoryCategoryPreference) || strings.Contains(strings.ToLower(candidate.Type), "preference") {
		return true
	}
	for _, key := range []string{"memory_category", "level", "memory_level", "preference_level"} {
		value := strings.ToLower(strings.TrimSpace(deepAgentWorkflowString(candidate.Metadata, key)))
		if value == MemoryCategoryPreference || value == MemoryLevelProfile || value == "l3" || value == "level_3" {
			return true
		}
	}
	return false
}

func deepAgentLearningMemoryItemID(candidateID string) string {
	candidateID = strings.TrimSpace(candidateID)
	if candidateID == "" {
		return ""
	}
	if strings.HasPrefix(candidateID, "mem_deep_agent_") {
		return candidateID
	}
	return "mem_deep_agent_" + candidateID
}

func deepAgentLearningSourceRefs(candidate DeepAgentLearningCandidate) []MemorySourceRef {
	refs := make([]MemorySourceRef, 0, 2)
	if strings.TrimSpace(candidate.RunID) != "" {
		refs = append(refs, MemorySourceRef{
			Kind:      "workflow_run",
			ID:        candidate.RunID,
			SessionID: candidate.SessionID,
		})
	}
	if strings.TrimSpace(candidate.EvidenceID) != "" {
		refs = append(refs, MemorySourceRef{
			Kind:      "deep_agent_evidence",
			ID:        candidate.EvidenceID,
			SessionID: candidate.SessionID,
			URI:       deepAgentLearningEvidenceURI(candidate),
		})
	}
	return refs
}

func deepAgentLearningStepID(state *DeepAgentState) string {
	if state == nil {
		return ""
	}
	for i := len(state.CompletedSteps) - 1; i >= 0; i-- {
		if strings.TrimSpace(state.CompletedSteps[i]) != "" {
			return state.CompletedSteps[i]
		}
	}
	if state.CurrentStepIndex >= 0 && state.CurrentStepIndex < len(state.Plan.Steps) {
		return state.Plan.Steps[state.CurrentStepIndex].ID
	}
	return ""
}

func deepAgentLearningEvidenceID(runID, stepID string, state *DeepAgentState) string {
	if state != nil {
		if raw := strings.TrimSpace(deepAgentWorkflowString(state.WorkingMemory, "final_evidence_id")); raw != "" {
			return raw
		}
		if raw := strings.TrimSpace(deepAgentWorkflowString(state.WorkingMemory, "evidence_id")); raw != "" {
			return raw
		}
	}
	parts := make([]string, 0, 2)
	if strings.TrimSpace(runID) != "" {
		parts = append(parts, "run:"+strings.TrimSpace(runID))
	}
	if strings.TrimSpace(stepID) != "" {
		parts = append(parts, "step:"+strings.TrimSpace(stepID))
	}
	return strings.Join(parts, "/")
}

func deepAgentLearningEvidenceURI(candidate DeepAgentLearningCandidate) string {
	if strings.TrimSpace(candidate.RunID) == "" {
		return ""
	}
	uri := "workflow://" + strings.TrimSpace(candidate.RunID)
	if strings.TrimSpace(candidate.StepID) != "" {
		uri += "/steps/" + strings.TrimSpace(candidate.StepID)
	}
	return uri
}

func deepAgentLearningCandidateConfidence(candidate DeepAgentLearningCandidate) float64 {
	if candidate.Metadata != nil {
		switch value := candidate.Metadata["confidence"].(type) {
		case float64:
			return value
		case float32:
			return float64(value)
		case int:
			return float64(value)
		case string:
			var parsed float64
			if _, err := fmt.Sscanf(strings.TrimSpace(value), "%f", &parsed); err == nil {
				return parsed
			}
		}
	}
	switch candidate.Type {
	case deepAgentLearningTypeBadcase:
		return 0.68
	case deepAgentLearningTypeSuccessPath:
		return 0.72
	default:
		return 0.65
	}
}

func deepAgentLearningCandidateHash(candidate DeepAgentLearningCandidate) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		strings.TrimSpace(candidate.UserID),
		strings.TrimSpace(candidate.SessionID),
		strings.TrimSpace(candidate.Type),
		strings.TrimSpace(candidate.Content),
	}, "\n")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func deepAgentLearningDuplicateSeen(items []MemoryItem, hash string) bool {
	hash = strings.TrimSpace(hash)
	if hash == "" {
		return false
	}
	for _, item := range items {
		if item.RawHash == hash {
			return true
		}
		if deepAgentWorkflowString(item.Metadata, "dedupe_hash") == hash {
			return true
		}
	}
	return false
}

func deepAgentLearningContentLooksSensitive(content string) bool {
	lower := strings.ToLower(content)
	return strings.Contains(lower, "api key") ||
		strings.Contains(lower, "password") ||
		strings.Contains(lower, "secret") ||
		strings.Contains(lower, "token=") ||
		strings.Contains(lower, "authorization:")
}

func firstNonZeroDeepAgentTime(values ...time.Time) time.Time {
	for _, value := range values {
		if !value.IsZero() {
			return value
		}
	}
	return time.Time{}
}
