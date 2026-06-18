import { AlertCircle, CheckCircle2, Clock, FileText, PlayCircle, RotateCcw } from "lucide-react";
import { Button } from "../../../../components/ui/button";
import { Input } from "../../../../components/ui/input";
import { Textarea } from "../../../../components/ui/textarea";
import type { DeepAgentLoopTemplate, DeepAgentResumeRequest, DeepAgentWorkflowSummary, LoopGoal, LoopGoalRunResult } from "../../../../types";

type LoopPanelProps = {
  sessionId: string;
  templates: DeepAgentLoopTemplate[];
  goals: LoopGoal[];
  selectedGoalId: string;
  selectedRun?: LoopGoalRunResult | null;
  createObjective: string;
  selectedTemplateId: string;
  busy: string;
  error: string;
  onObjectiveChange: (value: string) => void;
  onTemplateChange: (value: string) => void;
  onCreateGoal: () => void;
  onSelectGoal: (goalId: string) => void;
  onStartGoal: (goalId: string) => void;
  onResumeRun: (runId: string, request?: DeepAgentResumeRequest) => void;
};

export function LoopPanel({
  sessionId,
  templates,
  goals,
  selectedGoalId,
  selectedRun,
  createObjective,
  selectedTemplateId,
  busy,
  error,
  onObjectiveChange,
  onTemplateChange,
  onCreateGoal,
  onSelectGoal,
  onStartGoal,
  onResumeRun
}: LoopPanelProps) {
  const selectedTemplate = templates.find((item) => item.id === selectedTemplateId) || null;
  const selectedGoal = goals.find((item) => item.id === selectedGoalId) || selectedRun?.goal || null;
  const summary = selectedRun?.deep_agent || null;
  const runID = selectedRun?.run?.id || selectedGoal?.workflow_run_id || "";

  if (!sessionId) return <div className="empty-small">Open a session before creating a loop.</div>;

  return (
    <div className="loop-panel">
      <section className="loop-create">
        <div className="loop-panel-head">
          <strong>Loop goal</strong>
          <small>{templates.length} templates</small>
        </div>
        <label className="loop-field">
          <span>Template</span>
          <select value={selectedTemplateId} onChange={(event) => onTemplateChange(event.currentTarget.value)} aria-label="Loop template">
            <option value="">Choose explicitly</option>
            {templates.map((template) => (
              <option key={template.id} value={template.id}>{template.name}</option>
            ))}
          </select>
        </label>
        {selectedTemplate && (
          <div className="loop-template-summary">
            <span>{selectedTemplate.deliverable || selectedTemplate.task_type || selectedTemplate.id}</span>
            <small>{budgetSummary(selectedTemplate)}</small>
          </div>
        )}
        <label className="loop-field">
          <span>Objective</span>
          <Textarea value={createObjective} onChange={(event) => onObjectiveChange(event.currentTarget.value)} placeholder="Describe the loop goal" rows={3} />
        </label>
        <Button className="primary" onClick={onCreateGoal} disabled={!createObjective.trim() || !selectedTemplateId || Boolean(busy)}>
          <FileText size={15} />
          <span>{busy === "create-loop-goal" ? "Creating" : "Create goal"}</span>
        </Button>
        {error && <p className="loop-error"><AlertCircle size={14} />{error}</p>}
      </section>

      <section className="loop-goal-list">
        {goals.map((goal) => (
          <Button key={goal.id} className={`loop-goal-row ${goal.id === selectedGoalId ? "active" : ""}`} onClick={() => onSelectGoal(goal.id)}>
            {statusIcon(goal.status)}
            <span>
              <strong>{goal.objective}</strong>
              <small>{goalTemplateLabel(goal, templates)} · {goal.status}</small>
            </span>
            <small>{goal.workflow_run_id ? "run" : "draft"}</small>
          </Button>
        ))}
        {!goals.length && <div className="empty-small">No loop goals in this session.</div>}
      </section>

      {selectedGoal && (
        <section className="loop-detail">
          <div className="loop-panel-head">
            <strong>{selectedGoal.objective}</strong>
            <small>{selectedGoal.status}</small>
          </div>
          <div className="loop-facts">
            <span>Template<small>{goalTemplateLabel(selectedGoal, templates)}</small></span>
            <span>Trigger<small>{selectedGoal.trigger?.type || "manual"}</small></span>
            <span>Budget<small>{budgetSummary(selectedGoal)}</small></span>
            <span>Run<small>{runID || "not started"}</small></span>
          </div>
          <div className="loop-actions">
            <Button onClick={() => onStartGoal(selectedGoal.id)} disabled={Boolean(busy)}>
              <PlayCircle size={15} />
              <span>{busy === `start-${selectedGoal.id}` ? "Starting" : "Start"}</span>
            </Button>
            {runID && summary?.recovery?.resume_available && (
              <Button onClick={() => onResumeRun(runID)} disabled={Boolean(busy)}>
                <RotateCcw size={15} />
                <span>{busy === `resume-${runID}` ? "Resuming" : "Resume"}</span>
              </Button>
            )}
          </div>
          {summary && <LoopRecovery summary={summary} runID={runID} busy={busy} onResumeRun={onResumeRun} />}
        </section>
      )}
    </div>
  );
}

function LoopRecovery({ summary, runID, busy, onResumeRun }: { summary: DeepAgentWorkflowSummary; runID: string; busy: string; onResumeRun: (runId: string, request?: DeepAgentResumeRequest) => void }) {
  const recovery = summary.recovery;
  return (
    <div className="loop-recovery">
      <div className="loop-panel-head">
        <strong>{recovery?.user_facing_reason || summary.status || "Loop status"}</strong>
        <small>{recovery?.blocked_category || summary.status || "active"}</small>
      </div>
      {recovery?.recommended_next_action && <p>{recovery.recommended_next_action}</p>}
      {!!recovery?.missing_info?.length && (
        <ul>
          {recovery.missing_info.map((item) => <li key={item}>{item}</li>)}
        </ul>
      )}
      {runID && recovery?.review_pending && (
        <div className="loop-actions">
          <Button onClick={() => onResumeRun(runID, { review_decision: { action: "approve", step_id: recovery.review_step_id, action_hash: recovery.review_action_hash } })} disabled={Boolean(busy)}>
            <CheckCircle2 size={15} />
            <span>Approve</span>
          </Button>
          <Button className="danger-outline" onClick={() => onResumeRun(runID, { review_decision: { action: "reject", step_id: recovery.review_step_id, action_hash: recovery.review_action_hash, reason: "rejected from loop panel" } })} disabled={Boolean(busy)}>
            <AlertCircle size={15} />
            <span>Reject</span>
          </Button>
        </div>
      )}
      <LoopEvidence summary={summary} />
    </div>
  );
}

function LoopEvidence({ summary }: { summary: DeepAgentWorkflowSummary }) {
  const finalAnswer = summary.final_answer;
  const timeline = summary.timeline || [];
  return (
    <div className="loop-evidence">
      {!!timeline.length && (
        <div>
          <strong>Timeline</strong>
          {timeline.slice(-5).map((item, index) => (
            <div key={`${item.kind}-${item.step_id || item.action_hash || index}`} className="loop-evidence-row">
              <span>{item.kind}</span>
              <small>{item.title || item.summary || item.step_id || item.tool || "event"}</small>
            </div>
          ))}
        </div>
      )}
      {!!finalAnswer?.artifacts?.length && <EvidenceList title="Artifacts" items={finalAnswer.artifacts} labelKey="filename" fallbackKey="id" />}
      {!!finalAnswer?.sources?.length && <EvidenceList title="Sources" items={finalAnswer.sources} labelKey="title" fallbackKey="url" />}
      {!!finalAnswer?.tests?.length && <EvidenceList title="Tests" items={finalAnswer.tests} labelKey="command" fallbackKey="status" />}
      {!!finalAnswer?.known_gaps?.length && (
        <div>
          <strong>Known gaps</strong>
          {finalAnswer.known_gaps.map((gap) => <div key={gap} className="loop-evidence-row"><span>gap</span><small>{gap}</small></div>)}
        </div>
      )}
    </div>
  );
}

function EvidenceList({ title, items, labelKey, fallbackKey }: { title: string; items: Array<Record<string, unknown>>; labelKey: string; fallbackKey: string }) {
  return (
    <div>
      <strong>{title}</strong>
      {items.slice(0, 5).map((item, index) => (
        <div key={`${title}-${index}`} className="loop-evidence-row">
          <span>{title.slice(0, -1).toLowerCase()}</span>
          <small>{String(item[labelKey] || item[fallbackKey] || "recorded")}</small>
        </div>
      ))}
    </div>
  );
}

function statusIcon(status: string) {
  if (status === "succeeded") return <CheckCircle2 size={16} />;
  if (status === "running" || status === "pending") return <Clock size={16} />;
  return <AlertCircle size={16} />;
}

function goalTemplateLabel(goal: LoopGoal, templates: DeepAgentLoopTemplate[]) {
  const id = goal.template_id || String(goal.metadata?.template_id || "");
  return templates.find((item) => item.id === id)?.name || id || goal.task_type || "manual";
}

function budgetSummary(value: Pick<LoopGoal, "budget"> | DeepAgentLoopTemplate) {
  const budget = "budget" in value ? value.budget : undefined;
  if (!budget) return "default budget";
  const parts = [
    budget.max_steps ? `${budget.max_steps} steps` : "",
    budget.max_actions ? `${budget.max_actions} actions` : "",
    budget.max_tool_calls ? `${budget.max_tool_calls} tools` : ""
  ].filter(Boolean);
  return parts.join(" · ") || "default budget";
}
