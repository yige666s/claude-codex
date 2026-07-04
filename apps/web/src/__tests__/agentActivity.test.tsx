import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { AgentActivity } from "../features/workspace/components/messages/AgentActivity";

describe("AgentActivity", () => {
  it("renders cancelled traces as stopped instead of running", () => {
    const html = renderToStaticMarkup(
      <AgentActivity
        activity={{
          session_id: "session-1",
          running: false,
          items: [{
            id: "cancelled",
            type: "cancelled",
            channel: "notice",
            title: "Request cancelled",
            status: "cancelled",
            created_at: "2026-07-03T00:00:00Z"
          }]
        }}
      />
    );

    expect(html).toContain("Request cancelled");
    expect(html).toContain("Cancelled · 1 step");
    expect(html).not.toContain("Running · 1 step");
  });

  it("uses terminal events to stop running display", () => {
    const html = renderToStaticMarkup(
      <AgentActivity
        activity={{
          session_id: "session-1",
          running: true,
          items: [{
            id: "start",
            type: "start",
            channel: "thinking",
            title: "Generating response",
            status: "running",
            created_at: "2026-07-03T00:00:00Z"
          }, {
            id: "done",
            type: "done",
            channel: "thinking",
            title: "Response completed",
            status: "succeeded",
            created_at: "2026-07-03T00:00:01Z"
          }]
        }}
      />
    );

    expect(html).toContain("Completed · 2 steps");
    expect(html).toContain("Response completed");
    expect(html).not.toContain("Running · 2 steps");
  });

  it("separates folded thinking progress from visible tool status", () => {
    const html = renderToStaticMarkup(
      <AgentActivity
        activity={{
          session_id: "session-1",
          running: true,
          items: [{
            id: "start",
            type: "start",
            channel: "thinking",
            title: "Generating response",
            status: "running",
            created_at: "2026-07-03T00:00:00Z"
          }, {
            id: "tool",
            type: "live_tool_start",
            channel: "tool",
            title: "Using tool",
            detail: "web search",
            status: "running",
            created_at: "2026-07-03T00:00:01Z"
          }]
        }}
      />
    );

    expect(html).toContain("Thinking");
    expect(html).toContain("Thinking progress");
    expect(html).toContain("Tool activity");
    expect(html).toContain("agent-thinking-panel");
    expect(html).toContain("agent-tool-row");
    expect(html).toContain("Generating response");
    expect(html).toContain("Using tool");
    expect(html).toContain("web search");
  });

  it("renders a trace collapse control when details are available", () => {
    const html = renderToStaticMarkup(
      <AgentActivity
        activity={{
          session_id: "session-1",
          running: true,
          items: [{
            id: "tool",
            type: "live_tool_start",
            channel: "tool",
            title: "Using tool",
            detail: "a very long tool prompt",
            status: "running",
            created_at: "2026-07-03T00:00:01Z"
          }]
        }}
      />
    );

    expect(html).toContain("agent-activity-collapse");
    expect(html).toContain("aria-expanded=\"true\"");
    expect(html).toContain("Collapse thinking trace");
  });
});
