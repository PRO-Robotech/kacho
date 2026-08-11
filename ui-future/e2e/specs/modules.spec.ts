// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { test, expect } from "@playwright/test";
import { registerAndSignIn, apiCalls } from "./fixtures";

/**
 * Каждый модуль консоли ОТКРЫВАЕТСЯ и ГОВОРИТ С API.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ПОЧЕМУ «СТРАНИЦА ОТВЕТИЛА 200» — НЕ УТВЕРЖДЕНИЕ
 *
 * Консоль — одностраничное приложение: её сервер отдаёт один и тот же документ на
 * любой путь, поэтому 200 приходит и тогда, когда модуль не загрузился вовсе.
 * Ровно на этом обжигаются: страница «работает», а пользователь видит скелет.
 * Утверждать надо ДВА свойства сразу:
 *
 *   1. модуль СДЕЛАЛ свой вызов к API и получил не-ошибку — значит куски
 *      федерации загрузились и код модуля исполнился;
 *   2. в консоли браузера НЕТ ошибок — 404 на куске федерации виден только там.
 *
 * Наблюдалось живьём: переименование пути ассетов оставило страницу отвечающей
 * 200, при том что куски модуля отдавали 404 и консоль была пустым скелетом.
 */

interface Модуль {
  имя: string;
  путь: (projectId: string) => string;
  ждём: RegExp;
}

const МОДУЛИ: Модуль[] = [
  { имя: "сводка", путь: (p) => `/projects/${p}/dashboard`, ждём: /\/iam\/v1\// },
  { имя: "сети", путь: (p) => `/projects/${p}/vpc/networks`, ждём: /\/vpc\/v1\/networks/ },
  { имя: "подсети", путь: (p) => `/projects/${p}/vpc/subnets`, ждём: /\/vpc\/v1\/subnets/ },
  {
    имя: "группы безопасности",
    путь: (p) => `/projects/${p}/vpc/security-groups`,
    ждём: /\/vpc\/v1\/securityGroups/,
  },
  { имя: "машины", путь: (p) => `/projects/${p}/compute/instances`, ждём: /\/compute\/v1\// },
  { имя: "тома", путь: (p) => `/projects/${p}/storage/volumes`, ждём: /\/storage\/v1\// },
  { имя: "балансировщики", путь: (p) => `/projects/${p}/nlb/load-balancers`, ждём: /\/nlb\/v1\// },
  { имя: "реестры", путь: (p) => `/projects/${p}/registry/registries`, ждём: /\/registry\/v1\// },
  { имя: "пользователи", путь: (p) => `/projects/${p}/iam/users`, ждём: /\/iam\/v1\// },
];

test.describe("модули консоли", () => {
  test("каждый модуль загружается и делает свой вызов к API", async ({ page }) => {
    const ошибки: string[] = [];
    page.on("console", (m) => {
      if (m.type() === "error") ошибки.push(m.text().slice(0, 200));
    });
    const вызовы = apiCalls(page);

    const { projectId } = await registerAndSignIn(page);

    for (const м of МОДУЛИ) {
      вызовы.length = 0;
      ошибки.length = 0;

      await page.goto(м.путь(projectId), { waitUntil: "domcontentloaded" });

      await expect
        .poll(() => вызовы.filter((c) => м.ждём.test(c)), {
          message:
            `модуль «${м.имя}» не сделал ни одного своего вызова к API. ` +
            `Страница при этом отвечает 200 — так отвечает и скелет, у которого ` +
            `не загрузились куски федерации`,
          timeout: 30_000,
        })
        .not.toHaveLength(0);

      const неудачные = вызовы.filter((c) => м.ждём.test(c) && !c.startsWith("2"));
      expect(
        неудачные,
        `модуль «${м.имя}» получил отказ на своём вызове: ${неудачные.join(", ")}`,
      ).toHaveLength(0);

      expect(
        ошибки,
        `модуль «${м.имя}» дал ошибки в консоли браузера — так виден 404 на куске ` +
          `федерации, которого не видно по коду ответа страницы`,
      ).toHaveLength(0);
    }
  });

  test("админский раздел открывается и читает глобальный справочник", async ({ page }) => {
    const вызовы = apiCalls(page);
    await registerAndSignIn(page);

    await page.goto("/system/regions", { waitUntil: "domcontentloaded" });
    await expect
      .poll(() => вызовы.filter((c) => /\/geo\/v1\/regions/.test(c)), {
        message:
          "админский раздел не спросил справочник размещения. Он объявлен доступным " +
          "каждому аутентифицированному арендатору намеренно: без него нельзя выбрать " +
          "зону, то есть нельзя создать ни один размещаемый ресурс",
        timeout: 30_000,
      })
      .not.toHaveLength(0);

    const отказы = вызовы.filter((c) => /\/geo\/v1\/regions/.test(c) && !c.startsWith("2"));
    expect(отказы, `справочник размещения отказал: ${отказы.join(", ")}`).toHaveLength(0);
  });
});
