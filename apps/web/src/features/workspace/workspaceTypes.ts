export type Status = {
  tone: "idle" | "ok" | "busy" | "error";
  text: string;
};

export type ServiceStatus = Status & {
  details?: string;
};

export type RightPanelTab = "skills" | "jobs" | "attachments" | "artifacts";
export type RightPanelSearch = Record<RightPanelTab, string>;
export type JobStreamStatus = "idle" | "connecting" | "live" | "reconnecting" | "failed";
export type ComposerToolID = "image" | "web-search" | "plan-execute";

export type ConfirmDialog = {
  title: string;
  message: string;
  detail?: string;
  confirmLabel?: string;
  cancelLabel?: string;
  danger?: boolean;
};
