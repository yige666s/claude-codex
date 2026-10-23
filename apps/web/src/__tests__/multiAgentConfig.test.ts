import { describe, expect, it } from "vitest";
import { llmConfigDraftFromConfig, llmConfigFromDraft } from "../admin/shared";

describe("DB-backed multi-agent configuration", () => {
  it("round-trips the auto-routing toggle without conflating it with loop triggers", () => {
    const draft = llmConfigDraftFromConfig({ multi_agent_auto_route_enabled: true, automatic_trigger_enabled: false });
    expect(draft.multi_agent_auto_route_enabled).toBe("true");
    expect(draft.automatic_trigger_enabled).toBe("false");
    const patch = llmConfigFromDraft(draft);
    expect(patch.multi_agent_auto_route_enabled).toBe(true);
    expect(patch.automatic_trigger_enabled).toBe(false);
  });
});
