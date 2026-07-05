import { Bell, BellOff, CheckCircle2, ChevronRight, Clock3, Inbox, RotateCcw, ShieldAlert, X, XCircle } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import type { ReactNode } from "react";
import { Button } from "../../../components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "../../../components/ui/dialog";
import type { TaskInboxGroup, TaskInboxItem } from "../../../types";

type TaskInboxDialogProps = {
  open: boolean;
  items: TaskInboxItem[];
  loading: boolean;
  error: string;
  busyAction: string;
  browserNotificationPermission: NotificationPermission | "unsupported";
  browserNotificationsEnabled: boolean;
  onOpenChange: (open: boolean) => void;
  onRefresh: () => void;
  onEnableBrowserNotifications: () => void;
  onDisableBrowserNotifications: () => void;
  onOpenItem: (item: TaskInboxItem) => void;
  onReview: (item: TaskInboxItem, action: "approve" | "reject") => void;
  formatTime: (value?: string) => string;
};

const inboxGroups: Array<{ group: TaskInboxGroup; label: string; icon: ReactNode }> = [
  { group: "running", label: "Running", icon: <Clock3 size={16} /> },
  { group: "needs_review", label: "Needs review", icon: <ShieldAlert size={16} /> },
  { group: "failed", label: "Failed", icon: <XCircle size={16} /> },
  { group: "blocked", label: "Blocked", icon: <ShieldAlert size={16} /> },
  { group: "completed", label: "Completed", icon: <CheckCircle2 size={16} /> },
  { group: "scheduled", label: "Scheduled", icon: <Clock3 size={16} /> }
];

const taskInboxGroupPageSize = 5;

export function TaskInboxDialog({
  open,
  items,
  loading,
  error,
  busyAction,
  browserNotificationPermission,
  browserNotificationsEnabled,
  onOpenChange,
  onRefresh,
  onEnableBrowserNotifications,
  onDisableBrowserNotifications,
  onOpenItem,
  onReview,
  formatTime
}: TaskInboxDialogProps) {
  const [visibleByGroup, setVisibleByGroup] = useState<Partial<Record<TaskInboxGroup, number>>>({});
  const grouped = useMemo(() => inboxGroups.map((entry) => ({
    ...entry,
    items: items.filter((item) => item.group === entry.group)
  })).filter((entry) => entry.items.length > 0 || entry.group === "needs_review" || entry.group === "running"), [items]);

  useEffect(() => {
    if (open) {
      setVisibleByGroup({});
    }
  }, [open]);

  function visibleCountForGroup(group: TaskInboxGroup) {
    return visibleByGroup[group] ?? taskInboxGroupPageSize;
  }

  function showMore(group: TaskInboxGroup, total: number) {
    setVisibleByGroup((current) => {
      const next = Math.min((current[group] ?? taskInboxGroupPageSize) + taskInboxGroupPageSize, total);
      return { ...current, [group]: next };
    });
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="task-inbox-modal" hideClose>
        <DialogTitle className="sr-only">Task Inbox</DialogTitle>
        <DialogDescription className="sr-only">Review task status, approvals, failures, and generated artifacts.</DialogDescription>
        <div className="task-inbox-head">
          <div>
            <span className="task-inbox-kicker"><Inbox size={17} /> Task Inbox</span>
            <h2>通知与审批中心</h2>
            {browserNotificationPermission === "denied" && (
              <p className="task-inbox-permission-note">Browser notifications are blocked in this browser.</p>
            )}
          </div>
          <div className="task-inbox-actions">
            {browserNotificationPermission !== "unsupported" && (
              browserNotificationsEnabled ? (
                <Button
                  className="task-inbox-notification-toggle"
                  variant="ghost"
                  onClick={onDisableBrowserNotifications}
                  title="Disable browser notifications"
                >
                  <BellOff size={16} /> Alerts on
                </Button>
              ) : (
                <Button
                  className="task-inbox-notification-toggle"
                  variant="ghost"
                  onClick={onEnableBrowserNotifications}
                  disabled={browserNotificationPermission === "denied"}
                  title="Enable browser notifications"
                >
                  <Bell size={16} /> Enable alerts
                </Button>
              )
            )}
            <Button className="icon" variant="ghost" onClick={onRefresh} disabled={loading} title="Refresh inbox" aria-label="Refresh inbox">
              <RotateCcw size={17} />
            </Button>
            <Button className="icon" variant="ghost" onClick={() => onOpenChange(false)} title="Close inbox" aria-label="Close inbox">
              <X size={18} />
            </Button>
          </div>
        </div>
        {error && <div className="task-inbox-error">{error}</div>}
        <div className="task-inbox-body">
          {loading && items.length === 0 && <div className="task-inbox-empty">Loading tasks...</div>}
          {!loading && items.length === 0 && !error && <div className="task-inbox-empty">No recent task activity.</div>}
          {grouped.map((entry) => (
            <section key={entry.group} className="task-inbox-group">
              <header>
                <span>{entry.icon}{entry.label}</span>
                <strong>{entry.items.length}</strong>
              </header>
              {entry.items.length === 0 ? (
                <div className="task-inbox-group-empty">Nothing here.</div>
              ) : (
                <div className="task-inbox-list">
                  {entry.items.slice(0, visibleCountForGroup(entry.group)).map((item) => {
                    const sessionUnavailable = Boolean(item.session_id) && item.session_available === false;
                    const cardBody = (
                      <>
                        <span className="task-card-kind">{item.kind} · {item.status}</span>
                        <span className="task-card-title-row">
                          <strong>{item.title}</strong>
                          <time>{formatTime(item.last_event_at || item.updated_at)}</time>
                        </span>
                        <span className="task-card-subtitle">{sessionUnavailable ? "关联聊天已删除。" : (item.last_event || "Task updated")}</span>
                        <span className="task-card-meta">
                          {item.trigger && <span>{item.trigger}</span>}
                          {sessionUnavailable && <span>聊天已删除</span>}
                          {item.artifact_count > 0 && <span>{item.artifact_count} artifact{item.artifact_count === 1 ? "" : "s"}</span>}
                        </span>
                      </>
                    );
                    return (
                      <article key={item.id} className={`task-card task-card-${item.group}${sessionUnavailable ? " task-card-orphaned" : ""}`}>
                        {sessionUnavailable ? (
                          <div className="task-card-main task-card-main-static">{cardBody}</div>
                        ) : (
                          <button type="button" className="task-card-main" onClick={() => onOpenItem(item)}>
                            {cardBody}
                          </button>
                        )}
                        {!sessionUnavailable && <div className="task-card-actions">
                          {item.review?.run_id && (
                            <>
                              <Button
                                className="task-review-approve"
                                onClick={() => onReview(item, "approve")}
                                disabled={Boolean(busyAction)}
                              >
                                Approve
                              </Button>
                              <Button
                                className="task-review-reject"
                                onClick={() => onReview(item, "reject")}
                                disabled={Boolean(busyAction)}
                              >
                                Reject
                              </Button>
                            </>
                          )}
                          <Button className="icon ghost" onClick={() => onOpenItem(item)} title={item.next_action || "Open"} aria-label={item.next_action || "Open"}>
                            <ChevronRight size={18} />
                          </Button>
                        </div>}
                      </article>
                    );
                  })}
                  {entry.items.length > visibleCountForGroup(entry.group) && (
                    <Button className="task-inbox-show-more" variant="ghost" onClick={() => showMore(entry.group, entry.items.length)}>
                      查看更多
                    </Button>
                  )}
                </div>
              )}
            </section>
          ))}
        </div>
      </DialogContent>
    </Dialog>
  );
}
