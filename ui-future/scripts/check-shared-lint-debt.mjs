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
import { ESLint } from "eslint";

const uiRoot = process.cwd();
const sharedDir = path.join(uiRoot, "shared");
const debtPath = path.join(sharedDir, "eslint-debt.json");
const write = process.argv.includes("--write");

if (!fs.existsSync(path.join(sharedDir, "eslint.config.js"))) {
  console.error("::error::shared/eslint.config.js отсутствует — линт пакета shared снят, а не сокращён");
  process.exit(1);
}

const eslint = new ESLint({ cwd: sharedDir });
const results = await eslint.lintFiles(["."]);

const actual = {};
let files = 0;
let problems = 0;
for (const r of results) {
  files += 1;
  for (const m of r.messages) {
    const rule = m.ruleId ?? "(fatal)";
    actual[rule] = (actual[rule] ?? 0) + 1;
    problems += 1;
  }
}

console.log(`осмотрено: файлов ${files}, находок ${problems}, задетых правил ${Object.keys(actual).length}`);

if (write) {
  const sorted = Object.fromEntries(Object.entries(actual).sort(([a], [b]) => a.localeCompare(b)));
  fs.writeFileSync(
    debtPath,
    `${JSON.stringify(
      {
        __: "Долг линта пакета shared: находки, существовавшие в момент включения линта. Числу запрещено расти; при исправлении — уменьшить в том же коммите. Пустой rules ⇒ удалить этот файл и scripts/check-shared-lint-debt.mjs, заменив вызов на eslint без храповика. Правила гейта — в шапке scripts/check-shared-lint-debt.mjs.",
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
  if (allowed === undefined) findings.push(`НОВОЕ правило нарушено: ${rule} — ${n} шт. (в долге его нет)`);
  else if (n > allowed) findings.push(`РОСТ по ${rule}: было ${allowed}, стало ${n}`);
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
