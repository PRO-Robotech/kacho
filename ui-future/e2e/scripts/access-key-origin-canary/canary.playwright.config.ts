// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import path from "node:path";
import { defineConfig } from "@playwright/test";

import { ORIGIN_CANARY_EDGE_ENV, ORIGIN_CANARY_OUT_ENV } from "./names.ts";

/**
 * Прогон канарейки условия П4 (`origin.canary.ts`) — только её, и только из
 * самопроверки `../access-key-origin-selftest.ts`: адрес края петли и каталог
 * отчёта ставит она. Повторов нет — исход единицы и есть предмет. Браузер не
 * нужен: условие идёт посевом, а не страницей.
 */
const edge = process.env[ORIGIN_CANARY_EDGE_ENV];
const out = process.env[ORIGIN_CANARY_OUT_ENV];
if (!edge || !out) {
  throw new Error(
    `${ORIGIN_CANARY_EDGE_ENV} и ${ORIGIN_CANARY_OUT_ENV} не заданы: канарейку условия П4 исполняет ` +
      "самопроверка scripts/access-key-origin-selftest.ts со своим краем петли, а не прогон вручную.",
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
  use: { baseURL: edge },
});
