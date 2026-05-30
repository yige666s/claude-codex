import { useEffect, useState } from "react";
import { FileImage, FileText } from "lucide-react";
import { Button } from "../../../../components/ui/button";
import type { ApiClient } from "../../../../api/client";
import type { MessageAttachment } from "../../../../types";

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

export function MessageAssetAttachmentPreview({ attachment, api }: { attachment: MessageAttachment; api: ApiClient }) {
  const [imageURL, setImageURL] = useState("");
  const image = isImageAttachment(attachment);
  const filename = attachment.file_name || attachment.id;
  const mimeType = attachment.mime_type || attachment.file_type || "file";

  useEffect(() => {
    if (!image) return;
    let active = true;
    let objectURL = "";
    api.attachmentBlob(attachment.id)
      .then((blob) => {
        if (!active) return;
        objectURL = URL.createObjectURL(blob);
        setImageURL(objectURL);
      })
      .catch(() => {
        if (active) setImageURL("");
      });
    return () => {
      active = false;
      if (objectURL) URL.revokeObjectURL(objectURL);
    };
  }, [api, attachment.id, image]);

  if (image) {
    return (
      <section className="message-asset-attachment image">
        {imageURL ? <img src={imageURL} alt={filename} /> : <div className="message-image-placeholder"><FileImage size={18} /></div>}
        <div>
          <strong>{filename}</strong>
          <small>{mimeType}</small>
        </div>
      </section>
    );
  }

  return (
    <section className="message-asset-attachment file">
      <FileText size={15} />
      <span>
        <strong>{filename}</strong>
        <small>{mimeType}</small>
      </span>
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

function isImageAttachment(attachment: MessageAttachment): boolean {
  return attachment.file_type === "image" || (attachment.mime_type || "").toLowerCase().startsWith("image/");
}
