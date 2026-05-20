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
  riskEventSummary,
  selectedRunPassRate,
  terminalJobs,
  type AdminTabOption
} from "../shared";
import { sessionTitle } from "../../lib/sessionTitle";
import type { AdminHealthStatus, AdminUser, Asset, AuditLogRecord, AuditLogSummary, EvaluationResult, EvaluationReview, EvaluationRun, EvaluationRunSummary, Job, JobEvent, LLMGovernanceConfig, LLMQuotaAdminSummary, LLMUsageAdminSummary, RiskReviewSummary, RiskSummary, Session } from "../../types";

export function AdminAuditPanel({ api, adminToken }: { api: ApiClient; adminToken: string }) {
  const [audit, setAudit] = useState<AuditLogSummary | null>(null);
  const [risk, setRisk] = useState<RiskSummary | null>(null);
  const [reviews, setReviews] = useState<RiskReviewSummary | null>(null);
  const [selectedID, setSelectedID] = useState("");
  const [selectedRiskID, setSelectedRiskID] = useState("");
  const [userID, setUserID] = useState("");
  const [query, setQuery] = useState("");
  const [eventFilter, setEventFilter] = useState("all");
  const [operationFilter, setOperationFilter] = useState("all");
  const [riskFilter, setRiskFilter] = useState("all");
  const [reviewStatusFilter, setReviewStatusFilter] = useState("pending");
  const [days, setDays] = useState(7);
  const [loading, setLoading] = useState(false);
  const [reviewBusy, setReviewBusy] = useState("");
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [auditTab, setAuditTab] = useState<"overview" | "reviews" | "audit-event" | "risk-event">("overview");
  const token = adminToken.trim();
  const records = audit?.records || [];
  const riskEvents = risk?.events || [];
  const reviewItems = reviews?.items || [];
  const selected = records.find((record) => record.id === selectedID) || records[0] || null;
  const selectedRisk = riskEvents.find((event) => event.id === selectedRiskID) || riskEvents[0] || null;
  const auditTabs: Array<AdminTabOption<typeof auditTab>> = [
    { id: "overview", label: "Overview", icon: <Activity size={15} />, count: audit?.total ?? 0 },
    { id: "reviews", label: "Reviews", icon: <ShieldCheck size={15} />, count: reviews?.pending ?? 0 },
    { id: "audit-event", label: "Audit event", icon: <FileText size={15} />, count: records.length },
    { id: "risk-event", label: "Risk event", icon: <AlertCircle size={15} />, count: riskEvents.length }
  ];

  const loadAudit = async () => {
    if (!token) return;
    setLoading(true);
    setError("");
    try {
      const [next, nextRisk, nextReviews] = await Promise.all([
        api.adminOpsAudit(token, {
          userId: userID.trim(),
          event: eventFilter,
          risk: riskFilter,
          q: query.trim(),
          days,
          limit: 300
        }),
        api.adminOpsRisk(token, {
          userId: userID.trim(),
          operation: operationFilter,
          risk: riskFilter,
          q: query.trim(),
          days,
          limit: 300
        }),
        api.adminOpsRiskReviews(token, {
          userId: userID.trim(),
          status: reviewStatusFilter,
          operation: operationFilter,
          risk: riskFilter,
          q: query.trim(),
          days,
          limit: 100
        })
      ]);
      setAudit(next);
      setRisk(nextRisk);
      setReviews(nextReviews);
      setSelectedID((current) => {
        if (current && next.records.some((record) => record.id === current)) return current;
        return next.records[0]?.id || "";
      });
      setSelectedRiskID((current) => {
        if (current && nextRisk.events.some((event) => event.id === current)) return current;
        return nextRisk.events[0]?.id || "";
      });
      setNotice(`Loaded ${next.total} audit events, ${nextRisk.total} risk events, and ${nextReviews.total} reviews`);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    if (token) void loadAudit();
  }, [token]);

  const eventOptions = useMemo(() => {
    const values = new Set<string>();
    audit?.by_event?.forEach((group) => values.add(group.key));
    records.forEach((record) => values.add(record.event));
    return Array.from(values).sort();
  }, [audit, records]);

  const operationOptions = useMemo(() => {
    const values = new Set<string>();
    risk?.by_operation?.forEach((group) => values.add(group.key));
    riskEvents.forEach((event) => values.add(event.operation));
    return Array.from(values).sort();
  }, [risk, riskEvents]);

  const updateReview = async (id: string, status: string, resolution = "") => {
    if (!token) return;
    setReviewBusy(id);
    setError("");
    try {
      await api.updateRiskReview(token, id, {
        status,
        assignedTo: status === "in_review" ? "admin" : "",
        resolution,
        note: resolution || status
      });
      await loadAudit();
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setReviewBusy("");
    }
  };

  return (
    <div className="admin-skill-layout">
      <section className="admin-list-panel">
        <div className="admin-list-tools">
          <label className="admin-field">
            <span>User ID filter</span>
            <Input value={userID} onChange={(event) => setUserID(event.currentTarget.value)} placeholder="optional user_id" aria-label="Audit user ID filter" />
          </label>
          <div className="admin-search">
            <Search size={16} />
            <Input
              value={query}
              onChange={(event) => setQuery(event.currentTarget.value)}
              onKeyDown={(event) => {
                if (event.key === "Enter") void loadAudit();
              }}
              placeholder="Search request, event, metadata"
              aria-label="Search audit logs"
            />
          </div>
          <div className="admin-filter-row">
            <select value={riskFilter} onChange={(event) => setRiskFilter(event.currentTarget.value)} aria-label="Filter audit risk">
              <option value="all">All risks</option>
              <option value="high">High risk</option>
              <option value="medium">Medium risk</option>
              <option value="low">Low risk</option>
            </select>
            <select value={String(days)} onChange={(event) => setDays(Number(event.currentTarget.value))} aria-label="Audit time range">
              <option value="1">Last 24h</option>
              <option value="7">Last 7d</option>
              <option value="30">Last 30d</option>
              <option value="90">Last 90d</option>
            </select>
          </div>
          <select value={eventFilter} onChange={(event) => setEventFilter(event.currentTarget.value)} aria-label="Filter audit event">
            <option value="all">All events</option>
            {eventOptions.map((event) => <option key={event} value={event}>{event}</option>)}
          </select>
          <select value={operationFilter} onChange={(event) => setOperationFilter(event.currentTarget.value)} aria-label="Filter risk operation">
            <option value="all">All operations</option>
            {operationOptions.map((operation) => <option key={operation} value={operation}>{operation}</option>)}
          </select>
          <select value={reviewStatusFilter} onChange={(event) => setReviewStatusFilter(event.currentTarget.value)} aria-label="Filter risk reviews">
            <option value="pending">Pending reviews</option>
            <option value="in_review">In review</option>
            <option value="resolved">Resolved</option>
            <option value="dismissed">Dismissed</option>
            <option value="all">All reviews</option>
          </select>
          <Button className="primary wide" onClick={loadAudit} disabled={loading || !token}>
            {loading ? "Loading" : "Load audit logs"}
          </Button>
        </div>
        <div className="admin-skill-list">
          {records.map((record) => (
            <Button key={record.id} className={`admin-skill-row ${record.id === selected?.id ? "active" : ""}`} onClick={() => setSelectedID(record.id)}>
              <FileText size={18} />
              <span>
                <strong>{record.event}</strong>
                <small>{auditRecordSummary(record)}</small>
              </span>
              <StatusBadge value={record.risk_level || "low"} />
            </Button>
          ))}
          {!records.length && <div className="empty-small">{loading ? "Loading..." : "No audit events"}</div>}
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
            <h2>Risk overview</h2>
            <small>{audit?.since ? `Since ${formatTime(audit.since)}` : "No audit window loaded"}</small>
          </div>
          <Button className="skill-action" onClick={loadAudit} disabled={loading || !token}>
            <RefreshCw size={16} />
            <span>{loading ? "Loading" : "Refresh"}</span>
          </Button>
        </div>
        <div className="admin-metrics">
          <AdminMetric label="Events" value={String(audit?.total ?? 0)} />
          <AdminMetric label="Risk events" value={String(risk?.total ?? 0)} />
          <AdminMetric label="High risk" value={String((audit?.high_risk ?? 0) + (risk?.high_risk ?? 0))} />
          <AdminMetric label="Pending reviews" value={String(reviews?.pending ?? 0)} />
          <AdminMetric label="Risk scores" value={String(risk?.scores?.length ?? 0)} />
        </div>
        <AdminTabs tabs={auditTabs} active={auditTab} onChange={setAuditTab} label="Audit detail sections" compact />
        <div className="admin-detail-grid">
          {auditTab === "reviews" && <section className="admin-card wide">
            <div className="admin-card-head">
              <h3>Manual review queue</h3>
              <StatusBadge value={reviewStatusFilter} />
            </div>
            <div className="admin-table">
              {reviewItems.slice(0, 12).map((item) => (
                <div key={item.id} className="admin-table-row">
                  <StatusBadge value={item.priority || item.risk_level || "low"} />
                  <span>
                    <strong>{item.operation}</strong>
                    <small>{item.reason} · {item.user_id || item.ip_address || "anonymous"}</small>
                  </span>
                  <small>{formatTime(item.updated_at)}</small>
                  <Button className="small ghost" disabled={reviewBusy === item.id} onClick={() => updateReview(item.id, "in_review")}>Review</Button>
                  <Button className="small ghost" disabled={reviewBusy === item.id} onClick={() => updateReview(item.id, "resolved", "resolved by admin")}>Resolve</Button>
                  <Button className="small danger" disabled={reviewBusy === item.id} onClick={() => updateReview(item.id, "dismissed", "dismissed by admin")}>Dismiss</Button>
                </div>
              ))}
              {!reviewItems.length && <p className="muted-text">No manual review items in this filter.</p>}
            </div>
          </section>}
          {auditTab === "overview" && <section className="admin-card">
            <div className="admin-card-head">
              <h3>Event mix</h3>
            </div>
            <div className="admin-table">
              {(audit?.by_event || []).slice(0, 12).map((group) => (
                <Button key={group.key} className="admin-table-row button-row" onClick={() => setEventFilter(group.key)}>
                  <StatusBadge value={auditRiskForEventName(group.key)} />
                  <span>{group.key}</span>
                  <small>{group.count} events</small>
                </Button>
              ))}
              {!audit?.by_event?.length && <p className="muted-text">No events in this window.</p>}
            </div>
          </section>}
          {auditTab === "overview" && <section className="admin-card">
            <div className="admin-card-head">
              <h3>Risk scores</h3>
            </div>
            <div className="admin-table">
              {(risk?.scores || []).slice(0, 10).map((score) => (
                <Button key={`${score.subject_type}:${score.subject_id}`} className="admin-table-row button-row" onClick={() => score.subject_type === "user" ? setUserID(score.subject_id) : undefined}>
                  <StatusBadge value={score.risk_level || "low"} />
                  <span>{score.subject_type}:{score.subject_id}</span>
                  <small>{score.score} score · {score.event_count} events</small>
                </Button>
              ))}
              {!risk?.scores?.length && <p className="muted-text">No accumulated risk scores.</p>}
            </div>
          </section>}
          {auditTab === "audit-event" && <section className="admin-card">
            <div className="admin-card-head">
              <h3>Selected audit event</h3>
              {selected && <StatusBadge value={selected.risk_level || "low"} />}
            </div>
            {selected ? (
              <div className="admin-facts">
                <SkillFact label="Event" value={selected.event} />
                <SkillFact label="User ID" value={selected.user_id || "system"} />
                <SkillFact label="Request ID" value={selected.request_id || "none"} />
                <SkillFact label="Created" value={formatTime(selected.created_at)} />
                <SkillFact label="IP" value={selected.ip_address || "unknown"} />
                <SkillFact label="Session" value={selected.session_id || "none"} />
                <SkillFact label="Job" value={selected.job_id || "none"} />
                <SkillFact label="Asset" value={selected.asset_id || "none"} />
              </div>
            ) : (
              <p className="muted-text">Select an audit event to inspect details.</p>
            )}
          </section>}
          {auditTab === "audit-event" && <section className="admin-card wide">
            <div className="admin-card-head">
              <h3>Metadata</h3>
            </div>
            <pre className="admin-code-block">{selected ? formatAuditMetadata(selected) : "{}"}</pre>
          </section>}
          {auditTab === "risk-event" && <section className="admin-card wide">
            <div className="admin-card-head">
              <h3>Risk event queue</h3>
            </div>
            <div className="admin-table">
              {riskEvents.slice(0, 12).map((event) => (
                <Button key={event.id} className={`admin-table-row button-row ${event.id === selectedRisk?.id ? "active" : ""}`} onClick={() => setSelectedRiskID(event.id)}>
                  <StatusBadge value={event.risk_level || "low"} />
                  <span>{event.operation}</span>
                  <small>{riskEventSummary(event)}</small>
                  {event.reason && <em>{event.reason}</em>}
                </Button>
              ))}
              {!riskEvents.length && <p className="muted-text">No risk events in the current filter.</p>}
            </div>
          </section>}
          {auditTab === "risk-event" && <section className="admin-card wide">
            <div className="admin-card-head">
              <h3>Selected risk event</h3>
              {selectedRisk && <StatusBadge value={selectedRisk.risk_level || "low"} />}
            </div>
            {selectedRisk ? (
              <div className="admin-facts">
                <SkillFact label="Operation" value={selectedRisk.operation} />
                <SkillFact label="Reason" value={selectedRisk.reason} />
                <SkillFact label="Score delta" value={String(selectedRisk.score_delta)} />
                <SkillFact label="User ID" value={selectedRisk.user_id || "anonymous"} />
                <SkillFact label="IP" value={selectedRisk.ip_address || "unknown"} />
                <SkillFact label="Created" value={formatTime(selectedRisk.created_at)} />
              </div>
            ) : (
              <p className="muted-text">Select a risk event to inspect details.</p>
            )}
            <pre className="admin-code-block">{selectedRisk ? JSON.stringify({ metadata: selectedRisk.metadata || {}, request_id: selectedRisk.request_id || "" }, null, 2) : "{}"}</pre>
          </section>}
        </div>
      </section>
    </div>
  );
}
