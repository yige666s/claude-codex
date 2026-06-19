import { Fragment, ReactNode, Ref, RefObject } from "react";
import type { AgentActivity as AgentActivityState, Message } from "../../../../types";
import { AgentActivity } from "./AgentActivity";

type MessageListProps = {
  messages: Message[];
  userLabel: string;
  liveUserDraft: string;
  assistantDraft: string;
  agentActivity: AgentActivityState | null;
  highlightedMessageIndex: number | null;
  messagesRef: RefObject<HTMLDivElement | null>;
  renderMessage: (props: { message: Message; streaming?: boolean; highlighted?: boolean }) => ReactNode;
};

export function MessageList({
  messages,
  userLabel,
  liveUserDraft,
  assistantDraft,
  agentActivity,
  highlightedMessageIndex,
  messagesRef,
  renderMessage
}: MessageListProps) {
  const empty = !messages.length && !liveUserDraft && !assistantDraft && !agentActivity;
  return (
    <div className="messages" ref={messagesRef as Ref<HTMLDivElement>}>
      {empty && (
        <div className="empty-state">
          <div className="empty-orb" aria-hidden="true" />
          <h1>
            Hi {userLabel}
            <span>How can I help today?</span>
          </h1>
        </div>
      )}
      {messages.map((message, index) => (
        <Fragment key={`${message.created_at || index}-${index}`}>
          {renderMessage({
            message,
            highlighted: message.message_index !== undefined && message.message_index === highlightedMessageIndex
          })}
        </Fragment>
      ))}
      {liveUserDraft && renderMessage({ message: { role: "user", content: liveUserDraft }, streaming: true })}
      {agentActivity && <AgentActivity activity={agentActivity} />}
      {assistantDraft && renderMessage({ message: { role: "assistant", content: assistantDraft }, streaming: true })}
    </div>
  );
}
