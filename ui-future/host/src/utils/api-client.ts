import { redirectToLogin } from "./auth";

export async function apiList<T>(path: string, query?: Record<string, string>): Promise<T> {
  const qs = query && Object.keys(query).length > 0 ? `?${new URLSearchParams(query).toString()}` : "";
  return apiGet<T>(`${path}${qs}`);
}

export async function apiGet<T>(path: string): Promise<T> {
  const res = await fetch(path, {
    credentials: "include",
    headers: {
      "Content-Type": "application/json",
      "X-Request-ID": makeRequestId(),
    },
  });
  const text = await res.text();
  // Status handling must not depend on the body being JSON: a gateway/nginx
  // 401 or 5xx may return an HTML/plaintext page. Redirect on 401 first, and
  // parse defensively so a non-JSON body cannot mask the real HTTP status.
  if (res.status === 401) {
    redirectToLogin();
  }
  let parsed: unknown = null;
  if (text) {
    try {
      parsed = JSON.parse(text) as unknown;
    } catch {
      // Non-JSON body (e.g. an error page) — leave parsed as null.
    }
  }
  if (!res.ok) {
    const err = (parsed ?? {}) as { message?: string };
    throw new Error(err.message ?? res.statusText);
  }
  return parsed as T;
}

function makeRequestId(): string {
  try {
    if (typeof crypto !== "undefined" && typeof crypto.randomUUID === "function") {
      return crypto.randomUUID();
    }
  } catch {
    // ignore
  }
  return `${Math.random().toString(36).slice(2, 10)}-${Date.now().toString(36)}`;
}
