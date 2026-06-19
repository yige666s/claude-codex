import { CSSProperties, PointerEvent as ReactPointerEvent, ReactNode } from "react";

type WorkspaceFrameProps = {
  leftCollapsed: boolean;
  resizingPane?: "" | "sidebar" | "artifact";
  sidebar: ReactNode;
  workspace: ReactNode;
  modals?: ReactNode;
  style?: CSSProperties;
  onSidebarResizeStart?: (event: ReactPointerEvent<HTMLDivElement>) => void;
};

export function WorkspaceFrame({
  leftCollapsed,
  resizingPane = "",
  sidebar,
  workspace,
  modals,
  style,
  onSidebarResizeStart
}: WorkspaceFrameProps) {
  return (
    <main
      className={`app-shell ${leftCollapsed ? "left-collapsed" : ""} ${resizingPane ? "resizing" : ""}`}
      style={style}
    >
      {sidebar}
      {!leftCollapsed && onSidebarResizeStart && (
        <div
          className={`workspace-resizer workspace-resizer-sidebar ${resizingPane === "sidebar" ? "dragging" : ""}`}
          role="separator"
          aria-label="Resize sidebar"
          aria-orientation="vertical"
          tabIndex={0}
          onPointerDown={onSidebarResizeStart}
        />
      )}
      {workspace}
      {modals}
    </main>
  );
}
