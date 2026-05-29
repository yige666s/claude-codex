import { expect, test, type Page, type Route } from "@playwright/test";

const now = "2026-05-09T12:00:00.000Z";

test("Live mode releases microphone capture when rapidly switching back to text", async ({ page }) => {
  await installLiveBrowserFakes(page);
  await mockLiveAPI(page);
  await page.goto("/");
  await login(page);

  await page.getByRole("button", { name: "Start Live voice" }).click();
  await expect(page.getByRole("button", { name: "Stop Live voice" })).toBeVisible({ timeout: 10_000 });
  await expect.poll(() => page.evaluate(() => window.__liveTest.getUserMediaCalls)).toBe(1);
  await page.getByRole("button", { name: "Stop Live voice" }).click();

  await expect.poll(() => page.evaluate(() => window.__liveTest.stoppedTracks)).toBeGreaterThan(0);
  const sentFrames = await page.evaluate(() => window.__liveTest.sentFrames.join("\n"));
  expect(sentFrames).toContain("audio_end");
  await expect(page.getByRole("textbox", { name: "Message" })).not.toHaveAttribute("placeholder", "Live mode is active");
});

test("Live microphone capture can restart after stopping", async ({ page }) => {
  await installLiveBrowserFakes(page);
  await mockLiveAPI(page);
  await page.goto("/");
  await login(page);

  await page.getByRole("button", { name: "Start Live voice" }).click();
  await expect(page.getByRole("button", { name: "Stop Live voice" })).toBeVisible({ timeout: 10_000 });
  await expect.poll(() => page.evaluate(() => window.__liveTest.getUserMediaCalls)).toBe(1);
  await page.getByRole("button", { name: "Stop Live voice" }).click();
  await expect(page.getByRole("button", { name: "Start Live voice" })).toBeVisible();
  await page.waitForTimeout(500);
  await page.getByRole("button", { name: "Start Live voice" }).click();
  await expect(page.getByRole("button", { name: "Stop Live voice" })).toBeVisible({ timeout: 10_000 });
  await expect.poll(() => page.evaluate(() => window.__liveTest.getUserMediaCalls)).toBe(2);
});

test("Live permission denial is shown without internal details", async ({ page }) => {
  await installLiveBrowserFakes(page, { denyMicrophone: true });
  await mockLiveAPI(page);
  await page.goto("/");
  await login(page);

  await page.getByRole("button", { name: "Start Live voice" }).click();
  await expect(page.getByText("Microphone unavailable")).toBeVisible({ timeout: 10_000 });
  await expect(page.getByText(/\/run\/agentapi|GOOGLE_APPLICATION_CREDENTIALS|VERTEX_ACCESS_TOKEN|vertex-service-account/i)).toHaveCount(0);
});

test("Live credential errors are converted to user-safe copy", async ({ page }) => {
  await installLiveBrowserFakes(page, {
    firstFrame: {
      type: "error",
      error: "live vertex access token is required: read GOOGLE_APPLICATION_CREDENTIALS: open /run/agentapi/secrets/vertex-service-account.json: no such file or directory"
    }
  });
  await mockLiveAPI(page);
  await page.goto("/");
  await login(page);

  await page.getByRole("button", { name: "Start Live voice" }).click();
  await expect(page.getByText("Live mode is not configured for this environment. Ask an administrator to finish voice setup.")).toBeVisible();
  await expect(page.getByText(/\/run\/agentapi|GOOGLE_APPLICATION_CREDENTIALS|VERTEX_ACCESS_TOKEN|vertex-service-account/i)).toHaveCount(0);
});

test("Live prewarm click intent starts microphone capture after setup completes", async ({ page }) => {
  await installLiveBrowserFakes(page);
  await mockLiveAPI(page);
  await page.goto("/");
  await login(page);

  const liveButton = page.getByRole("button", { name: "Start Live voice" });
  await liveButton.dispatchEvent("pointerdown");
  await liveButton.click();

  await expect.poll(() => page.evaluate(() => window.__liveTest.sockets)).toBeGreaterThan(0);
  await expect.poll(() => page.evaluate(() => window.__liveTest.getUserMediaCalls)).toBe(1);
});

test("Live stops capture while assistant responds and can end input mode", async ({ page }) => {
  await installLiveBrowserFakes(page);
  await mockLiveAPI(page);
  await page.goto("/");
  await login(page);

  await page.getByRole("button", { name: "Start Live voice" }).click();
  await expect.poll(() => page.evaluate(() => window.__liveTest.getUserMediaCalls)).toBe(1);

  await page.evaluate(() => {
    const processor = window.__liveTest.processors[window.__liveTest.processors.length - 1];
    processor?.onaudioprocess?.({
      inputBuffer: { getChannelData: () => new Float32Array(1024).fill(0.2) }
    });
  });
  await page.waitForTimeout(650);
  await page.evaluate(() => {
    const processor = window.__liveTest.processors[window.__liveTest.processors.length - 1];
    processor?.onaudioprocess?.({
      inputBuffer: { getChannelData: () => new Float32Array(1024) }
    });
  });

  await expect.poll(() => page.evaluate(() => window.__liveTest.stoppedTracks)).toBeGreaterThan(0);
  await expect(page.getByRole("button", { name: "End Live voice" })).toBeVisible();
  const sentFrames = await page.evaluate(() => window.__liveTest.sentFrames.join("\n"));
  expect(sentFrames).toContain("activity_end");

  await page.getByRole("button", { name: "End Live voice" }).click();
  await expect(page.getByRole("button", { name: "Start Live voice" })).toBeVisible();
});

async function login(page: Page) {
  await page.getByLabel("Email").fill("live@example.com");
  await page.getByLabel("Password").fill("password123");
  await page.getByRole("button", { name: "Login" }).last().click();
  await expect(page.getByRole("heading", { name: /Hi Live User/i })).toBeVisible();
  await page.getByRole("button", { name: "新聊天" }).click();
}

async function mockLiveAPI(page: Page) {
  const session = {
    id: "live-session",
    working_dir: "/tmp",
    started_at: now,
    updated_at: now,
    messages: []
  };
  await page.route("**/readyz?**", (route) => json(route, { status: "ok", checks: [] }));
  await page.route("**/v1/auth/login", (route) => json(route, authSession()));
  await page.route("**/v1/auth/refresh", (route) => json(route, authSession()));
  await page.route("**/v1/auth/me", (route) => json(route, { user: authSession().user }));
  await page.route("**/v1/memory/settings", (route) => json(route, { enabled: true, capture_enabled: true, context_enabled: true }));
  await page.route("**/v1/personalization", (route) => json(route, { profile: {}, style: {}, traits: {}, custom_instructions: "", feature_flags: {} }));
  await page.route("**/v1/memory?**", (route) => json(route, { items: [] }));
  await page.route("**/v1/memory/maintenance", (route) => json(route, { actions: [] }));
  await page.route("**/v1/skills", (route) => json(route, { skills: [] }));
  await page.route("**/v1/sessions?**", (route) => json(route, [session]));
  await page.route("**/v1/sessions", (route) => route.request().method() === "POST" ? json(route, session, 201) : json(route, [session]));
  await page.route(/.*\/v1\/sessions\/[^/]+$/, (route) => json(route, session));
  await page.route("**/v1/attachments?**", (route) => json(route, { attachments: [] }));
  await page.route("**/v1/artifacts?**", (route) => json(route, { artifacts: [] }));
  await page.route("**/v1/jobs?**", (route) => json(route, { jobs: [] }));
}

async function installLiveBrowserFakes(page: Page, options: { denyMicrophone?: boolean; firstFrame?: Record<string, unknown> } = {}) {
  await page.addInitScript((config) => {
    window.__liveTest = { stoppedTracks: 0, sentFrames: [], getUserMediaCalls: 0, sockets: 0, processors: [] };
    class FakeWebSocket {
      static CONNECTING = 0;
      static OPEN = 1;
      static CLOSED = 3;
      readyState = FakeWebSocket.CONNECTING;
      onmessage: ((event: MessageEvent) => void) | null = null;
      onclose: (() => void) | null = null;
      onerror: (() => void) | null = null;
      constructor() {
        window.__liveTest.sockets += 1;
        window.__liveTest.latestSocket = this;
        setTimeout(() => {
          this.readyState = FakeWebSocket.OPEN;
          const frame = config.firstFrame || { type: "live_setup_complete" };
          this.onmessage?.({ data: JSON.stringify(frame) } as MessageEvent);
        }, 20);
      }
      send(frame: string) {
        window.__liveTest.sentFrames.push(frame);
      }
      close() {
        this.readyState = FakeWebSocket.CLOSED;
        this.onclose?.();
      }
    }
    window.WebSocket = FakeWebSocket as unknown as typeof WebSocket;
    Object.defineProperty(navigator, "mediaDevices", {
      configurable: true,
      value: {
      getUserMedia: async () => {
        window.__liveTest.getUserMediaCalls += 1;
        if (config.denyMicrophone) throw new DOMException("Permission denied", "NotAllowedError");
        const track = {
          enabled: true,
          label: "Fake microphone",
          getSettings: () => ({}),
          stop: () => {
            window.__liveTest.stoppedTracks += 1;
          },
          onended: null,
          onmute: null,
          onunmute: null
        };
        return {
          getTracks: () => [track],
          getAudioTracks: () => [track]
        } as unknown as MediaStream;
      }
      } as MediaDevices
    });
    Object.defineProperty(navigator, "permissions", {
      configurable: true,
      value: {
        query: async () => ({ state: config.denyMicrophone ? "denied" : "granted" })
      }
    });
    class FakeAudioContext {
      sampleRate = 48000;
      currentTime = 0;
      state: AudioContextState = "running";
      destination = {};
      createMediaStreamSource() {
        return { connect() {}, disconnect() {} };
      }
      createScriptProcessor() {
        const processor = { onaudioprocess: null, connect() {}, disconnect() {} };
        window.__liveTest.processors.push(processor);
        return processor;
      }
      createGain() {
        return { gain: { value: 1 }, connect() {}, disconnect() {} };
      }
      createBuffer() {
        return { duration: 0.01, getChannelData: () => new Float32Array(1) };
      }
      createBufferSource() {
        return { buffer: null, connect() {}, start() {}, stop() {}, onended: null };
      }
      resume() {
        return Promise.resolve();
      }
      close() {
        return Promise.resolve();
      }
    }
    window.AudioContext = FakeAudioContext as unknown as typeof AudioContext;
  }, options);
}

function authSession() {
  return {
    user: {
      id: "live-user",
      email: "live@example.com",
      display_name: "Live User",
      status: "active",
      created_at: now
    },
    access_token: "access-token",
    refresh_token: "refresh-token",
    csrf_token: "csrf-token",
    expires_at: "2099-01-01T00:00:00.000Z"
  };
}

function json(route: Route, body: unknown, status = 200) {
  return route.fulfill({ status, contentType: "application/json", body: JSON.stringify(body) });
}

declare global {
  interface Window {
    __liveTest: {
      stoppedTracks: number;
      sentFrames: string[];
      getUserMediaCalls: number;
      sockets: number;
      latestSocket?: {
        onmessage: ((event: MessageEvent) => void) | null;
      };
      processors: Array<{
        onaudioprocess: ((event: { inputBuffer: { getChannelData: () => Float32Array } }) => void) | null;
      }>;
    };
  }
}
