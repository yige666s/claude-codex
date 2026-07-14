import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { MessageBubble } from "../features/workspace/components/MessageBubble";
import type { ApiClient } from "../api/client";

const api = {
  attachmentBlob: async () => new Blob()
} as unknown as ApiClient;

describe("MessageBubble", () => {
  it("hides generated attachment summaries for user messages", () => {
    const html = renderToStaticMarkup(
      <MessageBubble
        api={api}
        message={{
          role: "user",
          content: "总结一下这个文档\n\nAttached files: brief.md",
          attachments: [{ id: "asset-1", file_type: "text", mime_type: "text/markdown", file_name: "brief.md" }]
        }}
      />
    );

    expect(html).toContain("总结一下这个文档");
    expect(html).toContain("brief.md");
    expect(html).not.toContain("Attached files:");
  });

  it("renders image attachments in the same user bubble", () => {
    const html = renderToStaticMarkup(
      <MessageBubble
        api={api}
        message={{
          role: "user",
          content: "Please analyze the attached file(s).\n\nAttached files: images.jpeg",
          attachments: [{ id: "asset-2", file_type: "image", mime_type: "image/jpeg", file_name: "images.jpeg" }]
        }}
      />
    );

    expect(html).toContain("Please analyze the attached file(s).");
    expect(html).toContain("message-asset-attachment image");
    expect(html).toContain("images.jpeg");
    expect(html).not.toContain("Attached files:");
  });

  it("renders structured outputs on assistant messages", () => {
    const html = renderToStaticMarkup(
      <MessageBubble
        api={api}
        message={{
          role: "assistant",
          content: "",
          structured_outputs: [{
            id: "so-1",
            version: "agentapi_structured_output.v1",
            kind: "artifact_card",
            title: "Draft ready",
            summary: "Generated a project brief",
            artifact_refs: [{ id: "artifact-1", filename: "brief.md" }],
            actions: [{ id: "open", label: "Open", intent: "open_artifact" }]
          }]
        }}
      />
    );

    expect(html).toContain("Draft ready");
    expect(html).toContain("brief.md");
    expect(html).toContain("Open");
  });
});
