// Разбор дерева консоли для гейта вкладки «Операции». Утверждения — в
// `operations-subroute-audit.test.ts`; здесь только чтение дерева, без единого
// ожидания, чтобы ту же логику можно было исполнить и вне jest.
//
// Предмет. Вкладка «Операции» адресует подмаршрут `GET <apiPath>/{id}/operations`.
// Ствол несёт его НЕ у всякого ресурса: каталожные и админские ресурсы (типы
// дисков и машин, каталог размещения geo, пулы адресов, вложенные ресурсы
// реестра) такого связывания не объявляют вовсе. Собранный на месте вызова путь
// у них бьёт в адрес, которого нет, и оператор читает отказ края как поломку.
//
// Почему разбор синтаксический, а не текстовый. Комментарии в этом же дереве
// ЦИТИРУЮТ и `spec.apiPath`, и `<apiPath>/{id}/operations` — как раз объясняя
// запрет. Текстовый предикат зачёл бы объяснение за нарушение, а запрет,
// краснеющий на собственном объяснении, снимут первым же. Поэтому читаются
// только узлы синтаксиса: до комментариев обход не доходит by construction.
// Тот же приём и по той же причине уже стоит в `api/client.endpoints.test.ts`.
//
// Почему `apiPath` РЕЗОЛВИТСЯ, а нерезолвнутый — находка. В реестре shared две
// спеки задают `apiPath` не литералом, а импортированной константой
// (`GEO_REGIONS_PATH`/`GEO_ZONES_PATH`). Предикат, читающий только литералы,
// молчит на них — то есть не видит целый ВИД предмета и при этом отчитывается
// «ноль находок». Поэтому идентификатор прослеживается до объявления (своего
// файла или импортированного модуля), а спека, чей `apiPath` проследить не
// удалось, возвращается с `apiPath === null` и обязана быть отвергнута
// вызывающим утверждением, а не пропущена молча.

import { readdirSync, readFileSync, statSync } from "node:fs";
import { dirname, join, resolve } from "node:path";

import ts from "typescript";

/** Спека реестра: имя, объявленный `apiPath` и то, во что он разрешился. */
export interface SpecEntry {
  /** Приложение, чей это реестр (`compute`, `shared`, …). */
  app: string;
  /** Значение поля `id` спеки. */
  id: string;
  /** Как `apiPath` записан в исходнике: литерал в кавычках либо имя константы. */
  rawApiPath: string;
  /** Разрешённое значение, либо `null` — «проследить не удалось». */
  apiPath: string | null;
}

/** Как приложение провязало вкладку операций. */
export interface TabWiring {
  file: string;
  app: string;
  /** Файл рендерит `<OperationsTab …>`. */
  rendersTab: boolean;
  /** Файл зовёт `operationsListPath(…)` — общий перечень подмаршрутов. */
  callsListPathHelper: boolean;
  /** Файл передаёт вкладке готовый путь пропом `listPath`. */
  passesListPathProp: boolean;
  /** Файл собирает путь операций сам, подставляя `apiPath` в шаблон. */
  buildsPathFromApiPath: boolean;
}

const SOURCE_RE = /\.tsx?$/;

/** Обход дерева без `node_modules`. */
export function walk(dir: string, match: RegExp, out: string[] = []): string[] {
  for (const entry of readdirSync(dir)) {
    if (entry === "node_modules" || entry === "dist") continue;
    const p = join(dir, entry);
    if (statSync(p).isDirectory()) walk(p, match, out);
    else if (match.test(p)) out.push(p);
  }
  return out;
}

function parse(file: string, source?: string): ts.SourceFile {
  return ts.createSourceFile(
    file,
    source ?? readFileSync(file, "utf8"),
    ts.ScriptTarget.ESNext,
    true,
    ts.ScriptKind.TSX,
  );
}

/**
 * Базы подмаршрутов операций, объявленные стволом: `GET <base>/{param}/operations`.
 *
 * Якорь конца строки отделяет глагольную форму (`…/operations:all` — отдельный
 * аккаунт-широкий список, подмаршрутом ресурса не являющийся).
 */
export function protoOperationBases(protoDir: string): string[] {
  const files = walk(protoDir, /\.proto$/);
  const bases = new Set<string>();
  for (const f of files) {
    for (const m of readFileSync(f, "utf8").matchAll(/get:\s*"([^"]+)\/\{[^}]+\}\/operations"/g)) {
      bases.add(m[1]);
    }
  }
  return [...bases].sort();
}

/** Сколько файлов proto прочитано — перепись рядом с находками. */
export function protoFileCount(protoDir: string): number {
  return walk(protoDir, /\.proto$/).length;
}

/** Реестры ресурсов, найденные в дереве консоли (перечень выводится, а не выписывается). */
export function findRegistryFiles(uiRoot: string): string[] {
  return walk(uiRoot, /\/src\/lib\/resource-registry\.tsx$/).sort();
}

/** Приложение, которому принадлежит файл: первый сегмент пути внутри `ui-future`. */
export function appOf(uiRoot: string, file: string): string {
  return file.slice(uiRoot.length + 1).split("/")[0];
}

/** Специфер модуля → файл на диске (`@shared/*`, `@/*`, относительные). */
function resolveModule(specifier: string, fromFile: string, uiRoot: string, app: string): string | null {
  let base: string;
  if (specifier.startsWith("@shared/")) base = join(uiRoot, "shared", "src", specifier.slice("@shared/".length));
  else if (specifier.startsWith("@/")) base = join(uiRoot, app, "src", specifier.slice("@/".length));
  else if (specifier.startsWith(".")) base = resolve(dirname(fromFile), specifier);
  else return null;
  for (const candidate of [`${base}.ts`, `${base}.tsx`, join(base, "index.ts"), join(base, "index.tsx")]) {
    try {
      if (statSync(candidate).isFile()) return candidate;
    } catch {
      // кандидата нет — пробуем следующий
    }
  }
  return null;
}

/** Строковые константы, объявленные в файле: `const NAME = "литерал"`. */
function stringConstsOf(sf: ts.SourceFile): Map<string, string> {
  const out = new Map<string, string>();
  const visit = (node: ts.Node): void => {
    if (
      ts.isVariableDeclaration(node) &&
      ts.isIdentifier(node.name) &&
      node.initializer !== undefined &&
      (ts.isStringLiteral(node.initializer) || ts.isNoSubstitutionTemplateLiteral(node.initializer))
    ) {
      out.set(node.name.text, node.initializer.text);
    }
    ts.forEachChild(node, visit);
  };
  visit(sf);
  return out;
}

/** Имя импортированного идентификатора → специфер модуля, откуда он пришёл. */
function importSourcesOf(sf: ts.SourceFile): Map<string, string> {
  const out = new Map<string, string>();
  for (const st of sf.statements) {
    if (!ts.isImportDeclaration(st) || !ts.isStringLiteral(st.moduleSpecifier)) continue;
    const bindings = st.importClause?.namedBindings;
    if (bindings === undefined || !ts.isNamedImports(bindings)) continue;
    for (const el of bindings.elements) out.set(el.name.text, st.moduleSpecifier.text);
  }
  return out;
}

/**
 * Спеки реестра. `apiPath` разрешается литералом, своей константой либо
 * константой импортированного модуля; неразрешённый возвращается как `null` —
 * это находка вызывающего, а не повод пропустить спеку.
 */
export function readRegistrySpecs(file: string, uiRoot: string, source?: string): SpecEntry[] {
  const app = appOf(uiRoot, file);
  const sf = parse(file, source);
  const locals = stringConstsOf(sf);
  const imports = importSourcesOf(sf);
  const foreign = new Map<string, Map<string, string>>();

  const resolveName = (name: string): string | null => {
    const local = locals.get(name);
    if (local !== undefined) return local;
    const specifier = imports.get(name);
    if (specifier === undefined) return null;
    if (!foreign.has(specifier)) {
      const target = resolveModule(specifier, file, uiRoot, app);
      foreign.set(specifier, target === null ? new Map() : stringConstsOf(parse(target)));
    }
    return foreign.get(specifier)?.get(name) ?? null;
  };

  const out: SpecEntry[] = [];
  const visit = (node: ts.Node): void => {
    if (ts.isObjectLiteralExpression(node)) {
      let id: string | null = null;
      let rawApiPath: string | null = null;
      let apiPath: string | null = null;
      for (const p of node.properties) {
        if (!ts.isPropertyAssignment(p) || !ts.isIdentifier(p.name)) continue;
        if (p.name.text === "id" && ts.isStringLiteral(p.initializer)) id = p.initializer.text;
        if (p.name.text === "apiPath") {
          if (ts.isStringLiteral(p.initializer) || ts.isNoSubstitutionTemplateLiteral(p.initializer)) {
            rawApiPath = JSON.stringify(p.initializer.text);
            apiPath = p.initializer.text;
          } else if (ts.isIdentifier(p.initializer)) {
            rawApiPath = p.initializer.text;
            apiPath = resolveName(p.initializer.text);
          } else {
            rawApiPath = p.initializer.getText(sf);
            apiPath = null;
          }
        }
      }
      if (id !== null && rawApiPath !== null) out.push({ app, id, rawApiPath, apiPath });
    }
    ts.forEachChild(node, visit);
  };
  visit(sf);
  return out;
}

/**
 * Провязка вкладки в одном файле.
 *
 * `buildsPathFromApiPath` — та самая форма дефекта: шаблон, в который подставлен
 * `apiPath`, а рядом в тексте шаблона стоит `/operations`. Читаются только узлы
 * шаблона, поэтому цитата из комментария сюда не попадает.
 */
export function tabWiringOf(file: string, uiRoot: string, source?: string): TabWiring {
  const sf = parse(file, source);
  const w: TabWiring = {
    file,
    app: appOf(uiRoot, file),
    rendersTab: false,
    callsListPathHelper: false,
    passesListPathProp: false,
    buildsPathFromApiPath: false,
  };

  const visit = (node: ts.Node): void => {
    if (ts.isJsxOpeningElement(node) || ts.isJsxSelfClosingElement(node)) {
      if (node.tagName.getText(sf) === "OperationsTab") {
        w.rendersTab = true;
        for (const attr of node.attributes.properties) {
          if (ts.isJsxAttribute(attr) && attr.name.getText(sf) === "listPath") w.passesListPathProp = true;
        }
      }
    }
    if (ts.isCallExpression(node) && node.expression.getText(sf) === "operationsListPath") w.callsListPathHelper = true;
    if (ts.isTemplateExpression(node)) {
      const literal = node.head.text + node.templateSpans.map((s) => s.literal.text).join("");
      const holes = node.templateSpans.map((s) => s.expression.getText(sf)).join(" ");
      if (literal.includes("/operations") && /\bapiPath\b/.test(holes)) w.buildsPathFromApiPath = true;
    }
    ts.forEachChild(node, visit);
  };
  visit(sf);
  return w;
}

/** Провязка вкладки по всем нетестовым исходникам консоли. */
export function tabWiringAcrossTree(uiRoot: string): TabWiring[] {
  return walk(uiRoot, SOURCE_RE)
    .filter((f) => !/\.test\.tsx?$/.test(f))
    .map((f) => tabWiringOf(f, uiRoot))
    .filter((w) => w.rendersTab || w.buildsPathFromApiPath || w.callsListPathHelper);
}
