// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { ApiError } from "../api/client";
import { UNREAD_BADGE_CAP, unreadBadge, unreadCount, unreadOfCycle } from "./unread-count";

/**
 * Счёт «нового» по правилу Р18 приёмки NTF-6 (замысел З7): одна функция над
 * СЦЕПКОЙ загруженных страниц ленты (CX6-17) и значок с доступным именем.
 *
 * Ветки — DoD 2 приёмки целиком: найдено; не найдено при последней странице;
 * не найдено при следующей; пустой `seenUpTo`; `N = 0`; `N > 99`; ответ ленты
 * не `200`; ответ настроек не `200`; ответ `200` не той формы. Плюс сцепка:
 * `seenUpTo` на первой странице — строки второй не новые (И5); `seenUpTo` не
 * найден на двух загруженных, токен есть — «не меньше».
 *
 * Позиции непрозрачны и сравниваются только на равенство (Р18): поэтому в
 * пробах они НЕ упорядочены ни лексически, ни числом — реализация, сравнившая
 * их на «больше», разошлась бы с пробой.
 */

const page = (...positions: string[]) => positions.map((position) => ({ position }));

describe("unreadCount — правило Р18", () => {
  test("seenUpTo найден: новое — строки выше него, счёт точен", () => {
    const items = page("p-q", "p-a", "p-z", "p-m");
    const r = unreadCount(items, "p-z", true);
    expect(r).toMatchObject({ n: 2, exact: true });
    expect(items.map((i) => r.isNew(i.position))).toEqual([true, true, false, false]);
  });

  test("seenUpTo не найден, страница последняя: новое всё, счёт точен", () => {
    const items = page("p-q", "p-a", "p-z");
    const r = unreadCount(items, "p-gone", false);
    expect(r).toMatchObject({ n: 3, exact: true });
    expect(items.every((i) => r.isNew(i.position))).toBe(true);
  });

  test("seenUpTo не найден, есть следующая страница: новое всё загруженное, «не меньше»", () => {
    const items = page("p-q", "p-a", "p-z");
    const r = unreadCount(items, "p-gone", true);
    expect(r).toMatchObject({ n: 3, exact: false });
    expect(items.every((i) => r.isNew(i.position))).toBe(true);
  });

  test("seenUpTo пуст: новое всё загруженное", () => {
    const items = page("p-q", "p-a");
    expect(unreadCount(items, "", false)).toMatchObject({ n: 2, exact: true });
    expect(unreadCount(items, "", true)).toMatchObject({ n: 2, exact: false });
    expect(unreadCount(items, "", false).isNew("p-a")).toBe(true);
  });

  test("seenUpTo — верхний элемент: N = 0, ничего не новое", () => {
    const items = page("p-q", "p-a");
    const r = unreadCount(items, "p-q", true);
    expect(r).toMatchObject({ n: 0, exact: true });
    expect(items.some((i) => r.isNew(i.position))).toBe(false);
  });

  test("позиция вне загруженного не новая", () => {
    const r = unreadCount(page("p-q", "p-a"), "p-a", false);
    expect(r.isNew("p-elsewhere")).toBe(false);
  });
});

describe("unreadCount — сцепка загруженных страниц (CX6-17, И5)", () => {
  const first = page("p-7", "p-3", "p-9");
  const second = page("p-1", "p-8", "p-2");

  test("seenUpTo на первой странице — ни одна строка второй не новая, счёт точен", () => {
    const r = unreadCount([...first, ...second], "p-3", true);
    expect(r).toMatchObject({ n: 1, exact: true });
    expect(second.some((i) => r.isNew(i.position))).toBe(false);
    expect(r.isNew("p-7")).toBe(true);
  });

  test("близнец: seenUpTo на второй странице — первая страница и строки второй выше него новые", () => {
    const r = unreadCount([...first, ...second], "p-8", true);
    expect(r).toMatchObject({ n: 4, exact: true });
    expect([...first, ...second].map((i) => r.isNew(i.position))).toEqual([true, true, true, true, false, false]);
  });

  test("seenUpTo не найден на двух загруженных, токен есть — «не меньше» всех шести", () => {
    const r = unreadCount([...first, ...second], "p-gone", true);
    expect(r).toMatchObject({ n: 6, exact: false });
  });
});

describe("unreadBadge — значок и доступное имя (Р18)", () => {
  test("точно и N = 0 — значка нет, имя «Уведомления»", () => {
    expect(unreadBadge({ n: 0, exact: true })).toEqual({ text: "", label: "Уведомления" });
  });

  test("точно и N ≤ 99 — «N»", () => {
    expect(unreadBadge({ n: 7, exact: true })).toEqual({ text: "7", label: "Уведомления, новых: 7" });
    expect(unreadBadge({ n: 99, exact: true })).toEqual({ text: "99", label: "Уведомления, новых: 99" });
  });

  test("N > 99 — «99+», имя несёт настоящее число", () => {
    expect(UNREAD_BADGE_CAP).toBe(99);
    expect(unreadBadge({ n: 100, exact: true })).toEqual({ text: "99+", label: "Уведомления, новых: 100" });
    expect(unreadBadge({ n: 150, exact: false })).toEqual({
      text: "99+",
      label: "Уведомления, новых: не меньше 150",
    });
  });

  test("неточно — «min(N,99)+» и «не меньше N»", () => {
    expect(unreadBadge({ n: 5, exact: false })).toEqual({
      text: "5+",
      label: "Уведомления, новых: не меньше 5",
    });
  });

  test("ответ не получен — «!» и «счётчик недоступен»", () => {
    expect(unreadBadge("unavailable")).toEqual({ text: "!", label: "Уведомления, счётчик недоступен" });
  });
});

describe("unreadOfCycle — исход цикла «лента + настройки» (Р18 «Ответ не получен»)", () => {
  const ok = <T>(value: T): PromiseSettledResult<T> => ({ status: "fulfilled", value });
  const refused = (reason: unknown): PromiseSettledResult<never> => ({ status: "rejected", reason });
  const inbox = { items: page("p-q", "p-a", "p-z"), hasMore: false };

  // Отказ формы ответа производит клиент `notify` (замысел З6: ответ `200` не той
  // формы — отказ `ResponseShapeError`, а не значение). Счёт видит его так же, как
  // любой отказ чтения: отвергнутое обещание. Здесь — отказ той же природы: `Error`
  // с именем класса клиента и без статуса.
  const shapeRefusal = Object.assign(new Error("unexpected response shape: /notify/v1/inbox items"), {
    name: "ResponseShapeError",
  });
  const unavailable503 = new ApiError(503, "UNAVAILABLE", null, "notify unavailable");

  test("близнец: оба ответа 200 верной формы — счёт по правилу", () => {
    const r = unreadOfCycle(ok(inbox), ok("p-a"));
    expect(r).not.toBe("unavailable");
    expect(r).toMatchObject({ n: 1, exact: true });
  });

  test("ответ ленты не 200 — «unavailable»", () => {
    expect(unreadOfCycle(refused(unavailable503), ok("p-a"))).toBe("unavailable");
  });

  test("ответ настроек не 200 — «unavailable»", () => {
    expect(unreadOfCycle(ok(inbox), refused(unavailable503))).toBe("unavailable");
  });

  test("ответ 200 не той формы — «unavailable», прежний счёт не выдаётся за «прочитано»", () => {
    expect(unreadOfCycle(refused(shapeRefusal), ok("p-a"))).toBe("unavailable");
    expect(unreadOfCycle(ok(inbox), refused(shapeRefusal))).toBe("unavailable");
    expect(unreadBadge(unreadOfCycle(ok(inbox), refused(shapeRefusal)))).toEqual({
      text: "!",
      label: "Уведомления, счётчик недоступен",
    });
  });
});
