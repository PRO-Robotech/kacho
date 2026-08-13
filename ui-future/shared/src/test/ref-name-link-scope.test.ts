import { readdirSync, readFileSync, statSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import ts from "typescript";

/**
 * Гейт: ссылка на ЧУЖОЙ ресурс спрашивает его именем по ЕГО области видимости.
 *
 * # Класс
 *
 * `RefNameLink` резолвит имя чужого ресурса одним списочным запросом. Область
 * видимости решает, чем этот список спрашивать: у ресурса проекта запрос несёт
 * `project_id`, у ГЛОБАЛЬНОГО справочника (регион, зона, каталог типов дисков)
 * измерения «проект» нет вовсе — и `project_id` там чужой параметр.
 *
 * Безусловный `project_id` даёт сразу два дефекта, и оба тихие: запрос уходит в
 * geo с параметром, которого его контракт не объявляет, а `enabled: !!projectId`
 * означает, что на страницах вне проекта имя не резолвится НИКОГДА — ссылка
 * молча показывает идентификатор вместо имени, то есть ровно то, что правило 2
 * канона консоли и запрещает.
 *
 * # Почему гейт, а не правка на месте
 *
 * Компонент форкнут: копий в дереве несколько, и они байт-в-байт совпадали в
 * этом месте. Правка одной копии закрыла бы находку и оставила класс — с ложным
 * ощущением, что вопрос снят. Замер на момент заведения гейта: копий 5, предикат
 * читала 1.
 *
 * # Что утверждается
 *
 * По КАЖДОЙ копии, разбором AST (а не поиском слова в тексте — слово находится и
 * в комментарии, объясняющем эту же защиту):
 *   (1) объект query-параметров, который уезжает вторым аргументом `api.list`,
 *       НЕ содержит безусловного свойства `project_id`;
 *   (2) он содержит условный спред — то есть параметр добавляется по признаку;
 *   (3) в файле есть сравнение `.scope` с "project" — то самое условие.
 *
 * # Объём осмотренного и собственная предпосылка
 *
 * Число найденных копий утверждается непустым и сверяется с известными
 * потребителями: «ноль находок» обязано быть отличимо от «ноль прочитанного», а
 * переименованный каталог не должен превращать обход в пустой. Сам детектор
 * проверяется в ОБЕ стороны на синтетических источниках: на прежней (дефектной)
 * форме он обязан дать находку, на нынешней — смолчать. Без второго контроля
 * гейт ловил бы форму, а не существо, и первый же ложный срабат его отключил бы.
 */

const consoleRoot = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  "../../..",
);

/** Каталоги верхнего уровня, приложениями этого дерева не являющиеся. */
const NOT_APPS = new Set([
  "node_modules",
  "deploy",
  "docs",
  "scripts",
  ".git",
  "e2e",
]);

function findRefNameLinks(): string[] {
  const out: string[] = [];
  const walk = (dir: string) => {
    for (const entry of readdirSync(dir, { withFileTypes: true })) {
      if (
        entry.name === "node_modules" ||
        entry.name === "dist" ||
        entry.name === ".git"
      )
        continue;
      const full = path.join(dir, entry.name);
      if (entry.isDirectory()) {
        walk(full);
        continue;
      }
      if (entry.name === "RefNameLink.tsx") out.push(full);
    }
  };
  for (const name of readdirSync(consoleRoot)) {
    if (NOT_APPS.has(name)) continue;
    const dir = path.join(consoleRoot, name);
    if (!statSync(dir).isDirectory()) continue;
    const src = path.join(dir, "src");
    try {
      if (statSync(src).isDirectory()) walk(src);
    } catch {
      // каталог без src/ — не приложение
    }
  }
  return out.sort();
}

interface Finding {
  /** Безусловное свойство project_id в объекте query-параметров. */
  unconditionalProjectId: boolean;
  /** Условный спред в том же объекте — параметр добавляется по признаку. */
  conditionalSpread: boolean;
  /** Сравнение `.scope` с "project" где-либо в файле. */
  readsScope: boolean;
  /** Найден ли вообще вызов `api.list(...)` с объектом параметров. */
  sawListCall: boolean;
}

/**
 * Разбор одного источника `RefNameLink`.
 *
 * Читается ИСПОЛНЯЕМАЯ часть: обход AST не видит ни комментариев, ни строковых
 * литералов, поэтому объяснение защиты рядом с ней гейт не обманывает.
 */
export function inspectRefNameLink(source: string): Finding {
  const sf = ts.createSourceFile(
    "RefNameLink.tsx",
    source,
    ts.ScriptTarget.Latest,
    true,
    ts.ScriptKind.TSX,
  );
  const f: Finding = {
    unconditionalProjectId: false,
    conditionalSpread: false,
    readsScope: false,
    sawListCall: false,
  };

  const visit = (node: ts.Node): void => {
    // `<что-то>.scope === "project"` — условие, по которому берётся ветка.
    if (
      ts.isBinaryExpression(node) &&
      (node.operatorToken.kind === ts.SyntaxKind.EqualsEqualsEqualsToken ||
        node.operatorToken.kind === ts.SyntaxKind.EqualsEqualsToken)
    ) {
      const left = node.left;
      const touchesScope =
        (ts.isPropertyAccessExpression(left) && left.name.text === "scope") ||
        (ts.isPropertyAccessChain(left) && left.name.text === "scope");
      if (
        touchesScope &&
        ts.isStringLiteral(node.right) &&
        node.right.text === "project"
      )
        f.readsScope = true;
    }

    // `api.list<…>(spec.apiPath, { … })` — объект параметров вторым аргументом.
    if (ts.isCallExpression(node)) {
      const callee = node.expression;
      const isListCall =
        (ts.isPropertyAccessExpression(callee) &&
          callee.name.text === "list") ||
        (ts.isPropertyAccessChain(callee) && callee.name.text === "list");
      if (isListCall && node.arguments.length >= 2) {
        const params = node.arguments[1];
        if (ts.isObjectLiteralExpression(params)) {
          f.sawListCall = true;
          for (const prop of params.properties) {
            if (ts.isSpreadAssignment(prop)) {
              // Спред считается защитой ТОЛЬКО когда он условный: безусловный
              // спред объекта с project_id — тот же дефект другой формой.
              //
              // Скобки снимаются: каноничная форма `...(cond ? {…} : {})` даёт
              // ParenthesizedExpression, и проверка «это условное выражение» без
              // разворота отвечала бы «нет» на КАЖДОЙ правильной копии. Ровно это
              // и произошло при первом прогоне — контроль в обе стороны его
              // поймал, потому что синтетический «хороший» источник покраснел
              // вместе с настоящими файлами.
              let expr: ts.Expression = prop.expression;
              while (ts.isParenthesizedExpression(expr)) expr = expr.expression;
              if (
                ts.isConditionalExpression(expr) ||
                ts.isBinaryExpression(expr)
              ) {
                f.conditionalSpread = true;
              }
              continue;
            }
            const name =
              (ts.isPropertyAssignment(prop) ||
                ts.isShorthandPropertyAssignment(prop)) &&
              prop.name
                ? prop.name.getText(sf)
                : "";
            if (name === "project_id" || name === '"project_id"')
              f.unconditionalProjectId = true;
          }
        }
      }
    }

    ts.forEachChild(node, visit);
  };

  visit(sf);
  return f;
}

const FILES = findRefNameLinks();

// ── Синтетические источники для контроля детектора в обе стороны ──

const BAD_SOURCE = `
const spec = REGISTRY[specId];
const { data } = useQuery({
  queryFn: () => api.list(spec!.apiPath, { project_id: projectId!, pageSize: "500" }),
  enabled: !!spec && !!projectId && !!refId,
});
`;

const GOOD_SOURCE = `
// Область видимости решает, чем спрашивать: project_id — чужой параметр для
// глобального каталога. Комментарий называет и scope, и project_id намеренно:
// гейт обязан читать исполняемую часть, а не текст.
const spec = REGISTRY[specId];
const projectScoped = spec?.scope === "project";
const { data } = useQuery({
  queryFn: () => api.list(spec!.apiPath, { ...(projectScoped ? { project_id: projectId! } : {}), pageSize: "500" }),
  enabled: !!spec && !!refId && (!projectScoped || !!projectId),
});
`;

describe("RefNameLink спрашивает чужой ресурс по ЕГО области видимости", () => {
  it(`объём осмотренного: копии компонента найдены обходом дерева (files=${FILES.length})`, () => {
    // «Ноль находок» обязано быть отличимо от «ноль прочитанного»: переехавший
    // корень или переименованный файл иначе делали бы все утверждения ниже
    // тождественно истинными.
    expect(FILES.length).toBeGreaterThan(0);
    const apps = FILES.map(
      (f) => path.relative(consoleRoot, f).split(path.sep)[0],
    ).sort();
    // Известные потребители пинятся, чтобы регрессия обхода не сузила охват молча.
    expect(apps).toEqual(
      expect.arrayContaining([
        "compute",
        "nlb",
        "registry",
        "shared",
        "storage",
      ]),
    );
    for (const file of FILES)
      expect(readFileSync(file, "utf8").length).toBeGreaterThan(0);
  });

  it("собственная предпосылка: детектор ловит прежнюю форму и молчит на нынешней", () => {
    // (а) верни дефект — гейт краснеет.
    const bad = inspectRefNameLink(BAD_SOURCE);
    expect({
      saw: bad.sawListCall,
      unconditional: bad.unconditionalProjectId,
      scope: bad.readsScope,
    }).toEqual({
      saw: true,
      unconditional: true,
      scope: false,
    });
    // (б) законная конструкция той же формы — гейт молчит. Без этого контроля
    // проверка ловила бы форму, а не существо.
    const good = inspectRefNameLink(GOOD_SOURCE);
    expect({
      saw: good.sawListCall,
      unconditional: good.unconditionalProjectId,
      spread: good.conditionalSpread,
      scope: good.readsScope,
    }).toEqual({ saw: true, unconditional: false, spread: true, scope: true });
  });

  it.each(FILES.map((f) => [path.relative(consoleRoot, f), f] as const))(
    "%s — project_id уходит только у ресурса проекта",
    (rel, file) => {
      const found = inspectRefNameLink(readFileSync(file, "utf8"));
      // Разбор обязан УВИДЕТЬ вызов списка. Не увидел — значит компонент
      // переписан так, что детектор его не понимает, и молчание гейта ничего не
      // означает; это находка, а не пропуск.
      expect({ file: rel, sawListCall: found.sawListCall }).toEqual({
        file: rel,
        sawListCall: true,
      });
      expect({
        file: rel,
        unconditionalProjectId: found.unconditionalProjectId,
        conditionalSpread: found.conditionalSpread,
        readsScope: found.readsScope,
      }).toEqual({
        file: rel,
        unconditionalProjectId: false,
        conditionalSpread: true,
        readsScope: true,
      });
    },
  );
});
