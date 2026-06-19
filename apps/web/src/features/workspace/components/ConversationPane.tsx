import { ReactNode, RefObject } from "react";
import { AlertCircle, Menu } from "lucide-react";
import { Button } from "../../../components/ui/button";
import type { AgentActivity, Message, Session } from "../../../types";
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
  userLabel: string;
  messages: Message[];
  liveUserDraft: string;
  assistantDraft: string;
  agentActivity: AgentActivity | null;
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
  userLabel,
  messages,
  liveUserDraft,
  assistantDraft,
  agentActivity,
  highlightedMessageIndex,
  messagesRef,
  composer,
  statusLine,
  messageBubble,
  onOpenMobileNav,
  onReconnectJob
}: ConversationPaneProps) {
  const empty = !messages.length && !liveUserDraft && !assistantDraft && !agentActivity;
  const title = activeSession ? sessionTitle(activeSession.messages?.length ? activeSession : { ...activeSession, messages }) : "";
  return (
    <section className={`workspace ${empty ? "empty-workspace" : ""}`}>
      <header className="topbar">
        <Button className="icon mobile-only" onClick={onOpenMobileNav} title="Open navigation" aria-label="Open navigation"><Menu size={20} /></Button>
        <div>
          <h2>{title}</h2>
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
        agentActivity={agentActivity}
        highlightedMessageIndex={highlightedMessageIndex}
        messagesRef={messagesRef}
        renderMessage={messageBubble}
      />
      {composer}
    </section>
  );
}
