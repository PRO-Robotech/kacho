// Перепись форков: что вне `shared/` дублирует `shared/`. Читают её ДВА
// потребителя — суд гейта `shared-organisms-single-source.test.ts` и пересбор
// ведомости (`KACHO_REGEN_FORK_LEDGER=1`, живёт в том же гейте), — и оба обязаны
// видеть ОДНО И ТО ЖЕ: иначе ведомость собирается одним предикатом, а судится
// другим, и расхождение проявляется ровно там, где его не видно (на валидном
// входе оба «согласны»).
//
// > Здесь стояла ссылка на `scripts/regen-shared-fork-ledger.mjs` — такого файла
// > в дереве нет; пересбор исполняется внутри самого гейта. Комментарий пережил
// > свой предмет и посылал читателя искать несуществующий скрипт.
//
// ФОРК УЗНАЁТСЯ ПО ДВУМ ПРИЗНАКАМ, и одного мало:
//
//   1. СИМВОЛ — файл объявляет имя, которое объявляет `shared/`. Ловит копию,
//      сохранившую имена.
//   2. АДРЕС — файл лежит по тому же пути под `src/`, что файл `shared/src/`, и
//      не является тонкой прослойкой. Ловит копию, имена в которой ПЕРЕИМЕНОВАНЫ.
//
// Второй признак заведён потому, что первый на переименовании слеп by
// construction: копия помощников подписи ссылки объявляет `headLabelFor` и
// `extraInfoFor` там, где общий объявляет `refOptionHead` и `refOptionExtra`, —
// один и тот же код под другими именами, для признака по символу невидимый.
// Именно такая копия и расходится тише всех: она не спорит с общим ни одним
// именем, поэтому её не видит ни гейт, ни поиск по имени.
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

/**
 * Тонкая прослойка — файл, который НИЧЕГО не объявляет и не описывает, а только
 * пробрасывает чужое: `export * from "@shared/…"`, барель `export * from "./X"`,
 * реэкспорт типов. Это и есть форма, которую требуется писать вместо копии,
 * поэтому она разрешена без всякой записи в ведомости.
 *
 * Предикат — по СОДЕРЖИМОМУ, а не по имени файла: `index.ts` бывает и барелем, и
 * копией, и судить о нём по имени значило бы мерить соглашение об именовании
 * вместо предмета.
 */
export function isReExportOnly(src: string): boolean {
  const code = src
    // Строки и шаблоны не разбираем: в прослойке их не бывает, а вот `//` внутри
    // адреса (`"@shared/…"`) снятие комментариев испортило бы.
    .replace(/^\s*\/\/.*$/gm, "")
    .replace(/\/\*[\s\S]*?\*\//g, "")
    .trim();
  if (code === "") return false; // пустой файл прослойкой не является
  const statements = code
    .split("\n")
    .map((l) => l.trim())
    .filter((l) => l !== "");
  return statements.every((l) => /^(import\b|export\s+(\*|type\s*\{|\{))/.test(l) || /^[}\w"',\s]*;?$/.test(l));
}

export interface ForkHit {
  /** Путь от корня дерева консоли. */
  file: string;
  /** Приложение, которому принадлежит файл. */
  app: string;
  /** Символы файла, которые уже объявлены в shared/. */
  symbols: string[];
  /** Файл лежит по тому же пути под `src/`, что файл `shared/src/`, и прослойкой
   *  не является. Признак ловит копию с ПЕРЕИМЕНОВАННЫМИ символами, которую
   *  признак по символу не видит вовсе. */
  pathPaired: boolean;
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
  /** Файлов приложений, ПАРНЫХ по пути с `shared/src` (вместе с прослойками). */
  pathPairedFiles: number;
  /** Из них тонких прослоек — то есть уже сведённых к общему. */
  shims: number;
  /** Форки: файл объявляет символ shared ЛИБО парен по пути и прослойкой не
   *  является. Отсортированы по пути. */
  hits: ForkHit[];
}

/** Полная перепись дерева консоли. */
export function sweep(repoRoot: string): Sweep {
  const sharedRoot = path.join(repoRoot, "shared/src");
  const sharedFiles = sourceFiles(sharedRoot);
  const sharedSymbols = new Set<string>();
  const sharedRelPaths = new Set<string>();
  for (const f of sharedFiles) {
    for (const s of declaredSymbols(readFileSync(f, "utf8"))) sharedSymbols.add(s);
    sharedRelPaths.add(path.relative(sharedRoot, f));
  }

  const apps = discoverApps(repoRoot);
  const hits: ForkHit[] = [];
  let filesRead = 0;
  let pathPairedFiles = 0;
  let shims = 0;
  for (const app of apps) {
    const appRoot = path.join(app, "src");
    for (const file of sourceFiles(path.join(repoRoot, appRoot))) {
      filesRead++;
      const src = readFileSync(file, "utf8");
      const own = [...declaredSymbols(src)].filter((s) => sharedSymbols.has(s)).sort();

      const paired = sharedRelPaths.has(path.relative(path.join(repoRoot, appRoot), file));
      if (paired) pathPairedFiles++;
      const shim = paired && isReExportOnly(src);
      if (shim) shims++;
      const pathPaired = paired && !shim;

      if (own.length > 0 || pathPaired) {
        hits.push({ file: path.relative(repoRoot, file), app, symbols: own, pathPaired });
      }
    }
  }
  hits.sort((a, b) => (a.file < b.file ? -1 : a.file > b.file ? 1 : 0));
  return {
    repoRoot,
    apps,
    filesRead,
    sharedFilesRead: sharedFiles.length,
    sharedSymbols: sharedSymbols.size,
    pathPairedFiles,
    shims,
    hits,
  };
}
