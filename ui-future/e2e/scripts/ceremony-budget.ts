// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

/**
 * Сторож бюджета оси ИСТОЧНИКА по отчёту прогона (приёмка F8, F8-41, ч. 1).
 *
 *   KACHO_CEREMONY_FAILURE_BUDGET=11 KACHO_ACCESS_KEY_SOURCE_BUDGET=20 \
 *     node scripts/ceremony-budget.ts results.json [results.json …]
 *
 * Каждая проба сдала свою запись отказов полосы вложением отчёта; сторож
 * складывает их и судит: каждый прогон ≤ бюджета, все названные вместе ≤
 * бюджета × их числа («трижды подряд» — три отчёта). Долей на счёте неверных
 * предъявлений две, и каждая судится своим бюджетом: F8-41 — отказы семи
 * глаголов (`KACHO_CEREMONY_FAILURE_BUDGET`), S4 — выдачи испытания и единые
 * отказы входа ключом (`KACHO_ACCESS_KEY_SOURCE_BUDGET`, приёмка F8-S4, Р8,
 * F8S4-15). Сумму долей против окна стенда судит проба развёртывания
 * (`deploy/console_suite_budgets_fit_the_stand_test.go`). Правила счёта —
 * `specs/ceremony-budget.ts`, один источник на запись и суд.
 *
 * Бюджет — ВХОД, а не умолчание: величину ставит тот же рецепт, что поднимает
 * стенд с профилем и гонит набор. Без неё вердикта нет, и это отказ, а не
 * «бюджет ноль». Отчёта нет — тоже отказ: сторож, не прочитавший прогона,
 * зелёным не бывает.
 *
 * Коды — ТРИ исхода, а не два (условие C21): 0 — в бюджете; 1 — бюджет
 * превышен (красное о прогоне); 3 — вердикта нет: величина не объявлена или
 * не разобрана, отчёта нет, в отчёте ни одной пробы. «Не выполнилось» не
 * сливается с «превышен» и не подменяется умолчанием величины.
 */

import { readFileSync } from "node:fs";
import {
  ACCESS_KEY_SHARE,
  BUDGET_ATTACHMENT,
  CEREMONY_SHARE,
  judgeBudget,
  type ScenarioRefusals,
} from "../specs/ceremony-budget.ts";

interface ReportAttachment {
  name?: string;
  body?: string;
}
interface ReportNode {
  suites?: ReportNode[];
  specs?: Array<{ tests?: Array<{ results?: Array<{ attachments?: ReportAttachment[] }> }> }>;
}

/** Записи проб из отчёта прогона playwright (`reporter: json`). */
export function scenarioRefusalsOf(report: ReportNode): { records: ScenarioRefusals[]; tests: number } {
  const records: ScenarioRefusals[] = [];
  let tests = 0;
  const walk = (node: ReportNode) => {
    for (const spec of node.specs ?? []) {
      for (const t of spec.tests ?? []) {
        tests += 1;
        for (const r of t.results ?? []) {
          for (const a of r.attachments ?? []) {
            if (a.name !== BUDGET_ATTACHMENT || typeof a.body !== "string") continue;
            records.push(JSON.parse(Buffer.from(a.body, "base64").toString("utf8")) as ScenarioRefusals);
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
export function budgetFromEnv(
  env: Record<string, string | undefined>,
  name: string = CEREMONY_SHARE.env,
): number {
  const raw = env[name] ?? "";
  if (!/^\d+$/.test(raw)) {
    throw new Error(
      `${name} не объявлен целым числом («${raw}»): бюджет доли оси источника — вход ` +
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
    const shares = [CEREMONY_SHARE, ACCESS_KEY_SHARE].map((share) => ({ share, budget: budgetFromEnv(env, share.env) }));
    if (files.length === 0) throw new Error("не назван ни один отчёт прогона — вердикта о бюджете нет");
    const runs = files.map((file) => {
      let text: string;
      try {
        text = readFileSync(file, "utf8");
      } catch {
        throw new Error(`отчёта прогона нет: ${file} — сторож, не прочитавший прогона, зелёным не бывает`);
      }
      const { records, tests } = scenarioRefusalsOf(JSON.parse(text) as ReportNode);
      if (tests === 0) throw new Error(`в отчёте ${file} нет ни одной пробы — вердикта о бюджете нет`);
      console.log(`[F8-41 · F8S4-15] ${file}: проб в отчёте ${tests}, записей отказов ${records.length}`);
      return records;
    });
    let ok = true;
    for (const { share, budget } of shares) {
      const verdict = judgeBudget(runs, budget, share);
      for (const line of verdict.report) console.log(line);
      ok &&= verdict.ok;
    }
    return ok ? 0 : 1;
  } catch (e) {
    console.error(`[F8-41 · F8S4-15] НЕ ВЫПОЛНИЛОСЬ: ${(e as Error).message}`);
    return NO_VERDICT;
  }
}

if (process.argv[1] && process.argv[1].endsWith("ceremony-budget.ts")) {
  process.exit(run(process.argv.slice(2), process.env));
}
