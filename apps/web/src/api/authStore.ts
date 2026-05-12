import type { AuthSession } from "../types";

const KEY = "agentapi.web.auth";

export function loadAuth(): AuthSession | null {
  try {
    const raw = localStorage.getItem(KEY);
    return raw ? (JSON.parse(raw) as AuthSession) : null;
  } catch {
    return null;
  }
}

export function saveAuth(session: AuthSession): void {
  localStorage.setItem(KEY, JSON.stringify(session));
}

export function clearAuth(): void {
  localStorage.removeItem(KEY);
}
