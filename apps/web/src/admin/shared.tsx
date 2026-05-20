import { ReactNode, useEffect, useRef, useState } from "react";
import { AlertCircle, Archive, Clock, RefreshCw, ShieldCheck, Sparkles, X } from "lucide-react";
import { ApiClient, ApiError } from "../api/client";
import { Badge } from "../components/ui/badge";
import { Button } from "../components/ui/button";
import { Input } from "../components/ui/input";
import { Textarea } from "../components/ui/textarea";
import { Tabs, TabsList, TabsTrigger } from "../components/ui/tabs";
import { AdminStatusBadge } from "./ui";
import type { AdminSkill, AuditLogRecord, EvaluationResult, EvaluationReview, EvaluationRun, RiskEvent, Skill, LLMGovernanceConfig, SkillPolicyConfig } from "../types";

export const terminalJobs = new Set(["succeeded", "failed", "cancelled"]);

export function StatusBadge({ value }: { value: string }) {
  return <AdminStatusBadge value={value} />;
}

export function llmConfigDraftFromConfig(config: LLMGovernanceConfig): Record<string, string> {
  const keys: Array<keyof LLMGovernanceConfig> = [
    "provider",
    "model",
    "vertex_location",
    "model_routes",
    "max_attempts",
    "retry_backoff_ms",
    "chat_timeout_ms",
    "skill_timeout_ms",
    "daily_token_quota",
    "daily_request_quota",
    "daily_cost_quota_usd",
    "input_cost_per_million",
    "output_cost_per_million",
    "failure_threshold",
    "circuit_cooldown_seconds"
  ];
  return Object.fromEntries(keys.map((key) => [key, config[key] == null ? "" : String(config[key])]));
}

export function llmConfigFromDraft(draft: Record<string, string>): LLMGovernanceConfig {
  type IntegerLLMConfigKey = "max_attempts" | "retry_backoff_ms" | "chat_timeout_ms" | "skill_timeout_ms" | "daily_token_quota" | "daily_request_quota" | "failure_threshold" | "circuit_cooldown_seconds";
  type DecimalLLMConfigKey = "daily_cost_quota_usd" | "input_cost_per_million" | "output_cost_per_million";
  const integerKeys: IntegerLLMConfigKey[] = [
    "max_attempts",
    "retry_backoff_ms",
    "chat_timeout_ms",
    "skill_timeout_ms",
    "daily_token_quota",
    "daily_request_quota",
    "failure_threshold",
    "circuit_cooldown_seconds"
  ];
  const decimalKeys: DecimalLLMConfigKey[] = [
    "daily_cost_quota_usd",
    "input_cost_per_million",
    "output_cost_per_million"
  ];
  const next: LLMGovernanceConfig = {};
  const model = String(draft.model || "").trim();
  if (model) next.model = model;
  for (const key of integerKeys) {
    const raw = String(draft[key] || "").trim();
    if (!raw) continue;
    const value = Number(raw);
    if (!Number.isInteger(value)) throw new Error(`${key} must be an integer`);
    next[key] = value;
  }
  for (const key of decimalKeys) {
    const raw = String(draft[key] || "").trim();
    if (!raw) continue;
    const value = Number(raw);
    if (!Number.isFinite(value)) throw new Error(`${key} must be a number`);
    next[key] = value;
  }
  return next;
}

export function modelOptionLocation(config: LLMGovernanceConfig | undefined, model: string | undefined): string {
  const selected = String(model || "").trim();
  return config?.allowed_models?.find((option) => option.id === selected)?.vertex_location || "";
}

export function AdminMetric({ label, value }: { label: string; value: string }) {
  return (
    <div className="admin-metric">
      <small>{label}</small>
      <strong>{value}</strong>
    </div>
  );
}



type SkillPolicyDraft = {
  allowedTools: string;
  allowedEnv: string;
  networkAllowlist: string;
  artifactContentTypes: string;
  shellTimeout: string;
  sandboxRunner: string;
  sandboxImage: string;
  sandboxNetwork: string;
  sandboxMemory: string;
  sandboxCpus: string;
  sandboxPidsLimit: string;
  sandboxTmpfsSize: string;
  sandboxMaxOutputBytes: string;
};

const emptySkillPolicyDraft: SkillPolicyDraft = {
  allowedTools: "",
  allowedEnv: "",
  networkAllowlist: "",
  artifactContentTypes: "",
  shellTimeout: "",
  sandboxRunner: "",
  sandboxImage: "",
  sandboxNetwork: "",
  sandboxMemory: "",
  sandboxCpus: "",
  sandboxPidsLimit: "",
  sandboxTmpfsSize: "",
  sandboxMaxOutputBytes: ""
};

export function SkillPolicyModal({
  api,
  skill,
  adminToken,
  onAdminTokenChange,
  onSaved,
  onClose
}: {
  api: ApiClient;
  skill: Skill;
  adminToken: string;
  onAdminTokenChange: (token: string) => void;
  onSaved: (skill: AdminSkill) => void;
  onClose: () => void;
}) {
  const modalRef = useFocusTrap<HTMLElement>(true, onClose);
  const [loadedSkill, setLoadedSkill] = useState<AdminSkill | null>(null);
  const [basePolicy, setBasePolicy] = useState<Record<string, unknown>>({});
  const [draft, setDraft] = useState<SkillPolicyDraft>(emptySkillPolicyDraft);
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");

  const updateDraft = (key: keyof SkillPolicyDraft, value: string) => {
    setDraft((current) => ({ ...current, [key]: value }));
  };

  const loadPolicy = async () => {
    const token = adminToken.trim();
    if (!token) {
      setError("Admin token is required.");
      return;
    }
    setLoading(true);
    setError("");
    try {
      const adminSkills = await api.adminSkills(token);
      const record = adminSkills.find((item) => item.name === skill.name);
      if (!record) throw new Error(`/${skill.name} was not found in the admin registry.`);
      const policy = skillPolicyFromMetadata(record.metadata);
      setLoadedSkill(record);
      setBasePolicy(policy);
      setDraft(policyDraftFromConfig(policy));
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    setLoadedSkill(null);
    setBasePolicy({});
    setDraft(emptySkillPolicyDraft);
    setError("");
    if (adminToken.trim()) {
      void loadPolicy();
    }
  }, [skill.name]);

  const savePolicy = async () => {
    const token = adminToken.trim();
    if (!token) {
      setError("Admin token is required.");
      return;
    }
    if (!loadedSkill) {
      setError("Load the current registry policy before saving.");
      return;
    }
    setSaving(true);
    setError("");
    try {
      const policy = skillPolicyConfigFromDraft(basePolicy, draft);
      const updated = await api.updateAdminSkill(skill.name, token, { metadata: { policy } });
      setLoadedSkill(updated);
      const nextPolicy = skillPolicyFromMetadata(updated.metadata);
      setBasePolicy(nextPolicy);
      setDraft(policyDraftFromConfig(nextPolicy));
      onSaved(updated);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="modal-backdrop">
      <section className="skill-policy-modal" ref={modalRef} role="dialog" aria-modal="true" aria-labelledby="skill-policy-title" tabIndex={-1}>
        <header>
          <div className="skill-modal-heading">
            <SkillGlyph skill={skill} />
            <div>
              <h2 id="skill-policy-title">Policy for /{skill.name}</h2>
              <small>{loadedSkill?.status ? `Registry status: ${loadedSkill.status}` : "Admin registry policy"}</small>
            </div>
          </div>
          <Button className="icon ghost" onClick={onClose} aria-label="Close skill policy" title="Close">
            <X size={18} />
          </Button>
        </header>
        <div className="skill-policy-body">
          <label className="policy-field policy-token">
            <span>Admin token</span>
            <Input
              type="password"
              value={adminToken}
              onChange={(event) => onAdminTokenChange(event.currentTarget.value)}
              placeholder="AGENT_API_ADMIN_TOKEN"
              autoComplete="off"
            />
            <Button type="button" className="skill-action" onClick={loadPolicy} disabled={loading}>
              {loading ? <RefreshCw size={15} /> : <ShieldCheck size={15} />}
              <span>{loading ? "Loading" : "Load"}</span>
            </Button>
          </label>
          {error && <div className="policy-error"><AlertCircle size={15} /> {error}</div>}
          <section className="policy-section">
            <h3>Permissions</h3>
            <PolicyTextArea label="Allowed tools" value={draft.allowedTools} onChange={(value) => updateDraft("allowedTools", value)} placeholder={"Read\nWrite\nBash"} />
            <PolicyTextArea label="Allowed env" value={draft.allowedEnv} onChange={(value) => updateDraft("allowedEnv", value)} placeholder={"GOOGLE_APPLICATION_CREDENTIALS\nOPENAI_API_KEY"} />
            <PolicyTextArea label="Allowed domains" value={draft.networkAllowlist} onChange={(value) => updateDraft("networkAllowlist", value)} placeholder={"example.com\napi.example.com"} />
            <PolicyTextArea label="Artifact content types" value={draft.artifactContentTypes} onChange={(value) => updateDraft("artifactContentTypes", value)} placeholder={"text/markdown\napplication/vnd.openxmlformats-officedocument.wordprocessingml.document"} />
            <label className="policy-field">
              <span>Shell timeout</span>
              <Input value={draft.shellTimeout} onChange={(event) => updateDraft("shellTimeout", event.currentTarget.value)} placeholder="90s, 2m" />
            </label>
          </section>
          <section className="policy-section">
            <h3>Sandbox</h3>
            <div className="policy-grid">
              <label className="policy-field">
                <span>Runner</span>
                <Input value={draft.sandboxRunner} onChange={(event) => updateDraft("sandboxRunner", event.currentTarget.value)} placeholder="docker" />
              </label>
              <label className="policy-field">
                <span>Image</span>
                <Input value={draft.sandboxImage} onChange={(event) => updateDraft("sandboxImage", event.currentTarget.value)} placeholder="python:3.12-slim" />
              </label>
              <label className="policy-field">
                <span>Network</span>
                <Input value={draft.sandboxNetwork} onChange={(event) => updateDraft("sandboxNetwork", event.currentTarget.value)} placeholder="none, bridge" />
              </label>
              <label className="policy-field">
                <span>Memory</span>
                <Input value={draft.sandboxMemory} onChange={(event) => updateDraft("sandboxMemory", event.currentTarget.value)} placeholder="512m" />
              </label>
              <label className="policy-field">
                <span>CPUs</span>
                <Input value={draft.sandboxCpus} onChange={(event) => updateDraft("sandboxCpus", event.currentTarget.value)} placeholder="1" />
              </label>
              <label className="policy-field">
                <span>Pids limit</span>
                <Input inputMode="numeric" value={draft.sandboxPidsLimit} onChange={(event) => updateDraft("sandboxPidsLimit", event.currentTarget.value)} placeholder="128" />
              </label>
              <label className="policy-field">
                <span>Tmpfs size</span>
                <Input value={draft.sandboxTmpfsSize} onChange={(event) => updateDraft("sandboxTmpfsSize", event.currentTarget.value)} placeholder="64m" />
              </label>
              <label className="policy-field">
                <span>Max output bytes</span>
                <Input inputMode="numeric" value={draft.sandboxMaxOutputBytes} onChange={(event) => updateDraft("sandboxMaxOutputBytes", event.currentTarget.value)} placeholder="1048576" />
              </label>
            </div>
          </section>
        </div>
        <footer>
          <Button className="skill-action" onClick={onClose}>Cancel</Button>
          <Button className="primary skill-modal-insert" onClick={savePolicy} disabled={saving || loading || !loadedSkill}>
            <ShieldCheck size={16} />
            <span>{saving ? "Saving" : "Save policy"}</span>
          </Button>
        </footer>
      </section>
    </div>
  );
}

export function PolicyTextArea({ label, value, onChange, placeholder }: { label: string; value: string; onChange: (value: string) => void; placeholder?: string }) {
  return (
    <label className="policy-field">
      <span>{label}</span>
      <Textarea value={value} onChange={(event) => onChange(event.currentTarget.value)} placeholder={placeholder} rows={3} />
    </label>
  );
}

export function skillPolicyFromMetadata(metadata?: Record<string, unknown>): Record<string, unknown> {
  if (!metadata) return {};
  for (const key of ["policy", "permissions", "runtime_policy", "runtimePolicy"]) {
    if (isRecord(metadata[key])) return { ...metadata[key] };
  }
  for (const key of ["agentapi", "runtime", "openclaw"]) {
    const nested = metadata[key];
    if (!isRecord(nested)) continue;
    for (const policyKey of ["policy", "permissions", "runtime_policy", "runtimePolicy"]) {
      if (isRecord(nested[policyKey])) return { ...nested[policyKey] };
    }
  }
  return {};
}

export function policyDraftFromConfig(policy: Record<string, unknown>): SkillPolicyDraft {
  const sandbox = isRecord(policy.sandbox) ? policy.sandbox : {};
  return {
    allowedTools: joinPolicyList(policy.allowed_tools ?? policy.allowedTools ?? policy.tools),
    allowedEnv: joinPolicyList(policy.allowed_env ?? policy.allowedEnv ?? policy.env),
    networkAllowlist: joinPolicyList(policy.network_allowlist ?? policy.networkAllowlist ?? policy.allowed_domains ?? policy.allowedDomains ?? policy.domains),
    artifactContentTypes: joinPolicyList(policy.artifact_content_types ?? policy.artifactContentTypes ?? policy.artifact_types ?? policy.artifactTypes ?? policy.output_artifact_types ?? policy.outputArtifactTypes),
    shellTimeout: stringPolicyValue(policy.shell_timeout ?? policy.shellTimeout ?? policy.timeout),
    sandboxRunner: stringPolicyValue(sandbox.runner),
    sandboxImage: stringPolicyValue(sandbox.image),
    sandboxNetwork: stringPolicyValue(sandbox.network),
    sandboxMemory: stringPolicyValue(sandbox.memory),
    sandboxCpus: stringPolicyValue(sandbox.cpus ?? sandbox.cpu),
    sandboxPidsLimit: stringPolicyValue(sandbox.pids_limit ?? sandbox.pidsLimit),
    sandboxTmpfsSize: stringPolicyValue(sandbox.tmpfs_size ?? sandbox.tmpfsSize),
    sandboxMaxOutputBytes: stringPolicyValue(sandbox.max_output_bytes ?? sandbox.maxOutputBytes)
  };
}

export function skillPolicyConfigFromDraft(base: Record<string, unknown>, draft: SkillPolicyDraft): SkillPolicyConfig {
  const next: SkillPolicyConfig = { ...base };
  setPolicyList(next, "allowed_tools", draft.allowedTools);
  setPolicyList(next, "allowed_env", draft.allowedEnv);
  setPolicyList(next, "network_allowlist", draft.networkAllowlist);
  setPolicyList(next, "artifact_content_types", draft.artifactContentTypes);
  setPolicyString(next, "shell_timeout", draft.shellTimeout);
  const sandbox: Record<string, unknown> = isRecord(next.sandbox) ? { ...next.sandbox } : {};
  setPolicyString(sandbox, "runner", draft.sandboxRunner);
  setPolicyString(sandbox, "image", draft.sandboxImage);
  setPolicyString(sandbox, "network", draft.sandboxNetwork);
  setPolicyString(sandbox, "memory", draft.sandboxMemory);
  setPolicyString(sandbox, "cpus", draft.sandboxCpus);
  setPolicyNumber(sandbox, "pids_limit", draft.sandboxPidsLimit);
  setPolicyString(sandbox, "tmpfs_size", draft.sandboxTmpfsSize);
  setPolicyNumber(sandbox, "max_output_bytes", draft.sandboxMaxOutputBytes);
  if (Object.keys(sandbox).length) next.sandbox = sandbox;
  else delete next.sandbox;
  return next;
}

export function setPolicyList(target: Record<string, unknown>, key: string, value: string) {
  const list = splitPolicyList(value);
  if (list.length) target[key] = list;
  else delete target[key];
}

export function setPolicyString(target: Record<string, unknown>, key: string, value: string) {
  const cleaned = value.trim();
  if (cleaned) target[key] = cleaned;
  else delete target[key];
}

export function setPolicyNumber(target: Record<string, unknown>, key: string, value: string) {
  const cleaned = value.trim();
  if (!cleaned) {
    delete target[key];
    return;
  }
  const parsed = Number(cleaned);
  if (Number.isFinite(parsed) && parsed > 0) target[key] = Math.floor(parsed);
}

export function splitPolicyList(value: string): string[] {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const item of value.split(/[\n,]/)) {
    const cleaned = item.trim();
    if (!cleaned || seen.has(cleaned)) continue;
    seen.add(cleaned);
    out.push(cleaned);
  }
  return out;
}

export function joinPolicyList(value: unknown): string {
  if (Array.isArray(value)) return value.map((item) => stringPolicyValue(item)).filter(Boolean).join("\n");
  return stringPolicyValue(value);
}

export function stringPolicyValue(value: unknown): string {
  if (typeof value === "string") return value;
  if (typeof value === "number" || typeof value === "boolean") return String(value);
  return "";
}

export function isRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === "object" && !Array.isArray(value);
}



export function SkillFact({ label, value }: { label: string; value: string }) {
  return (
    <div className="skill-fact">
      <small>{label}</small>
      <strong>{value}</strong>
    </div>
  );
}



export function SkillGlyph({ skill }: { skill: Skill }) {
  if (skill.icon) return <span className="skill-glyph text">{skill.icon}</span>;
  if (skill.produces_artifacts) return <span className="skill-glyph"><Archive size={17} /></span>;
  if (skill.run_as_job) return <span className="skill-glyph"><Clock size={17} /></span>;
  return <span className="skill-glyph"><Sparkles size={17} /></span>;
}



export function errorMessage(error: unknown): string {
  return error instanceof ApiError && error.requestId
    ? `${error.message} (${error.requestId})`
    : error instanceof Error
      ? error.message
      : String(error);
}


export function compareSkills(a: Skill, b: Skill): number {
  if (Boolean(a.featured) !== Boolean(b.featured)) return a.featured ? -1 : 1;
  const orderA = a.sort_order ?? Number.MAX_SAFE_INTEGER;
  const orderB = b.sort_order ?? Number.MAX_SAFE_INTEGER;
  if (orderA !== orderB) return orderA - orderB;
  return (a.display_name || a.name).localeCompare(b.display_name || b.name);
}


export function formatPercent(value: number): string {
  if (!Number.isFinite(value)) return "0%";
  const percent = value > 1 ? value : value * 100;
  return `${percent.toFixed(percent >= 10 ? 0 : 1)}%`;
}


export function formatNumber(value: number): string {
  if (!Number.isFinite(value)) return "0";
  return new Intl.NumberFormat().format(value);
}


export function formatUSD(value: number): string {
  if (!Number.isFinite(value)) return "$0.00";
  return new Intl.NumberFormat(undefined, { style: "currency", currency: "USD", maximumFractionDigits: value < 1 ? 4 : 2 }).format(value);
}

export function formatLatencyMetric(value: number): string {
  if (!Number.isFinite(value) || value <= 0) return "-";
  return `${formatNumber(Math.round(value))} ms`;
}

export function metricNumber(metrics: Record<string, unknown> | undefined, key: string): number {
  const value = metrics?.[key];
  if (typeof value === "number" && Number.isFinite(value)) return value;
  if (typeof value === "string") {
    const parsed = Number(value);
    return Number.isFinite(parsed) ? parsed : 0;
  }
  return 0;
}


export function selectedRunPassRate(run: EvaluationRun | null): number {
  if (!run || !run.total) return 0;
  return run.passed / run.total;
}


export function downloadTextFile(filename: string, content: string, type: string): void {
  const blob = new Blob([content], { type });
  const url = URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = url;
  link.download = filename;
  document.body.appendChild(link);
  link.click();
  link.remove();
  URL.revokeObjectURL(url);
}


export function mergeEvaluationReviews(current: EvaluationReview[], next: EvaluationReview[]): EvaluationReview[] {
  const byID = new Map<string, EvaluationReview>();
  current.forEach((review) => byID.set(review.id, review));
  next.forEach((review) => byID.set(review.id, review));
  return Array.from(byID.values()).sort((a, b) => String(b.updated_at || b.created_at).localeCompare(String(a.updated_at || a.created_at)));
}


export function filterEvaluationResults(results: EvaluationResult[], filter: { status: string; userID: string; sessionID: string; jobID: string; skillName: string; provider: string; model: string; subjectType: string }): EvaluationResult[] {
  return results.filter((result) => {
    if (filter.status !== "all" && result.status !== filter.status) return false;
    if (filter.subjectType !== "all" && result.subject_type !== filter.subjectType) return false;
    if (filter.userID && result.user_id !== filter.userID) return false;
    if (filter.sessionID && result.session_id !== filter.sessionID) return false;
    if (filter.jobID && result.job_id !== filter.jobID) return false;
    if (filter.skillName && result.skill_name !== filter.skillName) return false;
    if (filter.provider && result.provider !== filter.provider) return false;
    if (filter.model && result.model !== filter.model) return false;
    return true;
  });
}


export function auditRecordSummary(record: AuditLogRecord): string {
  const target = record.session_id || record.job_id || record.asset_id || record.request_id || record.id;
  return `${record.user_id || "system"} · ${formatTime(record.created_at)} · ${target}`;
}


export function riskEventSummary(event: RiskEvent): string {
  const actor = event.user_id || event.ip_address || "anonymous";
  return `${actor} · ${formatTime(event.created_at)} · +${event.score_delta}`;
}


export function formatAuditMetadata(record: AuditLogRecord): string {
  const payload = {
    metadata: record.metadata || {},
    user_agent: record.user_agent || "",
    request_id: record.request_id || ""
  };
  return JSON.stringify(payload, null, 2);
}


export function auditRiskForEventName(event: string): string {
  const normalized = event.toLowerCase();
  if (["account_delete", "memory_delete_user", "data_export", "user_ban", "user_disable", "skill_disable", "skill_policy_update", "admin_job_cancel"].includes(normalized)) return "high";
  if (normalized.includes("delete") || normalized.includes("disable") || normalized.includes("ban") || normalized.includes("policy")) return "high";
  if (normalized.includes("cancel") || normalized.includes("publish") || normalized.includes("unpublish") || normalized.includes("update") || normalized.includes("memory_")) return "medium";
  return "low";
}


export function formatShortDate(value?: string): string {
  if (!value) return "Never";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "Unknown";
  return date.toLocaleDateString();
}


export function initials(value: string): string {
  const parts = value.trim().split(/[\s@._-]+/).filter(Boolean);
  const letters = parts.slice(0, 2).map((part) => part[0]?.toUpperCase()).join("");
  return letters || "U";
}


export function fuzzyMatch(query: string, fields: Array<string | number | undefined | null>): boolean {
  const normalizedQuery = normalizeSearch(query);
  if (!normalizedQuery) return true;
  const rawHaystack = fields.filter((field) => field !== undefined && field !== null).join(" ");
  const haystack = normalizeSearch(rawHaystack);
  if (!haystack) return false;
  if (haystack.includes(normalizedQuery)) return true;
  return acronymSearch(rawHaystack).includes(normalizedQuery);
}


export function normalizeSearch(value: string | number | undefined | null): string {
  return String(value || "").toLowerCase().replace(/[\s_\-./]+/g, "");
}


export function acronymSearch(value: string): string {
  return value
    .toLowerCase()
    .split(/[^a-z0-9]+/i)
    .filter(Boolean)
    .map((word) => word[0])
    .join("");
}


export function useFocusTrap<T extends HTMLElement>(active: boolean, onEscape: () => void) {
  const containerRef = useRef<T | null>(null);
  const onEscapeRef = useRef(onEscape);

  useEffect(() => {
    onEscapeRef.current = onEscape;
  }, [onEscape]);

  useEffect(() => {
    if (!active) return;
    const container = containerRef.current;
    if (!container) return;
    const previousFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    const focusFirst = () => {
      const target = focusableElements(container)[0] || container;
      target.focus();
    };
    const frame = window.requestAnimationFrame(focusFirst);
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        event.preventDefault();
        onEscapeRef.current();
        return;
      }
      if (event.key !== "Tab") return;
      const focusable = focusableElements(container);
      if (!focusable.length) {
        event.preventDefault();
        container.focus();
        return;
      }
      const first = focusable[0];
      const last = focusable[focusable.length - 1];
      const current = document.activeElement;
      if (event.shiftKey && current === first) {
        event.preventDefault();
        last.focus();
      } else if (!event.shiftKey && current === last) {
        event.preventDefault();
        first.focus();
      }
    };
    document.addEventListener("keydown", handleKeyDown);
    return () => {
      window.cancelAnimationFrame(frame);
      document.removeEventListener("keydown", handleKeyDown);
      if (previousFocus?.isConnected) previousFocus.focus();
    };
  }, [active]);

  return containerRef;
}

export function focusableElements(container: HTMLElement): HTMLElement[] {
  const selector = [
    "a[href]",
    "button:not([disabled])",
    "input:not([disabled]):not([type='hidden'])",
    "select:not([disabled])",
    "textarea:not([disabled])",
    "[tabindex]:not([tabindex='-1'])"
  ].join(",");
  return Array.from(container.querySelectorAll<HTMLElement>(selector))
    .filter((element) => !element.closest("[aria-hidden='true']") && element.getClientRects().length > 0);
}


export function formatBytes(bytes: number): string {
  if (!bytes) return "0 KB";
  if (bytes < 1024 * 1024) return `${Math.ceil(bytes / 1024)} KB`;
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
}


export function formatTime(value?: string): string {
  if (!value) return "";
  return new Intl.DateTimeFormat(undefined, { month: "short", day: "2-digit", hour: "2-digit", minute: "2-digit" }).format(new Date(value));
}

export type AdminTabOption<T extends string> = {
  id: T;
  label: string;
  description?: string;
  icon?: ReactNode;
  count?: number;
};

export function AdminTabs<T extends string>({
  tabs,
  active,
  onChange,
  label,
  compact = false
}: {
  tabs: Array<AdminTabOption<T>>;
  active: T;
  onChange: (tab: T) => void;
  label: string;
  compact?: boolean;
}) {
  return (
    <Tabs value={active} onValueChange={(value) => onChange(value as T)}>
      <TabsList className={`admin-tabs${compact ? " compact" : ""}`} aria-label={label}>
        {tabs.map((tab) => (
          <TabsTrigger
            key={tab.id}
            value={tab.id}
            className={tab.id === active ? "active" : ""}
          >
            {tab.icon}
            <span>{tab.label}</span>
            {typeof tab.count === "number" && <Badge variant={tab.id === active ? "default" : "secondary"}>{tab.count}</Badge>}
          </TabsTrigger>
        ))}
      </TabsList>
    </Tabs>
  );
}
