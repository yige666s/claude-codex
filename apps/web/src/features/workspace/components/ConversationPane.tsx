import { ReactNode, RefObject } from "react";
import { AlertCircle, Menu } from "lucide-react";
import { Button } from "../../../components/ui/button";
import type { Message, Session } from "../../../types";
import { sessionTitle } from "../../../lib/sessionTitle";
import type { Status } from "../workspaceTypes";
import { MessageList } from "./messages/MessageList";

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
      <MessageList
        messages={messages}
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
