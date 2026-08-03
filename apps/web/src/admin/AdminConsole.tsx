import { useEffect, useMemo, useRef, useState } from "react";
import {
  Activity,
  Archive,
  Info,
  Menu,
  PlayCircle,
  Plus,
  RefreshCw,
  Search,
  ShieldCheck,
  Sparkles,
  UserX,
} from "lucide-react";
import { ApiClient } from "../api/client";
import { Button } from "../components/ui/button";
import { Input } from "../components/ui/input";
import {
  AdminDetailPanel,
  AdminEmptyState,
  AdminListPanel,
  AdminPageHeader,
  AdminSearchBox,
  AdminSectionNotice,
  AdminShell,
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
import {
  AdminContextSidebar,
  AdminRail,
  buildAdminDomains,
  domainForAdminSection,
  isAdminSection,
  type AdminDomain,
  type AdminSection
} from "./AdminNavigation";

function readAdminSection(): AdminSection {
  if (typeof window === "undefined") return "skills";
  const section = new URLSearchParams(window.location.search).get("section");
  return isAdminSection(section) ? section : "skills";
}

function writeAdminSection(section: AdminSection, replace = false) {
  if (typeof window === "undefined") return;
  const url = new URL(window.location.href);
  if (url.searchParams.get("section") === section) return;
  url.searchParams.set("section", section);
  window.history[replace ? "replaceState" : "pushState"]({}, "", `${url.pathname}${url.search}${url.hash}`);
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
  const [adminSection, setAdminSection] = useState<AdminSection>(readAdminSection);
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
  const [navigationOpen, setNavigationOpen] = useState(false);
  const [accessOpen, setAccessOpen] = useState(!adminToken.trim());
  const [adminTokenDraft, setAdminTokenDraft] = useState(adminToken);
  const [commandQuery, setCommandQuery] = useState("");
  const [promptEditorSignal, setPromptEditorSignal] = useState(0);
  const commandInputRef = useRef<HTMLInputElement>(null);
  const token = adminToken.trim();
  const adminDomains = useMemo(() => buildAdminDomains(skills.length), [skills.length]);
  const activeDomain = domainForAdminSection(adminDomains, adminSection);
  const adminSections = useMemo(() => adminDomains.flatMap((domain) => domain.sections), [adminDomains]);
  const selectedAdminSection = adminSections.find((section) => section.id === adminSection) || adminSections[0];
  const commandMatches = useMemo(() => {
    const normalized = commandQuery.trim().toLowerCase();
    if (!normalized) return [];
    return adminSections.filter((section) => `${section.label} ${section.description}`.toLowerCase().includes(normalized)).slice(0, 6);
  }, [adminSections, commandQuery]);
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

  const loadSkills = async (notify = false) => {
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
      if (notify) setNotice(`Loaded ${next.length} skills`);
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
    setAdminTokenDraft(adminToken);
  }, [adminToken]);

  useEffect(() => {
    if (token) void loadSkills();
  }, [token]);

  useEffect(() => {
    const focusCommand = (event: KeyboardEvent) => {
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "k") {
        event.preventDefault();
        commandInputRef.current?.focus();
      }
    };
    window.addEventListener("keydown", focusCommand);
    return () => window.removeEventListener("keydown", focusCommand);
  }, []);

  useEffect(() => {
    writeAdminSection(adminSection, true);
    const syncSectionFromLocation = () => setAdminSection(readAdminSection());
    window.addEventListener("popstate", syncSectionFromLocation);
    return () => window.removeEventListener("popstate", syncSectionFromLocation);
  }, []);

  useEffect(() => {
    if (selectedName && token) void loadSkillDetails(selectedName);
  }, [selectedName, token]);

  const refreshSelected = async () => {
    await loadSkills(true);
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

  const selectAdminSection = (section: AdminSection) => {
    if (section !== adminSection) writeAdminSection(section);
    setAdminSection(section);
    setCommandQuery("");
    setNavigationOpen(false);
  };

  const selectAdminDomain = (domain: AdminDomain) => {
    const targetDomain = adminDomains.find((item) => item.id === domain);
    if (!targetDomain) return;
    const currentInDomain = targetDomain.sections.find((section) => section.id === adminSection);
    selectAdminSection((currentInDomain || targetDomain.sections[0]).id);
  };

  const saveAdminAccess = () => {
    const nextToken = adminTokenDraft.trim();
    if (!nextToken) return;
    onAdminTokenChange(nextToken);
    setAccessOpen(false);
    if (nextToken === token) void loadSkills();
  };

  const commandSearch = (
    <div className="admin-command-search">
      <Search size={16} />
      <Input
        ref={commandInputRef}
        value={commandQuery}
        onChange={(event) => setCommandQuery(event.currentTarget.value)}
        onKeyDown={(event) => {
          if (event.key === "Escape") setCommandQuery("");
          if (event.key === "Enter" && commandMatches[0]) selectAdminSection(commandMatches[0].id);
        }}
        placeholder="Search or run a command..."
        aria-label="Search admin sections"
      />
      <kbd>⌘ K</kbd>
      {commandQuery.trim() && (
        <div className="admin-command-results" role="listbox" aria-label="Admin search results">
          {commandMatches.map((section) => (
            <button key={section.id} type="button" onClick={() => selectAdminSection(section.id)} role="option">
              {section.icon}
              <span>
                <strong>{section.label}</strong>
                <small>{section.description}</small>
              </span>
            </button>
          ))}
          {!commandMatches.length && <p>No matching admin section.</p>}
        </div>
      )}
    </div>
  );

  const rail = (
    <AdminRail
      domains={adminDomains}
      activeDomain={activeDomain.id}
      userLabel={userLabel}
      onDomainChange={selectAdminDomain}
      onExit={onExit}
      onAccess={() => setAccessOpen((current) => !current)}
      onCloseNavigation={() => setNavigationOpen(false)}
    />
  );

  const sidebar = (
    <AdminContextSidebar
      domain={activeDomain}
      activeSection={adminSection}
      userLabel={userLabel}
      accessOpen={accessOpen}
      tokenConfigured={Boolean(token)}
      adminTokenDraft={adminTokenDraft}
      onSectionChange={selectAdminSection}
      onAccessToggle={() => setAccessOpen((current) => !current)}
      onAdminTokenDraftChange={setAdminTokenDraft}
      onSaveAccess={saveAdminAccess}
      onLogout={onLogout}
    />
  );

  return (
    <AdminShell rail={rail} sidebar={sidebar} navigationOpen={navigationOpen}>
        <AdminPageHeader
          leading={(
            <Button variant="ghost" size="icon" className="admin-mobile-menu" onClick={() => setNavigationOpen(true)} aria-label="Open navigation">
              <Menu size={20} />
            </Button>
          )}
          breadcrumb={<><span>{activeDomain.label}</span><span aria-hidden="true">/</span><strong>{selectedAdminSection.label}</strong></>}
          title={selectedAdminSection.label}
          badge={<StatusBadge value="production" />}
          description={selectedAdminSection.description}
          search={commandSearch}
          action={adminSection === "skills" ? (
              <Button variant="outline" className="skill-action" onClick={refreshSelected} disabled={loading || !token}>
                <RefreshCw size={16} />
                <span>Refresh</span>
              </Button>
            ) : adminSection === "prompts" ? (
              <Button variant="primary" className="skill-action" onClick={() => setPromptEditorSignal((current) => current + 1)} disabled={!token}>
                <Plus size={16} />
                <span>New candidate</span>
              </Button>
            ) : null}
        />
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
            <span>Enter the protected Admin API credential to open this workspace.</span>
            <form className="admin-access-gate" onSubmit={(event) => { event.preventDefault(); saveAdminAccess(); }}>
              <Input
                type="password"
                value={adminTokenDraft}
                onChange={(event) => setAdminTokenDraft(event.currentTarget.value)}
                placeholder="AGENT_API_ADMIN_TOKEN"
                aria-label="Admin token"
                autoComplete="off"
              />
              <Button type="submit" variant="primary" disabled={!adminTokenDraft.trim()}>Open admin</Button>
            </form>
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
          <AdminPromptPanel api={api} adminToken={adminToken} openEditorSignal={promptEditorSignal} />
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
