import { Briefcase, X } from "lucide-react";
import { userFacingErrorMessage } from "../../../api/errorMessages";
import { Button } from "../../../components/ui/button";
import type { Job, JobEvent } from "../../../types";
import type { JobStreamStatus } from "../workspaceTypes";
import { JobEventTimeline, terminalJobs } from "./right-panel/JobPanel";

type JobWorkspaceProps = {
  className?: string;
  job: Job | null;
  events: JobEvent[];
  jobStreamNotice: string;
  jobStreamStatus: JobStreamStatus;
  onClose: () => void;
  onCancel: () => void;
  formatTime: (value?: string) => string;
};

export function JobWorkspace({
  className = "",
  job,
  events,
  jobStreamNotice,
  jobStreamStatus,
  onClose,
  onCancel,
  formatTime
}: JobWorkspaceProps) {
  return (
    <aside className={`artifact-workspace job-workspace ${className}`.trim()} aria-label="Job details">
      <header className="artifact-workspace-head">
        <div>
          <strong>Job Details</strong>
          <small>{job ? job.id : "No job selected"}</small>
        </div>
        <Button className="icon ghost" onClick={onClose} title="Close job details" aria-label="Close job details">
          <X size={18} />
        </Button>
      </header>
      <div className="artifact-workspace-body">
        <div className="artifact-workspace-preview job-workspace-preview">
          {job ? (
            <>
              <div className="artifact-workspace-preview-head">
                <div>
                  <strong>{job.content || job.id}</strong>
                  <small>{job.type || "job"} · {job.status}</small>
                </div>
                <div className="artifact-workspace-actions">
                  <Button className="danger inline" disabled={terminalJobs.has(job.status)} onClick={onCancel}>
                    Cancel job
                  </Button>
                </div>
              </div>
              <JobMetadata job={job} formatTime={formatTime} />
              <div className="job-workspace-events">
                {jobStreamNotice && !terminalJobs.has(job.status) && (
                  <div className={`job-stream-state ${jobStreamStatus}`}>{jobStreamNotice}</div>
                )}
                {job.error && <div className="job-workspace-error">{userFacingErrorMessage(job.error)}</div>}
                <header>
                  <div>
                    <strong>Events</strong>
                    <small>{events.length} event{events.length === 1 ? "" : "s"}</small>
                  </div>
                </header>
                <JobEventTimeline events={events} />
              </div>
            </>
          ) : (
            <div className="artifact-workspace-empty">
              <Briefcase size={32} />
              <strong>No job selected</strong>
            </div>
          )}
        </div>
      </div>
    </aside>
  );
}

function JobMetadata({ job, formatTime }: { job: Job; formatTime: (value?: string) => string }) {
  const metadata = [
    ["Status", job.status],
    ["Type", job.type || "job"],
    ["Created", formatTime(job.created_at)],
    ["Updated", formatTime(job.updated_at)],
    ["Started", formatTime(job.started_at)],
    ["Finished", formatTime(job.finished_at)],
    ["Session", job.session_id],
    ["Attachments", String((job.attachment_ids || []).length || "")],
    ["URLs", String((job.attachment_urls || []).length || "")],
    ["Job ID", job.id]
  ].filter(([, value]) => value);
  return (
    <dl className="artifact-workspace-metadata" aria-label="Job metadata">
      {metadata.map(([label, value]) => (
        <div key={label}>
          <dt>{label}</dt>
          <dd>{value}</dd>
        </div>
      ))}
    </dl>
  );
}
