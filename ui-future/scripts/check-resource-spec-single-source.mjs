#!/usr/bin/env node
// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

/**
 * Гейт единственного источника ФОРМЫ ресурса: типы, которыми консоль описывает любой
 * ресурс, объявлены ровно в одном модуле каждый — и больше нигде.
 *
 * Предмет (KAC #132). Форма была выписана в ПЯТИ файлах: в общем реестре и по копии у
 * compute / nlb / registry / storage. Копии разошлись молча — сверять их было нечем:
 * `admin` / `mutationsReturnOperation` / `listFilters` жили только в общей, `facet` /
 * `loadAllPages` — только в приватных, а комментарий у `immutable?` в `form-schema.ts`
 * существовал в ТРЁХ редакциях, из которых верна была одна. Первым о расхождении узнал
 * не автор правки, а тот, кто чинил упавшую сборку трёх приложений неделю спустя.
 *
 * Что проверяется. Набор охраняемых имён НЕ выписан здесь руками, а ВЫВЕДЕН из самих
 * канонических модулей: что они объявляют — то и охраняется. Рукописный перечень был бы
 * шестой копией формы и разошёлся бы ровно тем же способом, что и первые пять (новое поле
 * типа в `form-schema.ts` осталось бы вне охраны, и гейт молчал бы законно).
 *
 * Находка — ОБЪЯВЛЕНИЕ охраняемого имени вне его канонического модуля. Реэкспорт
 * (`export type { ResourceSpec } from "@shared/lib/resource-spec"`) находкой НЕ является
 * и является законным: у него нет тела, поэтому разойтись с источником он не может.
 * Различает их разбор AST, а не текст: регулярное выражение по тексту нашло бы имя в
 * комментарии, который эту же охрану и объясняет (в т.ч. в шапке этого файла).
 *
 * Проверка СВОЕЙ предпосылки. Гейт обоснован тем, что канонические модули существуют и
 * что-то объявляют. Пропадёт модуль, переименуется тип, опустеет обход дерева — это
 * ПАДЕНИЕ, а не тихое «ноль находок»: иначе гейт переживёт свой предмет и станет
 * зелёным навсегда.
 *
 * Единица счёта — отслеживаемый git-элемент (`git ls-files`), а не то, что лежит на диске.
 *
 * Запуск из ui-future/:  node scripts/check-resource-spec-single-source.mjs
 * Выход ненулевой — есть находки.
 */

import { execFileSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";

import ts from "typescript";

/**
 * Канонические модули формы. Что каждый ОБЪЯВЛЯЕТ — то и охраняется; перечень имён
 * выводится чтением этих файлов, а не переписывается сюда.
 */
const CANONICAL = [
  "shared/src/lib/resource-spec.ts", // ResourceColumn / ListFilter / ResourceSpec
  "shared/src/lib/form-schema.ts", // FormField и его варианты
];

const TS_FILE = /\.(ts|tsx)$/;

const uiRoot = process.cwd();
if (!fs.existsSync(path.join(uiRoot, "package.json"))) {
  console.error("::error::запускать из ui-future/ (нет package.json в текущем каталоге)");
  process.exit(2);
}

/** Имена типов, объявленных в файле. Разбор AST: комментарии и строки сюда не попадают. */
function declaredTypeNames(absPath) {
  const src = ts.createSourceFile(
    absPath,
    fs.readFileSync(absPath, "utf8"),
    ts.ScriptTarget.Latest,
    /* setParentNodes */ false,
    absPath.endsWith(".tsx") ? ts.ScriptKind.TSX : ts.ScriptKind.TS,
  );
  const names = [];
  const visit = (node) => {
    // Только ОБЪЯВЛЕНИЯ. ExportDeclaration (реэкспорт) сюда не попадает by construction:
    // это другой вид узла, и тела у него нет.
    if (ts.isInterfaceDeclaration(node) || ts.isTypeAliasDeclaration(node)) {
      names.push({ name: node.name.text, line: src.getLineAndCharacterOfPosition(node.name.pos).line + 1 });
    }
    ts.forEachChild(node, visit);
  };
  ts.forEachChild(src, visit);
  return names;
}

// ── предпосылка: канонические модули на месте и что-то объявляют ──────────────
const guarded = new Map(); // имя → канонический файл
for (const rel of CANONICAL) {
  const abs = path.join(uiRoot, rel);
  if (!fs.existsSync(abs)) {
    console.error(
      `::error::канонического модуля формы нет в дереве: ${rel}. ` +
        "Гейт обоснован его существованием — это падение, а не «ноль находок».",
    );
    process.exit(2);
  }
  const names = declaredTypeNames(abs);
  if (names.length === 0) {
    console.error(
      `::error::${rel} не объявляет ни одного типа — предпосылка гейта не выполняется ` +
        "(модуль переименован, опустошён или разобран неверно).",
    );
    process.exit(2);
  }
  for (const { name } of names) {
    const prev = guarded.get(name);
    if (prev && prev !== rel) {
      console.error(`::error::имя ${name} объявлено в ДВУХ канонических модулях: ${prev} и ${rel}`);
      process.exit(2);
    }
    guarded.set(name, rel);
  }
}

// ── обход дерева ─────────────────────────────────────────────────────────────
const tracked = execFileSync("git", ["ls-files", "."], { cwd: uiRoot, encoding: "utf8" })
  .split("\n")
  .filter((f) => f && TS_FILE.test(f) && !f.startsWith("node_modules/") && !f.includes("/node_modules/"));

if (tracked.length === 0) {
  console.error("::error::обход дерева не дал ни одного .ts/.tsx — читать было нечего, это падение");
  process.exit(2);
}

const findings = [];
let filesRead = 0;
let declsSeen = 0;
const pkgsSeen = new Set();

for (const rel of tracked) {
  const abs = path.join(uiRoot, rel);
  let names;
  try {
    names = declaredTypeNames(abs);
  } catch (e) {
    console.error(`::error::${rel} не разобрался: ${e.message}`);
    process.exit(2);
  }
  filesRead += 1;
  declsSeen += names.length;
  pkgsSeen.add(rel.split("/")[0]);

  for (const { name, line } of names) {
    const canonical = guarded.get(name);
    if (!canonical || canonical === rel) continue;
    findings.push(
      `${rel}:${line} объявляет «${name}» — форма ресурса объявлена ЗДЕСЬ и в ${canonical}. ` +
        "Два объявления одного типа расходятся молча: импортируй из канонического модуля " +
        "(реэкспорт `export type { … } from …` законен — у него нет тела).",
    );
  }
}

console.log(
  `осмотрено: пакетов ${pkgsSeen.size}, отслеживаемых .ts/.tsx ${filesRead}, ` +
    `объявлений типов ${declsSeen}; охраняемых имён ${guarded.size} в ${CANONICAL.length} модулях`,
);
console.log(`  канонические модули: ${CANONICAL.join(", ")}`);

if (findings.length) {
  for (const f of findings) console.error(`::error::${f}`);
  console.error(`\n✖ находок: ${findings.length}`);
  process.exit(1);
}
console.log("✓ форма ресурса объявлена ровно в одном месте на тип");
