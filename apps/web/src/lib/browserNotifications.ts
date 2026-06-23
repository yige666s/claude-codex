import type { TaskInboxItem } from "../types";

const taskNotificationPreferenceKey = "agentapi.task.browser_notifications";
const taskPushSubscriptionIDKey = "agentapi.task.browser_push_subscription_id";
const taskNotificationServiceWorkerURL = "/task-push-sw.js";

let taskNotificationServiceWorker: Promise<ServiceWorkerRegistration | null> | null = null;

export type BrowserNotificationPermission = NotificationPermission | "unsupported";

export function browserTaskNotificationsSupported(): boolean {
  return typeof window !== "undefined" && "Notification" in window;
}

export function browserTaskPushSupported(): boolean {
  return browserTaskNotificationsSupported() &&
    typeof navigator !== "undefined" &&
    "serviceWorker" in navigator &&
    "PushManager" in window;
}

export function browserTaskNotificationPermission(): BrowserNotificationPermission {
  if (!browserTaskNotificationsSupported()) return "unsupported";
  return Notification.permission;
}

export function browserTaskNotificationsEnabled(): boolean {
  if (!browserTaskNotificationsSupported()) return false;
  try {
    return localStorage.getItem(taskNotificationPreferenceKey) === "1" && Notification.permission === "granted";
  } catch {
    return false;
  }
}

export function setBrowserTaskNotificationsEnabled(enabled: boolean): void {
  try {
    if (enabled) localStorage.setItem(taskNotificationPreferenceKey, "1");
    else localStorage.removeItem(taskNotificationPreferenceKey);
  } catch {
    // Ignore storage failures; permission state still controls delivery.
  }
}

export function browserTaskPushSubscriptionID(): string {
  try {
    return localStorage.getItem(taskPushSubscriptionIDKey) || "";
  } catch {
    return "";
  }
}

export function setBrowserTaskPushSubscriptionID(subscriptionID: string): void {
  try {
    const value = subscriptionID.trim();
    if (value) localStorage.setItem(taskPushSubscriptionIDKey, value);
    else localStorage.removeItem(taskPushSubscriptionIDKey);
  } catch {
    // Ignore storage failures; the push endpoint will eventually expire.
  }
}

export async function requestBrowserTaskNotifications(): Promise<BrowserNotificationPermission> {
  if (!browserTaskNotificationsSupported()) return "unsupported";
  let permission = Notification.permission;
  if (permission === "default") {
    permission = await Notification.requestPermission();
  }
  setBrowserTaskNotificationsEnabled(permission === "granted");
  return permission;
}

export function taskInboxNotificationKey(item: TaskInboxItem): string {
  return [item.id, item.status, item.updated_at, item.notification_type || ""].join("|");
}

export function shouldNotifyTaskInboxItem(item: TaskInboxItem): boolean {
  return ["job_completed", "job_failed", "review_required", "quota_blocked", "loop_triggered"].includes(item.notification_type || "");
}

export function notifyTaskInboxItem(item: TaskInboxItem): void {
  if (!browserTaskNotificationsEnabled() || !shouldNotifyTaskInboxItem(item)) return;
  const title = taskInboxNotificationTitle(item);
  const options = taskInboxNotificationOptions(item);
  if (typeof navigator !== "undefined" && "serviceWorker" in navigator) {
    taskServiceWorkerRegistration().then((registration) => {
      if (!registration) {
        showWindowNotification(title, options);
        return;
      }
      return registration.showNotification(title, options).catch(() => showWindowNotification(title, options));
    }).catch(() => showWindowNotification(title, options));
    return;
  }
  showWindowNotification(title, options);
}

function showWindowNotification(title: string, options: NotificationOptions): void {
  try {
    const notification = new Notification(title, options);
    notification.onclick = () => {
      window.focus();
      notification.close();
    };
  } catch {
    // Browsers can reject notifications in quiet mode or insecure contexts.
  }
}

function taskServiceWorkerRegistration(): Promise<ServiceWorkerRegistration | null> {
  if (taskNotificationServiceWorker) return taskNotificationServiceWorker;
  taskNotificationServiceWorker = navigator.serviceWorker
    .register(taskNotificationServiceWorkerURL)
    .then((registration) => waitForActiveTaskServiceWorker(registration))
    .catch(() => null);
  return taskNotificationServiceWorker;
}

async function waitForActiveTaskServiceWorker(registration: ServiceWorkerRegistration): Promise<ServiceWorkerRegistration | null> {
  if (registration.active) return registration;
  try {
    const readyRegistration = await navigator.serviceWorker.ready;
    return readyRegistration.active ? readyRegistration : null;
  } catch {
    return registration.active ? registration : null;
  }
}

export async function subscribeBrowserTaskPush(publicKey: string): Promise<PushSubscription | null> {
  if (!browserTaskPushSupported()) return null;
  const registration = await taskServiceWorkerRegistration();
  if (!registration) return null;
  const existing = await registration.pushManager.getSubscription();
  if (existing) return existing;
  return registration.pushManager.subscribe({
    userVisibleOnly: true,
    applicationServerKey: urlBase64ToUint8Array(publicKey)
  });
}

export async function unsubscribeBrowserTaskPush(): Promise<void> {
  if (!browserTaskPushSupported()) return;
  const registration = await taskServiceWorkerRegistration();
  if (!registration) return;
  const existing = await registration.pushManager.getSubscription();
  if (existing) {
    await existing.unsubscribe();
  }
}

function urlBase64ToUint8Array(value: string): ArrayBuffer {
  const padding = "=".repeat((4 - (value.length % 4)) % 4);
  const base64 = `${value}${padding}`.replace(/-/g, "+").replace(/_/g, "/");
  const raw = window.atob(base64);
  const buffer = new ArrayBuffer(raw.length);
  const output = new Uint8Array(buffer);
  for (let i = 0; i < raw.length; i += 1) {
    output[i] = raw.charCodeAt(i);
  }
  return buffer;
}

function taskInboxNotificationOptions(item: TaskInboxItem): NotificationOptions {
  return {
    body: taskInboxNotificationBody(item),
    tag: `agentapi-${taskInboxNotificationKey(item)}`,
    icon: "/logo.png",
    data: {
      url: "/app",
      task_id: item.id,
      kind: item.kind,
      session_id: item.session_id,
      job_id: item.job_id,
      loop_goal_id: item.loop_goal_id,
      artifact_id: item.primary_artifact_id || item.artifact_id
    }
  };
}

function taskInboxNotificationTitle(item: TaskInboxItem): string {
  switch (item.notification_type) {
    case "review_required":
      return "Review required";
    case "job_failed":
      return "Task failed";
    case "quota_blocked":
      return "Task blocked";
    case "loop_triggered":
      return "Loop update";
    case "job_completed":
      return item.artifact_count > 0 ? "Artifact ready" : "Task completed";
    default:
      return "Task update";
  }
}

function taskInboxNotificationBody(item: TaskInboxItem): string {
  const title = item.title || item.last_event || "Task updated";
  const details = item.last_event && item.last_event !== title ? ` - ${item.last_event}` : "";
  return `${title}${details}`;
}
