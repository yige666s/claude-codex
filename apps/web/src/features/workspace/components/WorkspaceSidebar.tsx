import { ReactNode, Ref, RefObject } from "react";
import { Database, LogOut, MessageSquarePlus, PanelLeft, RefreshCw, Search, Settings, Trash2, X } from "lucide-react";
import { BrandLogo } from "../../../components/brand/BrandLogo";
import { Button } from "../../../components/ui/button";
import type { AuthSession, Session } from "../../../types";
import { sessionTitle } from "../../../lib/sessionTitle";
import type { ServiceStatus } from "../workspaceTypes";

type WorkspaceSidebarProps = {
  authSession: AuthSession;
  sessions: Session[];
  sessionId: string;
  mobileOpen: boolean;
  leftOpen: boolean;
  serviceStatus: ServiceStatus;
  settingsOpen: boolean;
  accountRef: RefObject<HTMLDivElement | null>;
  serviceStatusPill: (status: ServiceStatus) => ReactNode;
  onToggleLeft: () => void;
  onCollapseLeft: () => void;
  onCloseMobile: () => void;
  onCreateSession: () => void;
  onOpenSearch: () => void;
  onRefresh: () => void;
  onSelectSession: (id: string) => void;
  onRemoveSession: (id: string) => void;
  onToggleSettings: () => void;
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
  serviceStatusPill,
  onToggleLeft,
  onCollapseLeft,
  onCloseMobile,
  onCreateSession,
  onOpenSearch,
  onRefresh,
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
        <Button className="icon" variant="primary" size="icon" onClick={onCreateSession} title="New session" aria-label="New session"><MessageSquarePlus size={18} /></Button>
        <Button className="icon" variant="outline" size="icon" onClick={onOpenSearch} title="Search messages" aria-label="Search messages">
          <Search size={18} />
        </Button>
        <Button className="icon" variant="outline" size="icon" onClick={onRefresh} title="Refresh" aria-label="Refresh"><RefreshCw size={18} /></Button>
      </div>
      <div className="list sessions">
        {sessions.map((session) => (
          <div key={session.id} className={`list-item session-item ${session.id === sessionId ? "active" : ""}`}>
            <Button className="session-select" variant="ghost" onClick={() => onSelectSession(session.id)}>
              <span>{sessionTitle(session)}</span>
            </Button>
            <Button className="session-delete" variant="ghost" size="icon" onClick={() => onRemoveSession(session.id)} title="Delete session" aria-label="Delete session">
              <Trash2 size={16} />
            </Button>
          </div>
        ))}
      </div>
      <div className="account" ref={accountRef as Ref<HTMLDivElement>}>
        <div className="account-identity">
          <strong>{authSession.user.display_name || authSession.user.email}</strong>
          <small>{authSession.user.email}</small>
        </div>
        <Button className="icon" variant="outline" size="icon" onClick={onToggleSettings} title="Settings" aria-label="Settings"><Settings size={18} /></Button>
        {settingsOpen && (
          <div className="settings-menu">
            <Button variant="ghost" onClick={onOpenSettings}><Settings size={16} /> Settings</Button>
            <Button variant="ghost" onClick={onManageMemory}><Database size={16} /> Manage Memory</Button>
            <Button variant="ghost" onClick={onLogout}><LogOut size={16} /> Log Out</Button>
          </div>
        )}
      </div>
    </aside>
  );
}
