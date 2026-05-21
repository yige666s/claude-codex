import { ReactNode } from "react";

type WorkspaceFrameProps = {
  leftCollapsed: boolean;
  sidebar: ReactNode;
  workspace: ReactNode;
  modals?: ReactNode;
};

export function WorkspaceFrame({ leftCollapsed, sidebar, workspace, modals }: WorkspaceFrameProps) {
  return (
    <main className={`app-shell ${leftCollapsed ? "left-collapsed" : ""}`}>
      {sidebar}
      {workspace}
      {modals}
    </main>
  );
}
