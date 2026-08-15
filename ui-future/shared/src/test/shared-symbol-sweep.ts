// Перепись форков: какие символы, объявленные вне `shared/`, уже объявлены в
// `shared/`. Читают её ДВА потребителя — гейт
// `shared-organisms-single-source.test.ts` и генератор ведомости
// `scripts/regen-shared-fork-ledger.mjs`, — и оба обязаны видеть ОДНО И ТО ЖЕ:
// иначе ведомость собирается одним предикатом, а судится другим, и расхождение
// проявляется ровно там, где его не видно (на валидном входе оба «согласны»).
//
// Файл лежит в `src/test/`, поэтому под собственную перепись не подпадает —
// тестовые файлы из обхода исключены by construction.

import { existsSync, readdirSync, readFileSync, statSync } from "node:fs";
import path from "node:path";

/** Каталоги верхнего уровня, которые приложениями этого дерева не являются. */
export const NOT_APPS: ReadonlySet<string> = new Set([
  "node_modules",
  "shared",
  "deploy",
  "docs",
  "scripts",
  "e2e",
  ".git",
]);

/**
 * Объявление символа, а НЕ его ре-экспорт. Тонкая прослойка
 * `export * from "@shared/…"` не объявляет ничего и является разрешённой
 * формой — ради неё предикат и написан по ключевому слову объявления.
 */
const DECLARATION = /^[ \t]*export[ \t]+(?:async[ \t]+)?(?:function|const|let|var|class)[ \t]+([A-Za-z_$][\w$]*)/gm;

/** Символы, объявленные исходником (по одному вхождению на имя). */
export function declaredSymbols(src: string): Set<string> {
  const out = new Set<string>();
  DECLARATION.lastIndex = 0;
  let m: RegExpExecArray | null;
  while ((m = DECLARATION.exec(src)) !== null) out.add(m[1]);
  return out;
}

/** Все .ts/.tsx под `dir`, кроме node_modules и тестовых файлов. */
export function sourceFiles(dir: string): string[] {
  const out: string[] = [];
  const walk = (cur: string) => {
    for (const entry of readdirSync(cur, { withFileTypes: true })) {
      if (entry.name === "node_modules") continue;
      const full = path.join(cur, entry.name);
      if (entry.isDirectory()) {
        walk(full);
        continue;
      }
      if (!/\.tsx?$/.test(entry.name)) continue;
      if (/\.(test|spec)\.tsx?$/.test(entry.name)) continue;
      out.push(full);
    }
  };
  walk(dir);
  return out;
}

/** Приложения дерева — по факту наличия `src/`, а не по вписанному перечню. */
export function discoverApps(repoRoot: string): string[] {
  return readdirSync(repoRoot)
    .filter((name) => !NOT_APPS.has(name))
    .filter((name) => {
      const dir = path.join(repoRoot, name);
      return statSync(dir).isDirectory() && existsSync(path.join(dir, "src"));
    })
    .sort();
}

export interface ForkHit {
  /** Путь от корня дерева консоли. */
  file: string;
  /** Приложение, которому принадлежит файл. */
  app: string;
  /** Символы файла, которые уже объявлены в shared/. */
  symbols: string[];
}

export interface Sweep {
  repoRoot: string;
  apps: string[];
  /** Прочитано файлов приложений — перепись объёма осмотренного. */
  filesRead: number;
  /** Прочитано файлов shared/. */
  sharedFilesRead: number;
  /** Символов, объявленных в shared/. */
  sharedSymbols: number;
  /** Файлы вне shared/, объявляющие символ shared. Отсортированы по пути. */
  hits: ForkHit[];
}

/** Полная перепись дерева консоли. */
export function sweep(repoRoot: string): Sweep {
  const sharedFiles = sourceFiles(path.join(repoRoot, "shared/src"));
  const sharedSymbols = new Set<string>();
  for (const f of sharedFiles) for (const s of declaredSymbols(readFileSync(f, "utf8"))) sharedSymbols.add(s);

  const apps = discoverApps(repoRoot);
  const hits: ForkHit[] = [];
  let filesRead = 0;
  for (const app of apps) {
    for (const file of sourceFiles(path.join(repoRoot, app, "src"))) {
      filesRead++;
      const own = [...declaredSymbols(readFileSync(file, "utf8"))].filter((s) => sharedSymbols.has(s)).sort();
      if (own.length > 0) hits.push({ file: path.relative(repoRoot, file), app, symbols: own });
    }
  }
  hits.sort((a, b) => (a.file < b.file ? -1 : a.file > b.file ? 1 : 0));
  return {
    repoRoot,
    apps,
    filesRead,
    sharedFilesRead: sharedFiles.length,
    sharedSymbols: sharedSymbols.size,
    hits,
  };
}
