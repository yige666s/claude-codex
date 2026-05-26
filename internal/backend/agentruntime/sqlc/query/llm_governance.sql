-- name: InsertLLMUsage :exec
INSERT INTO agent_llm_usage (
	id,
	user_id,
	session_id,
	request_id,
	skill_name,
	provider,
	model,
	input_tokens,
	output_tokens,
	total_tokens,
	estimated_cost_usd,
	attempt,
	status,
	error,
	latency_ms,
	ttft_ms,
	created_at
) VALUES (
	$1,
	$2,
	$3,
	$4,
	$5,
	$6,
	$7,
	$8,
	$9,
	$10,
	$11,
	$12,
	$13,
	$14,
	$15,
	$16,
	$17
);

-- name: SumLLMUsageSuccess :one
SELECT
	COUNT(*)::bigint AS requests,
	COALESCE(SUM(input_tokens), 0)::bigint AS input_tokens,
	COALESCE(SUM(output_tokens), 0)::bigint AS output_tokens,
	COALESCE(SUM(total_tokens), 0)::bigint AS total_tokens,
	COALESCE(SUM(estimated_cost_usd), 0)::double precision AS estimated_cost_usd
FROM agent_llm_usage
WHERE user_id = $1
  AND created_at >= $2
  AND status = 'success';

-- name: SumLLMQuotaAdjustments :one
SELECT
	COALESCE(SUM(request_delta), 0)::bigint AS requests,
	COALESCE(SUM(input_token_delta), 0)::bigint AS input_tokens,
	COALESCE(SUM(output_token_delta), 0)::bigint AS output_tokens,
	COALESCE(SUM(total_token_delta), 0)::bigint AS total_tokens,
	COALESCE(SUM(estimated_cost_delta_usd), 0)::double precision AS estimated_cost_usd
FROM agent_llm_quota_adjustments
WHERE user_id = $1
  AND created_at >= $2;

-- name: SummarizeLLMUsageTotals :one
SELECT
	COUNT(*)::bigint AS requests,
	COALESCE(SUM(CASE WHEN status = 'success' THEN 1 ELSE 0 END), 0)::bigint AS successes,
	COALESCE(SUM(CASE WHEN status <> 'success' THEN 1 ELSE 0 END), 0)::bigint AS failures,
	COALESCE(SUM(input_tokens), 0)::bigint AS input_tokens,
	COALESCE(SUM(output_tokens), 0)::bigint AS output_tokens,
	COALESCE(SUM(total_tokens), 0)::bigint AS total_tokens,
	COALESCE(SUM(estimated_cost_usd), 0)::double precision AS estimated_cost_usd,
	COALESCE(AVG(NULLIF(latency_ms, 0)), 0)::double precision AS average_latency_ms
FROM agent_llm_usage
WHERE created_at >= sqlc.arg('since')
  AND (sqlc.arg('user_id')::text = '' OR user_id = sqlc.arg('user_id')::text);

-- name: ListLLMUsageGroups :many
SELECT
	provider,
	model,
	status,
	COUNT(*)::bigint AS requests,
	COALESCE(SUM(total_tokens), 0)::bigint AS total_tokens,
	COALESCE(SUM(estimated_cost_usd), 0)::double precision AS estimated_cost_usd
FROM agent_llm_usage
WHERE created_at >= sqlc.arg('since')
  AND (sqlc.arg('user_id')::text = '' OR user_id = sqlc.arg('user_id')::text)
GROUP BY provider, model, status
ORDER BY COALESCE(SUM(estimated_cost_usd), 0) DESC, COUNT(*) DESC;

-- name: ListRecentLLMUsage :many
SELECT
	id,
	user_id,
	session_id,
	request_id,
	skill_name,
	provider,
	model,
	input_tokens,
	output_tokens,
	total_tokens,
	estimated_cost_usd,
	attempt,
	status,
	error,
	latency_ms,
	ttft_ms,
	created_at
FROM agent_llm_usage
WHERE created_at >= sqlc.arg('since')
  AND (sqlc.arg('user_id')::text = '' OR user_id = sqlc.arg('user_id')::text)
ORDER BY created_at DESC
LIMIT sqlc.arg('limit_count')::int;

-- name: InsertLLMQuotaAdjustment :exec
INSERT INTO agent_llm_quota_adjustments (
	id,
	user_id,
	actor_user_id,
	reason,
	request_delta,
	input_token_delta,
	output_token_delta,
	total_token_delta,
	estimated_cost_delta_usd,
	created_at
) VALUES (
	$1,
	$2,
	$3,
	$4,
	$5,
	$6,
	$7,
	$8,
	$9,
	$10
);

-- name: ListLLMQuotaAdjustments :many
SELECT
	id,
	user_id,
	actor_user_id,
	reason,
	request_delta,
	input_token_delta,
	output_token_delta,
	total_token_delta,
	estimated_cost_delta_usd,
	created_at
FROM agent_llm_quota_adjustments
WHERE user_id = $1
  AND created_at >= $2
ORDER BY created_at DESC
LIMIT $3;
