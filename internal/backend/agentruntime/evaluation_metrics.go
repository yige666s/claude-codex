package agentruntime

import (
	"encoding/json"
	"math"
	"sort"
	"strings"
	"time"
)

type EvaluationTraceMetrics struct {
	DurationMS        int64   `json:"duration_ms,omitempty"`
	ChatLLMFullMS     int64   `json:"chat_llm_full_latency_ms,omitempty"`
	FirstTokenMS      int64   `json:"first_token_latency_ms,omitempty"`
	JobEndToEndMS     int64   `json:"job_end_to_end_latency_ms,omitempty"`
	SkillExecutionMS  int64   `json:"skill_execution_latency_ms,omitempty"`
	SandboxStartupMS  int64   `json:"sandbox_startup_latency_ms,omitempty"`
	ArtifactLatencyMS int64   `json:"artifact_generation_latency_ms,omitempty"`
	ToolCallCount     int     `json:"tool_call_count"`
	ToolErrorCount    int     `json:"tool_error_count"`
	SkillCount        int     `json:"skill_count"`
	SkillFailureCount int     `json:"skill_failure_count"`
	LLMRequests       int     `json:"llm_requests"`
	LLMFailures       int     `json:"llm_failures"`
	InputTokens       int     `json:"input_tokens"`
	OutputTokens      int     `json:"output_tokens"`
	TotalTokens       int     `json:"total_tokens"`
	EstimatedCostUSD  float64 `json:"estimated_cost_usd"`
	RiskHighCount     int     `json:"risk_high_count"`
	RiskMediumCount   int     `json:"risk_medium_count"`
	RiskLowCount      int     `json:"risk_low_count"`
	ArtifactCount     int     `json:"artifact_count"`
	EmptyOutput       bool    `json:"empty_output"`
}

type EvaluationAggregateMetrics struct {
	Total             int     `json:"total"`
	Passed            int     `json:"passed"`
	Failed            int     `json:"failed"`
	Warning           int     `json:"warning"`
	SuccessRate       float64 `json:"success_rate"`
	FailureRate       float64 `json:"failure_rate"`
	WarningRate       float64 `json:"warning_rate"`
	AverageLatencyMS  float64 `json:"average_latency_ms"`
	P50LatencyMS      int64   `json:"p50_latency_ms"`
	P95LatencyMS      int64   `json:"p95_latency_ms"`
	P99LatencyMS      int64   `json:"p99_latency_ms"`
	ChatLLMP95MS      int64   `json:"chat_llm_full_p95_ms"`
	FirstTokenP95MS   int64   `json:"first_token_p95_ms"`
	JobEndToEndP95MS  int64   `json:"job_end_to_end_p95_ms"`
	SkillP95MS        int64   `json:"skill_execution_p95_ms"`
	SandboxP95MS      int64   `json:"sandbox_startup_p95_ms"`
	ArtifactP95MS     int64   `json:"artifact_generation_p95_ms"`
	ToolCallCount     int     `json:"tool_call_count"`
	ToolErrorCount    int     `json:"tool_error_count"`
	ToolErrorRate     float64 `json:"tool_error_rate"`
	SkillCount        int     `json:"skill_count"`
	SkillFailureCount int     `json:"skill_failure_count"`
	SkillFailureRate  float64 `json:"skill_failure_rate"`
	LLMRequests       int     `json:"llm_requests"`
	LLMFailures       int     `json:"llm_failures"`
	LLMErrorRate      float64 `json:"llm_error_rate"`
	InputTokens       int     `json:"input_tokens"`
	OutputTokens      int     `json:"output_tokens"`
	TotalTokens       int     `json:"total_tokens"`
	EstimatedCostUSD  float64 `json:"estimated_cost_usd"`
	HighRiskCount     int     `json:"high_risk_count"`
	MediumRiskCount   int     `json:"medium_risk_count"`
	LowRiskCount      int     `json:"low_risk_count"`
	ArtifactCount     int     `json:"artifact_count"`
	EmptyOutputCount  int     `json:"empty_output_count"`
}

func calculateTraceMetrics(trace EvaluationTrace) EvaluationTraceMetrics {
	metrics := EvaluationTraceMetrics{
		ArtifactCount: len(trace.Artifacts),
		EmptyOutput:   strings.TrimSpace(trace.Output) == "",
	}
	if duration := traceDuration(trace); duration > 0 {
		metrics.DurationMS = duration.Milliseconds()
	}
	if trace.SubjectType == EvaluationSubjectJob && metrics.DurationMS > 0 {
		metrics.JobEndToEndMS = metrics.DurationMS
	}
	for _, message := range trace.Messages {
		metrics.ToolCallCount += len(message.ToolCalls)
		if message.Role == "tool" {
			if strings.TrimSpace(message.ToolOutput) == "" || looksLikeToolError(message.ToolOutput) {
				metrics.ToolErrorCount++
			}
		}
	}
	for _, event := range trace.JobEvents {
		if event == nil {
			continue
		}
		if event.Type == "error" || event.Event.Type == "error" || strings.TrimSpace(event.Event.Error) != "" {
			metrics.ToolErrorCount++
		}
		data := eventDataMap(event)
		switch event.Type {
		case "sandbox_metric":
			metrics.SandboxStartupMS = maxInt64(metrics.SandboxStartupMS, eventDataInt64(data, "startup_ms"))
		case "artifact_metric":
			metrics.ArtifactLatencyMS = maxInt64(metrics.ArtifactLatencyMS, eventDataInt64(data, "duration_ms"))
		}
	}
	for _, record := range trace.SkillExecutions {
		metrics.SkillCount++
		if record.Status == SkillExecutionStatusFailed {
			metrics.SkillFailureCount++
		}
		metrics.SkillExecutionMS = maxInt64(metrics.SkillExecutionMS, record.DurationMS)
	}
	for _, record := range trace.LLMUsage {
		metrics.LLMRequests++
		if record.Status != "success" {
			metrics.LLMFailures++
		}
		if strings.TrimSpace(record.SkillName) == "" {
			metrics.ChatLLMFullMS = maxInt64(metrics.ChatLLMFullMS, record.LatencyMs)
			metrics.FirstTokenMS = maxInt64(metrics.FirstTokenMS, record.TTFTMs)
		}
		metrics.InputTokens += record.InputTokens
		metrics.OutputTokens += record.OutputTokens
		metrics.TotalTokens += record.TotalTokens
		metrics.EstimatedCostUSD += record.EstimatedCostUSD
	}
	metrics.EstimatedCostUSD = roundEvaluationCost(metrics.EstimatedCostUSD)
	for _, event := range trace.RiskEvents {
		switch strings.ToLower(strings.TrimSpace(event.RiskLevel)) {
		case RiskLevelHigh:
			metrics.RiskHighCount++
		case RiskLevelMedium:
			metrics.RiskMediumCount++
		default:
			metrics.RiskLowCount++
		}
	}
	return metrics
}

func aggregateEvaluationMetrics(results []EvaluationResult) EvaluationAggregateMetrics {
	aggregate := EvaluationAggregateMetrics{Total: len(results)}
	var latencies []int64
	var chatLLMLatencies []int64
	var firstTokenLatencies []int64
	var jobEndToEndLatencies []int64
	var skillLatencies []int64
	var sandboxLatencies []int64
	var artifactLatencies []int64
	var latencyTotal int64
	for _, result := range results {
		switch result.Status {
		case EvaluationResultStatusPassed:
			aggregate.Passed++
		case EvaluationResultStatusFailed:
			aggregate.Failed++
		case EvaluationResultStatusWarning:
			aggregate.Warning++
		}
		metrics := evaluationTraceMetricsFromMap(result.Metrics)
		if metrics.DurationMS > 0 {
			latencies = append(latencies, metrics.DurationMS)
			latencyTotal += metrics.DurationMS
		}
		appendPositive := func(values *[]int64, value int64) {
			if value > 0 {
				*values = append(*values, value)
			}
		}
		appendPositive(&chatLLMLatencies, metrics.ChatLLMFullMS)
		appendPositive(&firstTokenLatencies, metrics.FirstTokenMS)
		appendPositive(&jobEndToEndLatencies, metrics.JobEndToEndMS)
		appendPositive(&skillLatencies, metrics.SkillExecutionMS)
		appendPositive(&sandboxLatencies, metrics.SandboxStartupMS)
		appendPositive(&artifactLatencies, metrics.ArtifactLatencyMS)
		aggregate.ToolCallCount += metrics.ToolCallCount
		aggregate.ToolErrorCount += metrics.ToolErrorCount
		aggregate.SkillCount += metrics.SkillCount
		aggregate.SkillFailureCount += metrics.SkillFailureCount
		aggregate.LLMRequests += metrics.LLMRequests
		aggregate.LLMFailures += metrics.LLMFailures
		aggregate.InputTokens += metrics.InputTokens
		aggregate.OutputTokens += metrics.OutputTokens
		aggregate.TotalTokens += metrics.TotalTokens
		aggregate.EstimatedCostUSD += metrics.EstimatedCostUSD
		aggregate.HighRiskCount += metrics.RiskHighCount
		aggregate.MediumRiskCount += metrics.RiskMediumCount
		aggregate.LowRiskCount += metrics.RiskLowCount
		aggregate.ArtifactCount += metrics.ArtifactCount
		if metrics.EmptyOutput {
			aggregate.EmptyOutputCount++
		}
	}
	if aggregate.Total > 0 {
		total := float64(aggregate.Total)
		aggregate.SuccessRate = float64(aggregate.Passed) / total
		aggregate.FailureRate = float64(aggregate.Failed) / total
		aggregate.WarningRate = float64(aggregate.Warning) / total
	}
	if len(latencies) > 0 {
		aggregate.AverageLatencyMS = float64(latencyTotal) / float64(len(latencies))
		sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
		aggregate.P50LatencyMS = percentileLatency(latencies, 0.50)
		aggregate.P95LatencyMS = percentileLatency(latencies, 0.95)
		aggregate.P99LatencyMS = percentileLatency(latencies, 0.99)
	}
	aggregate.ChatLLMP95MS = percentileLatencySorted(chatLLMLatencies, 0.95)
	aggregate.FirstTokenP95MS = percentileLatencySorted(firstTokenLatencies, 0.95)
	aggregate.JobEndToEndP95MS = percentileLatencySorted(jobEndToEndLatencies, 0.95)
	aggregate.SkillP95MS = percentileLatencySorted(skillLatencies, 0.95)
	aggregate.SandboxP95MS = percentileLatencySorted(sandboxLatencies, 0.95)
	aggregate.ArtifactP95MS = percentileLatencySorted(artifactLatencies, 0.95)
	if aggregate.ToolCallCount > 0 {
		aggregate.ToolErrorRate = float64(aggregate.ToolErrorCount) / float64(aggregate.ToolCallCount)
	}
	if aggregate.SkillCount > 0 {
		aggregate.SkillFailureRate = float64(aggregate.SkillFailureCount) / float64(aggregate.SkillCount)
	}
	if aggregate.LLMRequests > 0 {
		aggregate.LLMErrorRate = float64(aggregate.LLMFailures) / float64(aggregate.LLMRequests)
	}
	aggregate.EstimatedCostUSD = roundEvaluationCost(aggregate.EstimatedCostUSD)
	return aggregate
}

func traceDuration(trace EvaluationTrace) time.Duration {
	if trace.Job != nil && trace.Job.StartedAt != nil && trace.Job.FinishedAt != nil {
		return trace.Job.FinishedAt.Sub(*trace.Job.StartedAt)
	}
	if trace.CompletedAt != nil && !trace.CreatedAt.IsZero() {
		return trace.CompletedAt.Sub(trace.CreatedAt)
	}
	return 0
}

func evaluationTraceMetricsFromMap(values map[string]any) EvaluationTraceMetrics {
	var metrics EvaluationTraceMetrics
	metrics.DurationMS = mapInt64(values, "duration_ms")
	metrics.ChatLLMFullMS = mapInt64(values, "chat_llm_full_latency_ms")
	metrics.FirstTokenMS = mapInt64(values, "first_token_latency_ms")
	metrics.JobEndToEndMS = mapInt64(values, "job_end_to_end_latency_ms")
	metrics.SkillExecutionMS = mapInt64(values, "skill_execution_latency_ms")
	metrics.SandboxStartupMS = mapInt64(values, "sandbox_startup_latency_ms")
	metrics.ArtifactLatencyMS = mapInt64(values, "artifact_generation_latency_ms")
	metrics.ToolCallCount = mapInt(values, "tool_call_count")
	metrics.ToolErrorCount = mapInt(values, "tool_error_count")
	metrics.SkillCount = mapInt(values, "skill_count")
	metrics.SkillFailureCount = mapInt(values, "skill_failure_count")
	metrics.LLMRequests = mapInt(values, "llm_requests")
	metrics.LLMFailures = mapInt(values, "llm_failures")
	metrics.InputTokens = mapInt(values, "input_tokens")
	metrics.OutputTokens = mapInt(values, "output_tokens")
	metrics.TotalTokens = mapInt(values, "total_tokens")
	metrics.EstimatedCostUSD = mapFloat(values, "estimated_cost_usd")
	metrics.RiskHighCount = mapInt(values, "risk_high_count")
	metrics.RiskMediumCount = mapInt(values, "risk_medium_count")
	metrics.RiskLowCount = mapInt(values, "risk_low_count")
	metrics.ArtifactCount = mapInt(values, "artifact_count")
	metrics.EmptyOutput, _ = values["empty_output"].(bool)
	return metrics
}

func evaluationTraceMetricsMap(metrics EvaluationTraceMetrics) map[string]any {
	return map[string]any{
		"duration_ms":                    metrics.DurationMS,
		"chat_llm_full_latency_ms":       metrics.ChatLLMFullMS,
		"first_token_latency_ms":         metrics.FirstTokenMS,
		"job_end_to_end_latency_ms":      metrics.JobEndToEndMS,
		"skill_execution_latency_ms":     metrics.SkillExecutionMS,
		"sandbox_startup_latency_ms":     metrics.SandboxStartupMS,
		"artifact_generation_latency_ms": metrics.ArtifactLatencyMS,
		"tool_call_count":                metrics.ToolCallCount,
		"tool_error_count":               metrics.ToolErrorCount,
		"skill_count":                    metrics.SkillCount,
		"skill_failure_count":            metrics.SkillFailureCount,
		"llm_requests":                   metrics.LLMRequests,
		"llm_failures":                   metrics.LLMFailures,
		"input_tokens":                   metrics.InputTokens,
		"output_tokens":                  metrics.OutputTokens,
		"total_tokens":                   metrics.TotalTokens,
		"estimated_cost_usd":             metrics.EstimatedCostUSD,
		"risk_high_count":                metrics.RiskHighCount,
		"risk_medium_count":              metrics.RiskMediumCount,
		"risk_low_count":                 metrics.RiskLowCount,
		"artifact_count":                 metrics.ArtifactCount,
		"empty_output":                   metrics.EmptyOutput,
	}
}

func evaluationAggregateMetricsMap(metrics EvaluationAggregateMetrics) map[string]any {
	return map[string]any{
		"total":                      metrics.Total,
		"passed":                     metrics.Passed,
		"failed":                     metrics.Failed,
		"warning":                    metrics.Warning,
		"success_rate":               metrics.SuccessRate,
		"failure_rate":               metrics.FailureRate,
		"warning_rate":               metrics.WarningRate,
		"average_latency_ms":         metrics.AverageLatencyMS,
		"p50_latency_ms":             metrics.P50LatencyMS,
		"p95_latency_ms":             metrics.P95LatencyMS,
		"p99_latency_ms":             metrics.P99LatencyMS,
		"chat_llm_full_p95_ms":       metrics.ChatLLMP95MS,
		"first_token_p95_ms":         metrics.FirstTokenP95MS,
		"job_end_to_end_p95_ms":      metrics.JobEndToEndP95MS,
		"skill_execution_p95_ms":     metrics.SkillP95MS,
		"sandbox_startup_p95_ms":     metrics.SandboxP95MS,
		"artifact_generation_p95_ms": metrics.ArtifactP95MS,
		"tool_call_count":            metrics.ToolCallCount,
		"tool_error_count":           metrics.ToolErrorCount,
		"tool_error_rate":            metrics.ToolErrorRate,
		"skill_count":                metrics.SkillCount,
		"skill_failure_count":        metrics.SkillFailureCount,
		"skill_failure_rate":         metrics.SkillFailureRate,
		"llm_requests":               metrics.LLMRequests,
		"llm_failures":               metrics.LLMFailures,
		"llm_error_rate":             metrics.LLMErrorRate,
		"input_tokens":               metrics.InputTokens,
		"output_tokens":              metrics.OutputTokens,
		"total_tokens":               metrics.TotalTokens,
		"estimated_cost_usd":         metrics.EstimatedCostUSD,
		"high_risk_count":            metrics.HighRiskCount,
		"medium_risk_count":          metrics.MediumRiskCount,
		"low_risk_count":             metrics.LowRiskCount,
		"artifact_count":             metrics.ArtifactCount,
		"empty_output_count":         metrics.EmptyOutputCount,
	}
}

func percentileLatency(values []int64, percentile float64) int64 {
	if len(values) == 0 {
		return 0
	}
	if len(values) == 1 {
		return values[0]
	}
	index := int(math.Ceil(percentile*float64(len(values)))) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(values) {
		index = len(values) - 1
	}
	return values[index]
}

func percentileLatencySorted(values []int64, percentile float64) int64 {
	if len(values) == 0 {
		return 0
	}
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	return percentileLatency(values, percentile)
}

func maxInt64(a, b int64) int64 {
	if b > a {
		return b
	}
	return a
}

func eventDataMap(event *JobEvent) map[string]any {
	if event == nil || len(event.Event.Data) == 0 {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(event.Event.Data, &out); err != nil {
		return nil
	}
	return out
}

func eventDataInt64(values map[string]any, key string) int64 {
	if len(values) == 0 {
		return 0
	}
	return mapInt64(values, key)
}

func mapInt(values map[string]any, key string) int {
	return int(mapInt64(values, key))
}

func mapInt64(values map[string]any, key string) int64 {
	switch value := values[key].(type) {
	case int:
		return int64(value)
	case int64:
		return value
	case int32:
		return int64(value)
	case float64:
		return int64(value)
	case jsonNumber:
		parsed, _ := value.Int64()
		return parsed
	default:
		return 0
	}
}

func mapFloat(values map[string]any, key string) float64 {
	switch value := values[key].(type) {
	case float64:
		return value
	case float32:
		return float64(value)
	case int:
		return float64(value)
	case int64:
		return float64(value)
	case jsonNumber:
		parsed, _ := value.Float64()
		return parsed
	default:
		return 0
	}
}

type jsonNumber interface {
	Int64() (int64, error)
	Float64() (float64, error)
}

func looksLikeToolError(output string) bool {
	text := strings.ToLower(strings.TrimSpace(output))
	if text == "" {
		return true
	}
	for _, marker := range []string{"error", "failed", "permission denied", "requires approval", "not found", "timed out"} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func roundEvaluationCost(value float64) float64 {
	return math.Round(value*1_000_000) / 1_000_000
}
