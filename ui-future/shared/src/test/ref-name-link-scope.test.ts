import { readdirSync, readFileSync, statSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import ts from "typescript";

/**
 * Гейт: ссылка на ЧУЖОЙ ресурс спрашивает его именем по ЕГО области видимости —
 * и делает это ОДНОЙ реализацией на всё дерево.
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
 * # Гейт ПЕРЕЕХАЛ вместе со своим предметом — и что именно изменилось
 *
 * Заводился он на дереве с ПЯТЬЮ копиями компонента, байт-в-байт совпадавшими в
 * этом месте, и утверждал свойство про КАЖДУЮ копию: правка одной закрыла бы
 * находку и оставила класс. Копии сведены к прослойкам на общую реализацию —
 * значит прежняя форма утверждения перестала что-либо означать: у прослойки нет
 * своего запроса, и требовать от неё «увидеть вызов списка» — требовать формы,
 * которой у правильного файла быть не может. Заодно рассыпался пин потребителей
 * `["compute","nlb","registry","shared","storage"]`: у registry копия снята
 * ЦЕЛИКОМ (его `spec-columns` тоже стал прослойкой, и адрес модуля перестал
 * кто-либо спрашивать) — то есть список описывал дерево, которого больше нет.
 *
 * Поэтому охват здесь больше НЕ ВЫПИСАН, а ВЫВОДИТСЯ: потребители ищутся по
 * дереву (кто спрашивает `RefNameLink` из прод-кода), и по каждому проверяется,
 * что его адрес приводит к общей реализации. Рукописный список пережил бы
 * следующее сведение форка точно так же, как пережил это.
 *
 * # Что утверждается
 *
 * Разбором AST (а не поиском слова в тексте — слово находится и в комментарии,
 * объясняющем эту же защиту):
 *   (1) каждый файл `RefNameLink.tsx` опознан: он либо РЕАЛИЗАЦИЯ (несёт вызов
 *       списка), либо ПРОСЛОЙКА (только ре-экспорты, и ведут они на общий
 *       `RefNameLink`). Файл, не подошедший ни подо что, — находка, а не
 *       пропуск: молчание гейта на непонятом файле не означает ничего;
 *   (2) реализация РОВНО ОДНА, и лежит она в `shared`. Вторая — это возвращённый
 *       форк, ради которого гейт и заводился;
 *   (3) эта реализация не шлёт `project_id` безусловно, добавляет его условным
 *       спредом и читает `.scope === "project"`;
 *   (4) каждое приложение, чей ПРОД-код спрашивает `RefNameLink`, приходит к
 *       общей реализации — прямым `@shared/…` либо своей прослойкой.
 *
 * # Объём осмотренного и собственная предпосылка
 *
 * Число найденных файлов и выведенное множество потребителей утверждаются
 * непустыми: «ноль находок» обязано быть отличимо от «ноль прочитанного», а
 * переименованный каталог не должен превращать обход в пустой. Сам детектор
 * проверяется в ОБЕ стороны на синтетических источниках — включая тот, что
 * ВЫГЛЯДИТ прослойкой, но несёт свою реализацию рядом: без него форк прятался бы
 * за одной строкой ре-экспорта.
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

/** Адрес общей реализации во всех формах, которыми её законно спрашивают. */
const SHARED_SPECIFIER =
  /^@shared\/components\/molecules\/RefNameLink(\/RefNameLink|\/index)?$/;
/** Любой адрес `RefNameLink` — общий или модульный. */
const ANY_SPECIFIER =
  /^@(shared|)\/components\/molecules\/RefNameLink(\/RefNameLink|\/index)?$/;

function appDirs(): string[] {
  const out: string[] = [];
  for (const name of readdirSync(consoleRoot)) {
    if (NOT_APPS.has(name)) continue;
    const dir = path.join(consoleRoot, name);
    if (!statSync(dir).isDirectory()) continue;
    try {
      if (statSync(path.join(dir, "src")).isDirectory()) out.push(name);
    } catch {
      // каталог без src/ — не приложение
    }
  }
  return out.sort();
}

function walkSources(dir: string, take: (full: string) => void): void {
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    if (
      entry.name === "node_modules" ||
      entry.name === "dist" ||
      entry.name === ".git"
    )
      continue;
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) {
      walkSources(full, take);
      continue;
    }
    take(full);
  }
}

function findRefNameLinks(): string[] {
  const out: string[] = [];
  for (const app of appDirs())
    walkSources(path.join(consoleRoot, app, "src"), (full) => {
      if (path.basename(full) === "RefNameLink.tsx") out.push(full);
    });
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
  /** Ре-экспорты ВЕДУТ на общий `RefNameLink` (и таких объявлений ≥1). */
  reexportsShared: boolean;
  /** Есть ли в файле собственные объявления значений (функция/класс/переменная). */
  declaresOwnValues: boolean;
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
    reexportsShared: false,
    declaresOwnValues: false,
  };

  // Верхний уровень читается отдельно: «файл только ре-экспортирует» —
  // утверждение о СОСТАВЕ файла, а не о наличии где-то нужной строки.
  for (const st of sf.statements) {
    if (ts.isExportDeclaration(st) && st.moduleSpecifier) {
      const spec = ts.isStringLiteral(st.moduleSpecifier)
        ? st.moduleSpecifier.text
        : "";
      if (SHARED_SPECIFIER.test(spec)) f.reexportsShared = true;
      continue;
    }
    if (ts.isImportDeclaration(st) || ts.isExportDeclaration(st)) continue;
    if (
      ts.isFunctionDeclaration(st) ||
      ts.isClassDeclaration(st) ||
      ts.isVariableStatement(st) ||
      ts.isExportAssignment(st)
    )
      f.declaresOwnValues = true;
  }

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

export type RefNameLinkKind = "реализация" | "прослойка" | "неопознан";

/**
 * К какому виду относится файл.
 *
 * Порядок ветвей — существо классификатора: РЕАЛИЗАЦИЯ решается первой, поэтому
 * файл, приписавший к своему запросу строку `export * from "@shared/…"`, будет
 * назван реализацией (то есть вторым форком), а не спрячется за ре-экспортом.
 * «Прослойка» — опознание ПОЛОЖИТЕЛЬНОЕ (ре-экспорт ведёт на общий адрес, своих
 * значений нет), а не «детектор ничего не нашёл»: иначе переписанная реализация,
 * которую разбор не понял, молча считалась бы прослойкой.
 */
export function classifyRefNameLink(f: Finding): RefNameLinkKind {
  if (f.sawListCall || f.declaresOwnValues) return "реализация";
  if (f.reexportsShared) return "прослойка";
  return "неопознан";
}

/** Приложения, чей ПРОД-код спрашивает `RefNameLink`, и каким адресом. */
function consumersByApp(): Map<string, { shared: boolean; local: boolean }> {
  const out = new Map<string, { shared: boolean; local: boolean }>();
  for (const app of appDirs()) {
    walkSources(path.join(consoleRoot, app, "src"), (full) => {
      if (!/\.tsx?$/.test(full)) return;
      if (/\.test\.tsx?$/.test(full)) return;
      // Сам файл прослойки потребителем себя не делает.
      if (path.basename(full) === "RefNameLink.tsx") return;
      const sf = ts.createSourceFile(
        full,
        readFileSync(full, "utf8"),
        ts.ScriptTarget.Latest,
        true,
        ts.ScriptKind.TSX,
      );
      for (const st of sf.statements) {
        const ms =
          (ts.isImportDeclaration(st) || ts.isExportDeclaration(st)) &&
          st.moduleSpecifier &&
          ts.isStringLiteral(st.moduleSpecifier)
            ? st.moduleSpecifier.text
            : "";
        if (!ms || !ANY_SPECIFIER.test(ms)) continue;
        const cur = out.get(app) ?? { shared: false, local: false };
        if (ms.startsWith("@shared/")) cur.shared = true;
        else cur.local = true;
        out.set(app, cur);
      }
    });
  }
  return out;
}

const FILES = findRefNameLinks();
const KIND = new Map(
  FILES.map(
    (f) =>
      [f, classifyRefNameLink(inspectRefNameLink(readFileSync(f, "utf8")))] as const,
  ),
);
const appOf = (file: string) =>
  path.relative(consoleRoot, file).split(path.sep)[0];
const IMPLEMENTATIONS = FILES.filter((f) => KIND.get(f) === "реализация");
const SHIMS = FILES.filter((f) => KIND.get(f) === "прослойка");
const CONSUMERS = consumersByApp();

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

/** Законная прослойка — ровно та форма, что лежит в модулях. */
const REEXPORT_SOURCE = `
// Ссылка на чужой ресурс по имени — РЕ-ЭКСПОРТ общей реализации.
// Комментарий называет и project_id, и scope намеренно.
export * from "@shared/components/molecules/RefNameLink";
`;

/** Форк, замаскированный строкой ре-экспорта: своя реализация РЯДОМ с ней. */
const FAKE_REEXPORT_SOURCE = `
export * from "@shared/components/molecules/RefNameLink";

export function RefNameLinkFast({ specId, refId }: Props) {
  const { data } = useQuery({
    queryFn: () => api.list(REGISTRY[specId].apiPath, { project_id: projectId!, pageSize: "500" }),
  });
  return data ? <span>{refId}</span> : null;
}
`;

/** Ре-экспорт НЕ ТУДА: адрес есть, ведёт не на общую реализацию. */
const FOREIGN_REEXPORT_SOURCE = `
export * from "@shared/components/molecules/IamRefLink";
`;

describe("RefNameLink спрашивает чужой ресурс по ЕГО области видимости", () => {
  it(
    `объём осмотренного: файлов ${FILES.length} ` +
      `(реализаций ${IMPLEMENTATIONS.length}, прослоек ${SHIMS.length}), ` +
      `приложений-потребителей ${CONSUMERS.size} [${[...CONSUMERS.keys()].sort().join(", ")}]`,
    () => {
      // «Ноль находок» обязано быть отличимо от «ноль прочитанного»: переехавший
      // корень или переименованный файл иначе делали бы все утверждения ниже
      // тождественно истинными.
      expect(FILES.length).toBeGreaterThan(0);
      expect(CONSUMERS.size).toBeGreaterThan(0);
      for (const file of FILES)
        expect(readFileSync(file, "utf8").length).toBeGreaterThan(0);
    },
  );

  it("каждый найденный файл ОПОЗНАН — реализация либо прослойка", () => {
    // Непонятый файл — находка, а не пропуск: молчание разбора о нём не
    // означает ничего, и именно под этим молчанием прошёл бы переписанный форк.
    const неопознанные = FILES.filter((f) => KIND.get(f) === "неопознан").map(
      (f) => path.relative(consoleRoot, f),
    );
    expect(неопознанные).toEqual([]);
  });

  it("реализация ОДНА, и она в shared", () => {
    // Именно это свойство закрыло класс, ради которого гейт заводился: пока
    // копий было пять, правка одной молча минует остальные.
    expect(IMPLEMENTATIONS.map((f) => path.relative(consoleRoot, f))).toHaveLength(1);
    expect(appOf(IMPLEMENTATIONS[0])).toBe("shared");
  });

  it("единственная реализация шлёт project_id ТОЛЬКО ресурсу проекта", () => {
    const rel = path.relative(consoleRoot, IMPLEMENTATIONS[0]);
    const found = inspectRefNameLink(readFileSync(IMPLEMENTATIONS[0], "utf8"));
    expect({
      file: rel,
      sawListCall: found.sawListCall,
      unconditionalProjectId: found.unconditionalProjectId,
      conditionalSpread: found.conditionalSpread,
      readsScope: found.readsScope,
    }).toEqual({
      file: rel,
      sawListCall: true,
      unconditionalProjectId: false,
      conditionalSpread: true,
      readsScope: true,
    });
  });

  it("каждый потребитель приходит к общей реализации — охват ВЫВЕДЕН из дерева", () => {
    // Список потребителей здесь не выписан: рукописный пережил бы сведение
    // форка (и уже пережил — registry сняли, а пин остался требовать его).
    const находки: string[] = [];
    for (const [app, how] of CONSUMERS) {
      if (how.shared && !how.local) continue; // спрашивает общий адрес напрямую
      const свои = SHIMS.filter((f) => appOf(f) === app);
      if (свои.length === 0)
        находки.push(
          `${app}: спрашивает RefNameLink по адресу модуля, а прослойки на общую реализацию в нём нет`,
        );
    }
    expect(находки).toEqual([]);
  });

  it("собственная предпосылка: детектор ловит прежнюю форму и молчит на нынешней", () => {
    // (а) верни дефект — гейт краснеет.
    const bad = inspectRefNameLink(BAD_SOURCE);
    expect({
      kind: classifyRefNameLink(bad),
      saw: bad.sawListCall,
      unconditional: bad.unconditionalProjectId,
      scope: bad.readsScope,
    }).toEqual({
      kind: "реализация",
      saw: true,
      unconditional: true,
      scope: false,
    });
    // (б) законная конструкция той же формы — гейт молчит. Без этого контроля
    // проверка ловила бы форму, а не существо.
    const good = inspectRefNameLink(GOOD_SOURCE);
    expect({
      kind: classifyRefNameLink(good),
      saw: good.sawListCall,
      unconditional: good.unconditionalProjectId,
      spread: good.conditionalSpread,
      scope: good.readsScope,
    }).toEqual({
      kind: "реализация",
      saw: true,
      unconditional: false,
      spread: true,
      scope: true,
    });
  });

  it("собственная предпосылка: прослойка опознаётся ПОЛОЖИТЕЛЬНО, а форк за ней не прячется", () => {
    // (в) законная прослойка — вид «прослойка», а не «неопознан».
    expect(classifyRefNameLink(inspectRefNameLink(REEXPORT_SOURCE))).toBe("прослойка");
    // (г) та же строка ре-экспорта плюс своя реализация рядом — это РЕАЛИЗАЦИЯ,
    // то есть второй форк. Без этого контроля форк маскировался бы одной
    // строкой, а гейт считал бы его прослойкой и молчал.
    expect(classifyRefNameLink(inspectRefNameLink(FAKE_REEXPORT_SOURCE))).toBe("реализация");
    // (д) ре-экспорт, ведущий НЕ на общую реализацию, прослойкой не считается:
    // иначе «ведёт куда угодно» было бы неотличимо от «ведёт к общему».
    expect(classifyRefNameLink(inspectRefNameLink(FOREIGN_REEXPORT_SOURCE))).toBe("неопознан");
  });
});
