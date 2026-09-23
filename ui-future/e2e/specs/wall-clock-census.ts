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
  findings: WallClockFinding[];
  /** Ожидания условия — положительная сторона предиката. */
  conditionWaits: number;
  /** Сны внутри циклов — паузы между опросами, не находки. */
  pollPauses: number;
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

/** Внутри цикла ли узел — не выходя за границу функции, которая его содержит. */
function insideLoopOfSameFunction(node: ts.Node): boolean {
  for (let p = node.parent; p; p = p.parent) {
    if (isLoop(p)) return true;
    if (ts.isFunctionLike(p)) return false;
  }
  return false;
}

/** `new Promise(… setTimeout(<разрешение>, …) …)` — сон, а не таймер обрыва. */
function isSleep(node: ts.NewExpression): boolean {
  if (!ts.isIdentifier(node.expression) || node.expression.text !== "Promise") return false;
  const executor = node.arguments?.[0];
  if (!executor || !(ts.isArrowFunction(executor) || ts.isFunctionExpression(executor))) return false;
  const resolve = executor.parameters[0]?.name;
  if (!resolve || !ts.isIdentifier(resolve)) return false;
  let sleeps = false;
  const visit = (n: ts.Node) => {
    if (
      ts.isCallExpression(n) &&
      ts.isIdentifier(n.expression) &&
      n.expression.text === "setTimeout" &&
      n.arguments.length >= 1 &&
      ts.isIdentifier(n.arguments[0]) &&
      n.arguments[0].text === resolve.text
    ) {
      sleeps = true;
    }
    ts.forEachChild(n, visit);
  };
  visit(executor.body);
  return sleeps;
}

/** Перепись ОДНОГО источника — её зовёт и обход дерева, и инъекция. */
export function censusOfSource(file: string, text: string): Omit<WallClockCensus, "filesRead"> {
  const source = ts.createSourceFile(file, text, ts.ScriptTarget.Latest, true, ts.ScriptKind.TS);
  const findings: WallClockFinding[] = [];
  let conditionWaits = 0;
  let pollPauses = 0;
  const lineOf = (n: ts.Node) => source.getLineAndCharacterOfPosition(n.getStart(source)).line + 1;
  const visit = (n: ts.Node) => {
    if (ts.isCallExpression(n) && ts.isPropertyAccessExpression(n.expression)) {
      const name = n.expression.name.text;
      if (name === "waitForTimeout") findings.push({ file, line: lineOf(n), what: "waitForTimeout" });
      else if (CONDITION_WAITS.has(name)) conditionWaits++;
    }
    if (ts.isNewExpression(n) && isSleep(n)) {
      if (insideLoopOfSameFunction(n)) pollPauses++;
      else findings.push({ file, line: lineOf(n), what: "сон вне цикла" });
    }
    ts.forEachChild(n, visit);
  };
  visit(source);
  return { findings, conditionWaits, pollPauses };
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
  const total: WallClockCensus = { filesRead: sources.length, findings: [], conditionWaits: 0, pollPauses: 0 };
  for (const { file, text } of sources) {
    const one = censusOfSource(file, text);
    total.findings.push(...one.findings);
    total.conditionWaits += one.conditionWaits;
    total.pollPauses += one.pollPauses;
  }
  return total;
}

/** Обход кода набора под `root`. */
export function wallClockCensus(root: string): WallClockCensus {
  return wallClockCensusOf(
    codeFiles(root).map((f) => ({ file: path.relative(root, f), text: readFileSync(f, "utf8") })),
  );
}
