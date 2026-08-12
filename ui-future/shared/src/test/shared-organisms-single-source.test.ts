import { existsSync, readdirSync, readFileSync, statSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

/**
 * Anti-drift guard (sec-hardening-r3): the generic resource CRUD organisms
 * (ResourceListPage / ResourceCreatePage / ResourceEditPage) MUST live in a
 * single place — shared/src/components/organisms — and be consumed by every
 * app of this tree via @shared. A copy inside an app is a fork: a bugfix that
 * lands in one copy silently misses the others. That is how the vpc/iam pair
 * drifted, and how compute/storage/nlb/registry later came to carry four
 * byte-equal copies of ResourceCreatePage plus four near-equal ResourceListPage
 * copies — one of which (nlb) had already lost the read-only row-actions guard
 * its three siblings had.
 *
 * Scope is the WHOLE tree, enumerated by fact rather than by a hardcoded app
 * list: every top-level directory that ships a `src/` is walked in full, and a
 * file that *declares* one of the components is a finding wherever it sits. The
 * previous revision named `["vpc", "iam"]` inline and looked only at
 * `<app>/src/components/organisms/<Comp>/<Comp>.tsx`, so for four of the six
 * consumers it asserted nothing at all while its own docstring called the check
 * exhaustive.
 *
 * This test fails if:
 *   - the shared implementation is missing, or
 *   - any file outside shared/ declares one of the components (a fork), or
 *   - an app keeps a component directory that holds anything besides an
 *     index.ts re-exporting from @shared.
 *
 * Census (first test): the number of apps enumerated and of source files read
 * are asserted non-zero and carried in the test name, so "no forks found"
 * cannot be confused with "nothing was read" — a moved repoRoot or a renamed
 * layout fails loudly instead of passing vacuously.
 *
 * Own premise (second test): the fork detector is run against shared/ itself
 * and must find exactly one declaration per component. If the detector ever
 * stops recognising an implementation (a refactor to `const X = () => …`, a
 * default export, …), that surfaces here instead of turning the tree-wide sweep
 * into a silent no-op.
 */

const repoRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../..");

// Компонент описывается ТРЕМЯ фактами, а не одним именем: каталог в дереве,
// имя файла и экспортируемый символ. Прежняя редакция считала их совпадающими —
// верно ровно для трёх страниц ресурса и неверно для тела формы (лежит в
// `organisms/form/`) и рендерера поля (файл `FormField`, символ
// `FormFieldRenderer`, потому что `FormField` — это ТИП поля, а не компонент).
//
// ResourceFormBody и FormField добавлены 2026-08-12: их форки жили в четырёх
// модулях и УЖЕ разошлись с оригиналом — без ветки правил группы безопасности и
// со своим приведением значения к строке. Расхождение читалось пользователем как
// «другое место продукта», а правка тела формы доезжала не всюду. Реализация
// сведена в shared, копии стали ре-экспортом; гейт держит это состояние.
interface Organism {
  /** Каталог под `components/organisms/` — и в shared, и в приложениях. */
  dir: string;
  /** Имя файла реализации внутри каталога (без расширения). */
  file: string;
  /** Экспортируемый символ, по которому узнаётся объявление. */
  symbol: string;
}

const COMPONENTS: readonly Organism[] = [
  { dir: "ResourceListPage", file: "ResourceListPage", symbol: "ResourceListPage" },
  { dir: "ResourceCreatePage", file: "ResourceCreatePage", symbol: "ResourceCreatePage" },
  { dir: "ResourceEditPage", file: "ResourceEditPage", symbol: "ResourceEditPage" },
  { dir: "form/ResourceFormBody", file: "ResourceFormBody", symbol: "ResourceFormBody" },
  { dir: "form/FormField", file: "FormField", symbol: "FormFieldRenderer" },
] as const;

/** Top-level directories that are not apps of this tree. */
const NOT_APPS = new Set(["node_modules", "shared", "deploy", "docs", "scripts", ".git"]);

/** Every top-level directory that ships a `src/` — the apps, discovered by fact. */
function discoverApps(): string[] {
  return readdirSync(repoRoot)
    .filter((name) => !NOT_APPS.has(name))
    .filter((name) => {
      const dir = path.join(repoRoot, name);
      return statSync(dir).isDirectory() && existsSync(path.join(dir, "src"));
    })
    .sort();
}

/** Every .ts/.tsx file under `dir`, excluding node_modules and test files. */
function sourceFiles(dir: string): string[] {
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

/**
 * Does `src` *declare* the component, as opposed to re-exporting it? A thin
 * `export * from "@shared/…"` shim declares nothing and is the allowed form.
 */
function declaresComponent(src: string, comp: string): boolean {
  const decl = new RegExp(
    [
      `export\\s+function\\s+${comp}\\b`,
      `export\\s+default\\s+function\\s+${comp}\\b`,
      `export\\s+(?:const|let|var)\\s+${comp}\\s*[:=]`,
      `export\\s+class\\s+${comp}\\b`,
      `^\\s*function\\s+${comp}\\s*\\(`,
    ].join("|"),
    "m",
  );
  return decl.test(src);
}

const APPS = discoverApps();
const APP_FILES = new Map<string, string[]>(APPS.map((app) => [app, sourceFiles(path.join(repoRoot, app, "src"))]));
const FILES_READ = [...APP_FILES.values()].reduce((n, files) => n + files.length, 0);

describe("shared resource CRUD organisms are single-source", () => {
  it(`census: every app of the tree is enumerated and read (apps=${APPS.length}, files=${FILES_READ})`, () => {
    // "Zero forks" must be distinguishable from "zero files read": a moved
    // repoRoot or a renamed layout would otherwise make every assertion below
    // vacuously true.
    expect(APPS.length).toBeGreaterThan(0);
    expect(FILES_READ).toBeGreaterThan(0);
    for (const app of APPS) expect((APP_FILES.get(app) ?? []).length).toBeGreaterThan(0);
    // Enumerating by `src/` presence (rather than by an inline list) is what
    // makes this hold for an app added tomorrow; the known consumers are pinned
    // so a discovery regression cannot shrink the sweep unnoticed.
    expect(APPS).toEqual(expect.arrayContaining(["compute", "iam", "nlb", "registry", "storage", "vpc"]));
  });

  it("own premise: the fork detector recognises the shared implementations", () => {
    // If this fails the sweep below proves nothing — the detector no longer
    // matches the way these components are written.
    const sharedSources = sourceFiles(path.join(repoRoot, "shared/src"));
    expect(sharedSources.length).toBeGreaterThan(0);
    for (const comp of COMPONENTS) {
      const hits = sharedSources.filter((f) => declaresComponent(readFileSync(f, "utf8"), comp.symbol));
      expect(hits.map((f) => path.relative(repoRoot, f))).toEqual([
        `shared/src/components/organisms/${comp.dir}/${comp.file}.tsx`,
      ]);
    }
  });

  for (const comp of COMPONENTS) {
    const sharedFile = path.join(repoRoot, "shared/src/components/organisms", comp.dir, `${comp.file}.tsx`);

    it(`${comp.symbol} has a real implementation in shared/`, () => {
      expect(existsSync(sharedFile)).toBe(true);
      const src = readFileSync(sharedFile, "utf8");
      expect(src).toContain(`export function ${comp.symbol}`);
    });

    it(`${comp.symbol} is not forked anywhere outside shared/`, () => {
      const forks: string[] = [];
      for (const app of APPS) {
        for (const file of APP_FILES.get(app) ?? []) {
          if (declaresComponent(readFileSync(file, "utf8"), comp.symbol)) forks.push(path.relative(repoRoot, file));
        }
      }
      // Coordinates, not a count — a red gate must say where.
      expect(forks).toEqual([]);
    });

    for (const app of APPS) {
      const appDir = path.join(repoRoot, app, "src/components/organisms", comp.dir);

      it(`${app}/${comp.dir}: only a thin @shared re-export may live in the app`, () => {
        if (!existsSync(appDir)) return; // this app does not surface the component at all
        const indexFile = path.join(appDir, "index.ts");
        expect({ app, comp: comp.dir, hasIndex: existsSync(indexFile) }).toEqual({ app, comp: comp.dir, hasIndex: true });
        const idx = readFileSync(indexFile, "utf8");
        expect(idx).toContain("@shared/components/organisms/" + comp.dir);
        // Anything besides the shim is a fork in disguise.
        const stray = readdirSync(appDir).filter((f) => f !== "index.ts");
        expect({ app, comp: comp.dir, stray }).toEqual({ app, comp: comp.dir, stray: [] });
      });
    }
  }
});
