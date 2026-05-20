import { useEffect, useState } from "react";
import { Activity, Archive, Briefcase, FileText, RefreshCw, Sparkles, Star, Trash2, X } from "lucide-react";
import { Button } from "../../../components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "../../../components/ui/dialog";
import { Input } from "../../../components/ui/input";
import { Textarea } from "../../../components/ui/textarea";
import type { MemoryItem, MemoryMaintenanceAction } from "../../../types";

export function MemoryModal({
  items,
  actions,
  loading,
  error,
  scope,
  statusFilter,
  levelFilter,
  hasSession,
  onScopeChange,
  onStatusChange,
  onLevelChange,
  onRefresh,
  onRebuild,
  onScore,
  onRunMaintenance,
  onUpdate,
  onFeedback,
  onResolve,
  onDelete,
  onClose
}: {
  items: MemoryItem[];
  actions: MemoryMaintenanceAction[];
  loading: boolean;
  error: string;
  scope: "all" | "session";
  statusFilter: string;
  levelFilter: string;
  hasSession: boolean;
  onScopeChange: (scope: "all" | "session") => void;
  onStatusChange: (status: string) => void;
  onLevelChange: (level: string) => void;
  onRefresh: () => void;
  onRebuild: () => void;
  onScore: () => void;
  onRunMaintenance: () => void;
  onUpdate: (item: MemoryItem, patch: Partial<Pick<MemoryItem, "content" | "namespace" | "category" | "tags" | "visibility">>) => void;
  onFeedback: (item: MemoryItem, type: "important" | "incorrect" | "not_relevant") => void;
  onResolve: (item: MemoryItem, action: "accept" | "reject" | "keep_both") => void;
  onDelete: (item: MemoryItem) => void;
  onClose: () => void;
}) {
  const [editingId, setEditingId] = useState("");
  const [draftContent, setDraftContent] = useState("");
  const [draftNamespace, setDraftNamespace] = useState("default");
  const [draftCategory, setDraftCategory] = useState("fact");
  const [draftVisibility, setDraftVisibility] = useState("user");
  const [draftTags, setDraftTags] = useState("");

  function startEdit(item: MemoryItem) {
    setEditingId(item.id);
    setDraftContent(item.content);
    setDraftNamespace(item.namespace || "default");
    setDraftCategory(item.category || "fact");
    setDraftVisibility(item.visibility || "user");
    setDraftTags((item.tags || []).join(", "));
  }

  function cancelEdit() {
    setEditingId("");
    setDraftContent("");
    setDraftNamespace("default");
    setDraftCategory("fact");
    setDraftVisibility("user");
    setDraftTags("");
  }

  function saveEdit(item: MemoryItem) {
    onUpdate(item, {
      content: draftContent,
      namespace: draftNamespace,
      category: draftCategory,
      visibility: draftVisibility,
      tags: draftTags.split(",").map((tag) => tag.trim()).filter(Boolean)
    });
    cancelEdit();
  }

  const pendingActions = actions.filter((action) => action.status !== "dismissed");
  const conflictReviews = pendingActions.filter((action) => action.type === "confirm_conflict").length;
  const safeReviews = Math.max(0, pendingActions.length - conflictReviews);

  return (
    <Dialog open onOpenChange={(open) => {
      if (!open) onClose();
    }}>
      <DialogContent className="memory-modal" hideClose>
        <header>
          <div>
            <strong id="memory-title">Memory</strong>
            <small>{items.length} saved item{items.length === 1 ? "" : "s"}</small>
          </div>
          <div className="memory-actions">
            <Button className="icon ghost" onClick={onScore} title="Score memory quality" aria-label="Score memory quality">
              <Activity size={16} />
            </Button>
            <Button className="icon ghost" onClick={onRunMaintenance} title="Organize memory automatically" aria-label="Organize memory automatically">
              <Briefcase size={16} />
            </Button>
            <Button className="icon ghost" onClick={onRebuild} title="Rebuild memory summaries" aria-label="Rebuild memory summaries">
              <Sparkles size={16} />
            </Button>
            <Button className="icon ghost" onClick={onRefresh} title="Refresh memory" aria-label="Refresh memory">
              <RefreshCw size={16} />
            </Button>
            <Button className="icon ghost" onClick={onClose} title="Close memory" aria-label="Close memory">
              <X size={16} />
            </Button>
          </div>
        </header>
        <div className="memory-scope" role="tablist" aria-label="Memory scope">
          <Button type="button" className={scope === "all" ? "active" : ""} onClick={() => onScopeChange("all")}>
            All
          </Button>
          <Button type="button" className={scope === "session" ? "active" : ""} onClick={() => onScopeChange("session")} disabled={!hasSession}>
            Current Session
          </Button>
          <select value={statusFilter} onChange={(event) => onStatusChange(event.target.value)} aria-label="Memory status filter">
            <option value="active">Active</option>
            <option value="pending_confirm">Pending</option>
            <option value="archived">Archived</option>
            <option value="conflicted">Conflicted</option>
            <option value="all">All status</option>
          </select>
          <select value={levelFilter} onChange={(event) => onLevelChange(event.target.value)} aria-label="Memory level filter">
            <option value="all">All levels</option>
            <option value="atomic">Atomic</option>
            <option value="concept">Concept</option>
            <option value="profile">Profile</option>
          </select>
        </div>
        <div className="memory-scroll-area">
          <div className="memory-system-status" aria-live="polite">
            <div>
              <strong>Automatic memory care</strong>
              <p>System-managed cleanup is active. Low-confidence changes stay visible for audit instead of being applied silently.</p>
            </div>
            <div className="memory-system-badges">
              <span>{pendingActions.length ? `${pendingActions.length} review` : "No review needed"}</span>
              {!!conflictReviews && <span>{conflictReviews} conflict{conflictReviews === 1 ? "" : "s"}</span>}
              {!!safeReviews && <span>{safeReviews} guarded</span>}
            </div>
          </div>
          <div className="memory-list">
            {loading && <div className="memory-empty">Loading memory...</div>}
            {!loading && error && <div className="memory-empty error-text">{error}</div>}
            {!loading && !error && !items.length && <div className="memory-empty">No saved memory</div>}
            {!loading && !error && items.map((item) => (
              <article key={item.id} className="memory-item">
              {editingId === item.id ? (
                <div className="memory-editor">
                  <Textarea value={draftContent} onChange={(event) => setDraftContent(event.target.value)} aria-label="Memory content" />
                  <div className="memory-editor-row">
                    <Input value={draftNamespace} onChange={(event) => setDraftNamespace(event.target.value)} placeholder="namespace" aria-label="Memory namespace" />
                    <select value={draftCategory} onChange={(event) => setDraftCategory(event.target.value)} aria-label="Memory category">
                      <option value="fact">Fact</option>
                      <option value="preference">Preference</option>
                      <option value="event">Event</option>
                      <option value="skill">Skill</option>
                    </select>
                    <select value={draftVisibility} onChange={(event) => setDraftVisibility(event.target.value)} aria-label="Memory visibility">
                      <option value="user">User</option>
                      <option value="private">Private</option>
                      <option value="session_only">Session only</option>
                      <option value="shared">Shared</option>
                    </select>
                    <Input value={draftTags} onChange={(event) => setDraftTags(event.target.value)} placeholder="tags, comma separated" aria-label="Memory tags" />
                  </div>
                  <div className="memory-editor-actions">
                    <Button type="button" onClick={cancelEdit}>Cancel</Button>
                    <Button type="button" className="primary" onClick={() => saveEdit(item)}>Save</Button>
                  </div>
                </div>
              ) : (
                <>
                  <div className="memory-item-meta">
                    <span>{item.level || "atomic"}</span>
                    <span>{item.status}</span>
                    <span>{item.category || item.kind}</span>
                    <span>{item.source}</span>
                    {item.namespace && <span>{item.namespace}</span>}
                    {item.visibility && <span>{item.visibility}</span>}
                    <span>{Math.round((item.confidence || 0) * 100)}% confidence</span>
                    <span>{Math.round((item.weight || 0) * 100)} weight</span>
                    {typeof item.metadata?.quality_score === "number" && <span>{Math.round(item.metadata.quality_score * 100)} quality</span>}
                    <time>{formatTime(item.updated_at || item.created_at)}</time>
                  </div>
                  <p>{item.content}</p>
                  {!!item.tags?.length && <small>{item.tags.join(", ")}</small>}
                  {item.session_id && <small>Session {item.session_id}</small>}
                  {!!item.source_refs?.length && (
                    <small>
                      Source: {item.source_refs.map((ref) => ref.filename || `${ref.kind}:${ref.id}`).join(", ")}
                    </small>
                  )}
                  {!!item.conflict_ids?.length && <small>Conflicts: {item.conflict_ids.join(", ")}</small>}
                  {(item.status === "pending_confirm" || item.status === "conflicted") && (
                    <div className="memory-resolution-actions" aria-label="Resolve memory conflict">
                      <Button type="button" onClick={() => onResolve(item, "accept")}>Accept</Button>
                      <Button type="button" onClick={() => onResolve(item, "keep_both")}>Keep both</Button>
                      <Button type="button" className="danger" onClick={() => onResolve(item, "reject")}>Reject</Button>
                    </div>
                  )}
                  <div className="memory-feedback-actions" aria-label="Memory feedback">
                    <Button className="icon ghost" onClick={() => onFeedback(item, "important")} title="Mark memory as important" aria-label="Mark memory as important">
                      <Star size={15} />
                    </Button>
                    <Button className="icon ghost" onClick={() => onFeedback(item, "not_relevant")} title="Mark memory as less relevant" aria-label="Mark memory as less relevant">
                      <Archive size={15} />
                    </Button>
                    <Button className="icon ghost danger" onClick={() => onFeedback(item, "incorrect")} title="Mark memory as incorrect" aria-label="Mark memory as incorrect">
                      <X size={15} />
                    </Button>
                  </div>
                  <div className="memory-item-actions">
                    <Button className="icon ghost" onClick={() => startEdit(item)} title="Edit memory item" aria-label="Edit memory item">
                      <FileText size={16} />
                    </Button>
                    <Button className="icon ghost danger" onClick={() => onDelete(item)} title="Delete memory item" aria-label="Delete memory item">
                      <Trash2 size={16} />
                    </Button>
                  </div>
                </>
              )}
              </article>
            ))}
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}


function formatTime(value?: string): string {
  if (!value) return "";
  return new Intl.DateTimeFormat(undefined, { month: "short", day: "2-digit", hour: "2-digit", minute: "2-digit" }).format(new Date(value));
}
