// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import ts from "typescript";

/**
 * Перепись МЕСТ ВЫПУСКА обращений консоли к краю — по узлам разбора, а не по
 * тексту (приёмка F8, Р10, §11.1, N18).
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ЗАЧЕМ
 *
 * Упорядочение вокруг глагола, ставящего носитель, держит модульная проба — но
 * она судит упорядочивающий транспорт, а не то, что через него идёт КАЖДОЕ
 * обращение вкладки. Место выпуска мимо транспорта глагол не видит: его чтение
 * в полёте не отменяется, и ответ края на прежний носитель гасит новый. Мест
 * выпуска до упорядочения было девять в семи файлах, и один браузерный путь
 * этого не держит.
 *
 * ЧТО СЧИТАЕТСЯ МЕСТОМ ВЫПУСКА
 *
 *   • вызов транспорта браузера: `fetch(…)` (и `window.fetch`,
 *     `globalThis.fetch`, `self.fetch`), `sendBeacon(…)`, построение
 *     `EventSource`, `XMLHttpRequest`, `WebSocket`;
 *   • переход документа на путь края: `location.assign/replace(…)`,
 *     `window.open(…)`, присвоение `location`/`location.href`, чей адрес —
 *     литерал или голова шаблона с путём края.
 *
 * Место внутри упорядочивающего транспорта находкой не является — это и есть
 * транспорт, и перепись обязана его ВИДЕТЬ (иначе «ноль находок» не отличим от
 * «распознаватель слеп»). Всё прочее — находка, кроме названного поимённо с
 * основанием; исключение, которому нечего исключать, — само находка.
 * Комментарий находкой не является: судится узел.
 */

/** Упорядочивающий транспорт — единственный законный дом вызова транспорта. */
export const ORDERING_TRANSPORT = "shared/src/api/carrier-order.ts";

/** Путь края: API доменов (`/<домен>/v<N>/…`), операции, проверка живости края. */
const EDGE_PATH = /^\/(?:[a-z][a-z0-9-]*\/v\d+(?:[/?]|$)|operations(?:[/?]|$)|healthz$)/;

/** Адрес — путь края, в том числе с происхождением перед ним. */
export function isEdgePathText(text: string): boolean {
  return EDGE_PATH.test(text.replace(/^[a-z][a-z0-9+.-]*:\/\/[^/]+/i, ""));
}

const GLOBALS = new Set(["window", "globalThis", "self"]);
const CONSTRUCTED = new Set(["EventSource", "XMLHttpRequest", "WebSocket"]);

export interface IssuanceFinding {
  file: string;
  line: number;
  what: string;
}

export interface IssuanceExcuse {
  file: string;
  what: RegExp;
  reason: string;
}

export interface IssuanceCensus {
  filesRead: number;
  /** Места выпуска внутри упорядочивающего транспорта. */
  transport: IssuanceFinding[];
  findings: IssuanceFinding[];
  excused: Array<IssuanceFinding & { reason: string }>;
  staleExcuses: string[];
}

function lineOf(source: ts.SourceFile, node: ts.Node): number {
  return source.getLineAndCharacterOfPosition(node.getStart(source)).line + 1;
}

/** Статическая голова адреса: литерал целиком либо голова шаблона. */
function addressHead(node: ts.Expression | undefined): string | null {
  if (!node) return null;
  if (ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node)) return node.text;
  if (ts.isTemplateExpression(node)) return node.head.text;
  return null;
}

/** `location`, `window.location`, `document.location`, `globalThis.location`. */
function isLocation(node: ts.Expression): boolean {
  if (ts.isIdentifier(node)) return node.text === "location";
  return ts.isPropertyAccessExpression(node) && node.name.text === "location";
}

/** Имя вызываемого транспорта; `null` — вызов не транспорта. */
function transportCallee(callee: ts.Expression): string | null {
  if (ts.isIdentifier(callee) && (callee.text === "fetch" || callee.text === "sendBeacon")) return callee.text;
  if (ts.isPropertyAccessExpression(callee)) {
    const name = callee.name.text;
    if (name === "fetch" && ts.isIdentifier(callee.expression) && GLOBALS.has(callee.expression.text)) {
      return `${callee.expression.text}.fetch`;
    }
    if (name === "sendBeacon") return "sendBeacon";
  }
  return null;
}

/** Места выпуска ОДНОГО источника. */
export function issuancesIn(file: string, text: string): IssuanceFinding[] {
  const kind = /\.tsx$/.test(file) ? ts.ScriptKind.TSX : ts.ScriptKind.TS;
  const source = ts.createSourceFile(file, text, ts.ScriptTarget.Latest, true, kind);
  const found: IssuanceFinding[] = [];
  const push = (node: ts.Node, what: string) => found.push({ file, line: lineOf(source, node), what });
  const visit = (node: ts.Node) => {
    if (ts.isCallExpression(node)) {
      const transport = transportCallee(node.expression);
      if (transport) push(node, `вызов транспорта ${transport}`);
      const callee = node.expression;
      if (ts.isPropertyAccessExpression(callee)) {
        const name = callee.name.text;
        const navigates =
          ((name === "assign" || name === "replace") && isLocation(callee.expression)) ||
          (name === "open" && ts.isIdentifier(callee.expression) && GLOBALS.has(callee.expression.text));
        const head = addressHead(node.arguments[0]);
        if (navigates && head !== null && isEdgePathText(head)) push(node, `переход документа на путь края «${head}»`);
      }
    }
    if (ts.isNewExpression(node) && ts.isIdentifier(node.expression) && CONSTRUCTED.has(node.expression.text)) {
      push(node, `построение ${node.expression.text}`);
    }
    if (ts.isBinaryExpression(node) && node.operatorToken.kind === ts.SyntaxKind.EqualsToken) {
      const target = node.left;
      const assignsLocation =
        isLocation(target) || (ts.isPropertyAccessExpression(target) && target.name.text === "href" && isLocation(target.expression));
      const head = addressHead(node.right);
      if (assignsLocation && head !== null && isEdgePathText(head)) push(node, `переход документа на путь края «${head}»`);
    }
    ts.forEachChild(node, visit);
  };
  visit(source);
  return found;
}

/** Перепись набора источников. Пустой набор — отказ: вердикта нет. */
export function issuanceCensusOf(
  sources: ReadonlyArray<{ file: string; text: string }>,
  excuses: readonly IssuanceExcuse[],
): IssuanceCensus {
  if (sources.length === 0) throw new Error("перепись мест выпуска прочитала 0 файлов — вердикта нет");
  const census: IssuanceCensus = { filesRead: sources.length, transport: [], findings: [], excused: [], staleExcuses: [] };
  const used = new Set<IssuanceExcuse>();
  for (const { file, text } of sources) {
    for (const f of issuancesIn(file, text)) {
      if (file === ORDERING_TRANSPORT) {
        census.transport.push(f);
        continue;
      }
      const excuse = excuses.find((e) => e.file === f.file && e.what.test(f.what));
      if (excuse) {
        used.add(excuse);
        census.excused.push({ ...f, reason: excuse.reason });
      } else {
        census.findings.push(f);
      }
    }
  }
  census.staleExcuses = excuses.filter((e) => !used.has(e)).map((e) => `${e.file} · ${e.what.source} — ${e.reason}`);
  return census;
}

/** Строка находки с координатой. */
export function formatIssuance(f: IssuanceFinding): string {
  return `${f.file}:${f.line} ${f.what}`;
}
