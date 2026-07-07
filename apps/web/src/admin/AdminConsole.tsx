import { useEffect, useMemo, useState } from "react";
import {
  Activity,
  Archive,
  Briefcase,
  Database,
  FileText,
  Info,
  LogOut,
  MessageCircle,
  PlayCircle,
  ScrollText,
  RefreshCw,
  Search,
  ShieldCheck,
  Sparkles,
  UserX,
} from "lucide-react";
import { ApiClient } from "../api/client";
import { Button } from "../components/ui/button";
import { Input } from "../components/ui/input";
import { BrandLogo } from "../components/brand/BrandLogo";
import {
  AdminDetailPanel,
  AdminEmptyState,
  AdminListPanel,
  AdminPageHeader,
  AdminSearchBox,
  AdminSectionNotice,
  AdminShell,
  AdminSidebar,
  AdminSplitPane
} from "./ui";
import {
  AdminTabs,
  type AdminTabOption,
  AdminMetric,
  StatusBadge,
  SkillPolicyModal,
  SkillFact,
  SkillGlyph,
  compareSkills,
  errorMessage,
  formatPercent,
  formatTime,
  fuzzyMatch,
} from "./shared";
import type {
  AdminSkill,
  Skill,
  SkillExecution,
  SkillExecutionSummary,
  SkillReviewResult,
  SkillVersion
} from "../types";
import { AdminUsersPanel } from "./panels/AdminUsersPanel";
import { AdminOpsPanel } from "./panels/AdminOpsPanel";
import { AdminAuditPanel } from "./panels/AdminAuditPanel";
import { AdminEvaluationPanel } from "./panels/AdminEvaluationPanel";
import { AdminHealthCostPanel } from "./panels/AdminHealthCostPanel";
import { AdminPromptPanel } from "./panels/AdminPromptPanel";

type AdminSection = "skills" | "users" | "jobs-assets" | "health-cost" | "audit" | "evaluation" | "prompts";

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
    { id: "evaluation", label: "Evaluation", description: "Run lightweight evaluations over real runtime data, inspect pass/fail findings, and close review items.", icon: <ShieldCheck size={18} /> },
    { id: "prompts", label: "Prompts", description: "Create candidate prompt versions, preview, evaluate, promote, rollback, and inspect prompt usage.", icon: <ScrollText size={18} /> }
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

  const sidebar = (
      <AdminSidebar>
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
            <Input
              type="password"
              value={adminToken}
              onChange={(event) => onAdminTokenChange(event.currentTarget.value)}
              placeholder="AGENT_API_ADMIN_TOKEN"
              autoComplete="off"
            />
          </label>
          <Button className="wide" variant="primary" onClick={loadSkills} disabled={loading || !token || adminSection !== "skills"}>
            {loading ? "Loading" : "Load skill data"}
          </Button>
        </div>
        <div className="admin-sidebar-actions">
          <Button variant="outline" onClick={onExit}><MessageCircle size={16} /> Back to app</Button>
          <Button variant="outline" onClick={onLogout}><LogOut size={16} /> Log out</Button>
        </div>
      </AdminSidebar>
  );

  return (
    <AdminShell sidebar={sidebar}>
        <AdminPageHeader
          title={selectedAdminSection.label}
          description={selectedAdminSection.description}
          action={adminSection === "skills" && (
            <Button variant="outline" className="skill-action" onClick={refreshSelected} disabled={loading || !token}>
              <RefreshCw size={16} />
              <span>Refresh</span>
            </Button>
          )}
        />
        <AdminTabs tabs={adminSections} active={adminSection} onChange={setAdminSection} label="Admin sections" />
        {(error || notice) && (
          <AdminSectionNotice
            tone={error ? "destructive" : "success"}
            onDismiss={() => {
              setError("");
              setNotice("");
            }}
          >
            {error || notice}
          </AdminSectionNotice>
        )}
        {!token ? (
          <AdminEmptyState icon={<ShieldCheck size={26} />} title="Admin token required">
            Enter `AGENT_API_ADMIN_TOKEN` to load protected admin APIs. This console is separate from the C-end workspace.
          </AdminEmptyState>
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
        ) : adminSection === "prompts" ? (
          <AdminPromptPanel api={api} adminToken={adminToken} />
        ) : (
          <AdminSplitPane>
            <AdminListPanel>
              <div className="admin-list-tools">
                <AdminSearchBox icon={<Search size={16} />}>
                  <Input value={query} onChange={(event) => setQuery(event.currentTarget.value)} placeholder="Search skills" aria-label="Search admin skills" />
                </AdminSearchBox>
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
                  <Button
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
                  </Button>
                ))}
                {!filteredSkills.length && <div className="empty-small">{loading ? "Loading..." : "No skills"}</div>}
              </div>
            </AdminListPanel>
            <AdminDetailPanel>
              {!selectedSkill ? (
                <AdminEmptyState icon={<Sparkles size={24} />} title="Select a skill">
                  Choose a registry skill to inspect release status, policy, review issues, and execution metrics.
                </AdminEmptyState>
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
                    <Button className="primary skill-action" onClick={() => changeSkillStatus("publish")} disabled={Boolean(actionBusy)}>
                      <PlayCircle size={16} />
                      <span>{actionBusy === "publish" ? "Publishing" : "Publish"}</span>
                    </Button>
                    <Button className="skill-action" onClick={() => changeSkillStatus("unpublish")} disabled={Boolean(actionBusy)}>
                      <Archive size={16} />
                      <span>{actionBusy === "unpublish" ? "Unpublishing" : "Unpublish"}</span>
                    </Button>
                    <Button className="skill-action danger-outline" onClick={() => changeSkillStatus("disable")} disabled={Boolean(actionBusy)}>
                      <UserX size={16} />
                      <span>{actionBusy === "disable" ? "Disabling" : "Disable"}</span>
                    </Button>
                    <Button className="skill-action" onClick={() => setPolicyTarget(selectedSkill)}>
                      <ShieldCheck size={16} />
                      <span>Policy</span>
                    </Button>
                    <Button className="skill-action" onClick={() => loadSkillDetails(selectedSkill.name)} disabled={detailsLoading}>
                      <RefreshCw size={16} />
                      <span>{detailsLoading ? "Loading" : "Review"}</span>
                    </Button>
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
            </AdminDetailPanel>
          </AdminSplitPane>
        )}
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
    </AdminShell>
  );
}


export default AdminConsole;
