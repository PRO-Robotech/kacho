// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import path from "node:path";
import { defineConfig } from "@playwright/test";

import { MAIL_CANARY_DEAD_ENV, MAIL_CANARY_LIVE_ENV, MAIL_CANARY_OUT_ENV } from "./names.ts";

/**
 * Прогон канарейки чтения приёмника писем (`reads.canary.ts`) — только её, и
 * только из самопроверки `../mail-receiver-marks-selftest.ts`: адреса приёмника
 * петли и каталог отчёта ставит она. Повторов нет — исход единицы и есть
 * предмет. Браузер не нужен: единицы не берут ни `page`, ни `browser`.
 */
const live = process.env[MAIL_CANARY_LIVE_ENV];
const dead = process.env[MAIL_CANARY_DEAD_ENV];
const out = process.env[MAIL_CANARY_OUT_ENV];
if (!live || !dead || !out) {
  throw new Error(
    `${MAIL_CANARY_LIVE_ENV}, ${MAIL_CANARY_DEAD_ENV} и ${MAIL_CANARY_OUT_ENV} не заданы: канарейку чтения ` +
      "приёмника исполняет самопроверка scripts/mail-receiver-marks-selftest.ts со своим приёмником петли, " +
      "а не прогон вручную.",
  );
}

export default defineConfig({
  testDir: ".",
  testMatch: "*.canary.ts",
  retries: 0,
  workers: 1,
  timeout: 30_000,
  outputDir: path.join(out, "test-results"),
  reporter: [["json", { outputFile: path.join(out, "report.json") }]],
});
