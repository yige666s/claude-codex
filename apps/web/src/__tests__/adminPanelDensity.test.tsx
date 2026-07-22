import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { ApiClient } from "../api/client";
import { AdminEvaluationPanel } from "../admin/panels/AdminEvaluationPanel";

describe("admin panel information density", () => {
  it("keeps advanced evaluation filters and runtime telemetry in progressive disclosure regions", () => {
    const html = renderToStaticMarkup(
      <AdminEvaluationPanel api={new ApiClient(() => undefined)} adminToken="" />
    );

    expect(html).toContain('<details class="evaluation-advanced-filters">');
    expect(html).toContain("Advanced filters");
    expect(html).toContain("IDs, model, prompt, experiment");
    expect(html).toContain('<details class="evaluation-runtime-metrics">');
    expect(html).toContain("Advanced runtime metrics");
    expect(html).toContain("Latency, reliability, token and RAGAS detail");
  });
});
