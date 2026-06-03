package agentruntime

import (
	"context"
	"fmt"
	"strings"
	"time"
)

const (
	deepAgentLearningTypeSuccessPath = "success_path"
	deepAgentLearningTypeBadcase     = "badcase"
	deepAgentLearningStatusCandidate = "candidate"
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
	candidates := make([]DeepAgentLearningCandidate, 0, 2)
	if state.Status == DeepAgentRunStatusSucceeded && len(state.CompletedSteps) > 0 {
		candidates = append(candidates, DeepAgentLearningCandidate{
			ID:        newDeepAgentLearningID(),
			Type:      deepAgentLearningTypeSuccessPath,
			Status:    deepAgentLearningStatusCandidate,
			Source:    deepAgentTaskWorkflowName,
			UserID:    userID,
			SessionID: sessionID,
			RunID:     runID,
			Content:   deepAgentSuccessLearningContent(state),
			Metadata: map[string]any{
				"goal":            state.Goal,
				"completed_steps": state.CompletedSteps,
				"action_count":    state.ActionCount,
			},
			CreatedAt: now,
		})
	}
	if state.Status == DeepAgentRunStatusBlocked || state.Status == DeepAgentRunStatusBudgetExceeded || state.Status == DeepAgentRunStatusReviewPending {
		candidates = append(candidates, DeepAgentLearningCandidate{
			ID:        newDeepAgentLearningID(),
			Type:      deepAgentLearningTypeBadcase,
			Status:    deepAgentLearningStatusCandidate,
			Source:    deepAgentTaskWorkflowName,
			UserID:    userID,
			SessionID: sessionID,
			RunID:     runID,
			Content:   deepAgentBadcaseLearningContent(state),
			Metadata: map[string]any{
				"goal":              state.Goal,
				"status":            state.Status,
				"blocker":           state.Blocker,
				"failed_steps":      state.FailedSteps,
				"no_progress_count": state.NoProgressCount,
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
		if strings.TrimSpace(candidate.UserID) == "" || strings.TrimSpace(candidate.Content) == "" {
			continue
		}
		item := newConversationMemoryItem(candidate.UserID, candidate.SessionID, candidate.Content)
		item.ID = "mem_deep_agent_" + candidate.ID
		item.Namespace = MemoryNamespaceDefault
		item.Level = MemoryLevelAtomic
		item.Category = MemoryCategoryEvent
		if candidate.Type == deepAgentLearningTypeSuccessPath {
			item.Category = MemoryCategorySkill
		}
		item.Source = MemorySourceSystem
		item.Visibility = MemoryVisibilityUser
		item.Status = MemoryStatusPendingConfirm
		item.Confidence = 0.7
		item.Weight = 0.55
		item.Tags = []string{"deep_agent", candidate.Type, "candidate"}
		item.Metadata = map[string]any{
			"deep_agent_learning": true,
			"learning_id":         candidate.ID,
			"learning_type":       candidate.Type,
			"workflow_run_id":     candidate.RunID,
			"source":              candidate.Source,
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

func firstNonZeroDeepAgentTime(values ...time.Time) time.Time {
	for _, value := range values {
		if !value.IsZero() {
			return value
		}
	}
	return time.Time{}
}
