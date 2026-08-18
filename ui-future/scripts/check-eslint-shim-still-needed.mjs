#!/usr/bin/env node
// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

/**
 * Плагины линта и eslint, которым ЛИНТУЮТ, обязаны работать вместе — и это
 * проверяется ЗАПУСКОМ, а не сравнением объявленных диапазонов.
 *
 * ЗАЧЕМ. Переход на eslint 10 упирался в два плагина: `eslint-plugin-react` и
 * `eslint-plugin-jsx-a11y` объявляют peer `eslint` до девятого мажора включительно, и под
 * десятым падают на снятом API (`context.getFilename` больше нет). Пока релиза под v10 у
 * них нет, оба оборачиваются `fixupPluginRules` из `@eslint/compat` — это официальный
 * переходник команды ESLint ровно для такого случая, — а их peer сужается через
 * `overrides`, иначе npm ставит ВТОРУЮ копию eslint девятого мажора рядом.
 *
 * ПОЧЕМУ ГЕЙТ, А НЕ КОММЕНТАРИЙ. Послабление, у которого не названо условие снятия,
 * переживает своё основание и остаётся навсегда: плагин однажды выпустит совместимую
 * версию, шим станет лишним, и заметить это будет некому. Здесь условие снятия названо
 * машинно — и роняет прогон, когда наступает.
 *
 * ЧТО ЭТО ЗА ГЕЙТ (он способен упасть, и падает по пяти разным поводам):
 *   • плагин НЕ ГРУЗИТСЯ под установленным eslint          → `npm run lint:js` этого пакета
 *     неисполним: правила падают на снятом API;
 *   • плагин СТАЛ принимать установленный eslint            → шим и overrides снять (предмет исчез);
 *   • в конфигурации есть шим, а в манифесте нет overrides   → половина послабления, при
 *     которой npm молча заводит вторую копию eslint;
 *   • шим объявлен, а переходник не установлен               → конфигурация не загрузится;
 *   • рассматривать оказалось нечего                         → предпосылка гейта исчезла.
 *
 * Гейт заявляет ОБЪЁМ ОСМОТРЕННОГО: «ноль находок» обязано быть отличимо от «ноль
 * прочитанного». Набор пакетов выводится из дерева, а не выписывается: выписанный
 * переживёт добавление пакета и промолчит о нём.
 *
 * > [!warning] Здесь стояло «у члена workspace линт запускается КОРНЕВЫМ двоичным файлом»
 * > Из этой посылки следовал порядок поиска «члену workspace смотреть корень, автономному
 * > пакету свой каталог», и она НЕВЕРНА: `npm run lint:js` ставит в PATH сначала
 * > `<пакет>/node_modules/.bin`, и вложенная копия выигрывает у корневой. Ровно это и
 * > случилось: замок держал под четырьмя членами workspace остаток откачённой пробы
 * > eslint 10, объявленная команда линта падала на загрузке правила во всех четырёх, —
 * > а этот гейт судил корневую девятку и был ЗЕЛЁН. Порядок поиска теперь один для всех
 * > и повторяет запуск: ближайший каталог, затем корень.
 *
 * Отдельно о том, чем судить совместимость. Объявленный peer-диапазон — это МНЕНИЕ автора
 * плагина о версиях, которых он на момент релиза не видел. Он ошибается в обе стороны:
 * бывает пессимистичен (работает там, где не обещал) и оптимистичен (обещал, но падает).
 * Поэтому решение принимает ПРОБНЫЙ ЗАПУСК — каждое правило плагина создаётся под тем
 * eslint, которым пакет линтуется, и в том виде, в каком его подаёт конфигурация (сырым
 * либо через `fixupPluginRules`). Диапазон остаётся вторым голосом: он объясняет ПОЧЕМУ и
 * решает вопрос о снятии послабления.
 */

import { createRequire } from "node:module";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const require = createRequire(import.meta.url);
const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");

/** Плагины, ради которых заведено послабление. */
const SHIMMED = ["eslint-plugin-react", "eslint-plugin-jsx-a11y"];

/**
 * Настройки, под которыми пробуются правила. Ровно те, что стоят в конфигурациях
 * пакетов: без `react.version: detect` часть правил не доходит до вызова снятого API и
 * проба недосчитывает (проверено — 2 правила против 32 на одном и том же сломанном
 * дереве).
 */
const PROBE_SETTINGS = { react: { version: "detect" } };

/** Исходник пробы: правила создаются до обхода, поэтому содержимое роли не играет. */
const PROBE_SOURCE = "const a = 1;\n";

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
let probedPairs = 0;
let triedRules = 0;
let unloadableRules = 0;
let shimmedPkgs = 0;

/**
 * Манифест того самого модуля, который РЕЗОЛВНУЛСЯ: подъём от файла точки входа до
 * первого package.json. Читать манифест отдельным поиском нельзя — тогда версия
 * относилась бы к копии, которую резолв не выбрал, а это и есть разобранная ошибка.
 */
function manifestAbove(entryFile) {
  let dir = path.dirname(entryFile);
  for (let i = 0; i < 12; i += 1) {
    const p = path.join(dir, "package.json");
    if (fs.existsSync(p)) {
      const m = JSON.parse(fs.readFileSync(p, "utf8"));
      if (m.name && m.version) return m;
    }
    const up = path.dirname(dir);
    if (up === dir) break;
    dir = up;
  }
  return null;
}

/**
 * Создаёт каждое правило плагина под указанным eslint и возвращает те, что не
 * загрузились. ESLint оборачивает отказ создания правила своим префиксом
 * «Error while loading rule '<id>'» — по нему отличаем несовместимость API от
 * отказа проверки схемы опций (у той свой префикс и к совместимости она отношения
 * не имеет).
 */
function probePlugin(Linter, plugin) {
  const linter = new Linter();
  const names = Object.keys(plugin.rules ?? {});
  const failed = [];
  // Плагины печатают предупреждения об устаревших правилах на каждое создание —
  // на 11 пакетах это сотни строк, в которых утонет находка.
  const saved = { log: console.log, warn: console.warn };
  console.log = () => {};
  console.warn = () => {};
  try {
    for (const rule of names) {
      try {
        linter.verify(PROBE_SOURCE, {
          plugins: { probe: plugin },
          settings: PROBE_SETTINGS,
          rules: { [`probe/${rule}`]: "error" },
        });
      } catch (e) {
        const message = String(e?.message ?? e).split("\n")[0];
        if (/Error while loading rule/.test(message)) failed.push({ rule, message });
      }
    }
  } finally {
    console.log = saved.log;
    console.warn = saved.warn;
  }
  return { tried: names.length, failed };
}

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

  // Резолв повторяет ЗАПУСК линта: `npm run lint:js` ставит в PATH сначала
  // `<пакет>/node_modules/.bin`, и только потом поднимается к корню установки. Обычный
  // подъём node от каталога пакета даёт ровно этот порядок — один для всех, и для
  // автономного пакета, и для члена workspace.
  const req = createRequire(path.join(dir, "package.json"));

  for (const plugin of SHIMMED) {
    if (!deps[plugin]) continue;
    examinedPairs += 1;

    let pluginEntry;
    let eslintEntry;
    try {
      pluginEntry = req.resolve(plugin);
      eslintEntry = req.resolve("eslint");
    } catch {
      findings.push(`${name}: ${plugin} или eslint не установлены — гейт не может судить (нужен npm ci)`);
      continue;
    }
    const pluginManifest = manifestAbove(pluginEntry);
    const eslintManifest = manifestAbove(eslintEntry);
    const declared = pluginManifest?.peerDependencies?.eslint;
    const installed = eslintManifest?.version;
    if (!installed) {
      findings.push(`${name}: у резолвленного eslint (${eslintEntry}) не читается версия — гейт не может судить`);
      continue;
    }
    const pluginAcceptsInstalled =
      declared && semver.satisfies(installed, declared, { includePrerelease: false });

    // ГЛАВНЫЙ вопрос — не «что объявлено», а «грузится ли». Плагин подаётся ровно так,
    // как его подаёт конфигурация пакета: сырым либо через переходник.
    let Linter;
    let pluginObj;
    try {
      const eslintMod = await import(pathToFileURL(eslintEntry).href);
      Linter = eslintMod.Linter ?? eslintMod.default?.Linter;
      const mod = await import(pathToFileURL(pluginEntry).href);
      pluginObj = mod.default ?? mod;
    } catch (e) {
      findings.push(
        `${name}: ${plugin} или eslint не загружаются из ${path.relative(root, dir)} — ` +
          `гейт не может судить: ${String(e?.message ?? e).split("\n")[0]}`,
      );
      continue;
    }
    if (typeof Linter !== "function") {
      findings.push(`${name}: у установленного eslint ${installed} нет класса Linter — гейт не может судить`);
      continue;
    }

    if (hasShim) {
      // Конфигурация подаёт плагин через переходник — значит и пробовать надо его.
      try {
        const compat = await import(pathToFileURL(req.resolve("@eslint/compat")).href);
        const fixup = compat.fixupPluginRules ?? compat.default?.fixupPluginRules;
        if (typeof fixup !== "function") throw new Error("в @eslint/compat нет fixupPluginRules");
        pluginObj = fixup(pluginObj);
      } catch (e) {
        findings.push(
          `${name}: конфигурация зовёт fixupPluginRules, а переходник не берётся — ` +
            `конфигурация не загрузится: ${String(e?.message ?? e).split("\n")[0]}`,
        );
        continue;
      }
    }

    const { tried, failed } = probePlugin(Linter, pluginObj);
    probedPairs += 1;
    triedRules += tried;
    unloadableRules += failed.length;

    if (failed.length) {
      findings.push(
        `${name}: под установленным eslint ${installed} не загружается ${failed.length} из ${tried} правил ` +
          `${plugin}${hasShim ? " (даже через fixupPluginRules)" : ""} — ` +
          `«${failed[0].message}»; объявленный peer плагина «${declared}». ` +
          `npm run lint:js в этом пакете неисполним`,
      );
      continue;
    }

    if (pluginAcceptsInstalled) {
      // Предмета нет: плагин принимает то, что стоит, и грузится. Тогда находкой
      // является уже ОБРАТНОЕ — оставшийся шим, потому что он переживёт своё основание.
      if (hasShim) {
        findings.push(
          `${name}: ${plugin} УЖЕ принимает eslint ${installed} (peer «${declared}») и грузится сам — ` +
            `послаблению нечего исключать: снять fixupPluginRules из ${path.basename(cfg)}`,
        );
      }
      continue;
    }

    // Правила грузятся, но плагин этой версии eslint не обещал. Само по себе это не
    // находка (диапазон бывает пессимистичен), а вот половина послабления — находка:
    // overrides действуют ТОЛЬКО у корня установки, у члена workspace они мертвы.
    if (hasShim) {
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
}

console.log(
  `осмотрено: пакетов с конфигурацией линта ${examinedPkgs}, из них с шимом ${shimmedPkgs}; ` +
    `пар «пакет × плагин» ${examinedPairs}, из них пробным запуском ${probedPairs}; ` +
    `создано правил ${triedRules}, не загрузилось ${unloadableRules}`,
);

if (examinedPairs === 0) {
  console.error("::error::рассматривать нечего: ни один пакет не подключает шимленные плагины — гейт потерял предмет и подлежит снятию");
  process.exit(1);
}

if (probedPairs === 0 && findings.length === 0) {
  console.error("::error::ни одна пара не дошла до пробного запуска — «ноль находок» здесь означает «ноль прочитанного»");
  process.exit(1);
}

if (findings.length) {
  for (const f of findings) console.error(`::error::${f}`);
  console.error(`\n✖ находок: ${findings.length}`);
  process.exit(1);
}

console.log(
  "✓ каждый плагин грузится под тем eslint, которым линтуется его пакет; " +
    "где послабление есть — у него есть предмет, где предмета нет — нет и послабления",
);
