// Перепись: кто в дереве консоли объявляет ЗАНОВО типы провода платформы.
//
// Читают её два потребителя — суд (`platform-envelope.test.ts`) и доказательство
// его способности упасть (`…injection.test.ts`), — и оба обязаны видеть ОДНО И ТО
// ЖЕ: иначе инъекция удостоверяет не тот предикат, которым судят дерево.

import { readFileSync } from "node:fs";
import path from "node:path";

import { declaredSymbols, declaredTypeSymbols, discoverApps, sourceFiles } from "./shared-symbol-sweep";

/** Все объявленные символы исходника — и значения, и типы. */
export function allDeclared(src: string): Set<string> {
  return new Set([...declaredSymbols(src), ...declaredTypeSymbols(src)]);
}

export interface EnvelopeHit {
  /** Путь от корня дерева консоли. */
  file: string;
  /** Приложение, которому принадлежит файл. */
  app: string;
  /** Символы платформы, объявленные этим файлом заново. */
  symbols: string[];
}

export interface EnvelopeCensus {
  apps: string[];
  /** Прочитано файлов приложений — объём осмотренного. */
  filesRead: number;
  /** Символы, объявленные общим `api/types.ts`. */
  platformSymbols: string[];
  /** Модули, объявившие хоть один символ платформы заново. */
  hits: EnvelopeHit[];
}

/** Общий контракт провода — один файл на всю консоль. */
export const PLATFORM_TYPES = "shared/src/api/types.ts";

export function envelopeCensus(repoRoot: string): EnvelopeCensus {
  const platform = allDeclared(readFileSync(path.join(repoRoot, PLATFORM_TYPES), "utf8"));
  const apps = discoverApps(repoRoot);
  const hits: EnvelopeHit[] = [];
  let filesRead = 0;
  for (const app of apps) {
    for (const file of sourceFiles(path.join(repoRoot, app, "src"))) {
      filesRead++;
      const symbols = [...allDeclared(readFileSync(file, "utf8"))].filter((s) => platform.has(s)).sort();
      if (symbols.length > 0) hits.push({ file: path.relative(repoRoot, file), app, symbols });
    }
  }
  hits.sort((a, b) => (a.file < b.file ? -1 : a.file > b.file ? 1 : 0));
  return { apps, filesRead, platformSymbols: [...platform].sort(), hits };
}
