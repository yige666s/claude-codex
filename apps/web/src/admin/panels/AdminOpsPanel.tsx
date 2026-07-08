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
import type { AdminHealthStatus, AdminUser, Asset, AuditLogRecord, AuditLogSummary, DeepAgentReplayReport, DeepAgentResumeRequest, DeepAgentWorkflowSummary, EvaluationResult, EvaluationReview, EvaluationRun, EvaluationRunSummary, Job, JobEvent, LLMGovernanceConfig, LLMQuotaAdminSummary, LLMUsageAdminSummary, LoopTriggerRecord, RiskReviewSummary, RiskSummary, Session, WorkflowRun, WorkflowStepRun } from "../../types";

export function AdminOpsPanel({ api, adminToken }: { api: ApiClient; adminToken: string }) {
  const [userID, setUserID] = useState("");
  const [query, setQuery] = useState("");
  const [statusFilter, setStatusFilter] = useState("all");
  const [assetKind, setAssetKind] = useState("all");
  const [sessions, setSessions] = useState<Session[]>([]);
  const [jobs, setJobs] = useState<Job[]>([]);
  const [assets, setAssets] = useState<Asset[]>([]);
  const [events, setEvents] = useState<JobEvent[]>([]);
  const [loopTriggers, setLoopTriggers] = useState<LoopTriggerRecord[]>([]);
  const [workflows, setWorkflows] = useState<WorkflowRun[]>([]);
  const [workflowSteps, setWorkflowSteps] = useState<WorkflowStepRun[]>([]);
  const [deepAgentSummary, setDeepAgentSummary] = useState<DeepAgentWorkflowSummary | null>(null);
  const [deepAgentReplay, setDeepAgentReplay] = useState<DeepAgentReplayReport | null>(null);
  const [selectedSessionID, setSelectedSessionID] = useState("");
  const [selectedJobID, setSelectedJobID] = useState("");
  const [selectedWorkflowID, setSelectedWorkflowID] = useState("");
  const [loading, setLoading] = useState(false);
  const [actionBusy, setActionBusy] = useState("");
  const [resumeStatePatch, setResumeStatePatch] = useState("");
  const [resumeBudgetActions, setResumeBudgetActions] = useState("");
  const [resumeBudgetDuration, setResumeBudgetDuration] = useState("");
  const [reviewEditPatch, setReviewEditPatch] = useState("");
  const [triggerObjective, setTriggerObjective] = useState("");
  const [triggerSource, setTriggerSource] = useState("admin_ops");
  const [triggerDedupeKey, setTriggerDedupeKey] = useState("");
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [opsTab, setOpsTab] = useState<"session" | "jobs" | "events" | "workflows" | "triggers" | "assets">("jobs");
  const token = adminToken.trim();
  const cleanUserID = userID.trim();
  const selectedSession = sessions.find((session) => session.id === selectedSessionID) || null;
  const selectedJob = jobs.find((job) => job.id === selectedJobID) || null;
  const selectedWorkflow = workflows.find((workflow) => workflow.id === selectedWorkflowID) || null;
  const opsTabs: Array<AdminTabOption<typeof opsTab>> = [
    { id: "session", label: "Session", icon: <MessageCircle size={15} />, count: sessions.length },
    { id: "jobs", label: "Jobs", icon: <Briefcase size={15} />, count: jobs.length },
    { id: "events", label: "Events", icon: <Activity size={15} />, count: events.length },
    { id: "workflows", label: "Workflows", icon: <Settings size={15} />, count: workflows.length },
    { id: "triggers", label: "Triggers", icon: <Clock size={15} />, count: loopTriggers.length },
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
      const [nextSessions, nextJobs, nextAssets, nextWorkflows, nextLoopTriggers] = await Promise.all([
        api.adminOpsSessions(token, cleanUserID, { q: query, limit: 100 }),
        api.adminOpsJobs(token, cleanUserID, { sessionId: sessionID, q: query, status: statusFilter, limit: 100 }),
        api.adminOpsAssets(token, cleanUserID, { sessionId: sessionID, jobId: jobID, q: query, kind: assetKind, limit: 100 }),
        api.adminOpsWorkflows(token, cleanUserID, { sessionId: sessionID, jobId: jobID, status: statusFilter, limit: 100 }),
        api.adminOpsLoopTriggers(token, cleanUserID, { sessionId: sessionID, limit: 100 })
      ]);
      setSessions(nextSessions);
      setJobs(nextJobs);
      setAssets(nextAssets);
      setWorkflows(nextWorkflows);
      setLoopTriggers(nextLoopTriggers);
      const nextSessionID = sessionID && nextSessions.some((session) => session.id === sessionID) ? sessionID : "";
      const nextJobID = jobID && nextJobs.some((job) => job.id === jobID) ? jobID : nextJobs[0]?.id || "";
      const nextWorkflowID = selectPrimaryWorkflowID(nextWorkflows, selectedWorkflowID);
      setSelectedSessionID(nextSessionID);
      setSelectedJobID(nextJobID);
      setSelectedWorkflowID(nextWorkflowID);
      if (nextJobID) {
        const nextEvents = await api.adminOpsJobEvents(token, cleanUserID, nextJobID, 500);
        setEvents(nextEvents);
      } else {
        setEvents([]);
      }
      if (nextWorkflowID) {
        const detail = await api.adminOpsWorkflow(token, cleanUserID, nextWorkflowID);
        setWorkflowSteps(detail.steps);
        setDeepAgentSummary(detail.deepAgent || null);
        setDeepAgentReplay(null);
      } else {
        setWorkflowSteps([]);
        setDeepAgentSummary(null);
        setDeepAgentReplay(null);
      }
      setNotice(`Loaded ${nextSessions.length} sessions, ${nextJobs.length} jobs, ${nextWorkflows.length} workflows, ${nextLoopTriggers.length} triggers, ${nextAssets.length} assets`);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setLoading(false);
    }
  };

  const openSession = async (sessionID: string) => {
    setSelectedSessionID(sessionID);
    setSelectedJobID("");
    setSelectedWorkflowID("");
    setEvents([]);
    setWorkflowSteps([]);
    setDeepAgentSummary(null);
    setDeepAgentReplay(null);
    await loadOps(sessionID, "");
  };

  const openJob = async (jobID: string) => {
    setSelectedJobID(jobID);
    if (!token || !cleanUserID) return;
    setError("");
    try {
      const [nextEvents, nextWorkflows] = await Promise.all([
        api.adminOpsJobEvents(token, cleanUserID, jobID, 500),
        api.adminOpsWorkflows(token, cleanUserID, { sessionId: selectedSessionID, jobId: jobID, status: statusFilter, limit: 100 })
      ]);
      setEvents(nextEvents);
      setWorkflows(nextWorkflows);
      const nextWorkflowID = selectPrimaryWorkflowID(nextWorkflows);
      setSelectedWorkflowID(nextWorkflowID);
      if (nextWorkflowID) {
        const detail = await api.adminOpsWorkflow(token, cleanUserID, nextWorkflowID);
        setWorkflowSteps(detail.steps);
        setDeepAgentSummary(detail.deepAgent || null);
        setDeepAgentReplay(null);
      } else {
        setWorkflowSteps([]);
        setDeepAgentSummary(null);
        setDeepAgentReplay(null);
      }
    } catch (err) {
      setError(errorMessage(err));
    }
  };

  const openWorkflow = async (runID: string) => {
    setSelectedWorkflowID(runID);
    if (!token || !cleanUserID) return;
    setError("");
    try {
      const detail = await api.adminOpsWorkflow(token, cleanUserID, runID);
      setWorkflowSteps(detail.steps);
      setDeepAgentSummary(detail.deepAgent || null);
      setDeepAgentReplay(null);
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

  const createManualLoopTrigger = async () => {
    if (!token || !cleanUserID) return;
    const objective = triggerObjective.trim();
    if (!objective) {
      setError("Enter an objective for the manual loop trigger.");
      return;
    }
    setActionBusy("create-loop-trigger");
    setError("");
    try {
      const result = await api.adminOpsSubmitLoopDiscovery(token, cleanUserID, {
        session_id: selectedSessionID || undefined,
        trigger_type: "manual",
        source: triggerSource.trim() || "admin_ops",
        dedupe_key: triggerDedupeKey.trim() || undefined,
        objective
      });
      setNotice(result.duplicate ? `Duplicate trigger reused ${result.trigger.job_id || result.trigger.id}` : `Created loop job ${result.trigger.job_id}`);
      setTriggerObjective("");
      setTriggerDedupeKey("");
      await loadOps(selectedSessionID, result.trigger.job_id || selectedJobID);
      if (result.trigger.job_id) setSelectedJobID(result.trigger.job_id);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setActionBusy("");
    }
  };

  const resumeWorkflow = async (request: DeepAgentResumeRequest = {}) => {
    if (!selectedWorkflow || !token || !cleanUserID) return;
    setActionBusy(request.review_decision?.action ? `resume-${request.review_decision.action}` : "resume-workflow");
    setError("");
    try {
      const hint = deepAgentSummary?.recovery?.additional_budget_hint || {};
      await api.adminOpsResumeWorkflow(token, cleanUserID, selectedWorkflow.id, {
        ...request,
        additional_budget: {
          max_actions: hint.max_actions || 10,
          max_duration_ms: hint.max_duration_ms || 5 * 60 * 1000,
          ...request.additional_budget
        }
      });
      setNotice(`Resumed ${selectedWorkflow.id}`);
      await openWorkflow(selectedWorkflow.id);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setActionBusy("");
    }
  };

  const resumeWithForm = async () => {
    const statePatch = parseJSONPatch(resumeStatePatch, "state patch");
    if (statePatch === null) return;
    const request: DeepAgentResumeRequest = {};
    if (statePatch) request.state_patch = statePatch;
    const maxActions = Number(resumeBudgetActions);
    const maxDuration = Number(resumeBudgetDuration);
    if ((Number.isFinite(maxActions) && maxActions > 0) || (Number.isFinite(maxDuration) && maxDuration > 0)) {
      request.additional_budget = {};
      if (Number.isFinite(maxActions) && maxActions > 0) request.additional_budget.max_actions = maxActions;
      if (Number.isFinite(maxDuration) && maxDuration > 0) request.additional_budget.max_duration_ms = maxDuration * 60 * 1000;
    }
    await resumeWorkflow(request);
  };

  const reviewLearning = async (candidateID: string, action: "accept" | "reject" | "expire" | "rollback") => {
    if (!token || !cleanUserID || !candidateID) return;
    setActionBusy(`learning-${candidateID}-${action}`);
    setError("");
    try {
      const updated = await api.adminOpsReviewDeepAgentLearning(token, cleanUserID, candidateID, action, "admin ops review");
      setNotice(`Learning ${action}: ${candidateID}`);
      setDeepAgentSummary((summary) => {
        if (!summary?.learnings?.length) return summary;
        const reviewStatus = typeof updated.metadata?.review_status === "string" ? updated.metadata.review_status : action === "rollback" ? "rolled_back" : action === "expire" ? "expired" : action === "accept" ? "accepted" : "rejected";
        return {
          ...summary,
          learnings: summary.learnings.map((learning) => learning.id === candidateID
            ? {
                ...learning,
                status: reviewStatus,
                memory_item_id: updated.id,
                expires_at: updated.expires_at || learning.expires_at,
                reviewed_by: typeof updated.metadata?.reviewed_by === "string" ? updated.metadata.reviewed_by : learning.reviewed_by,
                reviewed_at: typeof updated.metadata?.reviewed_at === "string" ? updated.metadata.reviewed_at : learning.reviewed_at,
                user_confirmed: updated.metadata?.user_confirmed === true
              }
            : learning)
        };
      });
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setActionBusy("");
    }
  };

  const editReviewAction = async () => {
    const argsPatch = parseJSONPatch(reviewEditPatch, "review edit patch");
    if (argsPatch === null || !deepAgentSummary?.recovery?.review_pending) return;
    await resumeWorkflow({
      review_decision: {
        action: "edit",
        step_id: deepAgentSummary.recovery.review_step_id,
        action_hash: deepAgentSummary.recovery.review_action_hash,
        args_patch: argsPatch || {},
        reason: "edited from Admin Ops"
      }
    });
  };

  const parseJSONPatch = (value: string, label: string): Record<string, unknown> | undefined | null => {
    const text = value.trim();
    if (!text) return undefined;
    try {
      const parsed = JSON.parse(text);
      if (!parsed || Array.isArray(parsed) || typeof parsed !== "object") {
        setError(`${label} must be a JSON object.`);
        return null;
      }
      return parsed as Record<string, unknown>;
    } catch {
      setError(`${label} is not valid JSON.`);
      return null;
    }
  };

  const loadDeepAgentReplay = async () => {
    if (!selectedWorkflow || !token || !cleanUserID) return;
    setActionBusy("replay");
    setError("");
    try {
      const replay = await api.adminOpsDeepAgentReplay(token, cleanUserID, selectedWorkflow.id);
      setDeepAgentReplay(replay);
      setNotice(`Loaded replay for ${selectedWorkflow.id}`);
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
              <AdminMetric label="Workflows" value={String(workflows.length)} />
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
              {opsTab === "workflows" && <section className="admin-card wide">
                <div className="admin-card-head">
                  <h3>Workflow runs</h3>
                  {selectedWorkflow && <StatusBadge value={selectedWorkflow.status} />}
                </div>
                <div className="admin-table">
                  {workflows.slice(0, 12).map((workflow) => (
                    <Button key={workflow.id} className={`admin-table-row button-row ${workflow.id === selectedWorkflowID ? "active" : ""}`} onClick={() => openWorkflow(workflow.id)}>
                      <StatusBadge value={workflow.status} />
                      <span>{workflow.name}<small>/{workflow.version || "v1"}</small></span>
                      <small>{workflow.id}</small>
                      {workflow.job_id && <em>{workflow.job_id}</em>}
                    </Button>
                  ))}
                  {!workflows.length && <p className="muted-text">No workflow runs found for this scope.</p>}
                </div>
                {selectedWorkflow && (
                  <>
	                    <div className="admin-facts">
	                      <SkillFact label="Run ID" value={selectedWorkflow.id} />
	                      <SkillFact label="Session ID" value={selectedWorkflow.session_id || "None"} />
	                      <SkillFact label="Job ID" value={selectedWorkflow.job_id || "None"} />
	                      <SkillFact label="Started" value={formatTime(selectedWorkflow.started_at || selectedWorkflow.created_at)} />
	                      <SkillFact label="Finished" value={formatTime(selectedWorkflow.finished_at || "")} />
	                      <SkillFact label="Error" value={selectedWorkflow.error || "None"} />
	                    </div>
	                    {deepAgentSummary?.recovery?.resume_available && (
	                      <div className="admin-action-row">
	                        <Button className="skill-action" onClick={() => resumeWorkflow()} disabled={Boolean(actionBusy)}>
	                          <PlayCircle size={15} />
	                          <span>{actionBusy === "resume-workflow" ? "Resuming" : "Resume DeepAgent"}</span>
	                        </Button>
	                        {deepAgentSummary.recovery.review_pending && (
	                          <>
	                            <Button className="skill-action" onClick={() => resumeWorkflow({ review_decision: { action: "approve", step_id: deepAgentSummary.recovery?.review_step_id, action_hash: deepAgentSummary.recovery?.review_action_hash } })} disabled={Boolean(actionBusy)}>
	                              <ShieldCheck size={15} />
	                              <span>{actionBusy === "resume-approve" ? "Approving" : "Approve action"}</span>
	                            </Button>
	                            <Button className="skill-action danger-outline" onClick={() => resumeWorkflow({ review_decision: { action: "reject", step_id: deepAgentSummary.recovery?.review_step_id, action_hash: deepAgentSummary.recovery?.review_action_hash, reason: "rejected from Admin Ops" } })} disabled={Boolean(actionBusy)}>
	                              <Square size={15} />
	                              <span>{actionBusy === "resume-reject" ? "Rejecting" : "Reject action"}</span>
	                            </Button>
	                          </>
	                        )}
	                      </div>
	                    )}
	                    {deepAgentSummary?.present && (
	                      <div className="admin-deep-agent-panel">
                        <div className="admin-card-head">
                          <h3>DeepAgent</h3>
                          <StatusBadge value={deepAgentSummary.status || "unknown"} />
                        </div>
                        <div className="admin-facts">
                          <SkillFact label="Goal" value={deepAgentSummary.goal || "None"} />
                          <SkillFact label="Current step" value={deepAgentSummary.current_step?.title || deepAgentSummary.current_step_id || "None"} />
	                          <SkillFact label="Actions" value={String(deepAgentSummary.action_count || 0)} />
	                          <SkillFact label="Completed / failed" value={`${deepAgentSummary.completed_count || 0} / ${deepAgentSummary.failed_count || 0}`} />
	                          <SkillFact label="Blocker" value={deepAgentSummary.blocker || "None"} />
	                          <SkillFact label="Final verifier" value={formatRecordSummary(deepAgentSummary.final_verifier)} />
	                          <SkillFact label="Evaluator verdict" value={`${String(deepAgentSummary.evaluator_verdict?.verdict || "unknown")} · ${String(deepAgentSummary.evaluator_verdict?.reason || "No reason")}`} />
	                        </div>
	                        {deepAgentSummary.evaluator_verdict && (
	                          <div className="admin-facts">
	                            <SkillFact label="Evaluator confidence" value={String(deepAgentSummary.evaluator_verdict.confidence || "unknown")} />
	                            <SkillFact label="Failed criteria" value={(deepAgentSummary.evaluator_verdict.failed_criteria || []).join(", ") || "None"} />
	                            <SkillFact label="Repair plan" value={(deepAgentSummary.evaluator_verdict.repair_plan || []).join(" ") || "None"} />
	                            <SkillFact label="Source coverage" value={formatRecordSummary(deepAgentSummary.evaluator_verdict.source_coverage)} />
	                            <SkillFact label="Rubric coverage" value={formatRecordSummary(deepAgentSummary.evaluator_verdict.rubric_coverage)} />
	                          </div>
	                        )}
	                        <div className="admin-facts">
	                          <SkillFact label="Task type" value={String(deepAgentSummary.metrics?.task_type || "unknown")} />
	                          <SkillFact label="Trigger" value={String(deepAgentSummary.metrics?.trigger_type || "manual")} />
	                          <SkillFact label="Duration" value={String(deepAgentSummary.metrics?.duration_ms || 0) + "ms"} />
	                          <SkillFact label="Evidence / artifacts" value={`${String(deepAgentSummary.metrics?.evidence_count || 0)} / ${String(deepAgentSummary.metrics?.artifact_count || 0)}`} />
	                          <SkillFact label="Verifier checks" value={`${String(deepAgentSummary.metrics?.verifier_checks || 0)} checks, ${String(deepAgentSummary.metrics?.verifier_failed || 0)} failed`} />
	                          <SkillFact label="Governance" value={deepAgentSummary.governance?.policy_blocked ? deepAgentSummary.governance.policy_block_reason || "Policy blocked" : deepAgentSummary.governance?.kill_switch ? "Kill switch enabled" : "Active"} />
	                        </div>
	                        {deepAgentSummary.deep_research && (
	                          <div className="admin-facts">
	                            <SkillFact label="Deep research" value={`${deepAgentSummary.deep_research.status || "unknown"} · ${deepResearchWorkerCount(deepAgentSummary.deep_research)} workers`} />
	                            <SkillFact label="Worker backend" value={deepAgentSummary.deep_research.config?.worker_backend || "inline"} />
	                            <SkillFact label="Concurrency" value={String(deepAgentSummary.deep_research.plan?.max_concurrency || deepAgentSummary.deep_research.config?.max_concurrency || 0)} />
	                            <SkillFact label="Aggregate" value={deepAgentSummary.deep_research.aggregate?.summary || deepAgentSummary.deep_research.aggregate?.status || "None"} />
	                          </div>
	                        )}
	                        <div className="admin-action-row">
	                          <Button className="skill-action" onClick={loadDeepAgentReplay} disabled={Boolean(actionBusy)}>
	                            <RefreshCw size={15} />
	                            <span>{actionBusy === "replay" ? "Loading replay" : "Replay decisions"}</span>
	                          </Button>
	                        </div>
	                        {deepAgentSummary.recovery && (
	                          <>
	                            <div className="admin-facts">
	                              <SkillFact label="Recovery" value={deepAgentSummary.recovery.user_facing_reason || deepAgentSummary.recovery.recommended_next_action || "None"} />
	                              <SkillFact label="Blocked category" value={deepAgentSummary.recovery.blocked_category || "None"} />
	                              <SkillFact label="Missing info" value={(deepAgentSummary.recovery.missing_info || []).join(", ") || "None"} />
	                              <SkillFact label="Review action" value={deepAgentSummary.recovery.review_action_hash || "None"} />
	                              <SkillFact label="Last action" value={deepAgentSummary.recovery.last_action?.hash || deepAgentSummary.recovery.last_action?.step_id || "None"} />
	                            </div>
	                            {deepAgentSummary.recovery.resume_available && (
	                              <div className="admin-recovery-editor">
	                                <label className="admin-field">
	                                  <span>补充信息 / state patch</span>
	                                  <Textarea value={resumeStatePatch} onChange={(event) => setResumeStatePatch(event.currentTarget.value)} placeholder='{"missing_context":"..."}' rows={3} />
	                                </label>
	                                <div className="admin-filter-row">
	                                  <label className="admin-field">
	                                    <span>追加 actions</span>
	                                    <Input value={resumeBudgetActions} onChange={(event) => setResumeBudgetActions(event.currentTarget.value)} inputMode="numeric" placeholder="10" />
	                                  </label>
	                                  <label className="admin-field">
	                                    <span>追加分钟</span>
	                                    <Input value={resumeBudgetDuration} onChange={(event) => setResumeBudgetDuration(event.currentTarget.value)} inputMode="numeric" placeholder="5" />
	                                  </label>
	                                </div>
	                                <Button className="skill-action" onClick={resumeWithForm} disabled={Boolean(actionBusy)}>
	                                  <PlayCircle size={15} />
	                                  <span>{actionBusy === "resume-workflow" ? "Resuming" : "Resume with patch"}</span>
	                                </Button>
	                                {deepAgentSummary.recovery.review_pending && (
	                                  <>
	                                    <label className="admin-field">
	                                      <span>Edit high-risk action args</span>
	                                      <Textarea value={reviewEditPatch} onChange={(event) => setReviewEditPatch(event.currentTarget.value)} placeholder='{"query":"safer query"}' rows={3} />
	                                    </label>
	                                    <Button className="skill-action" onClick={editReviewAction} disabled={Boolean(actionBusy)}>
	                                      <Settings size={15} />
	                                      <span>{actionBusy === "resume-edit" ? "Editing" : "Edit and resume"}</span>
	                                    </Button>
	                                  </>
	                                )}
	                              </div>
	                            )}
	                          </>
	                        )}
	                        {!!deepAgentSummary.timeline?.length && (
	                          <div className="admin-table compact">
	                            {deepAgentSummary.timeline.slice(-8).map((item, index) => (
	                              <div key={`${item.kind}-${item.step_id || item.action_hash || index}`} className="admin-table-row">
	                                <StatusBadge value={item.kind} />
	                                <span>{item.title || item.step_id || item.tool || "timeline"}<small>{item.summary || ""}</small></span>
	                                <small>{item.status || item.action_hash || ""}</small>
	                              </div>
	                            ))}
	                          </div>
	                        )}
	                        {deepAgentReplay && (
	                          <div className="admin-table compact">
	                            {deepAgentReplay.trace_summary && (
	                              <div className="admin-table-row">
	                                <StatusBadge value={String(deepAgentReplay.trace_summary.category || deepAgentReplay.trace_summary.final_status || "trace")} />
	                                <span>Root cause<small>{deepAgentReplay.trace_summary.root_cause || "No root cause available"}</small></span>
	                                <small>{deepAgentReplay.trace_summary.suggested_repair || deepAgentReplay.trace_summary.failed_phase || ""}</small>
	                              </div>
	                            )}
	                            <details className="admin-table-row" open>
	                              <summary>
	                                <StatusBadge value={deepAgentReplay.status || "replay"} />
	                                <span>Replay decisions<small>{(deepAgentReplay.findings || []).map((finding) => finding.code).join(", ") || "no findings"}</small></span>
	                                <small>{deepAgentReplay.run_id}</small>
	                              </summary>
	                              <pre>{JSON.stringify(deepAgentReplay, null, 2)}</pre>
	                            </details>
	                          </div>
	                        )}
                        <div className="admin-table">
                          {(deepAgentSummary.plan?.steps || []).slice(0, 8).map((step) => (
                            <div key={step.id} className="admin-table-row">
                              <StatusBadge value={step.status || "pending"} />
                              <span>{step.title || step.id}<small>{step.done_condition || step.intent || ""}</small></span>
                              {step.risk_level && <em>{step.risk_level}</em>}
                            </div>
                          ))}
                          {!(deepAgentSummary.plan?.steps || []).length && <p className="muted-text">No DeepAgent plan steps recorded.</p>}
                        </div>
                        {!!deepAgentSummary.action_history?.length && (
                          <div className="admin-table compact">
                            {deepAgentSummary.action_history.slice(-5).map((action, index) => (
                              <div key={`${action.hash || action.step_id}-${index}`} className="admin-table-row">
                                <StatusBadge value={action.tool || "action"} />
                                <span>{action.step_id}</span>
                                <small>{action.hash || "no hash"}</small>
                              </div>
                            ))}
                          </div>
                        )}
                        {!!deepAgentSummary.routes?.length && (
                          <div className="admin-table compact">
                            {deepAgentSummary.routes.slice(-6).map((route, index) => (
                              <details key={`${String(route.action_hash || route.step_id || index)}`} className="admin-table-row">
                                <summary>
                                  <StatusBadge value={String(route.mode || "route")} />
                                  <span>{String(route.step_id || "route")}<small>{String(route.executor || "")} · {String(route.deliverable_type || "none")}</small></span>
                                  <small>{String(route.version || "v1")}</small>
                                </summary>
                                <pre>{JSON.stringify(route, null, 2)}</pre>
                              </details>
                            ))}
                          </div>
                        )}
                        {!!deepAgentSummary.evidence?.length && (
                          <div className="admin-table compact">
                            {deepAgentSummary.evidence.slice(-6).map((evidence, index) => (
                              <details key={`${String(evidence.step_id || index)}-evidence`} className="admin-table-row">
                                <summary>
                                  <StatusBadge value="evidence" />
                                  <span>{String(evidence.step_id || "step")}<small>{String(evidence.summary || "")}</small></span>
                                  <small>{Array.isArray(evidence.artifacts) ? `${evidence.artifacts.length} artifacts` : ""}</small>
                                </summary>
                                <pre>{JSON.stringify(evidence, null, 2)}</pre>
                              </details>
                            ))}
                          </div>
                        )}
                        {!!deepAgentSummary.artifact_refs?.length && (
                          <div className="admin-table compact">
                            {deepAgentSummary.artifact_refs.slice(0, 8).map((artifact, index) => (
                              <div key={`${String(artifact.id || artifact.filename || index)}-artifact`} className="admin-table-row">
                                <StatusBadge value="artifact" />
                                <span>{String(artifact.filename || artifact.id || "artifact")}<small>{String(artifact.content_type || "")}</small></span>
                                <small>{String(artifact.id || "")}</small>
                              </div>
                            ))}
                          </div>
                        )}
                        {!!deepAgentSummary.learnings?.length && (
                          <div className="admin-table compact">
                            {deepAgentSummary.learnings.slice(0, 4).map((learning) => (
                              <div key={learning.id} className="admin-table-row">
                                <StatusBadge value={learning.status || learning.type} />
                                <span>
                                  {learning.content}
                                  <small>
                                    {learning.type}
                                    {learning.run_id ? ` · run ${learning.run_id}` : ""}
                                    {learning.step_id ? ` · step ${learning.step_id}` : ""}
                                    {learning.evidence_id ? ` · evidence ${learning.evidence_id}` : ""}
                                    {typeof learning.confidence === "number" ? ` · conf ${Math.round(learning.confidence * 100)}%` : ""}
                                    {learning.source_job ? ` · job ${learning.source_job}` : ""}
                                    {learning.owner ? ` · owner ${learning.owner}` : ""}
                                    {learning.expires_at ? ` · expires ${new Date(learning.expires_at).toLocaleString()}` : ""}
                                  </small>
                                  {!!learning.evidence_refs?.length && (
                                    <small>{learning.evidence_refs.slice(0, 3).join(" · ")}</small>
                                  )}
                                  {learning.policy_reason && <small>{learning.policy_reason}</small>}
                                </span>
                                <div className="admin-inline-actions">
                                  {(learning.status === "pending" || learning.status === "candidate") && (
                                    <>
                                      <Button
                                        type="button"
                                        size="xs"
                                        variant="secondary"
                                        disabled={actionBusy === `learning-${learning.id}-accept`}
                                        onClick={() => void reviewLearning(learning.id, "accept")}
                                      >
                                        Accept
                                      </Button>
                                      <Button
                                        type="button"
                                        size="xs"
                                        variant="outline"
                                        disabled={actionBusy === `learning-${learning.id}-reject`}
                                        onClick={() => void reviewLearning(learning.id, "reject")}
                                      >
                                        Reject
                                      </Button>
                                      <Button
                                        type="button"
                                        size="xs"
                                        variant="outline"
                                        disabled={actionBusy === `learning-${learning.id}-expire`}
                                        onClick={() => void reviewLearning(learning.id, "expire")}
                                      >
                                        Expire
                                      </Button>
                                    </>
                                  )}
                                  {learning.status === "accepted" && (
                                    <>
                                      <Button
                                        type="button"
                                        size="xs"
                                        variant="outline"
                                        disabled={actionBusy === `learning-${learning.id}-expire`}
                                        onClick={() => void reviewLearning(learning.id, "expire")}
                                      >
                                        Expire
                                      </Button>
                                      <Button
                                        type="button"
                                        size="xs"
                                        variant="destructive"
                                        disabled={actionBusy === `learning-${learning.id}-rollback`}
                                        onClick={() => void reviewLearning(learning.id, "rollback")}
                                      >
                                        Rollback
                                      </Button>
                                    </>
                                  )}
                                </div>
                              </div>
                            ))}
                          </div>
                        )}
                      </div>
                    )}
                    <div className="admin-card-head">
                      <h3>Steps</h3>
                      <small>{workflowSteps.length}</small>
                    </div>
                    <div className="admin-table">
                      {workflowSteps.map((step) => (
                        <div key={step.id} className="admin-table-row">
                          <StatusBadge value={step.status} />
                          <span>{step.step_name}</span>
                          <small>{formatWorkflowStepSummary(step)}</small>
                          {step.error && <em>{step.error}</em>}
                        </div>
                      ))}
                      {!workflowSteps.length && <p className="muted-text">No steps recorded for this workflow run.</p>}
                    </div>
                  </>
                )}
              </section>}
              {opsTab === "triggers" && <section className="admin-card wide">
                <div className="admin-card-head">
                  <h3>Loop Triggers</h3>
                  <small>{loopTriggers.length}</small>
                </div>
                <div className="admin-filter-row">
                  <label className="admin-field">
                    <span>Objective</span>
                    <Input value={triggerObjective} onChange={(event) => setTriggerObjective(event.currentTarget.value)} placeholder="Run a loop job..." />
                  </label>
                  <label className="admin-field">
                    <span>Source</span>
                    <Input value={triggerSource} onChange={(event) => setTriggerSource(event.currentTarget.value)} placeholder="admin_ops" />
                  </label>
                  <label className="admin-field">
                    <span>Dedupe key</span>
                    <Input value={triggerDedupeKey} onChange={(event) => setTriggerDedupeKey(event.currentTarget.value)} placeholder="optional" />
                  </label>
                  <Button className="skill-action" onClick={createManualLoopTrigger} disabled={Boolean(actionBusy)}>
                    <PlayCircle size={15} />
                    <span>{actionBusy === "create-loop-trigger" ? "Creating" : "Create manual"}</span>
                  </Button>
                </div>
                <div className="admin-table">
                  {loopTriggers.slice(0, 20).map((trigger) => (
                    <div key={trigger.id} className="admin-table-row">
                      <StatusBadge value={trigger.status || trigger.trigger_type} />
                      <span>
                        {trigger.trigger_type}<small>{trigger.source || "unknown"} · {trigger.dedupe_key}</small>
                      </span>
                      <small>{formatTime(trigger.created_at)}</small>
                      {trigger.job_id && <em>{trigger.job_id}</em>}
                      {trigger.failure_reason && <em>{trigger.failure_reason}</em>}
                    </div>
                  ))}
                  {!loopTriggers.length && <p className="muted-text">No loop discovery events found.</p>}
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

function formatWorkflowStepSummary(step: WorkflowStepRun): string {
  const output = step.output || {};
  const input = step.input || {};
  const outputKeys = Object.keys(output);
  const inputKeys = Object.keys(input);
  const selectedKeys = [
    "intent",
    "execution_mode",
    "result_count",
    "candidate_count",
    "changed_count",
    "output_length",
    "final_status"
  ];
  const parts = selectedKeys
    .filter((key) => output[key] != null || input[key] != null)
    .slice(0, 3)
    .map((key) => `${key}=${String(output[key] ?? input[key])}`);
  if (parts.length > 0) return parts.join(" · ");
  if (outputKeys.length > 0) return `output: ${outputKeys.slice(0, 4).join(", ")}`;
  if (inputKeys.length > 0) return `input: ${inputKeys.slice(0, 4).join(", ")}`;
  return formatTime(step.started_at);
}

function selectPrimaryWorkflowID(workflows: WorkflowRun[], selectedID = ""): string {
  if (selectedID && workflows.some((workflow) => workflow.id === selectedID)) return selectedID;
  return workflows.find((workflow) => workflow.name === "deep_agent_task")?.id || workflows[0]?.id || "";
}

function deepResearchWorkerCount(run: NonNullable<DeepAgentWorkflowSummary["deep_research"]>): number {
  if (run.worker_runs) return Object.keys(run.worker_runs).length;
  return run.plan?.nodes?.length || 0;
}

function formatRecordSummary(record?: Record<string, unknown>): string {
  if (!record) return "None";
  const done = record.done;
  const reason = record.reason;
  const parts = [
    typeof done === "boolean" ? `done=${done}` : "",
    typeof reason === "string" && reason ? reason : ""
  ].filter(Boolean);
  return parts.join(" · ") || JSON.stringify(record);
}
