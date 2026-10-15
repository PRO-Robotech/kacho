// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { expect, type Page } from "@playwright/test";
import { LANE, newSeed, seedAddress, seedHuman, transferSession } from "./ceremony-seed";
import { runTag, test } from "./fixtures";

/**
 * Шаги, которые консоль называет человеку на экранах личности, — против того,
 * что исполняет служба (финальная имитация клиента, эпик #3172; находки #3184, #3210).
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

/** Текст ступени 2 — один на любой ответ запроса кода; выписан здесь, а не взят у консоли. */
const RECOVERY_CODE_SENT =
  "Если по этому адресу можно восстановить доступ, на него придёт письмо с кодом восстановления. Код действует " +
  "ограниченное время и применяется один раз. Введите код из последнего письма и новый пароль.";

function pathOf(page: Page): string {
  return new URL(page.url()).pathname;
}

async function requestRecoveryCode(page: Page, email: string) {
  await page.goto("/recovery", { waitUntil: "domcontentloaded" });
  const form = page.getByRole("form", { name: RECOVERY_TITLE });
  await expect(form.getByRole("textbox", { name: "Адрес почты" }), "форма восстановления не отрисована").toBeVisible({
    timeout: 30_000,
  });
  await form.getByRole("textbox", { name: "Адрес почты" }).fill(email);
  const answered = page.waitForResponse(
    (r) => new URL(r.url()).pathname === LANE.recovery && r.request().method() === "POST",
    { timeout: 30_000 },
  );
  await form.getByRole("button", { name: /Отправить код$/ }).click();
  expect((await answered).status(), "запрос кода восстановления не принят службой").toBe(200);
}

test("экран восстановления называет путь, который исполняет служба, и не обещает «подтверждён»", async ({ page }) => {
  // verifies #3184 — код восстановления выдаётся и на неподтверждённый адрес; текст один на все исходы.
  test.setTimeout(120_000);
  await requestRecoveryCode(page, seedAddress(`w8b-recovery-${runTag()}`));
  await expect(
    page.getByText(RECOVERY_CODE_SENT, { exact: true }),
    "после запроса кода экран не показал единый текст ступени 2",
  ).toBeVisible({ timeout: 30_000 });
  await expect(
    page.getByText(/подтверждён/),
    "экран обещает письмо только подтверждённому адресу — служба выдаёт код и неподтверждённому",
  ).toHaveCount(0);
});

test("отказ регистрации занятого адреса называет восстановление по адресу, а не повтор формы", async ({
  page,
}, testInfo) => {
  // verifies #3184 — адрес, занятый неподтверждённой записью, возвращается владельцу ящика восстановлением.
  test.setTimeout(180_000);
  const email = seedAddress(`w8b-taken-${runTag()}`);
  const seed = await newSeed(testInfo);
  try {
    await seedHuman(seed, email);
  } finally {
    await seed.dispose();
  }

  await page.goto("/registration", { waitUntil: "domcontentloaded" });
  await page.getByRole("textbox", { name: "Адрес электронной почты" }).fill(email);
  await page.getByLabel("Пароль", { exact: true }).fill(`Kacho-${runTag()}-Other!`);
  await page.getByRole("button", { name: "Завести учётную запись", exact: true }).click();

  const step = page.getByTestId("refusal-next-step");
  await expect(step, "отказ регистрации занятого адреса показан без следующего шага").toBeVisible({ timeout: 30_000 });
  await expect(step, "шаг не называет восстановление доступа по этому адресу").toContainText("восстановите доступ");
  // Владельцу ящика, на чей адрес учётную запись завёл другой, повтор формы не
  // поможет никогда — его путь (восстановление) обязан быть назван.
  await expect(step, "шаг не называет путь владельца ящика, чей адрес занят чужой записью").toContainText(
    "завёл не вы",
  );
});

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
