// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { expect, type Page } from "@playwright/test";
import { register, runTag, test } from "./fixtures";

/**
 * Неизвестный адрес консоли (#3199): замыкающее правило маршрута показывает
 * «Страница не найдена» и не подменяет адрес в строке браузера; корень по-прежнему
 * ведёт на панель. Проба идёт с сессией заведённого человека, как на адресе
 * находки: без сессии первое чтение каркаса получает `401`, и уход на вход
 * (`redirectToLogin`) отменяет переход пробы (`net::ERR_ABORTED`, t3184-w8b-2) —
 * утверждение о маршрутизации стало бы функцией гонки.
 */

function pathOf(page: Page): string {
  return new URL(page.url()).pathname;
}

test("неизвестный адрес консоли показывает «Страница не найдена», а не дашборд", async ({ page }) => {
  // verifies #3199 — замыкающее правило маршрута не выдаёт опечатку за успех.
  test.setTimeout(240_000);
  await register(page);
  const unknown = `/no-such-page-${runTag()}`;
  await page.goto(unknown, { waitUntil: "domcontentloaded" });
  await expect(
    page.getByRole("heading", { name: "Страница не найдена" }),
    "неизвестный адрес не назван страницей «не найдено»",
  ).toBeVisible({ timeout: 60_000 });
  expect(pathOf(page), "адрес в строке браузера подменён").toBe(unknown);

  // Положительный близнец: корень по-прежнему ведёт на дашборд.
  await page.goto("/", { waitUntil: "domcontentloaded" });
  await expect.poll(() => pathOf(page), { message: "корень консоли не ведёт на дашборд", timeout: 60_000 }).toBe(
    "/dashboard",
  );
});
