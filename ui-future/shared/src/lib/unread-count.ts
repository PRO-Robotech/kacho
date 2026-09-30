// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Счёт «нового» в ленте уведомлений — правило Р18 приёмки NTF-6, одна реализация
// на два бандла (Р20): хост считает ею значок кнопки рейла, модуль `notify` —
// пометки строк центра (замысел З7).
//
// ВХОД — СЦЕПКА ВСЕХ ЗАГРУЖЕННЫХ СТРАНИЦ в порядке ответа (CX6-17): хост передаёт
// первую страницу, центр — все, что загрузил. Иначе центр считал бы каждую
// страницу отдельно, и после `seenUpTo`, найденного на первой, вторая страница
// снова оказалась бы «новой» целиком (инвариант И5).
//
// ПОЗИЦИИ НЕПРОЗРАЧНЫ: сравниваются только на равенство. Порядок новизны — это
// порядок выдачи ленты, а не порядок строк позиций.

/** Потолок числа на значке: больше — «99+». */
export const UNREAD_BADGE_CAP = 99;

export interface UnreadCount {
  /** Число новых среди загруженного. */
  n: number;
  /** `false` — «не меньше n»: позиция отметки не найдена, а ниже есть ещё страницы. */
  exact: boolean;
  /** Строка с этой позицией — новая. Позиция вне загруженного — не новая. */
  isNew: (position: string) => boolean;
}

/**
 * Правило Р18: элемент с `position == seenUpTo` найден на индексе `i` — новое
 * `items[0..i)`, точно; не найден и страниц больше нет — новое всё, точно; не
 * найден и страницы есть — новое всё загруженное, «не меньше»; `seenUpTo` пуст —
 * новое всё загруженное.
 */
export function unreadCount(items: readonly { position: string }[], seenUpTo: string, hasMore: boolean): UnreadCount {
  const at = seenUpTo === "" ? -1 : items.findIndex((item) => item.position === seenUpTo);
  const fresh = at >= 0 ? items.slice(0, at) : items;
  const positions = new Set(fresh.map((item) => item.position));
  return {
    n: fresh.length,
    exact: at >= 0 || !hasMore,
    isNew: (position) => positions.has(position),
  };
}

/** Что показывает кнопка: счёт либо «ответ не получен». */
export type UnreadBadgeInput = Pick<UnreadCount, "n" | "exact"> | "unavailable";

/** Текст значка (`""` — значка нет) и доступное имя кнопки (Р18). */
export function unreadBadge(input: UnreadBadgeInput): { text: string; label: string } {
  if (input === "unavailable") return { text: "!", label: "Уведомления, счётчик недоступен" };
  const { n, exact } = input;
  if (exact && n === 0) return { text: "", label: "Уведомления" };
  const text = exact && n <= UNREAD_BADGE_CAP ? String(n) : `${Math.min(n, UNREAD_BADGE_CAP)}+`;
  const label = exact ? `Уведомления, новых: ${n}` : `Уведомления, новых: не меньше ${n}`;
  return { text, label };
}

/**
 * Исход одного цикла «лента + настройки» (Р18, «Ответ не получен»). Отвергнуто
 * хотя бы одно из двух чтений — ответ не `200` либо `200` не той формы, который
 * клиент `notify` отдаёт отказом, — и прежний счёт не выдаётся ни за текущий, ни
 * за «прочитано»: `"unavailable"`. Оба прочитаны — счёт по правилу выше.
 */
export function unreadOfCycle(
  inbox: PromiseSettledResult<{ items: readonly { position: string }[]; hasMore: boolean }>,
  seenUpTo: PromiseSettledResult<string>,
): UnreadCount | "unavailable" {
  if (inbox.status === "rejected" || seenUpTo.status === "rejected") return "unavailable";
  return unreadCount(inbox.value.items, seenUpTo.value, inbox.value.hasMore);
}
