#!/usr/bin/env node
// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

/**
 * Храповик долга линта пакета `shared`.
 *
 * ЗАЧЕМ. Пакет `shared` до сих пор не линтовался ничем: базовый путь ESLint — каталог
 * его конфигурации, поэтому `eslint .` из любого из девяти приложений отвергал ../shared
 * целиком. Включение линта на непроверявшийся пакет обнажило нарушения, которые копились
 * всё это время. Часть закрыта сразу; остаток зафиксирован ПОИМЁННО в eslint-debt.json —
 * не как «список известных красных, который можно не смотреть», а как величина, которой
 * запрещено расти.
 *
 * ЧТО ЭТО ЗА ГЕЙТ (он способен упасть, и падает по трём разным поводам):
 *   • правило, которого в долге нет, дало находку           → НОВОЕ нарушение;
 *   • число по правилу выросло                              → регресс;
 *   • число по правилу УМЕНЬШИЛОСЬ или правило исчезло       → долг записан крупнее, чем есть.
 * Третий повод — не придирка: без него запись переживает свой предмет и остаётся вечно
 * (`testing.md` §«Исключение живёт, пока у него есть предмет»). Чинишь — уменьшаешь число
 * в том же коммите.
 *
 * ПРЕДИКАТ СНЯТИЯ. Когда `rules` в eslint-debt.json станет пустым, гейт требует НУЛЯ
 * находок и сам сообщает, что файл долга и этот скрипт пора удалить, заменив вызов на
 * обычный `npm run lint:js --prefix shared`. До тех пор послабление не бессрочное:
 * у него есть измеримое условие конца.
 *
 * ГРАНИЦА ТОЧНОСТИ, названная честно. Долг ключуется правилом и числом, а НЕ координатой
 * файл:строка — иначе любое перемещение строки краснило бы гейт. Следствие: замена одного
 * нарушения правила X на другое нарушение того же правила X в другом месте остаётся
 * незамеченной. Гейт ловит РОСТ и ПОЯВЛЕНИЕ класса, а не тождество экземпляров.
 *
 * Запуск из ui-future/:  node scripts/check-shared-lint-debt.mjs
 *   --write   перезаписать eslint-debt.json текущим замером (для осознанного пересчёта)
 */

import fs from "node:fs";
import path from "node:path";
import { createRequire } from "node:module";
import { pathToFileURL } from "node:url";

const uiRoot = process.cwd();
const sharedDir = path.join(uiRoot, "shared");
const debtPath = path.join(sharedDir, "eslint-debt.json");
const write = process.argv.includes("--write");

if (!fs.existsSync(path.join(sharedDir, "eslint.config.js"))) {
  console.error("::error::shared/eslint.config.js отсутствует — линт пакета shared снят, а не сокращён");
  process.exit(1);
}

// ESLint берётся ОТ КАТАЛОГА shared, а не подъёмом от scripts/ (#572). Успех этого
// гейта прямо указывает на `npm run lint:js --prefix shared` как на своего преемника,
// поэтому судить он обязан ТЕМ ЖЕ инструментом, который эта команда запускает: npm
// ставит в PATH сперва `shared/node_modules/.bin`, и вложенная копия выигрывает у
// корневой. Пока здесь стоял корневой экземпляр, гейт был зелён на дереве, где
// объявленная команда падала на загрузке правила, — то есть предикат снятия долга вёл
// к команде, которая не запускалась. Порядок поиска тот же, что у
// scripts/check-lint-coverage.mjs и scripts/check-eslint-shim-still-needed.mjs.
const eslintEntry = createRequire(path.join(sharedDir, "package.json")).resolve("eslint");
const eslintMod = await import(pathToFileURL(eslintEntry).href);
const ESLint = eslintMod.ESLint ?? eslintMod.default?.ESLint;
if (typeof ESLint !== "function") {
  console.error(`::error::предпосылка гейта не выполнена: в ${eslintEntry} нет класса ESLint — судить нечем`);
  process.exit(1);
}
const eslint = new ESLint({ cwd: sharedDir });
let results;
try {
  results = await eslint.lintFiles(["."]);
} catch (e) {
  // Отказ ЗАПУСКА — не «ноль находок»: команда неисполнима, и молчание здесь
  // читалось бы как чистый долг.
  console.error(
    `::error::объявленная команда линта shared НЕИСПОЛНИМА собственным ESLint пакета ` +
      `(${eslintEntry}): «${String(e?.message ?? e).split("\n")[0]}» — долг не измерен`,
  );
  process.exit(1);
}

const actual = {};
// КООРДИНАТЫ СОХРАНЯЮТСЯ, А НЕ ВЫБРАСЫВАЮТСЯ (#643).
//
// Прежняя редакция считала находки по правилу и теряла путь со строкой, хотя
// линтер отдаёт их в том же сообщении. Читатель, увидев «правило X — 2 шт.»,
// обязан был воспроизвести прогон, чтобы узнать МЕСТО, — а воспроизвести его
// не так просто, как кажется: линтер надо звать той же версией и из того же
// каталога, что и гейт, иначе вердикт будет о другом дереве.
//
// Держим до трёх координат на правило. Три, а не все: перечень существует,
// чтобы отправить читателя в нужный файл, а не чтобы заменить собой вывод
// линтера; при двадцати находках полный список нечитаем и его перестанут
// читать целиком — вместе с первой строкой, которая и нужна.
const where = {};
let files = 0;
let problems = 0;
for (const r of results) {
  files += 1;
  for (const m of r.messages) {
    const rule = m.ruleId ?? "(fatal)";
    actual[rule] = (actual[rule] ?? 0) + 1;
    (where[rule] ??= []).push(`${path.relative(uiRoot, r.filePath)}:${m.line}:${m.column}`);
    problems += 1;
  }
}

/** Первые координаты правила — то, с чего начинать разбор. */
function coords(rule) {
  const all = where[rule] ?? [];
  if (all.length === 0) return "";
  const head = all.slice(0, 3).join(", ");
  return all.length > 3 ? ` — ${head} и ещё ${all.length - 3}` : ` — ${head}`;
}

console.log(`осмотрено: файлов ${files}, находок ${problems}, задетых правил ${Object.keys(actual).length}`);

if (write) {
  const sorted = Object.fromEntries(Object.entries(actual).sort(([a], [b]) => a.localeCompare(b)));
  fs.writeFileSync(
    debtPath,
    `${JSON.stringify(
      {
        __: "Долг линта пакета shared: находки, оставшиеся НЕ ЗАКРЫТЫМИ на момент последнего пересчёта (не «те, что были при включении линта» — часть их с тех пор исправлена, и число уменьшено в том же коммите). Числу запрещено расти; при исправлении — уменьшить в том же коммите. Пустой rules ⇒ удалить этот файл и scripts/check-shared-lint-debt.mjs, заменив вызов на eslint без храповика. Правила гейта — в шапке scripts/check-shared-lint-debt.mjs.",
        rules: sorted,
      },
      null,
      2,
    )}\n`,
  );
  console.log(`записан ${path.relative(uiRoot, debtPath)}`);
  process.exit(0);
}

if (!fs.existsSync(debtPath)) {
  console.error(`::error::нет ${path.relative(uiRoot, debtPath)} — сравнивать не с чем`);
  process.exit(1);
}
const debt = JSON.parse(fs.readFileSync(debtPath, "utf8")).rules ?? {};

const findings = [];
for (const [rule, n] of Object.entries(actual)) {
  const allowed = debt[rule];
  if (allowed === undefined) findings.push(`НОВОЕ правило нарушено: ${rule} — ${n} шт. (в долге его нет)${coords(rule)}`);
  else if (n > allowed) findings.push(`РОСТ по ${rule}: было ${allowed}, стало ${n}${coords(rule)}`);
}
for (const [rule, allowed] of Object.entries(debt)) {
  const n = actual[rule] ?? 0;
  if (n < allowed) {
    findings.push(
      `долг по ${rule} записан крупнее предмета: в долге ${allowed}, реально ${n} — ` +
        `${n === 0 ? "удалить запись" : `поставить ${n}`} в shared/eslint-debt.json`,
    );
  }
}

const total = Object.values(debt).reduce((a, b) => a + b, 0);
console.log(`долг: правил ${Object.keys(debt).length}, находок ${total}`);

if (findings.length) {
  for (const f of findings) console.error(`::error::${f}`);
  console.error(`\n✖ находок: ${findings.length}`);
  process.exit(1);
}

if (Object.keys(debt).length === 0) {
  console.log(
    "✓ долг пуст — удалить shared/eslint-debt.json и этот скрипт, заменив вызов на `npm run lint:js --prefix shared`",
  );
} else {
  console.log("✓ линт shared не даёт находок сверх зафиксированного долга");
}
