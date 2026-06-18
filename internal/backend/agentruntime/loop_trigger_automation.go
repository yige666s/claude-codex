package agentruntime

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	workerlifecycle "claude-codex/internal/backend/workers"

	robfigcron "github.com/robfig/cron/v3"
)

type LoopTriggerAutomationConfig struct {
	Enabled      bool
	PollInterval time.Duration
	Timeout      time.Duration
	BatchLimit   int
}

type LoopTriggerAutomationReport struct {
	Scanned         int    `json:"scanned"`
	Triggered       int    `json:"triggered"`
	Skipped         int    `json:"skipped"`
	Failed          int    `json:"failed"`
	QuotaBlocked    int    `json:"quota_blocked,omitempty"`
	DedupeConflicts int    `json:"dedupe_conflicts,omitempty"`
	PrunedExpired   int    `json:"pruned_expired,omitempty"`
	LastError       string `json:"last_error,omitempty"`
}

type LoopTriggerAutomationStatus struct {
	Enabled             bool                        `json:"enabled"`
	Running             bool                        `json:"running"`
	LastRunAt           *time.Time                  `json:"last_run_at,omitempty"`
	NextDueAt           *time.Time                  `json:"next_due_at,omitempty"`
	LastError           string                      `json:"last_error,omitempty"`
	ConsecutiveFailures int                         `json:"consecutive_failures"`
	LastReport          LoopTriggerAutomationReport `json:"last_report,omitempty"`
}

type EvalRepairTriggerReport struct {
	Scanned      int    `json:"scanned"`
	Triggered    int    `json:"triggered"`
	Skipped      int    `json:"skipped"`
	Failed       int    `json:"failed"`
	QuotaBlocked int    `json:"quota_blocked,omitempty"`
	MaxSkipped   int    `json:"max_skipped,omitempty"`
	LastError    string `json:"last_error,omitempty"`
}

func (s *Server) StartLoopTriggerAutomationScheduler(config LoopTriggerAutomationConfig) func() {
	if s == nil {
		return func() {}
	}
	config = normalizeLoopTriggerAutomationConfig(config)
	now := time.Now().UTC()
	if !config.Enabled {
		s.updateLoopTriggerAutomationStatus(func(status *LoopTriggerAutomationStatus) {
			status.Enabled = false
			status.Running = false
			status.LastError = ""
			status.ConsecutiveFailures = 0
			status.NextDueAt = nil
		})
		return func() {}
	}
	if s.runtime == nil || s.runtime.loopGoals == nil {
		s.updateLoopTriggerAutomationStatus(func(status *LoopTriggerAutomationStatus) {
			status.Enabled = true
			status.Running = false
			status.LastError = "loop trigger automation requires runtime and loop goal store"
			status.ConsecutiveFailures++
			status.NextDueAt = nil
		})
		return func() {}
	}
	nextDue := now.Add(config.PollInterval)
	s.updateLoopTriggerAutomationStatus(func(status *LoopTriggerAutomationStatus) {
		status.Enabled = true
		status.Running = true
		status.LastError = ""
		status.NextDueAt = &nextDue
	})
	group := workerlifecycle.New(context.Background(), componentLogger(s.logger, "loop_trigger_automation"))
	group.Start("loop_trigger_automation", func(ctx context.Context) error {
		ticker := time.NewTicker(config.PollInterval)
		defer ticker.Stop()
		for {
			select {
			case now := <-ticker.C:
				runCtx := ctx
				var cancel context.CancelFunc
				if config.Timeout > 0 {
					runCtx, cancel = context.WithTimeout(ctx, config.Timeout)
				}
				report, err := s.RunLoopTriggerAutomationOnce(runCtx, now, config)
				if cancel != nil {
					cancel()
				}
				if err != nil {
					logFields(s.logger, map[string]any{"event": "loop_trigger_automation_failed", "error": err.Error(), "scanned": report.Scanned, "triggered": report.Triggered, "failed": report.Failed})
				}
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	})
	return func() {
		_ = group.Stop(context.Background())
		s.updateLoopTriggerAutomationStatus(func(status *LoopTriggerAutomationStatus) {
			status.Running = false
			status.NextDueAt = nil
		})
	}
}

func (s *Server) RunLoopTriggerAutomationOnce(ctx context.Context, now time.Time, config LoopTriggerAutomationConfig) (LoopTriggerAutomationReport, error) {
	config = normalizeLoopTriggerAutomationConfig(config)
	report, err := s.runLoopTriggerAutomationOnce(ctx, now, config)
	if s != nil {
		s.recordLoopTriggerAutomationRun(now, config, report, err)
	}
	return report, err
}

func (s *Server) runLoopTriggerAutomationOnce(ctx context.Context, now time.Time, config LoopTriggerAutomationConfig) (LoopTriggerAutomationReport, error) {
	if s == nil || s.runtime == nil || s.runtime.loopGoals == nil {
		return LoopTriggerAutomationReport{}, fmt.Errorf("loop trigger automation requires runtime and loop goal store")
	}
	goals, err := s.runtime.loopGoals.ListLoopGoals(ctx, LoopGoalFilter{Limit: config.BatchLimit})
	if err != nil {
		return LoopTriggerAutomationReport{}, err
	}
	report := LoopTriggerAutomationReport{}
	if s.runtime.loopTriggers != nil {
		pruned, err := s.runtime.loopTriggers.PruneExpiredLoopTriggers(ctx, now)
		if err != nil {
			return report, err
		}
		report.PrunedExpired = pruned
	}
	for _, goal := range goals {
		if goal == nil {
			continue
		}
		report.Scanned++
		var req LoopTriggerRequest
		var ok bool
		switch normalizeLoopTriggerType(goal.Trigger.Type) {
		case LoopTriggerTypeSchedule:
			req, ok = scheduledLoopTriggerRequest(goal, now, config.PollInterval)
		case LoopTriggerTypeMonitor:
			req, ok = s.monitorLoopTriggerRequest(ctx, goal, now)
		default:
			report.Skipped++
			continue
		}
		if !ok {
			report.Skipped++
			continue
		}
		result, err := s.runtime.StartDeepAgentLoopTrigger(ctx, req)
		if err != nil {
			report.LastError = err.Error()
			if loopTriggerAutomationQuotaBlocked(err) {
				report.QuotaBlocked++
			}
			report.Failed++
			continue
		}
		if result.Duplicate {
			report.DedupeConflicts++
			report.Skipped++
			continue
		}
		s.recordLoopTriggerAudit(ctx, User{ID: req.UserID}, result, map[string]any{"automation": true})
		if s.metrics != nil {
			s.metrics.IncLoopAutomationTrigger(req.TriggerType, req.Source)
		}
		report.Triggered++
	}
	return report, nil
}

func normalizeLoopTriggerAutomationConfig(config LoopTriggerAutomationConfig) LoopTriggerAutomationConfig {
	if config.PollInterval <= 0 {
		config.PollInterval = time.Minute
	}
	if config.Timeout <= 0 {
		config.Timeout = 2 * time.Minute
	}
	if config.BatchLimit <= 0 {
		config.BatchLimit = 500
	}
	return config
}

func scheduledLoopTriggerRequest(goal *LoopGoal, now time.Time, window time.Duration) (LoopTriggerRequest, bool) {
	payload := goal.Trigger.Payload
	if len(payload) == 0 {
		return LoopTriggerRequest{}, false
	}
	scheduledAt, due := schedulePayloadDue(payload, now, window)
	if !due {
		return LoopTriggerRequest{}, false
	}
	dedupeKey := scheduleDedupeKey(goal, now, scheduledAt)
	return LoopTriggerRequest{
		UserID:      goal.UserID,
		SessionID:   goal.SessionID,
		Objective:   firstNonEmptyString(payloadString(payload, "objective"), goal.Objective),
		TemplateID:  firstNonEmptyString(loopTemplateIDFromMetadata(goal.Metadata), payloadString(payload, "template_id")),
		TaskType:    firstNonEmptyString(payloadString(payload, "task_type"), goal.TaskType, "scheduled"),
		Deliverable: firstNonEmptyString(payloadString(payload, "deliverable"), goal.Deliverable, "answer"),
		Rubric:      goal.Rubric,
		Budget:      goal.Budget,
		StopPolicy:  goal.StopPolicy,
		TriggerType: LoopTriggerTypeSchedule,
		Source:      firstNonEmptyString(goal.Trigger.Source, "scheduler"),
		DedupeKey:   dedupeKey,
		Payload: map[string]any{
			"scheduled_goal_id": goal.ID,
			"scheduled_at":      scheduledAt.UTC().Format(time.RFC3339),
			"template_id":       firstNonEmptyString(loopTemplateIDFromMetadata(goal.Metadata), payloadString(payload, "template_id")),
		},
	}, true
}

func schedulePayloadDue(payload map[string]any, now time.Time, window time.Duration) (time.Time, bool) {
	if window <= 0 {
		window = time.Minute
	}
	if runAt := payloadString(payload, "run_at"); runAt != "" {
		at, err := time.Parse(time.RFC3339, runAt)
		return at, err == nil && !now.Before(at)
	}
	if interval := payloadDurationSeconds(payload, "interval_seconds"); interval > 0 {
		bucket := now.Unix() / int64(interval.Seconds())
		at := time.Unix(bucket*int64(interval.Seconds()), 0).UTC()
		return at, !at.Before(now.Add(-window)) && !at.After(now)
	}
	if cron := payloadString(payload, "cron"); cron != "" {
		return cronDueTime(cron, payloadString(payload, "timezone"), now, window)
	}
	return time.Time{}, false
}

func scheduleDedupeKey(goal *LoopGoal, now, scheduledAt time.Time) string {
	payload := goal.Trigger.Payload
	if runAt := payloadString(payload, "run_at"); runAt != "" {
		return "schedule:" + goal.ID + ":run_at:" + runAt
	}
	if interval := payloadDurationSeconds(payload, "interval_seconds"); interval > 0 {
		return fmt.Sprintf("schedule:%s:interval:%d", goal.ID, now.Unix()/int64(interval.Seconds()))
	}
	if scheduledAt.IsZero() {
		scheduledAt = now
	}
	return "schedule:" + goal.ID + ":cron:" + scheduledAt.UTC().Format("20060102T1504")
}

func cronDueTime(expr, timezone string, now time.Time, window time.Duration) (time.Time, bool) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return time.Time{}, false
	}
	if timezone = strings.TrimSpace(timezone); timezone != "" && !strings.HasPrefix(expr, "TZ=") && !strings.HasPrefix(expr, "CRON_TZ=") {
		expr = "CRON_TZ=" + timezone + " " + expr
	}
	schedule, err := robfigcron.ParseStandard(expr)
	if err != nil {
		return time.Time{}, false
	}
	if window <= 0 {
		window = time.Minute
	}
	start := now.Add(-window).Add(-time.Nanosecond)
	next := schedule.Next(start)
	if next.IsZero() || next.After(now) {
		return time.Time{}, false
	}
	return next, true
}

func (s *Server) monitorLoopTriggerRequest(ctx context.Context, goal *LoopGoal, now time.Time) (LoopTriggerRequest, bool) {
	payload := goal.Trigger.Payload
	if !strings.EqualFold(payloadString(payload, "resource_type"), "job") {
		return LoopTriggerRequest{}, false
	}
	jobID := payloadString(payload, "job_id")
	if jobID == "" {
		return LoopTriggerRequest{}, false
	}
	job, err := s.runtime.GetJob(ctx, goal.UserID, jobID)
	if err != nil || job == nil {
		return LoopTriggerRequest{}, false
	}
	expected := firstNonEmptyString(payloadString(payload, "expected_status"), JobStatusSucceeded)
	if strings.EqualFold(job.Status, expected) {
		return LoopTriggerRequest{}, false
	}
	if payloadBool(payload, "terminal_only") && !isTerminalJobStatus(job.Status) {
		return LoopTriggerRequest{}, false
	}
	return LoopTriggerRequest{
		UserID:      goal.UserID,
		SessionID:   firstNonEmptyString(goal.SessionID, job.SessionID),
		Objective:   firstNonEmptyString(payloadString(payload, "objective"), "Investigate monitored job "+job.ID+" status "+job.Status),
		TemplateID:  firstNonEmptyString(loopTemplateIDFromMetadata(goal.Metadata), payloadString(payload, "template_id"), LoopTemplateWebMonitor),
		TaskType:    firstNonEmptyString(payloadString(payload, "task_type"), "monitor_repair"),
		Deliverable: firstNonEmptyString(payloadString(payload, "deliverable"), "answer"),
		Rubric:      goal.Rubric,
		Budget:      goal.Budget,
		StopPolicy:  goal.StopPolicy,
		TriggerType: LoopTriggerTypeMonitor,
		Source:      firstNonEmptyString(goal.Trigger.Source, "monitor"),
		DedupeKey:   "monitor:" + goal.ID + ":" + job.ID + ":" + job.Status,
		Payload: map[string]any{
			"monitor_goal_id": goal.ID,
			"job_id":          job.ID,
			"actual_status":   job.Status,
			"expected_status": expected,
			"checked_at":      now.UTC().Format(time.RFC3339),
			"template_id":     firstNonEmptyString(loopTemplateIDFromMetadata(goal.Metadata), payloadString(payload, "template_id"), LoopTemplateWebMonitor),
		},
	}, true
}

func payloadString(payload map[string]any, key string) string {
	if payload == nil {
		return ""
	}
	value, ok := payload[key]
	if !ok || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}

func payloadBool(payload map[string]any, key string) bool {
	switch strings.ToLower(payloadString(payload, key)) {
	case "true", "1", "yes", "y":
		return true
	default:
		return false
	}
}

func payloadDurationSeconds(payload map[string]any, key string) time.Duration {
	raw := payloadString(payload, key)
	if raw == "" {
		return 0
	}
	seconds, err := strconv.Atoi(raw)
	if err != nil || seconds <= 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}

func (s *Server) TriggerEvalRepairLoops(ctx context.Context, report EvaluationRunReport) EvalRepairTriggerReport {
	if s == nil || s.runtime == nil {
		return EvalRepairTriggerReport{}
	}
	out := EvalRepairTriggerReport{}
	policy := DefaultEvalRepairTriggerPolicy(report.Run)
	for _, result := range report.Results {
		out.Scanned++
		if !strings.EqualFold(result.Status, EvaluationResultStatusFailed) || strings.TrimSpace(result.UserID) == "" || strings.TrimSpace(result.SessionID) == "" {
			out.Skipped++
			continue
		}
		if !evalRepairResultAllowed(policy, result, out.Triggered) {
			out.Skipped++
			if policy.Enabled && policy.MaxTriggersPerRun > 0 && out.Triggered >= policy.MaxTriggersPerRun {
				out.MaxSkipped++
			}
			continue
		}
		req := LoopTriggerRequest{
			UserID:      result.UserID,
			SessionID:   result.SessionID,
			Objective:   evalRepairObjective(report.Run, result),
			TaskType:    "eval_repair",
			Deliverable: "fix or remediation report",
			TriggerType: LoopTriggerTypeEval,
			Source:      "evaluation:" + report.Run.ID,
			DedupeKey:   "eval:" + result.ID,
			Payload: map[string]any{
				"evaluation_run_id":    report.Run.ID,
				"evaluation_result_id": result.ID,
				"subject_type":         result.SubjectType,
				"subject_id":           result.SubjectID,
				"job_id":               result.JobID,
				"status":               result.Status,
				"score":                result.Score,
				"findings":             result.Findings,
			},
		}
		trigger, err := s.runtime.StartDeepAgentLoopTrigger(ctx, req)
		if err != nil {
			out.LastError = err.Error()
			if loopTriggerAutomationQuotaBlocked(err) {
				out.QuotaBlocked++
				if s.metrics != nil {
					s.metrics.IncLoopAutomationQuotaBlocked()
				}
				out.Failed++
				continue
			}
			out.Failed++
			continue
		}
		s.recordLoopTriggerAudit(ctx, User{ID: result.UserID}, trigger, map[string]any{"eval_auto_repair": true})
		if s.metrics != nil {
			s.metrics.IncLoopAutomationTrigger(req.TriggerType, req.Source)
		}
		out.Triggered++
	}
	return out
}

func evalRepairObjective(run EvaluationRun, result EvaluationResult) string {
	findings := make([]string, 0, len(result.Findings))
	for _, finding := range result.Findings {
		if strings.TrimSpace(finding.Message) != "" {
			findings = append(findings, strings.TrimSpace(finding.Message))
		}
	}
	if len(findings) > 0 {
		return "Repair evaluation failure from run " + run.ID + ": " + strings.Join(findings, "; ")
	}
	return "Repair evaluation failure from run " + run.ID + " for " + result.SubjectType + " " + result.SubjectID
}

func (s *Server) recordLoopTriggerAudit(ctx context.Context, user User, result LoopTriggerResult, extra map[string]any) {
	if s == nil || s.audit == nil {
		return
	}
	metadata := map[string]any{
		"session_id":   result.Trigger.SessionID,
		"job_id":       triggerResultJobID(result),
		"trigger_type": result.Trigger.Type,
		"source":       result.Trigger.Source,
		"dedupe_key":   result.Trigger.DedupeKey,
		"duplicate":    result.Duplicate,
	}
	for key, value := range extra {
		metadata[key] = value
	}
	_ = s.audit.Record(ctx, AuditRecord{
		ID:        newAuditID(),
		Event:     "loop_trigger_create",
		UserID:    user.ID,
		Metadata:  metadata,
		CreatedAt: time.Now().UTC(),
	})
}

func (s *Server) LoopTriggerAutomationStatus() LoopTriggerAutomationStatus {
	if s == nil {
		return LoopTriggerAutomationStatus{}
	}
	s.loopAutomationMu.RLock()
	defer s.loopAutomationMu.RUnlock()
	return cloneLoopTriggerAutomationStatus(s.loopAutomation)
}

func (s *Server) LoopTriggerAutomationReadinessCheck() func(context.Context) error {
	return func(context.Context) error {
		status := s.LoopTriggerAutomationStatus()
		if !status.Enabled {
			return nil
		}
		if !status.Running {
			if status.LastError != "" {
				return fmt.Errorf("loop trigger automation is not running: %s", status.LastError)
			}
			return fmt.Errorf("loop trigger automation is not running")
		}
		if status.LastError != "" && status.ConsecutiveFailures > 0 {
			return fmt.Errorf("loop trigger automation failing: %s", status.LastError)
		}
		return nil
	}
}

func (s *Server) updateLoopTriggerAutomationStatus(update func(*LoopTriggerAutomationStatus)) {
	if s == nil || update == nil {
		return
	}
	s.loopAutomationMu.Lock()
	update(&s.loopAutomation)
	status := cloneLoopTriggerAutomationStatus(s.loopAutomation)
	s.loopAutomationMu.Unlock()
	if s.metrics != nil {
		s.metrics.SetLoopAutomationStatus(status.Enabled, status.Running, status.ConsecutiveFailures, status.LastRunAt, status.NextDueAt)
	}
}

func (s *Server) recordLoopTriggerAutomationRun(now time.Time, config LoopTriggerAutomationConfig, report LoopTriggerAutomationReport, err error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	nextDue := now.Add(config.PollInterval)
	s.updateLoopTriggerAutomationStatus(func(status *LoopTriggerAutomationStatus) {
		if config.Enabled {
			status.Enabled = true
		}
		status.LastRunAt = &now
		status.NextDueAt = &nextDue
		status.LastReport = report
		if err != nil {
			status.LastError = err.Error()
			status.ConsecutiveFailures++
			return
		}
		status.LastError = ""
		status.ConsecutiveFailures = 0
	})
	if s.metrics != nil {
		s.metrics.RecordLoopAutomationReport(report)
	}
}

func cloneLoopTriggerAutomationStatus(status LoopTriggerAutomationStatus) LoopTriggerAutomationStatus {
	if status.LastRunAt != nil {
		value := status.LastRunAt.UTC()
		status.LastRunAt = &value
	}
	if status.NextDueAt != nil {
		value := status.NextDueAt.UTC()
		status.NextDueAt = &value
	}
	return status
}

func loopTriggerAutomationQuotaBlocked(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "quota")
}
