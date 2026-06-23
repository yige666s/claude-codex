import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  browserTaskNotificationPermission,
  browserTaskNotificationsEnabled,
  requestBrowserTaskNotifications,
  setBrowserTaskNotificationsEnabled,
  subscribeBrowserTaskPush,
  taskInboxNotificationKey
} from "../lib/browserNotifications";
import type { TaskInboxItem } from "../types";

describe("browser task notifications", () => {
  beforeEach(() => {
    vi.stubGlobal("localStorage", memoryStorage());
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("reports unsupported when the Notification API is missing", () => {
    vi.stubGlobal("window", {});
    vi.stubGlobal("Notification", undefined);

    expect(browserTaskNotificationPermission()).toBe("unsupported");
    expect(browserTaskNotificationsEnabled()).toBe(false);
  });

  it("requires both browser permission and local preference", () => {
    stubNotification("granted");

    expect(browserTaskNotificationsEnabled()).toBe(false);
    setBrowserTaskNotificationsEnabled(true);
    expect(browserTaskNotificationsEnabled()).toBe(true);
  });

  it("persists the preference only after permission is granted", async () => {
    const requestPermission = vi.fn(async () => {
      Object.defineProperty(Notification, "permission", { configurable: true, value: "granted" });
      return "granted" as NotificationPermission;
    });
    stubNotification("default", requestPermission);

    await expect(requestBrowserTaskNotifications()).resolves.toBe("granted");

    expect(requestPermission).toHaveBeenCalledTimes(1);
    expect(browserTaskNotificationsEnabled()).toBe(true);
  });

  it("keys notifications by task identity and latest state", () => {
    const item = taskInboxItem({ id: "job-1", status: "running", updated_at: "2026-06-22T01:00:00Z" });
    const changed = taskInboxItem({ id: "job-1", status: "succeeded", updated_at: "2026-06-22T01:05:00Z" });

    expect(taskInboxNotificationKey(item)).not.toBe(taskInboxNotificationKey(changed));
  });

  it("waits for the service worker to become active before subscribing to push", async () => {
    stubNotification("granted");
    const subscribe = vi.fn(async () => ({ endpoint: "https://push.example/sub" }));
    const registration = {
      active: null as unknown,
      pushManager: {
        getSubscription: vi.fn(async () => null),
        subscribe
      }
    };
    let resolveReady: (activeRegistration: ServiceWorkerRegistration) => void = () => undefined;
    const ready = new Promise<ServiceWorkerRegistration>((resolve) => {
      resolveReady = resolve;
    });
    vi.stubGlobal("navigator", {
      serviceWorker: {
        register: vi.fn(async () => registration),
        ready
      }
    });

    const subscriptionPromise = subscribeBrowserTaskPush("AQ");

    expect(subscribe).not.toHaveBeenCalled();
    registration.active = {};
    resolveReady(registration as unknown as ServiceWorkerRegistration);
    await expect(subscriptionPromise).resolves.toEqual({ endpoint: "https://push.example/sub" });
    expect(subscribe).toHaveBeenCalledWith({
      userVisibleOnly: true,
      applicationServerKey: expect.any(ArrayBuffer)
    });
  });
});

function stubNotification(permission: NotificationPermission, requestPermission = vi.fn(async () => permission)) {
  class FakeNotification {
    static permission = permission;
    static requestPermission = requestPermission;
  }
  vi.stubGlobal("Notification", FakeNotification);
  vi.stubGlobal("window", {
    Notification: FakeNotification,
    PushManager: class FakePushManager {},
    atob: (value: string) => Buffer.from(value, "base64").toString("binary")
  });
}

function taskInboxItem(patch: Partial<TaskInboxItem>): TaskInboxItem {
  return {
    id: "task-1",
    kind: "job",
    group: "completed",
    status: "succeeded",
    title: "Task",
    session_id: "session-1",
    job_id: "job-1",
    loop_goal_id: "",
    trigger: "",
    last_event: "done",
    last_event_at: "2026-06-22T01:00:00Z",
    updated_at: "2026-06-22T01:00:00Z",
    next_action: "Open",
    notification_type: "job_completed",
    artifact_count: 0,
    review: undefined,
    created_at: "2026-06-22T00:59:00Z",
    ...patch
  };
}

function memoryStorage(): Storage {
  const values = new Map<string, string>();
  return {
    get length() {
      return values.size;
    },
    clear: () => values.clear(),
    getItem: (key) => values.get(key) ?? null,
    key: (index) => Array.from(values.keys())[index] ?? null,
    removeItem: (key) => {
      values.delete(key);
    },
    setItem: (key, value) => {
      values.set(key, value);
    }
  };
}
