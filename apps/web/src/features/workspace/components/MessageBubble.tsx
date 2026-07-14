import { useEffect, useState } from "react";
import { Check, Copy } from "lucide-react";
import { Button } from "../../../components/ui/button";
import type { ApiClient } from "../../../api/client";
import { MarkdownContent } from "./messages/MarkdownContent";
import { MessageAssetAttachmentPreview, MessageAttachmentPreview, splitAttachedTextSections } from "./messages/MessageAttachmentPreview";
import type { Message, StructuredOutput } from "../../../types";

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
      {message.structured_outputs && message.structured_outputs.length > 0 && (
        <div className="message-structured-outputs">
          {message.structured_outputs.map((output, index) => (
            <StructuredOutputCard key={output.id || `${output.kind || "structured"}-${index}`} output={output} />
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

function StructuredOutputCard({ output }: { output: StructuredOutput }) {
  const kind = String(output.kind || "card");
  const title = String(output.title || output.summary || structuredOutputKindLabel(kind));
  const summary = output.summary && output.summary !== title ? String(output.summary) : "";
  const actions = Array.isArray(output.actions) ? output.actions.slice(0, 4) : [];
  const artifacts = Array.isArray(output.artifact_refs) ? output.artifact_refs.slice(0, 3) : [];
  return (
    <section className={`message-structured-output ${kind.replace(/[^a-z0-9_-]/gi, "-")}`}>
      <header>
        <span>{structuredOutputKindLabel(kind)}</span>
        <strong>{title}</strong>
      </header>
      {summary && <p>{summary}</p>}
      {artifacts.length > 0 && (
        <div className="message-structured-output-list">
          {artifacts.map((artifact, index) => (
            <span key={String(artifact.id || artifact.artifact_id || index)}>
              {String(artifact.filename || artifact.name || artifact.id || artifact.artifact_id || "artifact")}
            </span>
          ))}
        </div>
      )}
      {actions.length > 0 && (
        <div className="message-structured-output-actions">
          {actions.map((action, index) => (
            <button key={String(action.id || index)} type="button" disabled title={String(action.intent || "")}>
              {String(action.label || action.intent || "Action")}
            </button>
          ))}
        </div>
      )}
    </section>
  );
}

function structuredOutputKindLabel(kind: string): string {
  switch (kind) {
    case "artifact_card":
      return "Artifact";
    case "choice_set":
      return "Choices";
    case "progress":
      return "Progress";
    case "diagnostic":
      return "Diagnostic";
    default:
      return "Card";
  }
}

function stripAttachmentSummary(text: string, shouldStrip: boolean): string {
  if (!shouldStrip) return text;
  return text.replace(/\n{2}(?:Attachments|Attached files):[^\n]+$/i, "").trim();
}
