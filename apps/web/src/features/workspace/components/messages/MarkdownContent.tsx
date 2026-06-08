import ReactMarkdown, { type Components } from "react-markdown";
import rehypeKatex from "rehype-katex";
import remarkBreaks from "remark-breaks";
import remarkGfm from "remark-gfm";
import remarkMath from "remark-math";
import "katex/dist/katex.min.css";

const markdownComponents: Components = {
  h1: ({ children }) => <h2>{children}</h2>,
  h2: ({ children }) => <h3>{children}</h3>,
  h3: ({ children }) => <h4>{children}</h4>,
  h4: ({ children }) => <h5>{children}</h5>,
  h5: ({ children }) => <h6>{children}</h6>,
  h6: ({ children }) => <h6>{children}</h6>,
  a: ({ href, children, ...props }) => {
    const safeHref = typeof href === "string" && isSafeHref(href) ? href : "";
    if (!safeHref) return <span>{children}</span>;
    return (
      <a {...props} href={safeHref} target="_blank" rel="noreferrer">
        {children}
      </a>
    );
  },
  pre: ({ children }) => <pre className="markdown-code">{children}</pre>,
  code: ({ className, children, ...props }) => {
    const language = languageFromClassName(className);
    if (!language) return <code {...props}>{children}</code>;
    return (
      <>
        <span>{language}</span>
        <code {...props}>{children}</code>
      </>
    );
  },
  table: ({ children }) => (
    <div className="markdown-table-wrap">
      <table>{children}</table>
    </div>
  )
};

export function MarkdownContent({ text }: { text: string }) {
  if (!text.trim()) return <div className="message-text" />;
  return (
    <div className="markdown-content">
      <ReactMarkdown
        components={markdownComponents}
        rehypePlugins={[rehypeKatex]}
        remarkPlugins={[remarkGfm, [remarkMath, { singleDollarTextMath: true }], remarkBreaks]}
        skipHtml
      >
        {text}
      </ReactMarkdown>
    </div>
  );
}

function languageFromClassName(className?: string): string {
  const match = /(?:^|\s)language-([^\s]+)/.exec(className || "");
  return match?.[1] || "";
}

function isSafeHref(href: string): boolean {
  const trimmed = href.trim();
  if (trimmed.startsWith("#") || trimmed.startsWith("/")) return true;
  try {
    const parsed = new URL(trimmed);
    return ["http:", "https:", "mailto:"].includes(parsed.protocol);
  } catch {
    return false;
  }
}
