import { lazy, Suspense } from "react";

const AgentWorkspace = lazy(() => import("./features/workspace/AgentWorkspace"));

export function App() {
  return (
    <Suspense fallback={<div className="app-loading">Loading workspace...</div>}>
      <AgentWorkspace />
    </Suspense>
  );
}
