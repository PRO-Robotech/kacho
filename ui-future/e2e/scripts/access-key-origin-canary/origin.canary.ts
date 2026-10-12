// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { expect, test } from "@playwright/test";

import { accessKeyOriginCondition } from "../../specs/access-key-seed.ts";
import { MODE_PATH, ORIGIN_CANARY, type EdgeMode } from "./names.ts";

/**
 * КАНАРЕЙКА УСЛОВИЯ П4 (приёмка F8-S4, §4 П4, DoD п. 6) — не проба, а ВХОД
 * самопроверки `../access-key-origin-selftest.ts`. Каждая единица исполняет
 * ТЕЛО пробы-предусловия (`accessKeyOriginCondition` — его же зовёт
 * `preconditions/access-key-origin.precondition.ts`) против края петли; разница
 * с предусловием одна — «Дано»: вместо посева П-п единица ставит режим края.
 * Исход судит НАСТОЯЩИЙ распознаватель гейта вердикта, а не его пересказ.
 *
 * Имя файла — не `*.precondition.ts` и не `*.spec.ts`: в набор он не входит.
 */

const given = (mode: EdgeMode) => async (seed: Parameters<Parameters<typeof accessKeyOriginCondition>[1]>[0]) => {
  const res = await seed.read(`${MODE_PATH}${mode}`);
  expect(res.status(), `край петли не принял режим ${mode}`).toBe(200);
};

for (const unit of Object.values(ORIGIN_CANARY)) {
  test(unit.title, async ({ browserName: _browser }, testInfo) => {
    await accessKeyOriginCondition(testInfo, given(unit.mode));
  });
}
