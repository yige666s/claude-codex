import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ApiClient, joinAPIURL } from "../api/client";
import type { AuthSession } from "../types";

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

describe("ApiClient auth refresh", () => {
  beforeEach(() => {
    vi.stubGlobal("localStorage", memoryStorage());
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("coalesces concurrent access refreshes", async () => {
    localStorage.setItem("agentapi.web.auth", JSON.stringify(authSession("old-access", "old-refresh", Date.now() + 1_000)));
    let refreshCalls = 0;
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input);
      if (url === "/v1/auth/refresh") {
        refreshCalls++;
        await Promise.resolve();
        return jsonResponse(authSession("new-access", "new-refresh", Date.now() + 900_000));
      }
      if (url === "/v1/sessions?limit=50&summary=1") return jsonResponse([]);
      return jsonResponse({ error: "not found" }, 404);
    });
    vi.stubGlobal("fetch", fetchMock);

    const api = new ApiClient(vi.fn());
    await Promise.all([api.sessions(), api.sessions()]);

    expect(refreshCalls).toBe(1);
    expect(fetchMock).toHaveBeenCalledTimes(3);
    expect(api.session()?.access_token).toBe("new-access");
  });

  it("refreshes and retries after an access token 401", async () => {
    localStorage.setItem("agentapi.web.auth", JSON.stringify(authSession("expired-access", "refresh", Date.now() + 900_000)));
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url === "/v1/sessions?limit=50&summary=1" && fetchMock.mock.calls.filter(([callInput]) => String(callInput) === "/v1/sessions?limit=50&summary=1").length === 1) {
        return jsonResponse({ error: "token expired" }, 401);
      }
      if (url === "/v1/auth/refresh") return jsonResponse(authSession("fresh-access", "fresh-refresh", Date.now() + 900_000));
      if (url === "/v1/sessions?limit=50&summary=1") {
        expect(new Headers(init?.headers).get("Authorization")).toBe("Bearer fresh-access");
        return jsonResponse([]);
      }
      return jsonResponse({ error: "not found" }, 404);
    });
    vi.stubGlobal("fetch", fetchMock);

    const api = new ApiClient(vi.fn());
    await expect(api.sessions()).resolves.toEqual([]);
    expect(api.session()?.refresh_token).toBe("fresh-refresh");
  });

  it("does not put access tokens in browser stream URLs", () => {
    localStorage.setItem("agentapi.web.auth", JSON.stringify(authSession("access-token", "refresh", Date.now() + 900_000)));

    const api = new ApiClient(vi.fn());

    expect(api.jobStreamURL("job-1", "event-1")).toBe("/v1/jobs/job-1/events?stream=1&after_id=event-1");
    expect(api.liveSessionURL("session-1")).toBe("/v1/sessions/session-1/live/ws");
    expect(api.liveSessionURL("session-1", "resume/1")).toBe("/v1/sessions/session-1/live/ws?resume_handle=resume%2F1");
    expect(api.webSocketProtocols()[0]).toBe("agentapi.bearer");
  });

  it("authenticates job streams with headers instead of query tokens", async () => {
    localStorage.setItem("agentapi.web.auth", JSON.stringify(authSession("access-token", "refresh", Date.now() + 900_000)));
    const fetchMock = vi.fn(async (_input: RequestInfo | URL, init?: RequestInit) => {
      expect(String(_input)).toBe("/v1/jobs/job-1/events?stream=1");
      expect(new Headers(init?.headers).get("Authorization")).toBe("Bearer access-token");
      return new Response("", { status: 200 });
    });
    vi.stubGlobal("fetch", fetchMock);

    const api = new ApiClient(vi.fn());
    await expect(api.jobStreamResponse("job-1")).resolves.toBeInstanceOf(Response);
  });

  it("uses encoded prompt IDs for admin prompt lifecycle APIs", async () => {
    localStorage.setItem("agentapi.web.auth", JSON.stringify(authSession("access-token", "refresh", Date.now() + 900_000)));
    const calls: Array<{ url: string; init?: RequestInit }> = [];
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      calls.push({ url, init });
      if (url.endsWith("/versions")) {
        return jsonResponse({ version: { prompt_id: "runtime/deep_agent/planner", version: "candidate", status: "review_pending", content: "content", content_hash: "hash" } }, 201);
      }
      if (url.endsWith("/publish") || url.endsWith("/rollback")) {
        return jsonResponse({ version: { prompt_id: "runtime/deep_agent/planner", version: "candidate", status: "published", content: "content", content_hash: "hash" } });
      }
      if (url.includes("/versions/diff")) {
        return jsonResponse({ diff: [], from: { version: "base" }, to: { version: "candidate" } });
      }
      return jsonResponse({ error: "not found" }, 404);
    });
    vi.stubGlobal("fetch", fetchMock);

    const api = new ApiClient(vi.fn());
    await api.createPromptVersion("secret", "runtime/deep_agent/planner", { version: "candidate", content: "content", base_version: "base" });
    await api.publishPromptVersion("secret", "runtime/deep_agent/planner", "candidate", "ship");
    await api.rollbackPromptVersion("secret", "runtime/deep_agent/planner", "base", "rollback");
    await api.diffPromptVersions("secret", "runtime/deep_agent/planner", "base", "candidate");

    expect(calls.map((call) => call.url)).toEqual([
      "/v1/admin/ops/prompts/runtime%2Fdeep_agent%2Fplanner/versions",
      "/v1/admin/ops/prompts/runtime%2Fdeep_agent%2Fplanner/publish",
      "/v1/admin/ops/prompts/runtime%2Fdeep_agent%2Fplanner/rollback",
      "/v1/admin/ops/prompts/runtime%2Fdeep_agent%2Fplanner/versions/diff?from_version=base&to_version=candidate"
    ]);
    expect(JSON.parse(String(calls[0].init?.body))).toMatchObject({
      prompt_id: "runtime/deep_agent/planner",
      version: "candidate",
      content: "content",
      base_version: "base"
    });
    expect(new Headers(calls[0].init?.headers).get("X-Admin-Token")).toBe("secret");
  });

  it("does not send all as a prompt scope filter", async () => {
    localStorage.setItem("agentapi.web.auth", JSON.stringify(authSession("access-token", "refresh", Date.now() + 900_000)));
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      expect(String(input)).toBe("/v1/admin/ops/prompts?limit=300");
      expect(new Headers(init?.headers).get("X-Admin-Token")).toBe("secret");
      return jsonResponse({ prompts: [] });
    });
    vi.stubGlobal("fetch", fetchMock);

    const api = new ApiClient(vi.fn());
    await expect(api.adminOpsPrompts("secret", { scope: "all", status: "all", limit: 300 })).resolves.toEqual([]);
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });
});

function authSession(accessToken: string, refreshToken: string, expiresAtMs: number): AuthSession {
  return {
    user: {
      id: "user-1",
      email: "user@example.com",
      display_name: "User",
      status: "active",
      created_at: new Date(0).toISOString()
    },
    access_token: accessToken,
    refresh_token: refreshToken,
    expires_at: new Date(expiresAtMs).toISOString()
  };
}

function jsonResponse(payload: unknown, status = 200): Response {
  return new Response(JSON.stringify(payload), {
    status,
    headers: { "Content-Type": "application/json" }
  });
}

function memoryStorage(): Storage {
  const values = new Map<string, string>();
  return {
    get length() {
      return values.size;
    },
    clear: () => values.clear(),
    getItem: (key: string) => values.get(key) ?? null,
    key: (index: number) => Array.from(values.keys())[index] ?? null,
    removeItem: (key: string) => {
      values.delete(key);
    },
    setItem: (key: string, value: string) => {
      values.set(key, value);
    }
  };
}
