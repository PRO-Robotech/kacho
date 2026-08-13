#!/usr/bin/env node
// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

/**
 * Послабление под eslint 10 живёт, пока у него есть ПРЕДМЕТ.
 *
 * ЗАЧЕМ. Переход на eslint 10 упирался в два плагина: `eslint-plugin-react` и
 * `eslint-plugin-jsx-a11y` объявляют peer `eslint` до девятого мажора включительно, и под
 * десятым падают на снятом API (`context.getFilename` больше нет). Релиза под v10 у них на
 * момент перехода не было. Поэтому оба обёрнуты `fixupPluginRules` из `@eslint/compat` —
 * это официальный переходник команды ESLint ровно для такого случая, — а их peer сужен
 * через `overrides`, иначе npm ставит ВТОРУЮ копию eslint девятого мажора рядом.
 *
 * ПОЧЕМУ ГЕЙТ, А НЕ КОММЕНТАРИЙ. Послабление, у которого не названо условие снятия,
 * переживает своё основание и остаётся навсегда: плагин однажды выпустит совместимую
 * версию, шим станет лишним, и заметить это будет некому. Здесь условие снятия названо
 * машинно — и роняет прогон, когда наступает.
 *
 * ЧТО ЭТО ЗА ГЕЙТ (он способен упасть, и падает по четырём разным поводам):
 *   • плагин СТАЛ принимать установленный eslint     → шим и overrides снять (предмет исчез);
 *   • в конфигурации есть шим, а в манифесте нет overrides (или наоборот) → половина
 *     послабления, при которой npm молча заводит вторую копию eslint;
 *   • плагин подключён, а шима нет                    → под v10 линт этого пакета падает;
 *   • рассматривать оказалось нечего                  → предпосылка гейта исчезла.
 *
 * Гейт заявляет ОБЪЁМ ОСМОТРЕННОГО: «ноль находок» обязано быть отличимо от «ноль
 * прочитанного». Набор пакетов выводится из дерева, а не выписывается: выписанный
 * переживёт добавление пакета и промолчит о нём.
 */

import { createRequire } from "node:module";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const require = createRequire(import.meta.url);
const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");

/** Плагины, ради которых заведено послабление. */
const SHIMMED = ["eslint-plugin-react", "eslint-plugin-jsx-a11y"];

// Предпосылка гейта: без сравнителя версий он не может судить ни о чём и обязан
// сказать это вслух, а не тихо разрешить.
let semver;
try {
  semver = require("semver");
} catch {
  console.error("::error::предпосылка гейта не выполнена: пакет semver не резолвится из ui-future");
  process.exit(1);
}

const findings = [];
let examinedPkgs = 0;
let examinedPairs = 0;
let shimmedPkgs = 0;

const pkgDirs = fs
  .readdirSync(root, { withFileTypes: true })
  .filter((d) => d.isDirectory() && d.name !== "node_modules")
  .map((d) => path.join(root, d.name))
  .filter((p) => fs.existsSync(path.join(p, "package.json")));

for (const dir of pkgDirs) {
  const name = path.basename(dir);
  const cfg = ["eslint.config.js", "eslint.config.mjs"]
    .map((f) => path.join(dir, f))
    .find((f) => fs.existsSync(f));
  if (!cfg) continue;
  examinedPkgs += 1;

  const cfgText = fs.readFileSync(cfg, "utf8");
  const manifest = JSON.parse(fs.readFileSync(path.join(dir, "package.json"), "utf8"));
  const deps = { ...manifest.dependencies, ...manifest.devDependencies };
  const hasShim = cfgText.includes("fixupPluginRules");
  if (hasShim) shimmedPkgs += 1;

  for (const plugin of SHIMMED) {
    if (!deps[plugin]) continue;
    examinedPairs += 1;

    // Плагин подключён, а шима нет — под eslint 10 линт этого пакета упадёт на снятом API.
    if (!hasShim) {
      findings.push(`${name}: подключает ${plugin}, но не оборачивает его fixupPluginRules`);
      continue;
    }

    // overrides действуют ТОЛЬКО у корня установки: у члена workspace они мертвы, и
    // требовать их там значило бы краснеть на исправном пакете.
    const isWorkspaceMember = !fs.existsSync(path.join(dir, "package-lock.json"));
    const ovHost = isWorkspaceMember ? root : dir;
    const ovManifest = JSON.parse(fs.readFileSync(path.join(ovHost, "package.json"), "utf8"));
    if (!ovManifest.overrides?.[plugin]?.eslint) {
      findings.push(
        `${name}: шим есть, а сужения peer нет в ${path.relative(root, ovHost) || "."}/package.json ` +
          `(overrides.${plugin}.eslint) — npm заведёт вторую копию eslint`,
      );
    }

    // ГЛАВНОЕ: не исчез ли предмет послабления.
    const pluginPkgPath = ["node_modules", path.join("..", "node_modules")]
      .map((p) => path.join(dir, p, plugin, "package.json"))
      .find((p) => fs.existsSync(p));
    const eslintPkgPath = ["node_modules", path.join("..", "node_modules")]
      .map((p) => path.join(dir, p, "eslint", "package.json"))
      .find((p) => fs.existsSync(p));
    if (!pluginPkgPath || !eslintPkgPath) {
      findings.push(`${name}: ${plugin} или eslint не установлены — гейт не может судить (нужен npm ci)`);
      continue;
    }

    const declared = JSON.parse(fs.readFileSync(pluginPkgPath, "utf8")).peerDependencies?.eslint;
    const installed = JSON.parse(fs.readFileSync(eslintPkgPath, "utf8")).version;
    if (declared && semver.satisfies(installed, declared, { includePrerelease: false })) {
      findings.push(
        `${name}: ${plugin} УЖЕ принимает eslint ${installed} (peer «${declared}») — ` +
          `послаблению нечего исключать: снять fixupPluginRules из ${path.basename(cfg)} ` +
          `и overrides.${plugin} из ${path.relative(root, ovHost) || "."}/package.json`,
      );
    }
  }
}

console.log(
  `осмотрено: пакетов с конфигурацией линта ${examinedPkgs}, из них с шимом ${shimmedPkgs}; ` +
    `проверено пар «пакет × плагин» ${examinedPairs}`,
);

if (examinedPairs === 0) {
  console.error("::error::рассматривать нечего: ни один пакет не подключает шимленные плагины — гейт потерял предмет и подлежит снятию");
  process.exit(1);
}

if (findings.length) {
  for (const f of findings) console.error(`::error::${f}`);
  console.error(`\n✖ находок: ${findings.length}`);
  process.exit(1);
}

console.log("✓ послабление под eslint 10 всё ещё имеет предмет: оба плагина по-прежнему не принимают установленный eslint");
