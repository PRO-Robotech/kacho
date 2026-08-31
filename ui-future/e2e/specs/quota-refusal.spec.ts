// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { expect, type Page } from "@playwright/test";
import { registerAndSignIn, runTag, test } from "./fixtures";

/**
 * Отказ по пределу оставляет клиенту СЛЕДУЮЩИЙ ШАГ.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ПРЕДМЕТ
 *
 * Производитель отказа один на всю платформу и говорит по-английски машинными
 * именами: `project prj-1 has reached its limit of 5 vpc.network`. Строка точна
 * и контрактна — и для упёршегося бесполезна. Консоль показывала её дословно под
 * заголовком «Внимание»: вид назван машинным именем, кто задал величину — не
 * сказано, куда идти — не сказано.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ПОЧЕМУ БРАУЗЕРОМ, А НЕ МОДУЛЬНОЙ ПРОБОЙ
 *
 * Модульная проба монтирует компонент и об отказе, дошедшем до экрана, не
 * говорит ничего: между краем и текстом стоят разбор тела ответа, маршрутизация,
 * загрузка федеративного модуля и та поверхность, на которой отказ мутации
 * вообще показывается (всплывающее сообщение, а не экран отказа). Здесь
 * утверждается то, что видит человек, доведший форму до отправки.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ЧЕГО ЭТА ПРОБА НЕ ДОКАЗЫВАЕТ — СКАЗАНО ВСЛУХ, ЧТОБЫ НЕ ПРОЧЛИ ШИРЕ
 *
 * Ответ края на создание ПОДМЕНЯЕТСЯ: исчерпать настоящий предел стоило бы
 * создания стольких ресурсов, сколько назначено арендатору, и проба измеряла бы
 * пропускную способность стенда, а не текст отказа. Поэтому проба НЕ утверждает,
 * что край такой отказ производит, — это предмет проб владельцев на их стороне.
 *
 * Что она утверждает целиком на живом стенде и без подмены: адрес, на который
 * отказ отправляет клиента, СУЩЕСТВУЕТ и отвечает. Вторая половина без первой
 * оставила бы отказ, уводящий в никуда, — а именно это и есть «нет следующего
 * шага», только этажом ниже.
 */

/** Тело отказа в том виде, в каком его собирает край из `google.rpc.Status`. */
function quotaRefusalBody(code: number, reason: string, metadata: Record<string, string>, message: string) {
  return JSON.stringify({
    code,
    message,
    details: [
      {
        "@type": "type.googleapis.com/google.rpc.ErrorInfo",
        reason,
        domain: "vpc.kacho.cloud",
        metadata,
      },
    ],
  });
}

/**
 * Подменяет ответ на СОЗДАНИЕ сети, оставляя чтения нетронутыми.
 *
 * Различение по методу существенно: перехватив и чтения, проба лишила бы
 * страницу списка данных и упала бы раньше, чем дошла до своего предмета.
 */
async function refuseCreateWithQuota(
  page: Page,
  status: number,
  code: number,
  reason: string,
  metadata: Record<string, string>,
  message: string,
) {
  await page.route("**/vpc/v1/networks**", async (route) => {
    if (route.request().method() !== "POST") {
      await route.fallback();
      return;
    }
    await route.fulfill({
      status,
      contentType: "application/json",
      body: quotaRefusalBody(code, reason, metadata, message),
    });
  });
}

/** Доводит форму создания сети до отправки — тем же путём, каким идёт арендатор. */
async function submitNetworkForm(page: Page, projectId: string, name: string) {
  await page.goto(`/projects/${projectId}/vpc/networks`, { waitUntil: "domcontentloaded" });

  const createControl = page.locator('button:has-text("Создать"), a:has-text("Создать")').first();
  await expect(createControl, "на странице сетей нет элемента создания").toBeVisible({ timeout: 30_000 });
  await createControl.click();

  const field = page.locator('input[type="text"]:visible, input:not([type]):visible').first();
  await expect(field, "форма создания не предложила ни одного поля").toBeVisible({ timeout: 20_000 });
  await field.fill(name);

  // Адресный блок обязателен по форме: без него отказ придёт от ВАЛИДАЦИИ, и
  // проба измеряла бы её, а не отказ по пределу.
  const cidr = page.locator('input[placeholder="10.20.0.0/16"]:visible').first();
  await expect(cidr, "на форме создания сети нет поля адресного блока").toBeVisible({ timeout: 20_000 });
  await cidr.fill("10.93.0.0/16");

  await Promise.all([
    page.waitForResponse((r) => r.url().includes("/vpc/v1/networks") && r.request().method() === "POST", {
      timeout: 40_000,
    }),
    page.locator('button:has-text("Создать"):visible, button[type="submit"]:visible').last().click(),
  ]);
}

/** Сообщение об отказе — то, что видит человек. Отказ рисуется ролью `alert`. */
function refusalMessage(page: Page) {
  return page.getByRole("alert").filter({ hasText: /предел/i }).first();
}

test("отказ по пределу назван по-русски и уводит в раздел, который существует", async ({ page }) => {
  // verifies #1605
  const { projectId } = await registerAndSignIn(page);

  await refuseCreateWithQuota(
    page,
    429,
    8,
    "QUOTA_EXCEEDED",
    { kind: "vpc.network", limit: "5", used: "5", carrier_type: "project", carrier_id: projectId },
    `project ${projectId} has reached its limit of 5 vpc.network`,
  );

  await submitNetworkForm(page, projectId, `net-quota-${runTag()}`);

  const message = refusalMessage(page);
  await expect(message, "отказ по пределу не показан человеку вовсе").toBeVisible({ timeout: 20_000 });

  const shown = (await message.textContent()) ?? "";
  // Вид назван человеческим именем — тем же, которым его называет витрина.
  expect(shown, "вид ресурса не назван человеческим именем").toContain("Облачные сети");
  // Кто задаёт величины: без этого упёршийся не знает, к кому идти.
  expect(shown, "не сказано, кто задаёт величины").toContain("Величины задаёт администратор облака");
  // Куда идти.
  expect(shown, "не назван раздел, где видны действующие пределы").toContain("Квоты");
  // Строка производителя на экран не выносится — она адресована тому, кто чинит.
  expect(shown, "английская строка производителя показана арендатору дословно").not.toContain(
    "has reached its limit",
  );

  // ЖИВОЙ СТЕНД, БЕЗ ПОДМЕНЫ: раздел, названный в отказе, существует и отвечает.
  await page.unroute("**/vpc/v1/networks**");
  const [quotas] = await Promise.all([
    page.waitForResponse((r) => new URL(r.url()).pathname === "/vpc/v1/quotas", { timeout: 30_000 }),
    page.goto(`/projects/${projectId}/quotas`, { waitUntil: "domcontentloaded" }),
  ]);
  expect(
    quotas.status(),
    "отказ отправляет клиента в раздел, которого край не обслуживает — следующего шага по-прежнему нет",
  ).toBe(200);
});

test("«предел не задан» приходит статусом 400 и всё равно узнаётся как предел", async ({ page }) => {
  // verifies #1605
  //
  // Полоса несущая: «потолок не назван ни на одной области» приходит
  // `FAILED_PRECONDITION` (400), а не 429. Консоль, ключующаяся на статусе,
  // потеряла бы её целиком — и потеряла бы ровно ту, где действие
  // администратора другое: не поднять предел, а завести его.
  const { projectId } = await registerAndSignIn(page);

  await refuseCreateWithQuota(
    page,
    400,
    9,
    "QUOTA_NOT_PROVISIONED",
    { kind: "vpc.network", carrier_type: "project", carrier_id: projectId },
    "no limit provisioned for vpc.network",
  );

  await submitNetworkForm(page, projectId, `net-noquota-${runTag()}`);

  const message = refusalMessage(page);
  await expect(message, "отказ «предел не задан» не показан человеку вовсе").toBeVisible({ timeout: 20_000 });

  const shown = (await message.textContent()) ?? "";
  expect(shown, "вид ресурса не назван человеческим именем").toContain("Облачные сети");
  // Действие администратора у этой полосы — ЗАВЕСТИ предел.
  expect(shown, "не сказано, что предел надо завести").toContain("завести");
  // Положительный контроль к предыдущему: полосы не свелись в одну. Сведи их —
  // и читающий пойдёт искать, что понизить, там, где не назначено ничего.
  expect(shown, "полоса «предел не задан» показана словами полосы «место кончилось»").not.toContain("поднять");
});
