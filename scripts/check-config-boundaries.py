#!/usr/bin/env python3
"""Reject Compose flags that duplicate the same AGENT_API setting in environment.

Secrets must never be passed as CLI flags. This intentionally checks the local
agentapi service; static non-duplicate bootstrap flags remain supported.
"""
from pathlib import Path
import re
import sys

ROOT = Path(__file__).resolve().parents[1]
compose = (ROOT / "deploy/local/docker-compose.yml").read_text(encoding="utf-8").splitlines()
app = compose.index("  agentapi:")
end = next(i for i in range(app + 1, len(compose)) if re.match(r"^  [a-zA-Z][^ ]*:$", compose[i]))
lines = compose[app:end]
env = {}
for line in lines:
    match = re.match(r"^      (AGENT_API_\w+): (.*)$", line)
    if match:
        env[match[1]] = match[2].strip('"')

duplicates = []
db_owned_flags = {
    "model", "llm-provider", "llm-model-routes", "llm-max-attempts",
    "llm-retry-backoff", "llm-chat-timeout", "llm-skill-timeout",
    "llm-daily-token-quota", "llm-daily-request-quota", "llm-daily-cost-quota-usd",
    "llm-input-cost-per-million", "llm-output-cost-per-million",
    "llm-failure-threshold", "llm-circuit-cooldown",
}
for index, line in enumerate(lines):
    pair = re.match(r"^      - -([a-z][a-z0-9-]*)$", line)
    inline = re.match(r'^      - "?--?([a-z][a-z0-9-]*)=([^"\s]+)"?$', line)
    flag = pair[1] if pair else inline[1] if inline else ""
    if not flag:
        continue
    key = "AGENT_API_" + flag.upper().replace("-", "_")
    if key in env:
        duplicates.append(key)
    if flag in db_owned_flags:
        duplicates.append(f"{flag} (DB-owned; use bootstrap env only)")
    if re.search(r"(?:SECRET|PASSWORD|TOKEN|API_KEY|ACCESS_KEY|_DSN)$", key):
        duplicates.append(f"{flag} (secret in process arguments)")
if duplicates:
    print("Duplicate environment/command configuration:", ", ".join(duplicates), file=sys.stderr)
    sys.exit(1)
print("Compose config boundary OK: no duplicated, secret, or DB-owned CLI settings")
