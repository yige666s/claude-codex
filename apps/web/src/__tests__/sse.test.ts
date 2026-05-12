import { describe, expect, it } from "vitest";
import { parseSSEChunk } from "../lib/sse";

describe("parseSSEChunk", () => {
  it("parses typed runtime events", () => {
    const parsed = parseSSEChunk('id: evt-1\nevent: message\ndata: {"type":"message","role":"assistant","content":"hello"}');
    expect(parsed?.id).toBe("evt-1");
    expect(parsed?.event).toBe("message");
    expect(parsed?.data.content).toBe("hello");
  });

  it("ignores malformed json", () => {
    expect(parseSSEChunk("event: error\ndata: nope")).toBeNull();
  });
});
