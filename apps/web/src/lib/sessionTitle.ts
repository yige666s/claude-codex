import type { Session } from "../types";

const MAX_TITLE_LENGTH = 42;

export function firstUserText(session: Session): string {
  return (session.messages || [])
    .find((message) => message.role === "user" && !message.hidden && message.content?.trim())
    ?.content?.trim() || "";
}

export function sessionTitle(session: Session): string {
  const source = firstUserText(session) || session.description || session.id;
  return truncateOneLine(source, MAX_TITLE_LENGTH);
}

export function truncateOneLine(value: string, max = MAX_TITLE_LENGTH): string {
  const normalized = value.replace(/\s+/g, " ").trim();
  if (normalized.length <= max) return normalized;
  return normalized.slice(0, Math.max(0, max - 3)).trimEnd() + "...";
}
