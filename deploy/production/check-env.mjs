#!/usr/bin/env node
import { readFileSync } from "node:fs";

const envFile = process.argv[2] || process.env.AGENTAPI_ENV_FILE || "/opt/agentapi/.env";
const findings = [];
const env = { ...process.env, ...readEnvFile(envFile) };

checkRequired("core", [
  "AGENT_API_SQL_DRIVER",
  "AGENT_API_SQL_DIALECT",
  "AGENT_API_SQL_DSN",
  "AGENT_API_ENABLE_USER_SYSTEM",
  "AGENT_API_AUTH_MODE",
  "AGENT_API_JWT_SECRET",
  "AGENT_API_ADMIN_TOKEN"
]);
checkValue("core", "AGENT_API_SQL_DRIVER", (value) => value === "pgx", "must be pgx for Postgres deployments");
checkValue("core", "AGENT_API_SQL_DIALECT", (value) => value === "postgres", "must be postgres");
checkValue("core", "AGENT_API_STORE_BACKEND", (value) => (value || "").toLowerCase() === "sql", "must be sql for test deployments");

checkRequired("security", [
  "AGENT_API_CORS_ALLOWED_ORIGINS",
  "AGENT_API_CORS_ALLOW_CREDENTIALS",
  "AGENT_API_JWT_ISSUER",
  "AGENT_API_JWT_AUDIENCE"
]);
checkSecret("security", "AGENT_API_JWT_SECRET", 32);
checkSecret("security", "AGENT_API_ADMIN_TOKEN", 32);
if (truthy(env.AGENT_API_CORS_ALLOW_CREDENTIALS) && !env.AGENT_API_CORS_ALLOWED_ORIGINS?.trim()) {
  fail("security", "AGENT_API_CORS_ALLOWED_ORIGINS", "credentialed CORS requires explicit allowed origins");
}
if (env.VITE_AGENT_API_BASE_URL?.trim() && env.AGENT_API_CORS_ALLOWED_ORIGINS?.trim()) {
  warn("security", "VITE_AGENT_API_BASE_URL", "split-origin frontend builds must match AGENT_API_CORS_ALLOWED_ORIGINS");
}
if (truthy(env.AGENT_API_CSRF_ENABLED)) {
  checkRequired("security", ["AGENT_API_CSRF_COOKIE_NAME", "AGENT_API_CSRF_HEADER_NAME"]);
}
if (env.AGENT_API_SESSION_COOKIE_SECRET?.trim()) {
  checkSecret("security", "AGENT_API_SESSION_COOKIE_SECRET", 32);
  checkValue("security", "AGENT_API_SESSION_COOKIE_SECURE", truthy, "must be true for HTTPS cookie auth");
}

checkRequired("redis", ["AGENT_API_REDIS_URL"]);
if (env.AGENT_API_RATE_LIMIT_BACKEND === "redis") checkRequired("redis", ["AGENT_API_REDIS_URL"]);
if (env.AGENT_API_MESSAGE_CONTEXT_CACHE_BACKEND === "redis") checkRequired("redis", ["AGENT_API_MESSAGE_CONTEXT_CACHE_REDIS_URL"]);
if (env.AGENT_API_SESSION_LIST_CACHE_BACKEND === "redis") checkRequired("redis", ["AGENT_API_SESSION_LIST_CACHE_REDIS_URL"]);
if (env.AGENT_API_MESSAGE_SEQUENCE_BACKEND === "redis") checkRequired("redis", ["AGENT_API_MESSAGE_SEQUENCE_REDIS_URL"]);
checkRequired("job-queue", [
  "AGENT_API_JOB_QUEUE_REDIS_URL",
  "AGENT_API_JOB_QUEUE_STREAM",
  "AGENT_API_JOB_QUEUE_CONSUMER_GROUP",
  "AGENT_API_JOB_WORKER_ENABLED",
  "AGENT_API_JOB_EVENT_FANOUT_ENABLED",
  "AGENT_API_JOB_EVENT_FANOUT_CHANNEL"
]);

if ((env.AGENT_API_ARTIFACT_STORE || "").toLowerCase() === "s3") {
  checkRequired("object-storage", [
    "AGENT_API_ARTIFACT_S3_ENDPOINT",
    "AGENT_API_ARTIFACT_S3_ACCESS_KEY",
    "AGENT_API_ARTIFACT_S3_SECRET_KEY",
    "AGENT_API_ARTIFACT_S3_BUCKET",
    "AGENT_API_ARTIFACT_S3_PREFIX"
  ]);
}

const searchBackend = (env.AGENT_API_MESSAGE_SEARCH_BACKEND || "sql").toLowerCase();
if (["elasticsearch", "opensearch", "hybrid"].includes(searchBackend)) {
  checkRequired("search", ["AGENT_API_MESSAGE_SEARCH_ENDPOINT", "AGENT_API_MESSAGE_SEARCH_INDEX"]);
}
if (["semantic", "hybrid"].includes(searchBackend)) {
  checkRequired("search", [
    "AGENT_API_MESSAGE_SEARCH_QDRANT_ENDPOINT",
    "AGENT_API_MESSAGE_SEARCH_QDRANT_COLLECTION",
    "AGENT_API_MESSAGE_SEARCH_EMBEDDING_PROVIDER",
    "AGENT_API_MESSAGE_SEARCH_EMBEDDING_MODEL"
  ]);
}

const provider = (env.AGENT_API_LLM_PROVIDER || "").toLowerCase();
if (provider === "vertex") {
  checkRequired("vertex", ["VERTEX_PROJECT_ID", "VERTEX_LOCATION"]);
  checkAny("vertex", "Vertex credentials", ["GOOGLE_APPLICATION_CREDENTIALS_JSON", "GOOGLE_APPLICATION_CREDENTIALS", "VERTEX_SERVICE_ACCOUNT_JSON", "VERTEX_SERVICE_ACCOUNT_FILE", "VERTEX_ACCESS_TOKEN"]);
} else if (provider === "gemini") {
  checkAny("llm", "Gemini key", ["GEMINI_API_KEY", "GOOGLE_API_KEY", "AGENT_API_LLM_API_KEY"]);
} else if (provider === "openai") {
  checkAny("llm", "OpenAI key", ["OPENAI_API_KEY", "AGENT_API_LLM_API_KEY"]);
} else if (provider === "deepseek") {
  checkAny("llm", "DeepSeek key", ["DEEPSEEK_API_KEY", "AGENT_API_LLM_API_KEY"]);
} else if (provider === "nvidia" || provider === "nim") {
  checkAny("llm", "NVIDIA key", ["NVIDIA_API_KEY", "NGC_API_KEY", "AGENT_API_LLM_API_KEY", "AGENT_API_MESSAGE_SEARCH_EMBEDDING_API_KEY"]);
} else if (provider === "shortapi" || provider === "short") {
  checkAny("llm", "ShortAPI key", ["SHORTAPI_KEY", "AGENT_API_LLM_API_KEY"]);
} else if (provider === "anthropic") {
  checkAny("llm", "Anthropic key", ["ANTHROPIC_API_KEY", "CLAUDE_API_KEY", "AGENT_API_LLM_API_KEY"]);
}

if (truthy(env.AGENT_API_LIVE_ENABLED)) {
  checkRequired("live", ["AGENT_API_LIVE_PROVIDER", "AGENT_API_LIVE_MODEL"]);
  const liveProvider = (env.AGENT_API_LIVE_PROVIDER || "").toLowerCase();
  if (liveProvider === "xai") {
    checkAny("live", "xAI live key", ["AGENT_API_LIVE_XAI_API_KEY", "XAI_API_KEY"]);
    checkAny("live", "xAI live base URL", ["AGENT_API_LIVE_XAI_BASE_URL", "XAI_LIVE_BASE_URL"]);
  } else if (liveProvider === "vertex") {
    checkRequired("live", ["AGENT_API_LIVE_VERTEX_LOCATION"]);
    checkAny("live", "Live Vertex project", ["AGENT_API_LIVE_VERTEX_PROJECT_ID", "VERTEX_PROJECT_ID", "GOOGLE_CLOUD_PROJECT"]);
    checkAny("live", "Live Vertex credentials", ["GOOGLE_APPLICATION_CREDENTIALS_JSON", "GOOGLE_APPLICATION_CREDENTIALS", "VERTEX_SERVICE_ACCOUNT_JSON", "VERTEX_SERVICE_ACCOUNT_FILE", "VERTEX_ACCESS_TOKEN"]);
  }
}

if (truthy(env.AGENT_API_BACKUP_ENABLED)) {
  checkRequired("backup", [
    "AGENT_API_BACKUP_POSTGRES_DSN",
    "AGENT_API_BACKUP_S3_ENDPOINT",
    "AGENT_API_BACKUP_S3_BUCKET",
    "AGENT_API_BACKUP_S3_ACCESS_KEY",
    "AGENT_API_BACKUP_S3_SECRET_KEY",
    "AGENT_API_BACKUP_RETENTION_DAYS"
  ]);
} else {
  warn("backup", "AGENT_API_BACKUP_ENABLED", "backups are not marked enabled in this env file");
}

const errors = findings.filter((item) => item.level === "error");
for (const finding of findings) {
  const icon = finding.level === "error" ? "ERROR" : "WARN";
  console.log(`${icon} [${finding.category}] ${finding.name}: ${finding.message}`);
}
if (errors.length) {
  console.error(`Production env check failed with ${errors.length} error(s).`);
  process.exit(1);
}
console.log("Production env check passed.");

function readEnvFile(path) {
  try {
    const contents = readFileSync(path, "utf8");
    const result = {};
    for (const rawLine of contents.split(/\r?\n/)) {
      const line = rawLine.trim();
      if (!line || line.startsWith("#")) continue;
      const match = /^([A-Za-z_][A-Za-z0-9_]*)=(.*)$/.exec(line);
      if (!match) continue;
      result[match[1]] = unquote(match[2]);
    }
    return result;
  } catch (error) {
    fail("core", path, `could not read env file: ${error.message}`);
    return {};
  }
}

function unquote(value) {
  const trimmed = value.trim();
  if ((trimmed.startsWith('"') && trimmed.endsWith('"')) || (trimmed.startsWith("'") && trimmed.endsWith("'"))) {
    return trimmed.slice(1, -1);
  }
  return trimmed;
}

function checkRequired(category, names) {
  for (const name of names) {
    if (!env[name]?.trim()) fail(category, name, "is required");
    if (isPlaceholder(env[name])) fail(category, name, "still contains a placeholder value");
  }
}

function checkAny(category, label, names) {
  if (!names.some((name) => env[name]?.trim() && !isPlaceholder(env[name]))) {
    fail(category, label, `set one of ${names.join(", ")}`);
  }
}

function checkSecret(category, name, minLength) {
  const value = env[name] || "";
  if (!value.trim()) return;
  if (isPlaceholder(value)) fail(category, name, "still contains a placeholder value");
  if (value.length < minLength) fail(category, name, `must be at least ${minLength} characters`);
}

function checkValue(category, name, predicate, message) {
  const value = env[name] || "";
  if (value && !predicate(value)) fail(category, name, message);
}

function isPlaceholder(value = "") {
  return /REPLACE_ME|REPLACE_WITH|example\.com|example-gcp-project/i.test(value);
}

function truthy(value = "") {
  return /^(1|true|yes|y|on)$/i.test(String(value).trim());
}

function fail(category, name, message) {
  findings.push({ level: "error", category, name, message });
}

function warn(category, name, message) {
  findings.push({ level: "warn", category, name, message });
}
