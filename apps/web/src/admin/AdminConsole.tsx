import { ReactNode, useEffect, useMemo, useRef, useState } from "react";
import {
  Activity,
  AlertCircle,
  Archive,
  Briefcase,
  Clock,
  Database,
  Download,
  FileText,
  FileUp,
  Info,
  LogOut,
  MessageCircle,
  PlayCircle,
  RefreshCw,
  Search,
  Settings,
  ShieldCheck,
  Sparkles,
  Square,
  UserX,
  X
} from "lucide-react";
import { ApiClient, ApiError } from "../api/client";
import type {
  AdminHealthStatus,
  AdminSkill,
  AdminUser,
  Asset,
  AuditLogRecord,
  AuditLogSummary,
  EvaluationResult,
  EvaluationReview,
  EvaluationRun,
  EvaluationRunSummary,
  Job,
  JobEvent,
  LLMGovernanceConfig,
  LLMQuotaAdminSummary,
  LLMUsageAdminSummary,
  RiskEvent,
  RiskReviewSummary,
  RiskSummary,
  Session,
  Skill,
  SkillExecution,
  SkillExecutionSummary,
  SkillPolicyConfig,
  SkillReviewResult,
  SkillVersion
} from "../types";
import { sessionTitle } from "../lib/sessionTitle";

type AdminSection = "skills" | "users" | "jobs-assets" | "health-cost" | "audit" | "evaluation";
type AdminTabOption<T extends string> = {
  id: T;
  label: string;
  description?: string;
  icon?: ReactNode;
  count?: number;
};

const brandLogoSrc = "/logo.png";
const terminalJobs = new Set(["succeeded", "failed", "cancelled"]);

function BrandLogo({ className = "brand-mark" }: { className?: string }) {
  return (
    <span className={className} aria-hidden="true">
      <img src={brandLogoSrc} alt="" />
    </span>
  );
}

function AdminTabs<T extends string>({
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
    <nav className={`admin-tabs${compact ? " compact" : ""}`} aria-label={label}>
      {tabs.map((tab) => (
        <button
          key={tab.id}
          type="button"
          className={tab.id === active ? "active" : ""}
          onClick={() => onChange(tab.id)}
        >
          {tab.icon}
          <span>{tab.label}</span>
          {typeof tab.count === "number" && <small>{tab.count}</small>}
        </button>
      ))}
    </nav>
  );
}

function AdminConsole({
  api,
  adminToken,
  userLabel,
  onAdminTokenChange,
  onExit,
  onLogout
}: {
  api: ApiClient;
  adminToken: string;
  userLabel: string;
  onAdminTokenChange: (token: string) => void;
  onExit: () => void;
  onLogout: () => void;
}) {
  const [adminSection, setAdminSection] = useState<AdminSection>("skills");
  const [skills, setSkills] = useState<AdminSkill[]>([]);
  const [selectedName, setSelectedName] = useState("");
  const [query, setQuery] = useState("");
  const [statusFilter, setStatusFilter] = useState("all");
  const [loading, setLoading] = useState(false);
  const [detailsLoading, setDetailsLoading] = useState(false);
  const [actionBusy, setActionBusy] = useState("");
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [review, setReview] = useState<SkillReviewResult | null>(null);
  const [versions, setVersions] = useState<SkillVersion[]>([]);
  const [executions, setExecutions] = useState<SkillExecution[]>([]);
  const [summary, setSummary] = useState<SkillExecutionSummary | null>(null);
  const [policyTarget, setPolicyTarget] = useState<AdminSkill | null>(null);
  const [skillTab, setSkillTab] = useState<"overview" | "review" | "executions" | "versions">("overview");
  const token = adminToken.trim();
  const adminSections: Array<AdminTabOption<AdminSection>> = [
    { id: "skills", label: "Skills", description: "Publish, review, configure policy, and inspect execution health for registry-backed skills.", icon: <Sparkles size={18} />, count: skills.length },
    { id: "users", label: "Users", description: "Search users, inspect account state, and disable, ban, or reactivate access.", icon: <Database size={18} /> },
    { id: "jobs-assets", label: "Jobs & assets", description: "Inspect a user's sessions, queued jobs, replay events, and generated or uploaded assets.", icon: <Briefcase size={18} /> },
    { id: "health-cost", label: "Health & cost", description: "Watch readiness checks, LLM backend health, token usage, latency, and estimated cost.", icon: <Activity size={18} /> },
    { id: "audit", label: "Audit", description: "Review sensitive operations, high-risk actions, request IDs, user scope, and metadata for investigations.", icon: <FileText size={18} /> },
    { id: "evaluation", label: "Evaluation", description: "Run lightweight evaluations over real runtime data, inspect pass/fail findings, and close review items.", icon: <ShieldCheck size={18} /> }
  ];
  const selectedAdminSection = adminSections.find((section) => section.id === adminSection) || adminSections[0];
  const selectedSkill = skills.find((skill) => skill.name === selectedName) || null;
  const reviewIssues = review?.issues || [];
  const skillTabs: Array<AdminTabOption<typeof skillTab>> = [
    { id: "overview", label: "Overview", icon: <Info size={15} /> },
    { id: "review", label: "Review", icon: <ShieldCheck size={15} />, count: reviewIssues.length },
    { id: "executions", label: "Executions", icon: <Activity size={15} />, count: executions.length },
    { id: "versions", label: "Versions", icon: <Archive size={15} />, count: versions.length }
  ];
  const filteredSkills = useMemo(() => skills.filter((skill) => {
    const statusMatches = statusFilter === "all" || skill.status === statusFilter;
    return statusMatches && fuzzyMatch(query, [
      skill.name,
      skill.display_name,
      skill.description,
      skill.category,
      skill.status,
      skill.version,
      skill.source
    ]);
  }).sort(compareSkills), [skills, query, statusFilter]);

  const loadSkills = async () => {
    if (!token) {
      setError("Enter the admin token to load the console.");
      setSkills([]);
      return;
    }
    setLoading(true);
    setError("");
    try {
      const next = await api.adminSkills(token);
      setSkills(next);
      setSelectedName((current) => {
        if (current && next.some((skill) => skill.name === current)) return current;
        return next[0]?.name || "";
      });
      setNotice(`Loaded ${next.length} skills`);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setLoading(false);
    }
  };

  const loadSkillDetails = async (name: string) => {
    if (!token || !name) return;
    setDetailsLoading(true);
    setError("");
    try {
      const [nextReview, nextVersions, nextSummary, nextExecutions] = await Promise.all([
        api.adminSkillReview(name, token),
        api.adminSkillVersions(name, token),
        api.adminSkillAnalytics(name, token),
        api.adminSkillExecutions(name, token, 20)
      ]);
      setReview(nextReview);
      setVersions(nextVersions);
      setSummary(nextSummary);
      setExecutions(nextExecutions);
    } catch (err) {
      setError(errorMessage(err));
      setReview(null);
      setVersions([]);
      setSummary(null);
      setExecutions([]);
    } finally {
      setDetailsLoading(false);
    }
  };

  useEffect(() => {
    if (token) void loadSkills();
  }, []);

  useEffect(() => {
    if (selectedName && token) void loadSkillDetails(selectedName);
  }, [selectedName, token]);

  const refreshSelected = async () => {
    await loadSkills();
    if (selectedName) await loadSkillDetails(selectedName);
  };

  const changeSkillStatus = async (action: "publish" | "unpublish" | "disable") => {
    if (!selectedSkill || !token) return;
    setActionBusy(action);
    setError("");
    try {
      const updated = await api.setAdminSkillStatus(selectedSkill.name, token, action);
      setSkills((current) => current.map((skill) => skill.name === updated.name ? updated : skill));
      setNotice(`/${updated.name} ${action} complete`);
      await loadSkillDetails(updated.name);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setActionBusy("");
    }
  };

  const policySaved = (updated: AdminSkill) => {
    setSkills((current) => current.map((skill) => skill.name === updated.name ? updated : skill));
    setPolicyTarget(null);
    setNotice(`Policy saved for /${updated.name}`);
    void loadSkillDetails(updated.name);
  };

  return (
    <main className="admin-shell">
      <aside className="admin-sidebar">
        <div className="admin-brand">
          <BrandLogo />
          <div>
            <strong>AgentAPI Admin</strong>
            <small>{userLabel}</small>
          </div>
        </div>
        <div className="admin-token-box">
          <label>
            Admin token
            <input
              type="password"
              value={adminToken}
              onChange={(event) => onAdminTokenChange(event.currentTarget.value)}
              placeholder="AGENT_API_ADMIN_TOKEN"
              autoComplete="off"
            />
          </label>
          <button className="primary wide" onClick={loadSkills} disabled={loading || !token || adminSection !== "skills"}>
            {loading ? "Loading" : "Load skill data"}
          </button>
        </div>
        <div className="admin-sidebar-actions">
          <button onClick={onExit}><MessageCircle size={16} /> Back to app</button>
          <button onClick={onLogout}><LogOut size={16} /> Log out</button>
        </div>
      </aside>
      <section className="admin-main">
        <header className="admin-header">
          <div>
            <h1>{selectedAdminSection.label}</h1>
            <p>{selectedAdminSection.description}</p>
          </div>
          {adminSection === "skills" && (
            <button className="skill-action" onClick={refreshSelected} disabled={loading || !token}>
              <RefreshCw size={16} />
              <span>Refresh</span>
            </button>
          )}
        </header>
        <AdminTabs tabs={adminSections} active={adminSection} onChange={setAdminSection} label="Admin sections" />
        {(error || notice) && (
          <div className={`admin-banner ${error ? "error" : "ok"}`} role="status">
            {error ? <AlertCircle size={16} /> : <ShieldCheck size={16} />}
            <span>{error || notice}</span>
            <button className="icon ghost" onClick={() => { setError(""); setNotice(""); }} title="Dismiss" aria-label="Dismiss">
              <X size={14} />
            </button>
          </div>
        )}
        {!token ? (
          <div className="admin-empty">
            <ShieldCheck size={26} />
            <strong>Admin token required</strong>
            <p>Enter `AGENT_API_ADMIN_TOKEN` to load protected admin APIs. This console is separate from the C-end workspace.</p>
          </div>
        ) : adminSection === "users" ? (
          <AdminUsersPanel api={api} adminToken={adminToken} />
        ) : adminSection === "jobs-assets" ? (
          <AdminOpsPanel api={api} adminToken={adminToken} />
        ) : adminSection === "health-cost" ? (
          <AdminHealthCostPanel api={api} adminToken={adminToken} />
        ) : adminSection === "audit" ? (
          <AdminAuditPanel api={api} adminToken={adminToken} />
        ) : adminSection === "evaluation" ? (
          <AdminEvaluationPanel api={api} adminToken={adminToken} />
        ) : (
          <div className="admin-skill-layout">
            <section className="admin-list-panel">
              <div className="admin-list-tools">
                <div className="admin-search">
                  <Search size={16} />
                  <input value={query} onChange={(event) => setQuery(event.currentTarget.value)} placeholder="Search skills" aria-label="Search admin skills" />
                </div>
                <select value={statusFilter} onChange={(event) => setStatusFilter(event.currentTarget.value)} aria-label="Filter skill status">
                  <option value="all">All status</option>
                  <option value="published">Published</option>
                  <option value="unpublished">Unpublished</option>
                  <option value="draft">Draft</option>
                  <option value="disabled">Disabled</option>
                  <option value="archived">Archived</option>
                </select>
              </div>
              <div className="admin-skill-list">
                {filteredSkills.map((skill) => (
                  <button
                    key={skill.name}
                    className={`admin-skill-row ${skill.name === selectedName ? "active" : ""}`}
                    onClick={() => setSelectedName(skill.name)}
                  >
                    <SkillGlyph skill={skill} />
                    <span>
                      <strong>{skill.display_name || skill.name}</strong>
                      <small>/{skill.name}</small>
                    </span>
                    <StatusBadge value={skill.status || "unknown"} />
                  </button>
                ))}
                {!filteredSkills.length && <div className="empty-small">{loading ? "Loading..." : "No skills"}</div>}
              </div>
            </section>
            <section className="admin-detail-panel">
              {!selectedSkill ? (
                <div className="admin-empty">
                  <Sparkles size={24} />
                  <strong>Select a skill</strong>
                  <p>Choose a registry skill to inspect release status, policy, review issues, and execution metrics.</p>
                </div>
              ) : (
                <>
                  <div className="admin-skill-head">
                    <div className="skill-modal-heading">
                      <SkillGlyph skill={selectedSkill} />
                      <div>
                        <h2>{selectedSkill.display_name || selectedSkill.name}</h2>
                        <small>/{selectedSkill.name} · {selectedSkill.source || "registry"} · {selectedSkill.version ? `v${selectedSkill.version}` : "unversioned"}</small>
                      </div>
                    </div>
                    <StatusBadge value={selectedSkill.status || "unknown"} />
                  </div>
                  <p className="admin-description">{selectedSkill.description || "No description available."}</p>
                  <div className="admin-action-row">
                    <button className="primary skill-action" onClick={() => changeSkillStatus("publish")} disabled={Boolean(actionBusy)}>
                      <PlayCircle size={16} />
                      <span>{actionBusy === "publish" ? "Publishing" : "Publish"}</span>
                    </button>
                    <button className="skill-action" onClick={() => changeSkillStatus("unpublish")} disabled={Boolean(actionBusy)}>
                      <Archive size={16} />
                      <span>{actionBusy === "unpublish" ? "Unpublishing" : "Unpublish"}</span>
                    </button>
                    <button className="skill-action danger-outline" onClick={() => changeSkillStatus("disable")} disabled={Boolean(actionBusy)}>
                      <UserX size={16} />
                      <span>{actionBusy === "disable" ? "Disabling" : "Disable"}</span>
                    </button>
                    <button className="skill-action" onClick={() => setPolicyTarget(selectedSkill)}>
                      <ShieldCheck size={16} />
                      <span>Policy</span>
                    </button>
                    <button className="skill-action" onClick={() => loadSkillDetails(selectedSkill.name)} disabled={detailsLoading}>
                      <RefreshCw size={16} />
                      <span>{detailsLoading ? "Loading" : "Review"}</span>
                    </button>
                  </div>
                  <div className="admin-metrics">
                    <AdminMetric label="Runs" value={String(summary?.total ?? 0)} />
                    <AdminMetric label="Failure rate" value={formatPercent(summary?.failure_rate ?? 0)} />
                    <AdminMetric label="Avg latency" value={`${summary?.average_latency_ms ?? 0} ms`} />
                    <AdminMetric label="Versions" value={String(versions.length)} />
                  </div>
                  <AdminTabs tabs={skillTabs} active={skillTab} onChange={setSkillTab} label="Skill detail sections" compact />
                  <div className="admin-detail-grid">
                    {skillTab === "overview" && (
                      <section className="admin-card wide">
                        <div className="admin-card-head">
                          <h3>Registry</h3>
                        </div>
                        <div className="admin-facts">
                          <SkillFact label="Category" value={selectedSkill.category || "General"} />
                          <SkillFact label="Root" value={selectedSkill.skill_root || "Not set"} />
                          <SkillFact label="Hash" value={selectedSkill.content_hash ? selectedSkill.content_hash.slice(0, 12) : "Not set"} />
                          <SkillFact label="Updated" value={formatTime(selectedSkill.updated_at || selectedSkill.created_at || "")} />
                        </div>
                      </section>
                    )}
                    {skillTab === "review" && (
                      <section className="admin-card wide">
                        <div className="admin-card-head">
                          <h3>Review</h3>
                          {review && <StatusBadge value={review.passed ? "passed" : "blocked"} />}
                        </div>
                        {!review && <p className="muted-text">No review loaded.</p>}
                        {review && !reviewIssues.length && <p className="muted-text">No blocking issues or warnings.</p>}
                        {reviewIssues.map((issue) => (
                          <div key={`${issue.code}-${issue.field}`} className={`review-issue ${issue.severity}`}>
                            <strong>{issue.code}</strong>
                            <span>{issue.message}</span>
                            {issue.field && <small>{issue.field}</small>}
                          </div>
                        ))}
                      </section>
                    )}
                    {skillTab === "executions" && (
                      <section className="admin-card wide">
                        <div className="admin-card-head">
                          <h3>Recent executions</h3>
                        </div>
                        <div className="admin-table">
                          {executions.slice(0, 12).map((execution) => (
                            <div key={execution.id} className="admin-table-row">
                              <StatusBadge value={execution.status} />
                              <span>{execution.duration_ms} ms</span>
                              {(execution.provider || execution.model) && <span>{[execution.provider, execution.model].filter(Boolean).join(" / ")}</span>}
                              {execution.error_kind && <span>{execution.error_kind}</span>}
                              {typeof execution.artifact_count === "number" && execution.artifact_count > 0 && <span>{execution.artifact_count} artifact{execution.artifact_count === 1 ? "" : "s"}</span>}
                              <small>{formatTime(execution.completed_at)}</small>
                              {execution.error && <em>{execution.error}</em>}
                              {execution.input_summary && <em>{execution.input_summary}</em>}
                            </div>
                          ))}
                          {!executions.length && <p className="muted-text">No executions recorded.</p>}
                        </div>
                      </section>
                    )}
                    {skillTab === "versions" && (
                      <section className="admin-card wide">
                        <div className="admin-card-head">
                          <h3>Versions</h3>
                        </div>
                        <div className="admin-table">
                          {versions.slice(0, 12).map((version) => (
                            <div key={`${version.version}-${version.content_hash}-${version.created_at}`} className="admin-table-row">
                              <strong>{version.version || "unversioned"}</strong>
                              <span>{version.content_hash ? version.content_hash.slice(0, 10) : "no hash"}</span>
                              <small>{formatTime(version.published_at || version.created_at)}</small>
                              {version.changelog && <em>{version.changelog}</em>}
                            </div>
                          ))}
                          {!versions.length && <p className="muted-text">No versions recorded.</p>}
                        </div>
                      </section>
                    )}
                  </div>
                </>
              )}
            </section>
          </div>
        )}
      </section>
      {policyTarget && (
        <SkillPolicyModal
          api={api}
          skill={policyTarget}
          adminToken={adminToken}
          onAdminTokenChange={onAdminTokenChange}
          onSaved={policySaved}
          onClose={() => setPolicyTarget(null)}
        />
      )}
    </main>
  );
}

function AdminUsersPanel({ api, adminToken }: { api: ApiClient; adminToken: string }) {
  const [users, setUsers] = useState<AdminUser[]>([]);
  const [selectedID, setSelectedID] = useState("");
  const [query, setQuery] = useState("");
  const [statusFilter, setStatusFilter] = useState("all");
  const [loading, setLoading] = useState(false);
  const [actionBusy, setActionBusy] = useState("");
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [userTab, setUserTab] = useState<"account" | "access">("account");
  const token = adminToken.trim();
  const selectedUser = users.find((user) => user.id === selectedID) || null;
  const userTabs: Array<AdminTabOption<typeof userTab>> = [
    { id: "account", label: "Account", icon: <Database size={15} /> },
    { id: "access", label: "Access", icon: <ShieldCheck size={15} /> }
  ];

  const loadUsers = async () => {
    if (!token) return;
    setLoading(true);
    setError("");
    try {
      const next = await api.adminUsers(token, { q: query, status: statusFilter, limit: 100 });
      setUsers(next);
      setSelectedID((current) => {
        if (current && next.some((user) => user.id === current)) return current;
        return next[0]?.id || "";
      });
      setNotice(`Loaded ${next.length} users`);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void loadUsers();
  }, [token]);

  const updateUser = (updated: AdminUser) => {
    setUsers((current) => current.map((user) => user.id === updated.id ? updated : user));
    setSelectedID(updated.id);
  };

  const runAction = async (action: "disable" | "ban" | "reactivate") => {
    if (!selectedUser || !token) return;
    setActionBusy(action);
    setError("");
    try {
      const updated = await api.adminUserAction(selectedUser.id, token, action);
      updateUser(updated);
      setNotice(`${selectedUser.email} ${action} complete`);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setActionBusy("");
    }
  };

  const patchStatus = async (status: "active" | "disabled" | "banned") => {
    if (!selectedUser || !token || selectedUser.status === status) return;
    setActionBusy(status);
    setError("");
    try {
      const updated = await api.updateAdminUserStatus(selectedUser.id, token, status);
      updateUser(updated);
      setNotice(`${selectedUser.email} status set to ${status}`);
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
          <div className="admin-search">
            <Search size={16} />
            <input
              value={query}
              onChange={(event) => setQuery(event.currentTarget.value)}
              onKeyDown={(event) => {
                if (event.key === "Enter") void loadUsers();
              }}
              placeholder="Search users"
              aria-label="Search admin users"
            />
          </div>
          <div className="admin-filter-row">
            <select value={statusFilter} onChange={(event) => setStatusFilter(event.currentTarget.value)} aria-label="Filter user status">
              <option value="all">All status</option>
              <option value="active">Active</option>
              <option value="disabled">Disabled</option>
              <option value="banned">Banned</option>
            </select>
            <button className="skill-action" onClick={loadUsers} disabled={loading}>
              <RefreshCw size={15} />
              <span>{loading ? "Loading" : "Search"}</span>
            </button>
          </div>
        </div>
        <div className="admin-skill-list">
          {users.map((user) => (
            <button
              key={user.id}
              className={`admin-skill-row ${user.id === selectedID ? "active" : ""}`}
              onClick={() => setSelectedID(user.id)}
            >
              <span className="user-avatar">{initials(user.display_name || user.email)}</span>
              <span>
                <strong>{user.display_name || user.email}</strong>
                <small>{user.email}</small>
              </span>
              <StatusBadge value={user.status || "unknown"} />
            </button>
          ))}
          {!users.length && <div className="empty-small">{loading ? "Loading..." : "No users"}</div>}
        </div>
      </section>
      <section className="admin-detail-panel">
        {(error || notice) && (
          <div className={`admin-inline-banner ${error ? "error" : "ok"}`} role="status">
            {error ? <AlertCircle size={16} /> : <ShieldCheck size={16} />}
            <span>{error || notice}</span>
            <button className="icon ghost" onClick={() => { setError(""); setNotice(""); }} title="Dismiss" aria-label="Dismiss">
              <X size={14} />
            </button>
          </div>
        )}
        {!selectedUser ? (
          <div className="admin-empty">
            <Database size={24} />
            <strong>Select a user</strong>
            <p>Choose a user to inspect account status, refresh-token state, and access controls.</p>
          </div>
        ) : (
          <>
            <div className="admin-skill-head">
              <div className="skill-modal-heading">
                <span className="user-avatar large">{initials(selectedUser.display_name || selectedUser.email)}</span>
                <div>
                  <h2>{selectedUser.display_name || selectedUser.email}</h2>
                  <small>{selectedUser.email}</small>
                </div>
              </div>
              <StatusBadge value={selectedUser.status || "unknown"} />
            </div>
            <div className="admin-action-row">
              <button className="primary skill-action" onClick={() => runAction("reactivate")} disabled={Boolean(actionBusy) || selectedUser.status === "active"}>
                <PlayCircle size={16} />
                <span>{actionBusy === "reactivate" ? "Reactivating" : "Reactivate"}</span>
              </button>
              <button className="skill-action" onClick={() => runAction("disable")} disabled={Boolean(actionBusy) || selectedUser.status === "disabled"}>
                <UserX size={16} />
                <span>{actionBusy === "disable" ? "Disabling" : "Disable"}</span>
              </button>
              <button className="skill-action danger-outline" onClick={() => runAction("ban")} disabled={Boolean(actionBusy) || selectedUser.status === "banned"}>
                <UserX size={16} />
                <span>{actionBusy === "ban" ? "Banning" : "Ban"}</span>
              </button>
              <select
                className="admin-status-select"
                value={selectedUser.status}
                onChange={(event) => patchStatus(event.currentTarget.value as "active" | "disabled" | "banned")}
                aria-label="Set user status"
                disabled={Boolean(actionBusy)}
              >
                <option value="active">active</option>
                <option value="disabled">disabled</option>
                <option value="banned">banned</option>
              </select>
            </div>
            <div className="admin-metrics">
              <AdminMetric label="Refresh tokens" value={String(selectedUser.refresh_token_count || 0)} />
              <AdminMetric label="Active tokens" value={String(selectedUser.active_refresh_token_count || 0)} />
              <AdminMetric label="Created" value={formatShortDate(selectedUser.created_at)} />
              <AdminMetric label="Last login" value={formatShortDate(selectedUser.last_login_at)} />
            </div>
            <AdminTabs tabs={userTabs} active={userTab} onChange={setUserTab} label="User detail sections" compact />
            <div className="admin-detail-grid">
              {userTab === "account" && <section className="admin-card wide">
                <div className="admin-card-head">
                  <h3>Account</h3>
                </div>
                <div className="admin-facts">
                  <SkillFact label="User ID" value={selectedUser.id} />
                  <SkillFact label="Email" value={selectedUser.email} />
                  <SkillFact label="Display name" value={selectedUser.display_name || "Not set"} />
                  <SkillFact label="Updated" value={formatTime(selectedUser.updated_at)} />
                </div>
              </section>}
              {userTab === "access" && <section className="admin-card wide">
                <div className="admin-card-head">
                  <h3>Access notes</h3>
                </div>
                <p className="muted-text">Disabled and banned users cannot log in or refresh tokens. Changing a user to an inactive status revokes existing refresh tokens immediately; access tokens expire on their normal short TTL.</p>
              </section>}
            </div>
          </>
        )}
      </section>
    </div>
  );
}

function AdminOpsPanel({ api, adminToken }: { api: ApiClient; adminToken: string }) {
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
            <input value={userID} onChange={(event) => setUserID(event.currentTarget.value)} placeholder="user_id" aria-label="Troubleshooting user ID" />
          </label>
          <div className="admin-search">
            <Search size={16} />
            <input
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
          <button className="primary wide" onClick={() => loadOps()} disabled={loading || !token || !cleanUserID}>
            {loading ? "Loading" : "Load troubleshooting data"}
          </button>
        </div>
        <div className="admin-skill-list">
          {sessions.map((session) => (
            <button key={session.id} className={`admin-skill-row ${session.id === selectedSessionID ? "active" : ""}`} onClick={() => openSession(session.id)}>
              <MessageCircle size={18} />
              <span>
                <strong>{sessionTitle(session)}</strong>
                <small>{session.id}</small>
              </span>
              <small>{(session.messages || []).filter((message) => !message.hidden).length}</small>
            </button>
          ))}
          {!sessions.length && <div className="empty-small">{loading ? "Loading..." : "No sessions"}</div>}
        </div>
      </section>
      <section className="admin-detail-panel">
        {(error || notice) && (
          <div className={`admin-inline-banner ${error ? "error" : "ok"}`} role="status">
            {error ? <AlertCircle size={16} /> : <ShieldCheck size={16} />}
            <span>{error || notice}</span>
            <button className="icon ghost" onClick={() => { setError(""); setNotice(""); }} title="Dismiss" aria-label="Dismiss">
              <X size={14} />
            </button>
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
              <button className="skill-action" onClick={() => loadOps()} disabled={loading}>
                <RefreshCw size={16} />
                <span>{loading ? "Loading" : "Refresh"}</span>
              </button>
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
                    <button key={job.id} className={`admin-table-row button-row ${job.id === selectedJobID ? "active" : ""}`} onClick={() => openJob(job.id)}>
                      <StatusBadge value={job.status} />
                      <span>{job.type || "chat"}</span>
                      <small>{job.id}</small>
                      {job.error && <em>{job.error}</em>}
                    </button>
                  ))}
                  {!jobs.length && <p className="muted-text">No jobs found.</p>}
                </div>
                {selectedJob && (
                  <div className="admin-action-row">
                    <button className="skill-action danger-outline" onClick={cancelJob} disabled={Boolean(actionBusy) || terminalJobs.has(selectedJob.status)}>
                      <Square size={15} />
                      <span>{actionBusy === "cancel" ? "Cancelling" : "Cancel job"}</span>
                    </button>
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

function AdminAuditPanel({ api, adminToken }: { api: ApiClient; adminToken: string }) {
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
            <input value={userID} onChange={(event) => setUserID(event.currentTarget.value)} placeholder="optional user_id" aria-label="Audit user ID filter" />
          </label>
          <div className="admin-search">
            <Search size={16} />
            <input
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
          <button className="primary wide" onClick={loadAudit} disabled={loading || !token}>
            {loading ? "Loading" : "Load audit logs"}
          </button>
        </div>
        <div className="admin-skill-list">
          {records.map((record) => (
            <button key={record.id} className={`admin-skill-row ${record.id === selected?.id ? "active" : ""}`} onClick={() => setSelectedID(record.id)}>
              <FileText size={18} />
              <span>
                <strong>{record.event}</strong>
                <small>{auditRecordSummary(record)}</small>
              </span>
              <StatusBadge value={record.risk_level || "low"} />
            </button>
          ))}
          {!records.length && <div className="empty-small">{loading ? "Loading..." : "No audit events"}</div>}
        </div>
      </section>
      <section className="admin-detail-panel">
        {(error || notice) && (
          <div className={`admin-inline-banner ${error ? "error" : "ok"}`} role="status">
            {error ? <AlertCircle size={16} /> : <ShieldCheck size={16} />}
            <span>{error || notice}</span>
            <button className="icon ghost" onClick={() => { setError(""); setNotice(""); }} title="Dismiss" aria-label="Dismiss">
              <X size={14} />
            </button>
          </div>
        )}
        <div className="admin-skill-head">
          <div>
            <h2>Risk overview</h2>
            <small>{audit?.since ? `Since ${formatTime(audit.since)}` : "No audit window loaded"}</small>
          </div>
          <button className="skill-action" onClick={loadAudit} disabled={loading || !token}>
            <RefreshCw size={16} />
            <span>{loading ? "Loading" : "Refresh"}</span>
          </button>
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
                  <button className="small ghost" disabled={reviewBusy === item.id} onClick={() => updateReview(item.id, "in_review")}>Review</button>
                  <button className="small ghost" disabled={reviewBusy === item.id} onClick={() => updateReview(item.id, "resolved", "resolved by admin")}>Resolve</button>
                  <button className="small danger" disabled={reviewBusy === item.id} onClick={() => updateReview(item.id, "dismissed", "dismissed by admin")}>Dismiss</button>
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
                <button key={group.key} className="admin-table-row button-row" onClick={() => setEventFilter(group.key)}>
                  <StatusBadge value={auditRiskForEventName(group.key)} />
                  <span>{group.key}</span>
                  <small>{group.count} events</small>
                </button>
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
                <button key={`${score.subject_type}:${score.subject_id}`} className="admin-table-row button-row" onClick={() => score.subject_type === "user" ? setUserID(score.subject_id) : undefined}>
                  <StatusBadge value={score.risk_level || "low"} />
                  <span>{score.subject_type}:{score.subject_id}</span>
                  <small>{score.score} score · {score.event_count} events</small>
                </button>
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
                <button key={event.id} className={`admin-table-row button-row ${event.id === selectedRisk?.id ? "active" : ""}`} onClick={() => setSelectedRiskID(event.id)}>
                  <StatusBadge value={event.risk_level || "low"} />
                  <span>{event.operation}</span>
                  <small>{riskEventSummary(event)}</small>
                  {event.reason && <em>{event.reason}</em>}
                </button>
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

function AdminEvaluationPanel({ api, adminToken }: { api: ApiClient; adminToken: string }) {
  const [runs, setRuns] = useState<EvaluationRun[]>([]);
  const [summary, setSummary] = useState<EvaluationRunSummary | null>(null);
  const [results, setResults] = useState<EvaluationResult[]>([]);
  const [reviews, setReviews] = useState<EvaluationReview[]>([]);
  const [selectedRunID, setSelectedRunID] = useState("");
  const [selectedResultID, setSelectedResultID] = useState("");
  const [userID, setUserID] = useState("");
  const [sessionID, setSessionID] = useState("");
  const [jobID, setJobID] = useState("");
  const [skillName, setSkillName] = useState("");
  const [provider, setProvider] = useState("");
  const [model, setModel] = useState("");
  const [subjectType, setSubjectType] = useState("job");
  const [runStatusFilter, setRunStatusFilter] = useState("all");
  const [resultStatusFilter, setResultStatusFilter] = useState("all");
  const [days, setDays] = useState(7);
  const [loading, setLoading] = useState(false);
  const [running, setRunning] = useState(false);
  const [exportBusy, setExportBusy] = useState("");
  const [reviewBusy, setReviewBusy] = useState("");
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [evaluationTab, setEvaluationTab] = useState<"results" | "selected" | "reviews" | "io">("results");
  const token = adminToken.trim();
  const cleanUserID = userID.trim();
  const selectedRun = runs.find((run) => run.id === selectedRunID) || runs[0] || null;
  const selectedResult = results.find((result) => result.id === selectedResultID) || results[0] || null;
  const reviewsByResultID = useMemo(() => {
    const map = new Map<string, EvaluationReview[]>();
    reviews.forEach((review) => {
      const list = map.get(review.result_id) || [];
      list.push(review);
      map.set(review.result_id, list);
    });
    return map;
  }, [reviews]);

  const loadEvaluation = async (runID = selectedRunID) => {
    if (!token) return;
    setLoading(true);
    setError("");
    try {
      const from = new Date(Date.now() - days * 24 * 60 * 60 * 1000).toISOString();
      const [summaryPayload, nextReviews] = await Promise.all([
        api.adminOpsEvaluationSummary(token, { from, status: runStatusFilter, limit: 500 }),
        api.adminOpsEvaluationReviews(token, { status: "all", limit: 500 })
      ]);
      setSummary(summaryPayload.summary);
      setRuns(summaryPayload.runs);
      setReviews(nextReviews);
      const nextRunID = runID && summaryPayload.runs.some((run) => run.id === runID) ? runID : summaryPayload.runs[0]?.id || "";
      setSelectedRunID(nextRunID);
      if (nextRunID) {
        const report = await api.adminOpsEvaluationRun(token, nextRunID, 500);
        const filtered = filterEvaluationResults(report.results, {
          status: resultStatusFilter,
          userID: cleanUserID,
          sessionID: sessionID.trim(),
          jobID: jobID.trim(),
          skillName: skillName.trim(),
          provider: provider.trim(),
          model: model.trim(),
          subjectType
        });
        setResults(filtered);
        setReviews((current) => mergeEvaluationReviews(current, report.reviews));
        setSelectedResultID((current) => {
          if (current && filtered.some((result) => result.id === current)) return current;
          return filtered[0]?.id || "";
        });
      } else {
        setResults([]);
        setSelectedResultID("");
      }
      setNotice(`Loaded ${summaryPayload.runs.length} eval runs`);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setLoading(false);
    }
  };

  const createRun = async () => {
    if (!token || !cleanUserID) {
      setError("Enter a user ID before running evaluation.");
      return;
    }
    setRunning(true);
    setError("");
    try {
      const from = new Date(Date.now() - days * 24 * 60 * 60 * 1000).toISOString();
      const report = await api.createEvaluationRun(token, {
        name: `${subjectType}_quality_${new Date().toISOString().slice(0, 19).replace(/[-:T]/g, "")}`,
        trigger: "admin_ui",
        scope: {
          from,
          subject_type: subjectType,
          user_id: cleanUserID,
          session_id: sessionID.trim(),
          job_id: jobID.trim(),
          skill_name: skillName.trim(),
          provider: provider.trim(),
          model: model.trim()
        }
      });
      setRuns((current) => [report.run, ...current.filter((run) => run.id !== report.run.id)]);
      setSummary(report.summary);
      setResults(report.results);
      setReviews((current) => mergeEvaluationReviews(current, report.reviews));
      setSelectedRunID(report.run.id);
      setSelectedResultID(report.results[0]?.id || "");
      setNotice(`Evaluation completed: ${report.run.passed} passed, ${report.run.failed} failed, ${report.run.warning} warnings`);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setRunning(false);
    }
  };

  const openRun = async (runID: string) => {
    setSelectedRunID(runID);
    if (!token) return;
    setLoading(true);
    setError("");
    try {
      const report = await api.adminOpsEvaluationRun(token, runID, 500);
      const filtered = filterEvaluationResults(report.results, {
        status: resultStatusFilter,
        userID: cleanUserID,
        sessionID: sessionID.trim(),
        jobID: jobID.trim(),
        skillName: skillName.trim(),
        provider: provider.trim(),
        model: model.trim(),
        subjectType
      });
      setResults(filtered);
      setReviews((current) => mergeEvaluationReviews(current, report.reviews));
      setSelectedResultID(filtered[0]?.id || "");
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setLoading(false);
    }
  };

  const updateReview = async (review: EvaluationReview, status: string) => {
    if (!token) return;
    setReviewBusy(review.id);
    setError("");
    try {
      const updated = await api.updateEvaluationReview(token, review.id, {
        status,
        reviewer: "admin",
        note: status === "ignored" ? "ignored from Admin UI" : "reviewed from Admin UI"
      });
      setReviews((current) => mergeEvaluationReviews(current, [updated]));
      setNotice(`Review ${updated.id} marked ${updated.status}`);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setReviewBusy("");
    }
  };

  const exportResultsCSV = async () => {
    if (!token) return;
    setExportBusy("csv");
    setError("");
    try {
      const content = await api.adminOpsEvaluationResultsCSV(token, {
        runId: selectedRunID || selectedRun?.id,
        status: resultStatusFilter,
        userId: cleanUserID,
        sessionId: sessionID.trim(),
        jobId: jobID.trim(),
        skillName: skillName.trim(),
        provider: provider.trim(),
        model: model.trim(),
        subjectType,
        limit: 1000
      });
      downloadTextFile(`evaluation-results-${selectedRunID || "filtered"}.csv`, content, "text/csv;charset=utf-8");
      setNotice("Evaluation results CSV exported");
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setExportBusy("");
    }
  };

  const exportSummaryMarkdown = async () => {
    if (!token) return;
    setExportBusy("markdown");
    setError("");
    try {
      const from = new Date(Date.now() - days * 24 * 60 * 60 * 1000).toISOString();
      const content = await api.adminOpsEvaluationSummaryMarkdown(token, { from, status: runStatusFilter, limit: 500 });
      downloadTextFile("evaluation-summary.md", content, "text/markdown;charset=utf-8");
      setNotice("Evaluation summary Markdown exported");
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setExportBusy("");
    }
  };

  useEffect(() => {
    if (token) void loadEvaluation();
  }, [token]);

  const selectedResultReviews = selectedResult ? reviewsByResultID.get(selectedResult.id) || [] : [];
  const metrics = selectedRun?.metrics || summary?.metrics || {};
  const passRate = selectedRun ? selectedRunPassRate(selectedRun) : summary?.pass_rate ?? 0;
  const totalResults = selectedRun?.total ?? summary?.total ?? 0;
  const failedResults = selectedRun?.failed ?? summary?.failed ?? 0;
  const warningResults = selectedRun?.warning ?? summary?.warning ?? 0;
  const p95LatencyMS = metricNumber(metrics, "p95_latency_ms");
  const averageLatencyMS = metricNumber(metrics, "average_latency_ms");
  const chatLLMP95MS = metricNumber(metrics, "chat_llm_full_p95_ms");
  const firstTokenP95MS = metricNumber(metrics, "first_token_p95_ms");
  const jobEndToEndP95MS = metricNumber(metrics, "job_end_to_end_p95_ms");
  const skillExecutionP95MS = metricNumber(metrics, "skill_execution_p95_ms");
  const sandboxStartupP95MS = metricNumber(metrics, "sandbox_startup_p95_ms");
  const artifactGenerationP95MS = metricNumber(metrics, "artifact_generation_p95_ms");
  const totalTokens = metricNumber(metrics, "total_tokens");
  const estimatedCostUSD = metricNumber(metrics, "estimated_cost_usd");
  const toolErrorRate = metricNumber(metrics, "tool_error_rate");
  const llmErrorRate = metricNumber(metrics, "llm_error_rate");
  const evaluationTabs: Array<AdminTabOption<typeof evaluationTab>> = [
    { id: "results", label: "Results", icon: <Activity size={15} />, count: results.length },
    { id: "selected", label: "Selected", icon: <Info size={15} /> },
    { id: "reviews", label: "Reviews", icon: <ShieldCheck size={15} />, count: selectedResultReviews.length },
    { id: "io", label: "I/O", icon: <FileText size={15} /> }
  ];

  return (
    <div className="admin-skill-layout">
      <section className="admin-list-panel evaluation-list-panel">
        <div className="admin-list-tools">
          <label className="admin-field">
            <span>User ID</span>
            <input value={userID} onChange={(event) => setUserID(event.currentTarget.value)} placeholder="required for new eval" aria-label="Evaluation user ID" />
          </label>
          <div className="admin-filter-row">
            <select value={subjectType} onChange={(event) => setSubjectType(event.currentTarget.value)} aria-label="Evaluation subject">
              <option value="job">Jobs</option>
              <option value="session">Sessions</option>
              <option value="skill_execution">Skill executions</option>
            </select>
            <select value={String(days)} onChange={(event) => setDays(Number(event.currentTarget.value))} aria-label="Evaluation time window">
              <option value="1">Last 24h</option>
              <option value="7">Last 7d</option>
              <option value="30">Last 30d</option>
              <option value="90">Last 90d</option>
            </select>
          </div>
          <div className="admin-filter-row">
            <select value={runStatusFilter} onChange={(event) => setRunStatusFilter(event.currentTarget.value)} aria-label="Evaluation run status">
              <option value="all">All runs</option>
              <option value="completed">Completed</option>
              <option value="failed">Failed</option>
              <option value="running">Running</option>
            </select>
            <select value={resultStatusFilter} onChange={(event) => setResultStatusFilter(event.currentTarget.value)} aria-label="Evaluation result status">
              <option value="all">All results</option>
              <option value="failed">Failed</option>
              <option value="warning">Warning</option>
              <option value="passed">Passed</option>
            </select>
          </div>
          <div className="admin-filter-row">
            <label className="admin-field">
              <span>Session ID</span>
              <input value={sessionID} onChange={(event) => setSessionID(event.currentTarget.value)} placeholder="optional" aria-label="Evaluation session ID" />
            </label>
            <label className="admin-field">
              <span>Job ID</span>
              <input value={jobID} onChange={(event) => setJobID(event.currentTarget.value)} placeholder="optional" aria-label="Evaluation job ID" />
            </label>
          </div>
          <label className="admin-field">
            <span>Skill / model</span>
            <input value={skillName} onChange={(event) => setSkillName(event.currentTarget.value)} placeholder="skill name" aria-label="Evaluation skill name" />
          </label>
          <div className="admin-filter-row">
            <input value={provider} onChange={(event) => setProvider(event.currentTarget.value)} placeholder="provider" aria-label="Evaluation provider" />
            <input value={model} onChange={(event) => setModel(event.currentTarget.value)} placeholder="model" aria-label="Evaluation model" />
          </div>
          <div className="admin-action-row compact evaluation-actions">
            <button className="primary skill-action" onClick={createRun} disabled={running || !token || !cleanUserID}>
              <PlayCircle size={16} />
              <span>{running ? "Running" : "Run eval"}</span>
            </button>
            <button className="skill-action" onClick={() => loadEvaluation()} disabled={loading || !token}>
              <RefreshCw size={16} />
              <span>{loading ? "Loading" : "Load"}</span>
            </button>
            <button className="skill-action" onClick={exportResultsCSV} disabled={exportBusy === "csv" || !token}>
              <Download size={16} />
              <span>{exportBusy === "csv" ? "Exporting" : "CSV"}</span>
            </button>
            <button className="skill-action" onClick={exportSummaryMarkdown} disabled={exportBusy === "markdown" || !token}>
              <FileText size={16} />
              <span>{exportBusy === "markdown" ? "Exporting" : "Report"}</span>
            </button>
          </div>
        </div>
        <div className="admin-skill-list">
          {runs.map((run) => (
            <button key={run.id} className={`admin-skill-row ${run.id === selectedRun?.id ? "active" : ""}`} onClick={() => openRun(run.id)}>
              <Activity size={18} />
              <span>
                <strong>{run.name}</strong>
                <small>{run.id} · {formatTime(run.completed_at || run.started_at)}</small>
              </span>
              <StatusBadge value={run.status} />
            </button>
          ))}
          {!runs.length && <div className="empty-small">{loading ? "Loading..." : "No eval runs"}</div>}
        </div>
      </section>
      <section className="admin-detail-panel">
        {(error || notice) && (
          <div className={`admin-inline-banner ${error ? "error" : "ok"}`} role="status">
            {error ? <AlertCircle size={16} /> : <ShieldCheck size={16} />}
            <span>{error || notice}</span>
            <button className="icon ghost" onClick={() => { setError(""); setNotice(""); }} title="Dismiss" aria-label="Dismiss">
              <X size={14} />
            </button>
          </div>
        )}
        <div className="admin-skill-head">
          <div>
            <h2>{selectedRun?.name || "Evaluation overview"}</h2>
            <small>{selectedRun ? `${selectedRun.id} · ${selectedRun.scope?.user_id || "user scope"}` : "No run selected"}</small>
          </div>
          {selectedRun && <StatusBadge value={selectedRun.status} />}
        </div>
        <div className="admin-metrics evaluation-metrics">
          <AdminMetric label="Task success" value={formatPercent(passRate)} />
          <AdminMetric label="Results" value={String(totalResults)} />
          <AdminMetric label="Failed / warning" value={`${failedResults} / ${warningResults}`} />
          <AdminMetric label="P95 latency" value={`${formatNumber(Math.round(p95LatencyMS))} ms`} />
          <AdminMetric label="Avg latency" value={`${formatNumber(Math.round(averageLatencyMS))} ms`} />
          <AdminMetric label="TTFT P95" value={`${formatLatencyMetric(firstTokenP95MS)}`} />
          <AdminMetric label="Chat LLM P95" value={`${formatLatencyMetric(chatLLMP95MS)}`} />
          <AdminMetric label="Job P95" value={`${formatLatencyMetric(jobEndToEndP95MS)}`} />
          <AdminMetric label="Skill P95" value={`${formatLatencyMetric(skillExecutionP95MS)}`} />
          <AdminMetric label="Sandbox start P95" value={`${formatLatencyMetric(sandboxStartupP95MS)}`} />
          <AdminMetric label="Artifact P95" value={`${formatLatencyMetric(artifactGenerationP95MS)}`} />
          <AdminMetric label="Token cost" value={formatUSD(estimatedCostUSD)} />
          <AdminMetric label="Tokens" value={formatNumber(totalTokens)} />
          <AdminMetric label="Tool fail rate" value={formatPercent(toolErrorRate)} />
          <AdminMetric label="LLM fail rate" value={formatPercent(llmErrorRate)} />
          <AdminMetric label="Pending reviews" value={String(reviews.filter((review) => review.status === "pending").length)} />
        </div>
        <AdminTabs tabs={evaluationTabs} active={evaluationTab} onChange={setEvaluationTab} label="Evaluation detail sections" compact />
        <div className="admin-detail-grid">
          {evaluationTab === "results" && <section className="admin-card wide">
            <div className="admin-card-head">
              <h3>Results</h3>
              <small>{results.length} shown</small>
            </div>
            <div className="admin-table">
              {results.slice(0, 24).map((result) => (
                <button key={result.id} className={`admin-table-row button-row ${result.id === selectedResult?.id ? "active" : ""}`} onClick={() => setSelectedResultID(result.id)}>
                  <StatusBadge value={result.status} />
                  <span>
                    <strong>{result.subject_type}:{result.subject_id}</strong>
                    <small>{[result.user_id, result.session_id, result.job_id, result.skill_name].filter(Boolean).join(" · ") || "runtime record"}</small>
                  </span>
                  <small>{formatNumber(Math.round((result.score || 0) * 100))}</small>
                  {(result.findings || []).slice(0, 2).map((finding) => <em key={`${result.id}-${finding.code}`}>{finding.code}: {finding.message}</em>)}
                </button>
              ))}
              {!results.length && <p className="muted-text">No results in this filter.</p>}
            </div>
          </section>}
          {evaluationTab === "selected" && <section className="admin-card">
            <div className="admin-card-head">
              <h3>Selected result</h3>
              {selectedResult && <StatusBadge value={selectedResult.status} />}
            </div>
            {selectedResult ? (
              <div className="admin-facts">
                <SkillFact label="Subject" value={`${selectedResult.subject_type}:${selectedResult.subject_id}`} />
                <SkillFact label="Score" value={String(selectedResult.score)} />
                <SkillFact label="Provider" value={[selectedResult.provider, selectedResult.model].filter(Boolean).join(" / ") || "none"} />
                <SkillFact label="Created" value={formatTime(selectedResult.created_at)} />
                <SkillFact label="Session" value={selectedResult.session_id || "none"} />
                <SkillFact label="Job" value={selectedResult.job_id || "none"} />
              </div>
            ) : (
              <p className="muted-text">Select a result to inspect findings.</p>
            )}
          </section>}
          {evaluationTab === "selected" && <section className="admin-card">
            <div className="admin-card-head">
              <h3>Findings</h3>
            </div>
            <div className="admin-table">
              {(selectedResult?.findings || []).map((finding) => (
                <div key={`${finding.code}-${finding.message}`} className={`review-issue ${finding.severity}`}>
                  <strong>{finding.code}</strong>
                  <span>{finding.message}</span>
                </div>
              ))}
              {selectedResult && !selectedResult.findings?.length && <p className="muted-text">No findings for this result.</p>}
            </div>
          </section>}
          {evaluationTab === "reviews" && <section className="admin-card wide">
            <div className="admin-card-head">
              <h3>Review items</h3>
            </div>
            <div className="admin-table">
              {selectedResultReviews.map((review) => (
                <div key={review.id} className="admin-table-row">
                  <StatusBadge value={review.status} />
                  <span>
                    <strong>{review.id}</strong>
                    <small>{review.note || "No note"}</small>
                  </span>
                  <small>{formatTime(review.updated_at)}</small>
                  <button className="small ghost" disabled={reviewBusy === review.id} onClick={() => updateReview(review, "passed")}>Pass</button>
                  <button className="small danger" disabled={reviewBusy === review.id} onClick={() => updateReview(review, "ignored")}>Ignore</button>
                </div>
              ))}
              {!selectedResultReviews.length && <p className="muted-text">No review items for the selected result.</p>}
            </div>
          </section>}
          {evaluationTab === "io" && <section className="admin-card wide">
            <div className="admin-card-head">
              <h3>Input / output</h3>
            </div>
            <pre className="admin-code-block">{selectedResult ? JSON.stringify({
              input: selectedResult.input || "",
              output: selectedResult.output || "",
              metrics: selectedResult.metrics || {}
            }, null, 2) : "{}"}</pre>
          </section>}
        </div>
      </section>
    </div>
  );
}

function AdminHealthCostPanel({ api, adminToken }: { api: ApiClient; adminToken: string }) {
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
  const [healthTab, setHealthTab] = useState<"runtime" | "governance" | "usage" | "quota">("runtime");
  const token = adminToken.trim();
  const cleanUserID = userID.trim();
  const readiness = health?.readiness;
  const llm = health?.llm;
  const healthyBackends = (llm?.backends || []).filter((backend) => backend.healthy).length;
  const healthTabs: Array<AdminTabOption<typeof healthTab>> = [
    { id: "runtime", label: "Runtime", icon: <Activity size={15} />, count: readiness?.checks?.length ?? 0 },
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
            <input value={userID} onChange={(event) => setUserID(event.currentTarget.value)} placeholder="optional user_id" aria-label="LLM usage user filter" />
          </label>
          <div className="admin-filter-row">
            <select value={String(days)} onChange={(event) => setDays(Number(event.currentTarget.value))} aria-label="Usage time range">
              <option value="1">Last 24h</option>
              <option value="7">Last 7d</option>
              <option value="30">Last 30d</option>
              <option value="90">Last 90d</option>
            </select>
            <button className="skill-action" onClick={loadHealthCost} disabled={loading || !token}>
              <RefreshCw size={15} />
              <span>{loading ? "Loading" : "Refresh"}</span>
            </button>
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
            <button className="icon ghost" onClick={() => { setError(""); setNotice(""); }} title="Dismiss" aria-label="Dismiss">
              <X size={14} />
            </button>
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
          {healthTab === "governance" && <section className="admin-card wide">
            <div className="admin-card-head">
              <h3>Governance config</h3>
              <button className="skill-action" onClick={saveLLMConfig} disabled={configBusy || !token}>
                <Settings size={15} />
                <span>{configBusy ? "Saving" : "Save"}</span>
              </button>
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
                <input value={modelOptionLocation(llm?.config, configDraft.model) || configDraft.vertex_location || ""} readOnly aria-label="Selected model Vertex location" />
              </label>
              <label className="admin-field">
                <span>Daily token quota</span>
                <input inputMode="numeric" value={configDraft.daily_token_quota || ""} onChange={(event) => updateConfigDraft("daily_token_quota", event.currentTarget.value)} placeholder="0 disables" />
              </label>
              <label className="admin-field">
                <span>Daily request quota</span>
                <input inputMode="numeric" value={configDraft.daily_request_quota || ""} onChange={(event) => updateConfigDraft("daily_request_quota", event.currentTarget.value)} placeholder="0 disables" />
              </label>
              <label className="admin-field">
                <span>Daily cost quota USD</span>
                <input inputMode="decimal" value={configDraft.daily_cost_quota_usd || ""} onChange={(event) => updateConfigDraft("daily_cost_quota_usd", event.currentTarget.value)} placeholder="0 disables" />
              </label>
              <label className="admin-field">
                <span>Max attempts</span>
                <input inputMode="numeric" value={configDraft.max_attempts || ""} onChange={(event) => updateConfigDraft("max_attempts", event.currentTarget.value)} placeholder="1" />
              </label>
              <label className="admin-field">
                <span>Chat timeout ms</span>
                <input inputMode="numeric" value={configDraft.chat_timeout_ms || ""} onChange={(event) => updateConfigDraft("chat_timeout_ms", event.currentTarget.value)} placeholder="60000" />
              </label>
              <label className="admin-field">
                <span>Skill timeout ms</span>
                <input inputMode="numeric" value={configDraft.skill_timeout_ms || ""} onChange={(event) => updateConfigDraft("skill_timeout_ms", event.currentTarget.value)} placeholder="90000" />
              </label>
              <label className="admin-field">
                <span>Input cost / 1M</span>
                <input inputMode="decimal" value={configDraft.input_cost_per_million || ""} onChange={(event) => updateConfigDraft("input_cost_per_million", event.currentTarget.value)} placeholder="0.30" />
              </label>
              <label className="admin-field">
                <span>Output cost / 1M</span>
                <input inputMode="decimal" value={configDraft.output_cost_per_million || ""} onChange={(event) => updateConfigDraft("output_cost_per_million", event.currentTarget.value)} placeholder="2.50" />
              </label>
              <label className="admin-field">
                <span>Retry backoff ms</span>
                <input inputMode="numeric" value={configDraft.retry_backoff_ms || ""} onChange={(event) => updateConfigDraft("retry_backoff_ms", event.currentTarget.value)} placeholder="300" />
              </label>
              <label className="admin-field">
                <span>Failure threshold</span>
                <input inputMode="numeric" value={configDraft.failure_threshold || ""} onChange={(event) => updateConfigDraft("failure_threshold", event.currentTarget.value)} placeholder="3" />
              </label>
              <label className="admin-field">
                <span>Circuit cooldown sec</span>
                <input inputMode="numeric" value={configDraft.circuit_cooldown_seconds || ""} onChange={(event) => updateConfigDraft("circuit_cooldown_seconds", event.currentTarget.value)} placeholder="60" />
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
                <input inputMode="numeric" value={refundRequests} onChange={(event) => setRefundRequests(event.currentTarget.value)} placeholder="0" />
              </label>
              <label className="admin-field">
                <span>Refund tokens</span>
                <input inputMode="numeric" value={refundTokens} onChange={(event) => setRefundTokens(event.currentTarget.value)} placeholder="0" />
              </label>
              <label className="admin-field">
                <span>Refund cost USD</span>
                <input inputMode="decimal" value={refundCost} onChange={(event) => setRefundCost(event.currentTarget.value)} placeholder="0.00" />
              </label>
              <label className="admin-field">
                <span>Reason</span>
                <input value={quotaReason} onChange={(event) => setQuotaReason(event.currentTarget.value)} placeholder="support note" />
              </label>
            </div>
            <div className="admin-action-row">
              <button className="skill-action" onClick={refundQuota} disabled={!cleanUserID || Boolean(quotaBusy)}>
                <Download size={15} />
                <span>{quotaBusy === "refund" ? "Applying" : "Apply refund"}</span>
              </button>
              <button className="skill-action danger-outline" onClick={resetQuota} disabled={!cleanUserID || Boolean(quotaBusy)}>
                <RefreshCw size={15} />
                <span>{quotaBusy === "reset" ? "Resetting" : "Reset daily quota"}</span>
              </button>
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

function StatusBadge({ value }: { value: string }) {
  const normalized = value.toLowerCase().replace(/[^a-z0-9_-]+/g, "-");
  return <span className={`status-badge ${normalized}`}>{value}</span>;
}

function llmConfigDraftFromConfig(config: LLMGovernanceConfig): Record<string, string> {
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

function llmConfigFromDraft(draft: Record<string, string>): LLMGovernanceConfig {
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

function modelOptionLocation(config: LLMGovernanceConfig | undefined, model: string | undefined): string {
  const selected = String(model || "").trim();
  return config?.allowed_models?.find((option) => option.id === selected)?.vertex_location || "";
}

function AdminMetric({ label, value }: { label: string; value: string }) {
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

function SkillPolicyModal({
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
          <button className="icon ghost" onClick={onClose} aria-label="Close skill policy" title="Close">
            <X size={18} />
          </button>
        </header>
        <div className="skill-policy-body">
          <label className="policy-field policy-token">
            <span>Admin token</span>
            <input
              type="password"
              value={adminToken}
              onChange={(event) => onAdminTokenChange(event.currentTarget.value)}
              placeholder="AGENT_API_ADMIN_TOKEN"
              autoComplete="off"
            />
            <button type="button" className="skill-action" onClick={loadPolicy} disabled={loading}>
              {loading ? <RefreshCw size={15} /> : <ShieldCheck size={15} />}
              <span>{loading ? "Loading" : "Load"}</span>
            </button>
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
              <input value={draft.shellTimeout} onChange={(event) => updateDraft("shellTimeout", event.currentTarget.value)} placeholder="90s, 2m" />
            </label>
          </section>
          <section className="policy-section">
            <h3>Sandbox</h3>
            <div className="policy-grid">
              <label className="policy-field">
                <span>Runner</span>
                <input value={draft.sandboxRunner} onChange={(event) => updateDraft("sandboxRunner", event.currentTarget.value)} placeholder="docker" />
              </label>
              <label className="policy-field">
                <span>Image</span>
                <input value={draft.sandboxImage} onChange={(event) => updateDraft("sandboxImage", event.currentTarget.value)} placeholder="python:3.12-slim" />
              </label>
              <label className="policy-field">
                <span>Network</span>
                <input value={draft.sandboxNetwork} onChange={(event) => updateDraft("sandboxNetwork", event.currentTarget.value)} placeholder="none, bridge" />
              </label>
              <label className="policy-field">
                <span>Memory</span>
                <input value={draft.sandboxMemory} onChange={(event) => updateDraft("sandboxMemory", event.currentTarget.value)} placeholder="512m" />
              </label>
              <label className="policy-field">
                <span>CPUs</span>
                <input value={draft.sandboxCpus} onChange={(event) => updateDraft("sandboxCpus", event.currentTarget.value)} placeholder="1" />
              </label>
              <label className="policy-field">
                <span>Pids limit</span>
                <input inputMode="numeric" value={draft.sandboxPidsLimit} onChange={(event) => updateDraft("sandboxPidsLimit", event.currentTarget.value)} placeholder="128" />
              </label>
              <label className="policy-field">
                <span>Tmpfs size</span>
                <input value={draft.sandboxTmpfsSize} onChange={(event) => updateDraft("sandboxTmpfsSize", event.currentTarget.value)} placeholder="64m" />
              </label>
              <label className="policy-field">
                <span>Max output bytes</span>
                <input inputMode="numeric" value={draft.sandboxMaxOutputBytes} onChange={(event) => updateDraft("sandboxMaxOutputBytes", event.currentTarget.value)} placeholder="1048576" />
              </label>
            </div>
          </section>
        </div>
        <footer>
          <button className="skill-action" onClick={onClose}>Cancel</button>
          <button className="primary skill-modal-insert" onClick={savePolicy} disabled={saving || loading || !loadedSkill}>
            <ShieldCheck size={16} />
            <span>{saving ? "Saving" : "Save policy"}</span>
          </button>
        </footer>
      </section>
    </div>
  );
}

function PolicyTextArea({ label, value, onChange, placeholder }: { label: string; value: string; onChange: (value: string) => void; placeholder?: string }) {
  return (
    <label className="policy-field">
      <span>{label}</span>
      <textarea value={value} onChange={(event) => onChange(event.currentTarget.value)} placeholder={placeholder} rows={3} />
    </label>
  );
}

function skillPolicyFromMetadata(metadata?: Record<string, unknown>): Record<string, unknown> {
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

function policyDraftFromConfig(policy: Record<string, unknown>): SkillPolicyDraft {
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

function skillPolicyConfigFromDraft(base: Record<string, unknown>, draft: SkillPolicyDraft): SkillPolicyConfig {
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

function setPolicyList(target: Record<string, unknown>, key: string, value: string) {
  const list = splitPolicyList(value);
  if (list.length) target[key] = list;
  else delete target[key];
}

function setPolicyString(target: Record<string, unknown>, key: string, value: string) {
  const cleaned = value.trim();
  if (cleaned) target[key] = cleaned;
  else delete target[key];
}

function setPolicyNumber(target: Record<string, unknown>, key: string, value: string) {
  const cleaned = value.trim();
  if (!cleaned) {
    delete target[key];
    return;
  }
  const parsed = Number(cleaned);
  if (Number.isFinite(parsed) && parsed > 0) target[key] = Math.floor(parsed);
}

function splitPolicyList(value: string): string[] {
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

function joinPolicyList(value: unknown): string {
  if (Array.isArray(value)) return value.map((item) => stringPolicyValue(item)).filter(Boolean).join("\n");
  return stringPolicyValue(value);
}

function stringPolicyValue(value: unknown): string {
  if (typeof value === "string") return value;
  if (typeof value === "number" || typeof value === "boolean") return String(value);
  return "";
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === "object" && !Array.isArray(value);
}



function SkillFact({ label, value }: { label: string; value: string }) {
  return (
    <div className="skill-fact">
      <small>{label}</small>
      <strong>{value}</strong>
    </div>
  );
}



function SkillGlyph({ skill }: { skill: Skill }) {
  if (skill.icon) return <span className="skill-glyph text">{skill.icon}</span>;
  if (skill.produces_artifacts) return <span className="skill-glyph"><Archive size={17} /></span>;
  if (skill.run_as_job) return <span className="skill-glyph"><Clock size={17} /></span>;
  return <span className="skill-glyph"><Sparkles size={17} /></span>;
}



function errorMessage(error: unknown): string {
  return error instanceof ApiError && error.requestId
    ? `${error.message} (${error.requestId})`
    : error instanceof Error
      ? error.message
      : String(error);
}


function compareSkills(a: Skill, b: Skill): number {
  if (Boolean(a.featured) !== Boolean(b.featured)) return a.featured ? -1 : 1;
  const orderA = a.sort_order ?? Number.MAX_SAFE_INTEGER;
  const orderB = b.sort_order ?? Number.MAX_SAFE_INTEGER;
  if (orderA !== orderB) return orderA - orderB;
  return (a.display_name || a.name).localeCompare(b.display_name || b.name);
}


function formatPercent(value: number): string {
  if (!Number.isFinite(value)) return "0%";
  const percent = value > 1 ? value : value * 100;
  return `${percent.toFixed(percent >= 10 ? 0 : 1)}%`;
}


function formatNumber(value: number): string {
  if (!Number.isFinite(value)) return "0";
  return new Intl.NumberFormat().format(value);
}


function formatUSD(value: number): string {
  if (!Number.isFinite(value)) return "$0.00";
  return new Intl.NumberFormat(undefined, { style: "currency", currency: "USD", maximumFractionDigits: value < 1 ? 4 : 2 }).format(value);
}

function formatLatencyMetric(value: number): string {
  if (!Number.isFinite(value) || value <= 0) return "-";
  return `${formatNumber(Math.round(value))} ms`;
}

function metricNumber(metrics: Record<string, unknown> | undefined, key: string): number {
  const value = metrics?.[key];
  if (typeof value === "number" && Number.isFinite(value)) return value;
  if (typeof value === "string") {
    const parsed = Number(value);
    return Number.isFinite(parsed) ? parsed : 0;
  }
  return 0;
}


function selectedRunPassRate(run: EvaluationRun | null): number {
  if (!run || !run.total) return 0;
  return run.passed / run.total;
}


function downloadTextFile(filename: string, content: string, type: string): void {
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


function mergeEvaluationReviews(current: EvaluationReview[], next: EvaluationReview[]): EvaluationReview[] {
  const byID = new Map<string, EvaluationReview>();
  current.forEach((review) => byID.set(review.id, review));
  next.forEach((review) => byID.set(review.id, review));
  return Array.from(byID.values()).sort((a, b) => String(b.updated_at || b.created_at).localeCompare(String(a.updated_at || a.created_at)));
}


function filterEvaluationResults(results: EvaluationResult[], filter: { status: string; userID: string; sessionID: string; jobID: string; skillName: string; provider: string; model: string; subjectType: string }): EvaluationResult[] {
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


function auditRecordSummary(record: AuditLogRecord): string {
  const target = record.session_id || record.job_id || record.asset_id || record.request_id || record.id;
  return `${record.user_id || "system"} · ${formatTime(record.created_at)} · ${target}`;
}


function riskEventSummary(event: RiskEvent): string {
  const actor = event.user_id || event.ip_address || "anonymous";
  return `${actor} · ${formatTime(event.created_at)} · +${event.score_delta}`;
}


function formatAuditMetadata(record: AuditLogRecord): string {
  const payload = {
    metadata: record.metadata || {},
    user_agent: record.user_agent || "",
    request_id: record.request_id || ""
  };
  return JSON.stringify(payload, null, 2);
}


function auditRiskForEventName(event: string): string {
  const normalized = event.toLowerCase();
  if (["account_delete", "memory_delete_user", "data_export", "user_ban", "user_disable", "skill_disable", "skill_policy_update", "admin_job_cancel"].includes(normalized)) return "high";
  if (normalized.includes("delete") || normalized.includes("disable") || normalized.includes("ban") || normalized.includes("policy")) return "high";
  if (normalized.includes("cancel") || normalized.includes("publish") || normalized.includes("unpublish") || normalized.includes("update") || normalized.includes("memory_")) return "medium";
  return "low";
}


function formatShortDate(value?: string): string {
  if (!value) return "Never";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "Unknown";
  return date.toLocaleDateString();
}


function initials(value: string): string {
  const parts = value.trim().split(/[\s@._-]+/).filter(Boolean);
  const letters = parts.slice(0, 2).map((part) => part[0]?.toUpperCase()).join("");
  return letters || "U";
}


function fuzzyMatch(query: string, fields: Array<string | number | undefined | null>): boolean {
  const normalizedQuery = normalizeSearch(query);
  if (!normalizedQuery) return true;
  const rawHaystack = fields.filter((field) => field !== undefined && field !== null).join(" ");
  const haystack = normalizeSearch(rawHaystack);
  if (!haystack) return false;
  if (haystack.includes(normalizedQuery)) return true;
  return acronymSearch(rawHaystack).includes(normalizedQuery);
}


function normalizeSearch(value: string | number | undefined | null): string {
  return String(value || "").toLowerCase().replace(/[\s_\-./]+/g, "");
}


function acronymSearch(value: string): string {
  return value
    .toLowerCase()
    .split(/[^a-z0-9]+/i)
    .filter(Boolean)
    .map((word) => word[0])
    .join("");
}


function useFocusTrap<T extends HTMLElement>(active: boolean, onEscape: () => void) {
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

function focusableElements(container: HTMLElement): HTMLElement[] {
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


function formatBytes(bytes: number): string {
  if (!bytes) return "0 KB";
  if (bytes < 1024 * 1024) return `${Math.ceil(bytes / 1024)} KB`;
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
}


function formatTime(value?: string): string {
  if (!value) return "";
  return new Intl.DateTimeFormat(undefined, { month: "short", day: "2-digit", hour: "2-digit", minute: "2-digit" }).format(new Date(value));
}


export default AdminConsole;
