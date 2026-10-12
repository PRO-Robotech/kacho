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
 *   • отказ края в удостоверении (приёмка KA1, Р2) — в счёт не идёт, хотя его
 *     код и текст те же, что у отказа службы: различает их причина
 *     `AUTHN_REQUIRED`, которую несёт только край, — и запись снимает её с тела
 *     ответа;
 *   • каждый текст отказа службы (вход с полем `secondFactor`, завершение
 *     восстановления) — в счёт; тот же текст вне семи глаголов и чужой текст
 *     того же кода — нет;
 *   • три прогона: 33 — в бюджете, 34 — красное;
 *   • бюджета нет — отказ, а не «бюджет ноль»; отчёт без проб — отказ.
 *
 * И для ДОЛИ S4 (приёмка F8-S4, Р8, F8S4-15) — своей ручкой
 * `KACHO_ACCESS_KEY_SOURCE_BUDGET` на том же счёте неверных предъявлений:
 *   • выдача испытания входа ключом (`begin` → `200`) и единый отказ входа
 *     ключом (`401` · 16 · `authentication failed` без причины края) — в долю;
 *   • признак формы, успешный вход, отказы `400`/`403`/`429` и отказ края — нет;
 *   • выдача сверх бюджета доли — красное с числом и с именем сценария;
 *     близнец той же записи на пути признака формы — не красное;
 *   • доля S4 к доле F8-41 не прибавляет ни одного отказа;
 *   • без величины доли S4 вердикта нет — код 3, а не «бюджет ноль».
 */

import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { EDGE_AUTHN_FAILED } from "../../shared/src/test/edge-answers.ts";
import {
  ACCESS_KEY_BEGIN,
  ACCESS_KEY_LOGIN,
  ACCESS_KEY_SHARE,
  countsTowardAccessKeyShare,
  countsTowardSourceAxis,
  judgeBudget,
  recordableAttempt,
  recordableRefusal,
  type RecordedRefusal,
  type ScenarioRefusals,
} from "../specs/ceremony-budget.ts";
import { ACCESS_NOT_RESTORED, LOGIN_WITH_SECOND_FACTOR_FAILED } from "../specs/lane-texts.ts";
import { NO_VERDICT, budgetFromEnv, run as guard, scenarioRefusalsOf } from "./ceremony-budget.ts";

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
  ["F8-34 · неверный код второго фактора", [{ ...failedLogin, message: LOGIN_WITH_SECOND_FACTOR_FAILED }]],
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
  // Отказ края (KA1, Р2) записан так, как его записывает фиксация ответа, — из
  // тела производителя; код и текст у него те же, что у отказа службы.
  const edge = recordableRefusal("/iam/v1/auth/password", EDGE_AUTHN_FAILED.status, EDGE_AUTHN_FAILED.text);
  check(
    edge !== null && edge.code === failedLogin.code && edge.message === failedLogin.message,
    "отказ края записан с тем же кодом и текстом, что отказ службы",
  );
  const twin = [
    ...eleven,
    ...run(["регистрация", [{ ...failedLogin, path: "/iam/v1/auth/register" }]], ["край", edge ? [edge] : []]),
  ];
  const v = judgeBudget([twin], 11);
  check(v.ok && v.total === 11, `в бюджете: всего ${v.total} — подсадки вне правила не сосчитаны`);
}

console.log("ПРОГОН 3а — запись различает отказ службы и отказ края по причине тела");
{
  const service = recordableRefusal(
    "/iam/v1/auth/login",
    401,
    '{"code":16,"message":"authentication failed","details":[]}',
  );
  const edge = recordableRefusal("/iam/v1/auth/login", EDGE_AUTHN_FAILED.status, EDGE_AUTHN_FAILED.text);
  check(service !== null && countsTowardSourceAxis(service), "отказ службы (`details` пуст) — в счёт");
  check(edge !== null && !countsTowardSourceAxis(edge), "отказ края (`AUTHN_REQUIRED`) — не в счёт");
  // Неизвестная причина у того же кода и текста — в счёт: сторона ошибки та же,
  // что у всего сторожа, — счёт не меньше истинного.
  const unknown = recordableRefusal(
    "/iam/v1/auth/login",
    401,
    JSON.stringify({
      code: 16,
      message: "authentication failed",
      details: [{ "@type": "type.googleapis.com/google.rpc.ErrorInfo", reason: "SOMETHING_NEW", domain: "x" }],
    }),
  );
  check(unknown !== null && countsTowardSourceAxis(unknown), "неизвестная причина — в счёт");
}

console.log("ПРОГОН 3б — каждый текст отказа службы на своём глаголе идёт в счёт");
{
  // Тексты службы различают ФОРМУ запроса и глагол, а не причину: вход с полем
  // `secondFactor` и завершение восстановления отвечают своим текстом, и след по
  // оси источника они пишут так же, как вход без поля. Сторож, узнающий один
  // текст, недосчитал бы их молча — тело записывается так, как его отдаёт служба.
  const bodyOf = (message: string) => JSON.stringify({ code: 16, message, details: [] });
  const withFactor = recordableRefusal("/iam/v1/auth/login", 401, bodyOf(LOGIN_WITH_SECOND_FACTOR_FAILED));
  const notRestored = recordableRefusal("/iam/v1/auth/recovery/complete", 401, bodyOf(ACCESS_NOT_RESTORED));
  check(withFactor !== null && countsTowardSourceAxis(withFactor), "отказ входа с полем secondFactor — в счёт");
  check(notRestored !== null && countsTowardSourceAxis(notRestored), "отказ завершения восстановления — в счёт");
  // Законный близнец: тот же текст на глаголе вне семи в счёт не идёт — правило
  // пути не ослаблено расширением текстов.
  const offVerb = recordableRefusal("/iam/v1/auth/register", 401, bodyOf(LOGIN_WITH_SECOND_FACTOR_FAILED));
  check(offVerb !== null && !countsTowardSourceAxis(offVerb), "тот же текст вне семи глаголов — не в счёт");
  // И близнец по тексту: отказ того же кода с текстом, которого служба на этих
  // путях не пишет, в счёт не идёт — перечень текстов закрыт.
  const foreign = recordableRefusal("/iam/v1/auth/login", 401, bodyOf("some other refusal"));
  check(foreign !== null && !countsTowardSourceAxis(foreign), "чужой текст того же кода — не в счёт");
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

console.log("ПРОГОН 7 — три исхода сторожа разными кодами: в бюджете, превышен, вердикта нет");
{
  const dir = mkdtempSync(path.join(tmpdir(), "f8-41-"));
  const reportOf = (refusals: RecordedRefusal[]) => {
    const body = Buffer.from(JSON.stringify({ scenario: "F8-09 · потолок темпа", refusals })).toString("base64");
    return JSON.stringify({
      suites: [
        { specs: [{ tests: [{ results: [{ attachments: [{ name: "f8-41-source-axis-refusals", body }] }] }] }] },
      ],
    });
  };
  // Обе ручки объявлены: величина доли S4 (F8S4-15) — вход того же сторожа.
  const BOTH = { KACHO_CEREMONY_FAILURE_BUDGET: "11", KACHO_ACCESS_KEY_SOURCE_BUDGET: "20" };
  const within = path.join(dir, "within.json");
  const over = path.join(dir, "over.json");
  writeFileSync(within, reportOf(Array(5).fill(failedLogin)));
  writeFileSync(over, reportOf(Array(12).fill(failedLogin)));
  check(guard([within], BOTH) === 0, "в бюджете — 0");
  check(guard([over], BOTH) === 1, "превышен — 1");
  // Величина не объявлена или не разобрана — не «превышен» и не умолчание 11.
  check(guard([within], { KACHO_ACCESS_KEY_SOURCE_BUDGET: "20" }) === NO_VERDICT, "величины нет — вердикта нет (3), а не умолчание");
  check(guard([within], { ...BOTH, KACHO_CEREMONY_FAILURE_BUDGET: "одиннадцать" }) === NO_VERDICT, "величина не число — 3");
  check(guard([path.join(dir, "нет.json")], BOTH) === NO_VERDICT, "отчёта нет — 3");
  check(guard([], BOTH) === NO_VERDICT, "отчёт не назван — 3");
}

console.log("ПРОГОН 8 — F8S4-15: что идёт в долю S4, записанное так, как пишет фиксация ответа");
{
  const service = JSON.stringify({ code: 16, message: "authentication failed", details: [] });
  const begun = recordableAttempt(ACCESS_KEY_BEGIN, 200, '{"publicKey":{}}');
  const refused = recordableAttempt(ACCESS_KEY_LOGIN, 401, service);
  check(begun !== null && countsTowardAccessKeyShare(begun), "F8S4-15 · выдача испытания begin → 200 записана и идёт в долю S4");
  check(refused !== null && countsTowardAccessKeyShare(refused), "F8S4-15 · единый отказ входа ключом 401 · 16 — в долю S4");
  check(
    begun !== null && refused !== null && !countsTowardSourceAxis(begun) && !countsTowardSourceAxis(refused),
    "F8S4-15 · обе записи доли S4 к доле F8-41 не прибавляют",
  );
  // Отказ края несёт те же код и текст: различает причина тела.
  const edge = recordableAttempt(ACCESS_KEY_LOGIN, EDGE_AUTHN_FAILED.status, EDGE_AUTHN_FAILED.text);
  check(edge !== null && !countsTowardAccessKeyShare(edge), "F8S4-15 · отказ края (AUTHN_REQUIRED) на входе ключом — не в долю");
  // Не попытки (Ф13 Р9): признак формы, успешный вход, отказы 400 · 403 · 429.
  check(recordableAttempt("/iam/v1/auth/csrf", 200, '{"csrfToken":"x"}') === null, "F8S4-15 · признак формы не записывается");
  check(recordableAttempt(ACCESS_KEY_LOGIN, 200, "{}") === null, "F8S4-15 · успешный вход ключом не записывается");
  for (const status of [400, 403, 429]) {
    for (const p of [ACCESS_KEY_BEGIN, ACCESS_KEY_LOGIN]) {
      const r = recordableAttempt(p, status, JSON.stringify({ code: 3, message: "x", details: [] }));
      check(r === null || !countsTowardAccessKeyShare(r), `F8S4-15 · отказ ${status} на ${p} — не в долю`);
    }
  }
  // Запись прежней формы (только `401`) не теряет ни одного отказа F8-41.
  const old = recordableAttempt("/iam/v1/auth/login", 401, service);
  check(old !== null && countsTowardSourceAxis(old), "F8S4-15 · отказ входа паролем записывается и идёт в долю F8-41, как прежде");
}

// Доля S4 по сценариям — записи, какими их сдаёт прогон: выдачи испытания и
// единые отказы входа ключом. Число — бюджет шага работы.
const S4_BUDGET = 20;
const begin: RecordedRefusal = { path: ACCESS_KEY_BEGIN, status: 200, code: null, message: "", reason: null };
const keyRefused: RecordedRefusal = {
  path: ACCESS_KEY_LOGIN,
  status: 401,
  code: 16,
  message: "authentication failed",
  reason: null,
};
const s4 = run(
  ["F8S4-02 · вход ключом", [begin]],
  ["F8S4-03 · отказ браузера", [begin]],
  ["F8S4-04 · снятый ключ", [begin, keyRefused]],
  ["F7-07-console · чужая подпись", [begin, keyRefused]],
  ["F8S4-05 · две попытки", [begin, keyRefused, begin, keyRefused]],
  ["F8S4-09 · четыре адреса возврата", Array(4).fill(begin)],
  ["F8S4-10 · второй фактор", [begin]],
  ["F8S4-12 · клавиатура", [begin]],
  ["F8S4-02 · путь ключа сквозь консоль", [begin, begin, keyRefused]],
  ["F8S4-03 · путь ключа без проверки пользователя", [begin]],
);

console.log("ПРОГОН 9 — F8S4-15: доля S4 в бюджете; лишняя выдача испытания — красное с числом и именем");
{
  const v = judgeBudget([s4], S4_BUDGET, ACCESS_KEY_SHARE);
  check(v.ok && v.total === S4_BUDGET, `F8S4-15 · доля S4 ${v.total} при бюджете ${S4_BUDGET} — в бюджете`);
  check(v.report[0].startsWith("[F8S4-15]"), "F8S4-15 · отчёт доли S4 назван своим сценарием");
  check(
    v.report.some((l) => l.includes("F8S4-05 · две попытки")),
    "F8S4-15 · доля S4 напечатана по сценариям",
  );
  const planted = [...s4, ...run(["F8S4-15 · подсадка", [begin]])];
  const over = judgeBudget([planted], S4_BUDGET, ACCESS_KEY_SHARE);
  check(!over.ok, `F8S4-15 · выдача испытания сверх бюджета — красное`);
  check(over.report.some((l) => l.includes(`потратил ${S4_BUDGET + 1}`)), `F8S4-15 · названо число — ${S4_BUDGET + 1}`);
  check(over.report.some((l) => l.includes("F8S4-15 · подсадка")), "F8S4-15 · назван сценарий, потративший долю");
  // Положительный близнец: та же запись на пути признака формы — не попытка.
  const twin = [...s4, ...run(["F8S4-15 · признак формы", [{ ...begin, path: "/iam/v1/auth/csrf" }]])];
  check(judgeBudget([twin], S4_BUDGET, ACCESS_KEY_SHARE).ok, "F8S4-15 · та же запись на пути признака формы — не красное");
}

console.log("ПРОГОН 10 — F8S4-15: доля S4 к доле F8-41 не прибавляет; три прогона подряд");
{
  const both = [...eleven, ...s4];
  check(judgeBudget([both], 11).total === 11, "F8S4-15 · доля F8-41 с записями S4 — та же 11");
  check(judgeBudget([both], S4_BUDGET, ACCESS_KEY_SHARE).total === S4_BUDGET, "F8S4-15 · доля S4 с записями F8-41 — та же");
  check(judgeBudget([s4, s4, s4], S4_BUDGET, ACCESS_KEY_SHARE).ok, `F8S4-15 · три прогона — ${3 * S4_BUDGET} в бюджете`);
  const heavy = [...s4, ...run(["лишняя выдача", [begin]])];
  check(!judgeBudget([s4, s4, heavy], S4_BUDGET, ACCESS_KEY_SHARE).ok, `F8S4-15 · ${3 * S4_BUDGET + 1} — красное`);
}

console.log("ПРОГОН 11 — F8S4-15: сторож судит обе доли; без величины доли S4 вердикта нет");
{
  const dir = mkdtempSync(path.join(tmpdir(), "f8s4-15-"));
  const reportOf = (records: ScenarioRefusals[]) =>
    JSON.stringify({
      suites: [
        {
          specs: records.map((r) => ({
            tests: [
              {
                results: [
                  {
                    attachments: [
                      { name: "f8-41-source-axis-refusals", body: Buffer.from(JSON.stringify(r)).toString("base64") },
                    ],
                  },
                ],
              },
            ],
          })),
        },
      ],
    });
  const within = path.join(dir, "within.json");
  const over = path.join(dir, "over.json");
  writeFileSync(within, reportOf([...eleven, ...s4]));
  writeFileSync(over, reportOf([...eleven, ...s4, ...run(["F8S4-15 · подсадка", [begin]])]));
  const env = { KACHO_CEREMONY_FAILURE_BUDGET: "11", KACHO_ACCESS_KEY_SOURCE_BUDGET: String(S4_BUDGET) };
  check(guard([within], env) === 0, "F8S4-15 · обе доли в бюджете — 0");
  check(guard([over], env) === 1, "F8S4-15 · доля S4 превышена, доля F8-41 в бюджете — 1");
  check(guard([within], { KACHO_CEREMONY_FAILURE_BUDGET: "11" }) === NO_VERDICT, "F8S4-15 · величины доли S4 нет — 3, а не «бюджет ноль»");
  check(
    guard([within], { ...env, KACHO_ACCESS_KEY_SOURCE_BUDGET: "двадцать" }) === NO_VERDICT,
    "F8S4-15 · величина доли S4 не число — 3",
  );
  let refused = "";
  try {
    budgetFromEnv({ KACHO_CEREMONY_FAILURE_BUDGET: "11" }, ACCESS_KEY_SHARE.env);
  } catch (e) {
    refused = (e as Error).message;
  }
  check(refused.includes("KACHO_ACCESS_KEY_SOURCE_BUDGET"), "F8S4-15 · отказ называет ручку доли S4");
  rmSync(dir, { recursive: true, force: true });
}

if (failed > 0) {
  console.error(`\nсамопроверка сторожа бюджета оси источника: провалов ${failed}`);
  process.exit(1);
}
console.log("\nсамопроверка сторожа бюджета оси источника: все утверждения прошли (прогонов 13)");
