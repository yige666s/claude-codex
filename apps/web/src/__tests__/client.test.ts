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
});

describe("ApiClient projects", () => {
  beforeEach(() => {
    vi.stubGlobal("localStorage", memoryStorage());
    localStorage.setItem("agentapi.web.auth", JSON.stringify(authSession("access", "refresh", Date.now() + 900_000)));
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("creates project-scoped sessions", async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url === "/v1/projects") {
        expect(init?.method).toBe("POST");
        expect(JSON.parse(String(init?.body))).toMatchObject({ name: "Launch" });
        return jsonResponse({ id: "project-1", name: "Launch", created_at: new Date(0).toISOString(), updated_at: new Date(0).toISOString() }, 201);
      }
      if (url === "/v1/sessions") {
        expect(init?.method).toBe("POST");
        expect(JSON.parse(String(init?.body))).toMatchObject({ project_id: "project-1" });
        return jsonResponse({ id: "session-1", project_id: "project-1", working_dir: "", started_at: new Date(0).toISOString(), updated_at: new Date(0).toISOString() }, 201);
      }
      return jsonResponse({ error: "not found" }, 404);
    });
    vi.stubGlobal("fetch", fetchMock);

    const api = new ApiClient(vi.fn());
    const project = await api.createProject({ name: "Launch" });
    const session = await api.createSession(project.id);

    expect(project.id).toBe("project-1");
    expect(session.project_id).toBe("project-1");
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
