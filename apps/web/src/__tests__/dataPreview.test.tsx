import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { DataPreview, isPreviewableTextAsset } from "../features/workspace/components/messages/DataPreview";

describe("DataPreview", () => {
  it("formats JSON data before rendering", () => {
    const html = renderToStaticMarkup(<DataPreview filename="result.json" contentType="application/json" text='{"ok":true,"items":[1,2]}' />);

    expect(html).toContain("<span>json</span>");
    expect(html).toContain("&quot;ok&quot;: true");
    expect(html).toContain("&quot;items&quot;: [");
  });

  it("renders CSV data as a table", () => {
    const html = renderToStaticMarkup(<DataPreview filename="scores.csv" contentType="text/csv" text={'name,score\n"Ada, L.",98\nLinus,91'} />);

    expect(html).toContain("<table>");
    expect(html).toContain("<th>name</th>");
    expect(html).toContain("<td>Ada, L.</td>");
    expect(html).toContain("<td>98</td>");
  });

  it("keeps HTML data as source text instead of rendering it", () => {
    const html = renderToStaticMarkup(<DataPreview filename="page.html" contentType="text/html" text="<script>alert(1)</script>" />);

    expect(html).toContain("<span>html</span>");
    expect(html).toContain("&lt;script&gt;alert(1)&lt;/script&gt;");
    expect(html).not.toContain("<script>");
  });

  it("normalizes raw Notion MCP view output before Markdown preview", () => {
    const text = `Here is the result of "view" for the Page with URL https://app.notion.com/p/example as of 2026-06-22T11:19:37Z:\\n<page url=\\"https://app.notion.com/p/example\\">\\n\\n{"title":"Memory System 技术设计文档"}\\n\\n# 1. 背景\\n\\n系统 LLM 存在 Context Window 限制。\\n</page>`;
    const html = renderToStaticMarkup(<DataPreview filename="Memory System 技术设计文档.md" contentType="text/markdown" text={text} />);

    expect(html).toContain("Memory System 技术设计文档");
    expect(html).toContain("1. 背景");
    expect(html).not.toContain("Here is the result");
    expect(html).not.toContain("&lt;page");
    expect(html).not.toContain("\\n");
  });

  it("detects text-previewable assets by content type and extension", () => {
    expect(isPreviewableTextAsset(asset("1", "data.json", "application/json"))).toBe(true);
    expect(isPreviewableTextAsset(asset("2", "table.tsv", ""))).toBe(true);
    expect(isPreviewableTextAsset(asset("3", "image.png", "image/png"))).toBe(false);
  });
});

function asset(id: string, filename: string, contentType: string) {
  return {
    id,
    kind: "artifact" as const,
    filename,
    content_type: contentType,
    size_bytes: 10,
    created_at: "2026-06-08T00:00:00Z"
  };
}
