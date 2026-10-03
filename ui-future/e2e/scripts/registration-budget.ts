// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

/**
 * Сторож расхода окна регистраций источника прогоном (kacho#2909).
 *
 *   KACHO_REGISTRATION_BUDGET=320 node scripts/registration-budget.ts results.json [results.json …]
 *
 * Каждая проба сдала счёт своих регистраций вложением отчёта; сторож
 * складывает их по прогону и судит: каждый прогон ≤ бюджета. Правило счёта —
 * `specs/registration-budget.ts`, один источник на запись и суд.
 *
 * Бюджет — ВХОД, а не умолчание: величину ставит рецепт, гонящий набор, а её
 * сверку с окном стенда держит проба уровня развёртывания
 * (deploy/console_suite_budgets_fit_the_stand_test.go). Без величины вердикта
 * нет, и это отказ, а не «бюджет ноль». Отчёта нет — тоже отказ.
 *
 * Коды — ТРИ исхода: 0 — в бюджете; 1 — бюджет превышен; 3 — вердикта нет
 * (величина не объявлена или не разобрана, отчёта нет, в отчёте ни одной пробы).
 */

import { readFileSync } from "node:fs";
import { REGISTRATION_ATTACHMENT, judgeRegistrations, type ScenarioRegistrations } from "../specs/registration-budget.ts";

interface ReportAttachment {
  name?: string;
  body?: string;
}
interface ReportNode {
  suites?: ReportNode[];
  specs?: Array<{ tests?: Array<{ results?: Array<{ attachments?: ReportAttachment[] }> }> }>;
}

/** Записи проб из отчёта прогона playwright (`reporter: json`). */
export function scenarioRegistrationsOf(report: ReportNode): { records: ScenarioRegistrations[]; tests: number } {
  const records: ScenarioRegistrations[] = [];
  let tests = 0;
  const walk = (node: ReportNode) => {
    for (const spec of node.specs ?? []) {
      for (const t of spec.tests ?? []) {
        tests += 1;
        for (const r of t.results ?? []) {
          for (const a of r.attachments ?? []) {
            if (a.name !== REGISTRATION_ATTACHMENT || typeof a.body !== "string") continue;
            records.push(JSON.parse(Buffer.from(a.body, "base64").toString("utf8")) as ScenarioRegistrations);
          }
        }
      }
    }
    for (const child of node.suites ?? []) walk(child);
  };
  walk(report);
  return { records, tests };
}

/** Прочитать бюджет из окружения: целое неотрицательное, иначе — отказ с причиной. */
export function budgetFromEnv(env: Record<string, string | undefined>): number {
  const raw = env.KACHO_REGISTRATION_BUDGET ?? "";
  if (!/^\d+$/.test(raw)) {
    throw new Error(
      `KACHO_REGISTRATION_BUDGET не объявлен целым числом («${raw}»): бюджет окна регистраций — вход ` +
        "сторожа, его ставит рецепт прогона; без него вердикта о бюджете нет",
    );
  }
  return Number(raw);
}

/** Вердикта нет — исход «не выполнилось» со своим кодом. */
export const NO_VERDICT = 3;

/** Исход сторожа по аргументам и окружению — код возврата. */
export function run(argv: string[], env: Record<string, string | undefined>): number {
  const files = argv.filter((a) => !a.startsWith("--"));
  try {
    const budget = budgetFromEnv(env);
    if (files.length === 0) throw new Error("не назван ни один отчёт прогона — вердикта о бюджете нет");
    const runs = files.map((file) => {
      let text: string;
      try {
        text = readFileSync(file, "utf8");
      } catch {
        throw new Error(`отчёта прогона нет: ${file} — сторож, не прочитавший прогона, зелёным не бывает`);
      }
      const { records, tests } = scenarioRegistrationsOf(JSON.parse(text) as ReportNode);
      if (tests === 0) throw new Error(`в отчёте ${file} нет ни одной пробы — вердикта о бюджете нет`);
      console.log(`[#2909] ${file}: проб в отчёте ${tests}, проб с регистрациями ${records.length}`);
      return records;
    });
    const verdict = judgeRegistrations(runs, budget);
    for (const line of verdict.report) console.log(line);
    return verdict.ok ? 0 : 1;
  } catch (e) {
    console.error(`[#2909] НЕ ВЫПОЛНИЛОСЬ: ${(e as Error).message}`);
    return NO_VERDICT;
  }
}

if (process.argv[1] && process.argv[1].endsWith("registration-budget.ts")) {
  process.exit(run(process.argv.slice(2), process.env));
}
