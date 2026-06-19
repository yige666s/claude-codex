import { expect, test, type Page, type Route } from "@playwright/test";

const baselineDir = "test-results/frontend-baseline";
const now = "2026-05-21T12:00:00.000Z";

test.describe("frontend phase 0 visual baselines", () => {
  test("captures core workspace and admin surfaces", async ({ page }) => {
    await mockBaselineAPI(page);
    await seedAuth(page);

    await page.goto("/");
    await expect(page.getByRole("heading", { name: /provider-model/ })).toBeVisible();
    await screenshot(page, ".app-shell", `${baselineDir}/desktop-workspace.png`);

    await page.locator(".account button[aria-label='Settings']").click();
    await expect(page.getByRole("menuitem", { name: "Manage Memory" })).toBeVisible();
    await page.screenshot({ path: `${baselineDir}/desktop-settings-menu.png`, fullPage: true });
    await page.keyboard.press("Escape");

    await page.getByRole("button", { name: "资源" }).click();
    await expect(page.getByText("/docx baseline")).toBeVisible();
    await page.screenshot({ path: `${baselineDir}/desktop-resource-jobs.png`, fullPage: true });
    await page.getByLabel("Close resources").click();

    await page.goto("/admin");
    await expect(page.getByRole("heading", { name: "Skills" })).toBeVisible();
    await screenshot(page, ".admin-shell", `${baselineDir}/desktop-admin-shell.png`);
  });

  test("captures mobile workspace surfaces", async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 });
    await mockBaselineAPI(page);
    await seedAuth(page);

    await page.goto("/");
    await expect(page.getByRole("heading", { name: /provider-model/ })).toBeVisible();
    await screenshot(page, ".app-shell", `${baselineDir}/mobile-workspace.png`);

    await page.locator(".topbar button[aria-label='Open navigation']").click();
    await expect(page.getByRole("button", { name: "新聊天" })).toBeVisible();
    await screenshot(page, ".app-shell", `${baselineDir}/mobile-navigation.png`);
  });
});

async function seedAuth(page: Page) {
  await page.addInitScript((authSession) => {
    window.localStorage.setItem("agentapi.web.auth", JSON.stringify(authSession));
  }, {
    access_token: "baseline-access",
    refresh_token: "baseline-refresh",
    expires_at: "2026-05-21T13:00:00.000Z",
    user: {
      id: "baseline-user",
      email: "baseline@example.com",
      display_name: "Baseline User",
      status: "active",
      email_verified: true,
      created_at: now
    }
  });
}

async function mockBaselineAPI(page: Page) {
  const session = {
    id: "baseline-session",
    title: "Live baseline",
    working_dir: "/tmp",
    started_at: now,
    updated_at: now,
    messages: [
      { role: "user", content: "当前有哪些 provider-model 支持 live api", created_at: now, message_index: 0 },
      {
        role: "assistant",
        content: "Gemini Live is configured for voice mode. Use the left panel to inspect jobs, assets, and skills.",
        created_at: now,
        message_index: 1
      }
    ]
  };
  const job = {
    id: "job-baseline",
    session_id: session.id,
    type: "baseline_job",
    status: "succeeded",
    content: "/docx baseline",
    created_at: now,
    updated_at: now
  };

  await page.route("**/readyz?**", (route) => json(route, { status: "ok", checks: [] }));
  await page.route("**/v1/auth/me", (route) => json(route, { user: { id: "baseline-user", email: "baseline@example.com", display_name: "Baseline User" } }));
  await page.route("**/v1/auth/refresh", (route) => json(route, {
    access_token: "baseline-access-2",
    refresh_token: "baseline-refresh-2",
    expires_at: "2026-05-21T13:00:00.000Z",
    user: { id: "baseline-user", email: "baseline@example.com", display_name: "Baseline User", status: "active", email_verified: true, created_at: now }
  }));
  await page.route("**/v1/sessions?**", (route) => json(route, [session]));
  await page.route("**/v1/sessions/baseline-session", (route) => json(route, session));
  await page.route("**/v1/skills", (route) => json(route, {
    skills: [
      { name: "docx", description: "Create Word documents", icon: "DOC", run_as_job: true, produces_artifacts: true },
      { name: "vertex-image-artifact", description: "Generate images", icon: "IMG", run_as_job: true, produces_artifacts: true }
    ]
  }));
  await page.route("**/v1/loop-templates", (route) => json(route, { templates: [] }));
  await page.route("**/v1/loop-goals?**", (route) => json(route, { goals: [] }));
  await page.route("**/v1/jobs?**", (route) => json(route, { jobs: [job] }));
  await page.route("**/v1/jobs/job-baseline/events?stream=1**", (route) => sse(route, [
    { id: "evt-baseline-start", event: "start", data: { type: "start", session_id: session.id } },
    { id: "evt-baseline-progress", event: "message", data: { type: "message", message: "baseline running", session_id: session.id } }
  ]));
  await page.route("**/v1/jobs/job-baseline/events", (route) => json(route, {
    events: [
      { id: "evt-baseline", job_id: job.id, type: "start", event: { type: "start", content: "baseline started" }, created_at: now }
    ]
  }));
  await page.route("**/v1/attachments?**", (route) => json(route, { attachments: [] }));
  await page.route("**/v1/artifacts?**", (route) => json(route, { artifacts: [] }));
  await page.route("**/v1/memory/settings", (route) => json(route, { enabled: true, capture_enabled: true, context_enabled: true, updated_at: now }));
  await page.route("**/v1/personalization", (route) => json(route, {
    profile: {},
    style: {},
    traits: {},
    custom_instructions: "",
    feature_flags: { quick_answers: true, use_saved_memory: true, use_chat_history: true, use_browser_memory: false },
    version: 1,
    updated_at: now
  }));
  await page.route("**/v1/admin/skills", (route) => json(route, {
    skills: [
      { name: "docx", status: "published", description: "Create Word documents", versions: [] }
    ]
  }));
}

async function json(route: Route, body: unknown, status = 200) {
  await route.fulfill({
    status,
    contentType: "application/json",
    body: JSON.stringify(body)
  });
}

async function screenshot(page: Page, selector: string, path: string) {
  await page.locator(selector).screenshot({ path });
}

async function sse(route: Route, events: Array<{ id?: string; event: string; data: unknown }>) {
  const body = events.map((item) => [
    item.id ? `id: ${item.id}` : "",
    `event: ${item.event}`,
    `data: ${JSON.stringify(item.data)}`,
    ""
  ].filter(Boolean).join("\n")).join("\n");
  await route.fulfill({
    status: 200,
    headers: {
      "content-type": "text/event-stream",
      "cache-control": "no-cache"
    },
    body: `${body}\n`
  });
}
