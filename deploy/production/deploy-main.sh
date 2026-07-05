#!/usr/bin/env bash
set -euo pipefail

repo_url="${AGENTAPI_REPO_URL:-https://github.com/yige666s/claude-codex.git}"
branch="${AGENTAPI_BRANCH:-main}"
app_dir="${AGENTAPI_APP_DIR:-/opt/agentapi/repo}"
env_file="${AGENTAPI_ENV_FILE:-/opt/agentapi/.env}"
compose_file="${AGENTAPI_COMPOSE_FILE:-deploy/local/docker-compose.yml}"
health_url="${AGENTAPI_HEALTH_URL:-http://127.0.0.1:${AGENT_API_PORT:-8081}/readyz}"
skip_healthcheck="${AGENTAPI_SKIP_HEALTHCHECK:-0}"
prune_images="${AGENTAPI_PRUNE_IMAGES:-0}"
deploy_mode="${AGENTAPI_DEPLOY_MODE:-build}"

require() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 127
  fi
}

require git
require docker
require curl

run_with_heartbeat() {
  local heartbeat_seconds="${AGENTAPI_DEPLOY_HEARTBEAT_SECONDS:-30}"
  "$@" &
  local pid=$!
  while kill -0 "$pid" >/dev/null 2>&1; do
    sleep "$heartbeat_seconds"
    if kill -0 "$pid" >/dev/null 2>&1; then
      echo "deployment still running: $*"
    fi
  done
  set +e
  wait "$pid"
  local status=$?
  set -e
  return "$status"
}

mkdir -p "$(dirname "$app_dir")"

if [ ! -d "$app_dir/.git" ]; then
  rm -rf "$app_dir"
  git clone --branch "$branch" "$repo_url" "$app_dir"
fi

git config --global --add safe.directory "$app_dir" || true
cd "$app_dir"
git remote set-url origin "$repo_url"
git fetch --prune origin "$branch"
git reset --hard "origin/$branch"
git clean -ffd

compose_args=(-f "$compose_file")
if [ -f "$env_file" ]; then
  compose_args+=(--env-file "$env_file")
else
  echo "warning: env file not found: $env_file; using process environment only" >&2
fi

env_value() {
  local key="$1"
  local value="${!key:-}"
  if [ -n "$value" ]; then
    printf '%s' "$value"
    return 0
  fi
  if [ -f "$env_file" ]; then
    awk -v key="$key" '
      BEGIN { prefix = key "=" }
      index($0, prefix) == 1 {
        value = substr($0, length(prefix) + 1)
        gsub(/^["'\'']|["'\'']$/, "", value)
        print value
        exit
      }
    ' "$env_file"
  fi
}

append_profile() {
  local profile="$1"
  local existing=" ${compose_profiles[*]-} "
  if [[ "$existing" != *" $profile "* ]]; then
    compose_profiles+=("$profile")
  fi
}

compose_profiles=()
if [ -n "${AGENTAPI_COMPOSE_PROFILES:-}" ]; then
  IFS=',' read -r -a requested_profiles <<<"$AGENTAPI_COMPOSE_PROFILES"
  for profile in "${requested_profiles[@]}"; do
    profile="$(printf '%s' "$profile" | xargs)"
    if [ -n "$profile" ]; then
      append_profile "$profile"
    fi
  done
fi

message_events_backend="$(env_value AGENT_API_MESSAGE_EVENTS_BACKEND | tr '[:upper:]' '[:lower:]')"
case "$message_events_backend" in
  kafka|dual)
    append_profile kafka
    ;;
esac

message_search_backend="$(env_value AGENT_API_MESSAGE_SEARCH_BACKEND | tr '[:upper:]' '[:lower:]')"
case "$message_search_backend" in
  elasticsearch|opensearch|semantic|hybrid)
    append_profile search
    ;;
esac

for profile in "${compose_profiles[@]}"; do
  compose_args+=(--profile "$profile")
done

if [ "${#compose_profiles[@]}" -gt 0 ]; then
  echo "docker compose profiles: ${compose_profiles[*]}"
fi

case "$deploy_mode" in
  build)
    run_with_heartbeat docker compose "${compose_args[@]}" up -d --build
    ;;
  pull)
    run_with_heartbeat docker compose "${compose_args[@]}" pull agentapi agentweb
    run_with_heartbeat docker compose "${compose_args[@]}" up -d --no-build
    ;;
  *)
    echo "unsupported AGENTAPI_DEPLOY_MODE: $deploy_mode; expected build or pull" >&2
    exit 2
    ;;
esac

if [ "$prune_images" = "1" ]; then
  docker image prune -f
fi

if [ "$skip_healthcheck" = "1" ]; then
  echo "deployment completed; healthcheck skipped"
  exit 0
fi

for attempt in $(seq 1 60); do
  if curl -fsS "$health_url" >/tmp/agentapi-readyz.json; then
    echo "deployment healthy: $health_url"
    cat /tmp/agentapi-readyz.json
    echo
    exit 0
  fi
  sleep 2
done

echo "deployment healthcheck failed: $health_url" >&2
docker compose "${compose_args[@]}" ps >&2 || true
docker compose "${compose_args[@]}" logs --tail=200 agentapi >&2 || true
exit 1
