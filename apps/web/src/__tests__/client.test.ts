import { describe, expect, it } from "vitest";
import { joinAPIURL } from "../api/client";

describe("joinAPIURL", () => {
  it("keeps same-origin paths relative when no base URL is configured", () => {
    expect(joinAPIURL("", "/v1/sessions")).toBe("/v1/sessions");
  });

  it("joins absolute API origins without duplicate slashes", () => {
    expect(joinAPIURL("https://api.example.com/", "/v1/sessions")).toBe("https://api.example.com/v1/sessions");
  });

  it("supports reverse-proxy subpaths", () => {
    expect(joinAPIURL("/agentapi", "readyz")).toBe("/agentapi/readyz");
  });
});
