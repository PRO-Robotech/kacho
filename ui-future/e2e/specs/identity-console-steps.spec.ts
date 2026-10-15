// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { expect, type Page } from "@playwright/test";
import { newSeed, seedAddress, seedHuman, transferSession } from "./ceremony-seed";
import { runTag, test } from "./fixtures";

/**
 * Шаги, которые консоль называет человеку на экранах личности, — против того,
 * что исполняет служба (финальная имитация клиента, эпик #3172; находки #3210).
 *
 * Каждая проба утверждает НАБЛЮДАЕМОЕ на адресе находки: текст, который видит
 * человек, адрес страницы и обращение к краю, — а не разметку. Ожидания — по
 * условию (`toBeVisible`, `expect.poll`), повторов нет.
 *
 * Неподтверждённый посев разбору арендатора не записывается: снятие идёт
 * глаголами платформы, а их край неподтверждённой сессии не исполняет — так же
 * поступает `address-confirmation.spec.ts`; пространство стенда проб снимается
 * целиком.
 */

const RECOVERY_TITLE = "Восстановление доступа";

function pathOf(page: Page): string {
  return new URL(page.url()).pathname;
}

test("неподтверждённая сессия на /recovery получает экран восстановления с названным шагом «Выйти»", async ({
  page,
}, testInfo) => {
  // verifies #3210 — восстановление доступно в положении подтверждения; живая сессия получает названный шаг.
  test.setTimeout(180_000);
  const email = seedAddress(`w8b-unconfirmed-${runTag()}`);
  const seed = await newSeed(testInfo);
  try {
    await seedHuman(seed, email);
    await transferSession(seed, page.context());
  } finally {
    await seed.dispose();
  }

  // Положительный близнец: каркас неподтверждённой сессии по-прежнему закрыт.
  await page.goto("/settings", { waitUntil: "domcontentloaded" });
  await expect.poll(() => pathOf(page), { message: "/settings неподтверждённой сессии открылся", timeout: 30_000 }).toBe(
    "/verification",
  );

  await page.goto("/recovery", { waitUntil: "domcontentloaded" });
  await expect(
    page.getByRole("heading", { name: RECOVERY_TITLE }),
    "экран восстановления неподтверждённой сессии не отрисован",
  ).toBeVisible({ timeout: 30_000 });
  await expect(
    page.getByRole("button", { name: /Выйти и восстановить доступ$/ }),
    "шаг «Выйти» к форме восстановления не назван",
  ).toBeVisible();
  expect(pathOf(page), "страница уведена с /recovery").toBe("/recovery");
});
