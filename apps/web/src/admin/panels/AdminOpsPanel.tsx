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

export function AdminOpsPanel({ api, adminToken }: { api: ApiClient; adminToken: string }) {
  const [userID, setUserID] = useState("");
  const [query, setQuery] = useState("");
  const [statusFilter, setStatusFilter] = useState("all");
  const [assetKind, setAssetKind] = useState("all");
  const [sessions, setSessions] = useState<Session[]>([]);
  const [jobs, setJobs] = useState<Job[]>([]);
  const [assets, setAssets] = useState<Asset[]>([]);
  const [events, setEvents] = useState<JobEvent[]>([]);
  const [selectedSessionID, setSelectedSessionID] = useState("");
  const [selectedJobID, setSelectedJobID] = useState("");
  const [loading, setLoading] = useState(false);
  const [actionBusy, setActionBusy] = useState("");
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [opsTab, setOpsTab] = useState<"session" | "jobs" | "events" | "assets">("jobs");
  const token = adminToken.trim();
  const cleanUserID = userID.trim();
  const selectedSession = sessions.find((session) => session.id === selectedSessionID) || null;
  const selectedJob = jobs.find((job) => job.id === selectedJobID) || null;
  const opsTabs: Array<AdminTabOption<typeof opsTab>> = [
    { id: "session", label: "Session", icon: <MessageCircle size={15} />, count: sessions.length },
    { id: "jobs", label: "Jobs", icon: <Briefcase size={15} />, count: jobs.length },
    { id: "events", label: "Events", icon: <Activity size={15} />, count: events.length },
    { id: "assets", label: "Assets", icon: <FileUp size={15} />, count: assets.length }
  ];

  const loadOps = async (sessionID = selectedSessionID, jobID = selectedJobID) => {
    if (!token || !cleanUserID) {
      setError("Enter a user ID to inspect sessions, jobs, and assets.");
      return;
    }
    setLoading(true);
    setError("");
    try {
      const [nextSessions, nextJobs, nextAssets] = await Promise.all([
        api.adminOpsSessions(token, cleanUserID, { q: query, limit: 100 }),
        api.adminOpsJobs(token, cleanUserID, { sessionId: sessionID, q: query, status: statusFilter, limit: 100 }),
        api.adminOpsAssets(token, cleanUserID, { sessionId: sessionID, jobId: jobID, q: query, kind: assetKind, limit: 100 })
      ]);
      setSessions(nextSessions);
      setJobs(nextJobs);
      setAssets(nextAssets);
      const nextSessionID = sessionID && nextSessions.some((session) => session.id === sessionID) ? sessionID : "";
      const nextJobID = jobID && nextJobs.some((job) => job.id === jobID) ? jobID : nextJobs[0]?.id || "";
      setSelectedSessionID(nextSessionID);
      setSelectedJobID(nextJobID);
      if (nextJobID) {
        const nextEvents = await api.adminOpsJobEvents(token, cleanUserID, nextJobID, 500);
        setEvents(nextEvents);
      } else {
        setEvents([]);
      }
      setNotice(`Loaded ${nextSessions.length} sessions, ${nextJobs.length} jobs, ${nextAssets.length} assets`);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setLoading(false);
    }
  };

  const openSession = async (sessionID: string) => {
    setSelectedSessionID(sessionID);
    setSelectedJobID("");
    setEvents([]);
    await loadOps(sessionID, "");
  };

  const openJob = async (jobID: string) => {
    setSelectedJobID(jobID);
    if (!token || !cleanUserID) return;
    setError("");
    try {
      setEvents(await api.adminOpsJobEvents(token, cleanUserID, jobID, 500));
    } catch (err) {
      setError(errorMessage(err));
    }
  };

  const cancelJob = async () => {
    if (!selectedJob || !token || !cleanUserID) return;
    setActionBusy("cancel");
    setError("");
    try {
      await api.adminOpsCancelJob(token, cleanUserID, selectedJob.id);
      setNotice(`Cancelled ${selectedJob.id}`);
      await loadOps(selectedSessionID, selectedJob.id);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setActionBusy("");
    }
  };

  return (
    <div className="admin-skill-layout">
      <section className="admin-list-panel">
        <div className="admin-list-tools">
          <label className="admin-field">
            <span>User ID</span>
            <Input value={userID} onChange={(event) => setUserID(event.currentTarget.value)} placeholder="user_id" aria-label="Troubleshooting user ID" />
          </label>
          <div className="admin-search">
            <Search size={16} />
            <Input
              value={query}
              onChange={(event) => setQuery(event.currentTarget.value)}
              onKeyDown={(event) => {
                if (event.key === "Enter") void loadOps();
              }}
              placeholder="Search IDs, content, filenames"
              aria-label="Search troubleshooting data"
            />
          </div>
          <div className="admin-filter-row">
            <select value={statusFilter} onChange={(event) => setStatusFilter(event.currentTarget.value)} aria-label="Filter job status">
              <option value="all">All jobs</option>
              <option value="queued">Queued</option>
              <option value="running">Running</option>
              <option value="succeeded">Succeeded</option>
              <option value="failed">Failed</option>
              <option value="cancelled">Cancelled</option>
            </select>
            <select value={assetKind} onChange={(event) => setAssetKind(event.currentTarget.value)} aria-label="Filter asset kind">
              <option value="all">All assets</option>
              <option value="attachment">Attachments</option>
              <option value="artifact">Artifacts</option>
            </select>
          </div>
          <Button className="primary wide" onClick={() => loadOps()} disabled={loading || !token || !cleanUserID}>
            {loading ? "Loading" : "Load troubleshooting data"}
          </Button>
        </div>
        <div className="admin-skill-list">
          {sessions.map((session) => (
            <Button key={session.id} className={`admin-skill-row ${session.id === selectedSessionID ? "active" : ""}`} onClick={() => openSession(session.id)}>
              <MessageCircle size={18} />
              <span>
                <strong>{sessionTitle(session)}</strong>
                <small>{session.id}</small>
              </span>
              <small>{(session.messages || []).filter((message) => !message.hidden).length}</small>
            </Button>
          ))}
          {!sessions.length && <div className="empty-small">{loading ? "Loading..." : "No sessions"}</div>}
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
        {!cleanUserID ? (
          <div className="admin-empty">
            <Briefcase size={24} />
            <strong>Enter a user ID</strong>
            <p>Use User Management to copy a user ID, then inspect their sessions, jobs, replay events, and assets here.</p>
          </div>
        ) : (
          <>
            <div className="admin-skill-head">
              <div>
                <h2>{selectedSession ? sessionTitle(selectedSession) : "User scope"}</h2>
                <small>{selectedSessionID || cleanUserID}</small>
              </div>
              <Button className="skill-action" onClick={() => loadOps()} disabled={loading}>
                <RefreshCw size={16} />
                <span>{loading ? "Loading" : "Refresh"}</span>
              </Button>
            </div>
            <div className="admin-metrics">
              <AdminMetric label="Sessions" value={String(sessions.length)} />
              <AdminMetric label="Jobs" value={String(jobs.length)} />
              <AdminMetric label="Assets" value={String(assets.length)} />
              <AdminMetric label="Events" value={String(events.length)} />
            </div>
            <AdminTabs tabs={opsTabs} active={opsTab} onChange={setOpsTab} label="Troubleshooting sections" compact />
            <div className="admin-detail-grid">
              {opsTab === "session" && <section className="admin-card wide">
                <div className="admin-card-head">
                  <h3>Selected session</h3>
                </div>
                <div className="admin-facts">
                  <SkillFact label="Session ID" value={selectedSessionID || "All sessions"} />
                  <SkillFact label="Messages" value={String((selectedSession?.messages || []).filter((message) => !message.hidden).length)} />
                  <SkillFact label="Working dir" value={selectedSession?.working_dir || "Not selected"} />
                  <SkillFact label="Updated" value={formatTime(selectedSession?.updated_at || "")} />
                </div>
              </section>}
              {opsTab === "jobs" && <section className="admin-card wide">
                <div className="admin-card-head">
                  <h3>Jobs</h3>
                  {selectedJob && <StatusBadge value={selectedJob.status} />}
                </div>
                <div className="admin-table">
                  {jobs.slice(0, 12).map((job) => (
                    <Button key={job.id} className={`admin-table-row button-row ${job.id === selectedJobID ? "active" : ""}`} onClick={() => openJob(job.id)}>
                      <StatusBadge value={job.status} />
                      <span>{job.type || "chat"}</span>
                      <small>{job.id}</small>
                      {job.error && <em>{job.error}</em>}
                    </Button>
                  ))}
                  {!jobs.length && <p className="muted-text">No jobs found.</p>}
                </div>
                {selectedJob && (
                  <div className="admin-action-row">
                    <Button className="skill-action danger-outline" onClick={cancelJob} disabled={Boolean(actionBusy) || terminalJobs.has(selectedJob.status)}>
                      <Square size={15} />
                      <span>{actionBusy === "cancel" ? "Cancelling" : "Cancel job"}</span>
                    </Button>
                  </div>
                )}
              </section>}
              {opsTab === "events" && <section className="admin-card wide">
                <div className="admin-card-head">
                  <h3>Job events</h3>
                </div>
                <div className="admin-table">
                  {events.slice(0, 12).map((event) => (
                    <div key={event.id} className="admin-table-row">
                      <StatusBadge value={event.type} />
                      <span>{event.event?.content || event.event?.error || event.event?.type || "event"}</span>
                      <small>{formatTime(event.created_at)}</small>
                    </div>
                  ))}
                  {!events.length && <p className="muted-text">Select a job to inspect replay events.</p>}
                </div>
              </section>}
              {opsTab === "assets" && <section className="admin-card wide">
                <div className="admin-card-head">
                  <h3>Assets</h3>
                </div>
                <div className="admin-table">
                  {assets.slice(0, 12).map((asset) => (
                    <div key={asset.id} className="admin-table-row">
                      <StatusBadge value={asset.kind} />
                      <span>{asset.filename}</span>
                      <small>{formatBytes(asset.size_bytes)} · {asset.id}</small>
                      {asset.job_id && <em>{asset.job_id}</em>}
                    </div>
                  ))}
                  {!assets.length && <p className="muted-text">No attachments or artifacts found.</p>}
                </div>
              </section>}
            </div>
          </>
        )}
      </section>
    </div>
  );
}
