self.addEventListener("push", (event) => {
  event.waitUntil((async () => {
    const payload = readPushPayload(event);
    const title = payload.title || "Task update";
    await self.registration.showNotification(title, {
      body: payload.body || "",
      tag: payload.tag || payload.task_id || payload.job_id || "agentapi-task-update",
      icon: "/logo.png",
      badge: "/logo.png",
      data: {
        url: payload.url || "/app",
        task_id: payload.task_id,
        job_id: payload.job_id,
        session_id: payload.session_id,
        notification_type: payload.notification_type
      }
    });
  })());
});

self.addEventListener("notificationclick", (event) => {
  event.notification.close();
  const targetURL = event.notification.data?.url || "/app";
  event.waitUntil((async () => {
    const windows = await self.clients.matchAll({ type: "window", includeUncontrolled: true });
    for (const client of windows) {
      const url = new URL(client.url);
      if (url.origin === self.location.origin) {
        await client.focus();
        return;
      }
    }
    await self.clients.openWindow(targetURL);
  })());
});

function readPushPayload(event) {
  if (!event.data) return {};
  try {
    return event.data.json();
  } catch {
    try {
      return { body: event.data.text() };
    } catch {
      return {};
    }
  }
}
