// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import type { Page } from "@playwright/test";
import { test } from "../../specs/fixtures.ts";
import { FIXTURE_CANARY } from "./names.ts";

/**
 * КАНАРЕЙКА ФИКСТУРЫ НАБОРА (приёмка F8, Р10, F8-46) — не проба, а ВХОД
 * самопроверки `../issuance-guard-selftest.ts`. Её исполняет отдельный процесс
 * прогонщика, а `test` взят у НАСТОЯЩЕЙ фикстуры набора (`specs/fixtures.ts`):
 * судится то, что роняет пробу набора, — разбор `issuanceLedger`, а не
 * `takeBreaches`, который самопроверка зовёт мимо фикстуры.
 *
 * Первая единица ОБЯЗАНА упасть текстом фикстуры: обход, выпущенный страницей
 * мимо упорядочивающего транспорта, проглочен — так глотает его консоль, — и
 * пробу обязана уронить запись, а не отказ. Близнец выпускает то же
 * упорядочивающим транспортом и проходит. Изменён ровно один факт — чем выпущено.
 *
 * Имя файла — не `*.spec.ts`: в набор (`playwright.config.ts`, `specs/`) он не
 * входит и стенда не требует; его прогон задаёт `canary.playwright.config.ts`.
 */

type Ordered = { fetch(url: string): Promise<Response> };

/** Страница сервера петли с настоящим упорядочивающим транспортом (`window.__ordered`). */
async function openSender(page: Page): Promise<void> {
  await page.goto("/sender.html");
  await page.waitForFunction(() => typeof (window as unknown as { __ordered?: unknown }).__ordered === "object");
}

test(FIXTURE_CANARY.swallowed.title, async ({ page }) => {
  await openSender(page);
  await page.evaluate(
    (at) =>
      fetch(at).then(
        () => undefined,
        () => undefined,
      ),
    FIXTURE_CANARY.swallowed.path,
  );
});

test(FIXTURE_CANARY.twin.title, async ({ page }) => {
  await openSender(page);
  await page.evaluate(
    (at) =>
      (window as unknown as { __ordered: Ordered }).__ordered.fetch(at).then(
        () => undefined,
        () => undefined,
      ),
    FIXTURE_CANARY.twin.path,
  );
});
