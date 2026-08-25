// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { expect } from "@playwright/test";
import { copyToast, createdResourceId, runTag, tenantWithProject, test } from "./fixtures";

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

const KEY = "env";
const VALUE = "prod";

test("метка списка достижима клавишей Tab и срабатывает по Enter", async ({ page }) => {
  // verifies #925
  const { projectId } = await tenantWithProject(page);

  const name = `a11y-net-${runTag()}`;
  const response = await page.request.post("/vpc/v1/networks", {
    data: { projectId, name: name, labels: { [KEY]: VALUE } },
  });
  await createdResourceId(page, response, "networkId", (id) => `/vpc/v1/networks/${id}`, "сеть с меткой");

  await page.goto(`/projects/${projectId}/vpc/networks`, { waitUntil: "domcontentloaded" });

  const label = page.locator(`[title="Скопировать ${KEY}=${VALUE}"]`).first();
  await expect(
    label,
    `метки ${KEY}=${VALUE} нет в списке: предмет пробы не создан либо метки не показываются`,
  ).toBeVisible({ timeout: 30_000 });

  // 1. Метка ОБЪЯВЛЕНА действием — её видит и программа чтения с экрана.
  await expect(
    label,
    "метка не объявлена кнопкой: она совершает действие, но для клавиатуры и для " +
      "программы чтения с экрана этого действия не существует",
  ).toHaveRole("button");

  // 2. До неё ДОХОДИТ обход с клавиатуры. Это отдельное утверждение: роль может
  //    быть верной, а элемент — исключён из обхода либо накрыт слоем.
  await page.locator("body").press("Tab");
  const reached = await expect
    .poll(
      async () => {
        for (let i = 0; i < 60; i++) {
          if (await label.evaluate((el) => el === document.activeElement)) return true;
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
  void reached;

  // 3. И СРАБАТЫВАЕТ по клавише — с тем же результатом, что по указателю.
  await page.keyboard.press("Enter");
  // Исход читается по тому, что продукт СКАЗАЛ: подпись успеха несёт саму
  // строку и показывается только при состоявшемся копировании. Системный буфер
  // здесь не спрашивается — вне защищённого контекста его нет вовсе, и такая
  // проба утверждала бы о посадке стенда, а не о консоли.
  await expect(
    copyToast(page, `${KEY}=${VALUE}`),
    "Enter на метке не дал результата: до кнопки дошли, но копирование не " +
      "состоялось либо ушла не та строка",
  ).toBeVisible({ timeout: 15_000 });
});

test("счётчик скрытых меток в обход клавишей НЕ попадает — он ничего не делает", async ({ page }) => {
  // verifies #925
  //
  // Близнец к утверждению выше, и он обязателен. Без него «метка достижима с
  // клавиатуры» зеленело бы и на странице, где в обход попало ВСЁ подряд, —
  // а лишние остановки обхода мешают ровно тому, ради кого он делается: чтобы
  // дойти до нужного действия, приходится миновать десяток недействий.
  const { projectId } = await tenantWithProject(page);

  const name = `a11y-many-${runTag()}`;
  const response = await page.request.post("/vpc/v1/networks", {
    data: { projectId, name: name, labels: { a: "1", b: "2", c: "3", d: "4", e: "5" } },
  });
  await createdResourceId(page, response, "networkId", (id) => `/vpc/v1/networks/${id}`, "сеть с пятью метками");

  await page.goto(`/projects/${projectId}/vpc/networks`, { waitUntil: "domcontentloaded" });

  const counter = page.locator("text=/^\\+\\d+$/").first();
  await expect(
    counter,
    "счётчика скрытых меток нет: предмет пробы не создан — значит утверждение ниже " +
      "было бы вакуумным",
  ).toBeVisible({ timeout: 30_000 });

  await expect(
    counter,
    "счётчик скрытых меток объявлен кнопкой, хотя ничего не делает: обход с " +
      "клавиатуры получает лишнюю остановку без действия",
  ).not.toHaveRole("button");
});
