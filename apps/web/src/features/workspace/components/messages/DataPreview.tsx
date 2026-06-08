import type { Asset } from "../../../../types";
import { MarkdownContent } from "./MarkdownContent";

type DataPreviewProps = {
  text: string;
  filename?: string;
  contentType?: string;
};

const codeExtensions = new Map([
  ["css", "css"],
  ["env", "env"],
  ["go", "go"],
  ["h", "c"],
  ["html", "html"],
  ["htm", "html"],
  ["ini", "ini"],
  ["java", "java"],
  ["js", "javascript"],
  ["jsx", "jsx"],
  ["log", "log"],
  ["py", "python"],
  ["sh", "shell"],
  ["sql", "sql"],
  ["toml", "toml"],
  ["ts", "typescript"],
  ["tsx", "tsx"],
  ["xml", "xml"],
  ["yaml", "yaml"],
  ["yml", "yaml"]
]);

const textExtensions = new Set([
  "txt",
  "md",
  "markdown",
  "csv",
  "tsv",
  "json",
  "jsonl",
  ...codeExtensions.keys()
]);

export function DataPreview({ text, filename = "", contentType = "" }: DataPreviewProps) {
  const format = detectDataFormat(filename, contentType);
  if (format.kind === "markdown") return <MarkdownContent text={text} />;
  if (format.kind === "json") return <CodePreview text={formatJson(text)} language="json" />;
  if (format.kind === "table") return <DelimitedTablePreview text={text} delimiter={format.delimiter} />;
  if (format.kind === "code") return <CodePreview text={text} language={format.language} />;
  return <pre className="data-preview-plain">{text}</pre>;
}

export function isPreviewableTextAsset(asset: Asset): boolean {
  const contentType = normalizedContentType(asset.content_type);
  const ext = extensionFromFilename(asset.filename);
  return (
    contentType.startsWith("text/") ||
    contentType === "application/json" ||
    contentType === "application/x-ndjson" ||
    contentType === "application/xml" ||
    textExtensions.has(ext)
  );
}

function CodePreview({ text, language }: { text: string; language: string }) {
  return (
    <pre className="data-preview-code">
      <span>{language}</span>
      <code>{text}</code>
    </pre>
  );
}

function DelimitedTablePreview({ text, delimiter }: { text: string; delimiter: "," | "\t" }) {
  const rows = parseDelimitedRows(text, delimiter);
  if (!rows.length) return <pre className="data-preview-plain" />;
  const [head, ...body] = rows;
  const visibleBody = body.slice(0, 200);
  const hiddenRows = Math.max(0, body.length - visibleBody.length);

  return (
    <div className="data-preview-table-wrap">
      <table>
        <thead>
          <tr>
            {head.map((cell, index) => (
              <th key={index}>{cell}</th>
            ))}
          </tr>
        </thead>
        <tbody>
          {visibleBody.map((row, rowIndex) => (
            <tr key={rowIndex}>
              {head.map((_, cellIndex) => (
                <td key={cellIndex}>{row[cellIndex] || ""}</td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
      {hiddenRows > 0 && <div className="data-preview-truncated">Showing first 200 rows, {hiddenRows} hidden.</div>}
    </div>
  );
}

function detectDataFormat(filename: string, contentType: string):
  | { kind: "markdown" }
  | { kind: "json" }
  | { kind: "table"; delimiter: "," | "\t" }
  | { kind: "code"; language: string }
  | { kind: "plain" } {
  const normalizedType = normalizedContentType(contentType);
  const ext = extensionFromFilename(filename);

  if (normalizedType === "text/markdown" || ext === "md" || ext === "markdown") return { kind: "markdown" };
  if (normalizedType === "application/json" || ext === "json") return { kind: "json" };
  if (normalizedType === "application/x-ndjson" || ext === "jsonl") return { kind: "code", language: "jsonl" };
  if (normalizedType === "text/csv" || ext === "csv") return { kind: "table", delimiter: "," };
  if (normalizedType === "text/tab-separated-values" || ext === "tsv") return { kind: "table", delimiter: "\t" };
  if (normalizedType === "application/xml") return { kind: "code", language: "xml" };

  const codeLanguage = codeExtensions.get(ext);
  if (codeLanguage) return { kind: "code", language: codeLanguage };
  return { kind: "plain" };
}

function formatJson(text: string): string {
  try {
    return JSON.stringify(JSON.parse(text), null, 2);
  } catch {
    return text;
  }
}

function parseDelimitedRows(text: string, delimiter: "," | "\t"): string[][] {
  const rows: string[][] = [];
  let row: string[] = [];
  let cell = "";
  let inQuotes = false;

  for (let index = 0; index < text.length; index += 1) {
    const char = text[index];
    const next = text[index + 1];
    if (char === '"' && inQuotes && next === '"') {
      cell += '"';
      index += 1;
      continue;
    }
    if (char === '"') {
      inQuotes = !inQuotes;
      continue;
    }
    if (char === delimiter && !inQuotes) {
      row.push(cell);
      cell = "";
      continue;
    }
    if ((char === "\n" || char === "\r") && !inQuotes) {
      if (char === "\r" && next === "\n") index += 1;
      row.push(cell);
      if (row.some((value) => value.length > 0)) rows.push(row);
      row = [];
      cell = "";
      continue;
    }
    cell += char;
  }

  row.push(cell);
  if (row.some((value) => value.length > 0)) rows.push(row);
  return rows;
}

function normalizedContentType(contentType?: string): string {
  return (contentType || "").toLowerCase().split(";")[0].trim();
}

function extensionFromFilename(filename?: string): string {
  return filename?.split(".").pop()?.toLowerCase() || "";
}
