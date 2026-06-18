package agentruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	LoopTriggerTypeManual   = "manual"
	LoopTriggerTypeSchedule = "schedule"
	LoopTriggerTypeWebhook  = "webhook"
	LoopTriggerTypeEval     = "eval"
	LoopTriggerTypeMonitor  = "monitor"

	defaultLoopTriggerDedupeWindow = 10 * time.Minute
)

type LoopTriggerRequest struct {
	UserID         string              `json:"user_id,omitempty"`
	SessionID      string              `json:"session_id,omitempty"`
	Objective      string              `json:"objective"`
	TemplateID     string              `json:"template_id,omitempty"`
	TaskType       string              `json:"task_type,omitempty"`
	Deliverable    string              `json:"deliverable,omitempty"`
	Rubric         LoopRubric          `json:"rubric,omitempty"`
	Budget         LoopBudget          `json:"budget,omitempty"`
	StopPolicy     LoopStopPolicy      `json:"stop_policy,omitempty"`
	TriggerType    string              `json:"trigger_type,omitempty"`
	Source         string              `json:"source,omitempty"`
	DedupeKey      string              `json:"dedupe_key,omitempty"`
	Payload        map[string]any      `json:"payload,omitempty"`
	AttachmentIDs  []string            `json:"attachment_ids,omitempty"`
	AttachmentURLs []ChatAttachmentURL `json:"attachment_urls,omitempty"`
}

type LoopTriggerRecord struct {
	ID        string         `json:"id,omitempty"`
	UserID    string         `json:"user_id,omitempty"`
	SessionID string         `json:"session_id,omitempty"`
	Type      string         `json:"type"`
	Source    string         `json:"source,omitempty"`
	DedupeKey string         `json:"dedupe_key"`
	Payload   map[string]any `json:"payload,omitempty"`
	Status    string         `json:"status,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
	ExpiresAt time.Time      `json:"expires_at,omitempty"`
}

type LoopTriggerResult struct {
	Job       *Job              `json:"job,omitempty"`
	Goal      *LoopGoal         `json:"goal,omitempty"`
	Trigger   LoopTriggerRecord `json:"trigger"`
	Duplicate bool              `json:"duplicate"`
}

type loopTriggerDedupeEntry struct {
	result    LoopTriggerResult
	expiresAt time.Time
}

func (r *Runtime) StartDeepAgentLoopTrigger(ctx context.Context, req LoopTriggerRequest) (LoopTriggerResult, error) {
	if r == nil {
		return LoopTriggerResult{}, fmt.Errorf("runtime is not configured")
	}
	req = normalizeLoopTriggerRequest(req)
	req = applyLoopTemplateToTriggerRequest(req)
	if err := validateLoopTriggerRequest(req); err != nil {
		return LoopTriggerResult{}, err
	}
	if err := r.checkLoopTriggerPolicy(req); err != nil {
		return LoopTriggerResult{}, err
	}
	now := r.now()
	trigger := LoopTriggerRecord{
		ID:        NewLoopTriggerID(),
		UserID:    req.UserID,
		SessionID: req.SessionID,
		Type:      req.TriggerType,
		Source:    req.Source,
		DedupeKey: req.DedupeKey,
		Payload:   cloneLoopTriggerPayload(req.Payload),
		Status:    LoopTriggerStatusStarted,
		CreatedAt: now,
		ExpiresAt: now.Add(defaultLoopTriggerDedupeWindow),
	}
	if existing, ok, err := r.lookupLoopTriggerDedupe(ctx, trigger.DedupeKey, now); err != nil {
		return LoopTriggerResult{}, err
	} else if ok {
		existing.Duplicate = true
		return existing, nil
	}
	if r.triggerQuotaCheck != nil {
		if err := r.triggerQuotaCheck(ctx, req); err != nil {
			return LoopTriggerResult{}, err
		}
	}
	var goal *LoopGoal
	loopGoalID := ""
	if r.loopGoals != nil {
		goal = normalizeLoopGoal(&LoopGoal{
			UserID:      req.UserID,
			SessionID:   req.SessionID,
			Objective:   firstNonEmptyString(req.Objective, "Please analyze the attached file(s)."),
			TaskType:    firstNonEmptyString(req.TaskType, "deep_agent"),
			Deliverable: firstNonEmptyString(req.Deliverable, "answer"),
			Rubric:      req.Rubric,
			Budget:      firstNonZeroLoopBudget(req.Budget, loopBudgetFromDeepAgentPolicy(defaultDeepAgentJobPolicy())),
			Trigger:     loopGoalTriggerFromRecord(trigger),
			StopPolicy:  req.StopPolicy,
			Status:      LoopGoalStatusPending,
			Metadata:    map[string]any{"template_id": req.TemplateID},
			CreatedAt:   now,
			UpdatedAt:   now,
		})
		if err := r.loopGoals.UpsertLoopGoal(ctx, goal); err != nil {
			return LoopTriggerResult{}, err
		}
		loopGoalID = goal.ID
	}
	job, err := r.CreateJob(ctx, ChatRequest{
		UserID:         req.UserID,
		SessionID:      req.SessionID,
		LoopGoalID:     loopGoalID,
		Content:        req.Objective,
		AttachmentIDs:  req.AttachmentIDs,
		AttachmentURLs: req.AttachmentURLs,
		AgentMode:      AgentModePlanExecute,
	}, JobTypeDeepAgent)
	if err != nil {
		return LoopTriggerResult{}, err
	}
	if goal != nil {
		goal.JobID = job.ID
		goal.UpdatedAt = now
		_ = r.loopGoals.UpdateLoopGoalRun(ctx, req.UserID, goal.ID, job.ID, "", LoopGoalStatusPending, now)
	}
	r.rememberLoopTriggerForJob(job.ID, trigger)
	result := LoopTriggerResult{Job: job, Goal: goal, Trigger: trigger}
	if err := r.rememberLoopTriggerDedupe(ctx, result, trigger.ExpiresAt); err != nil {
		if errors.Is(err, ErrLoopTriggerDuplicate) {
			_ = r.jobs.UpdateJobStatus(ctx, req.UserID, job.ID, JobStatusCancelled, "duplicate loop trigger", now)
			if existing, ok, lookupErr := r.lookupLoopTriggerDedupe(ctx, trigger.DedupeKey, now); lookupErr != nil {
				return LoopTriggerResult{}, lookupErr
			} else if ok {
				existing.Duplicate = true
				return existing, nil
			}
		}
		return LoopTriggerResult{}, err
	}
	if err := r.StartJob(ctx, job); err != nil {
		return LoopTriggerResult{}, err
	}
	return result, nil
}

func normalizeLoopTriggerRequest(req LoopTriggerRequest) LoopTriggerRequest {
	req.UserID = strings.TrimSpace(req.UserID)
	req.SessionID = strings.TrimSpace(req.SessionID)
	req.Objective = strings.TrimSpace(req.Objective)
	req.TemplateID = normalizeLoopTemplateID(req.TemplateID)
	req.TaskType = strings.TrimSpace(req.TaskType)
	req.Deliverable = strings.TrimSpace(req.Deliverable)
	req.TriggerType = normalizeLoopTriggerType(req.TriggerType)
	req.Source = strings.TrimSpace(req.Source)
	if req.Source == "" {
		req.Source = req.TriggerType
	}
	req.DedupeKey = strings.TrimSpace(req.DedupeKey)
	if req.DedupeKey == "" {
		req.DedupeKey = buildLoopTriggerDedupeKey(req)
	}
	return req
}

func validateLoopTriggerRequest(req LoopTriggerRequest) error {
	if req.UserID == "" {
		return fmt.Errorf("user ID is required")
	}
	if req.SessionID == "" {
		return fmt.Errorf("session ID is required")
	}
	if req.Objective == "" && len(req.AttachmentIDs) == 0 && len(req.AttachmentURLs) == 0 {
		return fmt.Errorf("objective or attachment is required")
	}
	if !isSupportedLoopTriggerType(req.TriggerType) {
		return fmt.Errorf("unsupported loop trigger type: %s", req.TriggerType)
	}
	if req.DedupeKey == "" {
		return fmt.Errorf("dedupe key is required")
	}
	return nil
}

func normalizeLoopTriggerType(triggerType string) string {
	switch strings.ToLower(strings.TrimSpace(triggerType)) {
	case "", LoopTriggerTypeManual:
		return LoopTriggerTypeManual
	case LoopTriggerTypeSchedule:
		return LoopTriggerTypeSchedule
	case LoopTriggerTypeWebhook:
		return LoopTriggerTypeWebhook
	case LoopTriggerTypeEval:
		return LoopTriggerTypeEval
	case LoopTriggerTypeMonitor:
		return LoopTriggerTypeMonitor
	default:
		return strings.ToLower(strings.TrimSpace(triggerType))
	}
}

func isSupportedLoopTriggerType(triggerType string) bool {
	switch normalizeLoopTriggerType(triggerType) {
	case LoopTriggerTypeManual, LoopTriggerTypeSchedule, LoopTriggerTypeWebhook, LoopTriggerTypeEval, LoopTriggerTypeMonitor:
		return true
	default:
		return false
	}
}

func buildLoopTriggerDedupeKey(req LoopTriggerRequest) string {
	payload, _ := json.Marshal(stableLoopTriggerPayload(req.Payload))
	attachments, _ := json.Marshal(struct {
		IDs  []string            `json:"ids,omitempty"`
		URLs []ChatAttachmentURL `json:"urls,omitempty"`
	}{IDs: req.AttachmentIDs, URLs: req.AttachmentURLs})
	sum := sha256.Sum256([]byte(strings.Join([]string{
		req.UserID,
		req.SessionID,
		normalizeLoopTriggerType(req.TriggerType),
		strings.TrimSpace(req.Source),
		strings.TrimSpace(req.Objective),
		string(payload),
		string(attachments),
	}, "\x00")))
	return "loop-trigger-" + hex.EncodeToString(sum[:12])
}

func stableLoopTriggerPayload(payload map[string]any) map[string]any {
	if len(payload) == 0 {
		return nil
	}
	keys := make([]string, 0, len(payload))
	for key := range payload {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make(map[string]any, len(payload))
	for _, key := range keys {
		out[key] = payload[key]
	}
	return out
}

func cloneLoopTriggerPayload(payload map[string]any) map[string]any {
	if len(payload) == 0 {
		return nil
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return stableLoopTriggerPayload(payload)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return stableLoopTriggerPayload(payload)
	}
	return out
}

func (r *Runtime) lookupLoopTriggerDedupe(ctx context.Context, key string, now time.Time) (LoopTriggerResult, bool, error) {
	key = strings.TrimSpace(key)
	if r == nil || key == "" {
		return LoopTriggerResult{}, false, nil
	}
	if r.loopTriggers == nil {
		return LoopTriggerResult{}, false, nil
	}
	return r.loopTriggers.GetActiveLoopTrigger(ctx, key, now)
}

func (r *Runtime) rememberLoopTriggerDedupe(ctx context.Context, result LoopTriggerResult, expiresAt time.Time) error {
	if r == nil || r.loopTriggers == nil || strings.TrimSpace(result.Trigger.DedupeKey) == "" {
		return nil
	}
	return r.loopTriggers.UpsertLoopTrigger(ctx, result, expiresAt)
}

func (r *Runtime) rememberLoopTriggerForJob(jobID string, trigger LoopTriggerRecord) {
	jobID = strings.TrimSpace(jobID)
	if r == nil || jobID == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.loopTriggersByJob == nil {
		r.loopTriggersByJob = make(map[string]LoopTriggerRecord)
	}
	r.loopTriggersByJob[jobID] = cloneLoopTriggerRecord(trigger)
}

func (r *Runtime) loopTriggerForJob(jobID string) (LoopTriggerRecord, bool) {
	jobID = strings.TrimSpace(jobID)
	if r == nil || jobID == "" {
		return LoopTriggerRecord{}, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	trigger, ok := r.loopTriggersByJob[jobID]
	if !ok {
		return LoopTriggerRecord{}, false
	}
	return cloneLoopTriggerRecord(trigger), true
}

func cloneLoopTriggerResult(result LoopTriggerResult) LoopTriggerResult {
	out := result
	out.Job = cloneJob(result.Job)
	out.Goal = cloneLoopGoal(result.Goal)
	out.Trigger = cloneLoopTriggerRecord(result.Trigger)
	return out
}

func cloneLoopTriggerRecord(trigger LoopTriggerRecord) LoopTriggerRecord {
	trigger.Payload = cloneLoopTriggerPayload(trigger.Payload)
	return trigger
}

func loopGoalTriggerFromRecord(record LoopTriggerRecord) LoopTrigger {
	return normalizeLoopTrigger(LoopTrigger{
		Type:      record.Type,
		Source:    record.Source,
		DedupeKey: record.DedupeKey,
		Payload:   cloneLoopTriggerPayload(record.Payload),
	})
}

func firstNonZeroLoopBudget(primary, fallback LoopBudget) LoopBudget {
	if primary.MaxSteps != 0 || primary.MaxActions != 0 || primary.MaxDuration != 0 || primary.MaxTokens != 0 || primary.MaxCostCents != 0 || primary.MaxToolCalls != 0 {
		return primary
	}
	return fallback
}
