#!/usr/bin/env node
// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

/**
 * Гейт покрытия линта: каждый отслеживаемый линтуемый файл ui-future обязан попадать
 * в область ESLint СВОЕГО пакета.
 *
 * Предмет. `eslint`, запущенный из каталога приложения, отвергает всё, что лежит выше
 * этого каталога, сообщением «File ignored because outside of base path» — то есть
 * пакет `shared`, от которого зависят все девять приложений, не попадал ни в одну из
 * девяти областей. Правило было, предмета у правила не было, и заметить это по зелёному
 * выводу `eslint .` невозможно: он честно говорит «ноль находок» о том, чего не читал.
 *
 * Что проверяется (по каждому пакету):
 *   1. у пакета с отслеживаемыми линтуемыми файлами ЕСТЬ файл конфигурации ESLint —
 *      любое из имён, которые принимает сам ESLint (см. CONFIG_NAMES ниже);
 *   2. каждый такой файл либо ПРОВЕРЯЕТСЯ, либо отвергнут ОБЪЯВЛЕННЫМ `ignores`
 *      этого же пакета; «вне области» (выше базового пути) — находка.
 *
 * Единица счёта — отслеживаемый git-элемент (`git ls-files`), а не то, что лежит на
 * диске: объявление, `.gitignore` и поведение не могут разъехаться молча.
 *
 * Гейт печатает ОБЪЁМ ОСМОТРЕННОГО (пакетов, файлов), чтобы «ноль находок» было
 * отличимо от «ноль прочитанного».
 *
 * Запуск из ui-future/:  node scripts/check-lint-coverage.mjs
 * Выход ненулевой — есть находки.
 */

import { execFileSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";

// ESLint резолвится обычным подъёмом по дереву от scripts/ → ui-future/node_modules,
// то есть тем же экземпляром, который зовут скрипты пакетов.
import { ESLint } from "eslint";

const LINTABLE = /\.(ts|tsx|js|jsx|mjs|cjs)$/;

// Имена, которые ESLint 9 ищет как конфигурацию пакета. Список — предпосылка гейта:
// если ESLint когда-нибудь начнёт принимать иное имя, пакет с ним будет здесь ошибочно
// объявлен нелинтуемым. Поэтому имена перечислены явно и рядом с проверкой, а не
// сведены к одному `eslint.config.js`.
const CONFIG_NAMES = ["eslint.config.js", "eslint.config.mjs", "eslint.config.cjs", "eslint.config.ts"];

const uiRoot = process.cwd();
if (!fs.existsSync(path.join(uiRoot, "package.json"))) {
  console.error("::error::запускать из ui-future/ (нет package.json в текущем каталоге)");
  process.exit(2);
}

// Отслеживаемые линтуемые файлы, сгруппированные по пакету верхнего уровня.
const tracked = execFileSync("git", ["ls-files", "."], { cwd: uiRoot, encoding: "utf8" })
  .split("\n")
  .filter((f) => f && LINTABLE.test(f) && !f.startsWith("node_modules/"));

const byPkg = new Map();
for (const f of tracked) {
  const pkg = f.split("/")[0];
  if (!f.includes("/")) continue; // файлы в корне ui-future — не пакет
  if (!byPkg.has(pkg)) byPkg.set(pkg, []);
  byPkg.get(pkg).push(f);
}

const findings = [];
let filesSeen = 0;
let filesInspected = 0;
let filesDeclaredIgnore = 0;

const pkgs = [...byPkg.keys()].sort();
for (const pkg of pkgs) {
  const files = byPkg.get(pkg);
  const hasConfig = CONFIG_NAMES.some((n) => fs.existsSync(path.join(uiRoot, pkg, n)));
  if (!hasConfig) {
    findings.push(
      `${pkg}: ${files.length} отслеживаемых линтуемых файлов, но конфигурации ESLint НЕТ ` +
        `(искали ${CONFIG_NAMES.join(", ")}) — пакет не линтуется ничем`,
    );
    filesSeen += files.length;
    continue;
  }

  const eslint = new ESLint({ cwd: path.join(uiRoot, pkg) });
  let inspected = 0;
  const outside = [];
  const declared = [];
  for (const f of files) {
    filesSeen += 1;
    const abs = path.join(uiRoot, f);
    const rel = path.relative(path.join(uiRoot, pkg), abs);
    const isOutsideBasePath = rel.startsWith("..") || path.isAbsolute(rel);
    if (isOutsideBasePath) {
      // by construction недостижимо (файлы сгруппированы по своему же пакету),
      // но оставлено явной ветвью: группировка — предпосылка гейта, а не аксиома.
      outside.push(f);
      continue;
    }
    if (await eslint.isPathIgnored(abs)) {
      declared.push(f);
      continue;
    }
    inspected += 1;
  }
  filesInspected += inspected;
  filesDeclaredIgnore += declared.length;

  const ign = declared.length ? ` (объявленный ignores: ${declared.length})` : "";
  console.log(`  ${pkg}: проверяется ${inspected}/${files.length}${ign}`);
  if (outside.length) {
    findings.push(
      `${pkg}: ${outside.length} файлов вне базового пути конфигурации: ${outside.slice(0, 5).join(", ")}…`,
    );
  }
}

console.log("");
console.log(
  `осмотрено: пакетов ${pkgs.length}, отслеживаемых линтуемых файлов ${filesSeen} ` +
    `(проверяется ${filesInspected}, объявленный ignores ${filesDeclaredIgnore})`,
);

if (findings.length) {
  for (const f of findings) console.error(`::error::${f}`);
  console.error(`\n✖ находок: ${findings.length}`);
  process.exit(1);
}
console.log("✓ каждый отслеживаемый линтуемый файл попадает в область линта своего пакета");
