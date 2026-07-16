// Kratos wrapper (KAC-127 Phase 2).
//
// Зачем не `@ory/client`: SDK тащит axios + 200KB JSON-моделей, из которых
// нам нужно ~5 endpoint'ов. Свой thin-wrapper над fetch — простой,
// типизированный, без depend hell.
//
// Endpoint contract (Kratos self-service browser flow):
//   POST   /self-service/login/browser         → 303 with ?flow=<id>     (init login)
//   GET    /self-service/login/flows?id=...    → flow JSON + UI schema
//   POST   /self-service/login?flow=<id>       → submit credentials (csrf+method+...)
//   POST   /self-service/registration/browser  → 303 with ?flow=<id>     (init registration)
//   GET    /self-service/registration/flows?id=...  → flow JSON
//   POST   /self-service/registration?flow=<id>     → submit
//   POST   /self-service/recovery/browser      → init recovery
//   GET    /self-service/recovery/flows?id=...
//   POST   /self-service/recovery?flow=<id>    → submit (email → magic-link)
//   GET    /self-service/settings/browser      → init settings (управление passkeys / TOTP)
//   GET    /self-service/settings/flows?id=...
//   POST   /self-service/settings?flow=<id>
//   GET    /self-service/logout/browser        → init logout
//   POST   /self-service/logout?token=<token>
//   GET    /sessions/whoami                    → 200 session | 401 unauthorised
//
// Все запросы — `credentials: 'include'` для cookie `ory_kratos_session`.

import { config, kratosUrl } from "@shared/lib/config";

export type FlowType = "login" | "registration" | "recovery" | "settings" | "verification";

export interface UiNodeAttribute {
  name?: string;
  type?: string;
  value?: unknown;
  required?: boolean;
  disabled?: boolean;
  // WebAuthn-specific (Kratos rendered nodes):
  onclick?: string;
  // Free-form — Kratos returns rich metadata.
  [k: string]: unknown;
}

export interface UiNode {
  type: "input" | "img" | "a" | "text" | "script";
  group: string; // "default", "password", "totp", "webauthn", "lookup_secret", ...
  attributes: UiNodeAttribute;
  messages?: UiText[];
  meta?: { label?: UiText };
}

export interface UiText {
  id?: number;
  text: string;
  type?: "info" | "error" | "success";
  context?: Record<string, unknown>;
}

export interface UiContainer {
  action: string;
  method: string;
  nodes: UiNode[];
  messages?: UiText[];
}

export interface SelfServiceFlow {
  id: string;
  type: "browser" | "api";
  expires_at: string;
  issued_at: string;
  request_url: string;
  return_to?: string;
  ui: UiContainer;
  // Login/registration specific:
  refresh?: boolean;
  requested_aal?: "aal1" | "aal2";
  // Recovery / settings specific:
  state?: string;
}

export interface KratosSession {
  id: string;
  active: boolean;
  expires_at: string;
  authenticated_at: string;
  authenticator_assurance_level: "aal0" | "aal1" | "aal2";
  authentication_methods?: Array<{
    method: string;
    aal: string;
    completed_at: string;
  }>;
  identity: {
    id: string;
    schema_id: string;
    traits: Record<string, unknown>;
    metadata_public?: Record<string, unknown>;
  };
}

export interface KratosError extends Error {
  status: number;
  ui?: UiContainer;
  redirect_browser_to?: string;
  flow_id?: string;
}

function buildError(status: number, body: unknown): KratosError {
  const err = new Error(`Kratos ${status}`) as KratosError;
  err.status = status;
  if (body && typeof body === "object") {
    const b = body as Record<string, unknown>;
    if (b.ui) err.ui = b.ui as UiContainer;
    if (b.redirect_browser_to) err.redirect_browser_to = String(b.redirect_browser_to);
    if (b.id) err.flow_id = String(b.id);
    if (typeof b.error === "object" && b.error !== null) {
      const e = b.error as { message?: string; reason?: string };
      err.message = e.message || e.reason || err.message;
    }
  }
  return err;
}

async function kratosFetch<T>(method: string, path: string, body?: unknown, init?: RequestInit): Promise<T> {
  const url = kratosUrl(path);
  const headers: Record<string, string> = {
    Accept: "application/json",
    ...(init?.headers as Record<string, string> | undefined),
  };
  if (body !== undefined) {
    headers["Content-Type"] = "application/json";
  }
  const res = await fetch(url, {
    method,
    credentials: "include",
    headers,
    body: body !== undefined ? JSON.stringify(body) : undefined,
    redirect: "manual",
    ...init,
  });
  const text = await res.text();
  let parsed: unknown = null;
  if (text) {
    try {
      parsed = JSON.parse(text);
    } catch {
      // fallthrough
    }
  }
  if (!res.ok) {
    throw buildError(res.status, parsed);
  }
  return parsed as T;
}

/** Извлечь UI-node по name (input fields). */
export function findNode(ui: UiContainer, name: string): UiNode | undefined {
  return ui.nodes.find((n) => n.type === "input" && n.attributes && n.attributes.name === name);
}

/** Получить CSRF-token из flow.ui. */
export function csrfToken(ui: UiContainer): string {
  const node = findNode(ui, "csrf_token");
  return node?.attributes?.value ? String(node.attributes.value) : "";
}

/** Считать список доступных «методов» для login (по UI-nodes group). */
export function availableMethods(ui: UiContainer): string[] {
  const set = new Set<string>();
  for (const n of ui.nodes) {
    if (n.group && n.group !== "default") set.add(n.group);
  }
  return Array.from(set);
}

/** Получить UI-сообщения уровня flow (errors / info). */
export function flowMessages(ui: UiContainer): UiText[] {
  return ui.messages ?? [];
}

/** Получить error-сообщения для конкретного node. */
export function nodeMessages(ui: UiContainer, name: string): UiText[] {
  const n = findNode(ui, name);
  return n?.messages ?? [];
}

// ─────────────────────────────────────────────────────────────────────────────
// Public API
// ─────────────────────────────────────────────────────────────────────────────

export const kratos = {
  /** Init browser-flow (отдаст 303 в Kratos; SPA берёт ID из query). */
  initFlowUrl(flow: FlowType, returnTo?: string): string {
    const params = new URLSearchParams();
    if (returnTo) params.set("return_to", returnTo);
    const qs = params.toString();
    return `${kratosUrl(`/self-service/${flow}/browser`)}${qs ? `?${qs}` : ""}`;
  },

  /** Получить flow по ID (после init redirect). */
  async getFlow<T = SelfServiceFlow>(flow: FlowType, id: string): Promise<T> {
    const params = new URLSearchParams({ id });
    return kratosFetch<T>("GET", `/self-service/${flow}/flows?${params}`);
  },

  /** Submit flow с body. */
  async submitFlow<T = SelfServiceFlow>(flow: FlowType, id: string, body: Record<string, unknown>): Promise<T> {
    const params = new URLSearchParams({ flow: id });
    return kratosFetch<T>("POST", `/self-service/${flow}?${params}`, body);
  },

  /** Текущая session — 200 / 401. */
  async whoami(): Promise<KratosSession | null> {
    try {
      return await kratosFetch<KratosSession>("GET", "/sessions/whoami");
    } catch (e) {
      if ((e as KratosError).status === 401 || (e as KratosError).status === 403) {
        return null;
      }
      throw e;
    }
  },

  /** Init logout — возвращает {logout_token, logout_url}. */
  async initLogout(): Promise<{ logout_token: string; logout_url: string }> {
    return kratosFetch("GET", "/self-service/logout/browser");
  },

  /** Submit logout. */
  async submitLogout(logoutToken: string): Promise<void> {
    await kratosFetch("GET", `/self-service/logout?token=${encodeURIComponent(logoutToken)}`);
  },

  /** Url для прямого browser-redirect (когда нужен server-side return_to flow). */
  loginUrl(returnTo?: string): string {
    return this.initFlowUrl("login", returnTo);
  },
  registrationUrl(returnTo?: string): string {
    return this.initFlowUrl("registration", returnTo);
  },
  recoveryUrl(returnTo?: string): string {
    return this.initFlowUrl("recovery", returnTo);
  },
  settingsUrl(returnTo?: string): string {
    return this.initFlowUrl("settings", returnTo);
  },

  /** WebAuthn RP-ID (для navigator.credentials.create options). */
  webauthnRpId(): string {
    return config.webauthnRpId;
  },
  webauthnRpName(): string {
    return config.webauthnRpName;
  },
};

/** Re-export для удобства unit-тестов и других модулей. */
export { kratosUrl };
