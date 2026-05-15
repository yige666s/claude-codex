#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$repo_root"

compose_file="deploy/local/docker-compose.yml"
project="${COMPOSE_PROJECT_NAME:-claude-codex-message-verify}"

export COMPOSE_PROJECT_NAME="$project"
export AGENT_API_POSTGRES_DB="${AGENT_API_POSTGRES_DB:-agentapi}"
export AGENT_API_POSTGRES_USER="${AGENT_API_POSTGRES_USER:-agentapi}"
export AGENT_API_POSTGRES_PASSWORD="${AGENT_API_POSTGRES_PASSWORD:-agentapi}"
export AGENT_API_POSTGRES_PORT="${AGENT_API_POSTGRES_PORT:-15432}"
export AGENT_API_REDIS_PORT="${AGENT_API_REDIS_PORT:-16379}"
export AGENT_API_MINIO_PORT="${AGENT_API_MINIO_PORT:-19000}"
export AGENT_API_MINIO_CONSOLE_PORT="${AGENT_API_MINIO_CONSOLE_PORT:-19001}"
export AGENT_API_PORT="${AGENT_API_PORT:-18081}"
export AGENT_WEB_PORT="${AGENT_WEB_PORT:-18080}"
export AGENT_API_ARTIFACT_STORE="${AGENT_API_ARTIFACT_STORE:-s3}"
export AGENT_API_ARTIFACT_S3_ENDPOINT="${AGENT_API_ARTIFACT_S3_ENDPOINT:-minio:9000}"
export AGENT_API_ARTIFACT_S3_ACCESS_KEY="${AGENT_API_ARTIFACT_S3_ACCESS_KEY:-minioadmin}"
export AGENT_API_ARTIFACT_S3_SECRET_KEY="${AGENT_API_ARTIFACT_S3_SECRET_KEY:-minioadmin}"
export AGENT_API_ARTIFACT_S3_BUCKET="${AGENT_API_ARTIFACT_S3_BUCKET:-agentapi}"
export AGENT_API_ARTIFACT_S3_PREFIX="${AGENT_API_ARTIFACT_S3_PREFIX:-message-verify}"
export AGENT_API_ARTIFACT_S3_SSL="${AGENT_API_ARTIFACT_S3_SSL:-false}"
export AGENT_API_RATE_LIMIT_BACKEND="${AGENT_API_RATE_LIMIT_BACKEND:-redis}"
export AGENT_API_LLM_PROVIDER="${AGENT_API_VERIFY_LLM_PROVIDER:-custom}"
export AGENT_API_LLM_BASE_URL="${AGENT_API_VERIFY_LLM_BASE_URL:-http://127.0.0.1:9/v1}"
export AGENT_API_MODEL="${AGENT_API_VERIFY_LLM_MODEL:-local-test-model}"
export OPENAI_API_KEY="${AGENT_API_VERIFY_LLM_API_KEY:-local-verification-key}"
export AGENT_API_MESSAGE_SEARCH_BACKEND="${AGENT_API_MESSAGE_SEARCH_BACKEND:-sql}"

pg_dsn="postgres://${AGENT_API_POSTGRES_USER}:${AGENT_API_POSTGRES_PASSWORD}@localhost:${AGENT_API_POSTGRES_PORT}/${AGENT_API_POSTGRES_DB}?sslmode=disable"
api_base="http://127.0.0.1:${AGENT_API_PORT}"

echo "==> Starting local verification stack (${project})"
docker compose -f "$compose_file" up -d --build postgres redis minio agentapi

echo "==> Waiting for AgentAPI health"
for _ in $(seq 1 90); do
	if curl -fsS "${api_base}/healthz" >/dev/null; then
		break
	fi
	sleep 2
done
curl -fsS "${api_base}/healthz" >/dev/null
curl -fsS "${api_base}/readyz" >/tmp/agentapi-readyz.json || {
	cat /tmp/agentapi-readyz.json >&2 || true
	exit 1
}

echo "==> Running Postgres-backed message module integration tests"
AGENT_RUNTIME_TEST_PG_DSN="$pg_dsn" go test ./internal/backend/agentruntime -run 'TestSQL(SessionStoreSyncsMessages|MessageModuleIntegration)$' -count=1

echo "==> Exercising HTTP session and attachment paths"
email="verify+$(date +%s)@example.com"
auth_json="$(curl -fsS -X POST "${api_base}/v1/auth/register" \
	-H 'Content-Type: application/json' \
	-d "{\"email\":\"${email}\",\"password\":\"password123\",\"display_name\":\"Message Verify\"}")"
token="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["access_token"])' <<<"$auth_json")"
session_json="$(curl -fsS -X POST "${api_base}/v1/sessions" \
	-H "Authorization: Bearer ${token}" \
	-H 'Content-Type: application/json' \
	-d '{"working_dir":""}')"
session_id="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["id"])' <<<"$session_json")"
attachment_json="$(curl -fsS -X POST "${api_base}/v1/attachments" \
	-H "Authorization: Bearer ${token}" \
	-F "session_id=${session_id}" \
	-F "file=@Agent 系统消息存储技术方案.md;type=text/markdown")"
attachment_id="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["id"])' <<<"$attachment_json")"
curl -fsS "${api_base}/v1/attachments/${attachment_id}" -H "Authorization: Bearer ${token}" >/dev/null
curl -fsS "${api_base}/v1/search/messages?q=verify&limit=5" -H "Authorization: Bearer ${token}" >/dev/null

echo "==> Verification complete"
echo "AgentAPI: ${api_base}"
echo "Postgres DSN: ${pg_dsn}"
