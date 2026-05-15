package agentruntime

const appHTML = `<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Agent Runtime</title>
  <style>
    :root {
      color-scheme: light;
      --bg: #f6f7f8;
      --panel: #ffffff;
      --line: #d8dde3;
      --text: #182026;
      --muted: #65717c;
      --accent: #0f766e;
      --accent-2: #2563eb;
      --danger: #b42318;
      --soft: #eef5f4;
      --code: #102a43;
    }
    * { box-sizing: border-box; }
    html, body { margin: 0; height: 100%; }
    body {
      font-family: ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
      background: var(--bg);
      color: var(--text);
      overflow: hidden;
    }
    button, input, textarea, select {
      font: inherit;
    }
    button {
      border: 1px solid var(--line);
      background: var(--panel);
      color: var(--text);
      min-height: 36px;
      padding: 0 12px;
      border-radius: 6px;
      cursor: pointer;
    }
    button.primary {
      border-color: var(--accent);
      background: var(--accent);
      color: #fff;
    }
    button.danger {
      border-color: #f1b4ae;
      color: var(--danger);
    }
    button:disabled {
      opacity: 0.55;
      cursor: not-allowed;
    }
    input, textarea, select {
      width: 100%;
      border: 1px solid var(--line);
      background: #fff;
      color: var(--text);
      border-radius: 6px;
      padding: 8px 10px;
      outline: none;
    }
    textarea {
      min-height: 52px;
      max-height: 180px;
      resize: vertical;
      line-height: 1.45;
    }
    input:focus, textarea:focus, select:focus {
      border-color: var(--accent);
      box-shadow: 0 0 0 3px rgba(15, 118, 110, 0.12);
    }
    .shell {
      display: grid;
      grid-template-columns: 320px minmax(0, 1fr) 280px;
      height: 100vh;
      min-width: 960px;
    }
    .auth-screen {
      position: fixed;
      inset: 0;
      display: grid;
      place-items: center;
      padding: 24px;
      background: var(--bg);
      z-index: 10;
    }
    .auth-screen.hidden { display: none; }
    .auth-panel {
      width: min(420px, 100%);
      background: var(--panel);
      border: 1px solid var(--line);
      border-radius: 8px;
      padding: 22px;
      display: grid;
      gap: 14px;
    }
    .auth-title {
      display: grid;
      gap: 4px;
    }
    .auth-title strong {
      font-size: 20px;
    }
    .auth-title span {
      color: var(--muted);
      font-size: 13px;
    }
    .tabs {
      display: grid;
      grid-template-columns: 1fr 1fr;
      gap: 8px;
    }
    .tabs button.active {
      border-color: var(--accent);
      color: var(--accent);
      background: var(--soft);
    }
    .sidebar, .inspector {
      background: var(--panel);
      border-right: 1px solid var(--line);
      min-height: 0;
      display: flex;
      flex-direction: column;
    }
    .inspector {
      border-right: 0;
      border-left: 1px solid var(--line);
    }
    .brand {
      height: 56px;
      padding: 0 16px;
      display: flex;
      align-items: center;
      gap: 10px;
      border-bottom: 1px solid var(--line);
      font-weight: 700;
    }
    .mark {
      width: 26px;
      height: 26px;
      border-radius: 6px;
      background: var(--accent);
    }
    .section {
      padding: 14px 16px;
      border-bottom: 1px solid var(--line);
    }
    .section h2 {
      font-size: 12px;
      margin: 0 0 8px;
      color: var(--muted);
      text-transform: uppercase;
      letter-spacing: 0;
    }
    .field {
      display: grid;
      gap: 6px;
      margin-bottom: 10px;
    }
    .field label {
      color: var(--muted);
      font-size: 13px;
    }
    .row {
      display: flex;
      align-items: center;
      gap: 8px;
    }
    .row > * { min-width: 0; }
    .status {
      display: inline-flex;
      align-items: center;
      gap: 6px;
      color: var(--muted);
      font-size: 13px;
      min-height: 24px;
    }
    .dot {
      width: 8px;
      height: 8px;
      border-radius: 999px;
      background: #9aa4ad;
    }
    .dot.ok { background: var(--accent); }
    .dot.busy { background: var(--accent-2); }
    .dot.err { background: var(--danger); }
    .sessions, .skills {
      overflow: auto;
      min-height: 0;
    }
    .inspector-panels {
      min-height: 0;
      overflow: auto;
    }
    .drawer {
      border-bottom: 1px solid var(--line);
      background: #fff;
    }
    .drawer-toggle {
      width: 100%;
      border: 0;
      border-radius: 0;
      min-height: 46px;
      padding: 0 16px;
      display: grid;
      grid-template-columns: minmax(0, 1fr) auto;
      align-items: center;
      gap: 10px;
      text-align: left;
      background: #fff;
    }
    .drawer-title {
      min-width: 0;
      display: flex;
      align-items: center;
      gap: 8px;
      color: var(--muted);
      font-size: 12px;
      font-weight: 700;
      text-transform: uppercase;
      letter-spacing: 0;
    }
    .drawer-title::before {
      content: "+";
      width: 16px;
      color: var(--accent);
      font-weight: 700;
    }
    .drawer.open .drawer-title::before {
      content: "-";
    }
    .drawer-count {
      color: var(--muted);
      background: #edf1f5;
      border: 1px solid var(--line);
      border-radius: 999px;
      min-width: 24px;
      padding: 1px 7px;
      text-align: center;
      font-size: 11px;
      line-height: 18px;
    }
    .drawer-body {
      display: none;
      padding: 0 16px 14px;
    }
    .drawer.open .drawer-body {
      display: block;
    }
    .drawer-body .section {
      border: 0;
      padding: 0;
    }
    .drawer-body .field:last-child {
      margin-bottom: 0;
    }
    .drawer-list {
      max-height: 260px;
      margin: 0 -16px -14px;
      border-top: 1px solid var(--line);
    }
    .timeline {
      display: grid;
      gap: 8px;
      max-height: 300px;
      overflow: auto;
      margin-top: 10px;
      padding-right: 2px;
    }
    .timeline-event {
      border: 1px solid var(--line);
      border-radius: 6px;
      background: #fff;
      padding: 8px 9px;
      display: grid;
      gap: 4px;
    }
    .timeline-event strong {
      font-size: 12px;
    }
    .timeline-event span {
      color: var(--muted);
      font-size: 11px;
      overflow-wrap: anywhere;
    }
    .pill {
      display: inline-flex;
      align-items: center;
      min-height: 22px;
      border: 1px solid var(--line);
      border-radius: 999px;
      padding: 1px 8px;
      color: var(--muted);
      font-size: 11px;
      background: #f8fafc;
    }
    .pill.running { border-color: #bfdbfe; color: #1d4ed8; background: #eff6ff; }
    .pill.succeeded { border-color: #a7f3d0; color: #047857; background: #ecfdf5; }
    .pill.failed, .pill.cancelled { border-color: #fecaca; color: var(--danger); background: #fff4f2; }
    .empty {
      padding: 12px 16px;
      color: var(--muted);
      font-size: 12px;
    }
    .item {
      width: 100%;
      text-align: left;
      border: 0;
      border-bottom: 1px solid var(--line);
      border-radius: 0;
      padding: 10px 16px;
      min-height: 54px;
      background: #fff;
    }
    .item.active {
      background: var(--soft);
      border-left: 3px solid var(--accent);
      padding-left: 13px;
    }
    .item .title {
      display: block;
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
      font-weight: 600;
      font-size: 13px;
    }
    .item .sub {
      display: block;
      margin-top: 3px;
      color: var(--muted);
      font-size: 12px;
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
    }
    .main {
      min-width: 0;
      min-height: 0;
      display: grid;
      grid-template-rows: 56px minmax(0, 1fr) auto;
    }
    .topbar {
      border-bottom: 1px solid var(--line);
      background: rgba(255,255,255,0.84);
      display: flex;
      align-items: center;
      justify-content: space-between;
      padding: 0 18px;
      gap: 14px;
    }
    .titleline {
      min-width: 0;
      display: grid;
      gap: 2px;
    }
    .titleline strong {
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
    }
    .titleline span {
      color: var(--muted);
      font-size: 12px;
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
    }
    .messages {
      min-height: 0;
      overflow: auto;
      padding: 18px;
      display: flex;
      flex-direction: column;
      gap: 12px;
    }
    .msg {
      max-width: min(820px, 86%);
      border: 1px solid var(--line);
      border-radius: 8px;
      padding: 11px 12px;
      background: #fff;
      white-space: pre-wrap;
      line-height: 1.48;
      overflow-wrap: anywhere;
    }
    .msg.user {
      align-self: flex-end;
      background: #e8f0ff;
      border-color: #c7d7fe;
    }
    .msg.assistant {
      align-self: flex-start;
    }
    .msg.error {
      align-self: center;
      background: #fff4f2;
      border-color: #f4b8b0;
      color: var(--danger);
    }
    .composer {
      border-top: 1px solid var(--line);
      background: var(--panel);
      padding: 12px 18px;
      display: grid;
      grid-template-columns: minmax(0, 1fr) auto;
      gap: 10px;
      align-items: end;
    }
    .composer-actions {
      display: grid;
      grid-template-columns: 78px 78px;
      gap: 8px;
    }
    .meta {
      color: var(--muted);
      font-size: 12px;
      line-height: 1.45;
    }
    .account {
      display: grid;
      gap: 8px;
    }
    .kbd {
      color: var(--code);
      background: #edf1f5;
      border: 1px solid var(--line);
      border-radius: 4px;
      padding: 1px 5px;
    }
    @media (max-width: 980px) {
      body { overflow: auto; }
      .shell {
        min-width: 0;
        height: auto;
        min-height: 100vh;
        grid-template-columns: 1fr;
      }
      .sidebar, .inspector { border: 0; }
      .inspector-panels { overflow: visible; }
      .main { min-height: 70vh; }
      .msg { max-width: 100%; }
    }
  </style>
</head>
<body>
  <div id="authScreen" class="auth-screen">
    <div class="auth-panel">
      <div class="auth-title">
        <strong>Agent Runtime</strong>
        <span>登录后开始使用你的会话和工作流</span>
      </div>
      <div class="tabs">
        <button id="loginTab" class="active">登录</button>
        <button id="registerTab">注册</button>
      </div>
      <div id="displayNameField" class="field" style="display:none">
        <label for="displayName">显示名称</label>
        <input id="displayName" autocomplete="name">
      </div>
      <div class="field">
        <label for="email">邮箱</label>
        <input id="email" type="email" autocomplete="email">
      </div>
      <div class="field">
        <label for="password">密码</label>
        <input id="password" type="password" autocomplete="current-password">
      </div>
      <button class="primary" id="authSubmitBtn">登录</button>
      <div class="status"><span id="authStatusDot" class="dot"></span><span id="authStatusText">请输入账号信息</span></div>
    </div>
  </div>
  <div class="shell">
    <aside class="sidebar">
      <div class="brand"><span class="mark"></span><span>Agent Runtime</span></div>
      <div class="section">
        <h2>Account</h2>
        <div class="account">
          <strong id="accountName">未登录</strong>
          <span class="meta" id="accountEmail"></span>
        </div>
        <div class="row" style="margin-top:10px">
          <button id="logoutBtn">Logout</button>
          <span class="status"><span id="statusDot" class="dot"></span><span id="statusText">Idle</span></span>
        </div>
      </div>
      <div class="section">
        <div class="row">
          <button id="newSessionBtn">New Session</button>
          <button id="refreshBtn">Refresh</button>
        </div>
      </div>
      <div id="sessions" class="sessions"></div>
    </aside>

    <main class="main">
      <div class="topbar">
        <div class="titleline">
          <strong id="sessionTitle">No session</strong>
          <span id="sessionMeta">Create or select a session</span>
        </div>
        <div class="row">
          <select id="transport">
            <option value="sse">SSE</option>
            <option value="ws">WebSocket</option>
          </select>
          <button class="danger" id="cancelBtn">Cancel</button>
        </div>
      </div>
      <div id="messages" class="messages"></div>
      <div class="composer">
        <textarea id="prompt" placeholder="输入消息，或用 /skills 查看技能"></textarea>
        <div class="composer-actions">
          <button class="primary" id="sendBtn">Send</button>
          <button id="clearBtn">Clear</button>
        </div>
      </div>
    </main>

    <aside class="inspector">
      <div class="brand">Controls</div>
      <div class="inspector-panels">
        <section class="drawer open" data-drawer="runtime">
          <button class="drawer-toggle" type="button" data-toggle-drawer="runtime" aria-expanded="true">
            <span class="drawer-title">Runtime</span>
          </button>
          <div class="drawer-body">
            <div class="meta">
              <div>HTTP: <span class="kbd" id="originText"></span></div>
              <div>Events: <span class="kbd" id="transportText">SSE</span></div>
            </div>
          </div>
        </section>

        <section class="drawer" data-drawer="jobs">
          <button class="drawer-toggle" type="button" data-toggle-drawer="jobs" aria-expanded="false">
            <span class="drawer-title">Jobs</span>
            <span class="drawer-count" id="jobsCount">0</span>
          </button>
          <div class="drawer-body">
            <div class="row" style="margin-bottom:10px">
              <button id="refreshJobsBtn">Refresh Jobs</button>
              <button class="danger" id="cancelJobBtn">Cancel Job</button>
            </div>
            <div id="jobs" class="skills"></div>
            <div id="jobTimeline" class="timeline"></div>
          </div>
        </section>

        <section class="drawer" data-drawer="data">
          <button class="drawer-toggle" type="button" data-toggle-drawer="data" aria-expanded="false">
            <span class="drawer-title">Data</span>
          </button>
          <div class="drawer-body">
            <div class="field">
              <button id="exportBtn">Export Data</button>
            </div>
            <div class="field">
              <button id="deleteSessionMemoryBtn">Delete Session Memory</button>
            </div>
            <div class="field">
              <button id="deleteAllMemoryBtn">Delete All Memory</button>
            </div>
            <div class="field">
              <button class="danger" id="deleteSessionBtn">Delete Session</button>
            </div>
            <div class="field">
              <button class="danger" id="deleteAccountBtn">Delete Account</button>
            </div>
          </div>
        </section>

        <section class="drawer" data-drawer="attachments">
          <button class="drawer-toggle" type="button" data-toggle-drawer="attachments" aria-expanded="false">
            <span class="drawer-title">Attachments</span>
            <span class="drawer-count" id="attachmentsCount">0</span>
          </button>
          <div class="drawer-body">
            <div class="field">
              <input id="attachmentFile" type="file">
            </div>
            <div class="field">
              <button id="uploadAttachmentBtn">Upload Attachment</button>
            </div>
            <div id="attachments" class="skills drawer-list"></div>
          </div>
        </section>

        <section class="drawer" data-drawer="artifacts">
          <button class="drawer-toggle" type="button" data-toggle-drawer="artifacts" aria-expanded="false">
            <span class="drawer-title">Artifacts</span>
            <span class="drawer-count" id="artifactsCount">0</span>
          </button>
          <div class="drawer-body">
            <div id="artifacts" class="skills drawer-list"></div>
          </div>
        </section>

        <section class="drawer" data-drawer="skills">
          <button class="drawer-toggle" type="button" data-toggle-drawer="skills" aria-expanded="false">
            <span class="drawer-title">Skills</span>
            <span class="drawer-count" id="skillsCount">0</span>
          </button>
          <div class="drawer-body">
            <div id="skills" class="skills drawer-list"></div>
          </div>
        </section>
      </div>
    </aside>
  </div>

  <script>
    const $ = (id) => document.getElementById(id);
    const state = {
      sessionId: localStorage.getItem("agent.sessionId") || "",
      userId: localStorage.getItem("agent.userId") || "",
      accessToken: localStorage.getItem("agent.accessToken") || localStorage.getItem("agent.token") || "",
      refreshToken: localStorage.getItem("agent.refreshToken") || "",
      expiresAt: localStorage.getItem("agent.expiresAt") || "",
      csrfToken: localStorage.getItem("agent.csrfToken") || readCookie("agentapi_csrf"),
      user: JSON.parse(localStorage.getItem("agent.user") || "null"),
      authMode: "login",
      refreshPromise: null,
      refreshTimer: 0,
      ws: null,
      jobSource: null,
      jobReconnectTimer: 0,
      selectedJobId: localStorage.getItem("agent.jobId") || "",
      jobEvents: [],
      jobEventIds: new Set(),
      jobLastEventId: "",
      assistantNode: null,
      busy: false,
    };
    $("originText").textContent = location.origin;
    initDrawers();

    function headers() {
      const h = { "Content-Type": "application/json" };
      if (state.userId) h["X-User-ID"] = state.userId;
      if (state.accessToken) h["Authorization"] = "Bearer " + state.accessToken;
      if (state.csrfToken) h["X-CSRF-Token"] = state.csrfToken;
      return h;
    }

    function readCookie(name) {
      return document.cookie.split(";").map((item) => item.trim()).find((item) => item.startsWith(name + "="))?.slice(name.length + 1) || "";
    }

    function setStatus(kind, text) {
      $("statusDot").className = "dot " + (kind || "");
      $("statusText").textContent = text;
    }

    function initDrawers() {
      document.querySelectorAll("[data-toggle-drawer]").forEach((button) => {
        const name = button.dataset.toggleDrawer;
        const drawer = document.querySelector('[data-drawer="' + name + '"]');
        if (!drawer) return;
        const saved = localStorage.getItem("agent.drawer." + name);
        if (saved === "open") drawer.classList.add("open");
        if (saved === "closed") drawer.classList.remove("open");
        button.setAttribute("aria-expanded", drawer.classList.contains("open") ? "true" : "false");
        button.onclick = () => toggleDrawer(name);
      });
    }

    function toggleDrawer(name) {
      const drawer = document.querySelector('[data-drawer="' + name + '"]');
      if (!drawer) return;
      const isOpen = drawer.classList.toggle("open");
      localStorage.setItem("agent.drawer." + name, isOpen ? "open" : "closed");
      const button = document.querySelector('[data-toggle-drawer="' + name + '"]');
      if (button) button.setAttribute("aria-expanded", isOpen ? "true" : "false");
    }

    function openDrawer(name) {
      const drawer = document.querySelector('[data-drawer="' + name + '"]');
      if (!drawer) return;
      drawer.classList.add("open");
      localStorage.setItem("agent.drawer." + name, "open");
      const button = document.querySelector('[data-toggle-drawer="' + name + '"]');
      if (button) button.setAttribute("aria-expanded", "true");
    }

    function setDrawerCount(name, count) {
      const node = $(name + "Count");
      if (node) node.textContent = String(count);
    }

    function setAuthStatus(kind, text) {
      $("authStatusDot").className = "dot " + (kind || "");
      $("authStatusText").textContent = text;
    }

    function saveAuth(session) {
      const previousUserId = state.userId;
      state.user = session.user;
      state.userId = session.user.id;
      state.accessToken = session.access_token;
      state.refreshToken = session.refresh_token;
      state.expiresAt = session.expires_at || "";
      state.csrfToken = session.csrf_token || readCookie("agentapi_csrf") || state.csrfToken;
      if (previousUserId && previousUserId !== state.userId) {
        state.sessionId = "";
        localStorage.removeItem("agent.sessionId");
      }
      localStorage.setItem("agent.user", JSON.stringify(state.user));
      localStorage.setItem("agent.userId", state.userId);
      localStorage.setItem("agent.accessToken", state.accessToken);
      localStorage.setItem("agent.refreshToken", state.refreshToken);
      if (state.expiresAt) localStorage.setItem("agent.expiresAt", state.expiresAt);
      if (state.csrfToken) localStorage.setItem("agent.csrfToken", state.csrfToken);
      localStorage.removeItem("agent.token");
      scheduleAccessRefresh();
    }

    function clearAuth() {
      if (state.refreshTimer) {
        clearTimeout(state.refreshTimer);
        state.refreshTimer = 0;
      }
      state.refreshPromise = null;
      state.user = null;
      state.userId = "";
      state.accessToken = "";
      state.refreshToken = "";
      state.expiresAt = "";
      state.csrfToken = "";
      state.sessionId = "";
      closeJobStream();
      state.selectedJobId = "";
      state.jobEvents = [];
      state.jobEventIds = new Set();
      state.jobLastEventId = "";
      localStorage.removeItem("agent.user");
      localStorage.removeItem("agent.userId");
      localStorage.removeItem("agent.accessToken");
      localStorage.removeItem("agent.refreshToken");
      localStorage.removeItem("agent.expiresAt");
      localStorage.removeItem("agent.csrfToken");
      localStorage.removeItem("agent.sessionId");
      localStorage.removeItem("agent.jobId");
      localStorage.removeItem("agent.token");
    }

    function showAuth(show) {
      $("authScreen").classList.toggle("hidden", !show);
    }

    function renderAccount() {
      if (!state.user) {
        $("accountName").textContent = "未登录";
        $("accountEmail").textContent = "";
        return;
      }
      $("accountName").textContent = state.user.display_name || state.user.email;
      $("accountEmail").textContent = state.user.email || state.user.id;
    }

    function setAuthMode(mode) {
      state.authMode = mode;
      $("loginTab").classList.toggle("active", mode === "login");
      $("registerTab").classList.toggle("active", mode === "register");
      $("displayNameField").style.display = mode === "register" ? "grid" : "none";
      $("authSubmitBtn").textContent = mode === "register" ? "注册并登录" : "登录";
      $("password").autocomplete = mode === "register" ? "new-password" : "current-password";
    }

    async function api(path, options = {}) {
      await ensureFreshAccess();
      let response = await fetchWithAuth(path, options);
      if (!response.ok) {
        const text = await response.text();
        if (response.status === 401 && await refreshAccess(true)) {
          response = await fetchWithAuth(path, options);
          if (response.ok) return response;
          const retryText = await response.text();
          throw new Error(errorText(retryText, response.statusText));
        }
        throw new Error(errorText(text, response.statusText));
      }
      return response;
    }

    function fetchWithAuth(path, options = {}) {
      return fetch(path, {
        ...options,
        credentials: "include",
        headers: { ...headers(), ...(options.headers || {}) },
      });
    }

    function errorText(text, fallback) {
      try {
        const payload = JSON.parse(text || "{}");
        return payload.error || payload.message || fallback;
      } catch (_) {
        return text || fallback;
      }
    }

    async function authRequest(path, body) {
      const response = await fetch(path, {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json", ...(state.csrfToken ? { "X-CSRF-Token": state.csrfToken } : {}) },
        body: JSON.stringify(body),
      });
      const payload = await response.json().catch(() => ({}));
      if (!response.ok) throw new Error(payload.error || response.statusText);
      return payload;
    }

    async function submitAuth() {
      const email = $("email").value.trim();
      const password = $("password").value;
      setAuthStatus("busy", "处理中");
      try {
        const payload = state.authMode === "register"
          ? await authRequest("/v1/auth/register", { email, password, display_name: $("displayName").value.trim() })
          : await authRequest("/v1/auth/login", { email, password });
        saveAuth(payload);
        renderAccount();
        showAuth(false);
        await connect();
        setAuthStatus("ok", "已登录");
      } catch (err) {
        setAuthStatus("err", err.message || String(err));
      }
    }

    function accessExpiresAtMs() {
      const ms = Date.parse(state.expiresAt || "");
      return Number.isFinite(ms) ? ms : 0;
    }

    function accessNeedsRefresh(leadMs = 60000) {
      const expiresAt = accessExpiresAtMs();
      return !!state.refreshToken && (!state.accessToken || !expiresAt || Date.now() + leadMs >= expiresAt);
    }

    function scheduleAccessRefresh() {
      if (state.refreshTimer) clearTimeout(state.refreshTimer);
      state.refreshTimer = 0;
      if (!state.refreshToken || !state.expiresAt) return;
      const expiresAt = accessExpiresAtMs();
      if (!expiresAt) return;
      const delay = Math.max(5000, expiresAt - Date.now() - 60000);
      state.refreshTimer = window.setTimeout(() => {
        refreshAccess(false).catch(() => {});
      }, delay);
    }

    async function ensureFreshAccess() {
      if (!accessNeedsRefresh()) return true;
      return refreshAccess(false);
    }

    async function refreshAccess(clearOnFailure = true) {
      if (!state.refreshToken) return false;
      if (state.refreshPromise) return state.refreshPromise;
      state.refreshPromise = (async () => {
        try {
          const payload = await authRequest("/v1/auth/refresh", { refresh_token: state.refreshToken });
          saveAuth(payload);
          renderAccount();
          return true;
        } catch (_) {
          if (clearOnFailure) clearAuth();
          return false;
        } finally {
          state.refreshPromise = null;
        }
      })();
      return state.refreshPromise;
    }

    async function openWithFreshToken(path) {
      await ensureFreshAccess();
      window.open(path + "?token=" + encodeURIComponent(state.accessToken), "_blank");
    }

    async function loadMe() {
      const response = await api("/v1/auth/me");
      const payload = await response.json();
      state.user = payload.user;
      localStorage.setItem("agent.user", JSON.stringify(state.user));
      renderAccount();
    }

    async function logout() {
      await api("/v1/auth/logout", {
        method: "POST",
        body: JSON.stringify({ refresh_token: state.refreshToken }),
      }).catch(() => {});
      clearAuth();
      $("sessions").textContent = "";
      $("messages").textContent = "";
      $("jobs").textContent = "";
      renderJobTimeline([]);
      renderAccount();
      showAuth(true);
      setStatus("", "Logged out");
    }

    async function connect() {
      setStatus("busy", "Connecting");
      await loadMe();
      const [sessions] = await Promise.all([loadSessions(), loadSkills(), loadAttachments(), loadArtifacts(), loadJobs()]);
      await ensureActiveSession(sessions);
      setStatus("ok", "Ready");
    }

    async function ensureActiveSession(sessions) {
      const list = Array.isArray(sessions) ? sessions : [];
      const current = list.find((session) => session.id === state.sessionId);
      if (current) {
        selectSession(current);
        renderSessions(list);
        return;
      }
      state.sessionId = "";
      localStorage.removeItem("agent.sessionId");
      if (list.length > 0) {
        selectSession(list[0]);
        renderSessions(list);
        return;
      }
      await createSession();
    }

    async function createSession() {
      const response = await api("/v1/sessions", {
        method: "POST",
        body: JSON.stringify({ working_dir: "" }),
      });
      const session = await response.json();
      selectSession(session);
      await loadSessions();
    }

    async function loadSessions() {
      const response = await api("/v1/sessions");
      const sessions = await response.json();
      const list = Array.isArray(sessions) ? sessions : [];
      renderSessions(list);
      return list;
    }

    async function loadSkills() {
      const response = await api("/v1/skills");
      const payload = await response.json();
      renderSkills(payload.skills || []);
    }

    async function loadAttachments() {
      const response = await api("/v1/attachments");
      const payload = await response.json();
      renderAssets("attachments", payload.attachments || [], "No attachments", downloadAttachment, deleteAttachment);
    }

    async function loadArtifacts() {
      const response = await api("/v1/artifacts");
      const payload = await response.json();
      renderAssets("artifacts", payload.artifacts || [], "No generated artifacts", downloadArtifact, deleteArtifact);
    }

    async function loadJobs() {
      const suffix = state.sessionId ? "?session_id=" + encodeURIComponent(state.sessionId) : "";
      const response = await api("/v1/jobs" + suffix);
      const payload = await response.json();
      const jobs = payload.jobs || [];
      renderJobs(jobs);
      if (state.selectedJobId) {
        const selected = jobs.find((job) => job.id === state.selectedJobId);
        if (selected && state.jobEvents.length === 0 && !state.jobSource) await selectJob(selected, false);
      }
      return jobs;
    }

    function renderSessions(sessions) {
      const root = $("sessions");
      root.textContent = "";
      sessions.forEach((session) => {
        const btn = document.createElement("button");
        btn.className = "item" + (session.id === state.sessionId ? " active" : "");
        const title = document.createElement("span");
        title.className = "title";
        title.textContent = sessionTitleText(session);
        title.title = sessionFirstUserText(session) || session.description || session.id;
        const sub = document.createElement("span");
        sub.className = "sub";
        sub.textContent = (session.messages || []).length + " messages";
        btn.append(title, sub);
        btn.onclick = () => selectSession(session);
        root.appendChild(btn);
      });
    }

    function renderJobs(jobs) {
      const root = $("jobs");
      root.textContent = "";
      setDrawerCount("jobs", jobs.length);
      if (!jobs.length) {
        const empty = document.createElement("div");
        empty.className = "empty";
        empty.textContent = "No jobs yet";
        root.appendChild(empty);
        renderJobTimeline(state.selectedJobId ? state.jobEvents : []);
        return;
      }
      jobs.forEach((job) => {
        const btn = document.createElement("button");
        btn.className = "item" + (job.id === state.selectedJobId ? " active" : "");
        const title = document.createElement("span");
        title.className = "title";
        title.textContent = job.content || job.id;
        const sub = document.createElement("span");
        sub.className = "sub";
        sub.textContent = job.status + " · " + shortTime(job.updated_at || job.created_at);
        const status = document.createElement("span");
        status.className = "pill " + job.status;
        status.textContent = job.status;
        status.style.marginTop = "7px";
        btn.append(title, sub, status);
        btn.onclick = () => selectJob(job, true);
        root.appendChild(btn);
      });
    }

    function renderSkills(skills) {
      const root = $("skills");
      root.textContent = "";
      setDrawerCount("skills", skills.length);
      if (!skills.length) {
        const empty = document.createElement("div");
        empty.className = "empty";
        empty.textContent = "No skills loaded";
        root.appendChild(empty);
        return;
      }
      skills.forEach((skill) => {
        const btn = document.createElement("button");
        btn.className = "item";
        const title = document.createElement("span");
        title.className = "title";
        title.textContent = "/" + skill.name;
        const sub = document.createElement("span");
        sub.className = "sub";
        sub.textContent = skill.description || skill.usage || "";
        btn.append(title, sub);
        btn.onclick = () => {
          $("prompt").value = "/" + skill.name + " ";
          $("prompt").focus();
        };
        root.appendChild(btn);
      });
    }

    function renderAssets(rootID, assets, emptyText, downloadFn, deleteFn) {
      const root = $(rootID);
      root.textContent = "";
      setDrawerCount(rootID, assets.length);
      if (!assets.length) {
        const empty = document.createElement("div");
        empty.className = "empty";
        empty.textContent = emptyText;
        root.appendChild(empty);
        return;
      }
      assets.forEach((asset) => {
        const row = document.createElement("div");
        row.className = "item";
        const title = document.createElement("span");
        title.className = "title";
        title.textContent = asset.filename || asset.id;
        const sub = document.createElement("span");
        sub.className = "sub";
        sub.textContent = Math.round((asset.size_bytes || 0) / 1024) + " KB";
        const actions = document.createElement("div");
        actions.className = "row";
        actions.style.marginTop = "8px";
        const open = document.createElement("button");
        open.textContent = "Download";
        open.onclick = () => downloadFn(asset.id);
        const del = document.createElement("button");
        del.className = "danger";
        del.textContent = "Delete";
        del.onclick = () => deleteFn(asset.id);
        actions.append(open, del);
        row.append(title, sub, actions);
        root.appendChild(row);
      });
    }

    function selectSession(session) {
      const changed = state.sessionId && state.sessionId !== session.id;
      state.sessionId = session.id;
      localStorage.setItem("agent.sessionId", state.sessionId);
      $("sessionTitle").textContent = sessionTitleText(session);
      $("sessionTitle").title = sessionFirstUserText(session) || session.description || session.id;
      $("sessionMeta").textContent = session.working_dir || "";
      renderMessages(session.messages || []);
      if (changed) {
        state.selectedJobId = "";
        state.jobEvents = [];
        state.jobEventIds = new Set();
        state.jobLastEventId = "";
        localStorage.removeItem("agent.jobId");
        closeJobStream();
      }
      loadJobs().catch(() => {});
    }

    function sessionFirstUserText(session) {
      const messages = session.messages || [];
      for (const msg of messages) {
        const display = displayMessage(msg);
        if (display && display.role === "user") return normalizeTitleText(display.content || "");
      }
      return "";
    }

    function sessionTitleText(session) {
      return truncateTitle(sessionFirstUserText(session) || session.description || session.id || "New Session", 32);
    }

    function normalizeTitleText(text) {
      return String(text || "").replace(/\s+/g, " ").trim();
    }

    function truncateTitle(text, maxLength) {
      text = normalizeTitleText(text);
      if (text.length <= maxLength) return text;
      return text.slice(0, Math.max(0, maxLength - 3)).trimEnd() + "...";
    }

    function renderMessages(messages) {
      $("messages").textContent = "";
      messages.forEach((msg) => {
        const display = displayMessage(msg);
        if (!display) return;
        addMessage(display.role, display.content);
      });
    }

    function displayMessage(msg) {
      if (!msg || msg.role === "tool") return null;
      if (msg.hidden) {
        const command = skillCommandDisplayText(msg.content || "");
        return command ? { role: "user", content: command } : null;
      }
      const content = normalizeTitleText(msg.content || msg.tool_output || "");
      if (!content) return null;
      return { role: msg.role || "assistant", content };
    }

    function skillCommandDisplayText(content) {
      if (!content || !content.includes("<skill-format>true</skill-format>")) return "";
      const name = tagValue(content, "command-name");
      const args = tagValue(content, "command-args");
      return normalizeTitleText([name, args].filter(Boolean).join(" "));
    }

    function tagValue(content, tag) {
      const match = String(content || "").match(new RegExp("<" + tag + ">([\\s\\S]*?)<\\/" + tag + ">"));
      return match ? normalizeTitleText(match[1]) : "";
    }

    function addMessage(role, content) {
      const node = document.createElement("div");
      node.className = "msg " + role;
      node.textContent = content;
      $("messages").appendChild(node);
      $("messages").scrollTop = $("messages").scrollHeight;
      return node;
    }

    function appendAssistantDelta(text) {
      if (!state.assistantNode) {
        state.assistantNode = addMessage("assistant", "");
      }
      state.assistantNode.textContent += text;
      $("messages").scrollTop = $("messages").scrollHeight;
    }

    async function send() {
      const content = $("prompt").value.trim();
      if (!content || !state.sessionId || state.busy) return;
      state.busy = true;
      state.assistantNode = null;
      $("sendBtn").disabled = true;
      setStatus("busy", "Running");
      addMessage("user", content);
      $("prompt").value = "";
      let routedToJob = false;
      try {
        const mode = $("transport").value === "ws" ? await sendWS(content) : await sendSSE(content);
        routedToJob = mode === "job";
        if (!routedToJob) {
          await refreshSelected();
          setStatus("ok", "Ready");
        }
      } catch (err) {
        addMessage("error", err.message || String(err));
        setStatus("err", "Error");
      } finally {
        if (!routedToJob) state.busy = false;
        $("sendBtn").disabled = routedToJob;
        state.assistantNode = null;
      }
    }

    async function sendSSE(content) {
      $("transportText").textContent = "SSE";
      const response = await api("/v1/sessions/" + encodeURIComponent(state.sessionId) + "/messages", {
        method: "POST",
        body: JSON.stringify({ content }),
      });
      const reader = response.body.getReader();
      const decoder = new TextDecoder();
      let buffer = "";
      let mode = "chat";
      while (true) {
        const { value, done } = await reader.read();
        if (done) break;
        buffer += decoder.decode(value, { stream: true });
        const parts = buffer.split("\n\n");
        buffer = parts.pop();
        for (const part of parts) {
          if (handleSSEBlock(part) === "job") mode = "job";
        }
      }
      return mode;
    }

    function handleSSEBlock(block) {
      const dataLine = block.split("\n").find((line) => line.startsWith("data: "));
      if (!dataLine) return;
      const event = JSON.parse(dataLine.slice(6));
      handleEvent(event);
      if (event.type === "error") {
        throw new Error(event.error || "agent runtime error");
      }
      return event.type;
    }

    function sendWS(content) {
      $("transportText").textContent = "WebSocket";
      return new Promise((resolve, reject) => {
        ensureFreshAccess().then((ok) => {
          if (!ok) {
            reject(new Error("auth refresh failed"));
            return;
          }
          const protocol = location.protocol === "https:" ? "wss:" : "ws:";
          const url = protocol + "//" + location.host + "/v1/sessions/" + encodeURIComponent(state.sessionId) + "/ws?user_id=" + encodeURIComponent(state.userId) + "&token=" + encodeURIComponent(state.accessToken);
          const ws = new WebSocket(url);
          state.ws = ws;
          ws.onopen = () => ws.send(JSON.stringify({ type: "chat", content }));
          ws.onerror = () => reject(new Error("websocket error"));
          ws.onmessage = (event) => {
            const msg = JSON.parse(event.data);
            handleEvent(msg);
            if (msg.type === "done") {
              ws.close();
              resolve("chat");
            }
            if (msg.type === "job") {
              ws.close();
              resolve("job");
            }
            if (msg.type === "error") {
              ws.close();
              reject(new Error(msg.error));
            }
          };
        }).catch((err) => reject(err));
      });
    }

    function handleEvent(event) {
      if (event.type === "delta") appendAssistantDelta(event.content || "");
      if (event.type === "message" && event.role === "assistant") {
        if (state.assistantNode) {
          state.assistantNode.textContent = event.content || "";
        } else {
          addMessage("assistant", event.content || "");
        }
      }
      if (event.type === "job" && event.job) {
        addMessage("assistant", "已转入后台任务：" + (event.job_reason || event.job.type || event.job.id));
        openDrawer("jobs");
        selectJob(event.job, true).then(loadJobs).catch((err) => addMessage("error", err.message || String(err)));
        setStatus("busy", "Job running");
      }
      if (event.type === "error") addMessage("error", event.error || "error");
    }

    async function selectJob(job, stream) {
      state.selectedJobId = job.id;
      localStorage.setItem("agent.jobId", job.id);
      state.jobEvents = [];
      state.jobEventIds = new Set();
      state.jobLastEventId = "";
      renderJobTimeline([]);
      const response = await api("/v1/jobs/" + encodeURIComponent(job.id) + "/events?limit=500");
      const payload = await response.json();
      (payload.events || []).forEach((record) => addJobEvent(record, false));
      renderJobTimeline(state.jobEvents);
      if (stream && !isTerminalJob(job.status)) {
        openJobStream(job.id);
      } else {
        closeJobStream();
      }
      await loadArtifacts().catch(() => {});
    }

    async function openJobStream(jobId) {
      closeJobStream();
      if (!await ensureFreshAccess()) {
        setStatus("err", "Auth needed");
        return;
      }
      const params = new URLSearchParams({ stream: "1" });
      if (state.accessToken) params.set("token", state.accessToken);
      if (state.jobLastEventId) params.set("after_id", state.jobLastEventId);
      const source = new EventSource("/v1/jobs/" + encodeURIComponent(jobId) + "/events?" + params.toString(), { withCredentials: true });
      state.jobSource = source;
      source.onmessage = (message) => handleJobStreamMessage(message);
      ["start", "message", "delta", "done", "error", "cancelled"].forEach((type) => {
        source.addEventListener(type, handleJobStreamMessage);
      });
      source.onerror = () => {
        source.close();
        state.jobSource = null;
        if (!state.selectedJobId) return;
        if (state.jobReconnectTimer) clearTimeout(state.jobReconnectTimer);
        state.jobReconnectTimer = window.setTimeout(async () => {
          state.jobReconnectTimer = 0;
          if (await ensureFreshAccess()) {
            openJobStream(state.selectedJobId).catch(() => {});
          } else {
            setStatus("err", "Auth needed");
          }
        }, 1200);
      };
    }

    function closeJobStream() {
      if (state.jobReconnectTimer) {
        clearTimeout(state.jobReconnectTimer);
        state.jobReconnectTimer = 0;
      }
      if (state.jobSource) {
        state.jobSource.close();
        state.jobSource = null;
      }
    }

    function handleJobStreamMessage(message) {
      if (!message.data) return;
      const event = JSON.parse(message.data);
      const record = {
        id: message.lastEventId || event.id || "",
        job_id: event.job_id || state.selectedJobId,
        session_id: event.session_id || state.sessionId,
        type: event.type,
        event,
        created_at: new Date().toISOString(),
      };
      addJobEvent(record, true);
      handleEvent(event);
      if (message.lastEventId) state.jobLastEventId = message.lastEventId;
      if (event.type === "done" || event.type === "error" || event.type === "cancelled") {
        state.busy = false;
        $("sendBtn").disabled = false;
        setStatus(event.type === "done" ? "ok" : "err", event.type === "done" ? "Ready" : "Job stopped");
        closeJobStream();
        Promise.all([refreshSelected(), loadJobs(), loadArtifacts()]).catch(() => {});
      }
    }

    function addJobEvent(record, rerender) {
      if (!record) return;
      const id = record.id || record.event?.id || "";
      if (id && state.jobEventIds.has(id)) return;
      if (id) state.jobEventIds.add(id);
      if (id) state.jobLastEventId = id;
      state.jobEvents.push(record);
      if (rerender) renderJobTimeline(state.jobEvents);
    }

    function renderJobTimeline(events) {
      const root = $("jobTimeline");
      if (!root) return;
      root.textContent = "";
      if (!events.length) {
        const empty = document.createElement("div");
        empty.className = "empty";
        empty.textContent = state.selectedJobId ? "Waiting for events" : "Select a job to inspect events";
        root.appendChild(empty);
        return;
      }
      events.forEach((record) => {
        const event = record.event || record.Event || {};
        const node = document.createElement("div");
        node.className = "timeline-event";
        const title = document.createElement("strong");
        title.textContent = record.type || event.type || "event";
        const meta = document.createElement("span");
        meta.textContent = shortTime(record.created_at) + (record.id ? " · " + record.id : "");
        const body = document.createElement("span");
        body.textContent = event.error || event.content || event.role || "";
        node.append(title, meta);
        if (body.textContent) node.appendChild(body);
        root.appendChild(node);
      });
      root.scrollTop = root.scrollHeight;
    }

    async function cancelSelectedJob() {
      if (!state.selectedJobId) return;
      await api("/v1/jobs/" + encodeURIComponent(state.selectedJobId) + "/cancel", { method: "POST" });
      await loadJobs();
      setStatus("ok", "Job cancelled");
    }

    function isTerminalJob(status) {
      return status === "succeeded" || status === "failed" || status === "cancelled";
    }

    function shortTime(value) {
      if (!value) return "";
      const date = new Date(value);
      if (Number.isNaN(date.getTime())) return String(value);
      return date.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit" });
    }

    async function refreshSelected() {
      if (!state.sessionId) return;
      const response = await api("/v1/sessions/" + encodeURIComponent(state.sessionId));
      selectSession(await response.json());
      await loadSessions();
    }

    async function cancelRun() {
      if (state.ws && state.ws.readyState === WebSocket.OPEN) {
        state.ws.send(JSON.stringify({ type: "cancel" }));
      }
      if (state.sessionId) {
        await api("/v1/sessions/" + encodeURIComponent(state.sessionId) + "/cancel", { method: "POST" }).catch(() => {});
      }
    }

    async function exportData() {
      const response = await api("/v1/data/export", { headers: { "Content-Type": "application/json" } });
      const payload = await response.json();
      const blob = new Blob([JSON.stringify(payload, null, 2)], { type: "application/json" });
      const url = URL.createObjectURL(blob);
      const link = document.createElement("a");
      link.href = url;
      link.download = "agent-data-export.json";
      link.click();
      URL.revokeObjectURL(url);
      setStatus("ok", "Exported");
    }

    async function deleteCurrentSession() {
      if (!state.sessionId || !confirm("删除当前 session？")) return;
      await api("/v1/sessions/" + encodeURIComponent(state.sessionId), { method: "DELETE" });
      state.sessionId = "";
      localStorage.removeItem("agent.sessionId");
      $("messages").textContent = "";
      const sessions = await loadSessions();
      await ensureActiveSession(sessions);
    }

    async function deleteSessionMemory() {
      if (!state.sessionId || !confirm("删除当前 session memory？")) return;
      await api("/v1/sessions/" + encodeURIComponent(state.sessionId) + "/memory", { method: "DELETE" });
      setStatus("ok", "Session memory deleted");
    }

    async function deleteAllMemory() {
      if (!confirm("删除当前账号的全部 memory？")) return;
      await api("/v1/memory", { method: "DELETE" });
      setStatus("ok", "Memory deleted");
    }

    async function deleteAccount() {
      if (!confirm("注销账号并删除全部 session、memory 和 workspace？")) return;
      await api("/v1/account", {
        method: "DELETE",
        body: JSON.stringify({ refresh_token: state.refreshToken }),
      });
      clearAuth();
      $("sessions").textContent = "";
      $("messages").textContent = "";
      renderAccount();
      showAuth(true);
      setStatus("", "Account deleted");
    }

    async function uploadAttachment() {
      const file = $("attachmentFile").files[0];
      if (!file) return;
      const contentType = file.type || "application/octet-stream";
      try {
        const presignResponse = await api("/v1/attachments/presign", {
          method: "POST",
          body: JSON.stringify({
            session_id: state.sessionId || "",
            filename: file.name,
            content_type: contentType,
            size_bytes: file.size,
          }),
        });
        const upload = await presignResponse.json();
        const uploadResponse = await fetch(upload.upload_url, {
          method: upload.method || "PUT",
          headers: upload.headers || { "Content-Type": contentType },
          body: file,
        });
        if (!uploadResponse.ok) throw new Error(await uploadResponse.text());
        await api("/v1/attachments/" + encodeURIComponent(upload.attachment_id) + "/confirm", {
          method: "POST",
          body: JSON.stringify({
            session_id: state.sessionId || "",
            filename: file.name,
            content_type: contentType,
            size_bytes: file.size,
          }),
        });
      } catch (err) {
        if (!String(err.message || err).includes("presigned uploads")) throw err;
        await uploadAttachmentMultipart(file);
      }
      $("attachmentFile").value = "";
      await loadAttachments();
      openDrawer("attachments");
      setStatus("ok", "Attachment uploaded");
    }

    async function uploadAttachmentMultipart(file) {
      const form = new FormData();
      form.append("file", file);
      if (state.sessionId) form.append("session_id", state.sessionId);
      await api("/v1/attachments", { method: "POST", body: form });
    }

    function downloadAttachment(id) {
      openWithFreshToken("/v1/attachments/" + encodeURIComponent(id)).catch((err) => addMessage("error", err.message || String(err)));
    }

    async function deleteAttachment(id) {
      if (!confirm("删除这个 attachment？")) return;
      await api("/v1/attachments/" + encodeURIComponent(id), { method: "DELETE" });
      await loadAttachments();
      setStatus("ok", "Attachment deleted");
    }

    function downloadArtifact(id) {
      openWithFreshToken("/v1/artifacts/" + encodeURIComponent(id)).catch((err) => addMessage("error", err.message || String(err)));
    }

    async function deleteArtifact(id) {
      if (!confirm("删除这个 artifact？")) return;
      await api("/v1/artifacts/" + encodeURIComponent(id), { method: "DELETE" });
      await loadArtifacts();
      setStatus("ok", "Artifact deleted");
    }

    $("loginTab").onclick = () => setAuthMode("login");
    $("registerTab").onclick = () => setAuthMode("register");
    $("authSubmitBtn").onclick = submitAuth;
    $("logoutBtn").onclick = logout;
    $("newSessionBtn").onclick = createSession;
    $("refreshBtn").onclick = () => Promise.all([loadSessions(), loadSkills(), loadAttachments(), loadArtifacts(), loadJobs()]);
    $("sendBtn").onclick = send;
    $("cancelBtn").onclick = cancelRun;
    $("clearBtn").onclick = () => { $("messages").textContent = ""; };
    $("refreshJobsBtn").onclick = loadJobs;
    $("cancelJobBtn").onclick = () => cancelSelectedJob().catch((err) => {
      setStatus("err", "Cancel job error");
      addMessage("error", err.message || String(err));
    });
    $("exportBtn").onclick = exportData;
    $("deleteSessionBtn").onclick = deleteCurrentSession;
    $("deleteSessionMemoryBtn").onclick = deleteSessionMemory;
    $("deleteAllMemoryBtn").onclick = deleteAllMemory;
    $("deleteAccountBtn").onclick = deleteAccount;
    $("uploadAttachmentBtn").onclick = () => uploadAttachment().catch((err) => {
      setStatus("err", "Attachment error");
      addMessage("error", err.message || String(err));
    });
    $("transport").onchange = () => { $("transportText").textContent = $("transport").value.toUpperCase(); };
    $("prompt").addEventListener("keydown", (event) => {
      if (event.key === "Enter" && (event.metaKey || event.ctrlKey)) send();
    });
    $("password").addEventListener("keydown", (event) => {
      if (event.key === "Enter") submitAuth();
    });
    window.addEventListener("focus", () => {
      ensureFreshAccess().catch(() => {});
    });
    document.addEventListener("visibilitychange", () => {
      if (!document.hidden) ensureFreshAccess().catch(() => {});
    });

    async function boot() {
      renderAccount();
      if (!state.accessToken && state.refreshToken) {
        await refreshAccess();
      }
      if (!state.accessToken) {
        showAuth(true);
        setStatus("", "Auth needed");
        return;
      }
      scheduleAccessRefresh();
      showAuth(false);
      try {
        await connect();
      } catch (err) {
        const refreshed = await refreshAccess();
        if (refreshed) {
          await connect();
          return;
        }
        showAuth(true);
        setStatus("err", "Auth needed");
        setAuthStatus("err", err.message || String(err));
      }
    }
    boot();
  </script>
</body>
</html>`
