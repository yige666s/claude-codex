import { ReactNode } from "react";

type WorkspaceFrameProps = {
  leftCollapsed: boolean;
  rightCollapsed: boolean;
  sidebar: ReactNode;
  workspace: ReactNode;
  rightPanel: ReactNode;
  modals?: ReactNode;
};

export function WorkspaceFrame({ leftCollapsed, rightCollapsed, sidebar, workspace, rightPanel, modals }: WorkspaceFrameProps) {
  return (
    <main className={`app-shell ${leftCollapsed ? "left-collapsed" : ""} ${rightCollapsed ? "right-collapsed" : ""}`}>
      {sidebar}
      {workspace}
      {rightPanel}
      {modals}
    </main>
  );
}
