import { Trash2 } from "lucide-react";
import { Button } from "../../../../components/ui/button";
import type { Session } from "../../../../types";
import { sessionTitle } from "../../../../lib/sessionTitle";

type SessionRowProps = {
  session: Session;
  active: boolean;
  onSelect: (id: string) => void;
  onRemove: (id: string) => void;
};

export function SessionRow({ session, active, onSelect, onRemove }: SessionRowProps) {
  const title = sessionTitle(session);
  return (
    <div className={`session-list-item ${active ? "active" : ""}`}>
      <Button className="session-select" variant="ghost" onClick={() => onSelect(session.id)} title={title}>
        <span>{title}</span>
      </Button>
      <Button
        className="session-delete"
        variant="ghost"
        size="icon"
        onClick={() => onRemove(session.id)}
        title="Delete session"
        aria-label={`Delete ${title}`}
      >
        <Trash2 size={16} />
      </Button>
    </div>
  );
}
