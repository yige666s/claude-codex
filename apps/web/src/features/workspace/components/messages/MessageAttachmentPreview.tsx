import { useState } from "react";
import { FileText } from "lucide-react";
import { Button } from "../../../../components/ui/button";

export type AttachedTextSection = {
  filename: string;
  contentType: string;
  content: string;
};

export function MessageAttachmentPreview({ attachment }: { attachment: AttachedTextSection }) {
  const [expanded, setExpanded] = useState(false);
  return (
    <section className={`message-attachment-preview ${expanded ? "expanded" : ""}`}>
      <Button type="button" onClick={() => setExpanded((value) => !value)} aria-expanded={expanded}>
        <FileText size={15} />
        <span>
          <strong>{attachment.filename}</strong>
          <small>{attachment.contentType || "text"} · {expanded ? "Hide content" : "Show content"}</small>
        </span>
      </Button>
      {expanded && (
        <pre>{attachment.content}</pre>
      )}
    </section>
  );
}

export function splitAttachedTextSections(text: string): { text: string; attachments: AttachedTextSection[] } {
  const attachments: AttachedTextSection[] = [];
  const pattern = /(?:\n{2}|^)Attached text file: ([^\n]+)\nContent-Type: ([^\n]+)\n\n```text\n([\s\S]*?)\n```/g;
  const cleaned = text.replace(pattern, (_match, filename: string, contentType: string, content: string) => {
    attachments.push({
      filename: filename.trim(),
      contentType: contentType.trim(),
      content
    });
    return "";
  }).trim();
  return { text: cleaned, attachments };
}
