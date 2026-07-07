#!/usr/bin/env node

const args = parseArgs(process.argv.slice(2));

const apiURL = trimTrailingSlash(args["api-url"] || process.env.AGENT_API_URL || "http://127.0.0.1:8081");
const adminToken = args["admin-token"] || process.env.AGENT_API_ADMIN_TOKEN || "";
const promptID = args["prompt-id"] || process.env.PROMPT_ID || "";
const promptVersion = args.version || args["prompt-version"] || process.env.PROMPT_VERSION || "";
const evalRunID = args["eval-run-id"] || process.env.EVAL_RUN_ID || "";
const maxFailures = Number(args["max-failures"] ?? process.env.PROMPT_GATE_MAX_FAILURES ?? "0");

main().catch((error) => {
  console.error(JSON.stringify({ ok: false, error: error.message }, null, 2));
  process.exit(1);
});

async function main() {
  if (!adminToken) throw new Error("AGENT_API_ADMIN_TOKEN or --admin-token is required");
  if (!promptID) throw new Error("--prompt-id is required");
  if (!promptVersion) throw new Error("--version is required");
  if (!evalRunID) throw new Error("--eval-run-id is required");
  if (!Number.isFinite(maxFailures) || maxFailures < 0) throw new Error("--max-failures must be a non-negative number");

  const report = await getJSON(`${apiURL}/v1/admin/ops/eval/runs/${encodeURIComponent(evalRunID)}?limit=500`, adminToken);
  const run = report.run || {};
  const results = Array.isArray(report.results) ? report.results : [];
  const failures = [];

  if (run.status !== "completed") failures.push(`run status is ${run.status || "unknown"}, expected completed`);
  if (String(run.threshold_status || "").toLowerCase() === "failed") failures.push("run threshold_status is failed");
  if ((Number(run.failed) || 0) > maxFailures) failures.push(`run has ${run.failed} failed result(s), max allowed ${maxFailures}`);
  if (!runMatchesPromptVersion(run, results, promptID, promptVersion)) failures.push(`run is not bound to ${promptID}@${promptVersion}`);

  const summary = {
    ok: failures.length === 0,
    eval_run_id: evalRunID,
    prompt_id: promptID,
    prompt_version: promptVersion,
    status: run.status || "",
    total: Number(run.total) || 0,
    passed: Number(run.passed) || 0,
    failed: Number(run.failed) || 0,
    warning: Number(run.warning) || 0,
    threshold_status: run.threshold_status || "",
    failures
  };
  console.log(JSON.stringify(summary, null, 2));
  if (failures.length > 0) process.exit(1);
}

async function getJSON(url, token) {
  const response = await fetch(url, {
    headers: {
      "Accept": "application/json",
      "X-Admin-Token": token
    }
  });
  const text = await response.text();
  let payload = {};
  try {
    payload = text ? JSON.parse(text) : {};
  } catch {
    throw new Error(`invalid JSON from ${url}: ${text.slice(0, 200)}`);
  }
  if (!response.ok) {
    throw new Error(payload.error || payload.message || `${response.status} ${response.statusText}`);
  }
  return payload;
}

function runMatchesPromptVersion(run, results, promptID, promptVersion) {
  const metrics = run.metrics && typeof run.metrics === "object" ? run.metrics : {};
  const metricPromptID = String(metrics.prompt_id || "").trim();
  const metricPromptVersion = String(metrics.prompt_version || "").trim();
  if (metricPromptID || metricPromptVersion) {
    return metricPromptID === promptID && metricPromptVersion === promptVersion;
  }
  let matched = 0;
  for (const result of results) {
    const resultPromptID = String(result.prompt_id || "").trim();
    const resultPromptVersion = String(result.prompt_version || "").trim();
    if (!resultPromptID && !resultPromptVersion) continue;
    if (resultPromptID !== promptID || resultPromptVersion !== promptVersion) return false;
    matched++;
  }
  return matched > 0;
}

function parseArgs(values) {
  const out = {};
  for (let i = 0; i < values.length; i++) {
    const item = values[i];
    if (!item.startsWith("--")) continue;
    const raw = item.slice(2);
    const eq = raw.indexOf("=");
    if (eq >= 0) {
      out[raw.slice(0, eq)] = raw.slice(eq + 1);
      continue;
    }
    const next = values[i + 1];
    if (next && !next.startsWith("--")) {
      out[raw] = next;
      i++;
    } else {
      out[raw] = "true";
    }
  }
  return out;
}

function trimTrailingSlash(value) {
  return String(value || "").replace(/\/+$/, "");
}
