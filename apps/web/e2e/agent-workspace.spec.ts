import { expect, test, type Page, type Route } from "@playwright/test";

const now = "2026-05-09T12:00:00.000Z";

type Message = {
  role: string;
  content?: string;
  created_at?: string;
  message_index?: number;
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
  await page.getByRole("button", { name: "Create Account" }).click();

  await expect(page.getByRole("heading", { name: "New conversation" })).toBeVisible();

  await page.getByRole("button", { name: "New session" }).click();
  await expect(page.getByRole("heading", { name: "20260509T120100Z-e2e" })).toBeVisible();

  await page.getByRole("textbox", { name: "Message" }).fill("hello from playwright");
  await page.getByRole("button", { name: "Send" }).click();
  await expect(page.getByText("Echo: hello from playwright")).toBeVisible();

  await page.locator("input[type=file]").setInputFiles({
    name: "notes.md",
    mimeType: "text/markdown",
    buffer: Buffer.from("# Notes\n\nhello attachment")
  });
  await expect(page.getByLabel("Pending attachments").getByText("notes.md")).toBeVisible();
  await page.getByRole("textbox", { name: "Message" }).fill("please inspect the attachment");
  await page.getByRole("button", { name: "Send" }).click();
  await expect(page.getByText("Attachment received: notes.md")).toBeVisible();

  await page.getByRole("tab", { name: "Attachments" }).click();
  await page.getByRole("button", { name: "Preview notes.md" }).click();
  await expect(page.getByRole("dialog", { name: "notes.md" })).toBeVisible();
  await expect(page.getByText("# Notes")).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(page.getByRole("dialog", { name: "notes.md" })).toBeHidden();

  await page.getByRole("tab", { name: "Skills" }).click();
  await page.getByRole("button", { name: /vertex-image-artifact/i }).click();
  await page.getByRole("textbox", { name: "Message" }).fill("/vertex-image-artifact draw a blue square");
  await page.getByRole("button", { name: "Send" }).click();
  await expect(page.getByText("succeeded", { exact: true })).toBeVisible();

  await page.getByRole("tab", { name: "Artifacts" }).click();
  await page.getByRole("button", { name: "Preview result.txt" }).click();
  await expect(page.getByRole("dialog", { name: "result.txt" })).toBeVisible();
  await expect(page.getByText("generated artifact body")).toBeVisible();
  await page.getByRole("button", { name: "Close preview" }).click();

  await page.getByRole("button", { name: "Search messages" }).click();
  await page.getByRole("textbox", { name: "Search across all sessions" }).fill("playwright");
  await page.getByRole("dialog", { name: "Search across all sessions" }).locator(".global-search-result", { hasText: "hello from playwright" }).first().click();
  await expect(page.getByRole("heading", { name: "hello from playwright" })).toBeVisible();

  expect(api.sessions.some((session) => session.messages.some((message) => message.content?.includes("hello from playwright")))).toBe(true);
});

test("keeps sent chat text visible when the stream fails", async ({ page }) => {
  await mockAgentAPI(page, { failChat: true });

  await page.goto("/");
  await page.getByLabel("Email").fill("e2e@example.com");
  await page.getByLabel("Password").fill("password123");
  await page.getByRole("button", { name: "Login" }).last().click();

  await page.getByRole("textbox", { name: "Message" }).fill("this should stay visible");
  await page.getByRole("button", { name: "Send" }).click();

  await expect(page.getByText("this should stay visible")).toBeVisible();
  await expect(page.getByText(/Message delivery failed/).first()).toBeVisible();
});

async function mockAgentAPI(page: Page, options: { failChat?: boolean } = {}) {
  const sessionA: Session = {
    id: "20260509T120000Z-e2e",
    working_dir: "/tmp",
    started_at: now,
    updated_at: now,
    messages: []
  };
  const sessionB: Session = {
    id: "20260509T120100Z-e2e",
    working_dir: "/tmp",
    started_at: now,
    updated_at: now,
    messages: []
  };
  const state = {
    sessions: [sessionA] as Session[],
    attachments: [] as Asset[],
    artifacts: [] as Asset[],
    jobs: [] as Job[]
  };

  await page.route("**/readyz?**", (route) => json(route, { status: "ok", checks: [] }));
  await page.route("**/v1/auth/register", (route) => json(route, authSession("e2e@example.com"), 201));
  await page.route("**/v1/auth/login", (route) => json(route, authSession("e2e@example.com")));
  await page.route("**/v1/auth/refresh", (route) => json(route, authSession("e2e@example.com")));
  await page.route("**/v1/auth/logout", (route) => json(route, {}));
  await page.route("**/v1/auth/me", (route) => json(route, { user: authSession("e2e@example.com").user }));
  await page.route("**/v1/skills", (route) => json(route, {
    skills: [
      { name: "vertex-image-artifact", description: "Generate an artifact", run_as_job: true }
    ]
  }));

  await page.route("**/v1/sessions", async (route) => {
    if (route.request().method() === "POST") {
      if (!state.sessions.some((session) => session.id === sessionB.id)) state.sessions.unshift(sessionB);
      return json(route, sessionB, 201);
    }
    return json(route, state.sessions);
  });

  await page.route(/.*\/v1\/sessions\/[^/]+$/, async (route) => {
    const id = route.request().url().split("/v1/sessions/")[1].split("?")[0];
    return json(route, state.sessions.find((session) => session.id === decodeURIComponent(id)) || state.sessions[0]);
  });

  await page.route(/.*\/v1\/sessions\/[^/]+\/messages$/, async (route) => {
    if (options.failChat) return route.abort("failed");
    const sessionID = decodeURIComponent(route.request().url().split("/v1/sessions/")[1].split("/messages")[0]);
    const session = state.sessions.find((item) => item.id === sessionID) || state.sessions[0];
    const payload = await route.request().postDataJSON() as { content: string; attachment_ids?: string[] };
    const userMessage = { role: "user", content: payload.content, created_at: now, message_index: session.messages.length };
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
      state.attachments = [asset];
      return json(route, asset, 201);
    }
    return json(route, { attachments: state.attachments });
  });
  await page.route("**/v1/attachments?**", (route) => json(route, { attachments: state.attachments }));
  await page.route("**/v1/attachments/attachment-1?**", (route) => text(route, "# Notes\n\nhello attachment", "text/markdown"));

  await page.route("**/v1/artifacts", (route) => json(route, { artifacts: state.artifacts }));
  await page.route("**/v1/artifacts?**", (route) => json(route, { artifacts: state.artifacts }));
  await page.route("**/v1/artifacts/artifact-1?**", (route) => text(route, "generated artifact body", "text/plain"));

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

  return state;
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
    `data: ${JSON.stringify(item.data)}`,
    ""
  ].filter(Boolean).join("\n")).join("\n");
  return route.fulfill({
    status: 200,
    headers: {
      "content-type": "text/event-stream",
      "cache-control": "no-cache"
    },
    body: `${body}\n`
  });
}
