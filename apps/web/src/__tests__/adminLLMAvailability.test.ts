import { describe, expect, it } from "vitest";
import { modelAvailabilityFor } from "../admin/shared";
import type { LLMGovernanceConfig } from "../types";

describe("admin LLM model availability", () => {
  const config: LLMGovernanceConfig = {
    model: "simple",
    model_availability: [
      { id: "simple", provider: "simple", available: true },
      {
        id: "deepseek-chat",
        provider: "deepseek",
        available: false,
        reason: "set DEEPSEEK_API_KEY or AGENT_API_LLM_API_KEY and recreate AgentAPI"
      }
    ]
  };

  it("returns the backend readiness result for the selected model", () => {
    expect(modelAvailabilityFor(config, "deepseek-chat")).toEqual(config.model_availability?.[1]);
  });

  it("treats catalogs without readiness metadata as backward-compatible", () => {
    expect(modelAvailabilityFor({ model: "simple" }, "simple")).toBeUndefined();
  });
});
