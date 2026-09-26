// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { readFileSync, readdirSync, statSync } from "node:fs";
import path from "node:path";
import ts from "typescript";
import { isProviderAddressText } from "./provider-address";

export { isProviderAddressText };

/**
 * Статическая перепись обращений к ЧУЖОМУ поставщику личности — по узлам
 * разбора, а не по тексту (приёмка F8: F8-37/F8-38 о прод-файлах консоли,
 * F8-43 о коде набора сквозных проб).
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ЧТО СЧИТАЕТСЯ ОБРАЩЕНИЕМ
 *
 * Консоль обращается к поставщику тремя видами — переходом на адрес его потока,
 * запросом к его потоку и чтением его сессии — прямо либо через построитель его
 * адреса и его клиента. Все три вида пишутся в коде одними и теми же узлами:
 *
 *   • строковый литерал или часть шаблонной строки, несущие ФОРМУ адреса
 *     поставщика: путь под `/.ory/` и под `/oauth2` (в том числе с
 *     происхождением) и поверхность потоков — `/self-service/…`,
 *     `/sessions/whoami` — в любом месте литерала: база переопределяется при
 *     сборке, и распознаватель, знающий одну её запись, промолчал бы на другой;
 *   • чтение ручки базы поставщика — переменной окружения, названной его
 *     именем (`PROVIDER_KNOBS`);
 *   • импорт клиента поставщика — пакета его пространства имён (`@ory/…`).
 *
 * Комментарий находкой не является: судится узел, а не строка.
 *
 * ПОСТРОИТЕЛЬ АДРЕСА ОТДЕЛЬНОЙ ОСЬЮ НЕ СУДИТСЯ (#2874). Прежде вызов построителя
 * узнавался по ИМЕНИ (`PROVIDER_BUILDERS`), потому что адрес, который он
 * собирал, лежал в исключённом файле (`config.ts`), и литерал там находкой не
 * был. Ручка и построитель сняты вместе с исключением, и построитель адреса
 * поставщика теперь находка в месте своего литерала, где бы он ни стоял, —
 * вызывающим его файлам второй находки не нужно. Имя построителя и путь модуля
 * клиента (`lib/<имя поставщика>`) несут имя поставщика, а строку с этим именем
 * судит убывающий потолок привязок (kacho#2730): здесь второго места об этом
 * не заводится.
 *
 * ОТНЕСЁННОЕ ПО РЕФЕРЕНТУ. Найденное, что обращением не является, печатается
 * поимённо с основанием и находкой не считается — но ТОЛЬКО там, где названо.
 * Исключение, которому нечего исключать, — само находка: послабление не
 * переживает свой предмет.
 *
 * ПРЕДМЕТ ИСКЛЮЧЕНИЯ ЗАКРЕПЛЁН ПЕРЕЧНЕМ (F3 проверки круга 2). Исключение по
 * файлу прощало ВСЁ, что в файле появится: обёртка построителя, заведённая в
 * исключённом файле, и её вызов из прод-файла давали «обращений 0» — отнесённое
 * по референту молча росло 8 → 10. Поэтому исключение называет, ЧТО именно оно
 * относит (`covers`: файл и ВИД каждого найденного — без строки и без
 * значения), и перепись судит точное совпадение перечня: лишнее — новое
 * обращение внутри исключённого; недостающее — запись шире своего предмета.
 * Значения в перечне нет намеренно: оно несёт имя снимаемого издателя, чьи
 * привязки держит убывающий потолок (#2730), а перечень его не наращивает.
 * Цена названа: замена одного значения другим того же вида в исключённом
 * файле перечня не меняет — её судит прочтение исключённого файла на ревью.
 */

/**
 * Ручки базы поставщика — имена узлов. Ось держит ручку прокси сервера разработки
 * к внешнему экрану входа (исключение в `console-provider-not-addressed.test.ts`)
 * и снимается одним изменением с ней (остаток #2874).
 */
const PROVIDER_KNOBS = /^VITE_(KRATOS|HYDRA)_URL$|^KACHO_[A-Z_]*(KRATOS|HYDRA|ORY)[A-Z_]*$/;
/** Пакеты пространства имён поставщика. */
const PROVIDER_MODULE = /^@ory\//;

export interface ProviderFinding {
  file: string;
  line: number;
  /** Вид найденного без значения: «адрес поставщика», «ручка базы поставщика», … */
  kind: string;
  what: string;
  /** Имя теста набора, внутри которого стоит узел; пусто — вне теста. */
  test: string;
}

export interface ProviderCensus {
  filesRead: number;
  findings: ProviderFinding[];
  /** Найденное и отнесённое по референту — с основанием. */
  excused: Array<ProviderFinding & { reason: string }>;
  /** Исключения, которым нечего исключать. */
  staleExcuses: string[];
  /** Исключения, отнёсшие не ровно свой перечень: лишнее и недостающее поимённо. */
  excuseDrift: string[];
}

/**
 * Исключение по референту: файл (или его окончание пути), необязательно — тест
 * внутри него, и ТОЧНЫЙ перечень того, что оно относит.
 */
export interface Excuse {
  file: RegExp;
  test?: RegExp;
  reason: string;
  /** Что исключение относит: `«файл» «вид найденного»` на каждое место (`coverKey`). */
  covers: readonly string[];
}

/** Ключ найденного в перечне исключения: файл и вид — без строки и без значения. */
export function coverKey(f: Pick<ProviderFinding, "file" | "kind">): string {
  return `${f.file} ${f.kind}`;
}

/** Расхождение двух перечней как мультимножеств: что лишнее, чего недостаёт. */
function multisetDrift(actual: readonly string[], declared: readonly string[]): { extra: string[]; missing: string[] } {
  const left = new Map<string, number>();
  for (const k of declared) left.set(k, (left.get(k) ?? 0) + 1);
  const extra: string[] = [];
  for (const k of actual) {
    const n = left.get(k) ?? 0;
    if (n > 0) left.set(k, n - 1);
    else extra.push(k);
  }
  const missing = [...left.entries()].flatMap(([k, n]) => Array<string>(n).fill(k));
  return { extra, missing };
}

function lineOf(source: ts.SourceFile, node: ts.Node): number {
  return source.getLineAndCharacterOfPosition(node.getStart(source)).line + 1;
}

/** Имя теста `test("…", …)`, внутри которого стоит узел. */
function enclosingTest(node: ts.Node): string {
  for (let p = node.parent; p; p = p.parent) {
    if (
      ts.isCallExpression(p) &&
      ts.isIdentifier(p.expression) &&
      p.expression.text === "test" &&
      p.arguments.length > 0 &&
      ts.isStringLiteralLike(p.arguments[0])
    ) {
      return p.arguments[0].text;
    }
  }
  return "";
}

/** Находки ОДНОГО источника. */
export function providerAddressesIn(file: string, text: string): ProviderFinding[] {
  const kind = /\.tsx$/.test(file) ? ts.ScriptKind.TSX : ts.ScriptKind.TS;
  const source = ts.createSourceFile(file, text, ts.ScriptTarget.Latest, true, kind);
  const found: ProviderFinding[] = [];
  const push = (node: ts.Node, kind: string, value: string) =>
    found.push({ file, line: lineOf(source, node), kind, what: `${kind} ${value}`, test: enclosingTest(node) });
  const visit = (node: ts.Node) => {
    if (ts.isImportDeclaration(node) && ts.isStringLiteral(node.moduleSpecifier)) {
      if (PROVIDER_MODULE.test(node.moduleSpecifier.text))
        push(node, "импорт клиента поставщика", node.moduleSpecifier.text);
      return; // путь импорта — не адрес обращения
    }
    if (
      (ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node)) &&
      !ts.isImportDeclaration(node.parent)
    ) {
      if (isProviderAddressText(node.text)) push(node, "адрес поставщика", `«${node.text}»`);
      else if (PROVIDER_KNOBS.test(node.text)) push(node, "ручка базы поставщика", node.text);
    }
    if (ts.isTemplateExpression(node)) {
      const parts = [node.head.text, ...node.templateSpans.map((s) => s.literal.text)];
      if (parts.some((p) => isProviderAddressText(p)))
        push(node, "адрес поставщика в шаблоне", `«${parts.join("${…}")}»`);
    }
    if (ts.isPropertyAccessExpression(node) && ts.isIdentifier(node.name) && PROVIDER_KNOBS.test(node.name.text)) {
      push(node, "ручка базы поставщика", node.name.text);
    }
    ts.forEachChild(node, visit);
  };
  visit(source);
  // Одно место — одна находка: два узла одной строки с тем же найденным счёт не удваивают.
  const seen = new Set<string>();
  return found.filter((f) => {
    const key = `${f.line}:${f.what}`;
    if (seen.has(key)) return false;
    seen.add(key);
    return true;
  });
}

/** Перепись набора источников с исключениями по референту. Пустой набор — отказ. */
export function providerCensusOf(
  sources: ReadonlyArray<{ file: string; text: string }>,
  excuses: readonly Excuse[],
): ProviderCensus {
  if (sources.length === 0) throw new Error("перепись обращений к поставщику прочитала 0 файлов — вердикта нет");
  const census: ProviderCensus = {
    filesRead: sources.length,
    findings: [],
    excused: [],
    staleExcuses: [],
    excuseDrift: [],
  };
  const used = new Map<Excuse, string[]>();
  for (const { file, text } of sources) {
    for (const f of providerAddressesIn(file, text)) {
      const excuse = excuses.find((e) => e.file.test(f.file) && (e.test === undefined || e.test.test(f.test)));
      if (excuse) {
        used.set(excuse, [...(used.get(excuse) ?? []), coverKey(f)]);
        census.excused.push({ ...f, reason: excuse.reason });
      } else {
        census.findings.push(f);
      }
    }
  }
  const label = (e: Excuse) => `${e.file.source}${e.test ? ` · ${e.test.source}` : ""}`;
  census.staleExcuses = excuses.filter((e) => !used.has(e)).map((e) => `${label(e)} — ${e.reason}`);
  for (const [excuse, actual] of used) {
    const { extra, missing } = multisetDrift(actual, excuse.covers);
    if (extra.length === 0 && missing.length === 0) continue;
    census.excuseDrift.push(
      `${label(excuse)}: отнесено ${actual.length}, в перечне ${excuse.covers.length}` +
        (extra.length > 0 ? ` · сверх перечня: ${extra.join("; ")}` : "") +
        (missing.length > 0 ? ` · нет в дереве: ${missing.join("; ")}` : ""),
    );
  }
  return census;
}

const SKIP_DIRS = new Set([
  "node_modules",
  "dist",
  "build",
  "coverage",
  ".vite",
  ".turbo",
  "playwright-report",
  "test-results",
]);

/** Файлы кода под корнем; `accept` — какие относительные пути судятся. */
export function codeSources(root: string, accept: (rel: string) => boolean): Array<{ file: string; text: string }> {
  const out: Array<{ file: string; text: string }> = [];
  const walk = (dir: string) => {
    for (const name of readdirSync(dir)) {
      if (SKIP_DIRS.has(name) || name.startsWith(".")) continue;
      const full = path.join(dir, name);
      if (statSync(full).isDirectory()) walk(full);
      else if (/\.(ts|tsx|mjs)$/.test(name)) {
        const rel = path.relative(root, full).split(path.sep).join("/");
        if (accept(rel)) out.push({ file: rel, text: readFileSync(full, "utf8") });
      }
    }
  };
  walk(root);
  return out;
}

/** Строка находки с координатой. */
export function formatFinding(f: ProviderFinding): string {
  return `${f.file}:${f.line} ${f.what}${f.test ? ` [тест «${f.test}»]` : ""}`;
}
