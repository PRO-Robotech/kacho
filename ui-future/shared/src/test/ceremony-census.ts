// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import ts from "typescript";

/**
 * Статические переписи дерева консоли о церемониях личности (приёмка F8,
 * условия C3, C7, C14, C17). Судят УЗЛЫ разбора — строковые литералы,
 * обращения к членам, вызовы, объявления импорта, — а не текст: комментарий,
 * называющий предмет, находкой не является.
 *
 * Каждая перепись принимает набор источников и возвращает находки с
 * координатой; обход дерева и печать знаменателя делает проба, инъекции зовут
 * те же функции на подсаженном тексте.
 */

export interface Source {
  file: string;
  text: string;
}

export interface Finding {
  file: string;
  line: number;
  what: string;
}

export function formatCensusFinding(f: Finding): string {
  return `${f.file}:${f.line} ${f.what}`;
}

function parse(src: Source): ts.SourceFile {
  const kind = src.file.endsWith(".tsx") ? ts.ScriptKind.TSX : ts.ScriptKind.TS;
  return ts.createSourceFile(src.file, src.text, ts.ScriptTarget.Latest, true, kind);
}

function lineOf(sf: ts.SourceFile, node: ts.Node): number {
  return sf.getLineAndCharacterOfPosition(node.getStart(sf)).line + 1;
}

function visit(sf: ts.SourceFile, fn: (n: ts.Node) => void) {
  const walk = (n: ts.Node) => {
    fn(n);
    ts.forEachChild(n, walk);
  };
  walk(sf);
}

function literalText(n: ts.Node): string | null {
  if (ts.isStringLiteral(n) || ts.isNoSubstitutionTemplateLiteral(n)) return n.text;
  if (ts.isTemplateHead(n) || ts.isTemplateMiddle(n) || ts.isTemplateTail(n)) return n.text;
  return null;
}

// ─── C7 · читатель ответа края о сессии один ──────────────────────────────────

/** Путь ответа края «кто за этой сессией». */
export const SESSION_IDENTITY_ADDRESS = "/iam/v1/auth/me";

/** Литералы, несущие путь ответа края о сессии: кто его ЧИТАЕТ мимо единственного читателя. */
export function sessionAddressReaders(sources: readonly Source[]): Finding[] {
  const out: Finding[] = [];
  for (const src of sources) {
    const sf = parse(src);
    visit(sf, (n) => {
      const t = literalText(n);
      if (t !== null && (t === SESSION_IDENTITY_ADDRESS || t.startsWith(`${SESSION_IDENTITY_ADDRESS}?`))) {
        out.push({ file: src.file, line: lineOf(sf, n), what: `адрес ответа края о сессии «${t}»` });
      }
    });
  }
  return out;
}

/** Вызовы единственного читателя — стражи и прочие спрашивающие «есть ли сессия». */
export function sessionPredicateCallers(sources: readonly Source[], name = "sessionIdentity"): Finding[] {
  const out: Finding[] = [];
  for (const src of sources) {
    const sf = parse(src);
    visit(sf, (n) => {
      if (ts.isCallExpression(n) && ts.isIdentifier(n.expression) && n.expression.text === name) {
        out.push({ file: src.file, line: lineOf(sf, n), what: `спрашивает «есть ли сессия» (${name})` });
      }
    });
  }
  return out;
}

// ─── C17 · секрет заведения и запасные коды не оседают в браузере ─────────────

/** Хранилища и журнал, в которые экран церемонии ничего не кладёт. */
const SINKS: ReadonlyMap<string, string> = new Map([
  ["localStorage", "постоянное хранилище браузера"],
  ["sessionStorage", "хранилище вкладки"],
  ["indexedDB", "база браузера"],
  ["caches", "кэш служебного работника"],
  ["console", "журнал консоли браузера"],
]);

/** Методы, пишущие адрес страницы: секрет в адресе оседает в истории и журналах раздачи. */
const ADDRESS_WRITERS = new Set(["pushState", "replaceState"]);

/** Кэш запросов: у экрана церемонии его нет — ответ с секретом пережил бы уход с экрана. */
const QUERY_CACHE_HOOKS = new Set(["useQuery", "useQueries", "useSuspenseQuery", "useInfiniteQuery"]);

/**
 * Обращается ли идентификатор к глобальному объекту браузера: голое имя
 * (`localStorage`) или член окна (`window.localStorage`). Имя свойства ЧУЖОГО
 * объекта (`props.console`) и имя в объявлении обращением не являются.
 */
function refersToGlobal(n: ts.Identifier): boolean {
  const p = n.parent;
  if (!p) return true;
  if (ts.isPropertyAccessExpression(p) && p.name === n) {
    return ts.isIdentifier(p.expression) && ["window", "globalThis", "self"].includes(p.expression.text);
  }
  if (
    (ts.isPropertyAssignment(p) ||
      ts.isPropertySignature(p) ||
      ts.isVariableDeclaration(p) ||
      ts.isParameter(p) ||
      ts.isImportSpecifier(p) ||
      ts.isBindingElement(p)) &&
    (p as { name?: ts.Node }).name === n
  ) {
    return false;
  }
  return true;
}

export function ceremonySecretSinks(sources: readonly Source[]): Finding[] {
  const out: Finding[] = [];
  for (const src of sources) {
    const sf = parse(src);
    visit(sf, (n) => {
      if (ts.isIdentifier(n) && SINKS.has(n.text) && refersToGlobal(n)) {
        out.push({ file: src.file, line: lineOf(sf, n), what: SINKS.get(n.text)! });
      }
      if (ts.isPropertyAccessExpression(n) && ADDRESS_WRITERS.has(n.name.text)) {
        out.push({ file: src.file, line: lineOf(sf, n), what: `запись адреса страницы (${n.name.text})` });
      }
      if (ts.isCallExpression(n) && ts.isIdentifier(n.expression) && QUERY_CACHE_HOOKS.has(n.expression.text)) {
        out.push({ file: src.file, line: lineOf(sf, n), what: `кэш запросов (${n.expression.text})` });
      }
    });
  }
  return out;
}

// ─── C14 · хранилища браузера и чьи они ───────────────────────────────────────

const STORES = new Set(["localStorage", "sessionStorage", "indexedDB"]);

/** Прод-файлы, обращающиеся к хранилищу браузера, — по узлу обращения. */
export function browserStoreUsers(sources: readonly Source[]): Map<string, Finding[]> {
  const out = new Map<string, Finding[]>();
  for (const src of sources) {
    const sf = parse(src);
    visit(sf, (n) => {
      if (!ts.isIdentifier(n) || !STORES.has(n.text) || !refersToGlobal(n)) return;
      const list = out.get(src.file) ?? [];
      list.push({ file: src.file, line: lineOf(sf, n), what: n.text });
      out.set(src.file, list);
    });
  }
  return out;
}

// ─── C3 · разборщик отказа один у экранов и у распознавателя набора ───────────

/** Импортирует ли источник имя `name` из модуля, чей путь оканчивается на `moduleTail`. */
export function importsFrom(src: Source, name: string, moduleTail: string): boolean {
  const sf = parse(src);
  let found = false;
  visit(sf, (n) => {
    if (!ts.isImportDeclaration(n) || !ts.isStringLiteral(n.moduleSpecifier)) return;
    if (!n.moduleSpecifier.text.endsWith(moduleTail)) return;
    const named = n.importClause?.namedBindings;
    if (named && ts.isNamedImports(named) && named.elements.some((e) => e.name.text === name)) found = true;
  });
  return found;
}

/** Разбирает ли функция `fn` JSON сама — второй разборщик того же тела. */
export function functionParsesJsonItself(src: Source, fn: string): boolean {
  const sf = parse(src);
  let parses = false;
  visit(sf, (n) => {
    if (!ts.isFunctionDeclaration(n) || n.name?.text !== fn || !n.body) return;
    const inner = (m: ts.Node) => {
      if (
        ts.isCallExpression(m) &&
        ts.isPropertyAccessExpression(m.expression) &&
        ts.isIdentifier(m.expression.expression) &&
        m.expression.expression.text === "JSON" &&
        m.expression.name.text === "parse"
      ) {
        parses = true;
      }
      ts.forEachChild(m, inner);
    };
    inner(n.body);
  });
  return parses;
}

// ─── C21 · глаголы полосы из кода набора зовёт только выпускающий с записью ───

/** Методы контекста запросов playwright. */
const REQUEST_METHODS = new Set(["get", "post", "put", "patch", "delete", "head", "fetch"]);

/** Адрес — глагол полосы формы: путь `/iam/v1/auth/*`, кроме ответа края о сессии. */
function isLaneVerbAddress(n: ts.Node): string | null {
  const t = literalText(n);
  if (t !== null) {
    return t.startsWith("/iam/v1/auth/") && t !== SESSION_IDENTITY_ADDRESS ? t : null;
  }
  // `LANE.<глагол>` — перечень глаголов посева.
  if (ts.isPropertyAccessExpression(n) && ts.isIdentifier(n.expression) && n.expression.text === "LANE") {
    return `LANE.${n.name.text}`;
  }
  if (ts.isTemplateExpression(n) && n.head.text.startsWith("/iam/v1/auth/")) return n.head.text;
  return null;
}

/**
 * Обращения КОНТЕКСТОМ ЗАПРОСОВ (`page.request`, `context.request`, контекст
 * `request.newContext()`) к глаголам полосы мимо выпускающего `issuer`. Отказы
 * таких обращений не видит ни перепись страниц (N16), ни запись выпускающего —
 * значит сторож бюджета оси источника (F8-41) считал бы их меньше истинного
 * (условие C21). Выпускающий — файл, пишущий свою запись (`ceremony-seed.ts`).
 */
export function laneCallsOutsideIssuer(sources: readonly Source[], issuer: string): Finding[] {
  const out: Finding[] = [];
  for (const src of sources) {
    if (src.file === issuer) continue;
    const sf = parse(src);
    visit(sf, (n) => {
      if (!ts.isCallExpression(n) || !ts.isPropertyAccessExpression(n.expression)) return;
      if (!REQUEST_METHODS.has(n.expression.name.text)) return;
      const owner = n.expression.expression;
      // `x.request.<метод>(…)` либо контекст запросов под именем `api`/`request`.
      const viaRequest =
        (ts.isPropertyAccessExpression(owner) && owner.name.text === "request") ||
        (ts.isIdentifier(owner) && ["request", "api"].includes(owner.text));
      if (!viaRequest || n.arguments.length === 0) return;
      const address = isLaneVerbAddress(n.arguments[0]);
      if (address)
        out.push({ file: src.file, line: lineOf(sf, n), what: `глагол полосы мимо выпускающего: ${address}` });
    });
  }
  return out;
}
