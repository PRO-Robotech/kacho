// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { expect, type Page } from "@playwright/test";
import { newSeed } from "./ceremony-seed";
import { registerAndSignIn, test } from "./fixtures";

/**
 * Раздел «Система» предлагает мутацию регионов, зон и пулов адресов, и край её
 * обслуживает (kacho#3091, kacho#3094, продолжение #2692).
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ПРИЗНАК
 *
 * Консоль одна на обе посадки — внешнюю (арендаторскую) и внутреннюю
 * (операторскую). До kacho#3094 она узнавала посадку пробой края и на внешней
 * снимала кнопки мутаций; после ADM-1 S1 проба посадки отвечала неверно, и
 * кнопка обещала действие, на которое край отвечал «маршрута нет» (kacho#3091).
 *
 * С kacho#3094 все три экрана стоят на ПУБЛИЧНЫХ службах (geo — регионы и зоны,
 * `AddressPoolService` — пулы адресов): консоль посадку больше не спрашивает,
 * ветви «раздел недоступен на этой посадке» в ней нет, и кнопка мутации на
 * экране есть всегда. Поэтому предмет пробы — согласие двух сторон одного
 * обещания: кнопка на экране И ответ края на мутацию, которую она отправила бы.
 * «Маршрута нет» на публичной мутации — это потерянная служба, а не посадка.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ЧТО УТВЕРЖДАЕТСЯ И ОТКУДА БЕРЁТСЯ ОЖИДАНИЕ
 *
 *   - край обслуживает мутацию каждого экрана: аноним получает на неё отказ
 *     входа (401), а не «маршрута нет» (404). Тело пустое, носителя нет — до
 *     службы запрос не доходит, ничего не заводится;
 *   - арендатор видит на каждом экране кнопку этой мутации. Решение о показе
 *     судит не права: отказ в правах остаётся отказом в правах и называется
 *     словами (его утверждает `system-admin-public.spec.ts`).
 *
 * Обе стороны утверждаются положительно и на непустом предмете: экран сначала
 * обязан отрисоваться подписью, которую рисует только модуль раздела, и только
 * после этого судится кнопка.
 */

/** Экран раздела и мутация, которую его кнопка отправила бы краю. */
interface Surface {
  name: string;
  address: string;
  mutation: string;
  /** Кнопка мутации на этом экране. */
  action: (page: Page) => ReturnType<Page["getByRole"]>;
  /** Подпись, которую рисует только сам модуль раздела: признак, что экран отрисован. */
  rendered: (page: Page) => ReturnType<Page["getByText"]>;
}

const SURFACES: Surface[] = [
  {
    // Регионы и зоны — на публичной службе geo (kacho#3094, #3092). Экрана
    // администраторов кластера здесь нет: его перечень край отдаёт только
    // держателю права, и арендатор видит отказ словами — это решение прав, а не
    // посадки (его утверждает system-admin-public.spec.ts).
    name: "регионы",
    address: "/system/regions",
    mutation: "/geo/v1/regions",
    action: (page) => page.getByRole("link", { name: /Создать/ }),
    rendered: (page) => page.getByText("Зоны", { exact: true }).first(),
  },
  {
    name: "зоны",
    address: "/system/zones",
    mutation: "/geo/v1/zones",
    action: (page) => page.getByRole("link", { name: /Создать/ }),
    rendered: (page) => page.getByText("Регионы", { exact: true }).first(),
  },
  {
    // Пулы адресов — их мутации с ADM-1 S1 обслуживает публичная
    // `AddressPoolService` (kacho#3091).
    name: "пулы адресов",
    address: "/system/address-pools",
    mutation: "/vpc/v1/addressPools",
    action: (page) => page.getByRole("link", { name: /Создать/ }),
    rendered: (page) => page.getByText("Регионы", { exact: true }).first(),
  },
];

test("раздел «Система» показывает мутацию регионов, зон и пулов адресов, и край её обслуживает", async ({
  page,
}, testInfo) => {
  // verifies #3091 — ложное «плоскость есть» после ADM-1 S1: кнопка мутации обязана вести в обслуживаемую мутацию.
  // verifies #3094 — экраны регионов и зон на публичной службе geo: кнопки на любой посадке.
  test.setTimeout(240_000);

  // Ответ края на мутацию каждого экрана — до входа, анонимно.
  const seed = await newSeed(testInfo);
  try {
    for (const s of SURFACES) {
      const res = await seed.api.post(s.mutation, { data: {} });
      testInfo.annotations.push({
        type: "мутация экрана",
        description: `${s.name}: POST ${s.mutation} → ${res.status()}`,
      });
      expect(
        res.status(),
        `край ответил анониму на мутацию экрана «${s.name}» (${s.mutation}) ${res.status()} ` +
          `${(await res.text()).slice(0, 300)} — её обслуживает публичная служба, ждался отказ входа 401 ` +
          "(404 — служба потеряна: кнопка на экране обещала бы «маршрута нет»)",
      ).toBe(401);
    }
  } finally {
    await seed.dispose();
  }

  await registerAndSignIn(page);

  for (const s of SURFACES) {
    await page.goto(s.address, { waitUntil: "domcontentloaded" });
    await expect(
      s.rendered(page),
      `экран «${s.name}» не отрисовался — условие пробы не создано`,
    ).toBeVisible({
      timeout: 60_000,
    });
    await expect(
      s.action(page),
      `край обслуживает мутацию экрана «${s.name}» (${s.mutation}), а кнопки мутации на экране нет`,
    ).toBeVisible({ timeout: 30_000 });
  }
});
