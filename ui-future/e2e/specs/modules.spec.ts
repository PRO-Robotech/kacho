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

interface Module {
  name: string;
  path: (projectId: string) => string;
  awaitedCall: RegExp;
}

const MODULES: Module[] = [
  { name: "сводка", path: (p) => `/projects/${p}/dashboard`, awaitedCall: /\/iam\/v1\// },
  { name: "сети", path: (p) => `/projects/${p}/vpc/networks`, awaitedCall: /\/vpc\/v1\/networks/ },
  { name: "подсети", path: (p) => `/projects/${p}/vpc/subnets`, awaitedCall: /\/vpc\/v1\/subnets/ },
  {
    name: "группы безопасности",
    path: (p) => `/projects/${p}/vpc/security-groups`,
    awaitedCall: /\/vpc\/v1\/securityGroups/,
  },
  { name: "машины", path: (p) => `/projects/${p}/compute/instances`, awaitedCall: /\/compute\/v1\// },
  { name: "тома", path: (p) => `/projects/${p}/storage/volumes`, awaitedCall: /\/storage\/v1\// },
  { name: "балансировщики", path: (p) => `/projects/${p}/nlb/load-balancers`, awaitedCall: /\/nlb\/v1\// },
  { name: "реестры", path: (p) => `/projects/${p}/registry/registries`, awaitedCall: /\/registry\/v1\// },
  { name: "пользователи", path: (p) => `/projects/${p}/iam/users`, awaitedCall: /\/iam\/v1\// },
];

test.describe("модули консоли", () => {
  test("каждый модуль загружается и делает свой вызов к API", async ({ page }) => {
    const errors: string[] = [];
    page.on("console", (m) => {
      if (m.type() === "error") errors.push(m.text().slice(0, 200));
    });
    const calls = apiCalls(page);

    const { projectId } = await registerAndSignIn(page);

    for (const mod of MODULES) {
      calls.length = 0;
      errors.length = 0;

      await page.goto(mod.path(projectId), { waitUntil: "domcontentloaded" });

      await expect
        .poll(() => calls.filter((c) => mod.awaitedCall.test(c)), {
          message:
            `модуль «${mod.name}» не сделал ни одного своего вызова к API. ` +
            `Страница при этом отвечает 200 — так отвечает и скелет, у которого ` +
            `не загрузились куски федерации`,
          timeout: 30_000,
        })
        .not.toHaveLength(0);

      const failed = calls.filter((c) => mod.awaitedCall.test(c) && !c.startsWith("2"));
      expect(
        failed,
        `модуль «${mod.name}» получил отказ на своём вызове: ${failed.join(", ")}`,
      ).toHaveLength(0);

      expect(
        errors,
        `модуль «${mod.name}» дал ошибки в консоли браузера — так виден 404 на куске ` +
          `федерации, которого не видно по коду ответа страницы`,
      ).toHaveLength(0);
    }
  });

  test("админский раздел открывается и читает глобальный справочник", async ({ page }) => {
    const calls = apiCalls(page);
    await registerAndSignIn(page);

    await page.goto("/system/regions", { waitUntil: "domcontentloaded" });
    await expect
      .poll(() => calls.filter((c) => /\/geo\/v1\/regions/.test(c)), {
        message:
          "админский раздел не спросил справочник размещения. Он объявлен доступным " +
          "каждому аутентифицированному арендатору намеренно: без него нельзя выбрать " +
          "зону, то есть нельзя создать ни один размещаемый ресурс",
        timeout: 30_000,
      })
      .not.toHaveLength(0);

    const refusals = calls.filter((c) => /\/geo\/v1\/regions/.test(c) && !c.startsWith("2"));
    expect(refusals, `справочник размещения отказал: ${refusals.join(", ")}`).toHaveLength(0);
  });
});
