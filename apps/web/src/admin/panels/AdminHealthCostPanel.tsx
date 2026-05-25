import { useEffect, useMemo, useState } from "react";
import { Activity, AlertCircle, Archive, Briefcase, Clock, Database, Download, FileText, FileUp, Info, MessageCircle, PlayCircle, Settings, RefreshCw, Search, ShieldCheck, Sparkles, Square, UserX, X } from "lucide-react";
import { ApiClient } from "../../api/client";
import { Button } from "../../components/ui/button";
import { Input } from "../../components/ui/input";
import { Textarea } from "../../components/ui/textarea";
import { AdminSectionNotice } from "../ui";
import {
  AdminMetric,
  AdminTabs,
  StatusBadge,
  SkillFact,
  auditRecordSummary,
  auditRiskForEventName,
  downloadTextFile,
  errorMessage,
  filterEvaluationResults,
  formatAuditMetadata,
  formatBytes,
  formatLatencyMetric,
  formatNumber,
  formatPercent,
  formatShortDate,
  formatTime,
  formatUSD,
  fuzzyMatch,
  initials,
  mergeEvaluationReviews,
  metricNumber,
  llmConfigDraftFromConfig,
  llmConfigFromDraft,
  modelOptionLocation,
  riskEventSummary,
  selectedRunPassRate,
  terminalJobs,
  type AdminTabOption
} from "../shared";
import { sessionTitle } from "../../lib/sessionTitle";
import type { AdminHealthStatus, AdminUser, Asset, AuditLogRecord, AuditLogSummary, EvaluationResult, EvaluationReview, EvaluationRun, EvaluationRunSummary, Job, JobEvent, LLMGovernanceConfig, LLMQuotaAdminSummary, LLMUsageAdminSummary, RiskReviewSummary, RiskSummary, Session } from "../../types";

export function AdminHealthCostPanel({ api, adminToken }: { api: ApiClient; adminToken: string }) {
  const [health, setHealth] = useState<AdminHealthStatus | null>(null);
  const [usage, setUsage] = useState<LLMUsageAdminSummary | null>(null);
  const [quota, setQuota] = useState<LLMQuotaAdminSummary | null>(null);
  const [configDraft, setConfigDraft] = useState<Record<string, string>>({});
  const [userID, setUserID] = useState("");
  const [days, setDays] = useState(1);
  const [refundRequests, setRefundRequests] = useState("");
  const [refundTokens, setRefundTokens] = useState("");
  const [refundCost, setRefundCost] = useState("");
  const [quotaReason, setQuotaReason] = useState("");
  const [loading, setLoading] = useState(false);
  const [quotaBusy, setQuotaBusy] = useState("");
  const [configBusy, setConfigBusy] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [healthTab, setHealthTab] = useState<"runtime" | "live" | "governance" | "usage" | "quota">("runtime");
  const token = adminToken.trim();
  const cleanUserID = userID.trim();
  const readiness = health?.readiness;
  const llm = health?.llm;
  const live = health?.live;
  const healthyBackends = (llm?.backends || []).filter((backend) => backend.healthy).length;
  const healthTabs: Array<AdminTabOption<typeof healthTab>> = [
    { id: "runtime", label: "Runtime", icon: <Activity size={15} />, count: readiness?.checks?.length ?? 0 },
    { id: "live", label: "Live", icon: <MessageCircle size={15} />, count: live?.active_sessions ?? 0 },
    { id: "governance", label: "Governance", icon: <Settings size={15} /> },
    { id: "usage", label: "Usage", icon: <Database size={15} />, count: usage?.requests ?? 0 },
    { id: "quota", label: "Quota", icon: <ShieldCheck size={15} />, count: quota?.recent_adjustments?.length ?? 0 }
  ];

  const loadHealthCost = async () => {
    if (!token) return;
    setLoading(true);
    setError("");
    try {
      const [nextHealth, nextUsage, nextQuota] = await Promise.all([
        api.adminOpsHealth(token),
        api.adminOpsLLMUsage(token, { userId: cleanUserID, days, limit: 200 }),
        cleanUserID ? api.adminOpsQuota(token, cleanUserID, { days: 1, limit: 20 }) : Promise.resolve(null)
      ]);
      setHealth(nextHealth);
      setUsage(nextUsage);
      setQuota(nextQuota);
      setNotice(`Loaded runtime health and ${nextUsage.requests} LLM usage records`);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    if (token) void loadHealthCost();
  }, [token]);

  useEffect(() => {
    if (llm?.config) setConfigDraft(llmConfigDraftFromConfig(llm.config));
  }, [llm?.config]);

  const updateConfigDraft = (key: keyof LLMGovernanceConfig, value: string) => {
    setConfigDraft((current) => ({ ...current, [key]: value }));
  };

  const saveLLMConfig = async () => {
    if (!token) return;
    let patch: LLMGovernanceConfig;
    try {
      patch = llmConfigFromDraft(configDraft);
    } catch (err) {
      setError(errorMessage(err));
      return;
    }
    setConfigBusy(true);
    setError("");
    try {
      const nextConfig = await api.updateAdminOpsLLMConfig(token, patch);
      setHealth((current) => current ? { ...current, llm: { ...current.llm, config: nextConfig } } : current);
      setConfigDraft(llmConfigDraftFromConfig(nextConfig));
      setNotice("LLM governance config updated");
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setConfigBusy(false);
    }
  };

  const resetQuota = async () => {
    if (!token || !cleanUserID) {
      setError("Enter a user ID before resetting quota.");
      return;
    }
    setQuotaBusy("reset");
    setError("");
    try {
      const next = await api.adminOpsQuotaReset(token, cleanUserID, quotaReason.trim());
      setQuota(next);
      setNotice(`Daily quota reset for ${cleanUserID}`);
      await loadHealthCost();
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setQuotaBusy("");
    }
  };

  const refundQuota = async () => {
    if (!token || !cleanUserID) {
      setError("Enter a user ID before applying a refund.");
      return;
    }
    const requestRefund = Math.max(0, Number(refundRequests) || 0);
    const tokenRefund = Math.max(0, Number(refundTokens) || 0);
    const costRefundUSD = Math.max(0, Number(refundCost) || 0);
    if (!requestRefund && !tokenRefund && !costRefundUSD) {
      setError("Enter at least one refund amount.");
      return;
    }
    setQuotaBusy("refund");
    setError("");
    try {
      const next = await api.adminOpsQuotaRefund(token, { userId: cleanUserID, requestRefund, tokenRefund, costRefundUSD, reason: quotaReason.trim() });
      setQuota(next);
      setRefundRequests("");
      setRefundTokens("");
      setRefundCost("");
      setNotice(`Quota refund applied for ${cleanUserID}`);
      await loadHealthCost();
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setQuotaBusy("");
    }
  };

  return (
    <div className="admin-skill-layout">
      <section className="admin-list-panel">
        <div className="admin-list-tools">
          <label className="admin-field">
            <span>User ID filter</span>
            <Input value={userID} onChange={(event) => setUserID(event.currentTarget.value)} placeholder="optional user_id" aria-label="LLM usage user filter" />
          </label>
          <div className="admin-filter-row">
            <select value={String(days)} onChange={(event) => setDays(Number(event.currentTarget.value))} aria-label="Usage time range">
              <option value="1">Last 24h</option>
              <option value="7">Last 7d</option>
              <option value="30">Last 30d</option>
              <option value="90">Last 90d</option>
            </select>
            <Button className="skill-action" onClick={loadHealthCost} disabled={loading || !token}>
              <RefreshCw size={15} />
              <span>{loading ? "Loading" : "Refresh"}</span>
            </Button>
          </div>
        </div>
        <div className="admin-skill-list">
          {(readiness?.checks || []).map((check) => (
            <div key={check.name} className="admin-skill-row static">
              <Activity size={18} />
              <span>
                <strong>{check.name}</strong>
                <small>{check.error || "Ready"}</small>
              </span>
              <StatusBadge value={check.status} />
            </div>
          ))}
          {!readiness && <div className="empty-small">{loading ? "Loading..." : "No health snapshot loaded"}</div>}
        </div>
      </section>
      <section className="admin-detail-panel">
        {(error || notice) && (
          <div className={`admin-inline-banner ${error ? "error" : "ok"}`} role="status">
            {error ? <AlertCircle size={16} /> : <ShieldCheck size={16} />}
            <span>{error || notice}</span>
            <Button className="icon ghost" onClick={() => { setError(""); setNotice(""); }} title="Dismiss" aria-label="Dismiss">
              <X size={14} />
            </Button>
          </div>
        )}
        <div className="admin-skill-head">
          <div>
            <h2>Runtime snapshot</h2>
            <small>{usage?.since ? `Since ${formatTime(usage.since)}` : "No usage window loaded"}</small>
          </div>
          <StatusBadge value={readiness?.status || "unknown"} />
        </div>
        <div className="admin-metrics">
          <AdminMetric label="Requests" value={String(usage?.requests ?? 0)} />
          <AdminMetric label="Tokens" value={formatNumber(usage?.total_tokens ?? 0)} />
          <AdminMetric label="Cost" value={formatUSD(usage?.estimated_cost_usd ?? 0)} />
          <AdminMetric label="Avg latency" value={`${Math.round(usage?.average_latency_ms ?? 0)} ms`} />
        </div>
        <AdminTabs tabs={healthTabs} active={healthTab} onChange={setHealthTab} label="Health and cost sections" compact />
        <div className="admin-detail-grid">
          {healthTab === "runtime" && <section className="admin-card wide">
            <div className="admin-card-head">
              <h3>LLM backends</h3>
              <small>{healthyBackends}/{llm?.backends?.length || 0} healthy</small>
            </div>
            <div className="admin-table">
              {(llm?.backends || []).map((backend) => (
                <div key={`${backend.name}-${backend.model}`} className="admin-table-row">
                  <StatusBadge value={backend.healthy ? "healthy" : "unhealthy"} />
                  <span>{backend.provider} / {backend.model}</span>
                  <small>{backend.consecutive_failures} failures</small>
                  {backend.last_error && <em>{backend.last_error}</em>}
                </div>
              ))}
              {!llm?.backends?.length && <p className="muted-text">No LLM backend status loaded.</p>}
            </div>
          </section>}
          {healthTab === "live" && <section className="admin-card wide">
            <div className="admin-card-head">
              <h3>Live voice health</h3>
              <StatusBadge value={(live?.active_sessions || 0) > 0 ? "active" : "idle"} />
            </div>
            <div className="admin-metrics compact">
              <AdminMetric label="Sessions" value={String(live?.sessions ?? 0)} />
              <AdminMetric label="Active" value={String(live?.active_sessions ?? 0)} />
              <AdminMetric label="Error rate" value={formatPercent(live?.error_rate ?? 0)} />
              <AdminMetric label="Transcription" value={formatPercent(live?.transcription_success_rate ?? 0)} />
              <AdminMetric label="First transcript" value={`${Math.round(live?.average_first_transcript_ms ?? 0)} ms`} />
              <AdminMetric label="First voice" value={`${Math.round(live?.average_first_audio_ms ?? 0)} ms`} />
            </div>
            <div className="admin-table">
              <div className="admin-table-row">
                <StatusBadge value="audio" />
                <span>Audio sent</span>
                <small>{formatNumber(live?.audio_chunks ?? 0)} chunks</small>
                <em>{formatBytes(live?.audio_bytes ?? 0)}</em>
              </div>
              <div className="admin-table-row">
                <StatusBadge value="disconnects" />
                <span>Disconnects</span>
                <small>{formatNumber(live?.disconnected ?? 0)}</small>
                <em>{formatNumber(live?.failed ?? 0)} failed sessions</em>
              </div>
              {(live?.errors_by_code || []).map((item) => (
                <div key={item.key} className="admin-table-row">
                  <StatusBadge value="error" />
                  <span>{item.key}</span>
                  <small>{formatNumber(item.count)}</small>
                </div>
              ))}
              {!live?.sessions && <p className="muted-text">No Live sessions have been observed by this API instance yet.</p>}
            </div>
          </section>}
          {healthTab === "governance" && <section className="admin-card wide">
            <div className="admin-card-head">
              <h3>Governance config</h3>
              <Button className="skill-action" onClick={saveLLMConfig} disabled={configBusy || !token}>
                <Settings size={15} />
                <span>{configBusy ? "Saving" : "Save"}</span>
              </Button>
            </div>
            <div className="admin-config-grid">
              <label className="admin-field">
                <span>Model</span>
                <select value={configDraft.model || ""} onChange={(event) => updateConfigDraft("model", event.currentTarget.value)}>
                  {(llm?.config?.allowed_models || []).map((option) => (
                    <option key={option.id} value={option.id}>{option.label}</option>
                  ))}
                  {!llm?.config?.allowed_models?.length && <option value={configDraft.model || ""}>{configDraft.model || "No model loaded"}</option>}
                </select>
              </label>
              <label className="admin-field">
                <span>Vertex location</span>
                <Input value={modelOptionLocation(llm?.config, configDraft.model) || configDraft.vertex_location || ""} readOnly aria-label="Selected model Vertex location" />
              </label>
              <label className="admin-field">
                <span>Daily token quota</span>
                <Input inputMode="numeric" value={configDraft.daily_token_quota || ""} onChange={(event) => updateConfigDraft("daily_token_quota", event.currentTarget.value)} placeholder="0 disables" />
              </label>
              <label className="admin-field">
                <span>Daily request quota</span>
                <Input inputMode="numeric" value={configDraft.daily_request_quota || ""} onChange={(event) => updateConfigDraft("daily_request_quota", event.currentTarget.value)} placeholder="0 disables" />
              </label>
              <label className="admin-field">
                <span>Daily cost quota USD</span>
                <Input inputMode="decimal" value={configDraft.daily_cost_quota_usd || ""} onChange={(event) => updateConfigDraft("daily_cost_quota_usd", event.currentTarget.value)} placeholder="0 disables" />
              </label>
              <label className="admin-field">
                <span>Max attempts</span>
                <Input inputMode="numeric" value={configDraft.max_attempts || ""} onChange={(event) => updateConfigDraft("max_attempts", event.currentTarget.value)} placeholder="1" />
              </label>
              <label className="admin-field">
                <span>Chat timeout ms</span>
                <Input inputMode="numeric" value={configDraft.chat_timeout_ms || ""} onChange={(event) => updateConfigDraft("chat_timeout_ms", event.currentTarget.value)} placeholder="60000" />
              </label>
              <label className="admin-field">
                <span>Skill timeout ms</span>
                <Input inputMode="numeric" value={configDraft.skill_timeout_ms || ""} onChange={(event) => updateConfigDraft("skill_timeout_ms", event.currentTarget.value)} placeholder="90000" />
              </label>
              <label className="admin-field">
                <span>Input cost / 1M</span>
                <Input inputMode="decimal" value={configDraft.input_cost_per_million || ""} onChange={(event) => updateConfigDraft("input_cost_per_million", event.currentTarget.value)} placeholder="0.30" />
              </label>
              <label className="admin-field">
                <span>Output cost / 1M</span>
                <Input inputMode="decimal" value={configDraft.output_cost_per_million || ""} onChange={(event) => updateConfigDraft("output_cost_per_million", event.currentTarget.value)} placeholder="2.50" />
              </label>
              <label className="admin-field">
                <span>Retry backoff ms</span>
                <Input inputMode="numeric" value={configDraft.retry_backoff_ms || ""} onChange={(event) => updateConfigDraft("retry_backoff_ms", event.currentTarget.value)} placeholder="300" />
              </label>
              <label className="admin-field">
                <span>Failure threshold</span>
                <Input inputMode="numeric" value={configDraft.failure_threshold || ""} onChange={(event) => updateConfigDraft("failure_threshold", event.currentTarget.value)} placeholder="3" />
              </label>
              <label className="admin-field">
                <span>Circuit cooldown sec</span>
                <Input inputMode="numeric" value={configDraft.circuit_cooldown_seconds || ""} onChange={(event) => updateConfigDraft("circuit_cooldown_seconds", event.currentTarget.value)} placeholder="60" />
              </label>
            </div>
          </section>}
          {healthTab === "usage" && <section className="admin-card">
            <div className="admin-card-head">
              <h3>Cost by provider</h3>
            </div>
            <div className="admin-table">
              {(usage?.by_provider || []).map((group) => (
                <div key={`${group.provider}-${group.model}-${group.status}`} className="admin-table-row">
                  <StatusBadge value={group.status} />
                  <span>{group.provider} / {group.model}</span>
                  <small>{formatNumber(group.total_tokens)} tokens</small>
                  <em>{formatUSD(group.estimated_cost_usd)} · {group.requests} req</em>
                </div>
              ))}
              {!usage?.by_provider?.length && <p className="muted-text">No usage records in this window.</p>}
            </div>
          </section>}
          {healthTab === "usage" && <section className="admin-card">
            <div className="admin-card-head">
              <h3>Recent usage</h3>
            </div>
            <div className="admin-table">
              {(usage?.recent || []).slice(0, 12).map((record) => (
                <div key={record.id} className="admin-table-row">
                  <StatusBadge value={record.status} />
                  <span>{record.provider} / {record.model}</span>
                  <small>{formatNumber(record.total_tokens)} tokens · {record.latency_ms} ms{record.ttft_ms ? ` · TTFT ${record.ttft_ms} ms` : ""}</small>
                  {record.error && <em>{record.error}</em>}
                </div>
              ))}
              {!usage?.recent?.length && <p className="muted-text">No recent usage records.</p>}
            </div>
          </section>}
          {healthTab === "quota" && <section className="admin-card wide">
            <div className="admin-card-head">
              <h3>Quota reset & refund</h3>
              {cleanUserID ? <small>{cleanUserID}</small> : <small>User ID required</small>}
            </div>
            <div className="admin-metrics compact">
              <AdminMetric label="Effective requests" value={String(quota?.effective_usage?.requests ?? 0)} />
              <AdminMetric label="Effective tokens" value={formatNumber(quota?.effective_usage?.total_tokens ?? 0)} />
              <AdminMetric label="Effective cost" value={formatUSD(quota?.effective_usage?.estimated_cost_usd ?? 0)} />
              <AdminMetric label="Adjustments" value={String(quota?.recent_adjustments?.length ?? 0)} />
            </div>
            <div className="admin-quota-tools">
              <label className="admin-field">
                <span>Refund requests</span>
                <Input inputMode="numeric" value={refundRequests} onChange={(event) => setRefundRequests(event.currentTarget.value)} placeholder="0" />
              </label>
              <label className="admin-field">
                <span>Refund tokens</span>
                <Input inputMode="numeric" value={refundTokens} onChange={(event) => setRefundTokens(event.currentTarget.value)} placeholder="0" />
              </label>
              <label className="admin-field">
                <span>Refund cost USD</span>
                <Input inputMode="decimal" value={refundCost} onChange={(event) => setRefundCost(event.currentTarget.value)} placeholder="0.00" />
              </label>
              <label className="admin-field">
                <span>Reason</span>
                <Input value={quotaReason} onChange={(event) => setQuotaReason(event.currentTarget.value)} placeholder="support note" />
              </label>
            </div>
            <div className="admin-action-row">
              <Button className="skill-action" onClick={refundQuota} disabled={!cleanUserID || Boolean(quotaBusy)}>
                <Download size={15} />
                <span>{quotaBusy === "refund" ? "Applying" : "Apply refund"}</span>
              </Button>
              <Button className="skill-action danger-outline" onClick={resetQuota} disabled={!cleanUserID || Boolean(quotaBusy)}>
                <RefreshCw size={15} />
                <span>{quotaBusy === "reset" ? "Resetting" : "Reset daily quota"}</span>
              </Button>
            </div>
            <div className="admin-table">
              {(quota?.recent_adjustments || []).slice(0, 8).map((adjustment) => (
                <div key={adjustment.id} className="admin-table-row">
                  <StatusBadge value={adjustment.total_token_delta < 0 || adjustment.request_delta < 0 || adjustment.estimated_cost_delta_usd < 0 ? "refund" : "adjust"} />
                  <span>{adjustment.reason || "manual adjustment"}</span>
                  <small>{formatNumber(adjustment.total_token_delta)} tokens · {formatUSD(adjustment.estimated_cost_delta_usd)}</small>
                  <em>{formatTime(adjustment.created_at)}</em>
                </div>
              ))}
              {!quota?.recent_adjustments?.length && <p className="muted-text">{cleanUserID ? "No quota adjustments for this user today." : "Enter a user ID to load quota tools."}</p>}
            </div>
          </section>}
        </div>
      </section>
    </div>
  );
}
