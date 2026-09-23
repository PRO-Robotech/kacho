// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Адреса церемоний личности — ОДНО объявление на консоль (приёмка F8, Р1, Р3).
//
// Консоль объявляет маршруты на все шесть и не спрашивает, какая посадка под
// ней: второй ручки «а вести ли церемонию самим» не заводится — она разошлась
// бы с раздачей молча. Адрес, которого консоль на этой стадии не ведёт,
// отвечает собственной страницей «такого адреса здесь нет», а не переводом на
// панель: замыкающее правило маршрутизатора делает отказ похожим на успех.
//
// `/error` и `/consent` сюда НЕ входят: это адреса чужих потоков, и
// совместимость с их ссылками не нужна — они разделяют судьбу любого
// неизвестного адреса.

/** Все адреса церемоний, получающие маршрут. */
export const CEREMONY_ADDRESSES = [
  "/login",
  "/registration",
  "/logout",
  "/settings",
  "/recovery",
  "/verification",
] as const;

export type CeremonyAddress = (typeof CEREMONY_ADDRESSES)[number];

/**
 * Адреса, которых консоль пока не ведёт: восстановление доступа и подтверждение
 * адреса ждут доставки письма (под-фаза S3). Обещать путь на них нельзя —
 * страница входа их не называет (Р4).
 */
export const NOT_SERVED_CEREMONY_ADDRESSES: readonly CeremonyAddress[] = ["/recovery", "/verification"];

/** Куда уводит вход без адреса возврата и с отвергнутым: корень консоли. */
export const CONSOLE_ROOT = "/";

/** Адрес экрана входа с адресом возврата — ЕДИНСТВЕННАЯ его сборка. */
export function loginAddress(returnTo?: string): string {
  return returnTo ? `/login?returnTo=${encodeURIComponent(returnTo)}` : "/login";
}
