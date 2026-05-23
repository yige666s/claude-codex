import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { MarkdownContent } from "../features/workspace/components/messages/MarkdownContent";

describe("MarkdownContent", () => {
  it("keeps ordered list numbering when items are separated by blank lines", () => {
    const html = renderToStaticMarkup(
      <MarkdownContent
        text={[
          "1. CPU registers",
          "",
          "1. CPU cache",
          "",
          "1. Main memory",
          "",
          "1. Secondary storage"
        ].join("\n")}
      />
    );

    expect(html.match(/<ol(?:\s|>)/g)).toHaveLength(1);
    expect(html.match(/<li>/g)).toHaveLength(4);
  });

  it("preserves ordered list starts after intervening bullet lists", () => {
    const html = renderToStaticMarkup(
      <MarkdownContent
        text={[
          "1. Market context",
          "2. Product advantages",
          "",
          "- Multi-provider models",
          "- Automation hooks",
          "",
          "3. Business model",
          "",
          "- Subscription plus usage",
          "",
          "4. Target markets"
        ].join("\n")}
      />
    );

    expect(html).toContain("<ol start=\"3\">");
    expect(html).toContain("<ol start=\"4\">");
  });
});
