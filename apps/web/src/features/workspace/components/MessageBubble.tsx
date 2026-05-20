import { ReactNode, useState } from "react";
import { FileText } from "lucide-react";
import { Button } from "../../../components/ui/button";
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

type MarkdownBlock =
  | { type: "paragraph"; lines: string[] }
  | { type: "heading"; level: number; text: string }
  | { type: "code"; language: string; text: string }
  | { type: "list"; ordered: boolean; items: string[] }
  | { type: "quote"; lines: string[] }
  | { type: "table"; headers: string[]; rows: string[][] };

function MarkdownContent({ text }: { text: string }) {
  const blocks = parseMarkdown(text);
  if (blocks.length === 0) return <div className="message-text" />;
  return (
    <div className="markdown-content">
      {blocks.map((block, index) => renderMarkdownBlock(block, index))}
    </div>
  );
}

function parseMarkdown(text: string): MarkdownBlock[] {
  const lines = text.replace(/\r\n/g, "\n").split("\n");
  const blocks: MarkdownBlock[] = [];
  let paragraph: string[] = [];

  const flushParagraph = () => {
    const content = paragraph.join("\n").trim();
    if (content) blocks.push({ type: "paragraph", lines: paragraph });
    paragraph = [];
  };

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    const trimmed = line.trim();

    if (!trimmed) {
      flushParagraph();
      continue;
    }

    const fence = trimmed.match(/^```([\w+-]*)\s*$/);
    if (fence) {
      flushParagraph();
      const code: string[] = [];
      i++;
      for (; i < lines.length; i++) {
        if (lines[i].trim() === "```") break;
        code.push(lines[i]);
      }
      blocks.push({ type: "code", language: fence[1] || "", text: code.join("\n") });
      continue;
    }

    const heading = trimmed.match(/^(#{1,6})\s+(.+)$/);
    if (heading) {
      flushParagraph();
      blocks.push({ type: "heading", level: heading[1].length, text: heading[2].trim() });
      continue;
    }

    if (isTableStart(lines, i)) {
      flushParagraph();
      const headers = splitTableRow(lines[i]);
      const rows: string[][] = [];
      i += 2;
      for (; i < lines.length && lines[i].includes("|") && lines[i].trim(); i++) {
        rows.push(splitTableRow(lines[i]));
      }
      i--;
      blocks.push({ type: "table", headers, rows });
      continue;
    }

    const bullet = parseListItem(trimmed);
    if (bullet) {
      flushParagraph();
      const items = [bullet.text];
      const ordered = bullet.ordered;
      for (let j = i + 1; j < lines.length; j++) {
        const next = parseListItem(lines[j].trim());
        if (!next || next.ordered !== ordered) break;
        items.push(next.text);
        i = j;
      }
      blocks.push({ type: "list", ordered, items });
      continue;
    }

    if (trimmed.startsWith(">")) {
      flushParagraph();
      const quoteLines = [trimmed.replace(/^>\s?/, "")];
      for (let j = i + 1; j < lines.length; j++) {
        const next = lines[j].trim();
        if (!next.startsWith(">")) break;
        quoteLines.push(next.replace(/^>\s?/, ""));
        i = j;
      }
      blocks.push({ type: "quote", lines: quoteLines });
      continue;
    }

    paragraph.push(line);
  }

  flushParagraph();
  return blocks;
}

function renderMarkdownBlock(block: MarkdownBlock, index: number): ReactNode {
  switch (block.type) {
    case "heading": {
      const Heading = `h${Math.min(block.level + 1, 6)}` as keyof JSX.IntrinsicElements;
      return <Heading key={index}>{renderInlineMarkdown(block.text, `h-${index}`)}</Heading>;
    }
    case "code":
      return (
        <pre key={index} className="markdown-code">
          {block.language && <span>{block.language}</span>}
          <code>{block.text}</code>
        </pre>
      );
    case "list": {
      const List = block.ordered ? "ol" : "ul";
      return (
        <List key={index}>
          {block.items.map((item, itemIndex) => (
            <li key={itemIndex}>{renderInlineMarkdown(item, `li-${index}-${itemIndex}`)}</li>
          ))}
        </List>
      );
    }
    case "quote":
      return <blockquote key={index}>{renderInlineMarkdown(block.lines.join("\n"), `q-${index}`)}</blockquote>;
    case "table":
      return (
        <div key={index} className="markdown-table-wrap">
          <table>
            <thead>
              <tr>{block.headers.map((cell, cellIndex) => <th key={cellIndex}>{renderInlineMarkdown(cell, `th-${index}-${cellIndex}`)}</th>)}</tr>
            </thead>
            <tbody>
              {block.rows.map((row, rowIndex) => (
                <tr key={rowIndex}>{row.map((cell, cellIndex) => <td key={cellIndex}>{renderInlineMarkdown(cell, `td-${index}-${rowIndex}-${cellIndex}`)}</td>)}</tr>
              ))}
            </tbody>
          </table>
        </div>
      );
    case "paragraph":
    default:
      return <p key={index}>{renderInlineMarkdown(block.lines.join("\n"), `p-${index}`)}</p>;
  }
}

function parseListItem(line: string): { ordered: boolean; text: string } | null {
  const ordered = line.match(/^\d+[.)]\s+(.+)$/);
  if (ordered) return { ordered: true, text: ordered[1] };
  const bullet = line.match(/^[-*+]\s+(.+)$/);
  if (bullet) return { ordered: false, text: bullet[1] };
  return null;
}

function isTableStart(lines: string[], index: number): boolean {
  if (!lines[index]?.includes("|") || !lines[index + 1]?.includes("|")) return false;
  return splitTableRow(lines[index + 1]).every((cell) => /^:?-{3,}:?$/.test(cell.trim()));
}

function splitTableRow(line: string): string[] {
  return line.trim().replace(/^\|/, "").replace(/\|$/, "").split("|").map((cell) => cell.trim());
}

function renderInlineMarkdown(text: string, keyPrefix: string): ReactNode[] {
  const nodes: ReactNode[] = [];
  let index = 0;
  let key = 0;

  const pushText = (value: string) => {
    if (!value) return;
    const pieces = value.split("\n");
    pieces.forEach((piece, pieceIndex) => {
      if (piece) nodes.push(piece);
      if (pieceIndex < pieces.length - 1) nodes.push(<br key={`${keyPrefix}-br-${key++}`} />);
    });
  };

  while (index < text.length) {
    const candidates = [
      { marker: "`", pos: text.indexOf("`", index) },
      { marker: "**", pos: text.indexOf("**", index) },
      { marker: "*", pos: text.indexOf("*", index) },
      { marker: "[", pos: text.indexOf("[", index) }
    ].filter((candidate) => candidate.pos >= 0).sort((a, b) => a.pos - b.pos || b.marker.length - a.marker.length);

    const next = candidates[0];
    if (!next) {
      pushText(text.slice(index));
      break;
    }
    pushText(text.slice(index, next.pos));

    if (next.marker === "`") {
      const end = text.indexOf("`", next.pos + 1);
      if (end < 0) {
        pushText(text.slice(next.pos));
        break;
      }
      nodes.push(<code key={`${keyPrefix}-code-${key++}`}>{text.slice(next.pos + 1, end)}</code>);
      index = end + 1;
      continue;
    }

    if (next.marker === "**") {
      const end = text.indexOf("**", next.pos + 2);
      if (end < 0) {
        pushText(text.slice(next.pos));
        break;
      }
      nodes.push(<strong key={`${keyPrefix}-strong-${key++}`}>{renderInlineMarkdown(text.slice(next.pos + 2, end), `${keyPrefix}-strong-${key}`)}</strong>);
      index = end + 2;
      continue;
    }

    if (next.marker === "*") {
      if (text[next.pos + 1] === "*") {
        index = next.pos + 1;
        continue;
      }
      const end = text.indexOf("*", next.pos + 1);
      if (end < 0) {
        pushText(text.slice(next.pos));
        break;
      }
      nodes.push(<em key={`${keyPrefix}-em-${key++}`}>{renderInlineMarkdown(text.slice(next.pos + 1, end), `${keyPrefix}-em-${key}`)}</em>);
      index = end + 1;
      continue;
    }

    const labelEnd = text.indexOf("]", next.pos + 1);
    const urlStart = labelEnd >= 0 ? text.indexOf("(", labelEnd + 1) : -1;
    const urlEnd = urlStart >= 0 ? text.indexOf(")", urlStart + 1) : -1;
    if (labelEnd < 0 || urlStart !== labelEnd + 1 || urlEnd < 0) {
      pushText(text[next.pos]);
      index = next.pos + 1;
      continue;
    }
    const label = text.slice(next.pos + 1, labelEnd);
    const href = text.slice(urlStart + 1, urlEnd).trim();
    if (isSafeHref(href)) {
      nodes.push(
        <a key={`${keyPrefix}-a-${key++}`} href={href} target="_blank" rel="noreferrer">
          {renderInlineMarkdown(label, `${keyPrefix}-a-${key}`)}
        </a>
      );
    } else {
      pushText(text.slice(next.pos, urlEnd + 1));
    }
    index = urlEnd + 1;
  }

  return nodes;
}

function isSafeHref(href: string): boolean {
  return /^(https?:|mailto:)/i.test(href);
}

type AttachedTextSection = {
  filename: string;
  contentType: string;
  content: string;
};

function MessageAttachmentPreview({ attachment }: { attachment: AttachedTextSection }) {
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

function splitAttachedTextSections(text: string): { text: string; attachments: AttachedTextSection[] } {
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
