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

export function AdminUsersPanel({ api, adminToken }: { api: ApiClient; adminToken: string }) {
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
            <Input
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
            <Button className="skill-action" onClick={loadUsers} disabled={loading}>
              <RefreshCw size={15} />
              <span>{loading ? "Loading" : "Search"}</span>
            </Button>
          </div>
        </div>
        <div className="admin-skill-list">
          {users.map((user) => (
            <Button
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
            </Button>
          ))}
          {!users.length && <div className="empty-small">{loading ? "Loading..." : "No users"}</div>}
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
              <Button className="primary skill-action" onClick={() => runAction("reactivate")} disabled={Boolean(actionBusy) || selectedUser.status === "active"}>
                <PlayCircle size={16} />
                <span>{actionBusy === "reactivate" ? "Reactivating" : "Reactivate"}</span>
              </Button>
              <Button className="skill-action" onClick={() => runAction("disable")} disabled={Boolean(actionBusy) || selectedUser.status === "disabled"}>
                <UserX size={16} />
                <span>{actionBusy === "disable" ? "Disabling" : "Disable"}</span>
              </Button>
              <Button className="skill-action danger-outline" onClick={() => runAction("ban")} disabled={Boolean(actionBusy) || selectedUser.status === "banned"}>
                <UserX size={16} />
                <span>{actionBusy === "ban" ? "Banning" : "Ban"}</span>
              </Button>
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
