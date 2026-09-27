// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Разбор тела отказа `google.rpc.Status` — ОДИН на консоль и на её сквозной
// набор (приёмка F8, условие C3).
//
// Отказ полосы формы приходит двумя производителями, и они пишут одно и то же
// значение двумя способами: служба доступа кладёт `details` всегда — пустым
// массивом либо с `ErrorInfo` (`loginlanehttp.writeRefusal`), край отказывает на
// глаголе с носителем БЕЗ поля `details` вовсе (`writeHTTPUnauthorized`). Для
// читателя это одно значение: причины нет. Два разборщика разошлись бы именно
// здесь — один признал бы тело края отказом, другой нет, — и разошлись бы
// молча. Поэтому разборщик один, и его зовут экраны консоли
// (`@shared/api/login-lane`) и распознаватель отказа сквозного набора
// (`ui-future/e2e/specs/fixtures.ts`, F8-44).
//
// Модуль без зависимостей: набор импортирует его из дерева консоли напрямую.

/** Тело отказа после разбора. `details` — всегда массив: «поля нет» ≡ «пусто». */
export interface RpcStatus {
  code: number;
  message: string;
  details: unknown[];
}

/**
 * Тело `google.rpc.Status` из текста ответа, либо `null` — текст отказом не
 * является. Отказ — это ОБЕ величины: числовой `code` и строковый `message`;
 * `details`, если пришёл, обязан быть массивом.
 */
export function parseRpcStatus(text: string): RpcStatus | null {
  let parsed: unknown;
  try {
    parsed = text ? JSON.parse(text) : null;
  } catch {
    return null;
  }
  if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) return null;
  const { code, message, details } = parsed as { code?: unknown; message?: unknown; details?: unknown };
  if (typeof code !== "number" || !Number.isInteger(code) || typeof message !== "string") return null;
  if (details !== undefined && !Array.isArray(details)) return null;
  return { code, message, details: details ?? [] };
}

/** Машинная причина отказа — `ErrorInfo.reason`; `null` — причины не назвали. */
export function reasonOfDetails(details: readonly unknown[]): string | null {
  for (const d of details) {
    if (d && typeof d === "object" && typeof (d as { reason?: unknown }).reason === "string") {
      return (d as { reason: string }).reason;
    }
  }
  return null;
}
