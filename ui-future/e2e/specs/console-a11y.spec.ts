// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { expect, test } from "@playwright/test";
import { createdResourceId, runTag, tenantWithProject } from "./fixtures";

/**
 * Доступность действий консоли с КЛАВИАТУРЫ.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ПОЧЕМУ БРАУЗЕРОМ, А НЕ МОДУЛЬНОЙ ПРОБОЙ
 *
 * Модульная проба монтирует компонент и спрашивает у него роль и обработчики.
 * Этого мало: достижимость с клавиатуры — свойство ЖИВОЙ страницы, где есть
 * порядок обхода, перекрывающие слои, прокрутка и настоящее событие клавиши.
 * Кнопка с правильной ролью, до которой не доходит Tab (её накрыл слой, или у
 * неё отрицательный порядок обхода), для модульной пробы неотличима от рабочей.
 *
 * Здесь проверяется то, что делает человек: дошёл клавишей — нажал — получил
 * результат. Ровно так же, как это делал бы тот, кто мышью не пользуется.
 */

const КЛЮЧ = "env";
const ЗНАЧЕНИЕ = "prod";

test("метка списка достижима клавишей Tab и срабатывает по Enter", async ({ page }) => {
  // verifies #925
  const { projectId } = await tenantWithProject(page);

  const имя = `a11y-net-${runTag()}`;
  const ответ = await page.request.post("/vpc/v1/networks", {
    data: { projectId, name: имя, labels: { [КЛЮЧ]: ЗНАЧЕНИЕ } },
  });
  await createdResourceId(page, ответ, "networkId", (id) => `/vpc/v1/networks/${id}`, "сеть с меткой");

  // Разрешение на буфер: результат нажатия читается ИЗ БУФЕРА, а не по подписи
  // об успехе. Подпись сообщает о намерении, буфер — о случившемся.
  await page.context().grantPermissions(["clipboard-read", "clipboard-write"]);
  await page.goto(`/projects/${projectId}/vpc/networks`, { waitUntil: "domcontentloaded" });

  const метка = page.locator(`[title="Скопировать ${КЛЮЧ}=${ЗНАЧЕНИЕ}"]`).first();
  await expect(
    метка,
    `метки ${КЛЮЧ}=${ЗНАЧЕНИЕ} нет в списке: предмет пробы не создан либо метки не показываются`,
  ).toBeVisible({ timeout: 30_000 });

  // 1. Метка ОБЪЯВЛЕНА действием — её видит и программа чтения с экрана.
  await expect(
    метка,
    "метка не объявлена кнопкой: она совершает действие, но для клавиатуры и для " +
      "программы чтения с экрана этого действия не существует",
  ).toHaveRole("button");

  // 2. До неё ДОХОДИТ обход с клавиатуры. Это отдельное утверждение: роль может
  //    быть верной, а элемент — исключён из обхода либо накрыт слоем.
  await page.locator("body").press("Tab");
  const дошли = await expect
    .poll(
      async () => {
        for (let i = 0; i < 60; i++) {
          if (await метка.evaluate((el) => el === document.activeElement)) return true;
          await page.keyboard.press("Tab");
        }
        return false;
      },
      {
        message:
          "за 60 нажатий Tab фокус до метки не дошёл — значит действие есть, а достать " +
          "его с клавиатуры нельзя: элемент вне порядка обхода либо перекрыт",
        timeout: 60_000,
      },
    )
    .toBe(true);
  void дошли;

  // 3. И СРАБАТЫВАЕТ по клавише — с тем же результатом, что по указателю.
  await page.keyboard.press("Enter");
  await expect
    .poll(async () => page.evaluate(() => navigator.clipboard.readText()), {
      message:
        "Enter на метке не положил в буфер машинную форму: до кнопки дошли, но " +
        "нажатие клавишей результата не дало",
      timeout: 15_000,
    })
    .toBe(`${КЛЮЧ}=${ЗНАЧЕНИЕ}`);
});

test("счётчик скрытых меток в обход клавишей НЕ попадает — он ничего не делает", async ({ page }) => {
  // verifies #925
  //
  // Близнец к утверждению выше, и он обязателен. Без него «метка достижима с
  // клавиатуры» зеленело бы и на странице, где в обход попало ВСЁ подряд, —
  // а лишние остановки обхода мешают ровно тому, ради кого он делается: чтобы
  // дойти до нужного действия, приходится миновать десяток недействий.
  const { projectId } = await tenantWithProject(page);

  const имя = `a11y-many-${runTag()}`;
  const ответ = await page.request.post("/vpc/v1/networks", {
    data: { projectId, name: имя, labels: { a: "1", b: "2", c: "3", d: "4", e: "5" } },
  });
  await createdResourceId(page, ответ, "networkId", (id) => `/vpc/v1/networks/${id}`, "сеть с пятью метками");

  await page.goto(`/projects/${projectId}/vpc/networks`, { waitUntil: "domcontentloaded" });

  const счётчик = page.locator("text=/^\\+\\d+$/").first();
  await expect(
    счётчик,
    "счётчика скрытых меток нет: предмет пробы не создан — значит утверждение ниже " +
      "было бы вакуумным",
  ).toBeVisible({ timeout: 30_000 });

  await expect(
    счётчик,
    "счётчик скрытых меток объявлен кнопкой, хотя ничего не делает: обход с " +
      "клавиатуры получает лишнюю остановку без действия",
  ).not.toHaveRole("button");
});
