// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { test, expect } from "@playwright/test";
import { registerAndSignIn, runTag } from "./fixtures";

/**
 * Мутирующий поток проверяется по РЕСУРСУ, а не по закрытию формы.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ПОЧЕМУ «ФОРМА ЗАКРЫЛАСЬ» НИЧЕГО НЕ ЗНАЧИТ
 *
 * Форма закрывается и на успехе, и на потерянном ответе, и на асинхронной
 * операции, завершившейся отказом: мутации возвращают операцию, а не ресурс, и
 * отказ приходит ПОСЛЕ того, как форма ушла с экрана. Проба, утверждающая
 * закрытие формы, зеленеет на несозданном ресурсе.
 *
 * Поэтому утверждается цепочка целиком: запрос принят → ресурс есть в списке →
 * ресурс ВИДЕН на странице. Последнее звено отдельное: список может отвечать
 * верно, пока страница его не показывает.
 */

test("создание сети: ресурс появляется и виден на странице", async ({ page }) => {
  const { projectId } = await registerAndSignIn(page);
  const имя = `net-e2e-${runTag()}`;

  await page.goto(`/projects/${projectId}/vpc/networks`, { waitUntil: "domcontentloaded" });

  const создать = page.locator('button:has-text("Создать"), a:has-text("Создать")').first();
  await expect(создать, "на странице сетей нет элемента создания").toBeVisible({
    timeout: 30_000,
  });
  await создать.click();

  const поле = page.locator('input[type="text"]:visible, input:not([type]):visible').first();
  await expect(поле, "форма создания не предложила ни одного поля").toBeVisible({
    timeout: 20_000,
  });
  await поле.fill(имя);

  // АДРЕСНЫЙ БЛОК ЗАПОЛНЯЕТСЯ, потому что форма его требует, — и требует
  // справедливо. Шаблон сети открывает список одной пустой строкой, а её
  // подполе объявлено обязательным: отправив форму нетронутой, арендатор
  // послал бы на край пустой диапазон и получил отказ СЕРВЕРА — сообщение о
  // поле, которого он не заполнял, в тоне чужого API. Форма теперь называет
  // предмет сама, до отправки, и это то поведение, которое проба обязана
  // воспроизводить: она проходит путь арендатора, а не обходит его.
  //
  // Сеть без адресного блока край принимает (проверено: 200), но это НЕ то же
  // самое, что сеть с пустой строкой в списке, — а форма шлёт именно её.
  const cidr = page.locator('input[placeholder="10.20.0.0/16"]:visible').first();
  await expect(
    cidr,
    "на форме создания сети нет поля адресного блока — предмет пробы изменился, а не сломался",
  ).toBeVisible({ timeout: 20_000 });
  await cidr.fill("10.91.0.0/16");

  const [ответ] = await Promise.all([
    page.waitForResponse(
      (r) => r.url().includes("/vpc/v1/networks") && r.request().method() === "POST",
      { timeout: 40_000 },
    ),
    page.locator('button:has-text("Создать"):visible, button[type="submit"]:visible').last().click(),
  ]);
  expect(ответ.status(), "запрос на создание отвергнут краем").toBe(200);

  // Ресурс появляется в ограниченном окне — это законное ожидание СОБЫТИЯ,
  // а не повтор пробы: мутация асинхронна by design.
  await expect
    .poll(
      async () => {
        const res = await page.request.get(`/vpc/v1/networks?projectId=${projectId}`);
        if (!res.ok()) return "";
        return await res.text();
      },
      {
        message:
          "сеть не появилась в списке после принятого запроса. Форма при этом " +
          "закрылась — именно поэтому её закрытие не является утверждением",
        timeout: 60_000,
      },
    )
    .toContain(имя);

  await page.goto(`/projects/${projectId}/vpc/networks`, { waitUntil: "domcontentloaded" });
  await expect(
    page.getByText(имя, { exact: false }).first(),
    "сеть есть в API, но не видна на странице: список отвечает верно, а консоль " +
      "его не показывает — для пользователя ресурса нет",
  ).toBeVisible({ timeout: 30_000 });
});

test("создание зональной подсети: справочник размещения обязан быть засеян", async ({ page }) => {
  const { projectId } = await registerAndSignIn(page);
  const имяСети = `net-z-${runTag()}`;
  const имяПодсети = `sub-z-${runTag()}`;

  const зоны = await page.request.get("/geo/v1/zones");
  expect(зоны.ok(), "справочник зон недоступен").toBeTruthy();
  const зона = ((await зоны.json()) as { zones?: Array<{ id: string }> }).zones?.[0]?.id;
  expect(
    зона,
    "справочник зон ПУСТ. Это не косметика: любое создание зонального ресурса " +
      "отвечает «зона не найдена», и свежеподнятый стенд непригоден — при том " +
      "что все поды готовы и ни один гейт этого не показывает",
  ).toBeTruthy();

  // Супернет объявляется НАМЕРЕННО: сеть без него подсети не принимает — нарезать
  // не из чего. Проба, создававшая сеть без супернета, была снисходительнее
  // продукта и прятала бы этот отказ.
  const сеть = await page.request.post("/vpc/v1/networks", {
    data: { projectId, name: имяСети, ipv4CidrBlocks: ["10.77.0.0/16"] },
  });
  expect(сеть.status(), "сеть не создана").toBe(200);

  const netId = await expect
    .poll(
      async () => {
        const res = await page.request.get(`/vpc/v1/networks?projectId=${projectId}`);
        if (!res.ok()) return "";
        const b = (await res.json()) as { networks?: Array<{ id: string; name: string }> };
        return b.networks?.find((n) => n.name === имяСети)?.id ?? "";
      },
      { message: "идентификатор созданной сети не получен", timeout: 60_000 },
    )
    .not.toBe("");
  void netId;

  const list = (await (await page.request.get(`/vpc/v1/networks?projectId=${projectId}`)).json()) as {
    networks: Array<{ id: string; name: string }>;
  };
  const id = list.networks.find((n) => n.name === имяСети)!.id;

  // placement_type НЕ передаётся: он выводится сервером, и присланное значение
  // отвергается явно. Проба говорит с контрактом, а не спорит с ним.
  //
  // Якорь адресов называется `ipv4CidrPrimary` и он ОБЯЗАТЕЛЕН (в паре с
  // `ipv6CidrPrimary`: подсеть вправе быть только-v6, но безадресной — нет).
  // Прежде здесь стояло `v4CidrBlocks` — набор равноправных диапазонов, снятый
  // с контракта: у подсети есть ОДИН неизменяемый основной якорь, а
  // дополнительные диапазоны добавляются отдельным глаголом. Проба, посылавшая
  // снятое поле, получала отказ края по существу — и это ровно то, ради чего
  // сквозная проба и существует.
  const подсеть = await page.request.post("/vpc/v1/subnets", {
    data: {
      projectId,
      networkId: id,
      name: имяПодсети,
      zoneId: зона,
      ipv4CidrPrimary: "10.77.7.0/24",
    },
  });
  expect(
    подсеть.status(),
    `зональная подсеть не создана: ${(await подсеть.text()).slice(0, 200)}`,
  ).toBe(200);
});
