#!/usr/bin/env node
// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

/**
 * Гейт линта ui-future — ДВА утверждения об одном и том же инструменте:
 *
 *   1. ПОКРЫТИЕ. Каждый отслеживаемый линтуемый файл обязан попадать в область
 *      ESLint СВОЕГО пакета.
 *   2. ИСПОЛНИМОСТЬ. Объявленная пакетом команда `npm run lint:js` обязана
 *      запускаться — то есть его собственный ESLint обязан прочитать его
 *      конфигурацию и пролинтовать настоящий файл, не упав.
 *
 * Предмет первого. `eslint`, запущенный из каталога приложения, отвергает всё, что лежит
 * выше этого каталога, сообщением «File ignored because outside of base path» — то есть
 * пакет `shared`, от которого зависят все девять приложений, не попадал ни в одну из
 * девяти областей. Правило было, предмета у правила не было, и заметить это по зелёному
 * выводу `eslint .` невозможно: он честно говорит «ноль находок» о том, чего не читал.
 *
 * Предмет второго (#572). Гейт судил КОРНЕВЫМ экземпляром ESLint — `import { ESLint }
 * from "eslint"` из scripts/ поднимается в `ui-future/node_modules`. А `npm run lint:js`
 * запускает БЛИЖАЙШИЙ: npm ставит в PATH сперва `<пакет>/node_modules/.bin`. Пока замок
 * держал под четырьмя членами workspace вторую копию eslint другого мажора, объявленная
 * команда падала на загрузке правила во всех четырёх — а этот гейт был ЗЕЛЁН и печатал
 * «проверяется 1347 файлов», хотя проверял их не тем инструментом, которым линтуется
 * пакет. Утверждение об объёме осмотренного было при этом верным по числу и ложным по
 * предмету, что хуже отсутствия утверждения.
 *
 * Поэтому ESLint здесь резолвится ОТ КАТАЛОГА ПАКЕТА (ближайший, затем корень) — тем же
 * порядком, что даёт запуск, — и тем же, каким его резолвит гейт шима
 * (scripts/check-eslint-shim-still-needed.mjs). Один порядок на два гейта: разойтись им
 * негде.
 *
 * Шов с гейтом шима назван, чтобы два места об одном предмете не завелись снова: тот
 * отвечает на вопрос «нужно ли ещё послабление под этот плагин» и пробует правила
 * ПОИМЁННО; этот — на вопрос «запускается ли объявленная команда вообще» и линтует
 * НАСТОЯЩИЙ файл пакета, поэтому ловит и то, что плагинами не объясняется (конфигурация
 * не грузится, парсер не резолвится, ESLint сменил API загрузки).
 *
 * Что проверяется (по каждому пакету):
 *   1. у пакета с отслеживаемыми линтуемыми файлами ЕСТЬ файл конфигурации ESLint —
 *      любое из имён, которые принимает сам ESLint (см. CONFIG_NAMES ниже);
 *   2. его собственный ESLint резолвится;
 *   3. каждый такой файл либо ПРОВЕРЯЕТСЯ, либо отвергнут ОБЪЯВЛЕННЫМ `ignores`
 *      этого же пакета; «вне области» (выше базового пути) — находка;
 *   4. пробный прогон линта по настоящему файлу пакета не падает.
 *
 * Единица счёта — отслеживаемый git-элемент (`git ls-files`), а не то, что лежит на
 * диске: объявление, `.gitignore` и поведение не могут разъехаться молча.
 *
 * Гейт печатает ОБЪЁМ ОСМОТРЕННОГО (пакетов, файлов, пробных прогонов, различных
 * экземпляров ESLint), чтобы «ноль находок» было отличимо от «ноль прочитанного».
 *
 * Запуск из ui-future/:  node scripts/check-lint-coverage.mjs
 *                        node scripts/check-lint-coverage.mjs --self-test
 * Выход ненулевой — есть находки.
 */

import { execFileSync } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { createRequire } from "node:module";
import { pathToFileURL } from "node:url";

const LINTABLE = /\.(ts|tsx|js|jsx|mjs|cjs)$/;

// Имена, которые ESLint 9 ищет как конфигурацию пакета. Список — предпосылка гейта:
// если ESLint когда-нибудь начнёт принимать иное имя, пакет с ним будет здесь ошибочно
// объявлен нелинтуемым. Поэтому имена перечислены явно и рядом с проверкой, а не
// сведены к одному `eslint.config.js`.
const CONFIG_NAMES = ["eslint.config.js", "eslint.config.mjs", "eslint.config.cjs", "eslint.config.ts"];

/**
 * ESLint ПАКЕТА, а не ui-future: подъём от `<пакет>/package.json` даёт ровно тот порядок
 * поиска, что и запуск `npm run lint:js` (ближайший `node_modules`, затем корень).
 */
async function eslintOf(pkgDir) {
  const req = createRequire(path.join(pkgDir, "package.json"));
  const entry = req.resolve("eslint");
  const mod = await import(pathToFileURL(entry).href);
  const ESLint = mod.ESLint ?? mod.default?.ESLint;
  if (typeof ESLint !== "function") throw new Error(`в ${entry} нет класса ESLint`);
  let version = "?";
  try {
    version = JSON.parse(fs.readFileSync(path.join(path.dirname(entry), "..", "package.json"), "utf8")).version;
  } catch {
    /* версия — вторая строка отчёта, её отсутствие не повод не судить */
  }
  return { ESLint, entry, version };
}

/**
 * Пробный прогон линта. Возвращает `null`, когда команда исполнима, и первую строку
 * отказа, когда нет. Находки самого линта здесь НЕ рассматриваются: предмет пробы —
 * запускается ли инструмент, а не что он говорит о коде.
 *
 * Чистая функция — её же зовёт `--self-test`.
 */
async function probeLintRuns(ESLint, pkgDir, files) {
  if (files.length === 0) return null;
  const eslint = new ESLint({ cwd: pkgDir });
  try {
    await eslint.lintFiles(files);
    return null;
  } catch (e) {
    return String(e?.message ?? e).split("\n")[0];
  }
}

/**
 * Самопроверка — ИНЪЕКЦИЯ В ОБЕ СТОРОНЫ на синтетических пакетах во временном каталоге.
 * В дерево репозитория ничего не вносится: писать в чужое рабочее состояние ради пробы
 * запрещено, и испорченный индекс делает лживыми проверки, которые читают дерево.
 *
 * Сторона «красное»: конфигурация, которая падает при загрузке правила, — ровно та форма
 * отказа, с которой заведена #572. Сторона «зелёное»: законная конфигурация той же
 * внешности обязана молчать, иначе гейт ловил бы форму, а не существо.
 */
async function selfTest() {
  const tmp = fs.mkdtempSync(path.join(os.tmpdir(), "lint-coverage-selftest-"));
  const mk = (name, configBody) => {
    const dir = path.join(tmp, name);
    fs.mkdirSync(dir, { recursive: true });
    fs.writeFileSync(path.join(dir, "package.json"), JSON.stringify({ name, version: "0.0.0", type: "module" }));
    fs.writeFileSync(path.join(dir, "eslint.config.js"), configBody);
    fs.writeFileSync(path.join(dir, "probe.js"), "const a = 1;\nexport default a;\n");
    return dir;
  };
  // Правило, которое падает при создании, — так же, как падает правило плагина под
  // ESLint, снявшим API, на котором оно построено.
  const brokenDir = mk(
    "broken",
    `export default [{ files: ["**/*.js"], plugins: { probe: { rules: { boom: { create() { throw new TypeError("contextOrFilename.getFilename is not a function"); } } } } }, rules: { "probe/boom": "error" } }];\n`,
  );
  const legalDir = mk(
    "legal",
    `export default [{ files: ["**/*.js"], plugins: { probe: { rules: { boom: { create() { return {}; } } } } }, rules: { "probe/boom": "error" } }];\n`,
  );

  // ESLint берём тот, что резолвится от ui-future: у синтетических пакетов своего нет,
  // и подъём приводит сюда — ровно как у автономного пакета без установки.
  const { ESLint, version } = await eslintOf(process.cwd());

  const red = await probeLintRuns(ESLint, brokenDir, [path.join(brokenDir, "probe.js")]);
  if (red === null) {
    console.error("::error::самопроверка: внесённый дефект НЕ пойман — гейт не способен упасть");
    fs.rmSync(tmp, { recursive: true, force: true });
    process.exit(1);
  }
  const green = await probeLintRuns(ESLint, legalDir, [path.join(legalDir, "probe.js")]);
  if (green !== null) {
    console.error(
      `::error::самопроверка: гейт краснеет на ЗАКОННОЙ конфигурации той же внешности («${green}») — ` +
        "он ловит форму, а не существо",
    );
    fs.rmSync(tmp, { recursive: true, force: true });
    process.exit(1);
  }
  fs.rmSync(tmp, { recursive: true, force: true });
  console.log(
    `самопроверка (ESLint ${version}): на сломанной конфигурации отказ «${red}», ` +
      "на законной той же внешности — молчание",
  );
  process.exit(0);
}

const uiRoot = process.cwd();
if (!fs.existsSync(path.join(uiRoot, "package.json"))) {
  console.error("::error::запускать из ui-future/ (нет package.json в текущем каталоге)");
  process.exit(2);
}

if (process.argv.includes("--self-test")) await selfTest();

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
let probesRun = 0;
const engines = new Set();

const pkgs = [...byPkg.keys()].sort();
for (const pkg of pkgs) {
  const files = byPkg.get(pkg);
  const pkgDir = path.join(uiRoot, pkg);
  const hasConfig = CONFIG_NAMES.some((n) => fs.existsSync(path.join(pkgDir, n)));
  if (!hasConfig) {
    findings.push(
      `${pkg}: ${files.length} отслеживаемых линтуемых файлов, но конфигурации ESLint НЕТ ` +
        `(искали ${CONFIG_NAMES.join(", ")}) — пакет не линтуется ничем`,
    );
    filesSeen += files.length;
    continue;
  }

  let ESLint;
  let version;
  try {
    ({ ESLint, version } = await eslintOf(pkgDir));
  } catch (e) {
    findings.push(
      `${pkg}: собственный ESLint не резолвится (${String(e?.message ?? e).split("\n")[0]}) — ` +
        `npm run lint:js в этом пакете неисполним; гейт судить не может`,
    );
    filesSeen += files.length;
    continue;
  }
  engines.add(`${pkg}→${version}`);

  const eslint = new ESLint({ cwd: pkgDir });
  let inspected = 0;
  const outside = [];
  const declared = [];
  const lintable = [];
  for (const f of files) {
    filesSeen += 1;
    const abs = path.join(uiRoot, f);
    const rel = path.relative(pkgDir, abs);
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
    lintable.push(abs);
  }
  filesInspected += inspected;
  filesDeclaredIgnore += declared.length;

  // Пробный прогон — по ОДНОМУ файлу каждого расширения, встретившегося в пакете.
  // Расширение выбирает, какие блоки конфигурации применятся, а с ними и какие
  // правила будут созданы: правила React грузятся на `.tsx` и могут не грузиться
  // на `.mjs`, поэтому проба по одному файлу недосчитывала бы по построению.
  const byExt = new Map();
  for (const abs of lintable) {
    const ext = path.extname(abs);
    if (!byExt.has(ext)) byExt.set(ext, abs);
  }
  const probeFiles = [...byExt.values()];
  const failure = await probeLintRuns(ESLint, pkgDir, probeFiles);
  probesRun += probeFiles.length;

  const ign = declared.length ? ` (объявленный ignores: ${declared.length})` : "";
  console.log(
    `  ${pkg}: проверяется ${inspected}/${files.length}${ign}; ` +
      `ESLint ${version}, пробный прогон по ${probeFiles.length} файлам — ${failure === null ? "исполнился" : "ОТКАЗ"}`,
  );
  if (failure !== null) {
    findings.push(
      `${pkg}: объявленная команда npm run lint:js НЕИСПОЛНИМА — собственный ESLint ${version} ` +
        `упал на пробном прогоне: «${failure}». Область линта этого пакета не проверена ничем, ` +
        `и «ноль находок» здесь означало бы «ноль прочитанного»`,
    );
  }
  if (outside.length) {
    findings.push(
      `${pkg}: ${outside.length} файлов вне базового пути конфигурации: ${outside.slice(0, 5).join(", ")}…`,
    );
  }
}

console.log("");
console.log(
  `осмотрено: пакетов ${pkgs.length}, отслеживаемых линтуемых файлов ${filesSeen} ` +
    `(проверяется ${filesInspected}, объявленный ignores ${filesDeclaredIgnore}); ` +
    `пробных прогонов линта ${probesRun}; экземпляров ESLint ${engines.size} [${[...engines].join(", ")}]`,
);

// Предпосылка гейта: без пакетов и без файлов он не судит ни о чём и обязан сказать это
// вслух, а не тихо разрешить.
if (pkgs.length === 0 || filesSeen === 0) {
  console.error(
    "::error::предпосылка гейта не выполнена: пакетов или отслеживаемых линтуемых файлов ноль — " +
      "«ноль находок» здесь означало бы «ноль прочитанного»",
  );
  process.exit(1);
}
if (probesRun === 0 && findings.length === 0) {
  console.error("::error::ни один пакет не дошёл до пробного прогона — исполнимость команды не проверена ничем");
  process.exit(1);
}

if (findings.length) {
  for (const f of findings) console.error(`::error::${f}`);
  console.error(`\n✖ находок: ${findings.length}`);
  process.exit(1);
}
console.log(
  "✓ каждый отслеживаемый линтуемый файл попадает в область линта своего пакета, " +
    "и объявленная команда линта исполнима в каждом пакете",
);
