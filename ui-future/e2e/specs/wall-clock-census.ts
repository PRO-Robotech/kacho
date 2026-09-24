// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { readFileSync, readdirSync, statSync } from "node:fs";
import path from "node:path";
import ts from "typescript";

/**
 * Перепись ожиданий СТЕННЫМИ ЧАСАМИ в коде набора (приёмка F8, Р9, F8-45).
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ПРЕДМЕТ
 *
 * Условие, которое служба создаёт временем, проба строит подстановкой
 * наблюдаемого ответа, а не пересиживанием окна (Р9). Без держателя правило —
 * проза: первая же оснастка вернула бы ожидание следующего шага одноразового
 * кода обратно, и вердикт «трижды подряд» снова зависел бы от фазы часов.
 *
 * ЧТО СЧИТАЕТСЯ НАХОДКОЙ — по узлам разбора, а не по тексту:
 *
 *   • вызов `….waitForTimeout(…)` — ожидание времени по определению;
 *   • СОН — обещание, разрешаемое `setTimeout`, — ВНЕ цикла. Такой сон ждёт,
 *     пока пройдёт время, и больше ничего.
 *
 * ЧТО НАХОДКОЙ НЕ ЯВЛЯЕТСЯ, И ПЕРЕПИСЬ ЭТО ПЕЧАТАЕТ (положительная сторона):
 *
 *   • ожидание УСЛОВИЯ — `expect.poll`, `toBeVisible`, `waitForResponse`,
 *     `waitForURL` и подобные: ждётся предмет, время лишь ограничивает;
 *   • пауза МЕЖДУ опросами — сон ВНУТРИ цикла, условие выхода которого
 *     предметное: пауза не ждёт срока, а не даёт опросу бить без передышки;
 *   • таймер, обрывающий обращение (`setTimeout(() => controller.abort(), …)`):
 *     он ничего не ждёт, а ограничивает;
 *   • `test.setTimeout(…)` — предел пробы, а не ожидание.
 *
 * Комментарий находкой не является: судится узел, а не строка.
 */

const SKIP = new Set(["node_modules", "playwright-report", "test-results", ".git"]);

export interface WallClockFinding {
  file: string;
  line: number;
  what: string;
}

export interface WallClockCensus {
  filesRead: number;
  /** Ожидания истечения срока стенными часами — находки. */
  findings: WallClockFinding[];
  /** Ожидания условия — положительная сторона предиката. */
  conditionWaits: number;
  /** Паузы между опросами условия — не находки. */
  pollPauses: number;
  /** Те же паузы с координатой: вторая группа переписи печатается поимённо (условие C22). */
  pauses: WallClockFinding[];
}

/** Методы, ожидающие УСЛОВИЯ, — их перепись находит и находками не считает. */
const CONDITION_WAITS = new Set([
  "poll",
  "toBeVisible",
  "toBeHidden",
  "toHaveURL",
  "toHaveText",
  "toContainText",
  "toHaveCount",
  "waitForResponse",
  "waitForRequest",
  "waitForURL",
  "waitForSelector",
  "waitForLoadState",
  "waitForEvent",
]);

function isLoop(node: ts.Node): boolean {
  return (
    ts.isForStatement(node) ||
    ts.isForOfStatement(node) ||
    ts.isForInStatement(node) ||
    ts.isWhileStatement(node) ||
    ts.isDoStatement(node)
  );
}

/** Ближайший цикл той же функции, в которой стоит узел; `null` — узел вне цикла. */
function enclosingLoop(node: ts.Node): ts.IterationStatement | null {
  for (let p = node.parent; p; p = p.parent) {
    if (isLoop(p)) return p as ts.IterationStatement;
    if (ts.isFunctionLike(p)) return null;
  }
  return null;
}

/** Есть ли в поддереве вызов — не заходя во вложенные функции. */
function containsCall(node: ts.Node): boolean {
  let found = false;
  const walk = (n: ts.Node) => {
    if (found || (n !== node && ts.isFunctionLike(n))) return;
    if (ts.isCallExpression(n)) found = true;
    ts.forEachChild(n, walk);
  };
  walk(node);
  return found;
}

/**
 * У цикла ПРЕДМЕТНЫЙ выход (условие C22): условие цикла спрашивает предмет
 * вызовом (`while (!collected() && …)`) либо тело выходит из цикла под
 * условием (`if (id) return id;`, `if (ok) break;`). Цикл без такого выхода —
 * не опрос, а отсчёт времени: пауза в нём и есть ожидание срока.
 */
function hasSubjectExit(loop: ts.IterationStatement): boolean {
  const condition =
    ts.isWhileStatement(loop) || ts.isDoStatement(loop)
      ? loop.expression
      : ts.isForStatement(loop)
        ? loop.condition
        : undefined;
  if (condition && containsCall(condition)) return true;
  let exits = false;
  const walk = (n: ts.Node, underIf: boolean) => {
    if (exits || ts.isFunctionLike(n)) return;
    if ((ts.isReturnStatement(n) || ts.isBreakStatement(n)) && underIf) exits = true;
    ts.forEachChild(n, (c) => walk(c, underIf || ts.isIfStatement(n)));
  };
  walk(loop.statement, false);
  return exits;
}

/** Выведена ли длительность из стенных часов: `Date.now()`, `new Date`, `performance.now()`, `.getTime()`. */
function wallClockDerived(duration: ts.Node | undefined): boolean {
  if (!duration) return false;
  let derived = false;
  const walk = (n: ts.Node) => {
    if (derived) return;
    if (ts.isNewExpression(n) && ts.isIdentifier(n.expression) && n.expression.text === "Date") derived = true;
    if (ts.isCallExpression(n) && ts.isPropertyAccessExpression(n.expression)) {
      const owner = n.expression.expression;
      const name = n.expression.name.text;
      if (name === "getTime") derived = true;
      if (name === "now" && ts.isIdentifier(owner) && (owner.text === "Date" || owner.text === "performance")) {
        derived = true;
      }
    }
    ts.forEachChild(n, walk);
  };
  walk(duration);
  return derived;
}

/**
 * `new Promise(… setTimeout(<разрешение>, <длительность>) …)` — сон, а не таймер
 * обрыва. Возвращает длительность сна (второй аргумент `setTimeout`) либо
 * `null` — узел сном не является.
 */
function sleepDuration(node: ts.NewExpression): { duration: ts.Node | undefined } | null {
  if (!ts.isIdentifier(node.expression) || node.expression.text !== "Promise") return null;
  const executor = node.arguments?.[0];
  if (!executor || !(ts.isArrowFunction(executor) || ts.isFunctionExpression(executor))) return null;
  const resolve = executor.parameters[0]?.name;
  if (!resolve || !ts.isIdentifier(resolve)) return null;
  let sleep: { duration: ts.Node | undefined } | null = null;
  const visit = (n: ts.Node) => {
    if (
      ts.isCallExpression(n) &&
      ts.isIdentifier(n.expression) &&
      n.expression.text === "setTimeout" &&
      n.arguments.length >= 1 &&
      ts.isIdentifier(n.arguments[0]) &&
      n.arguments[0].text === resolve.text
    ) {
      sleep = { duration: n.arguments[1] };
    }
    ts.forEachChild(n, visit);
  };
  visit(executor.body);
  return sleep;
}

/** Перепись ОДНОГО источника — её зовёт и обход дерева, и инъекция. */
export function censusOfSource(file: string, text: string): Omit<WallClockCensus, "filesRead"> {
  const source = ts.createSourceFile(file, text, ts.ScriptTarget.Latest, true, ts.ScriptKind.TS);
  const findings: WallClockFinding[] = [];
  const pauses: WallClockFinding[] = [];
  let conditionWaits = 0;
  const lineOf = (n: ts.Node) => source.getLineAndCharacterOfPosition(n.getStart(source)).line + 1;
  const visit = (n: ts.Node) => {
    if (ts.isCallExpression(n) && ts.isPropertyAccessExpression(n.expression)) {
      const name = n.expression.name.text;
      if (name === "waitForTimeout") findings.push({ file, line: lineOf(n), what: "waitForTimeout" });
      else if (CONDITION_WAITS.has(name)) conditionWaits++;
    }
    const sleep = ts.isNewExpression(n) ? sleepDuration(n) : null;
    if (sleep) {
      const loop = enclosingLoop(n);
      if (!loop) findings.push({ file, line: lineOf(n), what: "сон вне цикла" });
      else if (wallClockDerived(sleep.duration)) {
        findings.push({ file, line: lineOf(n), what: "сон до момента стенных часов" });
      } else if (!hasSubjectExit(loop)) {
        findings.push({ file, line: lineOf(n), what: "сон в цикле без предметного выхода" });
      } else pauses.push({ file, line: lineOf(n), what: "пауза между опросами условия" });
    }
    ts.forEachChild(n, visit);
  };
  visit(source);
  return { findings, conditionWaits, pollPauses: pauses.length, pauses };
}

function codeFiles(dir: string, acc: string[] = []): string[] {
  for (const name of readdirSync(dir)) {
    if (SKIP.has(name)) continue;
    const full = path.join(dir, name);
    if (statSync(full).isDirectory()) codeFiles(full, acc);
    else if (/\.(ts|mjs)$/.test(name)) acc.push(full);
  }
  return acc;
}

/** Перепись набора источников. Пустой набор — отказ: «0 находок» при 0 прочитанных не вердикт. */
export function wallClockCensusOf(sources: ReadonlyArray<{ file: string; text: string }>): WallClockCensus {
  if (sources.length === 0) {
    throw new Error("перепись ожиданий стенными часами прочитала 0 файлов — вердикта нет");
  }
  const total: WallClockCensus = {
    filesRead: sources.length,
    findings: [],
    conditionWaits: 0,
    pollPauses: 0,
    pauses: [],
  };
  for (const { file, text } of sources) {
    const one = censusOfSource(file, text);
    total.findings.push(...one.findings);
    total.conditionWaits += one.conditionWaits;
    total.pollPauses += one.pollPauses;
    total.pauses.push(...one.pauses);
  }
  return total;
}

/** Обход кода набора под `root`. */
export function wallClockCensus(root: string): WallClockCensus {
  return wallClockCensusOf(
    codeFiles(root).map((f) => ({ file: path.relative(root, f), text: readFileSync(f, "utf8") })),
  );
}
