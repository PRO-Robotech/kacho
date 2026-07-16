// Auth API — обращения к Ory Kratos self-service endpoints + api-gateway /iam/v1/auth/me.
//
// Контракт (KAC-115 Ory stack):
//   GET  /login                       → Kratos self-service Login UI
//                                       (Kratos выставляет ory_kratos_session cookie)
//   GET  /registration                → Kratos self-service Registration UI
//   GET  /.ory/kratos/public/sessions/whoami
//                                    → 200 session.identity | 401 если cookie не валидна
//   GET  /iam/v1/auth/me             → 200 {user, permissions[]} | 401 если нет session
//                                       (api-gateway резолвит principal по Kratos session)
//   GET  /iam/v1/me                  → 200 WhoAmIResponse (KAC items 1-5):
//                                       subject + user_id + email + display_name +
//                                       system_admin + cluster_viewer + accounts[]
//   GET  /logout                      → Kratos self-service logout flow (token-based)
//
// Все запросы — `credentials: 'include'` для cookie ory_kratos_session.

import { camelToSnake } from "@shared/lib/case";

export type SubjectType = "user" | "service_account" | "system";

export interface AuthUser {
  /** Внутренний User.id (`usr-...`) либо ServiceAccount.id (`sva-...`). */
  id: string;
  /** Display name из Zitadel (email или ФИО). */
  display_name?: string;
  email?: string;
  subject_type: SubjectType;
  /** Account.id (если default-account резолвится). E0 — может быть пусто. */
  account_id?: string;
  /** Effective permissions (E3 OpenFGA). E0 — может быть пусто или содержать `*` для admin. */
  permissions?: string[];
}

export interface AuthMeResponse {
  user: AuthUser;
}

// ====== WhoAmIResponse (KAC items 1-5) ======
//
// GET /iam/v1/me — единая ручка, отдающая всё что нужно UI для bootstrap-а
// разрешений и навигации. Backend строит ответ на основе принципала (Kratos
// session → IAM Subject) + FGA (cluster-level relations + per-account roles).
//
// Wire-format: api-gateway сериализует proto в JSON camelCase; адаптер
// `api/client.ts` конвертирует в snake_case на приёме, поэтому здесь поля в
// snake_case (consistent с iam.ts).

/** Per-account роль user'а — массив role IDs / human names. */
export interface AccountMembership {
  account_id: string;
  account_name: string;
  /** Список ролей user'а в этом аккаунте (role.id или role.name). */
  roles: string[];
}

/** Ответ GET /iam/v1/me — bootstrap-info для UI permission-gate'ов. */
export interface WhoAmIResponse {
  /** `user:<id>` или `service_account:<id>` — каноническая subject-form для FGA. */
  subject: string;
  /** User.id (если subject = user). Для service_account — пусто. */
  user_id?: string;
  email?: string;
  display_name?: string;
  /** True если у user'а FGA-relation `admin@cluster:cluster_kacho_root` (KAC item #4). */
  system_admin: boolean;
  /** True если у user'а FGA-relation `viewer@cluster:cluster_kacho_root`. */
  cluster_viewer: boolean;
  /** Аккаунты, в которых user является членом (с ролями). */
  accounts: AccountMembership[];
}

/**
 * Структурированная причина отказа в 403-ответе (KAC item #4 rich deny_reasons).
 * Backend складывает их в `error.details[].metadata.deny_reasons`.
 */
export interface DenyReason {
  /** Краткий машинный код причины — e.g. "missing_relation", "wrong_account". */
  reason: string;
  /** Человекочитаемое описание для toast / inline-error. */
  message: string;
  /** Какой FGA-relation требовался (если применимо). */
  required_relation?: string;
  /** Какой ресурс был проверен. */
  resource?: string;
}

async function fetchAuth<T>(method: string, path: string, body?: unknown): Promise<T> {
  const init: RequestInit = {
    method,
    credentials: "include",
    headers: { "Content-Type": "application/json" },
  };
  if (body !== undefined) {
    init.body = JSON.stringify(camelToSnake(body));
  }
  const res = await fetch(path, init);
  if (!res.ok) {
    // 401 — нормальный «не залогинен» сигнал, не Error.
    const err = new Error(`${res.status} ${res.statusText}`) as Error & {
      status: number;
    };
    err.status = res.status;
    throw err;
  }
  if (res.status === 204) return undefined as T;
  const text = await res.text();
  if (!text) return undefined as T;
  return JSON.parse(text) as T;
}

/** Универсальный fetch к /iam/v1/me с `camelToSnake` адаптацией ответа. */
async function fetchWhoAmI(): Promise<WhoAmIResponse> {
  const res = await fetch("/iam/v1/me", {
    method: "GET",
    credentials: "include",
    headers: { Accept: "application/json" },
  });
  if (!res.ok) {
    const err = new Error(`${res.status} ${res.statusText}`) as Error & {
      status: number;
    };
    err.status = res.status;
    throw err;
  }
  const text = await res.text();
  if (!text) {
    return {
      subject: "",
      system_admin: false,
      cluster_viewer: false,
      accounts: [],
    };
  }
  // grpc-gateway эмит camelCase; адаптируем к snake_case через `camelToSnake`
  // (тот же контракт, что и `api/client.ts`).
  const parsed = JSON.parse(text);
  const adapted = camelToSnake<Record<string, unknown>>(parsed);
  return {
    subject: String(adapted.subject ?? ""),
    user_id: adapted.user_id as string | undefined,
    email: adapted.email as string | undefined,
    display_name: adapted.display_name as string | undefined,
    system_admin: Boolean(adapted.system_admin),
    cluster_viewer: Boolean(adapted.cluster_viewer),
    accounts: ((adapted.accounts as AccountMembership[] | undefined) ?? []).map((a) => ({
      account_id: String(a.account_id ?? ""),
      account_name: String(a.account_name ?? ""),
      roles: Array.isArray(a.roles) ? a.roles.map(String) : [],
    })),
  };
}

export const authApi = {
  /** Перейти на Kratos self-service login page. */
  login(): void {
    window.location.assign("/login");
  },

  /** Перейти на Kratos self-service registration page. */
  register(): void {
    window.location.assign("/registration");
  },

  /** Получить текущего user'а. 401 → AuthContext выставит user=null. */
  me(): Promise<AuthMeResponse> {
    return fetchAuth<AuthMeResponse>("GET", "/iam/v1/auth/me");
  },

  /**
   * GET /iam/v1/me — bootstrap-info для permission-gate'ов (KAC items 1-5).
   * 401/403 → throw {status} — AuthContext выставит whoami=null.
   */
  whoami(): Promise<WhoAmIResponse> {
    return fetchWhoAmI();
  },

  /** Запустить Kratos logout flow — POST к /.ory/kratos/public/self-service/logout/browser
   * сначала получит logout_token, потом редирект на logout-url. Простейший вариант — full-page nav. */
  logout(): void {
    window.location.assign("/.ory/kratos/public/self-service/logout/browser");
  },
};

/** Проверка permission, толерантная к admin `*` wildcard. */
export function hasPermission(user: AuthUser | null, perm: string): boolean {
  if (!user) return false;
  const perms = user.permissions ?? [];
  return perms.includes("*") || perms.includes(perm);
}

/**
 * Извлекает массив `DenyReason` из ApiError.details (KAC item #4):
 * 403-ответ имеет форму:
 *   { code: 7, message: "...", details: [
 *       { "@type": ".../ErrorInfo", metadata: { deny_reasons: [{...}, ...] } }
 *   ]}
 * Возвращает [] если структуры нет — caller fallback'ит на generic message.
 */
export function extractDenyReasons(details: unknown): DenyReason[] {
  if (!Array.isArray(details)) return [];
  const out: DenyReason[] = [];
  for (const d of details) {
    if (!d || typeof d !== "object") continue;
    const md = (d as { metadata?: unknown }).metadata;
    if (!md || typeof md !== "object") continue;
    const raw =
      (md as { deny_reasons?: unknown; denyReasons?: unknown }).deny_reasons ??
      (md as { denyReasons?: unknown }).denyReasons;
    if (!Array.isArray(raw)) continue;
    for (const r of raw) {
      if (!r || typeof r !== "object") continue;
      const rr = r as Record<string, unknown>;
      out.push({
        reason: String(rr.reason ?? ""),
        message: String(rr.message ?? rr.reason ?? "Permission denied"),
        required_relation: (rr.required_relation as string | undefined) ?? (rr.requiredRelation as string | undefined),
        resource: rr.resource as string | undefined,
      });
    }
  }
  return out;
}
