import { expect, test, type Page } from "@playwright/test";

const realE2EEnabled = process.env.REAL_E2E === "1";
const email = process.env.E2E_EMAIL || "";
const password = process.env.E2E_PASSWORD || "";
const generatedEmail = `agentapi-e2e-${Date.now()}@example.com`;
const generatedPassword = "AgentAPI-e2e-12345";
const workspaceGreeting = /^(Hello|Hi) /;

test.describe("real environment E2E", () => {
  test.skip(!realE2EEnabled, "Set REAL_E2E=1 and E2E_BASE_URL to run against a real AgentAPI deployment.");
  test.skip(!process.env.E2E_BASE_URL, "Set E2E_BASE_URL to a real AgentAPI Web deployment.");

  test.beforeEach(async ({ context }) => {
    await context.grantPermissions(["microphone"]);
  });

  test("real login, chat, upload-backed attachment preview, search, and settings", async ({ page }) => {
    await login(page);
    await expect(page.getByRole("heading", { name: workspaceGreeting })).toBeVisible();

    await createNewChat(page);
    const prompt = `AgentAPI real E2E smoke ${Date.now()}. Reply with one short sentence.`;
    await sendMessage(page, prompt);
    await expectAssistantReply(page);

    await uploadTextAttachment(page, "real-e2e-notes.txt", `real object storage upload ${Date.now()}`);
    await sendMessage(page, "Please acknowledge the uploaded text file briefly.");
    await expectAssistantReply(page);

    await page.getByRole("button", { name: "Attachments" }).click();
    await page.getByRole("button", { name: "Preview real-e2e-notes.txt" }).click();
    await expect(page.getByRole("dialog", { name: "real-e2e-notes.txt" })).toBeVisible();
    await expect(page.getByText("real object storage upload")).toBeVisible();
    await page.getByRole("button", { name: "Close preview" }).click();
    await page.getByRole("dialog", { name: "Attachments" }).getByLabel("Close resources").click();

    await page.getByRole("button", { name: "搜索聊天" }).click();
    await page.getByRole("textbox", { name: "Search across all sessions" }).fill("AgentAPI real E2E smoke");
    await expect(page.locator(".global-search-result").first()).toBeVisible({ timeout: 30_000 });
    await page.keyboard.press("Escape");

    await page.getByRole("button", { name: "Settings" }).click();
    await page.getByRole("menuitem", { name: "Settings" }).click();
    await expect(page.getByRole("dialog", { name: "Settings" })).toBeVisible();
  });

  test("real job creates an artifact and opens Artifact Workspace", async ({ page }) => {
    test.skip(process.env.E2E_RUN_ARTIFACT !== "1", "Set E2E_RUN_ARTIFACT=1 to run provider-backed job/artifact generation.");

    await login(page);
    await createNewChat(page);
    await page.getByRole("button", { name: "Use image generation" }).click();
    await sendMessage(page, process.env.E2E_ARTIFACT_PROMPT || "Create a simple blue square test image with no text.");

    await expect(page.getByRole("button", { name: "Artifacts, new item available" }).or(page.getByRole("button", { name: "Artifacts" }))).toBeVisible({ timeout: 180_000 });
    await page.getByRole("button", { name: /Artifacts/ }).click();
    await expect(page.getByRole("complementary", { name: "Artifact workspace" })).toBeVisible();
    await expect(page.locator(".artifact-workspace-item").first()).toBeVisible({ timeout: 60_000 });
  });

  test("real Live websocket reaches a user-safe terminal state", async ({ page }) => {
    test.skip(process.env.E2E_RUN_LIVE !== "1", "Set E2E_RUN_LIVE=1 when Live credentials are configured.");

    await login(page);
    await createNewChat(page);
    await page.getByRole("button", { name: "Choose mode" }).click();
    await page.getByRole("menuitem", { name: "Live" }).click();
    await expect(page.getByRole("textbox", { name: "Message" })).toHaveAttribute("placeholder", "Live mode is active", { timeout: 30_000 });
    await expect(page.getByText(/GOOGLE_APPLICATION_CREDENTIALS|VERTEX_ACCESS_TOKEN|\/run\/agentapi|vertex-service-account/i)).toHaveCount(0);
    await page.getByRole("button", { name: "Choose mode" }).click();
    await page.getByRole("menuitem", { name: "Chat" }).click();
    await expect(page.getByRole("textbox", { name: "Message" })).not.toHaveAttribute("placeholder", "Live mode is active");
  });
});

async function login(page: Page) {
  await page.goto("/");
  if (email && password) {
    await page.getByLabel("Email").fill(email);
    await page.getByLabel("Password").fill(password);
    await page.getByRole("button", { name: "Login" }).last().click();
    await expect(page.getByRole("heading", { name: workspaceGreeting })).toBeVisible({ timeout: 30_000 });
  } else {
    await page.getByRole("button", { name: "Register" }).click();
    await page.getByLabel("Email").fill(generatedEmail);
    await page.getByLabel("Name").fill("Real E2E User");
    await page.getByLabel("Password").fill(generatedPassword);
    await page.getByLabel("Repeat secret").fill(generatedPassword);
    await page.getByRole("button", { name: "Create Account" }).click();
    const registered = await Promise.race([
      page.getByRole("heading", { name: workspaceGreeting }).waitFor({ state: "visible", timeout: 30_000 }).then(() => true),
      page.getByText(/Verification email sent|email verification/i).waitFor({ state: "visible", timeout: 30_000 }).then(() => false)
    ]);
    if (!registered) {
      test.skip(true, "Registration requires email verification; set E2E_EMAIL and E2E_PASSWORD for this deployment.");
    }
  }
}

async function createNewChat(page: Page) {
  await Promise.all([
    page.waitForResponse((response) => response.url().includes("/v1/sessions") && response.request().method() === "POST" && response.status() === 201),
    page.getByRole("button", { name: "新聊天" }).click()
  ]);
  await expect(page.getByRole("textbox", { name: "Message" })).toBeEnabled();
}

async function sendMessage(page: Page, message: string) {
  const input = page.getByRole("textbox", { name: "Message" });
  await expect(input).toBeEnabled();
  await input.fill(message);
  await expect(input).toHaveValue(message);
  const send = page.getByRole("button", { name: "Send" });
  await expect(send).toBeEnabled();
  await send.click();
}

async function expectAssistantReply(page: Page) {
  await expect(page.locator(".message.assistant .markdown-content").last()).toContainText(/./, { timeout: 90_000 });
}

async function uploadTextAttachment(page: Page, filename: string, content: string) {
  await Promise.all([
    page.waitForResponse((response) => response.url().includes("/v1/attachments") && response.request().method() === "POST" && response.status() === 201),
    page.locator("input[type=file]").setInputFiles({
      name: filename,
      mimeType: "text/plain",
      buffer: Buffer.from(content)
    })
  ]);
  const pendingAttachment = page.getByLabel("Pending attachments").getByText(filename);
  if (await pendingAttachment.isVisible().catch(() => false)) return;
  await page.getByRole("button", { name: /Attachments/ }).click();
  await page.getByRole("button", { name: `Add ${filename} to message` }).click();
  await page.getByRole("dialog", { name: "Attachments" }).getByLabel("Close resources").click();
  await expect(pendingAttachment).toBeVisible();
}
