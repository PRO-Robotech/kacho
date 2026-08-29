// Перепись: кто в дереве консоли объявляет СВОЙ конверт мутации.
//
// Конверт — это пять глаголов платформы над ОДНИМ базовым путём ресурса
// (`list`/`get`/`create`/`update`/`delete`). Форма у них одна на всю консоль:
// список читается с фильтром, чтение и мутации адресуются `<путь>/<id>`, а
// мутация возвращает `{ operation }`. Домен в этом не участвует ничем — он даёт
// только базовый путь и тип страницы.
//
// Читают перепись два потребителя — суд (`mutation-envelope.test.ts`) и
// доказательство его способности упасть (`…injection.test.ts`); оба обязаны
// видеть ОДНО И ТО ЖЕ, иначе инъекция удостоверяет не тот предикат, которым
// судят дерево.
//
// РАЗБОР, А НЕ ПОИСК ПО ОБРАЗЦУ: имена глаголов встречаются в прозе, в строках и
// в чужих объектах, и текстовый предикат нашёл бы их в комментарии, который сам
// же это правило и объясняет.

import { readFileSync } from "node:fs";
import path from "node:path";

import ts from "typescript";

import { discoverApps, sourceFiles } from "./shared-symbol-sweep";

/** Пять глаголов платформы. Объект, объявляющий их все, и есть конверт. */
export const ENVELOPE_VERBS: readonly string[] = ["list", "get", "create", "update", "delete"];

export interface FileScan {
  /** Строки объявления конвертов — координата, а не только имя файла. */
  lines: number[];
  /** Сколько объектных литералов осмотрено: распознавателю есть что различать. */
  literals: number;
}

/** Литералы файла, объявляющие ВСЕ пять глаголов своими свойствами. */
export function scanEnvelopes(source: string, fileName: string): FileScan {
  const sf = ts.createSourceFile(fileName, source, ts.ScriptTarget.ESNext, true, ts.ScriptKind.TSX);
  const lines: number[] = [];
  let literals = 0;
  const visit = (node: ts.Node): void => {
    if (ts.isObjectLiteralExpression(node)) {
      literals++;
      const names = new Set(
        node.properties
          .map((p) => (p.name && (ts.isIdentifier(p.name) || ts.isStringLiteral(p.name)) ? p.name.text : null))
          .filter((n): n is string => n !== null),
      );
      if (ENVELOPE_VERBS.every((v) => names.has(v))) {
        lines.push(sf.getLineAndCharacterOfPosition(node.getStart(sf)).line + 1);
      }
    }
    ts.forEachChild(node, visit);
  };
  visit(sf);
  return { lines, literals };
}

export interface MutationEnvelopeCensus {
  apps: string[];
  /** Прочитано файлов приложений — объём осмотренного. */
  filesRead: number;
  /** Осмотрено объектных литералов. */
  literalsSeen: number;
  /** Координаты конвертов, объявленных приложением. */
  hits: string[];
}

export function mutationEnvelopeCensus(repoRoot: string): MutationEnvelopeCensus {
  const apps = discoverApps(repoRoot);
  const hits: string[] = [];
  let filesRead = 0;
  let literalsSeen = 0;
  for (const app of apps) {
    for (const file of sourceFiles(path.join(repoRoot, app, "src"))) {
      filesRead++;
      const scan = scanEnvelopes(readFileSync(file, "utf8"), file);
      literalsSeen += scan.literals;
      for (const line of scan.lines) hits.push(`${path.relative(repoRoot, file)}:${line}`);
    }
  }
  return { apps, filesRead, literalsSeen, hits: hits.sort() };
}
