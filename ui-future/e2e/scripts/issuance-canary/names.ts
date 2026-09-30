// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

/**
 * Единицы канарейки набора и адреса их выпуска — общие для канарейки
 * (`fixtures.canary.ts`) и самопроверки, которая читает её исход
 * (`../issuance-guard-selftest.ts`). Отдельным модулем, потому что файл с
 * вызовами `test(…)` вне прогонщика не импортируется.
 *
 * Адреса лежат под префиксами сервера петли самопроверки: `/b/` — выпуск мимо
 * транспорта (до сервера не должно дойти ничего), `/t/` — выпуск транспортом.
 */
export const FIXTURE_CANARY = {
  swallowed: {
    title: "канарейка F8-46 · обход, проглоченный консолью, роняет пробу набора",
    path: "/b/fixture-canary",
  },
  twin: {
    title: "близнец F8-46 · тот же выпуск упорядочивающим транспортом проходит",
    path: "/t/fixture-canary",
  },
} as const;

/** Адрес сервера петли самопроверки — его ставит самопроверка процессу канарейки. */
export const CANARY_ORIGIN_ENV = "KACHO_ISSUANCE_CANARY_ORIGIN";

/** Каталог отчёта и выходов процесса канарейки — его же читает самопроверка. */
export const CANARY_OUT_ENV = "KACHO_ISSUANCE_CANARY_OUT";

/** Текст, которым фикстура набора роняет пробу с обходом (`specs/issuance-guard.ts`, `formatBreaches`). */
export const FIXTURE_REFUSAL = "обращений консоли мимо упорядочивающего транспорта";
