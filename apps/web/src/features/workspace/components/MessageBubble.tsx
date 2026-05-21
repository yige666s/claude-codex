import { MarkdownContent } from "./messages/MarkdownContent";
import { MessageAttachmentPreview, splitAttachedTextSections } from "./messages/MessageAttachmentPreview";
import type { Message } from "../../../types";

export function MessageBubble({ message, streaming = false, highlighted = false }: { message: Message; streaming?: boolean; highlighted?: boolean }) {
  const text = message.content || message.tool_output || "";
  const rendered = splitAttachedTextSections(text);
  const isAssistant = message.role !== "user";
  return (
    <article
      className={`message ${message.role === "user" ? "user" : "assistant"} ${highlighted ? "highlighted" : ""}`}
      data-message-index={message.message_index}
    >
      <div className="message-role">{message.role === "user" ? "You" : "Agent"}{streaming ? " ..." : ""}</div>
      {isAssistant ? <MarkdownContent text={rendered.text} /> : <div className="message-text">{rendered.text}</div>}
      {rendered.attachments.length > 0 && (
        <div className="message-attachment-previews">
          {rendered.attachments.map((attachment, index) => (
            <MessageAttachmentPreview key={`${attachment.filename}-${index}`} attachment={attachment} />
          ))}
        </div>
      )}
    </article>
  );
}
