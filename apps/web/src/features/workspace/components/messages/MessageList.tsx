import { Fragment, ReactNode, Ref, RefObject } from "react";
import { Sparkles } from "lucide-react";
import type { Message } from "../../../../types";

type MessageListProps = {
  messages: Message[];
  liveUserDraft: string;
  assistantDraft: string;
  highlightedMessageIndex: number | null;
  messagesRef: RefObject<HTMLDivElement | null>;
  renderMessage: (props: { message: Message; streaming?: boolean; highlighted?: boolean }) => ReactNode;
};

export function MessageList({
  messages,
  liveUserDraft,
  assistantDraft,
  highlightedMessageIndex,
  messagesRef,
  renderMessage
}: MessageListProps) {
  const empty = !messages.length && !liveUserDraft && !assistantDraft;
  return (
    <div className="messages" ref={messagesRef as Ref<HTMLDivElement>}>
      {empty && (
        <div className="empty-state">
          <div className="empty-orb" aria-hidden="true" />
          <h1>
            Good Morning
            <span>How Can I Assist You Today?</span>
          </h1>
          <div className="empty-prompt-card" aria-hidden="true">
            <Sparkles size={16} />
            <span>Initiate a query or send a command to the AI...</span>
          </div>
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
      {assistantDraft && renderMessage({ message: { role: "assistant", content: assistantDraft }, streaming: true })}
    </div>
  );
}
