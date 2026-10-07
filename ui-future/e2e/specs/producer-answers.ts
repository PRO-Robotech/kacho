// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import type { Route } from "@playwright/test";
import {
  EDGE_AUTHN_FAILED as EDGE_AUTHN_FAILED_SOURCE,
  EDGE_CREDENTIAL_STATE_UNKNOWN as EDGE_CREDENTIAL_STATE_UNKNOWN_SOURCE,
  type EdgeAnswer,
} from "../../shared/src/test/edge-answers";

/**
 * Ответы, которые проба ПОДСТАВЛЯЕТ, — в той форме, в какой их отдаёт
 * производитель: тело И заголовки (приёмка F8, условие C20).
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ЗАЧЕМ ОДНО МЕСТО
 *
 * Подстановка нужна там, где условие нельзя создать иначе: служба не отвечает
 * краю (F8-10, F8-19, F8-25) и сессия не свежа (F8-29, Р9 ось 3а). Подставленный
 * ответ снисходительнее настоящего — и проба зеленеет на экране, который на
 * настоящем ответе ломается: у ответа края F8-25 (приёмка KA1, Р1) статус `503`,
 * НЕТ вызова `WWW-Authenticate` и НЕТ поля `details`, и экран, читающий их
 * иначе, чем рукописное тело, проба бы не поймала. Поэтому тела и заголовки
 * собраны здесь по коду производителя, с координатой, и пробы берут их отсюда,
 * а не набирают рукой.
 *
 * ПРОИЗВОДИТЕЛИ
 *
 *   служба доступа — `kaname` `internal/handler/loginlanehttp/handler.go`:
 *     `writeRefusal` (тело `{code, message, details}`, `details` — всегда
 *     массив, `ErrorInfo` с доменом отказа) и `writeJSON` (`Content-Type:
 *     application/json`, `Cache-Control: no-store`);
 *   край — ответы на предъявленное удостоверение (приёмка KA1) набраны ОДИН
 *     раз, в `shared/src/test/edge-answers.ts`, и сверены там с литералами
 *     производителя модульной пробой (`edge-answers.test.ts`): отказ `401`
 *     (Р2, `gateway/internal/authnrefusal`) — один на все причины, с вызовом
 *     `Bearer realm="kacho", error="invalid_token"` без `error_description` и
 *     причиной `AUTHN_REQUIRED`; ответ `503` на молчание авторитета (Р1) — без
 *     вызова и без `details`.
 *
 * ЧЕГО ЗДЕСЬ НЕТ. Захвата с живого стенда: подстановка собрана по коду
 * производителя, и расхождение кода и развёрнутого образа она не увидит.
 * Захват ответа на цепочке own — заказ сквозной пробе API (newman), он
 * назван в возврате полосы.
 */

export interface ProducerAnswer {
  /** Кто производит и где — координата для того, кто сверяет. */
  producer: string;
  status: number;
  headers: Record<string, string>;
  body: string;
}

const LANE_HEADERS = { "Content-Type": "application/json", "Cache-Control": "no-store" };
const REFUSAL_DOMAIN = "iam.kaname.cloud";

function laneRefusal(producer: string, status: number, code: number, message: string, reason?: string): ProducerAnswer {
  const details = reason
    ? [{ "@type": "type.googleapis.com/google.rpc.ErrorInfo", reason, domain: REFUSAL_DOMAIN }]
    : [];
  return { producer, status, headers: LANE_HEADERS, body: JSON.stringify({ code, message, details }) };
}

function edgeAnswer(a: EdgeAnswer): ProducerAnswer {
  return { producer: a.producer, status: a.status, headers: a.headers, body: a.text };
}

/** Служба не выполнила глагол (не ответило хранилище) — `writeError`, ветвь недоступности. */
export const LANE_UNAVAILABLE = laneRefusal(
  "kaname loginlanehttp.writeError → writeRefusal(503, UNAVAILABLE, unavailableText)",
  503,
  14,
  "request not performed; try again later",
);

/** Служба не выполнила выход — свой текст недоступности у глагола выхода. */
export const LOGOUT_UNAVAILABLE = laneRefusal(
  "kaname loginlanehttp.logout → writeError(…, «logout not performed…»)",
  503,
  14,
  "logout not performed; try again later",
);

/**
 * Отказ по частоте — `writeError`, ветвь `TooManyAttemptsError` (приёмка F8-S3,
 * средство П-О, F8S3-11): `429`, тело `{code: 8, message: TextTooManyAttempts,
 * details: [ErrorInfo TOO_MANY_ATTEMPTS]}` и заголовок `Retry-After` целыми
 * секундами (`retryAfterSeconds`, не меньше одной). Срок — вход подстановки:
 * служба называет его своим счётом, и проба называет тот, который утверждает.
 */
export function laneTooManyAttempts(retryAfterSeconds: number): ProducerAnswer {
  const answer = laneRefusal(
    "kaname loginlanehttp.writeError → Retry-After + writeRefusal(429, RESOURCE_EXHAUSTED, TextTooManyAttempts, TOO_MANY_ATTEMPTS)",
    429,
    8,
    "too many attempts; try again later",
    "TOO_MANY_ATTEMPTS",
  );
  return { ...answer, headers: { ...answer.headers, "Retry-After": String(retryAfterSeconds) } };
}

/** Сессия не свежа — `ErrSessionNotFresh`. */
export const SESSION_NOT_FRESH = laneRefusal(
  "kaname loginlanehttp.writeError → writeRefusal(403, PERMISSION_DENIED, TextSessionNotFresh, SESSION_NOT_FRESH)",
  403,
  7,
  "re-authentication required: present a credential again",
  "SESSION_NOT_FRESH",
);

/**
 * Сессия не свежа на глаголе ключа доступа (Ф7) — отказ службы СИНХРОННО, до
 * операции (приёмка F8, N28): `api/access_keys/refusals.go` `sessionNotFresh` →
 * `withReason(PERMISSION_DENIED, SESSION_NOT_FRESH, TextSessionNotFresh)` с
 * `ErrorInfo` домена отказа службы. Глаголы Ф7 — поверхность платформы, а не
 * полоса формы: тело отдаёт край разбором `google.rpc.Status` по-умолчанию
 * (`403`, `{code, message, details}`, `Content-Type: application/json`).
 * Средство оси 3 (Р9) у F8-51 и F8-57: подставляется ТОЛЬКО первый ответ.
 */
export const ACCESS_KEY_SESSION_NOT_FRESH: ProducerAnswer = {
  producer:
    "kaname api/access_keys.sessionNotFresh → withReason(PERMISSION_DENIED, SESSION_NOT_FRESH, TextSessionNotFresh) → край: google.rpc.Status 403",
  status: 403,
  headers: { "Content-Type": "application/json" },
  body: JSON.stringify({
    code: 7,
    message: "re-authentication required: present a credential again",
    details: [
      { "@type": "type.googleapis.com/google.rpc.ErrorInfo", reason: "SESSION_NOT_FRESH", domain: REFUSAL_DOMAIN },
    ],
  }),
};

/** Край: служба не ответила о предъявленном на глаголе с носителем (KA1, Р1) — носитель цел. */
export const EDGE_CREDENTIAL_STATE_UNKNOWN = edgeAnswer(EDGE_CREDENTIAL_STATE_UNKNOWN_SOURCE);

/** Край: удостоверение не принято (KA1, Р2) — один отказ на все причины. */
export const EDGE_AUTHN_FAILED = edgeAnswer(EDGE_AUTHN_FAILED_SOURCE);

/** Ответить подставленным ответом производителя — телом и заголовками. */
export function fulfillWith(route: Route, answer: ProducerAnswer): Promise<void> {
  return route.fulfill({ status: answer.status, headers: answer.headers, body: answer.body });
}

/** Тело подставленного ответа — разобранным, для утверждения «что показал экран». */
export function bodyOf(answer: ProducerAnswer): { code: number; message: string; details?: unknown[] } {
  return JSON.parse(answer.body) as { code: number; message: string; details?: unknown[] };
}
