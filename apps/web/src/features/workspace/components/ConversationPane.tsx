import { Fragment, ReactNode, Ref, RefObject } from "react";
import { AlertCircle, Menu } from "lucide-react";
import { Button } from "../../../components/ui/button";
import type { Message, Session } from "../../../types";
import { sessionTitle } from "../../../lib/sessionTitle";
import type { Status } from "../workspaceTypes";

type RecoveryBanner = {
  tone: "busy" | "error";
  text: string;
} | null;

type ConversationPaneProps = {
  activeSession?: Session;
  status: Status;
  recoveryBanner: RecoveryBanner;
  online: boolean;
  selectedJobId: string;
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
  status,
  recoveryBanner,
  online,
  selectedJobId,
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
  return (
    <section className="workspace">
      <header className="topbar">
        <Button className="icon mobile-only" onClick={onOpenMobileNav} title="Open navigation" aria-label="Open navigation"><Menu size={20} /></Button>
        <div>
          <h2>{activeSession ? sessionTitle(activeSession) : "New conversation"}</h2>
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
      <div className="messages" ref={messagesRef as Ref<HTMLDivElement>}>
        {!messages.length && !liveUserDraft && !assistantDraft && <div className="empty-state">Start with a message or choose a skill from the right panel.</div>}
        {messages.map((message, index) => (
          <Fragment key={`${message.created_at || index}-${index}`}>
            {messageBubble({
              message,
              highlighted: message.message_index !== undefined && message.message_index === highlightedMessageIndex
            })}
          </Fragment>
        ))}
        {liveUserDraft && messageBubble({ message: { role: "user", content: liveUserDraft }, streaming: true })}
        {assistantDraft && messageBubble({ message: { role: "assistant", content: assistantDraft }, streaming: true })}
      </div>
      {composer}
    </section>
  );
}
