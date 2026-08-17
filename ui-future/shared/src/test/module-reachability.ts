import { existsSync, readFileSync, readdirSync, statSync } from "node:fs";
import path from "node:path";

/**
 * Обходчик достижимости модулей приложения от его ОБЪЯВЛЕННЫХ входов.
 *
 * Вынесен из пробы отдельным файлом, чтобы им могла пользоваться и проба
 * инъекции: она строит синтетическое дерево и требует от обходчика находку —
 * без общего кода обе стороны меряли бы разными линейками.
 *
 * ЧТО СЧИТАЕТСЯ ВХОДОМ. Ровно то, что называет дерево, а не то, что похоже на
 * точку входа: `src/main.tsx` (standalone-вход `index.html`) и всё, что
 * `vite.config.ts` отдаёт наружу федерацией (`exposes`). Файл, который просто
 * выглядит корнем приложения (`src/App.tsx`), входом НЕ является — ровно на этом
 * допущении половина недостижимого пряталась годами: пока `App.tsx` считали
 * входом, за ним скрывалось тридцать модулей, и снятие мёртвого дерева
 * маршрутов их не завело, а обнажило.
 */

/** Каталоги верхнего уровня `ui-future`, которые приложениями не являются. */
const NOT_APPS = new Set(["node_modules", "deploy", "docs", "scripts", ".git", "e2e"]);

/**
 * `shared` — БИБЛИОТЕКА, а не приложение: своих входов у неё нет, и вопрос
 * «достижим ли модуль от входа» к ней не применим в этой форме. Её мёртвый код
 * ловится не отсюда — это отдельный предмет с отдельным обходом (от достижимых
 * множеств всех приложений), и он назван задачей, а не подразумевается.
 */
const NOT_APPS_LIBRARY = new Set(["shared"]);

const EXTS = [".ts", ".tsx", ".js", ".jsx"];

/**
 * Импорты и ре-экспорты, статические и динамические.
 *
 * Читается ИСХОДНИК, а не разобранное дерево: разбор потребовал бы компилятора в
 * харнессе пробы. Цена названа: строковый литерал вида `import("…")` внутри
 * комментария был бы засчитан за ребро. Это ошибка В СТОРОНУ «достижим», то есть
 * гейт может пропустить мёртвый модуль, но НЕ объявит мёртвым живой — ложная
 * находка была бы дороже, её первый же ложный срабат отключил бы гейт.
 */
const IMPORT_RE =
  /(?:^|[\s;}])(?:import|export)\s+(?:[^'"()]*?\sfrom\s+)?["']([^"']+)["']|import\s*\(\s*["']([^"']+)["']\s*\)/gm;

export interface Reachability {
  app: string;
  /** входы, названные деревом (относительно каталога приложения) */
  entries: string[];
  /** все не-тестовые модули `src/**` (относительно каталога приложения) */
  modules: string[];
  reachable: string[];
  unreachable: string[];
}

export function isTestFile(p: string): boolean {
  const posix = p.split(path.sep).join("/");
  return (
    /\.(test|spec)\.[cm]?[jt]sx?$/.test(posix) ||
    /(^|\/)(test|__tests__|__mocks__)\//.test(posix) ||
    posix.endsWith(".d.ts")
  );
}

/** Приложения дерева — по наличию `src/`, а не по выписанному перечню. */
export function discoverApps(uiRoot: string): string[] {
  return readdirSync(uiRoot)
    .filter((n) => !NOT_APPS.has(n) && !NOT_APPS_LIBRARY.has(n))
    .filter((n) => {
      const dir = path.join(uiRoot, n);
      return statSync(dir).isDirectory() && existsSync(path.join(dir, "src"));
    })
    .sort();
}

function sourceFiles(dir: string, acc: string[] = []): string[] {
  for (const name of readdirSync(dir)) {
    const full = path.join(dir, name);
    if (statSync(full).isDirectory()) sourceFiles(full, acc);
    else if (/\.tsx?$/.test(name) && !isTestFile(full)) acc.push(full);
  }
  return acc;
}

/** Входы приложения: standalone-вход плюс всё, что отдаётся федерацией. */
export function entryPoints(uiRoot: string, app: string): string[] {
  const appDir = path.join(uiRoot, app);
  const found: string[] = [];

  const main = path.join(appDir, "src", "main.tsx");
  if (existsSync(main)) found.push(main);

  const vite = path.join(appDir, "vite.config.ts");
  if (existsSync(vite)) {
    const config = readFileSync(vite, "utf8");
    const block = config.match(/exposes:\s*\{([\s\S]*?)\n {6}\}/);
    if (block) {
      for (const m of block[1].matchAll(/"(\.\/[\w-]+)":\s*"(\.\/[^"]+)"/g)) {
        const file = path.join(appDir, m[2]);
        if (existsSync(file)) found.push(file);
      }
    }
  }
  return [...new Set(found)];
}

function resolveSpec(spec: string, importer: string, appSrc: string): string | null {
  let base: string;
  if (spec.startsWith("@/")) base = path.join(appSrc, spec.slice(2));
  // Голые `.` и `..` — такие же относительные ссылки, как `./` и `../`: это
  // импорт индекса своего (или родительского) каталога. Пропустив их, обходчик
  // объявил бы мёртвым модуль, который импортируют, — то есть выдал бы ЛОЖНУЮ
  // находку, а первый же ложный срабат отключает гейт. Форма найдена не
  // рассуждением: без неё первая же чистка снесла бы четыре живые пробы,
  // импортирующие свой каталог как `from "."`.
  else if (spec === "." || spec === ".." || spec.startsWith("./") || spec.startsWith("../"))
    base = path.resolve(path.dirname(importer), spec);
  // Всё прочее — пакет либо ЧУЖОЙ модуль (`@shared/…`): в счёт этого приложения
  // не берётся, у него свой владелец.
  else return null;

  const candidates = [base, ...EXTS.map((e) => base + e), ...EXTS.map((e) => path.join(base, "index" + e))];
  for (const c of candidates) {
    if (existsSync(c) && statSync(c).isFile() && /\.[cm]?[jt]sx?$/.test(c)) return c;
  }
  return null;
}

export function walkApp(uiRoot: string, app: string): Reachability {
  const appDir = path.join(uiRoot, app);
  const appSrc = path.join(appDir, "src");
  const rel = (p: string) => path.relative(appDir, p).split(path.sep).join("/");

  const modules = sourceFiles(appSrc);
  const entries = entryPoints(uiRoot, app);

  const seen = new Set(entries);
  const stack = [...entries];
  while (stack.length) {
    const cur = stack.pop()!;
    let text: string;
    try {
      text = readFileSync(cur, "utf8");
    } catch {
      continue;
    }
    for (const m of text.matchAll(IMPORT_RE)) {
      const spec = m[1] ?? m[2];
      if (!spec) continue;
      const target = resolveSpec(spec, cur, appSrc);
      // Ребро наружу приложения (`../shared/src/…`) не делает достижимым МОДУЛЬ
      // ЭТОГО приложения — считается только своё дерево.
      if (!target || !target.startsWith(appSrc + path.sep)) continue;
      if (!seen.has(target)) {
        seen.add(target);
        stack.push(target);
      }
    }
  }

  return {
    app,
    entries: entries.map(rel).sort(),
    modules: modules.map(rel).sort(),
    reachable: modules.filter((m) => seen.has(m)).map(rel).sort(),
    unreachable: modules.filter((m) => !seen.has(m)).map(rel).sort(),
  };
}
