import { ChevronDown } from "lucide-react";
import { Button } from "../../../../components/ui/button";
import { MotionPanel } from "../../../../components/motion";
import type { Job, JobEvent } from "../../../../types";
import type { JobStreamStatus } from "../../workspaceTypes";

const terminalJobs = new Set(["succeeded", "failed", "cancelled"]);

type JobPanelProps = {
  jobs: Job[];
  selectedJobId: string;
  jobEvents: JobEvent[];
  jobStreamNotice: string;
  jobStreamStatus: JobStreamStatus;
  emptyLabel: string;
  onToggleJob: (jobId: string) => void;
  onCancelJob: () => void;
  formatTime: (value?: string) => string;
};

export function JobPanel({
  jobs,
  selectedJobId,
  jobEvents,
  jobStreamNotice,
  jobStreamStatus,
  emptyLabel,
  onToggleJob,
  onCancelJob,
  formatTime
}: JobPanelProps) {
  if (!jobs.length) return <div className="empty-small">{emptyLabel}</div>;
  return (
    <div className="job-list">
      {jobs.map((job) => {
        const expanded = job.id === selectedJobId;
        return (
          <section key={job.id} className={`job-list-entry ${expanded ? "expanded" : ""}`}>
            <Button
              className={`job-summary ${expanded ? "active" : ""}`}
              onClick={() => onToggleJob(job.id)}
              aria-expanded={expanded}
            >
              <span>{job.content || job.id}</span>
              <small>{job.status} · {formatTime(job.updated_at)}</small>
              <ChevronDown size={16} aria-hidden="true" />
            </Button>
            {expanded && (
              <MotionPanel className="job-expanded">
                <div className="job-card">
                  <div className={`pill ${job.status}`}>{job.status}</div>
                  {jobStreamNotice && !terminalJobs.has(job.status) && (
                    <span className={`job-stream-state ${jobStreamStatus}`}>{jobStreamNotice}</span>
                  )}
                  <Button className="danger inline" disabled={terminalJobs.has(job.status)} onClick={onCancelJob}>Cancel job</Button>
                </div>
                <div className="timeline">
                  {visibleJobEvents(jobEvents).map((event) => (
                    <div key={event.id} className="timeline-row">
                      <span>{event.type}</span>
                      <p>{event.event?.error || event.event?.content || event.event?.job_reason || event.id}</p>
                    </div>
                  ))}
                </div>
              </MotionPanel>
            )}
          </section>
        );
      })}
    </div>
  );
}

function visibleJobEvents(events: JobEvent[]): JobEvent[] {
  return events.filter((event) => !(event.type === "delta" && event.event?.role === "assistant"));
}
