// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

/**
 * Самопроверка сторожа окна регистраций источника (kacho#2909).
 *
 * Сторож, неспособный покраснеть, отвечал бы «в бюджете» всегда. Здесь он
 * получает подсадку ВО ВХОД, который читает, — отчёт прогона playwright с
 * вложениями проб, — а не живые регистрации, поэтому окна стенда самопроверка
 * не тратит:
 *
 *   • пять регистраций при бюджете пять — в бюджете; шестая — красное с числом;
 *   • два прогона по пять — в бюджете: окно снимается временем, прогоны не
 *     складываются;
 *   • бюджета нет — «вердикта нет», а не «бюджет ноль»; отчёта нет и отчёт без
 *     проб — тоже «вердикта нет»;
 *   • в счёт идёт только `POST /iam/v1/auth/register`; записанное вне пробы не
 *     теряется.
 */

import { mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import {
  OUTSIDE_TEST,
  REGISTRATION_ATTACHMENT,
  isRegistration,
  judgeRegistrations,
  noteRegistration,
  takeRegistrations,
} from "../specs/registration-budget.ts";
import { NO_VERDICT, budgetFromEnv, run as guard, scenarioRegistrationsOf } from "./registration-budget.ts";

let failed = 0;
let cases = 0;
function check(ok: boolean, what: string): void {
  cases += 1;
  console.log(`${ok ? "  ok  " : "  ПРОВАЛ"} ${what}`);
  if (!ok) failed += 1;
}

/** Отчёт playwright (`reporter: json`) с записями проб — та форма, что пишет фикстура. */
function report(records: Array<[string, number]>, extraTests = 0): string {
  const tests = records.map(([scenario, registrations]) => ({
    results: [
      {
        attachments:
          registrations > 0
            ? [
                {
                  name: REGISTRATION_ATTACHMENT,
                  contentType: "application/json",
                  body: Buffer.from(JSON.stringify({ scenario, registrations })).toString("base64"),
                },
              ]
            : [],
      },
    ],
  }));
  for (let i = 0; i < extraTests; i++) tests.push({ results: [{ attachments: [] }] });
  return JSON.stringify({ suites: [{ suites: [{ specs: tests.map((t) => ({ tests: [t] })) }] }] });
}

const dir = mkdtempSync(path.join(tmpdir(), "kacho-2909-"));
const write = (name: string, text: string) => {
  const p = path.join(dir, name);
  writeFileSync(p, text);
  return p;
};

const five = write("five.json", report([["регистрация экраном", 3], ["посев человека", 2]], 4));
const six = write("six.json", report([["регистрация экраном", 4], ["посев человека", 2]], 4));
const empty = write("empty.json", JSON.stringify({ suites: [] }));

console.log("ЗАПИСЬ — что идёт в счёт");
check(isRegistration("POST", "/iam/v1/auth/register"), "POST регистрации — в счёт");
check(isRegistration("post", "/iam/v1/auth/register"), "метод сравнивается без учёта регистра");
check(!isRegistration("GET", "/iam/v1/auth/register"), "GET того же пути — не регистрация");
check(!isRegistration("POST", "/iam/v1/auth/login"), "вход — не регистрация");
noteRegistration("t1");
noteRegistration(OUTSIDE_TEST);
noteRegistration("t1");
check(takeRegistrations("t1") === 3, "записанное вне пробы отдаётся следующей пробе, а не теряется");
check(takeRegistrations("t1") === 0, "счёт пробы забирается ровно один раз");

console.log("РАЗБОР ОТЧЁТА");
{
  const { records, tests } = scenarioRegistrationsOf(JSON.parse(report([["a", 2], ["b", 0]], 1)));
  check(tests === 3 && records.length === 1 && records[0].registrations === 2,
    `проб 3, записей 1 (проба без регистраций вложения не сдаёт): получено проб ${tests}, записей ${records.length}`);
}

console.log("СУД — инъекция и законный близнец");
check(guard([five], { KACHO_REGISTRATION_BUDGET: "5" }) === 0, "близнец: 5 регистраций при бюджете 5 — код 0");
check(guard([six], { KACHO_REGISTRATION_BUDGET: "5" }) === 1, "инъекция: 6 регистраций при бюджете 5 — код 1");
check(guard([five, five], { KACHO_REGISTRATION_BUDGET: "5" }) === 0, "два прогона по 5 при бюджете 5 — код 0, не складываются");
check(guard([five, six], { KACHO_REGISTRATION_BUDGET: "5" }) === 1, "один из двух прогонов превысил — код 1");
{
  const v = judgeRegistrations([[{ scenario: "x", registrations: 6 }]], 5);
  check(!v.ok && v.report.some((l) => l.includes("6 регистраций при бюджете 5")), "красное называет число и бюджет");
}

console.log("ВЕРДИКТА НЕТ — отдельный исход");
check(guard([five], {}) === NO_VERDICT, "бюджет не объявлен — код 3, а не «бюджет ноль»");
check(guard([five], { KACHO_REGISTRATION_BUDGET: "пять" }) === NO_VERDICT, "бюджет не число — код 3");
check(guard([], { KACHO_REGISTRATION_BUDGET: "5" }) === NO_VERDICT, "отчёт не назван — код 3");
check(guard([path.join(dir, "нет.json")], { KACHO_REGISTRATION_BUDGET: "5" }) === NO_VERDICT, "отчёта нет — код 3");
check(guard([empty], { KACHO_REGISTRATION_BUDGET: "5" }) === NO_VERDICT, "в отчёте ни одной пробы — код 3");
{
  let threw = false;
  try {
    budgetFromEnv({ KACHO_REGISTRATION_BUDGET: "-1" });
  } catch {
    threw = true;
  }
  check(threw, "отрицательный бюджет — отказ");
}

console.log(`\nслучаев: ${cases}; провалено: ${failed}`);
process.exit(failed === 0 && cases > 0 ? 0 : 1);
