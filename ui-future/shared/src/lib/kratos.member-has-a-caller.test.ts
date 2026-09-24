// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// КЛИЕНТ ЧУЖОЙ СЛУЖБЫ ЛИЧНОСТИ НЕ ДЕРЖИТ ЧЛЕНОВ БЕЗ ВЫЗЫВАЮЩЕГО (#2733).
//
// ПРЕДМЕТ. `shared/src/lib/kratos.ts` — единственная дверь консоли к потокам
// прежнего поставщика личности. Дверь эта временная: полоса входа продукта уже
// объявлена краем тринадцатью глаголами `/iam/v1/auth/*`, и клиент уйдёт вместе
// с экранами входа (#1274). Пока он стоит, он обязан объявлять РОВНО то, что
// консоль действительно зовёт.
//
// Почему это не косметика. Необойдённый член клиента объявляет зависимость,
// которой у продукта нет: по нему читают, чего консоль «умеет», по нему же
// заводят ручки развёртывания (так `config.webauthnRpId` и появился — ради
// обёртки, которую не звал никто, при том что вызов ключа доступа консоль берёт
// ИЗ УЗЛА ПОТОКА, а не из ручки). Перечень снимаемого при этом обязан
// СОКРАЩАТЬСЯ сам: член, дописанный без вызывающего, краснеет здесь же.
//
// ПРЕДИКАТ. Член — разбором объявления: экспортируемая функция файла и метод
// объекта `kratos`. Вызывающий — упоминание имени в дереве продукта ВНЕ самого
// файла: `kratos.<член>` для метода, `<член>(` либо именованный импорт для
// функции. Пробы из корпуса исключены намеренно: заглушка `jest.fn()` с тем же
// именем — не вызывающий, и прежде ровно она удержала бы `getFlow`.
//
// Вызов СОСЕДНИМ членом (`this.<член>(`) считается вызовом: судится объявление
// без вызывающего, а не объявление без вызывающего снаружи. Без этой стороны
// гейт краснел бы на `initFlowUrl`, которую зовут `loginUrl` и `settingsUrl`, —
// то есть на члене, у которого вызывающий есть, и находка была бы ложной.
//
// ПУСТОЙ ОБХОД — ПАДЕНИЕ: перечень членов, прочитанный пустым, значит, что гейт
// судит не то.

import { readFileSync, readdirSync, statSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { isTestFile } from "@shared/test/module-reachability";

const HERE = path.dirname(fileURLToPath(import.meta.url));
const UI_ROOT = path.resolve(HERE, "..", "..", "..");
const CLIENT_FILE = path.join(UI_ROOT, "shared", "src", "lib", "kratos.ts");

/** Каталоги верхнего уровня `ui-future`, которые исходниками продукта не являются. */
const NOT_SOURCE = new Set(["node_modules", "deploy", "docs", "scripts", ".git"]);

function sourceFiles(dir: string, acc: string[] = []): string[] {
  for (const name of readdirSync(dir)) {
    if (NOT_SOURCE.has(name)) continue;
    const full = path.join(dir, name);
    if (statSync(full).isDirectory()) sourceFiles(full, acc);
    else if (/\.tsx?$/.test(name) && !isTestFile(full)) acc.push(full);
  }
  return acc;
}

/** Тело объявления `export const kratos = { … }` — по балансу скобок. */
function clientObjectBody(text: string): string {
  const start = text.indexOf("export const kratos = {");
  if (start < 0) return "";
  const open = text.indexOf("{", start);
  let depth = 0;
  for (let i = open; i < text.length; i += 1) {
    if (text[i] === "{") depth += 1;
    else if (text[i] === "}") {
      depth -= 1;
      if (depth === 0) return text.slice(open + 1, i);
    }
  }
  return "";
}

/** Методы объекта — объявления первого уровня вложенности его тела. */
function objectMethods(body: string): string[] {
  const out: string[] = [];
  let depth = 0;
  for (const line of body.split("\n")) {
    if (depth === 0) {
      const m = /^\s*(?:async\s+)?([a-zA-Z_$][\w$]*)\s*(?:<[^>]*>)?\s*\(/.exec(line);
      if (m) out.push(m[1]);
    }
    for (const ch of line) {
      if (ch === "{") depth += 1;
      else if (ch === "}") depth -= 1;
    }
  }
  return out;
}

/** Экспортируемые функции файла. */
function exportedFunctions(text: string): string[] {
  const out: string[] = [];
  for (const m of text.matchAll(/^export\s+(?:async\s+)?function\s+([a-zA-Z_$][\w$]*)/gm)) out.push(m[1]);
  return out;
}

describe("клиент чужой службы личности", () => {
  const clientText = readFileSync(CLIENT_FILE, "utf8");
  const methods = objectMethods(clientObjectBody(clientText));
  const functions = exportedFunctions(clientText);
  const files = sourceFiles(UI_ROOT).filter((f) => f !== CLIENT_FILE);
  const corpus = files.map((f) => readFileSync(f, "utf8")).join("\n");

  it("предпосылка: объявление прочитано и корпус непуст", () => {
    expect(methods.length).toBeGreaterThan(0);
    expect(functions.length).toBeGreaterThan(0);
    expect(files.length).toBeGreaterThan(0);
  });

  it("у каждого объявленного члена есть вызывающий вне файла клиента", () => {
    const orphaned = [
      ...methods.filter(
        (m) =>
          !new RegExp(`\\bkratos\\.${m}\\b`).test(corpus) &&
          !new RegExp(`\\bthis\\.${m}\\s*\\(`).test(clientText),
      ),
      ...functions.filter((f) => !new RegExp(`\\b${f}\\s*\\(`).test(corpus)),
    ].sort();
    expect({
      orphaned,
      methodsExamined: methods.length,
      functionsExamined: functions.length,
      filesExamined: files.length,
    }).toEqual({
      orphaned: [],
      methodsExamined: methods.length,
      functionsExamined: functions.length,
      filesExamined: files.length,
    });
  });
});
