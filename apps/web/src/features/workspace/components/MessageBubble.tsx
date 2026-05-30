import { useEffect, useState } from "react";
import { Check, Copy } from "lucide-react";
import { Button } from "../../../components/ui/button";
import type { ApiClient } from "../../../api/client";
import { MarkdownContent } from "./messages/MarkdownContent";
import { MessageAssetAttachmentPreview, MessageAttachmentPreview, splitAttachedTextSections } from "./messages/MessageAttachmentPreview";
import type { Message } from "../../../types";

type MessageBubbleProps = {
  message: Message;
  api: ApiClient;
  streaming?: boolean;
  highlighted?: boolean;
};

export function MessageBubble({
  message,
  api,
  streaming = false,
  highlighted = false
}: MessageBubbleProps) {
  const [copied, setCopied] = useState(false);
  const text = message.content || message.tool_output || "";
  const attachments = message.attachments || [];
  const rendered = splitAttachedTextSections(stripAttachmentSummary(text, message.role === "user"));
  const visibleAttachments = attachments.filter((attachment) => !rendered.attachments.some((item) => item.filename === (attachment.file_name || attachment.id)));
  const isAssistant = message.role !== "user";

  useEffect(() => {
    if (!copied) return;
    const timer = window.setTimeout(() => setCopied(false), 1000);
    return () => window.clearTimeout(timer);
  }, [copied]);

  async function copyMessage() {
    if (copied || !text) return;
    try {
      await navigator.clipboard.writeText(text);
      setCopied(true);
    } catch {
      const textarea = document.createElement("textarea");
      textarea.value = text;
      textarea.style.position = "fixed";
      textarea.style.opacity = "0";
      document.body.appendChild(textarea);
      textarea.select();
      document.execCommand("copy");
      textarea.remove();
      setCopied(true);
    }
  }

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
      {visibleAttachments.length > 0 && (
        <div className="message-attachment-previews">
          {visibleAttachments.map((attachment) => (
            <MessageAssetAttachmentPreview key={attachment.id} attachment={attachment} api={api} />
          ))}
        </div>
      )}
      {isAssistant && !streaming && (
        <div className="message-actions" aria-label="Assistant message actions">
          <Button className={`message-action-button ${copied ? "copied" : ""}`} variant="ghost" size="icon" onClick={copyMessage} disabled={copied || !text} title={copied ? "Copied" : "Copy"} aria-label={copied ? "Copied" : "Copy"}>
            {copied ? <Check size={17} /> : <Copy size={17} />}
          </Button>
        </div>
      )}
    </article>
  );
}

function stripAttachmentSummary(text: string, shouldStrip: boolean): string {
  if (!shouldStrip) return text;
  return text.replace(/\n{2}(?:Attachments|Attached files):[^\n]+$/i, "").trim();
}
