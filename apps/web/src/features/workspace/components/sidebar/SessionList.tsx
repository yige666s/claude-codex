import type { Session } from "../../../../types";
import { SessionRow } from "./SessionRow";

type SessionListProps = {
  sessions: Session[];
  sessionId: string;
  onSelectSession: (id: string) => void;
  onRemoveSession: (id: string) => void;
};

export function SessionList({
  sessions,
  sessionId,
  onSelectSession,
  onRemoveSession
}: SessionListProps) {
  return (
    <div className="session-list" aria-label="Sessions">
      {sessions.map((session) => (
        <SessionRow
          key={session.id}
          session={session}
          active={session.id === sessionId}
          onSelect={onSelectSession}
          onRemove={onRemoveSession}
        />
      ))}
    </div>
  );
}
