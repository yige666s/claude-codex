import type { ReactNode } from "react";
import {
  Activity,
  Boxes,
  BriefcaseBusiness,
  ChevronDown,
  Database,
  FileClock,
  FileText,
  HeartPulse,
  LogOut,
  MessageCircle,
  ScrollText,
  Settings2,
  ShieldCheck,
  Sparkles,
  Users,
  X
} from "lucide-react";
import { BrandLogo } from "../components/brand/BrandLogo";
import { Button } from "../components/ui/button";
import { Input } from "../components/ui/input";

export type AdminSection = "skills" | "users" | "jobs-assets" | "health-cost" | "audit" | "evaluation" | "prompts";
export type AdminDomain = "build" | "operate" | "observe" | "govern";

const adminSections = new Set<AdminSection>(["skills", "users", "jobs-assets", "health-cost", "audit", "evaluation", "prompts"]);

export function isAdminSection(value: string | null | undefined): value is AdminSection {
  return Boolean(value && adminSections.has(value as AdminSection));
}

export type AdminNavigationItem = {
  id: AdminSection;
  label: string;
  description: string;
  icon: ReactNode;
  count?: number;
};

export type AdminDomainItem = {
  id: AdminDomain;
  label: string;
  icon: ReactNode;
  sections: AdminNavigationItem[];
};

export function buildAdminDomains(skillCount: number): AdminDomainItem[] {
  return [
    {
      id: "build",
      label: "Build",
      icon: <Boxes size={20} />,
      sections: [
        { id: "skills", label: "Skills", description: "Publish, review, configure policy, and inspect execution health for registry-backed skills.", icon: <Sparkles size={17} />, count: skillCount },
        { id: "prompts", label: "Prompts", description: "Manage prompt versions, previews, evaluations, release gates, and production pins.", icon: <ScrollText size={17} /> }
      ]
    },
    {
      id: "operate",
      label: "Operate",
      icon: <BriefcaseBusiness size={20} />,
      sections: [
        { id: "jobs-assets", label: "Runs & assets", description: "Inspect sessions, queued jobs, event replays, and generated or uploaded assets.", icon: <FileClock size={17} /> }
      ]
    },
    {
      id: "observe",
      label: "Observe",
      icon: <Activity size={20} />,
      sections: [
        { id: "health-cost", label: "Health & cost", description: "Watch readiness, model health, token usage, latency, quota, and estimated cost.", icon: <HeartPulse size={17} /> },
        { id: "audit", label: "Audit", description: "Investigate sensitive operations, risk signals, request IDs, user scope, and metadata.", icon: <FileText size={17} /> }
      ]
    },
    {
      id: "govern",
      label: "Govern",
      icon: <ShieldCheck size={20} />,
      sections: [
        { id: "users", label: "Users", description: "Search accounts, inspect access state, and disable, ban, or reactivate users.", icon: <Users size={17} /> },
        { id: "evaluation", label: "Evaluations", description: "Run evaluations over runtime data, inspect findings, and close review items.", icon: <Database size={17} /> }
      ]
    }
  ];
}

export function domainForAdminSection(domains: AdminDomainItem[], section: AdminSection): AdminDomainItem {
  return domains.find((domain) => domain.sections.some((item) => item.id === section)) || domains[0];
}

export function AdminRail({
  domains,
  activeDomain,
  userLabel,
  onDomainChange,
  onExit,
  onAccess,
  onCloseNavigation
}: {
  domains: AdminDomainItem[];
  activeDomain: AdminDomain;
  userLabel: string;
  onDomainChange: (domain: AdminDomain) => void;
  onExit: () => void;
  onAccess: () => void;
  onCloseNavigation: () => void;
}) {
  const initial = userLabel.trim().slice(0, 1).toUpperCase() || "A";
  return (
    <aside className="admin-rail" aria-label="Admin domains">
      <div className="admin-rail-brand">
        <BrandLogo className="admin-rail-logo" />
        <Button variant="ghost" size="icon" className="admin-navigation-close" onClick={onCloseNavigation} aria-label="Close navigation">
          <X size={18} />
        </Button>
      </div>
      <nav className="admin-rail-nav">
        {domains.map((domain) => (
          <Button
            key={domain.id}
            variant="ghost"
            className={domain.id === activeDomain ? "active" : ""}
            onClick={() => onDomainChange(domain.id)}
            aria-current={domain.id === activeDomain ? "page" : undefined}
            title={domain.label}
          >
            {domain.icon}
            <span>{domain.label}</span>
          </Button>
        ))}
      </nav>
      <div className="admin-rail-utilities">
        <Button variant="ghost" onClick={onExit} title="Back to app">
          <MessageCircle size={19} />
          <span>App</span>
        </Button>
        <Button variant="ghost" onClick={onAccess} title="Admin access">
          <Settings2 size={19} />
          <span>Access</span>
        </Button>
        <button type="button" className="admin-rail-avatar" onClick={onAccess} title={userLabel} aria-label={`Open admin access for ${userLabel}`}>
          {initial}
        </button>
      </div>
    </aside>
  );
}

export function AdminContextSidebar({
  domain,
  activeSection,
  userLabel,
  accessOpen,
  tokenConfigured,
  adminTokenDraft,
  onSectionChange,
  onAccessToggle,
  onAdminTokenDraftChange,
  onSaveAccess,
  onLogout
}: {
  domain: AdminDomainItem;
  activeSection: AdminSection;
  userLabel: string;
  accessOpen: boolean;
  tokenConfigured: boolean;
  adminTokenDraft: string;
  onSectionChange: (section: AdminSection) => void;
  onAccessToggle: () => void;
  onAdminTokenDraftChange: (value: string) => void;
  onSaveAccess: () => void;
  onLogout: () => void;
}) {
  return (
    <aside className="admin-context-sidebar">
      <div className="admin-context-header">
        <strong>AgentAPI</strong>
        <button type="button" className="admin-workspace-switcher" onClick={onAccessToggle} aria-expanded={accessOpen}>
          <Database size={16} />
          <span>
            <strong>Production</strong>
            <small>Workspace access</small>
          </span>
          <ChevronDown size={15} />
        </button>
      </div>
      <nav className="admin-context-nav" aria-label={`${domain.label} administration`}>
        <h2>{domain.label}</h2>
        {domain.sections.map((section) => (
          <Button
            key={section.id}
            variant="ghost"
            className={section.id === activeSection ? "active" : ""}
            onClick={() => onSectionChange(section.id)}
            aria-current={section.id === activeSection ? "page" : undefined}
          >
            {section.icon}
            <span>{section.label}</span>
            {typeof section.count === "number" && <small>{section.count}</small>}
          </Button>
        ))}
      </nav>
      <div className="admin-context-footer">
        {accessOpen && (
          <form className="admin-access-panel" onSubmit={(event) => { event.preventDefault(); onSaveAccess(); }}>
            <div>
              <strong>Admin access</strong>
              <small>{tokenConfigured ? "Credential configured" : "Credential required"}</small>
            </div>
            <label>
              <span>Admin token</span>
              <Input
                type="password"
                value={adminTokenDraft}
                onChange={(event) => onAdminTokenDraftChange(event.currentTarget.value)}
                placeholder="AGENT_API_ADMIN_TOKEN"
                autoComplete="off"
              />
            </label>
            <Button type="submit" variant="primary" disabled={!adminTokenDraft.trim()}>Save access</Button>
          </form>
        )}
        <div className="admin-operator-row">
          <span className={`admin-access-dot ${tokenConfigured ? "ready" : ""}`} />
          <span>
            <strong>{userLabel || "Administrator"}</strong>
            <small>{tokenConfigured ? "Admin access ready" : "Access required"}</small>
          </span>
          <Button variant="ghost" size="icon-sm" onClick={onLogout} title="Log out" aria-label="Log out">
            <LogOut size={16} />
          </Button>
        </div>
      </div>
    </aside>
  );
}
