// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { test, expect } from "@playwright/test";
import { registerAndSignIn, runTag } from "./fixtures";

/**
 * Список отвечает про СПИСОК, а не про загруженную страницу.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ПОЧЕМУ ЭТО ПРОВЕРЯЕТСЯ БРАУЗЕРОМ, А НЕ МОДУЛЬНОЙ ПРОБОЙ
 *
 * Модульная проба подменяет `fetch` и утверждает про адрес, который собрала
 * страница. Она не может утверждать, что край этот адрес ПРИНЯЛ: выражение
 * фильтра разбирает владелец по своему белому списку, и незнакомое поле — не
 * «фильтр без эффекта», а отказ на всю страницу. Между «мы отправили» и «нам
 * ответили» лежит ровно тот дефект, ради которого поиск уехал на сервер.
 *
 * Поэтому здесь утверждается ПАРА: запрос несёт выражение — и край отвечает
 * успехом, а не отказом разбора.
 */

test("поиск в списке уходит запросом, и край его принимает", async ({ page }) => {
  // verifies #373
  const { projectId } = await registerAndSignIn(page);

  await page.goto(`/projects/${projectId}/vpc/networks`, { waitUntil: "domcontentloaded" });

  const поиск = page.locator('input[type="search"]').first();
  await expect(поиск, "на странице списка нет строки поиска").toBeVisible({ timeout: 30_000 });

  // Строка поиска обязана НАЗЫВАТЬ, о чём судит: серверная — о всём списке.
  await expect(поиск, "строка поиска не объявляет, что спрашивает сервер").toHaveAttribute(
    "placeholder",
    /по всему списку/,
  );

  const [ответ] = await Promise.all([
    page.waitForResponse(
      (r) => {
        const u = new URL(r.url());
        return (
          u.pathname === "/vpc/v1/networks" &&
          r.request().method() === "GET" &&
          (u.searchParams.get("filter") ?? "").includes("CONTAINS")
        );
      },
      { timeout: 30_000 },
    ),
    поиск.fill(`ищем-${runTag()}`),
  ]);

  // 200 против 400 здесь и есть весь предмет: 400 означает, что выражение
  // собрано не по грамматике владельца, и тогда поиск ронял бы страницу целиком.
  expect(ответ.status(), `край отверг выражение фильтра: ${await ответ.text()}`).toBe(200);
});

/**
 * Сортировка не предлагается там, где она солгала бы.
 *
 * Стрелка сортирует загруженный кусок; пока за курсором есть страницы, порядок
 * молча меняется при догрузке. Проверяется наблюдаемая связь: есть кнопка
 * догрузки — нет ни одного сортируемого заголовка.
 */
test("пока список прочитан не весь, сортировка не предлагается", async ({ page }) => {
  // verifies #373
  const { projectId } = await registerAndSignIn(page);
  await page.goto(`/projects/${projectId}/vpc/networks`, { waitUntil: "domcontentloaded" });
  await expect(page.locator("table").first()).toBeVisible({ timeout: 30_000 });

  const ещё = page.locator('button:has-text("Показать ещё")');
  const сортируемые = page.locator("th.ant-table-column-has-sorters");

  if ((await ещё.count()) > 0) {
    expect(await сортируемые.count(), "список прочитан не весь, а сортировка предлагается").toBe(0);
  } else {
    // Положительный контроль: на прочитанном целиком списке сортировка ЕСТЬ —
    // иначе «нуль сортируемых» означало бы и «убрали правильно», и «сломали
    // везде», а различить было бы нечем.
    expect(await сортируемые.count(), "список прочитан целиком, а сортировать нечем").toBeGreaterThan(0);
  }
});

/**
 * Поиск по всем ресурсам сразу ничего не спрашивает, пока не набран запрос,
 * и спрашивает сервер там, где владелец это умеет.
 *
 * Прежде страница на открытии тянула девять списков по 500 строк — безусловно,
 * ещё до первого нажатия клавиши.
 */
test("общий поиск не работает вхолостую и уходит на сервер", async ({ page }) => {
  // verifies #373
  await registerAndSignIn(page);

  const списки: string[] = [];
  page.on("request", (r) => {
    const u = new URL(r.url());
    if (/\/v1\//.test(u.pathname)) списки.push(u.pathname + u.search);
  });

  await page.goto("/system/search", { waitUntil: "domcontentloaded" });
  const поиск = page.locator('input[placeholder*="Поиск"]').first();
  await expect(поиск).toBeVisible({ timeout: 30_000 });

  // Дать странице время сделать то, чего она делать не должна.
  await expect
    .poll(async () => списки.filter((u) => u.includes("/vpc/v1/networks")).length, { timeout: 3_000 })
    .toBe(0)
    .catch(() => {
      throw new Error(`страница спросила списки до запроса: ${списки.join(", ")}`);
    });

  const [ответ] = await Promise.all([
    page.waitForResponse(
      (r) => {
        const u = new URL(r.url());
        return u.pathname === "/vpc/v1/networks" && (u.searchParams.get("filter") ?? "").includes("CONTAINS");
      },
      { timeout: 30_000 },
    ),
    поиск.fill("прод"),
  ]);
  expect(ответ.status(), `край отверг выражение поиска: ${await ответ.text()}`).toBe(200);
});
