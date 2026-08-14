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

    // ПЕРВЫМ делом — есть ли у послабления предмет ВООБЩЕ.
    //
    // Шим нужен ровно тогда, когда плагин установленный eslint НЕ принимает. Прежняя
    // редакция спрашивала «есть ли шим» раньше, чем «нужен ли он», и на возврате к
    // девятому мажору требовала переходник от каждого пакета — двадцать находок на
    // исправном дереве. Гейт, краснеющий на законном, снимут первым же срабатыванием,
    // поэтому порядок здесь несущий, а не косметический.
    // ВАЖНО, каким eslint судить: тем, которым ЛИНТУЮТ, а не первым найденным.
    //
    // У члена workspace линт запускается корневым двоичным файлом
    // (`../node_modules/.bin/eslint .` — так делает и конвейер), а рядом с самим
    // пакетом npm может держать вложенную копию другого мажора. Гейт, читавший
    // ближайшую, судил версию, которая в линте не участвует: двадцать находок на
    // дереве, где линт исправен. Порядок поиска теперь повторяет запуск —
    // автономный пакет смотрит свой каталог, член workspace корневой.
    const isMember = !fs.existsSync(path.join(dir, "package-lock.json"));
    const lookup = isMember
      ? [path.join("..", "node_modules"), "node_modules"]
      : ["node_modules", path.join("..", "node_modules")];
    const pluginPkgPath0 = lookup
      .map((p) => path.join(dir, p, plugin, "package.json"))
      .find((p) => fs.existsSync(p));
    const eslintPkgPath0 = lookup
      .map((p) => path.join(dir, p, "eslint", "package.json"))
      .find((p) => fs.existsSync(p));
    if (!pluginPkgPath0 || !eslintPkgPath0) {
      findings.push(`${name}: ${plugin} или eslint не установлены — гейт не может судить (нужен npm ci)`);
      continue;
    }
    const declared0 = JSON.parse(fs.readFileSync(pluginPkgPath0, "utf8")).peerDependencies?.eslint;
    const installed0 = JSON.parse(fs.readFileSync(eslintPkgPath0, "utf8")).version;
    const pluginAcceptsInstalled =
      declared0 && semver.satisfies(installed0, declared0, { includePrerelease: false });

    if (pluginAcceptsInstalled) {
      // Предмета нет: плагин принимает то, что стоит. Тогда находкой является уже
      // ОБРАТНОЕ — оставшийся шим, потому что он переживёт своё основание.
      if (hasShim) {
        findings.push(
          `${name}: ${plugin} УЖЕ принимает eslint ${installed0} (peer «${declared0}») — ` +
            `послаблению нечего исключать: снять fixupPluginRules из ${path.basename(cfg)}`,
        );
      }
      continue;
    }

    // Плагин установленный eslint НЕ принимает — вот тогда шим обязателен.
    if (!hasShim) {
      findings.push(
        `${name}: подключает ${plugin}, который НЕ принимает установленный eslint ${installed0} ` +
          `(peer «${declared0}»), и не оборачивает его fixupPluginRules`,
      );
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

console.log(
  "✓ послабление и его предмет сходятся: где плагин установленный eslint не принимает — " +
    "шим на месте; где принимает — шима нет",
);
