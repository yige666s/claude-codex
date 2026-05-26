package agentruntime

import (
	"context"
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	workerlifecycle "claude-codex/internal/backend/workers"
)

const EvaluationTriggerDailyIncremental = "daily_incremental"

type DailyEvaluationConfig struct {
	Enabled     bool
	Location    *time.Location
	Hour        int
	Minute      int
	SubjectType string
	UserIDs     []string
	Thresholds  EvaluationThresholds
	Timeout     time.Duration
	BatchLimit  int
}

type DailyEvaluationReport struct {
	From    time.Time
	To      time.Time
	Day     string
	Created int
	Skipped int
	Failed  int
	Total   int
	Passed  int
	Warning int
	Users   []string
}

func (s *Server) StartDailyEvaluationScheduler(config DailyEvaluationConfig) func() {
	if s == nil || !config.Enabled || s.evaluation == nil || s.runtime == nil {
		return func() {}
	}
	config = normalizeDailyEvaluationConfig(config)
	group := workerlifecycle.New(context.Background(), componentLogger(s.logger, "daily_evaluation_scheduler"))
	group.Start("daily_evaluation_scheduler", func(ctx context.Context) error {
		for {
			delay := durationUntilNextDailyEvaluation(time.Now(), config)
			timer := time.NewTimer(delay)
			select {
			case <-timer.C:
				runCtx := ctx
				if config.Timeout > 0 {
					var runCancel context.CancelFunc
					runCtx, runCancel = context.WithTimeout(ctx, config.Timeout)
					report, err := s.RunDailyEvaluationOnce(runCtx, time.Now(), config)
					runCancel()
					s.logDailyEvaluationReport(report, err)
				} else {
					report, err := s.RunDailyEvaluationOnce(runCtx, time.Now(), config)
					s.logDailyEvaluationReport(report, err)
				}
			case <-ctx.Done():
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				return ctx.Err()
			}
		}
	})
	return func() {
		_ = group.Stop(context.Background())
	}
}

func (s *Server) RunDailyEvaluationOnce(ctx context.Context, now time.Time, config DailyEvaluationConfig) (DailyEvaluationReport, error) {
	if s == nil || s.evaluation == nil || s.runtime == nil {
		return DailyEvaluationReport{}, fmt.Errorf("daily evaluation requires runtime and evaluation store")
	}
	config = normalizeDailyEvaluationConfig(config)
	from, to, day := previousDailyEvaluationWindow(now, config)
	users, err := s.dailyEvaluationUserIDs(ctx, config)
	if err != nil {
		return DailyEvaluationReport{From: from, To: to, Day: day}, err
	}
	report := DailyEvaluationReport{From: from, To: to, Day: day, Users: users}
	engine := NewEvaluationEngine(RuntimeEvaluationTraceSource{
		Runtime:  s.runtime,
		LLMUsage: s.llmUsage,
		Risk:     s.risk,
	})
	for _, userID := range users {
		runID := dailyEvaluationRunID(day, userID, config.SubjectType)
		if _, err := s.evaluation.GetEvaluationRun(ctx, runID); err == nil {
			report.Skipped++
			continue
		} else if !errors.Is(err, sql.ErrNoRows) {
			report.Failed++
			continue
		}
		req := EvaluationRunRequest{
			ID:      runID,
			Name:    dailyEvaluationRunName(day, userID, config.SubjectType),
			Trigger: EvaluationTriggerDailyIncremental,
			Scope: EvaluationScope{
				From:        &from,
				To:          &to,
				SubjectType: config.SubjectType,
				UserID:      userID,
			},
			Thresholds: config.Thresholds,
		}
		runReport, err := engine.Evaluate(ctx, req)
		if err != nil {
			report.Failed++
			continue
		}
		if _, err := s.persistEvaluationRunReportContext(ctx, runReport); err != nil {
			report.Failed++
			continue
		}
		report.Created++
		report.Total += runReport.Run.Total
		report.Passed += runReport.Run.Passed
		report.Warning += runReport.Run.Warning
	}
	return report, nil
}

func (s *Server) dailyEvaluationUserIDs(ctx context.Context, config DailyEvaluationConfig) ([]string, error) {
	if len(config.UserIDs) > 0 {
		return normalizeDailyEvaluationUserIDs(config.UserIDs), nil
	}
	store, ok := s.adminUserStore()
	if !ok {
		return []string{}, nil
	}
	limit := config.BatchLimit
	if limit <= 0 {
		limit = 200
	}
	users := make([]string, 0, limit)
	offset := 0
	for len(users) < limit {
		pageLimit := minInt(100, limit-len(users))
		records, err := store.ListUsers(ctx, AdminUserFilter{
			Status: UserStatusActive,
			Limit:  pageLimit,
			Offset: offset,
		})
		if err != nil {
			return nil, err
		}
		if len(records) == 0 {
			break
		}
		for _, record := range records {
			if strings.TrimSpace(record.ID) != "" {
				users = append(users, strings.TrimSpace(record.ID))
			}
		}
		offset += len(records)
		if len(records) < pageLimit {
			break
		}
	}
	return users, nil
}

func (s *Server) logDailyEvaluationReport(report DailyEvaluationReport, err error) {
	if err != nil {
		logFields(s.logger, map[string]any{
			"event": "daily_evaluation_failed",
			"day":   report.Day,
			"from":  report.From.Format(time.RFC3339),
			"to":    report.To.Format(time.RFC3339),
			"error": err.Error(),
		})
		return
	}
	logFields(s.logger, map[string]any{
		"event":   "daily_evaluation_complete",
		"day":     report.Day,
		"users":   len(report.Users),
		"created": report.Created,
		"skipped": report.Skipped,
		"failed":  report.Failed,
		"total":   report.Total,
		"passed":  report.Passed,
		"warning": report.Warning,
	})
}

func normalizeDailyEvaluationConfig(config DailyEvaluationConfig) DailyEvaluationConfig {
	if config.Location == nil {
		config.Location = time.FixedZone("UTC+8", 8*60*60)
	}
	if config.Hour < 0 || config.Hour > 23 {
		config.Hour = 5
	}
	if config.Minute < 0 || config.Minute > 59 {
		config.Minute = 0
	}
	config.SubjectType = normalizeEvaluationSubjectType(config.SubjectType)
	if config.SubjectType == "" {
		config.SubjectType = EvaluationSubjectJob
	}
	config.UserIDs = normalizeDailyEvaluationUserIDs(config.UserIDs)
	if config.Timeout <= 0 {
		config.Timeout = 10 * time.Minute
	}
	if config.BatchLimit <= 0 {
		config.BatchLimit = 200
	}
	return config
}

func normalizeDailyEvaluationUserIDs(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		userID := strings.TrimSpace(value)
		if userID == "" || seen[userID] {
			continue
		}
		seen[userID] = true
		out = append(out, userID)
	}
	return out
}

func durationUntilNextDailyEvaluation(now time.Time, config DailyEvaluationConfig) time.Duration {
	config = normalizeDailyEvaluationConfig(config)
	localNow := now.In(config.Location)
	next := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), config.Hour, config.Minute, 0, 0, config.Location)
	if !next.After(localNow) {
		next = next.AddDate(0, 0, 1)
	}
	return next.Sub(localNow)
}

func previousDailyEvaluationWindow(now time.Time, config DailyEvaluationConfig) (time.Time, time.Time, string) {
	config = normalizeDailyEvaluationConfig(config)
	localNow := now.In(config.Location)
	todayStart := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, config.Location)
	fromLocal := todayStart.AddDate(0, 0, -1)
	return fromLocal.UTC(), todayStart.UTC(), fromLocal.Format("2006-01-02")
}

func dailyEvaluationRunID(day, userID, subjectType string) string {
	raw := strings.Join([]string{day, strings.TrimSpace(userID), normalizeEvaluationSubjectType(subjectType)}, "|")
	sum := sha1.Sum([]byte(raw))
	return "evalrun_daily_" + strings.ReplaceAll(day, "-", "") + "_" + hex.EncodeToString(sum[:])[:12]
}

func dailyEvaluationRunName(day, userID, subjectType string) string {
	return fmt.Sprintf("daily_%s_%s_%s", normalizeEvaluationSubjectType(subjectType), strings.ReplaceAll(day, "-", ""), strings.TrimSpace(userID))
}
