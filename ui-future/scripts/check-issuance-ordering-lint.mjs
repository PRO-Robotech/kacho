#!/usr/bin/env node
// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

/**
 * Правило линта мест выпуска (приёмка F8, Р10, F8-46) ПОДКЛЮЧЕНО в каждом пакете
 * консоли и СПОСОБНО упасть — судится исполнением линта, а не чтением конфигурации.
 *
 * Правило одно (`shared/issuance-ordering.eslint.config.js`), подключают его десять
 * конфигураций. Подключение, снятое в одной из них, не видно ни по одному зелёному
 * `eslint .`: он честно молчит о правиле, которого нет. Поэтому по КАЖДОМУ пакету:
 *
 *   1. подключение — действующая конфигурация файла пакета несёт правило;
 *   2. инъекция — каждая подсаженная форма выпуска мимо упорядочивающего
 *      транспорта даёт находку ЭТОГО правила;
 *   3. близнецы — законный выпуск (`orderedTransport.fetch`), комментарий, текст,
 *      чужой член с похожим именем — молчание;
 *   4. дома — транспорт законен только в своём доме (`shared`: упорядочивающий
 *      транспорт для `fetch`, приёмник потока для `EventSource`); тот же путь в
 *      приложении домом не является;
 *   5. дерево — прод-файлы пакета (без проб и их оснастки) находок правила не
 *      дают; число прочитанных печатается, пустой обход — отказ.
 *
 * Пакет судится отдельным процессом собственным ESLint (тем, что запускает
 * `npm run lint:js`), — по той же причине и тем же порядком, что
 * `check-lint-coverage.mjs`. Разбор идёт без сведений о типах: правило их не
 * читает, а сервис проекта сделал бы каждую подсадку вне tsconfig отказом разбора.
 *
 * Запуск из ui-future/:  node scripts/check-issuance-ordering-lint.mjs
 * Выход ненулевой — есть находки.
 */

import { execFileSync, spawnSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import { createRequire } from "node:module";
import { fileURLToPath, pathToFileURL } from "node:url";

const RULES = new Set(["no-restricted-globals", "no-restricted-properties", "no-restricted-syntax"]);
const PLANT = "src/__issuance_probe__.tsx";

/** Формы выпуска мимо упорядочивающего транспорта: каждая обязана дать находку. */
const RED = [
  ["голый fetch", 'fetch("/iam/v1/me");'],
  ["window.fetch", 'window.fetch("/iam/v1/me");'],
  ["globalThis.fetch", 'globalThis.fetch("/iam/v1/me");'],
  ["self.fetch", 'self.fetch("/iam/v1/me");'],
  ["ключ строкой", 'window["fetch"]("/iam/v1/me");'],
  ["ссылка в переменной", "export const send = fetch;"],
  ["привязка", "export const bound = globalThis.fetch.bind(globalThis);"],
  ["разбор", "const { fetch: f } = window;\nvoid f;"],
  ["отражение", 'export const r = Reflect.get(window, "fetch");'],
  ["окно фрейма", 'declare const frame: HTMLIFrameElement;\nvoid frame.contentWindow?.fetch("/iam/v1/me");'],
  ["результат open()", 'void window.open("about:blank")?.fetch("/iam/v1/me");'],
  ["XMLHttpRequest", "export const x = new XMLHttpRequest();"],
  ["WebSocket", 'export const w = new WebSocket("wss://console.test/x");'],
  ["sendBeacon", 'navigator.sendBeacon("/iam/v1/audit", "x");'],
  ["EventSource вне приёмника потока", 'export const s = new EventSource("/subscription/v1/events");'],
  ["window.EventSource", "export const S = window.EventSource;"],
  ["транспорт пробы в продукте", 'export const k = Symbol.for("kacho.probe.fetch");'],
];

/** Законное и не-транспорт: каждое обязано молчать. */
const TWINS = [
  [
    "упорядочивающий транспорт",
    'import { orderedTransport } from "@shared/api/carrier-order";\nvoid orderedTransport.fetch("/iam/v1/me");',
  ],
  ["комментарий", '// прежде здесь стоял fetch("/iam/v1/me")\nexport const a = 1;'],
  ["текст", 'export const hint = "fetch(/iam/v1/me) не зовётся";'],
  ["чужой член с похожим именем", "declare const q: { fetchQuery(): void };\nq.fetchQuery();"],
  ["ключ объекта", "export const o = { fetch: 1 };"],
];

/** Дома транспортов: путь, законное там и то, что незаконно и там. */
const HOMES = [
  {
    file: "src/api/carrier-order.ts",
    legal: "export const p = globalThis.fetch(u);",
    still: "export const x = new XMLHttpRequest();",
  },
  {
    file: "src/lib/subscription/hub.ts",
    legal: "export const s = new EventSource(u);",
    still: 'export const p = fetch("/iam/v1/me");',
  },
];
/** Пакет, в котором дома лежат: в остальных тот же путь — не дом. */
const HOME_PACKAGE = "shared";

function isProductFile(rel) {
  return /\.(ts|tsx)$/.test(rel) && !/\.test\./.test(rel) && !/(^|\/)test\//.test(rel) && !rel.endsWith(".d.ts");
}

async function eslintOf(pkgDir) {
  const entry = createRequire(path.join(pkgDir, "package.json")).resolve("eslint");
  const mod = await import(pathToFileURL(entry).href);
  const ESLint = mod.ESLint ?? mod.default?.ESLint;
  if (typeof ESLint !== "function") throw new Error(`в ${entry} нет класса ESLint`);
  return ESLint;
}

async function judgePackage(uiRoot, pkg) {
  const pkgDir = path.join(uiRoot, pkg);
  const findings = [];
  const ESLint = await eslintOf(pkgDir);
  const eslint = new ESLint({
    cwd: pkgDir,
    ruleFilter: ({ ruleId }) => RULES.has(ruleId),
    overrideConfig: { languageOptions: { parserOptions: { projectService: false, project: null } } },
  });
  const ours = async (code, rel) => {
    const [res] = await eslint.lintText(code, { filePath: path.join(pkgDir, rel) });
    const fatal = res.messages.filter((m) => m.fatal);
    if (fatal.length > 0) throw new Error(`${pkg}/${rel}: разбор отказал — ${fatal[0].message}`);
    return res.messages.filter((m) => RULES.has(m.ruleId ?? ""));
  };

  // 1. Подключение.
  const cfg = await eslint.calculateConfigForFile(path.join(pkgDir, PLANT));
  const globalsRule = cfg?.rules?.["no-restricted-globals"];
  const wired = Array.isArray(globalsRule) && globalsRule.slice(1).some((o) => o?.name === "fetch");
  if (!wired)
    findings.push(`${pkg}: правило мест выпуска НЕ подключено — действующая конфигурация ${PLANT} не запрещает fetch`);

  // 2. Инъекция.
  let red = 0;
  for (const [name, code] of RED) {
    const got = await ours(code, PLANT);
    if (got.length === 0) findings.push(`${pkg}: подсаженная форма «${name}» находки не дала — правило слепо к ней`);
    else red += 1;
  }
  // 3. Близнецы.
  for (const [name, code] of TWINS) {
    const got = await ours(code, PLANT);
    if (got.length > 0)
      findings.push(
        `${pkg}: законный близнец «${name}» дал находку: ${got.map((m) => m.message.slice(0, 60)).join("; ")}`,
      );
  }
  // 4. Дома.
  let homes = 0;
  for (const h of HOMES) {
    const legal = await ours(`declare const u: string;\n${h.legal}`, h.file);
    const still = await ours(h.still, h.file);
    if (pkg === HOME_PACKAGE) {
      if (legal.length > 0) findings.push(`${pkg}: в доме ${h.file} законный транспорт дал находку`);
      if (still.length === 0)
        findings.push(`${pkg}: в доме ${h.file} чужой транспорт находки не дал — дом шире своего предмета`);
    } else if (legal.length === 0) {
      findings.push(`${pkg}: путь ${h.file} в приложении стал домом транспорта — дом есть только у ${HOME_PACKAGE}`);
    }
    homes += 1;
  }
  // Пробы и их оснастка выведены из области правила: подставляют сеть под стражем исполнения.
  const inTest = await ours(
    "globalThis.fetch = (() => undefined) as unknown as typeof fetch;",
    "src/__issuance_probe__.test.ts",
  );
  if (inTest.length > 0)
    findings.push(`${pkg}: проба, подставляющая сеть, дала находку — пробы из области правила выведены`);

  // 5. Дерево.
  const files = execFileSync("git", ["ls-files", "src"], { cwd: pkgDir, encoding: "utf8" })
    .split("\n")
    .filter((f) => f && isProductFile(f))
    .map((f) => path.join(pkgDir, f));
  if (files.length === 0)
    findings.push(`${pkg}: прод-файлов 0 — обход не того корня, «ноль находок» было бы «ноль прочитанного»`);
  const results = files.length > 0 ? await eslint.lintFiles(files) : [];
  let inTree = 0;
  for (const r of results) {
    for (const m of r.messages) {
      if (m.fatal) findings.push(`${pkg}: ${path.relative(uiRoot, r.filePath)} — разбор отказал: ${m.message}`);
      else if (RULES.has(m.ruleId ?? "")) {
        inTree += 1;
        findings.push(`${pkg}: ${path.relative(uiRoot, r.filePath)}:${m.line} — ${m.message}`);
      }
    }
  }
  return {
    pkg,
    findings,
    line: `  ${pkg}: подключено ${wired ? "да" : "НЕТ"} · инъекций красных ${red}/${RED.length} · близнецов ${TWINS.length} · домов ${homes} · прод-файлов ${files.length}, находок ${inTree}`,
  };
}

const uiRoot = process.cwd();
if (!fs.existsSync(path.join(uiRoot, "package.json"))) {
  console.error("::error::запускать из ui-future/ (нет package.json в текущем каталоге)");
  process.exit(2);
}

const packageArg = process.argv.indexOf("--package");
if (packageArg !== -1) {
  const pkg = process.argv[packageArg + 1];
  let res;
  try {
    res = await judgePackage(uiRoot, pkg);
  } catch (e) {
    res = {
      pkg,
      findings: [`${pkg}: суд не состоялся — ${String(e?.message ?? e).split("\n")[0]}`],
      line: `  ${pkg}: ОТКАЗ`,
    };
  }
  process.stdout.write(`${JSON.stringify(res)}\n`);
  process.exit(0);
}

// Пакеты консоли — каталоги с исходниками и своей конфигурацией линта: выводятся из
// дерева, а не выписываются, чтобы новый пакет попал под суд сам.
const packages = fs
  .readdirSync(uiRoot, { withFileTypes: true })
  .filter(
    (d) =>
      d.isDirectory() &&
      fs.existsSync(path.join(uiRoot, d.name, "eslint.config.js")) &&
      fs.existsSync(path.join(uiRoot, d.name, "src")),
  )
  .map((d) => d.name)
  .sort();

const self = fileURLToPath(import.meta.url);
const findings = [];
const lines = [];
for (const pkg of packages) {
  const child = spawnSync(process.execPath, [self, "--package", pkg], {
    cwd: uiRoot,
    encoding: "utf8",
    maxBuffer: 64 * 1024 * 1024,
  });
  const last = (child.stdout ?? "").trim().split("\n").pop() ?? "";
  let res;
  try {
    res = JSON.parse(last);
  } catch {
    res = {
      pkg,
      findings: [`${pkg}: процесс суда не отдал итога (код ${child.status}): ${(child.stderr ?? "").split("\n")[0]}`],
      line: `  ${pkg}: ОТКАЗ`,
    };
  }
  lines.push(res.line);
  findings.push(...res.findings);
}

console.log(`[F8-46] правило линта мест выпуска: пакетов консоли ${packages.length}`);
for (const l of lines) console.log(l);
if (packages.length === 0) findings.push("пакетов консоли 0 — обход не того корня");
if (findings.length > 0) {
  for (const f of findings) console.error(`::error::${f}`);
  process.exit(1);
}
console.log("находок 0: правило подключено в каждом пакете, подсадки краснеют, близнецы молчат, дерево чисто");
