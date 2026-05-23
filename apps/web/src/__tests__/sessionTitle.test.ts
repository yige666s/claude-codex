import { describe, expect, it } from "vitest";
import { firstUserText, sessionTitle, truncateOneLine } from "../lib/sessionTitle";
import type { Session } from "../types";

function session(messages: Session["messages"]): Session {
  return {
    id: "20260508T120000Z-demo",
    working_dir: "/tmp",
    started_at: new Date().toISOString(),
    updated_at: new Date().toISOString(),
    messages
  };
}

describe("session titles", () => {
  it("uses the first visible user message", () => {
    const title = sessionTitle(session([
      { role: "user", content: "hidden setup", hidden: true },
      { role: "assistant", content: "hello" },
      { role: "user", content: "Generate a launch poster" }
    ]));
    expect(title).toBe("Generate a launch poster");
  });

  it("collapses whitespace and truncates one line", () => {
    expect(truncateOneLine("make\n\nan image   with a very very very very long instruction", 24)).toBe("make an image with a...");
  });

  it("leaves empty sessions untitled instead of showing the id", () => {
    const item = session([{ role: "assistant", content: "ready" }]);
    expect(firstUserText(item)).toBe("");
    expect(sessionTitle(item)).toBe("");
  });

  it("uses the stored title when list rows omit messages", () => {
    const item = session(undefined);
    item.title = "Generate project architecture";
    expect(sessionTitle(item)).toBe("Generate project architecture");
  });
});
