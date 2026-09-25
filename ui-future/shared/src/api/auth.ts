// Auth API — bootstrap прав вызывающего (`GET /iam/v1/me`) и проверка права.
//
// «Кто за браузерной сессией» здесь НЕ читается: читатель `GET /iam/v1/auth/me`
// в консоли один — `sessionIdentity` клиента полосы формы
// (`@shared/api/login-lane`, условия C6, C7, C9). Прежде здесь стоял второй
// читатель того же ответа, и он объявлял человека в snake_case, тогда как край
// пишет camelCase, — объявленные поля приходили пустыми, и второй тип того же
// провода расходился с первым молча. Контекст личности берёт человека у
// единственного читателя, в форме провода.

import { orderedTransport } from "@shared/api/carrier-order";
import type { SessionUser } from "@shared/api/login-lane";
import { camelToSnake } from "@shared/lib/case";
import { displayText } from "@shared/lib/display-text";

/** Человек за сессией — в форме провода ответа края (`/iam/v1/auth/me`). */
export type AuthUser = SessionUser;

// ====== WhoAmIResponse (KAC items 1-5) ======
//
// GET /iam/v1/me — единая ручка, отдающая всё что нужно UI для bootstrap-а
// разрешений и навигации. Backend строит ответ на основе принципала (нашей
// сессии → IAM Subject) + FGA (cluster-level relations + per-account roles).
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
  /** True если у user'а FGA-relation `admin@cluster:cluster_root` (KAC item #4). */
  system_admin: boolean;
  /** True если у user'а FGA-relation `viewer@cluster:cluster_root`. */
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

/** Универсальный fetch к /iam/v1/me с `camelToSnake` адаптацией ответа. */
async function fetchWhoAmI(): Promise<WhoAmIResponse> {
  const res = await orderedTransport.fetch("/iam/v1/me", {
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
    subject: displayText(adapted.subject),
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
  /**
   * GET /iam/v1/me — bootstrap-info для permission-gate'ов (KAC items 1-5).
   * 401/403 → throw {status} — AuthContext выставит whoami=null.
   */
  whoami(): Promise<WhoAmIResponse> {
    return fetchWhoAmI();
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
        reason: displayText(rr.reason),
        message: displayText(rr.message) || displayText(rr.reason) || "Недостаточно прав",
        required_relation: (rr.required_relation as string | undefined) ?? (rr.requiredRelation as string | undefined),
        resource: rr.resource as string | undefined,
      });
    }
  }
  return out;
}
