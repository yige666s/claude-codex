import { ReactNode, RefObject } from "react";
import { AlertCircle, Menu } from "lucide-react";
import { Button } from "../../../components/ui/button";
import type { Message, Project, Session } from "../../../types";
import { sessionTitle } from "../../../lib/sessionTitle";
import type { Status } from "../workspaceTypes";
import { MessageList } from "./messages/MessageList";

type RecoveryBanner = {
  tone: "busy" | "error";
  text: string;
} | null;

type ConversationPaneProps = {
  activeSession?: Session;
  activeProject?: Project;
  status: Status;
  recoveryBanner: RecoveryBanner;
  online: boolean;
  selectedJobId: string;
  userLabel: string;
  messages: Message[];
  liveUserDraft: string;
  assistantDraft: string;
  highlightedMessageIndex: number | null;
  messagesRef: RefObject<HTMLDivElement | null>;
  composer: ReactNode;
  statusLine: (status: Status) => ReactNode;
  messageBubble: (props: { message: Message; streaming?: boolean; highlighted?: boolean }) => ReactNode;
  onOpenMobileNav: () => void;
  onReconnectJob: () => void;
};

export function ConversationPane({
  activeSession,
  activeProject,
  status,
  recoveryBanner,
  online,
  selectedJobId,
  userLabel,
  messages,
  liveUserDraft,
  assistantDraft,
  highlightedMessageIndex,
  messagesRef,
  composer,
  statusLine,
  messageBubble,
  onOpenMobileNav,
  onReconnectJob
}: ConversationPaneProps) {
  const empty = !messages.length && !liveUserDraft && !assistantDraft;
  const title = activeSession ? sessionTitle(activeSession.messages?.length ? activeSession : { ...activeSession, messages }) : "";
  return (
    <section className={`workspace ${empty ? "empty-workspace" : ""}`}>
      <header className="topbar">
        <Button className="icon mobile-only" onClick={onOpenMobileNav} title="Open navigation" aria-label="Open navigation"><Menu size={20} /></Button>
        <div>
          <h2>{title}</h2>
          {activeProject && <small className="topbar-project-label">{activeProject.name}</small>}
          {statusLine(status)}
        </div>
      </header>
      {recoveryBanner && (
        <div className={`recovery-banner ${recoveryBanner.tone}`} role="status">
          <AlertCircle size={16} />
          <span>{recoveryBanner.text}</span>
          {online && selectedJobId && (
            <Button className="inline" onClick={onReconnectJob}>Reconnect</Button>
          )}
        </div>
      )}
      <MessageList
        messages={messages}
        userLabel={userLabel}
        liveUserDraft={liveUserDraft}
        assistantDraft={assistantDraft}
        highlightedMessageIndex={highlightedMessageIndex}
        messagesRef={messagesRef}
        renderMessage={messageBubble}
      />
      {composer}
    </section>
  );
}
