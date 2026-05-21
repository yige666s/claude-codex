import { useEffect, useMemo, useState } from "react";
import type { Session } from "../../../../types";
import { SessionRow } from "./SessionRow";

const sessionPageSize = 10;

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
  const [visibleCount, setVisibleCount] = useState(sessionPageSize);
  const visibleSessions = useMemo(() => sessions.slice(0, visibleCount), [sessions, visibleCount]);

  useEffect(() => {
    setVisibleCount(sessionPageSize);
  }, [sessions.length]);

  return (
    <div
      className="session-list"
      aria-label="Sessions"
      onScroll={(event) => {
        const node = event.currentTarget;
        if (visibleCount >= sessions.length) return;
        if (node.scrollTop + node.clientHeight >= node.scrollHeight - 60) {
          setVisibleCount((count) => Math.min(sessions.length, count + sessionPageSize));
        }
      }}
    >
      {visibleSessions.map((session) => (
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
