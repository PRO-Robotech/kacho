// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Что консоль ДЕЛАЕТ на отказ — одно решение на все поверхности (условие C2).
//
// ─────────────────────────────────────────────────────────────────────────────
// ПОЧЕМУ ПО МАШИННЫМ ПРИЗНАКАМ, А НЕ ПО СТАТУСУ
//
// Статус один на несколько смыслов, и смыслы требуют РАЗНЫХ действий:
//
//   403 / code 7  — отвергнут признак формы (`FORM_TOKEN_REJECTED`) · сессия не
//                   свежа (`SESSION_NOT_FRESH`);
//   401 / code 16 — служба: «не сошлось» (без заголовка) · край: сессия
//                   кончилась (`error="invalid_token"`) · край: пол уровня
//                   (`error="insufficient_user_authentication"`, RFC 9470).
//
// Действие выбирают ТОЛЬКО два признака: причина `details[].reason`
// (`ErrorInfo`) и значение `error=` заголовка `WWW-Authenticate`. Статус
// участвует в одном правиле — «на поверхности платформы `401` без пола значит
// „войдите“», — и там он не выбирает между смыслами, а отличает отказ от
// неотказа.
//
// ПЕРЕЧЕНЬ ДЕЙСТВИЙ ЗАКРЫТ. Неизвестная причина — «показать дословно»: без
// перехода на вход, без повышения и без гашения состояния. Умолчания в пользу
// действия нет: новая причина службы, прочитанная как знакомая, увела бы
// человека не туда молча.

import { LANE_REASON } from "./lane-reasons";

/** Где случился отказ. */
export type RefusalSurface =
  /** глагол полосы формы: экран церемонии ведёт её сам и на вход не уводит */
  | "ceremony"
  /** запрос платформы: данные страницы, у которой сессия — условие */
  | "platform";

/** Действие на отказ — перечень закрыт. */
export type RefusalAction =
  /** признак формы отвергнут: добыть ОДИН свежий, повторяет человек */
  | "fresh-form-token"
  /** служба требует свежего предъявления: повышение, пароль допустим */
  | "step-up-freshness"
  /** край требует уровня: повышение вторым фактором, пароля нет */
  | "step-up-floor"
  /** платформа: сессии нет — экран входа */
  | "sign-in"
  /** показать текст отказа дословно */
  | "show";

/** Машинные признаки отказа. */
export interface RefusalSigns {
  status: number;
  /** `ErrorInfo.reason`; `null` — причины не назвали. */
  reason: string | null;
  /** Значение `error=` заголовка `WWW-Authenticate`; `null` — вызова нет. */
  challenge: string | null;
}

export function refusalActionOf(signs: RefusalSigns, surface: RefusalSurface): RefusalAction {
  if (signs.reason === LANE_REASON.formTokenRejected) return surface === "ceremony" ? "fresh-form-token" : "show";
  if (signs.reason === LANE_REASON.sessionNotFresh) return "step-up-freshness";
  if (signs.challenge === "insufficient_user_authentication") return "step-up-floor";
  // `401` платформы без пола — сессии нет. Ответа на носитель, прежний после
  // перевыпуска этой вкладкой, сюда не приходит: такое обращение к выпуску
  // глагола уже имеет исход (`carrier-order.ts`, приёмка F8, Р10), и повтора
  // «с текущим носителем» нет — после такого ответа носителя у браузера нет.
  if (surface === "platform" && signs.status === 401) return "sign-in";
  return "show";
}
