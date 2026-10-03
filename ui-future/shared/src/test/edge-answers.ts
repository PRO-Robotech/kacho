// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import type { LaneAnswer } from "./lane-fake";

/**
 * Ответы КРАЯ на предъявленное удостоверение — в той форме, в какой их отдаёт
 * производитель (приёмка KA1, Р1 и Р2; kacho#2728, kacho#2958). Одно место для
 * модульных проб консоли: тело и заголовки набраны здесь по коду производителя,
 * а пробы берут их отсюда, а не набирают рукой, — рукописная копия пережила бы
 * смену формы края и продолжала бы зеленеть на ответе, которого край больше не
 * производит.
 *
 * ПРОИЗВОДИТЕЛИ
 *
 *   Р2 — `gateway/internal/authnrefusal/authnrefusal.go`, `WriteHTTP`: ОДИН отказ
 *        `401` на все причины непринятия удостоверения. Причины в ответе нет —
 *        ни в тексте, ни в вызове (`error_description` снят); `error="invalid_token"`
 *        сохранён, по нему консоль повторяет вопрос о сессии. На полосе
 *        браузерной сессии отказ вдобавок гасит носитель — печенье здесь не
 *        набирается: дублёр сети консоли печенья не держит;
 *   Р1 — `gateway/internal/middleware/credential_state_unknown.go`,
 *        `writeCredentialStateUnknown`: наш авторитет не ответил о предъявленном —
 *        `503`, без вызова и без печенья: носитель цел.
 *
 * Указание повысить уровень (Р3, `insufficient_user_authentication`) отказом не
 * является и в KA1 не менялось — его набирают пробы повышения.
 */
export interface EdgeAnswer {
  /** Кто производит и где — координата для того, кто сверяет. */
  producer: string;
  status: number;
  headers: Record<string, string>;
  /** Тело побайтово так, как его пишет производитель. */
  text: string;
}

export const EDGE_REFUSAL_REASON = "AUTHN_REQUIRED";
export const EDGE_REFUSAL_MESSAGE = "authentication failed";
export const EDGE_UNANSWERED_MESSAGE = "credential state could not be established";

/** Р2: удостоверение не принято — один отказ на все причины. */
export const EDGE_AUTHN_FAILED: EdgeAnswer = {
  producer: "gateway authnrefusal.WriteHTTP",
  status: 401,
  headers: {
    "Content-Type": "application/json",
    "WWW-Authenticate": 'Bearer realm="kacho", error="invalid_token"',
  },
  text: `{"code":16,"message":"${EDGE_REFUSAL_MESSAGE}","details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo","reason":"${EDGE_REFUSAL_REASON}","domain":"kaname.cloud.iam.v1"}]}`,
};

/** Р1: наш авторитет не ответил — состояние удостоверения не установлено. */
export const EDGE_CREDENTIAL_STATE_UNKNOWN: EdgeAnswer = {
  producer: "gateway middleware.writeCredentialStateUnknown",
  status: 503,
  headers: { "Content-Type": "application/json" },
  // Пишет `json.Encoder` словаря: ключи по алфавиту и перевод строки в конце.
  text: `{"code":14,"message":"${EDGE_UNANSWERED_MESSAGE}"}\n`,
};

/**
 * Тот же ответ для дублёра полосы (`lane-fake.ts`): дублёр сериализует тело сам,
 * и разобранное тело сериализуется в тот же текст — порядок полей сохраняется.
 */
export function laneAnswerOf(a: EdgeAnswer): LaneAnswer {
  return { status: a.status, headers: a.headers, body: JSON.parse(a.text) as unknown };
}

/** Тот же ответ так, как его видит `fetch`, — для проб, подменяющих сеть целиком. */
export function responseOf(a: EdgeAnswer): Promise<Response> {
  const h = Object.fromEntries(Object.entries(a.headers).map(([k, v]) => [k.toLowerCase(), v]));
  return Promise.resolve({
    ok: false,
    status: a.status,
    statusText: String(a.status),
    headers: { get: (n: string) => h[n.toLowerCase()] ?? null },
    text: () => Promise.resolve(a.text),
  } as unknown as Response);
}
