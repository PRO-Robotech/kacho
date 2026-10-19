// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Синтетический набор, чьи НАСТОЯЩИЕ отчёты playwright читает самопроверка
// гейта `assert-console-probes-verdict.py` (#3289). Раскладка — как у набора
// консоли (`ui-future/e2e/playwright.config.ts`): корневой `testDir` — каталог
// проб, проект условий со своим каталогом и проект проб, зависящий от него,
// без повторов на красном. Отчёт — в файл из `FX_OUT`; пересборка — `regen.sh`.
import { defineConfig } from "@playwright/test";

export default defineConfig({
  testDir: "./specs",
  projects: [
    { name: "precondition", testDir: "./preconditions", testMatch: "*.precondition.ts" },
    { name: "probes", dependencies: ["precondition"] },
  ],
  retries: 0,
  workers: 1,
  maxFailures: 5,
  reporter: [["json", { outputFile: process.env.FX_OUT ?? "results.json" }]],
});
