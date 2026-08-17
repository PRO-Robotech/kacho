// Разбор выражений пути консоли — по СИНТАКСИЧЕСКОМУ ДЕРЕВУ, не по тексту.
//
// # Предмет: перепись слепла от постороннего символа
//
// Прежняя редакция собирала литералы парным разбором обратных кавычек по сырому
// тексту: `` /`([^`\\]*)`|"([^"\\]*)"/g ``. Обратная кавычка внутри комментария
// сдвигает парность, и КАЖДЫЙ следующий литерал файла разбирается как
// содержимое, а не как выражение пути. То есть число, которое перепись печатала,
// зависело от разметки соседней прозы.
//
// Слепота ходила в ОБЕ стороны, и обе стороны были в дереве одновременно —
// поэтому она и не выдавала себя счётом:
//
//   · ЛОЖНОЕ ОТСУТСТВИЕ: `internalGetPath: "/vpc/v1/networks/{id}:internal"` в
//     `shared/src/lib/resource-registry.tsx` — настоящий литерал, живое
//     обращение, переписью не читался вовсе (до места объявления в файле стояло
//     нечётное число обратных кавычек);
//   · ЛОЖНОЕ ПРИСУТСТВИЕ: тот же путь в `shared/src/lib/resource-spec.ts` — но
//     там он стоит в ОБЪЯСНЯЮЩЕМ КОММЕНТАРИИ («Пример: …»), то есть переписью
//     считался вызов, которого нет.
//
// Одно место потерялось, одно лишнее прибавилось, итог совпал — 32 места и 29
// маршрутов до и после. Пин чисел этого не показывал и показать не мог.
//
// # Что здесь сделано
//
// Литералы берутся у компилятора TypeScript: `ts.createSourceFile` даёт
// синтаксическое дерево, из которого читаются узлы строковых и шаблонных
// литералов. Комментарий узлом литерала не является by construction, а парность
// кавычек разбирает лексер языка, а не регулярное выражение. Шаблонный литерал
// восстанавливается дословно — `${…}` сохраняется, потому что подстановку
// констант делает следующий шаг.
//
// # Чем это НЕ является
//
// Это не замена `strip-comments.ts` и не «второй разборщик того же». Тот снимает
// комментарии с исходника для проб, читающих ТЕКСТ (в том числе Go), и остаётся
// на своём месте. Здесь предмет другой: перечень литералов TypeScript вместе с
// восстановленной формой шаблона. Унифицировать их нельзя — один отвечает на
// «что здесь не комментарий», другой на «какие здесь литералы».
import ts from "typescript";

/**
 * Плейсхолдер неразрешённого `${…}`-сегмента пути.
 *
 * Символ выбран так, чтобы не столкнуться ни с одним настоящим сегментом URL.
 * Записан ЭКРАНИРОВАННО и ровно в одном месте: сырой управляющий байт в
 * исходнике невидим — его не показывает ни diff, ни обзор, и он молча теряется
 * при копировании строки, после чего сверка маршрутов начинает сравнивать не то,
 * что думает.
 */
export const UNRESOLVED = "\u0000";

/**
 * verbTail — путь-действие: `:verb` ПРИКЛЕЕН к предыдущему сегменту
 * (`…/{id}:start`). Обязательный не-слэш перед двоеточием отделяет действие от
 * параметра маршрута браузера (`/projects/:projectId`), который выглядит так же
 * и REST-путём не является вовсе.
 */
export const verbTail = /[^/:]:[a-zA-Z][A-Za-z0-9-]*$/;

/** Исходник консоли вместе с тем, что о нём знает вызывающий. */
export interface ConsoleSource {
  /** Путь для сообщения находки (относительный — его печатают). */
  file: string;
  /**
   * Приложение консоли. Копия реестра у каждого приложения СВОЯ и намеренно
   * расходится, поэтому путь резолвится реестром СВОЕГО приложения.
   */
  app: string;
  /** Файл — реестр ресурсов приложения (`resource-registry.tsx`). */
  isResourceRegistry: boolean;
  source: string;
}

export interface VerbRouteUse {
  file: string;
  literal: string;
  resolved: string;
}

export interface VerbRouteScan {
  uses: VerbRouteUse[];
  /** Объём осмотренного — ОТДЕЛЬНО по файлам и ОТДЕЛЬНО по литералам. */
  filesParsed: number;
  literalsParsed: number;
}

interface SourceFacts {
  /** Текст каждого литерала; `${…}` сохранены дословно. */
  literals: string[];
  /** `const NAME = "…"` — включая формы под `as const` и в скобках. */
  stringConsts: Map<string, string>;
  /** `NAME.key` → путь, из объектной константы путей. */
  objectPathConsts: Map<string, string>;
  /** `const X = REGISTRY["id"]` → id спеки. */
  registryAliases: Map<string, string>;
  /** `id` спеки ресурса → её `apiPath` (только в реестре). */
  registrySpecs: Map<string, string>;
}

function literalText(node: ts.Node, sf: ts.SourceFile): string | null {
  if (ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node)) {
    return node.text;
  }
  if (ts.isTemplateExpression(node)) {
    let out = node.head.text;
    for (const span of node.templateSpans) {
      out += "${" + span.expression.getText(sf).trim() + "}" + span.literal.text;
    }
    return out;
  }
  return null;
}

/** Снятие обёрток, не меняющих значение: `as const`, `satisfies X`, скобки. */
function unwrap(node: ts.Expression): ts.Expression {
  if (
    ts.isAsExpression(node) ||
    ts.isSatisfiesExpression(node) ||
    ts.isParenthesizedExpression(node)
  ) {
    return unwrap(node.expression);
  }
  return node;
}

function propertyName(prop: ts.PropertyAssignment): string | null {
  if (ts.isIdentifier(prop.name)) return prop.name.text;
  if (ts.isStringLiteral(prop.name)) return prop.name.text;
  return null;
}

/** parseSource — факты одного файла, прочитанные из его синтаксического дерева. */
export function parseSource(file: string, source: string): SourceFacts {
  const sf = ts.createSourceFile(
    file,
    source,
    ts.ScriptTarget.Latest,
    /* setParentNodes */ false,
    file.endsWith(".tsx") ? ts.ScriptKind.TSX : ts.ScriptKind.TS,
  );

  const facts: SourceFacts = {
    literals: [],
    stringConsts: new Map(),
    objectPathConsts: new Map(),
    registryAliases: new Map(),
    registrySpecs: new Map(),
  };

  const visit = (node: ts.Node): void => {
    const lit = literalText(node, sf);
    if (lit !== null) facts.literals.push(lit);

    if (ts.isVariableDeclaration(node) && ts.isIdentifier(node.name) && node.initializer) {
      const name = node.name.text;
      const init = unwrap(node.initializer);

      // `const NAME = "…"` — шаблон с подстановкой сюда НЕ попадает: его
      // значение неизвестно до резолва, и подставлять его как строку значило бы
      // подставить `${…}` внутрь `${…}`.
      const plain = literalText(init, sf);
      if (plain !== null && !ts.isTemplateExpression(init)) {
        facts.stringConsts.set(name, plain);
      }

      if (ts.isObjectLiteralExpression(init)) {
        for (const prop of init.properties) {
          if (!ts.isPropertyAssignment(prop)) continue;
          const key = propertyName(prop);
          const value = literalText(unwrap(prop.initializer), sf);
          if (key !== null && value !== null && value.startsWith("/")) {
            facts.objectPathConsts.set(`${name}.${key}`, value);
          }
        }
      }

      if (
        ts.isElementAccessExpression(init) &&
        ts.isIdentifier(init.expression) &&
        init.expression.text === "REGISTRY" &&
        ts.isStringLiteral(init.argumentExpression)
      ) {
        facts.registryAliases.set(name, init.argumentExpression.text);
      }
    }

    // Спека ресурса — объект, несущий и `id`, и `apiPath`. Прежняя редакция
    // искала `id:` и брала первый `apiPath` в пределах двух тысяч символов
    // ниже по тексту; здесь это одно и то же ОБЪЯВЛЕНИЕ, а не соседство строк.
    if (ts.isObjectLiteralExpression(node)) {
      let id: string | null = null;
      let apiPath: string | null = null;
      for (const prop of node.properties) {
        if (!ts.isPropertyAssignment(prop)) continue;
        const key = propertyName(prop);
        const value = literalText(unwrap(prop.initializer), sf);
        if (value === null) continue;
        if (key === "id") id = value;
        if (key === "apiPath") apiPath = value;
      }
      if (id !== null && apiPath !== null) facts.registrySpecs.set(id, apiPath);
    }

    ts.forEachChild(node, visit);
  };
  visit(sf);
  return facts;
}

/**
 * collectVerbRouteUses — все места консоли, адресующие действие-глагол.
 *
 * Разбор ОДИН и тот же для настоящего дерева и для синтетического ввода пробы:
 * фикстура не может оказаться снисходительнее того, что судит дерево.
 */
export function collectVerbRouteUses(sources: ConsoleSource[]): VerbRouteScan {
  const parsed = sources.map((s) => ({ src: s, facts: parseSource(s.file, s.source) }));

  // Объектные константы путей (`IAM.accessBindings` и подобные) объявлены в
  // одном модуле, а используются из другого — поэтому собираются по всему дереву.
  const objects = new Map<string, string>();
  for (const { facts } of parsed) {
    for (const [k, v] of facts.objectPathConsts) objects.set(k, v);
  }

  // Реестр резолвится СВОИМ приложением: копии намеренно расходятся.
  const specsByApp = new Map<string, Map<string, string>>();
  for (const { src, facts } of parsed) {
    if (!src.isResourceRegistry) continue;
    const byID = specsByApp.get(src.app) ?? new Map<string, string>();
    specsByApp.set(src.app, byID);
    for (const [k, v] of facts.registrySpecs) byID.set(k, v);
  }

  const uses: VerbRouteUse[] = [];
  let literalsParsed = 0;

  for (const { src, facts } of parsed) {
    const ownSpecs = specsByApp.get(src.app);
    const locals = new Map(facts.stringConsts);
    for (const [alias, id] of facts.registryAliases) {
      const api = ownSpecs?.get(id);
      if (api !== undefined) locals.set(`${alias}.apiPath`, api);
    }

    for (const literal of facts.literals) {
      literalsParsed++;
      if (!literal || !/:[a-zA-Z][A-Za-z0-9-]*$/.test(literal)) continue;
      if (!literal.includes("/") && !literal.startsWith("${")) continue;

      const resolved = literal.replace(/\$\{([^}]*)\}/g, (_all, expr: string) => {
        const key = expr.trim();
        const local = locals.get(key);
        if (local !== undefined) return local;
        const obj = objects.get(key);
        if (obj !== undefined) return obj;
        return UNRESOLVED;
      });
      if (!resolved.startsWith("/")) continue;
      // Проверка `verbTail` идёт по РАЗРЕШЁННОМУ пути: до подстановки перед
      // двоеточием стоит `}` у обеих форм, и действие от параметра маршрута
      // так не отличить.
      if (!verbTail.test(resolved)) continue;
      uses.push({ file: src.file, literal, resolved });
    }
  }

  return { uses, filesParsed: parsed.length, literalsParsed };
}
