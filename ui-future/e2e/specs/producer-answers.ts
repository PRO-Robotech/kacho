// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import type { Route } from "@playwright/test";

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
 * настоящем ответе ломается: у ответа края F8-25 есть вызов
 * `WWW-Authenticate: Bearer error="invalid_token"` и НЕТ поля `details`, и экран,
 * читающий их иначе, чем рукописное тело, проба бы не поймала. Поэтому тела и
 * заголовки собраны здесь по коду производителя, с координатой, и пробы берут
 * их отсюда, а не набирают рукой.
 *
 * ПРОИЗВОДИТЕЛИ
 *
 *   служба доступа — `kaname` `internal/handler/loginlanehttp/handler.go`:
 *     `writeRefusal` (тело `{code, message, details}`, `details` — всегда
 *     массив, `ErrorInfo` с доменом отказа) и `writeJSON` (`Content-Type:
 *     application/json`, `Cache-Control: no-store`);
 *   край — `gateway/internal/middleware/auth.go`, `writeHTTPUnauthorized`:
 *     `Content-Type: application/json`, вызов
 *     `Bearer error="invalid_token", error_description="<текст>"` и тело
 *     `{"code":16,"message":"<текст>"}` — без `details`.
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

function edgeUnauthorized(producer: string, description: string): ProducerAnswer {
  return {
    producer,
    status: 401,
    headers: {
      "Content-Type": "application/json",
      "WWW-Authenticate": `Bearer error="invalid_token", error_description="${description}"`,
    },
    body: `{"code":16,"message":"${description}"}`,
  };
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

/** Сессия не свежа — `ErrSessionNotFresh`. */
export const SESSION_NOT_FRESH = laneRefusal(
  "kaname loginlanehttp.writeError → writeRefusal(403, PERMISSION_DENIED, TextSessionNotFresh, SESSION_NOT_FRESH)",
  403,
  7,
  "re-authentication required: present a credential again",
  "SESSION_NOT_FRESH",
);

/** Край: служба не ответила о сессии на глаголе с носителем (F4d-23). */
export const EDGE_SESSION_ENDED = edgeUnauthorized(
  "gateway auth_own_session.go → writeHTTPUnauthorized(sessionCutoffDenyDescription)",
  "session ended; sign in again",
);

/** Ответить подставленным ответом производителя — телом и заголовками. */
export function fulfillWith(route: Route, answer: ProducerAnswer): Promise<void> {
  return route.fulfill({ status: answer.status, headers: answer.headers, body: answer.body });
}

/** Тело подставленного ответа — разобранным, для утверждения «что показал экран». */
export function bodyOf(answer: ProducerAnswer): { code: number; message: string; details?: unknown[] } {
  return JSON.parse(answer.body) as { code: number; message: string; details?: unknown[] };
}
