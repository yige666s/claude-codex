import { expect, test, type Page, type Route } from "@playwright/test";

const now = "2026-05-09T12:00:00.000Z";
const viewports = [
  { name: "mobile-390", width: 390, height: 844 },
  { name: "tablet-768", width: 768, height: 1024 },
  { name: "desktop-1440", width: 1440, height: 1000 }
];

for (const viewport of viewports) {
  test(`responsive visual QA covers workspace surfaces at ${viewport.name}`, async ({ page }) => {
    await page.setViewportSize({ width: viewport.width, height: viewport.height });
    await mockVisualAPI(page);
    await page.goto("/");
    await page.getByLabel("Email").fill("visual@example.com");
    await page.getByLabel("Password").fill("password123");
    await page.getByRole("button", { name: "Login" }).last().click();

    await expect(page.locator(".workspace-stage")).toBeVisible();
    await expect(page.locator(".composer")).toBeVisible();
    await assertNoHorizontalOverflow(page);
    await assertBoxWithinViewport(page, ".composer", viewport.width);

    await domClickButton(page, "资源");
    await page.getByRole("tab", { name: "Attachments" }).click();
    await expect(page.getByRole("dialog", { name: "Attachments" })).toBeVisible();
    await assertBoxWithinViewport(page, ".resource-modal", viewport.width);
    await page.getByRole("button", { name: "Preview notes.md" }).click();
    await expect(page.getByRole("dialog", { name: "notes.md" })).toBeVisible();
    await assertBoxWithinViewport(page, ".preview-modal", viewport.width);
    await page.getByRole("button", { name: "Close preview" }).click();
    await page.getByRole("dialog", { name: "Attachments" }).getByLabel("Close resources").click();

    await domClickButton(page, "资源");
    await expect(page.getByRole("dialog", { name: "Jobs" })).toBeVisible();
    await page.getByRole("tab", { name: "Artifacts" }).click();
    await expect(page.getByRole("dialog", { name: "Artifacts" }).locator(".asset-row", { hasText: "artifact.md" })).toBeVisible();
    await page.getByRole("dialog", { name: "Artifacts" }).locator(".asset-row-main", { hasText: "artifact.md" }).click();
    await expect(page.getByRole("complementary", { name: "Artifact preview" })).toBeVisible();
    await assertBoxWithinViewport(page, ".artifact-workspace", viewport.width);
    if (viewport.width >= 1081) {
      await dragHorizontal(page, ".workspace-resizer-sidebar", -120);
      await expect.poll(() => elementWidth(page, ".sidebar")).toBeGreaterThanOrEqual(312);
      await assertElementsInside(page, ".sidebar", [
        ".sidebar-head",
        ".service-status",
        ".sidebar-collapse-button",
        ".toolbar",
        ".session-list",
        ".account"
      ]);

      const minSidebarWidth = await elementWidth(page, ".sidebar");
      await dragHorizontal(page, ".workspace-resizer-sidebar", 72);
      await expect.poll(() => elementWidth(page, ".sidebar")).toBeGreaterThan(minSidebarWidth + 40);
      await expect.poll(() => elementWidth(page, ".sidebar")).toBeGreaterThanOrEqual(312);

      const initialArtifactWidth = await elementWidth(page, ".artifact-workspace");
      await dragHorizontal(page, ".workspace-resizer-artifact", 96);
      await expect.poll(() => elementWidth(page, ".artifact-workspace")).toBeLessThan(initialArtifactWidth - 48);
      await expect.poll(() => elementWidth(page, ".artifact-workspace")).toBeGreaterThanOrEqual(360);
      await expect.poll(() => elementWidth(page, ".workspace")).toBeGreaterThanOrEqual(520);
    }
    await page.getByRole("button", { name: "Open preview for artifact.md" }).click();
    await expect(page.getByRole("dialog", { name: "artifact.md" })).toBeVisible();
    await assertBoxWithinViewport(page, ".preview-modal", viewport.width);
    await page.getByRole("button", { name: "Close preview" }).click();
    await page.getByLabel("Close artifact preview").click();

    await domClickButton(page, "搜索聊天");
    await expect(page.getByRole("dialog", { name: "Search across all sessions" })).toBeVisible();
    await assertBoxWithinViewport(page, ".global-search-modal", viewport.width);
    await page.getByRole("textbox", { name: "Search across all sessions" }).fill("visual");
    await expect(page.locator(".global-search-result").first()).toBeVisible();
    await assertNoHorizontalOverflow(page);
  });
}

async function assertNoHorizontalOverflow(page: Page) {
  const metrics = await page.evaluate(() => ({
    scrollWidth: document.documentElement.scrollWidth,
    clientWidth: document.documentElement.clientWidth
  }));
  expect(metrics.scrollWidth).toBeLessThanOrEqual(metrics.clientWidth + 1);
}

async function assertBoxWithinViewport(page: Page, selector: string, viewportWidth: number) {
  await expect.poll(async () => {
    const box = await page.locator(selector).first().boundingBox();
    if (!box) return false;
    return box.x >= -1 && box.x + box.width <= viewportWidth + 1;
  }, { message: `${selector} should settle inside the viewport` }).toBe(true);
}

async function elementWidth(page: Page, selector: string): Promise<number> {
  const box = await page.locator(selector).first().boundingBox();
  return box?.width || 0;
}

async function assertElementsInside(page: Page, containerSelector: string, childSelectors: string[]) {
  const container = await page.locator(containerSelector).first().boundingBox();
  expect(container, `${containerSelector} should be visible`).not.toBeNull();
  for (const childSelector of childSelectors) {
    const child = await page.locator(childSelector).first().boundingBox();
    expect(child, `${childSelector} should be visible`).not.toBeNull();
    expect(child!.x, `${childSelector} should not overflow left`).toBeGreaterThanOrEqual(container!.x - 1);
    expect(child!.x + child!.width, `${childSelector} should not overflow right`).toBeLessThanOrEqual(container!.x + container!.width + 1);
  }
}

async function dragHorizontal(page: Page, selector: string, deltaX: number) {
  const box = await page.locator(selector).first().boundingBox();
  if (!box) throw new Error(`${selector} is not visible`);
  const startX = box.x + box.width / 2;
  const startY = box.y + box.height / 2;
  await page.mouse.move(startX, startY);
  await page.mouse.down();
  await page.mouse.move(startX + deltaX, startY, { steps: 8 });
  await page.mouse.up();
}

async function domClickButton(page: Page, name: string) {
  await page.getByRole("button", { name }).first().evaluate((element) => {
    (element as HTMLButtonElement).click();
  });
}

async function mockVisualAPI(page: Page) {
  const session = {
    id: "visual-session",
    working_dir: "/tmp",
    started_at: now,
    updated_at: now,
    messages: [
      { role: "user", content: "visual QA prompt", created_at: now, message_index: 0 },
      { role: "assistant", content: "A short visual QA answer with **markdown**.", created_at: now, message_index: 1 }
    ]
  };
  const attachment = asset("attachment-1", "attachment", "notes.md", "text/markdown", 20);
  const artifact = asset("artifact-1", "artifact", "artifact.md", "text/markdown", 30);

  await page.route("**/readyz?**", (route) => json(route, { status: "ok", checks: [] }));
  await page.route("**/v1/auth/login", (route) => json(route, authSession()));
  await page.route("**/v1/auth/refresh", (route) => json(route, authSession()));
  await page.route("**/v1/auth/me", (route) => json(route, { user: authSession().user }));
  await page.route("**/v1/memory/settings", (route) => json(route, { enabled: true, capture_enabled: true, context_enabled: true }));
  await page.route("**/v1/personalization", (route) => json(route, { profile: {}, style: {}, traits: {}, custom_instructions: "", feature_flags: {} }));
  await page.route("**/v1/memory?**", (route) => json(route, { items: [] }));
  await page.route("**/v1/memory/maintenance", (route) => json(route, { actions: [] }));
  await page.route("**/v1/skills", (route) => json(route, { skills: [{ name: "vertex-image-artifact", description: "Generate an artifact", run_as_job: true }] }));
  await page.route("**/v1/sessions?**", (route) => json(route, [session]));
  await page.route(/.*\/v1\/sessions\/[^/]+$/, (route) => json(route, session));
  await page.route("**/v1/attachments", (route) => json(route, { attachments: [attachment] }));
  await page.route("**/v1/attachments?**", (route) => json(route, { attachments: [attachment] }));
  await page.route("**/v1/artifacts?**", (route) => json(route, { artifacts: [artifact] }));
  await page.route("**/v1/jobs?**", (route) => json(route, { jobs: [{ id: "job-1", session_id: session.id, type: "skill", status: "succeeded", content: "visual artifact", created_at: now, updated_at: now }] }));
  await page.route(/.*\/v1\/attachments\/attachment-1(?:\?.*)?$/, (route) => text(route, "# Notes\n\nvisual attachment", "text/markdown"));
  await page.route(/.*\/v1\/artifacts\/artifact-1(?:\?.*)?$/, (route) => text(route, "# Artifact\n\nvisual artifact", "text/markdown"));
  await page.route("**/v1/search/messages?**", (route) => json(route, { items: [{
    session_id: session.id,
    message_index: 0,
    role: "user",
    content: "visual QA prompt",
    snippet: "visual QA prompt",
    session_title: "visual QA prompt",
    created_at: now
  }] }));
}

function authSession() {
  return {
    user: {
      id: "visual-user",
      email: "visual@example.com",
      display_name: "Visual User",
      status: "active",
      created_at: now
    },
    access_token: "access-token",
    refresh_token: "refresh-token",
    csrf_token: "csrf-token",
    expires_at: "2099-01-01T00:00:00.000Z"
  };
}

function asset(id: string, kind: string, filename: string, contentType: string, sizeBytes: number) {
  return { id, kind, session_id: "visual-session", filename, content_type: contentType, size_bytes: sizeBytes, created_at: now };
}

function json(route: Route, body: unknown, status = 200) {
  return route.fulfill({ status, contentType: "application/json", body: JSON.stringify(body) });
}

function text(route: Route, body: string, contentType: string) {
  return route.fulfill({ status: 200, contentType, body });
}
