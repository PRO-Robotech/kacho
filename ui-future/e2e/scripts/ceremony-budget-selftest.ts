// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

/**
 * Самопроверка сторожа бюджета оси источника (приёмка F8, F8-41).
 *
 * Сторож, неспособный покраснеть, отвечал бы «в бюджете» всегда. Здесь он
 * получает подсадку ВО ВХОД, который читает, — запись отказов в отчёте, — а не
 * живое обращение, поэтому бюджета стенда самопроверка не тратит:
 *
 *   • одиннадцать отказов в счёт — в бюджете; двенадцатый, на пути СНЯТИЯ
 *     второго фактора, — красное с числом и с именем сценария;
 *   • тот же отказ на пути глагола вне семи (регистрация) — в счёт не идёт;
 *   • отказ края с тем же кодом и другим текстом — в счёт не идёт;
 *   • три прогона: 33 — в бюджете, 34 — красное;
 *   • бюджета нет — отказ, а не «бюджет ноль»; отчёт без проб — отказ.
 */

import { judgeBudget, type RecordedRefusal, type ScenarioRefusals } from "../specs/ceremony-budget.ts";
import { budgetFromEnv, scenarioRefusalsOf } from "./ceremony-budget.ts";

let failed = 0;
function check(ok: boolean, what: string): void {
  console.log(`${ok ? "  ok  " : "  ПРОВАЛ"} ${what}`);
  if (!ok) failed += 1;
}

const failedLogin: RecordedRefusal = {
  path: "/iam/v1/auth/login",
  status: 401,
  code: 16,
  message: "authentication failed",
};
const run = (...xs: Array<[string, RecordedRefusal[]]>): ScenarioRefusals[] =>
  xs.map(([scenario, refusals]) => ({ scenario, refusals }));
const eleven = run(
  ["F8-05 · неверный пароль", [failedLogin]],
  ["F8-09 · потолок темпа", Array(5).fill(failedLogin)],
  ["F8-23 · смена пароля", [failedLogin]],
  ["F8-24 · текущий пароль неверен", [{ ...failedLogin, path: "/iam/v1/auth/password" }]],
  ["F8-28 · неверный первый код", [{ ...failedLogin, path: "/iam/v1/auth/second-factor/confirm" }]],
  ["F8-34 · неверный код второго фактора", [failedLogin]],
  ["F8-40 · адрес не заведён", [failedLogin]],
);

console.log("ПРОГОН 1 — одиннадцать отказов в счёт укладываются в бюджет");
{
  const v = judgeBudget([eleven], 11);
  check(v.ok && v.total === 11, `в бюджете: всего ${v.total}`);
}

console.log("ПРОГОН 2 — подсаженный отказ на пути снятия второго фактора краснит с числом и именем");
{
  const planted = [
    ...eleven,
    ...run(["F8-41 · подсадка", [{ ...failedLogin, path: "/iam/v1/auth/second-factor/remove" }]]),
  ];
  const v = judgeBudget([planted], 11);
  check(!v.ok, "бюджет превышен");
  check(
    v.report.some((l) => l.includes("потратил 12")),
    "названо число — 12",
  );
  check(
    v.report.some((l) => l.includes("F8-41 · подсадка")),
    "назван сценарий, потративший ось",
  );
}

console.log("ПРОГОН 3 — отказ вне семи глаголов и отказ края в счёт не идут");
{
  const twin = [
    ...eleven,
    ...run(
      ["регистрация", [{ ...failedLogin, path: "/iam/v1/auth/register" }]],
      ["край", [{ ...failedLogin, path: "/iam/v1/auth/password", message: "session ended; sign in again" }]],
    ),
  ];
  const v = judgeBudget([twin], 11);
  check(v.ok && v.total === 11, `в бюджете: всего ${v.total} — подсадки вне правила не сосчитаны`);
}

console.log("ПРОГОН 4 — три прогона подряд: 33 в бюджете, 34 — нет");
{
  check(judgeBudget([eleven, eleven, eleven], 11).ok, "33 при бюджете 33");
  const heavy = [...eleven, ...run(["лишний", [failedLogin]])];
  check(!judgeBudget([eleven, eleven, heavy], 11).ok, "34 — красное (и прогон 3 сверх своего)");
}

console.log("ПРОГОН 5 — без величины и без прогона вердикта нет");
{
  let refused = "";
  try {
    budgetFromEnv({});
  } catch (e) {
    refused = (e as Error).message;
  }
  check(refused.includes("KACHO_CEREMONY_FAILURE_BUDGET"), "бюджет не объявлен — отказ, а не ноль");
  let empty = "";
  try {
    judgeBudget([], 11);
  } catch (e) {
    empty = (e as Error).message;
  }
  check(empty !== "", "ни одного прогона — отказ");
}

console.log("ПРОГОН 6 — запись читается из вложения отчёта прогона");
{
  const body = Buffer.from(JSON.stringify({ scenario: "F8-05 · неверный пароль", refusals: [failedLogin] })).toString(
    "base64",
  );
  const report = {
    suites: [
      {
        specs: [
          {
            tests: [{ results: [{ attachments: [{ name: "f8-41-source-axis-refusals", body }, { name: "trace" }] }] }],
          },
          { tests: [{ results: [{ attachments: [] }] }] },
        ],
      },
    ],
  };
  const { records, tests } = scenarioRefusalsOf(report);
  check(tests === 2, `проб в отчёте ${tests}`);
  check(records.length === 1 && records[0].scenario === "F8-05 · неверный пароль", "запись пробы прочитана");
}

if (failed > 0) {
  console.error(`\nсамопроверка сторожа бюджета оси источника: провалов ${failed}`);
  process.exit(1);
}
console.log("\nсамопроверка сторожа бюджета оси источника: все утверждения прошли (прогонов 6)");
