// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { expect } from "@playwright/test";
import { registerAndSignIn, test } from "./fixtures";

/**
 * Отказ — тоже правильное поведение, и он проверяется НАРАВНЕ с успехом.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ПОЧЕМУ ЭТИ ПРОБЫ НЕ МОГУТ СТОЯТЬ БЕЗ ПОЛОЖИТЕЛЬНЫХ
 *
 * Утверждение «этому отказано» зеленеет и на продукте, который отказывает ВСЕМ:
 * сломанная модель прав, недоступное хранилище, незаписанная модель — всё это
 * даёт отказ. Поэтому в каждой пробе рядом стоит положительное: своё обязано
 * отвечать успехом в том же прогоне, той же сессией.
 *
 * Наблюдалось живьём: незаписанная модель прав давала 401 на КАЖДОМ запросе, и
 * набор из одних отрицаний был бы полностью зелёным на полностью неработающем
 * продукте.
 */

test("неаутентифицированный не получает ничего, аутентифицированный — своё", async ({
  page,
  request,
}) => {
  // Отрицание: без сессии — отказ на каждом домене.
  for (const path of [
    "/vpc/v1/networks",
    "/compute/v1/instances",
    "/storage/v1/volumes",
    "/nlb/v1/networkLoadBalancers",
    "/registry/v1/registries",
    "/iam/v1/projects",
  ]) {
    const r = await request.get(path);
    expect(
      r.status(),
      `${path} ответил ${r.status()} без единого признака личности — ` +
        `отказ по умолчанию не действует`,
    ).toBe(401);
  }

  // Положительное В ТОМ ЖЕ ПРОГОНЕ: иначе всё выше зеленеет на мёртвом продукте.
  const { projectId } = await registerAndSignIn(page);
  for (const path of [
    `/vpc/v1/networks?projectId=${projectId}`,
    `/compute/v1/instances?projectId=${projectId}`,
    "/iam/v1/projects",
  ]) {
    const r = await page.request.get(path);
    expect(r.status(), `своё недоступно: ${path} → ${r.status()}`).toBe(200);
  }
});

test("админские поверхности закрыты обычному арендатору, справочник — открыт", async ({
  page,
}) => {
  await registerAndSignIn(page);

  // Отрицание: поверхности, требующие полномочий уровня кластера.
  for (const path of ["/vpc/v1/addressPools", "/iam/v1/internal/cluster/admins"]) {
    const r = await page.request.get(path);
    expect(
      [403, 404].includes(r.status()),
      `${path} ответил ${r.status()} обычному арендатору — админская поверхность открыта`,
    ).toBeTruthy();
  }

  // Положительное: глобальный справочник размещения объявлен доступным каждому
  // аутентифицированному НАМЕРЕННО — без него нельзя выбрать зону.
  const catalog = await page.request.get("/geo/v1/regions");
  expect(
    catalog.status(),
    "справочник размещения закрыт: арендатор без него не создаст ни один " +
      "размещаемый ресурс, а решение об открытости принято и записано",
  ).toBe(200);
});

test("чужой проект не читается", async ({ page }) => {
  const { projectId } = await registerAndSignIn(page);

  const own = await page.request.get(`/vpc/v1/networks?projectId=${projectId}`);
  expect(own.status(), "своё недоступно — отрицание ниже стало бы бессмысленным").toBe(200);

  const foreign = await page.request.get("/vpc/v1/networks?projectId=prjzzzzzzzzzzzzzzzzz");
  expect(
    [400, 403, 404].includes(foreign.status()),
    `чужой проект ответил ${foreign.status()} — граница арендатора не держится`,
  ).toBeTruthy();
});
