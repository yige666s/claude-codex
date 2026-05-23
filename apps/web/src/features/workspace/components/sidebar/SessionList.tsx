import { useEffect, useMemo, useState } from "react";
import type { Session } from "../../../../types";
import { sessionTitle } from "../../../../lib/sessionTitle";
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
  const titledSessions = useMemo(() => sessions.filter((session) => Boolean(sessionTitle(session))), [sessions]);
  const visibleSessions = useMemo(() => titledSessions.slice(0, visibleCount), [titledSessions, visibleCount]);

  useEffect(() => {
    setVisibleCount(sessionPageSize);
  }, [titledSessions.length]);

  return (
    <div
      className="session-list"
      aria-label="Sessions"
      onScroll={(event) => {
        const node = event.currentTarget;
        if (visibleCount >= titledSessions.length) return;
        if (node.scrollTop + node.clientHeight >= node.scrollHeight - 60) {
          setVisibleCount((count) => Math.min(titledSessions.length, count + sessionPageSize));
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
