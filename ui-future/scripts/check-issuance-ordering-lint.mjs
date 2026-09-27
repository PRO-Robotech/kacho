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
 *      транспорта даёт находку ЭТОГО правила; переход документа на путь края —
 *      КАЖДАЯ форма, названная в шапке правила, и каждая форма записи адреса —
 *      находку правила перехода, а не соседнего;
 *   3. близнецы — у каждой формы перехода близнец из ТОЙ ЖЕ записи, где подставлен
 *      путь консоли вместо пути края: один факт, и отличие проверяется тем же
 *      выражением пути края. Мутант, судящий форму без адреса, краснеет близнецом,
 *      слепой к форме — подсадкой; дерево для этого не нужно. Сверх того молчат
 *      законный выпуск (`orderedTransport.fetch`), комментарий, текст, чужой член
 *      с похожим именем, адрес объекта, `open()` хранилища. Граница «чужое
 *      происхождение с путём формы края судится краем» закреплена такой же парой;
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

/** Путь края и путь консоли — единственный факт, которым подсадка перехода отличается от близнеца. */
const EDGE = ["/iam/v1/me", "/iam/users"];
const TMPL = "`";
const SUBST = "$" + "{";

/**
 * Переход документа на путь края: каждая форма — ПАРА. Запись одна, адрес подставляется:
 * путь края — подсадка, обязана дать находку правила перехода (находка соседнего правила
 * его слепоты не прикрывает); путь консоли — близнец, обязан молчать. Близнец меняет
 * ровно один факт — адрес — by construction: мутант, судящий форму без адреса, краснеет
 * близнецом, мутант, слепой к форме, — подсадкой. Третий член — пара адресов, если
 * форма требует своих.
 */
const NAV = [
  // Атрибут элемента.
  ["<a href>", (a) => `export const A = () => <a href="${a}">x</a>;`],
  ["<form action>", (a) => `export const F = () => <form action="${a}" method="post" />;`, ["/iam/v1/sessions", "/settings"]],
  ["<button formAction>", (a) => `export const B = () => <button formAction="${a}">x</button>;`],
  ["<a xlinkHref> в svg", (a) => `export const U = () => <svg><a xlinkHref="${a}">x</a></svg>;`],
  // Вложенный документ и загрузка по адресу.
  ["<iframe src>", (a) => `export const I = () => <iframe src="${a}" title="x" />;`],
  ["<object data>", (a) => `export const O = () => <object data="${a}" />;`],
  ["<img src>", (a) => `export const P = () => <img src="${a}" alt="" />;`],
  // Присвоение члену.
  ["href и click()", (a) => `const a = document.createElement("a");\na.href = "${a}";\na.click();`],
  ["action формы и submit()", (a) => `const f = document.createElement("form");\nf.action = "${a}";\nf.submit();`, ["/iam/v1/sessions", "/settings"]],
  ["formAction кнопки присвоением", (a) => `const b = document.createElement("button");\nb.formAction = "${a}";\nb.click();`],
  ["src кадра присвоением", (a) => `const i = document.createElement("iframe");\ni.src = "${a}";\ndocument.body.append(i);`],
  ["data объекта присвоением", (a) => `const o = document.createElement("object");\no.data = "${a}";\ndocument.body.append(o);`],
  // setAttribute.
  ["setAttribute href", (a) => `document.createElement("a").setAttribute("href", "${a}");`],
  ["setAttribute src кадра", (a) => `document.createElement("iframe").setAttribute("src", "${a}");`],
  [
    "setAttributeNS xlink:href",
    (a) => `document.createElementNS("http://www.w3.org/2000/svg", "a").setAttributeNS("http://www.w3.org/1999/xlink", "xlink:href", "${a}");`,
  ],
  ["setAttribute.call", (a) => `const a = document.createElement("a");\na.setAttribute.call(a, "href", "${a}");`],
  // location.
  ["location.assign", (a) => `window.location.assign("${a}");`],
  ["псевдоним location и replace", (a) => `const loc = window.location;\nloc.replace("${a}");`],
  ["псевдоним location.assign", (a) => `const go = window.location.assign;\ngo("${a}");`],
  ["присвоение location.href", (a) => `window.location.href = "${a}";`, ["/operations/op-1", "/vpc/operations"]],
  ["присвоение location", (a) => `window.location = "${a}";`],
  ["присвоение location.pathname", (a) => `window.location.pathname = "${a}";`],
  // open.
  ["open()", (a) => `void window.open("${a}");`],
  ["псевдоним open", (a) => `const o = window.open;\nvoid o("${a}");`],
  ["open.call", (a) => `void window.open.call(window, "${a}");`],
  ["open.apply", (a) => `void window.open.apply(window, ["${a}"]);`],
  // Запись адреса: на `<a href>` как носителе.
  [
    "голова шаблона",
    (a) => `declare const id: string;\nexport const A = () => <a href={${TMPL}${a}${SUBST}id}${TMPL}}>x</a>;`,
    ["/vpc/v1/networks/", "/vpc/networks/"],
  ],
  ["постоянная файла", (a) => `const P = "${a}";\nexport const A = () => <a href={P}>x</a>;`],
  [
    "левое плечо сцепления",
    (a) => `declare const id: string;\nexport const A = () => <a href={"${a}" + id}>x</a>;`,
    ["/vpc/v1/networks/", "/vpc/networks/"],
  ],
  ["ветвь ?:", (a) => `declare const c: boolean;\nexport const A = () => <a href={c ? "/settings" : "${a}"}>x</a>;`],
  ["правое плечо ??", (a) => `declare const h: string | undefined;\nexport const A = () => <a href={h ?? "${a}"}>x</a>;`],
  [
    "происхождение окна в шаблоне",
    (a) => `export const A = () => <a href={${TMPL}${SUBST}window.location.origin}${a}${TMPL}}>x</a>;`,
  ],
  ["происхождение окна сцеплением", (a) => `export const A = () => <a href={window.location.origin + "${a}"}>x</a>;`],
  ["записанное происхождение", (a) => `export const A = () => <a href="https://console.test${a}">x</a>;`],
  ["происхождение без схемы", (a) => `export const A = () => <a href="//console.test${a}">x</a>;`],
  // ГРАНИЦА, названная в шапке правила: судится форма пути после ЛЮБОГО происхождения,
  // своё происхождение консоли по записи не известно. Пара закрепляет это: чужое
  // происхождение с путём формы края краснеет, с путём другой формы — молчит.
  [
    "чужое происхождение, путь формы края (граница)",
    (a) => `export const A = () => <a href="https://kubernetes.io${a}">x</a>;`,
    ["/docs/v1/", "/docs/"],
  ],
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
  // Не-адрес в месте перехода и путь края вне места перехода; близнецы путём консоли — в NAV.
  [
    "href и click() на адрес объекта",
    'declare const blob: Blob;\nconst a = document.createElement("a");\na.href = URL.createObjectURL(blob);\na.click();',
  ],
  ["путь края текстом, не адресом перехода", 'export const endpoint = "/iam/v1/me";'],
  [
    "action не формы",
    'declare const Shell: (p: { action: string }) => null;\nexport const S = () => <Shell action="edit" />;',
  ],
  ["open() хранилища, не окна", 'export const db = indexedDB.open("kacho-dpop", 1);'],
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
  for (const [name, code] of RED) {
    const got = await ours(code, PLANT);
    if (got.length === 0) findings.push(`${pkg}: подсаженная форма «${name}» находки правила не дала — правило слепо к ней`);
    else red += 1;
  }
  // 2а. Переход документа: подсадка на путь края и близнец на путь консоли — одна запись.
  let navRed = 0;
  for (const [name, form, [edge, own] = EDGE] of NAV) {
    const planted = await ours(form(edge), PLANT);
    if (!planted.some((m) => m.ruleId === NAV_RULE))
      findings.push(`${pkg}: подсаженная форма «${name}» на путь края находки ${NAV_RULE} не дала — правило слепо к ней`);
    else navRed += 1;
    const twin = await ours(form(own), PLANT);
    if (twin.length > 0)
      findings.push(
        `${pkg}: законный близнец «${name}» на путь консоли дал находку: ${twin.map((m) => m.message.slice(0, 60)).join("; ")}`,
      );
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
    line: `  ${pkg}: подключено ${wired ? "да" : "НЕТ"} · инъекций красных ${red}/${RED.length} · переходов красных ${navRed}/${NAV.length}, их близнецов ${NAV.length} · прочих близнецов ${TWINS.length} · домов ${homes} · прод-файлов ${files.length}, находок ${inTree}`,
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

// Пара перехода различает ровно факт адреса: первый — путь края, второй — нет, тем же
// выражением, что у правила. Пара, где оба адреса одной стороны, близнецом не служит.
const edgeText = (t) => ordering.EDGE_PATH.test(t.replace(ordering.EDGE_ORIGIN, ""));
for (const [name, form, [edge, own] = EDGE] of NAV) {
  if (!edgeText(edge) || edgeText(own) || form(edge) === form(own))
    findings.push(`пара «${name}»: «${edge}» и «${own}» не различают путь края и путь консоли — близнецом не служит`);
}

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
