// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import { readdirSync, readFileSync, statSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import ts from "typescript";

/**
 * Гейт: АДРЕС, ВЕДУЩИЙ НА ТУ ЖЕ СТРАНИЦУ, В КОНСОЛИ НЕ ОБЪЯВЛЯЕТСЯ (#1611).
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ЧТО ЛОВИТ
 *
 * Объявление ссылки с адресом `"#"`. Такой адрес — не ссылка: он ведёт на ту же
 * страницу, обещает переход, которого не существует, и обнаруживает это только
 * кликом. Оба места показа консоли это уже знают и рисуют темы ТЕКСТОМ, поэтому
 * адрес не читал никто — и ровно поэтому он опасен: поле формы ссылки
 * приглашает «починить» его одной живой строкой, после чего не произойдёт
 * ничего, потому что читателя нет.
 *
 * Замер на момент заведения: сорок четыре объявления, живых адресов ноль.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ЧЕГО ГЕЙТ НЕ ЗАПРЕЩАЕТ
 *
 * Он не запрещает ссылки на документацию — он запрещает МЁРТВЫЙ адрес. Появится
 * адрес сайта документации (сегодня его неоткуда взять: сайты собираются гейтом
 * конвейера, но не публикуются ни одним профилем развёртывания), и живой `href`
 * пройдёт молча. Проверяется это здесь же, законным близнецом.
 *
 * Якорь внутри страницы (`href="#section"`) — тоже законная форма и не
 * подпадает: он ведёт к месту на странице, а не в никуда. Запрещён ровно
 * вырожденный `"#"`.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ПОЧЕМУ РАЗБОР, А НЕ ПОИСК ПО ПОДСТРОКЕ
 *
 * Комментарии этого дерева обсуждают снятые адреса как урок — их в дереве шесть
 * абзацев, и они цитируют `href: "#"` дословно. Гейт по подстроке краснел бы на
 * собственном объяснении, и его сняли бы как непонятный. Разбор видит только
 * узлы значений.
 */

const consoleRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../..");
const SKIP_DIRS = new Set(["node_modules", "dist", "build", "coverage", ".vite", ".turbo", "playwright-report", "test-results"]);

function walk(dir: string, out: string[] = []): string[] {
  for (const e of readdirSync(dir, { withFileTypes: true })) {
    if (SKIP_DIRS.has(e.name) || e.name.startsWith(".")) continue;
    const full = path.join(dir, e.name);
    if (e.isDirectory()) walk(full, out);
    else if (/\.(ts|tsx)$/.test(e.name) && !/\.test\.tsx?$/.test(e.name)) out.push(full);
  }
  return out;
}

function consolePackages(): string[] {
  return readdirSync(consoleRoot)
    .filter((d) => {
      try {
        return statSync(path.join(consoleRoot, d, "src")).isDirectory();
      } catch {
        return false;
      }
    })
    .sort();
}

export interface DeadAddress {
  where: string;
  key: string;
}

/**
 * Объявления адреса, ведущего в никуда.
 *
 * Судятся ДВЕ формы записи, и распознаватель обязан знать обе: свойство объекта
 * (`href: "#"`) и атрибут разметки (`href="#"`). Знай он одну — вторая ушла бы
 * из наблюдения молча, оставаясь такой же живой.
 */
export function deadAddresses(source: string, rel = "fixture.tsx"): DeadAddress[] {
  const sf = ts.createSourceFile(rel, source, ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
  const out: DeadAddress[] = [];
  const at = (node: ts.Node) => `${rel}:${sf.getLineAndCharacterOfPosition(node.getStart(sf)).line + 1}`;
  const isDead = (v: string) => v.trim() === "#";
  const visit = (node: ts.Node): void => {
    if (ts.isPropertyAssignment(node) && ts.isStringLiteral(node.initializer) && isDead(node.initializer.text)) {
      const key = node.name.getText(sf).replace(/['"]/g, "");
      if (/^(href|url|link|to)$/i.test(key)) out.push({ where: at(node), key });
    }
    if (ts.isJsxAttribute(node) && node.initializer && ts.isStringLiteral(node.initializer) && isDead(node.initializer.text)) {
      const key = node.name.getText(sf);
      if (/^(href|to)$/i.test(key)) out.push({ where: at(node), key });
    }
    ts.forEachChild(node, visit);
  };
  visit(sf);
  return out;
}

const packages = consolePackages();
const files = packages.flatMap((p) => walk(path.join(consoleRoot, p, "src")));
const findings = files.flatMap((f) => deadAddresses(readFileSync(f, "utf8"), path.relative(consoleRoot, f)));

process.stdout.write(
  `\n  мёртвые адреса: пакетов ${packages.length}, файлов прочитано ${files.length}, ` +
    `объявлений адреса «#» ${findings.length}\n\n`,
);

describe("консоль не объявляет адрес, ведущий в никуда (#1611)", () => {
  it("перепись непуста: пустой обход — отказ, а не зелёное", () => {
    expect(packages.length).toBeGreaterThan(0);
    expect(files.length).toBeGreaterThan(0);
  });

  it("ни одного объявления адреса «#»", () => {
    expect(findings.map((f) => `${f.where}: ${f.key}: "#" — адрес ведёт на ту же страницу`)).toEqual([]);
  });

  it("ловит обе формы записи и молчит на законных близнецах", () => {
    // Инъекция в обе стороны. Без второй половины гейт ловил бы форму, а не
    // существо, и первый же ложный срабат его отключил бы.
    expect(deadAddresses(`const a = { label: "Тема", href: "#" };`).length).toBe(1);
    expect(deadAddresses(`const a = <a href="#">Тема</a>;`).length).toBe(1);

    // ЗАКОННЫЕ БЛИЗНЕЦЫ: живой адрес, якорь внутри страницы, чужой ключ и
    // проза, цитирующая снятое объявление.
    expect(deadAddresses(`const a = { href: "https://vpc.kacho.cloud/api/network" };`)).toEqual([]);
    expect(deadAddresses(`const a = <a href="#section">К разделу</a>;`)).toEqual([]);
    expect(deadAddresses(`const a = { placeholder: "#" };`)).toEqual([]);
    expect(deadAddresses(`// здесь стояло \`href: "#"\` — снято\nconst a = 1;`)).toEqual([]);
  });
});
