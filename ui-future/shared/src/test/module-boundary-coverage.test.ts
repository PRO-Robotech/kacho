import { existsSync, readFileSync, readdirSync, statSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

/**
 * Гейт покрытия границами отказа на стороне МОДУЛЕЙ (#371).
 *
 * Норма: каждая страница, которую модуль выставляет наружу через федерацию,
 * обёрнута собственной границей отказа. Граница только в host закрывает ровно
 * один путь — монтирование из host'а; у модуля есть и своя точка входа
 * (standalone-разработка, `<модуль>/src/App.tsx`), где host-границы нет вовсе.
 *
 * Перечень выводится ИЗ ДЕРЕВА, а не выписывается: приложения — по наличию
 * `src/`, выставленные страницы — из блока `exposes` их `vite.config.ts`.
 * Поэтому модуль, заведённый завтра, попадает под гейт сам, а `exposes`,
 * добавленный без границы, краснит его с координатой файла.
 *
 * Собственная предпосылка гейта проверяется отдельной пробой: если чтение
 * `exposes` перестанет находить страницы (переписали конфиг, сменили форму),
 * это упадёт здесь, а не превратит обход в тихий no-op.
 */

const repoRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../..");

/** Каталоги верхнего уровня, которые приложениями этого дерева не являются. */
const NOT_APPS = new Set(["node_modules", "shared", "deploy", "docs", "scripts", ".git", "e2e"]);
/** host — оболочка, а не удалённый модуль: его граница живёт в `App.tsx` и проверяется в host-пробах. */
const NOT_REMOTES = new Set(["host"]);

function discoverRemoteApps(): string[] {
  return readdirSync(repoRoot)
    .filter((name) => !NOT_APPS.has(name) && !NOT_REMOTES.has(name))
    .filter((name) => {
      const dir = path.join(repoRoot, name);
      return (
        statSync(dir).isDirectory() && existsSync(path.join(dir, "src")) && existsSync(path.join(dir, "vite.config.ts"))
      );
    })
    .sort();
}

/** Выставленные наружу страницы модуля: `"./XPage": "./src/pages/XPage/index.ts"`. */
function exposedPages(app: string): { expose: string; file: string }[] {
  const config = readFileSync(path.join(repoRoot, app, "vite.config.ts"), "utf8");
  const block = config.match(/exposes:\s*\{([\s\S]*?)\n {6}\}/);
  if (!block) throw new Error(`${app}/vite.config.ts: блок exposes не найден — гейт читает не то`);
  return (
    [...block[1].matchAll(/"(\.\/[\w-]+)":\s*"(\.\/[^"]+)"/g)]
      .map((m) => ({ expose: m[1], file: m[2] }))
      // `./navigation` — данные меню, а не страница: рисовать там нечего, и
      // отказ его загрузки закрыт пометкой недоступности в рейле host'а.
      .filter((entry) => entry.expose !== "./navigation")
  );
}

const APPS = discoverRemoteApps();
const EXPOSED = APPS.flatMap((app) => exposedPages(app).map((entry) => ({ app, ...entry })));

describe("выставленные наружу страницы модулей несут свою границу отказа", () => {
  it(`перепись: приложений=${APPS.length}, выставленных страниц=${EXPOSED.length}`, () => {
    // «Нарушений нет» обязано быть отличимо от «ничего не прочитано».
    expect(APPS.length).toBeGreaterThan(0);
    expect(EXPOSED.length).toBeGreaterThan(0);
    expect(APPS).toEqual(
      expect.arrayContaining(["compute", "dashboard", "iam", "nlb", "registry", "storage", "system", "vpc"]),
    );
    for (const app of APPS) expect(exposedPages(app).length).toBeGreaterThan(0);
  });

  it("каждая выставленная страница обёрнута withModuleBoundary", () => {
    const missing: string[] = [];
    for (const entry of EXPOSED) {
      const file = path.join(repoRoot, entry.app, entry.file.replace(/^\.\//, ""));
      if (!existsSync(file)) {
        missing.push(`${entry.app}${entry.file}: файл, объявленный в exposes, не существует`);
        continue;
      }
      const src = readFileSync(file, "utf8");
      if (!src.includes("withModuleBoundary")) {
        missing.push(`${entry.app}/${entry.file}: нет границы отказа (withModuleBoundary)`);
      }
    }
    // Координаты, а не число: красный гейт обязан сказать, где именно.
    expect(missing).toEqual([]);
  });

  it("граница берётся из общего организма, а не из копии в модуле", () => {
    const forked: string[] = [];
    for (const entry of EXPOSED) {
      const file = path.join(repoRoot, entry.app, entry.file.replace(/^\.\//, ""));
      if (!existsSync(file)) continue;
      const src = readFileSync(file, "utf8");
      if (!src.includes("withModuleBoundary")) continue;
      if (!/from\s+"@shared\/components\/organisms\/ModuleErrorBoundary"/.test(src)) {
        forked.push(`${entry.app}/${entry.file}: граница берётся не из @shared`);
      }
    }
    expect(forked).toEqual([]);
  });
});
