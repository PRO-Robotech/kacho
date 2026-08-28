// Исход мутации машины — ОДНО место чтения на три глагола домена.
//
// Все восемь мутаций машины (`compute/src/api/resources.ts`) типизированы как
// `Promise<{ operation }>`, и шапка реестра объявляет «мутации async →
// Operation». Значит ответ без операции — не синхронный успех, а нарушение
// контракта: доказательств, что мутация исполнилась, нет.
//
// Разбор берётся общий (`@shared/lib/operation-outcome`), а не свой: у исхода
// мутации в дереве один словарь, и второй разошёлся бы с ним молча. Здесь
// только РАЗВОДКА трёх исходов по обработчикам домена — вида «нарушение» у
// прежнего кода не было вовсе, поэтому он и уходил в тихий успех.

import { resolveMutationResponse } from "@shared/lib/operation-outcome";

export interface MutationHandlers {
  /** Ответ несёт операцию — её опрашивают до завершения. */
  onOperation: (opId: string) => void;
  /** Ответ синхронный и законный (глагол операции не возвращает). */
  onSync: () => void;
  /** Контракт нарушен: операции нет там, где она обязана быть. */
  onViolation: (message: string) => void;
}

/**
 * Развести ответ края по трём исходам.
 *
 * `expectOperation` не выведен и не угадан: он приходит от вызывающего, потому
 * что это свойство ГЛАГОЛА, а не ответа. Глагол, который операцию возвращать не
 * обязан, передаёт `false`, и «нет операции» остаётся законным синхронным
 * исходом — иначе проверка отвергала бы верное поведение.
 */
export function applyMutationOutcome(resp: unknown, expectOperation: boolean, h: MutationHandlers): void {
  const res = resolveMutationResponse(resp, expectOperation);
  if (res.kind === "operation") h.onOperation(res.opId);
  else if (res.kind === "violation") h.onViolation(res.message);
  else h.onSync();
}
