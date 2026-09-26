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
 *   1. подключение — действующая конфигурация файла пакета несёт правило: запрет
 *      транспортов и правило перехода документа (`kacho-issuance/document-navigation`);
 *   2. инъекция — каждая подсаженная форма выпуска мимо упорядочивающего
 *      транспорта даёт находку ЭТОГО правила; переход документа на путь края
 *      (`<a href>`, `<form action>`, `href` и `click()` и их варианты) — находку
 *      правила перехода, а не соседнего;
 *   3. близнецы — законный выпуск (`orderedTransport.fetch`), комментарий, текст,
 *      чужой член с похожим именем, те же переходы на путь консоли и на адрес
 *      объекта — молчание;
 *   4. дома — транспорт законен только в своём доме (`shared`: упорядочивающий
 *      транспорт для `fetch`, приёмник потока для `EventSource`); тот же путь в
 *      приложении домом не является;
 *   5. дерево — прод-файлы пакета (без проб и их оснастки) находок правила не
 *      дают; число прочитанных печатается, пустой обход — отказ.
 *
 * Сверх того, один раз на прогон: путь края у правила перехода и у переписи мест
 * выпуска (`shared/src/test/issuance-census.ts`) — ОДНО выражение. Сверяются узлы
 * разбора переписи, а не текст: два определения одного предмета, разошедшиеся молча,
 * дали бы держателю и подсказке разный путь края.
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

import * as ordering from "../shared/issuance-ordering.eslint.config.js";

/** Правило перехода документа на путь края — из того же набора, что запрет транспортов. */
const NAV_RULE = "kacho-issuance/document-navigation";
const RULES = new Set(["no-restricted-globals", "no-restricted-properties", "no-restricted-syntax", NAV_RULE]);
const PLANT = "src/__issuance_probe__.tsx";
/** Перепись мест выпуска — второе определение пути края; сверяется с правилом. */
const CENSUS = "shared/src/test/issuance-census.ts";

/**
 * Формы выпуска мимо упорядочивающего транспорта: каждая обязана дать находку.
 * Третий член — правило, чья находка обязательна: переход документа судит своё
 * правило, и находка соседнего его слепоты не прикрывает.
 */
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
  // Переход документа на путь края: три формы находки #2872 и их варианты записи.
  ["<a href> на путь края", 'export const A = () => <a href="/iam/v1/me">x</a>;', NAV_RULE],
  ["<form action> на путь края", 'export const F = () => <form action="/iam/v1/sessions" method="post" />;', NAV_RULE],
  [
    "href и click() на путь края",
    'const a = document.createElement("a");\na.href = "/iam/v1/me";\na.click();',
    NAV_RULE,
  ],
  [
    "голова шаблона на путь края",
    "declare const id: string;\nexport const A = () => <a href={`/vpc/v1/networks/${id}`}>x</a>;",
    NAV_RULE,
  ],
  ["путь края с происхождением", 'export const A = () => <a href="https://console.test/iam/v1/me">x</a>;', NAV_RULE],
  [
    "происхождение окна и путь края",
    "export const A = () => <a href={`${window.location.origin}/iam/v1/me`}>x</a>;",
    NAV_RULE,
  ],
  [
    "постоянная файла на путь края",
    'const EDGE = "/iam/v1/me";\nexport const A = () => <a href={EDGE}>x</a>;',
    NAV_RULE,
  ],
  [
    "formAction кнопки на путь края",
    'export const B = () => <button formAction="/iam/v1/sessions">x</button>;',
    NAV_RULE,
  ],
  ["setAttribute href на путь края", 'document.createElement("a").setAttribute("href", "/iam/v1/me");', NAV_RULE],
  [
    "action формы присвоением на путь края",
    'const f = document.createElement("form");\nf.action = "/iam/v1/sessions";\nf.submit();',
    NAV_RULE,
  ],
  ["location.assign на путь края", 'window.location.assign("/iam/v1/me");', NAV_RULE],
  ["псевдоним location на путь края", 'const loc = window.location;\nloc.replace("/iam/v1/auth/logout");', NAV_RULE],
  ["присвоение location.href на путь края", 'window.location.href = "/operations/op-1";', NAV_RULE],
  ["open() на путь края", 'void window.open("/iam/v1/me");', NAV_RULE],
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
  // Те же переходы, меняющие ровно один факт — адрес: путь консоли либо адрес объекта;
  // путь края вне места перехода; проп `action` компонента со значением-не-адресом.
  ["<a href> на путь консоли", 'export const A = () => <a href="/iam/users">x</a>;'],
  ["<form action> на путь консоли", 'export const F = () => <form action="/settings" method="post" />;'],
  ["href и click() на путь консоли", 'const a = document.createElement("a");\na.href = "/iam/users";\na.click();'],
  [
    "href и click() на адрес объекта",
    'declare const blob: Blob;\nconst a = document.createElement("a");\na.href = URL.createObjectURL(blob);\na.click();',
  ],
  ["путь края текстом, не адресом перехода", 'export const endpoint = "/iam/v1/me";'],
  [
    "action не формы",
    'declare const Shell: (p: { action: string }) => null;\nexport const S = () => <Shell action="edit" />;',
  ],
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
  const transports = Array.isArray(globalsRule) && globalsRule.slice(1).some((o) => o?.name === "fetch");
  if (!transports)
    findings.push(`${pkg}: правило мест выпуска НЕ подключено — действующая конфигурация ${PLANT} не запрещает fetch`);
  const navRule = cfg?.rules?.[NAV_RULE];
  const navigation = [2, "error"].includes(Array.isArray(navRule) ? navRule[0] : navRule);
  if (!navigation)
    findings.push(`${pkg}: правило перехода документа НЕ подключено — ${NAV_RULE} в конфигурации ${PLANT} не судит`);
  const wired = transports && navigation;

  // 2. Инъекция.
  let red = 0;
  for (const [name, code, rule] of RED) {
    const got = (await ours(code, PLANT)).filter((m) => rule === undefined || m.ruleId === rule);
    if (got.length === 0)
      findings.push(`${pkg}: подсаженная форма «${name}» находки ${rule ?? "правила"} не дала — правило слепо к ней`);
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

/**
 * Путь края — ОДНО выражение у правила перехода и у переписи мест выпуска: экспорт
 * правила (`EDGE_PATH`, `EDGE_ORIGIN`) против узлов разбора переписи — постоянной
 * `EDGE_PATH` и выражения происхождения в теле `isEdgePathText`.
 */
function edgePathAgreement(uiRoot) {
  const edge = ordering.EDGE_PATH;
  const origin = ordering.EDGE_ORIGIN;
  if (!(edge instanceof RegExp) || !(origin instanceof RegExp)) {
    return {
      findings: ["правило перехода не отдаёт пути края (EDGE_PATH, EDGE_ORIGIN) — сверять перепись не с чем"],
      line: "[F8-46] путь края: ОТКАЗ",
    };
  }
  let ts;
  let source;
  try {
    ts = createRequire(path.join(uiRoot, "shared", "package.json"))("typescript");
    const file = path.join(uiRoot, CENSUS);
    source = ts.createSourceFile(file, fs.readFileSync(file, "utf8"), ts.ScriptTarget.Latest, true, ts.ScriptKind.TS);
  } catch (e) {
    return {
      findings: [`${CENSUS}: перепись не разобрана — ${String(e?.message ?? e).split("\n")[0]}`],
      line: "[F8-46] путь края: ОТКАЗ",
    };
  }
  const lineOf = (node) => source.getLineAndCharacterOfPosition(node.getStart(source)).line + 1;
  const regexIn = (node, into) => {
    if (node.kind === ts.SyntaxKind.RegularExpressionLiteral) into.push({ text: node.text, line: lineOf(node) });
    ts.forEachChild(node, (child) => regexIn(child, into));
  };
  const paths = [];
  const origins = [];
  const visit = (node) => {
    if (
      ts.isVariableDeclaration(node) &&
      ts.isIdentifier(node.name) &&
      node.name.text === "EDGE_PATH" &&
      node.initializer
    )
      regexIn(node.initializer, paths);
    if (ts.isFunctionDeclaration(node) && node.name?.text === "isEdgePathText" && node.body)
      regexIn(node.body, origins);
    ts.forEachChild(node, visit);
  };
  visit(source);
  const findings = [];
  const agree = (what, found, own) => {
    if (found.length !== 1) {
      findings.push(`${CENSUS}: ${what} — выражений ${found.length}, ожидалось одно: сверять правило не с чем`);
      return false;
    }
    if (found[0].text !== String(own)) {
      findings.push(
        `${CENSUS}:${found[0].line} ${what} ${found[0].text} расходится с правилом перехода ${String(own)}`,
      );
      return false;
    }
    return true;
  };
  const same = [agree("путь края EDGE_PATH", paths, edge), agree("происхождение в isEdgePathText", origins, origin)];
  return {
    findings,
    line: `[F8-46] путь края: правило перехода и перепись — ${same.every(Boolean) ? "одно выражение" : "РАСХОДЯТСЯ"} (${String(edge)})`,
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

const agreement = edgePathAgreement(uiRoot);
findings.push(...agreement.findings);

console.log(`[F8-46] правило линта мест выпуска: пакетов консоли ${packages.length}`);
for (const l of lines) console.log(l);
console.log(agreement.line);
if (packages.length === 0) findings.push("пакетов консоли 0 — обход не того корня");
if (findings.length > 0) {
  for (const f of findings) console.error(`::error::${f}`);
  process.exit(1);
}
console.log(
  "находок 0: правило подключено в каждом пакете, подсадки краснеют, близнецы молчат, дерево чисто, путь края один",
);
