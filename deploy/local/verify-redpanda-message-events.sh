#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$repo_root"

compose_file="${COMPOSE_FILE:-deploy/local/docker-compose.yml}"
env_file="${ENV_FILE:-.env}"
api_port="${AGENT_API_PORT:-8081}"
api_base="${AGENT_API_BASE_URL:-http://127.0.0.1:${api_port}}"
mode="${AGENT_API_MESSAGE_EVENTS_BACKEND:-kafka}"
search_backend="${AGENT_API_MESSAGE_SEARCH_BACKEND:-elasticsearch}"
search_index="${AGENT_API_MESSAGE_SEARCH_INDEX:-agent_messages}"
qdrant_collection="${AGENT_API_MESSAGE_SEARCH_QDRANT_COLLECTION:-agent_messages}"
unique="redpanda-e2e-$(date +%s)-$RANDOM"
topic="${AGENT_API_MESSAGE_EVENTS_KAFKA_TOPIC:-agent.messages.e2e.${unique}}"
dlq_topic="${AGENT_API_MESSAGE_EVENTS_KAFKA_DLQ_TOPIC:-${topic}.dlq}"
message="redpanda pipeline verification ${unique}"

export AGENT_API_LLM_PROVIDER="${AGENT_API_LLM_PROVIDER:-simple}"
export AGENT_API_MODEL="${AGENT_API_MODEL:-simple}"
export AGENT_API_EMAIL_PROVIDER=""
export AGENT_API_EMAIL_VERIFICATION_REQUIRED=false
export AGENT_API_QDRANT_HOST="${AGENT_API_QDRANT_HOST:-127.0.0.1}"
export AGENT_API_QDRANT_PORT="${AGENT_API_QDRANT_PORT:-16333}"
export AGENT_API_MESSAGE_EVENTS_BACKEND="$mode"
export AGENT_API_MESSAGE_EVENTS_KAFKA_TOPIC="$topic"
export AGENT_API_MESSAGE_EVENTS_KAFKA_CONSUMER_ENABLED="${AGENT_API_MESSAGE_EVENTS_KAFKA_CONSUMER_ENABLED:-true}"
export AGENT_API_MESSAGE_EVENTS_KAFKA_DLQ_TOPIC="$dlq_topic"
export AGENT_API_MESSAGE_SEARCH_BACKEND="$search_backend"

compose() {
	docker compose --env-file "$env_file" --profile kafka --profile search -f "$compose_file" "$@"
}

json_get() {
	python3 -c 'import json,sys; data=json.load(sys.stdin); print(data'"$1"')'
}

wait_for() {
	local label="$1"
	local command="$2"
	local attempts="${3:-90}"
	echo "==> Waiting for ${label}"
	for _ in $(seq 1 "$attempts"); do
		if eval "$command" >/dev/null 2>&1; then
			return 0
		fi
		sleep 2
	done
	echo "Timed out waiting for ${label}" >&2
	return 1
}

topic_contains_needle() {
	local tmp_file
	local consumer_pid
	tmp_file="$(mktemp)"
	compose exec -T redpanda rpk topic consume "$topic" \
		--offset start \
		--num 50 \
		--fetch-max-wait 1s \
		--format '%v\n' >"$tmp_file" 2>/dev/null &
	consumer_pid=$!
	for _ in $(seq 1 8); do
		if grep -Fq "$unique" "$tmp_file"; then
			kill "$consumer_pid" >/dev/null 2>&1 || true
			wait "$consumer_pid" >/dev/null 2>&1 || true
			rm -f "$tmp_file"
			return 0
		fi
		if ! kill -0 "$consumer_pid" >/dev/null 2>&1; then
			break
		fi
		sleep 1
	done
	wait "$consumer_pid" >/dev/null 2>&1 || true
	if grep -Fq "$unique" "$tmp_file"; then
		rm -f "$tmp_file"
		return 0
	fi
	rm -f "$tmp_file"
	return 1
}

echo "==> Starting Redpanda message-events stack (${mode}, search=${search_backend})"
compose up -d --build redpanda elasticsearch qdrant postgres redis

wait_for "Redpanda broker" "compose exec -T redpanda rpk cluster health"

if [[ "$search_backend" == "hybrid" || "$search_backend" == "elasticsearch" || "$search_backend" == "fulltext" || "$search_backend" == "full-text" ]]; then
	wait_for "Elasticsearch" "compose exec -T elasticsearch curl -fsS 'http://127.0.0.1:9200'"
	echo "==> Preparing local Elasticsearch allocation"
	compose exec -T elasticsearch curl -fsS -X PUT 'http://127.0.0.1:9200/_cluster/settings' \
		-H 'Content-Type: application/json' \
		-d '{"persistent":{"cluster.routing.allocation.disk.threshold_enabled":false}}' >/dev/null
	compose exec -T elasticsearch curl -fsS -X PUT "http://127.0.0.1:9200/${search_index}/_settings" \
		-H 'Content-Type: application/json' \
		-d '{"index.blocks.read_only_allow_delete":null,"index.number_of_replicas":0}' >/dev/null 2>&1 || true
	compose exec -T elasticsearch curl -fsS -X POST 'http://127.0.0.1:9200/_cluster/reroute?retry_failed=true' >/dev/null 2>&1 || true
fi

echo "==> Ensuring Kafka topics exist"
compose exec -T redpanda rpk topic create "$topic" -p 3 -r 1 >/dev/null 2>&1 || true
if [[ -n "$dlq_topic" ]]; then
	compose exec -T redpanda rpk topic create "$dlq_topic" -p 3 -r 1 >/dev/null 2>&1 || true
fi
compose exec -T redpanda rpk topic describe "$topic" >/dev/null

compose up -d --build --force-recreate --no-deps agentapi
wait_for "AgentAPI health" "curl -fsS '${api_base}/healthz'"
wait_for "AgentAPI readiness" "curl -fsS '${api_base}/readyz'"

echo "==> Creating a real user session and chat message"
email="redpanda-${unique}@example.com"
auth_json="$(curl -fsS -X POST "${api_base}/v1/auth/register" \
	-H 'Content-Type: application/json' \
	-d "{\"email\":\"${email}\",\"password\":\"password123\",\"display_name\":\"Redpanda E2E\"}")"
token="$(json_get '["access_token"]' <<<"$auth_json")"
session_json="$(curl -fsS -X POST "${api_base}/v1/sessions" \
	-H "Authorization: Bearer ${token}" \
	-H 'Content-Type: application/json' \
	-d '{"working_dir":""}')"
session_id="$(json_get '["id"]' <<<"$session_json")"

curl -fsS -N -X POST "${api_base}/v1/sessions/${session_id}/messages" \
	-H "Authorization: Bearer ${token}" \
	-H 'Content-Type: application/json' \
	-d "{\"content\":\"${message}\"}" >/tmp/agentapi-redpanda-message.sse

if ! grep -q 'event: done' /tmp/agentapi-redpanda-message.sse; then
	echo "Chat SSE did not finish successfully" >&2
	cat /tmp/agentapi-redpanda-message.sse >&2
	exit 1
fi

echo "==> Verifying Redpanda received the message event"
wait_for "Kafka message event" "topic_contains_needle"

echo "==> Waiting for async search index to contain the message"
for _ in $(seq 1 90); do
	search_json="$(curl --max-time 15 -fsS "${api_base}/v1/search/messages?q=$(python3 -c 'import urllib.parse,sys; print(urllib.parse.quote(sys.argv[1]))' "$unique")&limit=10" \
		-H "Authorization: Bearer ${token}")"
	if python3 -c '
import json, sys
needle = sys.argv[1]
payload = json.load(sys.stdin)
items = payload.get("items") or []
sys.exit(0 if any(needle in (item.get("content") or item.get("snippet") or "") for item in items) else 1)
' "$unique" <<<"$search_json"; then
		break
	fi
	sleep 2
done

search_json="$(curl --max-time 15 -fsS "${api_base}/v1/search/messages?q=$(python3 -c 'import urllib.parse,sys; print(urllib.parse.quote(sys.argv[1]))' "$unique")&limit=10" \
	-H "Authorization: Bearer ${token}")"
python3 -c '
import json, sys
needle = sys.argv[1]
payload = json.load(sys.stdin)
items = payload.get("items") or []
if not any(needle in (item.get("content") or item.get("snippet") or "") for item in items):
    print(json.dumps(payload, ensure_ascii=False, indent=2), file=sys.stderr)
    raise SystemExit("search did not return the Redpanda-indexed message")
' "$unique" <<<"$search_json"

if [[ "$search_backend" == "hybrid" || "$search_backend" == "elasticsearch" || "$search_backend" == "fulltext" || "$search_backend" == "full-text" ]]; then
	echo "==> Verifying Elasticsearch index"
	wait_for "Elasticsearch document" "compose exec -T elasticsearch curl -fsS 'http://127.0.0.1:9200/${search_index}/_search?q=${unique}' | grep -q '${unique}'"
fi

if [[ "$search_backend" == "hybrid" || "$search_backend" == "semantic" ]]; then
	echo "==> Verifying Qdrant collection is available"
	compose exec -T qdrant bash -lc "curl -fsS http://127.0.0.1:6333/collections/${qdrant_collection}" >/dev/null
fi

if [[ -n "$dlq_topic" ]]; then
	echo "==> Checking DLQ topic"
	compose exec -T redpanda rpk topic describe "$dlq_topic" >/dev/null
fi

echo "==> Redpanda message-events verification complete"
echo "Mode: ${mode}"
echo "Search backend: ${search_backend}"
echo "Session: ${session_id}"
echo "Needle: ${unique}"
