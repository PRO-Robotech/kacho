// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import type { LaneRefusal, SessionAnswer } from "@shared/api/login-lane";

// Положение подтверждения адреса почты — по ответу края о сессии, и только по
// нему (приёмка F6b, Р7). Одно решение на двух читателей: страж над каркасом и
// экран подтверждения, — иначе два места об одном ответе разошлись бы молча, и
// экран подтверждения отпускал бы туда, куда страж не пускает.
//
// Решения шесть, и два из них названы отдельно от «подтверждён», потому что
// умолчание здесь ЗАКРЫВАЕТ: край, не назвавший подтверждённость, каркаса не
// открывает, но и «вы вышли» не говорит — «спросить не удалось» не есть «сессии
// нет» (так уже решено в дереве, `host/src/utils/session.ts`).

export type AddressState =
  /** край ещё не ответил */
  | { kind: "asking" }
  /** сессии нет — поведение анонимного вызова этой под-фазой не меняется */
  | { kind: "absent" }
  /** сессия есть, адрес подтверждён */
  | { kind: "confirmed" }
  /** сессия есть, адрес не подтверждён; адрес — из ответа края, а не из ввода */
  | { kind: "unconfirmed"; email: string }
  /** сессия есть, но подтверждённость край не назвал */
  | { kind: "not-named" }
  /** край не ответил по существу */
  | { kind: "unknown"; refusal: LaneRefusal };

export function addressStateOf(answer: SessionAnswer): AddressState {
  if (answer.kind === "absent") return { kind: "absent" };
  if (answer.kind === "unknown") return { kind: "unknown", refusal: answer.refusal };
  const verified = answer.session?.emailVerified;
  if (verified === true) return { kind: "confirmed" };
  if (verified === false) return { kind: "unconfirmed", email: answer.user.email };
  return { kind: "not-named" };
}
