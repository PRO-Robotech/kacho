// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import path from "node:path";
import { defineConfig } from "@playwright/test";

import { CANARY_ORIGIN_ENV, CANARY_OUT_ENV } from "./names.ts";

/**
 * Прогон канарейки фикстуры набора (`fixtures.canary.ts`) — только её, и только
 * из самопроверки `../issuance-guard-selftest.ts`: адрес сервера петли и каталог
 * отчёта ставит она. Повторов нет — исход единицы и есть предмет; трассу пишет
 * фикстура набора, поэтому штатная выключена (см. `specs/fixtures.ts`).
 */
const origin = process.env[CANARY_ORIGIN_ENV];
const out = process.env[CANARY_OUT_ENV];
if (!origin || !out) {
  throw new Error(
    `${CANARY_ORIGIN_ENV} и ${CANARY_OUT_ENV} не заданы: канарейку фикстуры набора исполняет ` +
      "самопроверка scripts/issuance-guard-selftest.ts со своим сервером петли, а не прогон вручную.",
  );
}

export default defineConfig({
  testDir: ".",
  testMatch: "*.canary.ts",
  retries: 0,
  workers: 1,
  timeout: 60_000,
  outputDir: path.join(out, "test-results"),
  reporter: [["json", { outputFile: path.join(out, "report.json") }]],
  use: {
    baseURL: origin,
    trace: "off",
    video: "off",
    screenshot: "off",
    launchOptions: process.env.KACHO_CHROMIUM ? { executablePath: process.env.KACHO_CHROMIUM } : {},
  },
});
