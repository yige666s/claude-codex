import { lazy, Suspense } from "react";
import { TooltipProvider } from "./components/ui/tooltip";

const AgentWorkspace = lazy(() => import("./features/workspace/AgentWorkspace"));

export function App() {
  return (
    <TooltipProvider>
      <Suspense fallback={<div className="app-loading">Loading workspace...</div>}>
        <AgentWorkspace />
      </Suspense>
    </TooltipProvider>
  );
}
