import { bearerEpoch } from "@shared/api/lane-epochs";
import { refusalActionOf } from "@shared/api/refusal-action";
import { parseRpcStatus, reasonOfDetails } from "@shared/api/rpc-status";
import { acrFromChallenge, challengeError, challengeOf, requestStepUp } from "@shared/api/step-up";
import { redirectToLogin } from "./auth";

export async function apiList<T>(path: string, query?: Record<string, string>): Promise<T> {
  const qs = query && Object.keys(query).length > 0 ? `?${new URLSearchParams(query).toString()}` : "";
  return apiGet<T>(`${path}${qs}`);
}

/**
 * Чтение каркаса. Отказ ведёт к действию ОДНОГО решения консоли
 * (`refusalActionOf`, приёмка F8, условия C2 и C15):
 *
 *   • вызов пола уровня — церемония повышения и ОДИН повтор; на вход не уводит:
 *     человек вошёл, ему не хватает уровня, а не сессии;
 *   • `invalid_token` после того, как ЭТА вкладка перевыпустила носитель, пока
 *     запрос шёл, — ОДИН повтор с текущим носителем (условие C18): сессия не
 *     кончилась, её перевыпустили здесь же;
 *   • прочий `401` — сессии нет: экран входа с адресом возврата;
 *   • остальное — отказ с текстом ответа.
 *
 * Статус здесь не выбирает действие сам: у `401` края три смысла.
 */
export async function apiGet<T>(path: string, replayed = false): Promise<T> {
  const issuedAt = bearerEpoch();
  const res = await fetch(path, {
    credentials: "include",
    headers: {
      "Content-Type": "application/json",
      "X-Request-ID": makeRequestId(),
    },
  });
  const text = await res.text();
  // Status handling must not depend on the body being JSON: a gateway/nginx
  // 401 or 5xx may return an HTML/plaintext page. Parse defensively so a
  // non-JSON body cannot mask the real HTTP status.
  let parsed: unknown = null;
  if (text) {
    try {
      parsed = JSON.parse(text) as unknown;
    } catch {
      // Non-JSON body (e.g. an error page) — leave parsed as null.
    }
  }
  if (!res.ok) {
    const www = challengeOf(res);
    const status = parseRpcStatus(text);
    const action = refusalActionOf(
      {
        status: res.status,
        reason: status ? reasonOfDetails(status.details) : null,
        challenge: challengeError(www),
        rotatedSinceIssue: bearerEpoch() !== issuedAt,
      },
      "platform",
    );
    if (!replayed && action === "replay") return apiGet<T>(path, true);
    if (!replayed && action === "step-up-floor" && (await requestStepUp(acrFromChallenge(www)))) {
      return apiGet<T>(path, true);
    }
    if (action === "sign-in") redirectToLogin();
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
