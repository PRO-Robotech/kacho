// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import path from "node:path";
import { expect } from "@playwright/test";
import { test } from "./fixtures";
import { censusOfSource, wallClockCensus, wallClockCensusOf } from "./wall-clock-census";

/**
 * Держатель правила Р9 приёмки F8: в наборе нет ожидания стенными часами.
 *
 * Браузера здесь нет: предмет — код набора, а не продукт. Проба живёт в наборе,
 * потому что судит ИМЕННО его и исполняется его прогоном: правило, которое
 * держат вне прогона набора, ломается тем же изменением, что вносит ожидание.
 */

function describe(findings: ReadonlyArray<{ file: string; line: number; what: string }>): string[] {
  return findings.map((f) => `${f.file}:${f.line} ${f.what}`);
}

test("F8-45 · в наборе не осталось ожидания стенными часами", () => {
  // verifies #1274
  const root = path.dirname(test.info().project.testDir);
  const census = wallClockCensus(root);
  // Обе группы — поимённо (условие C22): ожидания срока (находки) и паузы между
  // опросами условия (не находки). Паузу, перешедшую из второй группы в первую,
  // видно по координате, а не только по числу.
  console.log(
    `[перепись ожиданий] файлов прочитано ${census.filesRead} · ожиданий условия ${census.conditionWaits}` +
      ` · пауз между опросами ${census.pollPauses} · находок ${census.findings.length}\n` +
      census.pauses.map((p) => `    пауза: ${p.file}:${p.line} ${p.what}\n`).join("") +
      census.findings.map((f) => `    НАХОДКА: ${f.file}:${f.line} ${f.what}\n`).join(""),
  );
  expect(census.filesRead, "обход набора пуст — вердикта нет").toBeGreaterThan(0);
  // Положительная сторона: без неё «ноль» неотличим от предиката, не умеющего
  // распознать ожидание вовсе.
  expect(census.conditionWaits, "ни одного ожидания условия не распознано — предикат слеп").toBeGreaterThan(0);
  expect(describe(census.findings), "в наборе ждут истечения срока стенными часами").toEqual([]);
});

test("F8-45 · инъекция: подсаженное ожидание краснит перепись с координатой, пауза опроса — нет", () => {
  // verifies #1274 — способность упасть доказана подсадкой, а не зеленью выше.
  const sleep = "export async function f() {\n  await new Promise((r) => setTimeout(r, 30_000));\n}\n";
  expect(describe(censusOfSource("planted.ts", sleep).findings)).toEqual(["planted.ts:2 сон вне цикла"]);

  const timed = "export async function f(page) {\n  await page.waitForTimeout(1_000);\n}\n";
  expect(describe(censusOfSource("timed.ts", timed).findings)).toEqual(["timed.ts:2 waitForTimeout"]);

  // Близнец: тот же сон ВНУТРИ цикла опроса — пауза, а не ожидание срока.
  const pause =
    "export async function f(ready) {\n  for (let i = 0; i < 3; i++) {\n    if (await ready()) return;\n" +
    "    await new Promise((r) => setTimeout(r, 2_000));\n  }\n}\n";
  const twin = censusOfSource("twin.ts", pause);
  expect(twin.findings).toEqual([]);
  expect(twin.pollPauses).toBe(1);

  // Таймер обрыва и предел пробы — не ожидания.
  const bounded =
    "const t = setTimeout(() => controller.abort(), 5_000);\ntest.setTimeout(90_000);\n// await page.waitForTimeout(1)\n";
  expect(censusOfSource("bounded.ts", bounded).findings).toEqual([]);

  // Ожидание условия находится и находкой не считается.
  const condition = "await expect.poll(() => x()).toBe(1);\nawait expect(el).toBeVisible();\n";
  const found = censusOfSource("condition.ts", condition);
  expect(found.findings).toEqual([]);
  expect(found.conditionWaits).toBe(2);

  // Пустой обход — отказ, а не «0 находок».
  expect(() => wallClockCensusOf([])).toThrow(/0 файлов/);
});

test("F8-45 · пауза в цикле — пауза только при предметном выходе и длительности не от часов", () => {
  // verifies #1274 — условие C22: «ожидание истечения срока» и «пауза между
  // опросами условия» различаются по узлам. Близнецы — три законные паузы
  // набора по форме: опрос с выходом `return` (фикстура проекта), переоткрытие
  // потока с условием-вызовом (`while (!collected() …)`), подъём с `break`.
  const twins: Array<[string, string]> = [
    [
      "fixture.ts",
      "async function f() {\n  for (let i = 0; i < 8; i++) {\n    const id = await read();\n    if (id) return id;\n" +
        "    await new Promise((r) => setTimeout(r, 2_000));\n  }\n}\n",
    ],
    [
      "stream.ts",
      "async function f() {\n  while (!collected() && Date.now() < until) {\n    await read();\n" +
        "    await new Promise((resolve) => setTimeout(resolve, 250));\n  }\n}\n",
    ],
    [
      "reach.mjs",
      "for (let i = 1; i <= 5; i++) {\n  if (await reached()) break;\n  await new Promise((r) => setTimeout(r, 5_000));\n}\n",
    ],
  ];
  for (const [file, text] of twins) {
    const c = censusOfSource(file, text);
    expect([file, describe(c.findings)]).toEqual([file, []]);
    expect([file, c.pollPauses]).toEqual([file, 1]);
  }

  // Изменён ровно один факт против близнеца — и пауза становится находкой.
  const noExit =
    "async function f() {\n  for (let i = 0; i < 8; i++) {\n    await read();\n" +
    "    await new Promise((r) => setTimeout(r, 2_000));\n  }\n}\n";
  expect(describe(censusOfSource("count.ts", noExit).findings)).toEqual([
    "count.ts:4 сон в цикле без предметного выхода",
  ]);
  const untilClock =
    "async function f() {\n  for (let i = 0; i < 8; i++) {\n    const id = await read();\n    if (id) return id;\n" +
    "    await new Promise((r) => setTimeout(r, nextWindow() - Date.now()));\n  }\n}\n";
  expect(describe(censusOfSource("clock.ts", untilClock).findings)).toEqual([
    "clock.ts:5 сон до момента стенных часов",
  ]);
});
