import { expect, test, type Page, type Route } from "@playwright/test";

const now = "2026-05-09T12:00:00.000Z";

type Message = {
  role: string;
  content?: string;
  created_at?: string;
  message_index?: number;
  attachments?: Array<{
    id: string;
    file_type?: string;
    mime_type?: string;
    file_name?: string;
    file_size?: number;
  }>;
};

type Session = {
  id: string;
  working_dir: string;
  started_at: string;
  updated_at: string;
  messages: Message[];
};

type Asset = {
  id: string;
  kind: "attachment" | "artifact";
  session_id: string;
  filename: string;
  content_type: string;
  size_bytes: number;
  created_at: string;
  job_id?: string;
};

type Job = {
  id: string;
  session_id: string;
  type: string;
  status: "queued" | "running" | "succeeded" | "failed" | "cancelled";
  content: string;
  created_at: string;
  updated_at: string;
};

test("covers auth, sessions, chat, attachments, jobs, previews, and search", async ({ page }) => {
  const api = await mockAgentAPI(page);

  await page.goto("/");
  await page.getByRole("button", { name: "Register" }).click();
  await page.getByLabel("Email").fill("e2e@example.com");
  await page.getByLabel("Name").fill("E2E User");
  await page.getByLabel("Password").fill("password123");
  await page.getByLabel("Repeat secret").fill("password123");
  await page.getByRole("button", { name: "Create Account" }).click();

  await expect(page.getByRole("heading", { name: /Hi E2E User/i })).toBeVisible();
  await expect(page.locator(".empty-state")).toBeVisible();
  await expect(page.getByRole("button", { name: "Use image generation" })).toBeVisible();
  await expect(page.getByRole("button", { name: "Use web search" })).toBeVisible();
  await expect(page.getByRole("button", { name: "Use plan and execute mode" })).toBeVisible();
  await page.getByRole("textbox", { name: "Message" }).fill("d".repeat(56) + "\n" + "d".repeat(10));
  const emptyPromptBox = await page.locator(".empty-state").boundingBox();
  const emptyComposerBox = await page.locator(".composer").boundingBox();
  expect(emptyPromptBox).not.toBeNull();
  expect(emptyComposerBox).not.toBeNull();
  expect(emptyComposerBox!.y).toBeGreaterThan(emptyPromptBox!.y);
  await page.getByRole("button", { name: "Add attachment" }).click();
  await expect(page.locator(".composer-attachment-menu")).toBeVisible();
  await expect.poll(async () => {
    const trigger = await page.locator(".composer-upload").boundingBox();
    const menu = await page.locator(".composer-attachment-menu").boundingBox();
    return !!trigger && !!menu && menu.y >= trigger.y + trigger.height;
  }, { message: "empty-session attachment menu should open below the composer trigger" }).toBe(true);
  await page.keyboard.press("Escape");
  await page.getByRole("textbox", { name: "Message" }).fill("");

  await page.getByRole("button", { name: "新聊天" }).click();
  await expect(page.getByRole("textbox", { name: "Message" })).toBeVisible();

  await page.getByRole("button", { name: "Use plan and execute mode" }).click();
  await expect(page.getByRole("button", { name: "Use plan and execute mode" })).toHaveAttribute("aria-pressed", "true");
  await page.getByRole("textbox", { name: "Message" }).fill("hello from playwright");
  await page.getByRole("button", { name: "Send" }).click();
  await expect(page.getByText("Echo: hello from playwright")).toBeVisible();
  await expect(page.getByRole("button", { name: "Use image generation" })).toBeHidden();
  await expect(page.getByRole("button", { name: "Use web search" })).toBeHidden();
  await expect(page.getByRole("button", { name: "Use plan and execute mode" })).toBeHidden();
  await page.getByRole("button", { name: "Add attachment" }).click();
  await expect(page.locator(".composer-attachment-menu")).toBeVisible();
  await expect.poll(async () => {
    const trigger = await page.locator(".composer-upload").boundingBox();
    const menu = await page.locator(".composer-attachment-menu").boundingBox();
    return !!trigger && !!menu && menu.y + menu.height <= trigger.y;
  }, { message: "active-session attachment menu should open above the composer trigger" }).toBe(true);
  await page.keyboard.press("Escape");

  await page.locator("input[type=file]").setInputFiles({
    name: "notes.md",
    mimeType: "text/markdown",
    buffer: Buffer.from("# Notes\n\nhello attachment")
  });
  await expect(page.getByLabel("Pending attachments").getByText("notes.md")).toBeVisible();
  await page.getByRole("textbox", { name: "Message" }).fill("please inspect the attachment");
  await page.getByRole("button", { name: "Send" }).click();
  await expect(page.getByText("Attachment received: notes.md")).toBeVisible();

  await page.getByRole("button", { name: "Add attachment" }).click();
  await expect(page.getByText("从库中添加")).toBeVisible();
  await expect(page.locator(".composer-attachment-recent-list .composer-attachment-file")).toHaveCount(5);
  await expect(page.getByRole("menuitem", { name: /oldest\.txt/ })).toHaveCount(0);
  await page.getByRole("menuitem", { name: /notes\.md/ }).click();
  await expect(page.getByLabel("Pending attachments").getByText("notes.md")).toBeVisible();
  await page.getByRole("textbox", { name: "Message" }).fill("please inspect the existing attachment");
  await page.getByRole("button", { name: "Send" }).click();
  await expect(page.getByText("Attachment received: notes.md").last()).toBeVisible();

  await page.getByRole("button", { name: "Add attachment" }).click();
  await page.getByRole("menuitem", { name: "从库中添加" }).click();
  await expect(page.getByRole("dialog", { name: "从库中添加" })).toBeVisible();
  await expect.poll(async () => {
    const viewport = page.viewportSize();
    const backdrop = await page.locator(".composer-attachment-library-backdrop").boundingBox();
    const dialog = await page.locator(".composer-attachment-library-dialog").boundingBox();
    return !!viewport && !!backdrop && !!dialog
      && backdrop.x <= 1
      && backdrop.y <= 1
      && backdrop.width >= viewport.width - 1
      && backdrop.height >= viewport.height - 1
      && dialog.y < viewport.height / 4;
  }, { message: "library dialog should be viewport-centered instead of constrained by the composer" }).toBe(true);
  await page.getByRole("option", { name: /diagram\.png/ }).click();
  await page.getByRole("option", { name: /data\.csv/ }).click();
  await expect(page.getByText("已选 2 个")).toBeVisible();
  await page.getByRole("button", { name: "添加至聊天" }).click();
  await expect(page.getByLabel("Pending attachments").getByText("diagram.png")).toBeVisible();
  await expect(page.getByLabel("Pending attachments").getByText("data.csv")).toBeVisible();
  await page.getByRole("textbox", { name: "Message" }).fill("please inspect two library attachments");
  await page.getByRole("button", { name: "Send" }).click();
  await expect(page.getByText("Attachment received: diagram.png, data.csv")).toBeVisible();
  await expect.poll(async () => {
    const userMessage = await page.locator(".message.user", { hasText: "please inspect two library attachments" }).boundingBox();
    const imageCard = await page.locator(".message.user", { hasText: "please inspect two library attachments" }).locator(".message-asset-attachment.image").boundingBox();
    return !!userMessage && !!imageCard && imageCard.width >= userMessage.width * 0.72;
  }, { message: "image attachment card should fill the message bubble without leaving a large right gap" }).toBe(true);

  await page.getByRole("button", { name: "资源" }).click();
  await page.getByRole("tab", { name: "Attachments" }).click();
  await page.getByRole("button", { name: "Preview notes.md" }).click();
  await expect(page.getByRole("dialog", { name: "notes.md" })).toBeVisible();
  await expect(page.getByRole("document", { name: "notes.md" }).getByRole("heading", { name: "Notes" })).toBeVisible();
  await expect(page.getByRole("document", { name: "notes.md" }).getByText("hello attachment")).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(page.getByRole("dialog", { name: "notes.md" })).toBeHidden();

  await page.getByRole("dialog", { name: "Attachments" }).getByLabel("Close resources").click();
  await page.getByRole("button", { name: "新聊天" }).click();

  await expect(page.getByRole("button", { name: "Use image generation" })).toBeVisible();
  await expect(page.getByRole("button", { name: "Use web search" })).toBeVisible();
  await expect(page.getByRole("button", { name: "Use plan and execute mode" })).toBeVisible();
  await page.getByRole("button", { name: "Use image generation" }).click();
  await expect(page.getByRole("button", { name: "Use image generation" })).toHaveAttribute("aria-pressed", "true");
  await page.getByRole("textbox", { name: "Message" }).fill("draw a blue square");
  await page.getByRole("button", { name: "Send" }).click();

  const artifactWorkspace = page.getByRole("complementary", { name: "Artifact preview" });
  await expect(artifactWorkspace).toBeVisible();
  await expect(artifactWorkspace.getByText("job-1")).toBeVisible();
  await expect(artifactWorkspace.getByText("generated artifact body")).toBeVisible();
  await page.getByRole("button", { name: "Open preview for result.txt" }).click();
  const artifactPreview = page.getByRole("dialog", { name: "result.txt" });
  await expect(artifactPreview).toBeVisible();
  await expect(artifactPreview.getByText("generated artifact body")).toBeVisible();
  await page.getByRole("button", { name: "Close preview" }).click();
  await page.getByLabel("Close artifact preview").click();

  await page.getByRole("button", { name: "资源" }).click();
  await page.getByRole("tab", { name: "Artifacts" }).click();
  const artifactsDialog = page.getByRole("dialog", { name: "Artifacts" });
  await expect(artifactsDialog.locator(".asset-row", { hasText: "result.txt" })).toBeVisible();
  await artifactsDialog.locator(".asset-row-main", { hasText: "result.txt" }).click();
  await expect(page.getByRole("complementary", { name: "Artifact preview" })).toBeVisible();
  await expect(page.getByRole("complementary", { name: "Artifact preview" }).getByText("generated artifact body")).toBeVisible();
  await page.getByLabel("Close artifact preview").click();

  await page.getByRole("button", { name: "资源" }).click();
  await page.getByRole("tab", { name: "Jobs" }).click();
  const jobsDialog = page.getByRole("dialog", { name: "Jobs" });
  await expect(jobsDialog.locator(".job-summary", { hasText: "draw a blue square" })).toBeVisible();
  await jobsDialog.locator(".job-summary", { hasText: "draw a blue square" }).click();
  const jobWorkspace = page.getByRole("complementary", { name: "Job details" });
  await expect(jobWorkspace).toBeVisible();
  await expect(jobWorkspace.getByLabel("Job metadata").getByText("job-1")).toBeVisible();
  await expect(jobWorkspace.getByText("Events", { exact: true })).toBeVisible();
  await expect(jobWorkspace.locator(".timeline-row", { hasText: "done" })).toBeVisible();
  await page.getByRole("button", { name: "新聊天" }).click();
  await expect(jobWorkspace).toBeHidden();

  await page.getByRole("button", { name: "搜索聊天" }).click();
  await page.getByRole("textbox", { name: "Search across all sessions" }).fill("playwright");
  await page.getByRole("dialog", { name: "Search across all sessions" }).locator(".global-search-result", { hasText: "hello from playwright" }).first().click();
  await expect(page.getByRole("heading", { name: "hello from playwright" })).toBeVisible();

  await page.getByRole("button", { name: "Settings" }).click();
  await page.getByRole("menuitem", { name: "Settings" }).click();
  await expect(page.getByRole("dialog", { name: "Settings" })).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(page.getByRole("dialog", { name: "Settings" })).toBeHidden();

  await page.getByRole("button", { name: "Settings" }).click();
  await page.getByRole("menuitem", { name: "Manage Memory" }).click();
  await expect(page.getByRole("dialog", { name: "Memory" })).toBeVisible();
  await page.getByRole("button", { name: "Close memory" }).click();
  await expect(page.getByRole("dialog", { name: "Memory" })).toBeHidden();

  const longComposerText = "asdaasdfsafasfsafasfas1c2e`2c1111111112wasdasd".repeat(10);
  await page.getByRole("textbox", { name: "Message" }).fill(longComposerText);
  const composerBox = await page.locator(".composer").boundingBox();
  const textareaBox = await page.locator(".composer textarea").boundingBox();
  const actionsBox = await page.locator(".composer-actions").boundingBox();
  expect(composerBox).not.toBeNull();
  expect(textareaBox).not.toBeNull();
  expect(actionsBox).not.toBeNull();
  expect(textareaBox!.width / composerBox!.width).toBeGreaterThan(0.9);
  expect(actionsBox!.y).toBeGreaterThanOrEqual(textareaBox!.y + textareaBox!.height - 2);

  expect(api.sessions.some((session) => session.messages.some((message) => message.content?.includes("hello from playwright")))).toBe(true);
  expect(api.chatPayloads.some((payload) => payload.content === "hello from playwright" && payload.agent_mode === "plan_execute")).toBe(true);
  expect(api.chatPayloads.some((payload) => payload.content === "please inspect the existing attachment" && payload.attachment_ids?.includes("attachment-1"))).toBe(true);
  expect(api.chatPayloads.some((payload) => payload.content.startsWith("/vertex-image-artifact") && payload.thinking_mode !== true)).toBe(true);
});

test("keeps sent chat text visible when the stream fails", async ({ page }) => {
  await mockAgentAPI(page, { failChat: true });

  await page.goto("/");
  await page.getByLabel("Email").fill("e2e@example.com");
  await page.getByLabel("Password").fill("password123");
  await page.getByRole("button", { name: "Login" }).last().click();

  await page.getByRole("textbox", { name: "Message" }).fill("this should stay visible");
  await page.getByRole("button", { name: "Send" }).click();

  await expect(page.locator(".message.user .message-text", { hasText: "this should stay visible" })).toBeVisible();
  await expect(page.getByText(/Message delivery failed/).first()).toBeVisible();
});

test("shows streamed agent activity as a collapsible timeline", async ({ page }) => {
  await mockAgentAPI(page);

  await page.goto("/");
  await page.getByLabel("Email").fill("e2e@example.com");
  await page.getByLabel("Password").fill("password123");
  await page.getByRole("button", { name: "Login" }).last().click();

  await page.getByRole("textbox", { name: "Message" }).fill("show agent activity timeline");
  await page.getByRole("button", { name: "Send" }).click();

  const activity = page.locator(".agent-activity");
  await expect(activity).toBeVisible();
  await expect(activity).toHaveClass(/complete/);
  await expect(activity.locator("summary")).toContainText("Agent activity");
  await expect(activity.locator("summary")).toContainText("Completed");
  await expect(activity.locator(".agent-activity-list")).toBeHidden();

  await activity.locator("summary").click();
  await expect(page.getByText("Started plan-and-execute")).toBeVisible();
  await expect(page.getByText("Gather facts · WebSearch").first()).toBeVisible();
  await expect(page.getByText("Web search returned source notes")).toBeVisible();
  await expect(activity.locator(".agent-activity-item.running")).toHaveCount(0);
  await expect(activity.locator(".agent-activity-item.default")).toHaveCount(0);
  await expect(activity.locator(".agent-activity-item.succeeded").first()).toBeVisible();
  await expect.poll(async () => activity.locator(".agent-activity-list").evaluate((node) => {
    const style = window.getComputedStyle(node);
    return style.overflowY === "auto" && node.scrollHeight > node.clientHeight;
  }), { message: "expanded agent activity should scroll internally when the event list is long" }).toBe(true);
});

test("opens a fresh chat after deleting the active session", async ({ page }) => {
  await mockAgentAPI(page, {
    initialSessions: [{
      id: "20260509T115900Z-old",
      working_dir: "/tmp",
      started_at: now,
      updated_at: "2026-05-09T11:59:00.000Z",
      messages: [
        { role: "user", content: "old history should not become active", created_at: now, message_index: 0 },
        { role: "assistant", content: "old session answer", created_at: now, message_index: 1 }
      ]
    }]
  });

  await page.goto("/");
  await page.getByLabel("Email").fill("e2e@example.com");
  await page.getByLabel("Password").fill("password123");
  await page.getByRole("button", { name: "Login" }).last().click();
  await expect(page.getByText("old session answer")).toBeVisible();

  await page.getByRole("button", { name: "新聊天" }).click();
  await expect(page.locator(".empty-state")).toBeVisible();
  await page.getByRole("button", { name: "Use image generation" }).click();
  await page.getByRole("textbox", { name: "Message" }).fill("delete this active session");
  await page.getByRole("button", { name: "Send" }).click();
  await expect(page.getByRole("complementary", { name: "Artifact preview" })).toBeVisible();
  await page.getByLabel("Close artifact preview").click();
  await page.getByRole("button", { name: "资源" }).click();
  await page.getByRole("tab", { name: "Jobs" }).click();
  await page.getByRole("dialog", { name: "Jobs" }).locator(".job-summary", { hasText: "delete this active session" }).click();
  await expect(page.getByRole("complementary", { name: "Job details" })).toBeVisible();

  await page.locator(".session-list-item.active .session-delete").click();
  await page.getByRole("dialog", { name: "Remove session?" }).getByRole("button", { name: "Remove" }).click();

  await expect(page.locator(".empty-state")).toBeVisible();
  await expect(page.getByRole("complementary", { name: "Job details" })).toBeHidden();
  await expect(page.locator(".message")).toHaveCount(0);
  await expect(page.locator(".session-list-item.active", { hasText: "old history should not become active" })).toHaveCount(0);
});

test("covers admin console smoke after the panel split", async ({ page }) => {
  await mockAgentAPI(page);

  await page.goto("/");
  await page.getByLabel("Email").fill("admin@example.com");
  await page.getByLabel("Password").fill("password123");
  await page.getByRole("button", { name: "Login" }).last().click();
  await expect(page.getByRole("heading", { name: /Hi E2E User/i })).toBeVisible();

  await page.goto("/admin");
  await expect(page.getByRole("heading", { name: "Skills" })).toBeVisible();
  await page.getByLabel("Admin token").fill("test-admin-token");
  await page.getByRole("button", { name: "Load skill data" }).click();
  await expect(page.getByRole("button", { name: /DOCX Builder/i })).toBeVisible();
  await expect(page.getByText("Runs")).toBeVisible();

  await page.getByRole("tab", { name: /Users/i }).click();
  await expect(page.getByRole("button", { name: /Admin User/i })).toBeVisible();

  await page.getByRole("tab", { name: /Health & cost/i }).click();
  await expect(page.getByRole("heading", { name: "Runtime snapshot" })).toBeVisible();
  await expect(page.getByText("gemini")).toBeVisible();
});

async function mockAgentAPI(page: Page, options: { failChat?: boolean; initialSessions?: Session[] } = {}) {
  const sessionA: Session = {
    id: "20260509T120000Z-e2e",
    working_dir: "/tmp",
    started_at: now,
    updated_at: now,
    messages: []
  };
  let createdSessionCount = 0;
  const state = {
    sessions: cloneSessions(options.initialSessions || [sessionA]),
    attachments: [] as Asset[],
    artifacts: [] as Asset[],
    jobs: [] as Job[],
    chatPayloads: [] as Array<{ content: string; attachment_ids?: string[]; thinking_mode?: boolean; agent_mode?: string }>
  };

  await page.route("**/readyz?**", (route) => json(route, { status: "ok", checks: [] }));
  await page.route("**/v1/auth/register", (route) => json(route, authSession("e2e@example.com"), 201));
  await page.route("**/v1/auth/login", (route) => json(route, authSession("e2e@example.com")));
  await page.route("**/v1/auth/refresh", (route) => json(route, authSession("e2e@example.com")));
  await page.route("**/v1/auth/logout", (route) => json(route, {}));
  await page.route("**/v1/auth/me", (route) => json(route, { user: authSession("e2e@example.com").user }));
  await page.route("**/v1/memory/settings", (route) => json(route, {
    enabled: true,
    capture_enabled: true,
    context_enabled: true
  }));
  await page.route("**/v1/personalization", (route) => json(route, {
    profile: {},
    style: {},
    traits: {},
    custom_instructions: "",
    feature_flags: {}
  }));
  await page.route("**/v1/memory?**", (route) => json(route, { items: [] }));
  await page.route("**/v1/memory/maintenance", (route) => json(route, { actions: [] }));
  await page.route("**/v1/skills", (route) => json(route, {
    skills: [
      { name: "vertex-image-artifact", description: "Generate an artifact", run_as_job: true }
    ]
  }));
  await page.route("**/v1/sessions?**", async (route) => {
    return json(route, state.sessions);
  });

  await page.route("**/v1/sessions", async (route) => {
    if (route.request().method() === "POST") {
      createdSessionCount += 1;
      const session: Session = {
        id: `20260509T12010${createdSessionCount}Z-e2e`,
        working_dir: "/tmp",
        started_at: now,
        updated_at: now,
        messages: []
      };
      state.sessions.unshift(session);
      return json(route, session, 201);
    }
    return json(route, state.sessions);
  });

  await page.route(/.*\/v1\/sessions\/[^/]+$/, async (route) => {
    const id = route.request().url().split("/v1/sessions/")[1].split("?")[0];
    const sessionID = decodeURIComponent(id);
    if (route.request().method() === "DELETE") {
      state.sessions = state.sessions.filter((session) => session.id !== sessionID);
      state.jobs = state.jobs.filter((job) => job.session_id !== sessionID);
      state.attachments = state.attachments.filter((asset) => asset.session_id !== sessionID);
      state.artifacts = state.artifacts.filter((asset) => asset.session_id !== sessionID);
      return json(route, {});
    }
    const session = state.sessions.find((item) => item.id === sessionID);
    if (!session) return json(route, { error: "session not found" }, 404);
    return json(route, session);
  });

  await page.route(/.*\/v1\/sessions\/[^/]+\/messages$/, async (route) => {
    if (options.failChat) return route.abort("failed");
    const sessionID = decodeURIComponent(route.request().url().split("/v1/sessions/")[1].split("/messages")[0]);
    const session = state.sessions.find((item) => item.id === sessionID) || state.sessions[0];
    const payload = await route.request().postDataJSON() as { content: string; attachment_ids?: string[]; thinking_mode?: boolean; agent_mode?: string };
    state.chatPayloads.push(payload);
    const payloadAttachments = state.attachments
      .filter((attachment) => payload.attachment_ids?.includes(attachment.id))
      .map((attachment) => ({
        id: attachment.id,
        file_type: attachment.content_type.startsWith("image/") ? "image" : "file",
        mime_type: attachment.content_type,
        file_name: attachment.filename,
        file_size: attachment.size_bytes
      }));
    const userMessage = {
      role: "user",
      content: payload.content,
      created_at: now,
      message_index: session.messages.length,
      attachments: payloadAttachments
    };
    session.messages.push(userMessage);

    if (payload.content.startsWith("/vertex-image-artifact")) {
      const job: Job = {
        id: "job-1",
        session_id: session.id,
        type: "skill",
        status: "succeeded",
        content: payload.content,
        created_at: now,
        updated_at: now
      };
      state.jobs = [job];
      state.artifacts = [{
        id: "artifact-1",
        kind: "artifact",
        session_id: session.id,
        job_id: job.id,
        filename: "result.txt",
        content_type: "text/plain",
        size_bytes: 23,
        created_at: now
      }];
      session.messages.push({ role: "assistant", content: "Generated artifact result.txt", created_at: now, message_index: session.messages.length });
      return sse(route, [
        { event: "job", data: { type: "job", job_id: job.id, job, session_id: session.id } }
      ]);
    }

    const attachmentNames = state.attachments
      .filter((attachment) => payload.attachment_ids?.includes(attachment.id))
      .map((attachment) => attachment.filename);
    const response = attachmentNames.length ? `Attachment received: ${attachmentNames.join(", ")}` : `Echo: ${payload.content}`;
    session.messages.push({ role: "assistant", content: response, created_at: now, message_index: session.messages.length });
    if (payload.content === "show agent activity timeline") {
      return sse(route, [
        { event: "start", data: { type: "start", session_id: session.id } },
        { event: "deep_agent_started", data: { type: "deep_agent_started", session_id: session.id, content: "Plan-and-execute task started" } },
        {
          event: "deep_agent_action_started",
          data: {
            type: "deep_agent_action_started",
            session_id: session.id,
            data: {
              step_id: "gather",
              step_title: "Gather facts",
              skill_name: "WebSearch",
              status: "running"
            }
          }
        },
        {
          event: "deep_agent_action_succeeded",
          data: {
            type: "deep_agent_action_succeeded",
            session_id: session.id,
            data: {
              step_id: "gather",
              step_title: "Gather facts",
              skill_name: "WebSearch",
              result_status: "succeeded",
              summary: "Web search returned source notes"
            }
          }
        },
        ...Array.from({ length: 26 }, (_, index) => ({
          event: "workflow_step_succeeded",
          data: {
            type: "workflow_step_succeeded",
            session_id: session.id,
            data: {
              step_id: `step-${index + 1}`,
              step_title: `Verification step ${index + 1}`,
              status: "succeeded"
            }
          }
        })),
        { event: "message", data: { type: "message", role: "assistant", content: response, session_id: session.id } },
        { event: "done", data: { type: "done", session_id: session.id } }
      ]);
    }
    return sse(route, [
      { event: "message", data: { type: "message", role: "assistant", content: response, session_id: session.id } },
      { event: "done", data: { type: "done", session_id: session.id } }
    ]);
  });

  await page.route("**/v1/attachments", async (route) => {
    if (route.request().method() === "POST") {
      const asset: Asset = {
        id: "attachment-1",
        kind: "attachment",
        session_id: state.sessions[0].id,
        filename: "notes.md",
        content_type: "text/markdown",
        size_bytes: 26,
        created_at: now
      };
      state.attachments = [
        asset,
        {
          id: "attachment-2",
          kind: "attachment",
          session_id: state.sessions[0].id,
          filename: "diagram.png",
          content_type: "image/png",
          size_bytes: 84,
          created_at: "2026-05-09T11:59:00.000Z"
        },
        {
          id: "attachment-3",
          kind: "attachment",
          session_id: state.sessions[0].id,
          filename: "data.csv",
          content_type: "text/csv",
          size_bytes: 42,
          created_at: "2026-05-09T11:58:00.000Z"
        },
        {
          id: "attachment-4",
          kind: "attachment",
          session_id: state.sessions[0].id,
          filename: "brief.pdf",
          content_type: "application/pdf",
          size_bytes: 1024,
          created_at: "2026-05-09T11:57:00.000Z"
        },
        {
          id: "attachment-5",
          kind: "attachment",
          session_id: state.sessions[0].id,
          filename: "slides.pptx",
          content_type: "application/vnd.openxmlformats-officedocument.presentationml.presentation",
          size_bytes: 2048,
          created_at: "2026-05-09T11:56:00.000Z"
        },
        {
          id: "attachment-6",
          kind: "attachment",
          session_id: state.sessions[0].id,
          filename: "oldest.txt",
          content_type: "text/plain",
          size_bytes: 12,
          created_at: "2026-05-09T11:55:00.000Z"
        }
      ];
      return json(route, asset, 201);
    }
    return json(route, { attachments: state.attachments });
  });
  await page.route("**/v1/attachments?**", (route) => json(route, { attachments: state.attachments }));
  await page.route(/.*\/v1\/attachments\/attachment-1(?:\?.*)?$/, (route) => {
    expect(new URL(route.request().url()).searchParams.has("token")).toBe(false);
    expect(route.request().headers().authorization).toBe("Bearer access-token");
    return text(route, "# Notes\n\nhello attachment", "text/markdown");
  });

  await page.route("**/v1/artifacts", (route) => json(route, { artifacts: state.artifacts }));
  await page.route("**/v1/artifacts?**", (route) => json(route, { artifacts: state.artifacts }));
  await page.route(/.*\/v1\/artifacts\/artifact-1(?:\?.*)?$/, (route) => {
    expect(new URL(route.request().url()).searchParams.has("token")).toBe(false);
    expect(route.request().headers().authorization).toBe("Bearer access-token");
    return text(route, "generated artifact body", "text/plain");
  });

  await page.route("**/v1/jobs/job-1/events?stream=1**", (route) => sse(route, [
    { id: "evt-1", event: "start", data: { type: "start", session_id: state.sessions[0].id } },
    { id: "evt-2", event: "done", data: { type: "done", session_id: state.sessions[0].id } }
  ]));
  await page.route("**/v1/jobs/job-1/events", (route) => json(route, {
    events: [
      { id: "evt-1", job_id: "job-1", type: "start", event: { type: "start" }, created_at: now },
      { id: "evt-2", job_id: "job-1", type: "done", event: { type: "done", session_id: state.sessions[0].id }, created_at: now }
    ]
  }));
  await page.route("**/v1/jobs", (route) => json(route, { jobs: state.jobs }));
  await page.route("**/v1/jobs?**", (route) => json(route, { jobs: state.jobs }));

  await page.route("**/v1/search/messages?**", (route) => json(route, {
    items: state.sessions.flatMap((session) => session.messages.map((message, index) => ({
      session_id: session.id,
      message_index: message.message_index ?? index,
      role: message.role,
      content: message.content,
      snippet: message.content || "",
      session_title: message.content || session.id,
      created_at: message.created_at || now
    }))).filter((item) => item.content?.toLowerCase().includes("playwright"))
  }));

  await mockAdminAPI(page);

  return state;
}

async function mockAdminAPI(page: Page) {
  const adminSkill = {
    name: "docx",
    display_name: "DOCX Builder",
    description: "Create Word documents",
    category: "documents",
    status: "published",
    source: "registry",
    skill_root: ".claude/skills/docx",
    version: "1.0.0",
    run_as_job: true,
    produces_artifacts: true,
    content_hash: "abcdef1234567890",
    created_at: now,
    updated_at: now,
    metadata: { policy: { allowed_tools: ["Bash"], sandbox: { runner: "docker" } } }
  };
  const adminUser = {
    id: "admin-user-1",
    email: "admin@example.com",
    display_name: "Admin User",
    status: "active",
    email_verified: true,
    created_at: now,
    updated_at: now,
    refresh_token_count: 2,
    active_refresh_token_count: 1,
    last_login_at: now
  };

  await page.route("**/v1/admin/skills", (route) => json(route, { skills: [adminSkill] }));
  await page.route("**/v1/admin/skills/docx/review", (route) => json(route, {
    review: { passed: true, issues: [], checked_at: now }
  }));
  await page.route("**/v1/admin/skills/docx/versions", (route) => json(route, {
    versions: [{ skill_name: "docx", version: "1.0.0", content_hash: "abcdef1234567890", created_at: now, published_at: now }]
  }));
  await page.route("**/v1/admin/skills/docx/analytics", (route) => json(route, {
    summary: { total: 4, succeeded: 4, failed: 0, failure_rate: 0, average_latency_ms: 1234 }
  }));
  await page.route("**/v1/admin/skills/docx/executions?**", (route) => json(route, {
    executions: [{ id: "exec-1", skill_name: "docx", status: "completed", result: "ok", started_at: now, completed_at: now, latency_ms: 1234 }]
  }));
  await page.route("**/v1/admin/users?**", (route) => json(route, { users: [adminUser] }));
  await page.route("**/v1/admin/ops/health", (route) => json(route, {
    readiness: { status: "ok", checks: [{ name: "sql", status: "ok" }] },
    llm: {
      status: "ok",
      backends: [{ provider: "google", model: "gemini", healthy: true, latency_ms: 250 }],
      config: { chat_model: "gemini-2.5-flash", live_model: "live-unchanged" }
    }
  }));
  await page.route("**/v1/admin/ops/llm-usage?**", (route) => json(route, {
    usage: {
      since: now,
      requests: 3,
      total_tokens: 1200,
      estimated_cost_usd: 0.012,
      average_latency_ms: 900,
      by_provider: [{ provider: "google", model: "gemini", requests: 3, total_tokens: 1200, estimated_cost_usd: 0.012, status: "ok" }],
      recent: []
    }
  }));
}

function authSession(email: string) {
  return {
    user: {
      id: "user-1",
      email,
      display_name: "E2E User",
      status: "active",
      created_at: now
    },
    access_token: "access-token",
    refresh_token: "refresh-token",
    csrf_token: "csrf-token",
    expires_at: "2099-01-01T00:00:00.000Z"
  };
}

function cloneSessions(sessions: Session[]): Session[] {
  return sessions.map((session) => ({
    ...session,
    messages: session.messages.map((message) => ({ ...message }))
  }));
}

function json(route: Route, body: unknown, status = 200) {
  return route.fulfill({
    status,
    contentType: "application/json",
    body: JSON.stringify(body)
  });
}

function text(route: Route, body: string, contentType: string) {
  return route.fulfill({ status: 200, contentType, body });
}

function sse(route: Route, events: Array<{ id?: string; event: string; data: unknown }>) {
  const body = events.map((item) => [
    item.id ? `id: ${item.id}` : "",
    `event: ${item.event}`,
    `data: ${JSON.stringify(item.data)}`
  ].filter(Boolean).join("\n")).join("\n\n");
  return route.fulfill({
    status: 200,
    headers: {
      "content-type": "text/event-stream",
      "cache-control": "no-cache"
    },
    body: `${body}\n`
  });
}
