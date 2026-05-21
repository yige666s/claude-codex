import { ReactNode, Ref, RefObject } from "react";
import { Briefcase, Database, FileUp, Image, LogOut, MessageSquarePlus, PanelLeft, Search, Settings, Sparkles, X } from "lucide-react";
import { BrandLogo } from "../../../components/brand/BrandLogo";
import { Button } from "../../../components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger
} from "../../../components/ui/dropdown-menu";
import type { AuthSession, Session } from "../../../types";
import type { RightPanelTab, ServiceStatus } from "../workspaceTypes";
import { SessionList } from "./sidebar/SessionList";

type WorkspaceSidebarProps = {
  authSession: AuthSession;
  sessions: Session[];
  sessionId: string;
  mobileOpen: boolean;
  leftOpen: boolean;
  serviceStatus: ServiceStatus;
  settingsOpen: boolean;
  accountRef: RefObject<HTMLDivElement | null>;
  resourceCounts: Record<RightPanelTab, number>;
  serviceStatusPill: (status: ServiceStatus) => ReactNode;
  onToggleLeft: () => void;
  onCollapseLeft: () => void;
  onCloseMobile: () => void;
  onCreateSession: () => void;
  onOpenSearch: () => void;
  onOpenResource: (tab: RightPanelTab) => void;
  onSelectSession: (id: string) => void;
  onRemoveSession: (id: string) => void;
  onToggleSettings: (open: boolean) => void;
  onOpenSettings: () => void;
  onManageMemory: () => void;
  onLogout: () => void;
};

export function WorkspaceSidebar({
  authSession,
  sessions,
  sessionId,
  mobileOpen,
  leftOpen,
  serviceStatus,
  settingsOpen,
  accountRef,
  resourceCounts,
  serviceStatusPill,
  onToggleLeft,
  onCollapseLeft,
  onCloseMobile,
  onCreateSession,
  onOpenSearch,
  onOpenResource,
  onSelectSession,
  onRemoveSession,
  onToggleSettings,
  onOpenSettings,
  onManageMemory,
  onLogout
}: WorkspaceSidebarProps) {
  return (
    <aside className={`sidebar ${mobileOpen ? "open" : ""}`}>
      <div className="sidebar-head">
        <Button
          className="brand-toggle"
          variant="ghost"
          onClick={onToggleLeft}
          title={leftOpen ? "Collapse sidebar" : "Expand sidebar"}
          aria-label={leftOpen ? "Collapse sidebar" : "Expand sidebar"}
          aria-expanded={leftOpen}
        >
          <BrandLogo className="brand-icon" />
          <span className="brand-toggle-icon"><PanelLeft size={18} /></span>
        </Button>
        <BrandLogo className="brand-mark sidebar-logo" />
        <strong>AgentAPI</strong>
        {serviceStatusPill(serviceStatus)}
        <Button
          className="icon sidebar-collapse-button"
          variant="outline"
          size="icon"
          onClick={onCollapseLeft}
          title="Collapse sidebar"
          aria-label="Collapse sidebar"
          aria-expanded={leftOpen}
        >
          <PanelLeft size={18} />
        </Button>
        <Button className="icon mobile-only" variant="ghost" size="icon" onClick={onCloseMobile} title="Close navigation" aria-label="Close navigation"><X size={18} /></Button>
      </div>
      <div className="toolbar">
        <Button className="sidebar-action-button primary-action" variant="ghost" onClick={onCreateSession} title="新聊天" aria-label="新聊天">
          <MessageSquarePlus size={18} />
          <span className="sidebar-action-label">新聊天</span>
        </Button>
        <Button className="sidebar-action-button" variant="ghost" onClick={onOpenSearch} title="搜索聊天" aria-label="搜索聊天">
          <Search size={18} />
          <span className="sidebar-action-label">搜索聊天</span>
        </Button>
      </div>
      <nav className="sidebar-resource-nav" aria-label="Workspace resources">
        <ResourceButton tab="skills" label="Skills" count={resourceCounts.skills} icon={<Sparkles size={17} />} onOpen={onOpenResource} />
        <ResourceButton tab="jobs" label="Jobs" count={resourceCounts.jobs} icon={<Briefcase size={17} />} onOpen={onOpenResource} />
        <ResourceButton tab="attachments" label="Attachments" count={resourceCounts.attachments} icon={<FileUp size={17} />} onOpen={onOpenResource} />
        <ResourceButton tab="artifacts" label="Artifacts" count={resourceCounts.artifacts} icon={<Image size={17} />} onOpen={onOpenResource} />
      </nav>
      <SessionList
        sessions={sessions}
        sessionId={sessionId}
        onSelectSession={onSelectSession}
        onRemoveSession={onRemoveSession}
      />
      <div className="account" ref={accountRef as Ref<HTMLDivElement>}>
        <div className="account-identity">
          <strong>{authSession.user.display_name || authSession.user.email}</strong>
          <small>{authSession.user.email}</small>
        </div>
        <DropdownMenu open={settingsOpen} onOpenChange={onToggleSettings}>
          <DropdownMenuTrigger asChild>
            <Button className="icon" variant="outline" size="icon" title="Settings" aria-label="Settings">
              <Settings size={18} />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent className="settings-menu" align="end" side="top" sideOffset={8}>
            <DropdownMenuItem onClick={onOpenSettings}><Settings size={16} /> Settings</DropdownMenuItem>
            <DropdownMenuItem onClick={onManageMemory}><Database size={16} /> Manage Memory</DropdownMenuItem>
            <DropdownMenuItem onClick={onLogout}><LogOut size={16} /> Log Out</DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </aside>
  );
}

function ResourceButton({
  tab,
  label,
  count,
  icon,
  onOpen
}: {
  tab: RightPanelTab;
  label: string;
  count: number;
  icon: ReactNode;
  onOpen: (tab: RightPanelTab) => void;
}) {
  return (
    <Button className="sidebar-resource-button" variant="ghost" onClick={() => onOpen(tab)} title={label} aria-label={label}>
      {icon}
      <span className="sidebar-action-label">{label}</span>
      <small className="sidebar-resource-count">{count}</small>
    </Button>
  );
}
