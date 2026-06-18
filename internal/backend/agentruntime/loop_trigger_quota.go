package agentruntime

import (
	"context"
	"fmt"
	"strings"
	"time"
)

func CheckLoopTriggerQuota(ctx context.Context, usage LLMUsageAdminStore, config LLMGovernanceConfig, userID string) error {
	userID = strings.TrimSpace(userID)
	if usage == nil || userID == "" {
		return nil
	}
	config = config.normalized()
	if config.DailyTokenQuota <= 0 && config.DailyRequestQuota <= 0 && config.DailyCostQuotaUSD <= 0 {
		return nil
	}
	summary, err := usage.SummarizeLLMQuota(ctx, userID, startOfUTCDay(time.Now()), 1)
	if err != nil {
		return fmt.Errorf("check loop trigger quota: %w", err)
	}
	effective := summary.EffectiveUsage
	if config.DailyRequestQuota > 0 && effective.Requests >= config.DailyRequestQuota {
		return fmt.Errorf("daily LLM request quota exceeded")
	}
	if config.DailyTokenQuota > 0 && effective.TotalTokens >= config.DailyTokenQuota {
		return fmt.Errorf("daily LLM token quota exceeded")
	}
	if config.DailyCostQuotaUSD > 0 && effective.EstimatedCostUSD >= config.DailyCostQuotaUSD {
		return fmt.Errorf("daily LLM cost quota exceeded")
	}
	return nil
}
