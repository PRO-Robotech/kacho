import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import ts from "typescript";

import {
  consoleRoot,
  listConsoleSources,
  readMessageFields,
  repeatedMessageFieldNames,
} from "./proto-contract";

/**
 * Гейт: состав ЧЕРНОВИКА НАБОРА против состава сообщения контракта.
 *
 * # Класс
 *
 * Форма правит набор, читая его с сервера в черновик и отправляя весь массив
 * обратно. Поле контракта, которого черновик не назвал, исчезает у ВСЕХ
 * элементов — включая те, которых оператор не касался. Край получает валидный
 * запрос, где поля просто нет: ни отказа, ни предупреждения (#422, #498).
 *
 * # Чем распознаётся место полной замены — и почему это ИСПОЛНЯЕМАЯ часть
 *
 * Разбором AST, а не поиском слова в тексте: слово находится и в комментарии,
 * объясняющем эту же защиту, — такой гейт остался бы зелёным при снятой
 * защите. Признак — связка двух фактов:
 *
 *   (1) записывается НАБОР: свойство объекта / присваивание / `setByPath`,
 *       чьё ИМЯ есть `repeated`-поле СООБЩЕНЧЕСКОГО типа хотя бы одного
 *       контракта дерева (словарь строится чтением `proto/`, не выписан), а
 *       значение — массив, элементы которого НЕ построены на месте: иначе это
 *       не замена загруженного, а свежесозданное тело, где терять нечего;
 *   (2) запись доходит до КРАЯ: в объемлющей функции есть вызов клиента края
 *       (`api.create|update|post|patch|put|action|del`) или запуск мутации
 *       (`mutateAsync`/`mutate`/`run`), ЛИБО место стоит внутри свойства схемы
 *       формы, тело которой уезжает на край (`sanitize`/`render`/`template`/
 *       `mutationFn`).
 *
 * `repeated string` в словарь не входит намеренно: у элемента-строки нет
 * полей, терять нечего.
 *
 * # Почему пятый черновик попадает под наблюдение ПО ПОСТРОЕНИЮ
 *
 * Перепись мест берётся ОБХОДОМ ДЕРЕВА. Новое место обязано нести объявление
 * `SetReplacementDraft` в своём файле; нет объявления — красное с координатой.
 * Перечня мест, который надо не забыть пополнить, не существует: объявление
 * стоит там же, где код, и переезжает вместе с ним.
 *
 * Обратная сторона держится тем же обходом: объявление, которому в его файле
 * больше нечего описывать, — находка, а не остаток. Исключение обязано
 * истекать само.
 *
 * Она же не даёт детектору ОСЛЕПНУТЬ незаметно. Сломайся распознавание — мест
 * станет ноль, и «недекларированных» тоже ноль; но объявления-то останутся, и
 * каждое из них станет беспредметным. То есть тихое зеленение от собственной
 * поломки невозможно, пока в дереве есть хоть одно объявление.
 *
 * # Что проверяется по каждому объявлению
 *
 * Читается САМ `.proto` (не его отражение в коде консоли — отражение и есть
 * предмет проверки): КАЖДОЕ поле сообщения обязано быть названо членом КАЖДОГО
 * типа-черновика. Лишние члены — служебные, только для представления — законны
 * и не проверяются: черновик вправе нести своё, но не вправе умалчивать чужое.
 *
 * Индексная сигнатура (`[k: string]: unknown`) членом НЕ считается. Она и есть
 * та самая надежда на распространение объекта: поле выживает случайно, а не
 * потому, что черновик его назвал.
 *
 * Типы-черновики разрешаются ПО ИМЕНИ во всём дереве, поэтому форк типа
 * (копия того же имени в другом модуле консоли) проверяется вместе с
 * оригиналом — а не остаётся незамеченным ровно потому, что он копия.
 *
 * # Объём осмотренного
 *
 * Перепись — отдельное утверждение: файлов осмотрено, контрактов прочитано,
 * мест найдено, объявлений найдено, типов сверено, полей сверено. «Ноль
 * находок» обязано быть отличимо от «ноль прочитанного». На ПУСТОМ перечне
 * мест гейт проходит, объявляя перепись: пустой перечень есть цель, а не
 * поломка.
 *
 * # Инъекция
 *
 * Способность детектора упасть и смолчать доказывается на синтетике здесь же
 * (проба «собственная предпосылка»), в обе стороны: место без объявления даёт
 * находку, оно же с объявлением — молчит; черновик без поля контракта даёт
 * находку С ИМЕНЕМ ПОЛЯ, он же с лишним полем, которого в контракте нет, —
 * молчит.
 */

const CONSOLE_ROOT = consoleRoot(path.dirname(fileURLToPath(import.meta.url)));

/** Методы, дающие новый массив из существующего. */
const ARRAY_METHODS = new Set(["map", "filter", "slice", "concat", "flatMap", "toSorted", "toReversed"]);

/** Вызовы, означающие «это уехало на край». */
const EDGE_CALL = /(^|\.)api\.(create|update|post|patch|put|action|del)$|(^|\.)(mutateAsync|mutate|run)$/;

/** Свойства схемы формы, чьё тело уезжает на край целиком. */
const SCHEMA_PROPS = new Set(["sanitize", "render", "template", "mutationFn"]);

export interface ReplacementSite {
  line: number;
  field: string;
  form: "literal" | "assign" | "path";
}

export interface DraftDeclaration {
  line: number;
  field: string;
  contract: string;
  message: string;
  drafts: string[];
}

export interface TypeDeclaration {
  name: string;
  line: number;
  members: string[];
  /** Индексная сигнатура есть — она членом НЕ считается, но о ней стоит знать в отчёте. */
  indexSignature: boolean;
}

export interface FileScan {
  sites: ReplacementSite[];
  declarations: DraftDeclaration[];
  types: TypeDeclaration[];
}

function unwrap(node: ts.Node): ts.Node {
  let n = node;
  while (ts.isParenthesizedExpression(n) || ts.isAsExpression(n) || ts.isNonNullExpression(n)) n = n.expression;
  return n;
}

/**
 * Значение заменяет набор целиком тем, что пришло откуда-то ещё.
 *
 * Литерал массива из ОБЪЕКТНЫХ литералов исключён намеренно: элементы построены
 * здесь же, загруженного нет, терять нечего. Пустой литерал — тоже (это шаблон
 * новой формы).
 */
function replacesWholeSet(expr: ts.Expression): boolean {
  const x = unwrap(expr) as ts.Expression;
  if (ts.isArrayLiteralExpression(x))
    return x.elements.length > 0 && x.elements.every((el) => !ts.isObjectLiteralExpression(unwrap(el)));
  if (ts.isIdentifier(x)) return x.text !== "undefined" && x.text !== "null";
  if (ts.isPropertyAccessExpression(x) || ts.isElementAccessExpression(x)) return true;
  if (ts.isCallExpression(x)) {
    const callee = x.expression;
    return (
      (ts.isPropertyAccessExpression(callee) || ts.isPropertyAccessChain(callee)) &&
      ARRAY_METHODS.has(callee.name.text)
    );
  }
  if (ts.isConditionalExpression(x)) return replacesWholeSet(x.whenTrue) && replacesWholeSet(x.whenFalse);
  if (ts.isBinaryExpression(x) && x.operatorToken.kind === ts.SyntaxKind.QuestionQuestionToken)
    return replacesWholeSet(x.left) || replacesWholeSet(x.right);
  return false;
}

function reachesEdge(node: ts.Node, sf: ts.SourceFile): boolean {
  for (let n: ts.Node | undefined = node; n; n = n.parent) {
    if (
      ts.isPropertyAssignment(n) &&
      (ts.isIdentifier(n.name) || ts.isStringLiteral(n.name)) &&
      SCHEMA_PROPS.has(n.name.text)
    )
      return true;
    const isFn =
      ts.isFunctionDeclaration(n) ||
      ts.isFunctionExpression(n) ||
      ts.isArrowFunction(n) ||
      ts.isMethodDeclaration(n);
    if (!isFn) continue;
    let found = false;
    const look = (x: ts.Node): void => {
      if (found) return;
      if (ts.isCallExpression(x) && EDGE_CALL.test(x.expression.getText(sf).trim())) found = true;
      ts.forEachChild(x, look);
    };
    look(n);
    if (found) return true;
  }
  return false;
}

function membersOf(node: ts.InterfaceDeclaration | ts.TypeLiteralNode): {
  members: string[];
  indexSignature: boolean;
} {
  const members: string[] = [];
  let indexSignature = false;
  for (const m of node.members) {
    if (ts.isIndexSignatureDeclaration(m)) {
      indexSignature = true;
      continue;
    }
    if ((ts.isPropertySignature(m) || ts.isMethodSignature(m)) && m.name) {
      if (ts.isIdentifier(m.name) || ts.isStringLiteral(m.name)) members.push(m.name.text);
    }
  }
  return { members, indexSignature };
}

/** Разбор одного исходника: места полной замены, объявления и объявленные типы. */
export function inspectSource(fileName: string, source: string, repeated: ReadonlySet<string>): FileScan {
  const sf = ts.createSourceFile(
    fileName,
    source,
    ts.ScriptTarget.Latest,
    true,
    fileName.endsWith(".tsx") ? ts.ScriptKind.TSX : ts.ScriptKind.TS,
  );
  const at = (n: ts.Node) => sf.getLineAndCharacterOfPosition(n.getStart(sf)).line + 1;
  const scan: FileScan = { sites: [], declarations: [], types: [] };

  const strings = (node: ts.Node | undefined): string[] =>
    node && ts.isArrayLiteralExpression(node)
      ? node.elements.filter(ts.isStringLiteral).map((e) => e.text)
      : [];
  const stringProp = (obj: ts.ObjectLiteralExpression, key: string): string | null => {
    for (const p of obj.properties)
      if (ts.isPropertyAssignment(p) && (ts.isIdentifier(p.name) || ts.isStringLiteral(p.name)) && p.name.text === key)
        if (ts.isStringLiteral(p.initializer)) return p.initializer.text;
    return null;
  };
  const arrayProp = (obj: ts.ObjectLiteralExpression, key: string): ts.Expression | undefined => {
    for (const p of obj.properties)
      if (ts.isPropertyAssignment(p) && (ts.isIdentifier(p.name) || ts.isStringLiteral(p.name)) && p.name.text === key)
        return p.initializer;
    return undefined;
  };

  const visit = (node: ts.Node): void => {
    // ── объявленные типы (кандидаты в черновики) ──
    if (ts.isInterfaceDeclaration(node)) {
      const { members, indexSignature } = membersOf(node);
      scan.types.push({ name: node.name.text, line: at(node), members, indexSignature });
    }
    if (ts.isTypeAliasDeclaration(node) && ts.isTypeLiteralNode(node.type)) {
      const { members, indexSignature } = membersOf(node.type);
      scan.types.push({ name: node.name.text, line: at(node), members, indexSignature });
    }

    // ── объявление `const X: SetReplacementDraft = { … }` ──
    if (
      ts.isVariableDeclaration(node) &&
      node.type &&
      ts.isTypeReferenceNode(node.type) &&
      ts.isIdentifier(node.type.typeName) &&
      node.type.typeName.text === "SetReplacementDraft" &&
      node.initializer
    ) {
      const obj = unwrap(node.initializer);
      if (ts.isObjectLiteralExpression(obj)) {
        const field = stringProp(obj, "field");
        const contract = stringProp(obj, "contract");
        const message = stringProp(obj, "message");
        const drafts = strings(arrayProp(obj, "drafts"));
        if (field && contract && message)
          scan.declarations.push({ line: at(node), field, contract, message, drafts });
      }
    }

    // ── места полной замены ──
    if (ts.isPropertyAssignment(node)) {
      const key = ts.isIdentifier(node.name) || ts.isStringLiteral(node.name) ? node.name.text : null;
      if (key && repeated.has(key) && replacesWholeSet(node.initializer) && reachesEdge(node, sf))
        scan.sites.push({ line: at(node), field: key, form: "literal" });
    }
    if (
      ts.isBinaryExpression(node) &&
      node.operatorToken.kind === ts.SyntaxKind.EqualsToken &&
      (ts.isPropertyAccessExpression(node.left) || ts.isElementAccessExpression(node.left))
    ) {
      const left = node.left;
      const key = ts.isPropertyAccessExpression(left)
        ? left.name.text
        : ts.isStringLiteral(left.argumentExpression)
          ? left.argumentExpression.text
          : null;
      if (key && repeated.has(key) && replacesWholeSet(node.right) && reachesEdge(node, sf))
        scan.sites.push({ line: at(node), field: key, form: "assign" });
    }
    if (
      ts.isCallExpression(node) &&
      ts.isIdentifier(node.expression) &&
      node.expression.text === "setByPath" &&
      node.arguments.length >= 3
    ) {
      const key = node.arguments[1];
      if (ts.isStringLiteral(key) && repeated.has(key.text) && replacesWholeSet(node.arguments[2]) && reachesEdge(node, sf))
        scan.sites.push({ line: at(node), field: key.text, form: "path" });
    }

    ts.forEachChild(node, visit);
  };

  visit(sf);
  return scan;
}

// ── обход дерева ──

const { names: REPEATED, contractsRead: CONTRACTS_READ, messagesRead: MESSAGES_READ } = repeatedMessageFieldNames();
const SOURCES = listConsoleSources(CONSOLE_ROOT);

const SCANS = SOURCES.map((file) => ({
  file,
  rel: path.relative(CONSOLE_ROOT, file),
  scan: inspectSource(file, readFileSync(file, "utf8"), REPEATED),
}));

/** Все объявленные в дереве типы по имени — форк того же имени проверяется вместе с оригиналом. */
const TYPES_BY_NAME = new Map<string, { rel: string; type: TypeDeclaration }[]>();
for (const s of SCANS)
  for (const t of s.scan.types) {
    const bucket = TYPES_BY_NAME.get(t.name) ?? [];
    bucket.push({ rel: s.rel, type: t });
    TYPES_BY_NAME.set(t.name, bucket);
  }

const ALL_SITES = SCANS.flatMap((s) => s.scan.sites.map((site) => ({ rel: s.rel, ...site })));
const ALL_DECLS = SCANS.flatMap((s) => s.scan.declarations.map((d) => ({ rel: s.rel, ...d })));

/** Место без объявления в своём файле — новый черновик, о котором никто не заявил. */
const UNDECLARED = ALL_SITES.filter(
  (site) => !ALL_DECLS.some((d) => d.rel === site.rel && d.field === site.field),
).map((s) => `${s.rel}:${s.line} — набор «${s.field}» заменяется целиком, объявления SetReplacementDraft нет`);

/** Объявление, которому больше нечего описывать, — находка: исключение обязано истекать само. */
const ORPHAN_DECLS = ALL_DECLS.filter(
  (d) => !ALL_SITES.some((site) => site.rel === d.rel && site.field === d.field),
).map((d) => `${d.rel}:${d.line} — объявлен черновик «${d.field}», а места полной замены в этом файле нет`);

interface CompositionFinding {
  where: string;
  draft: string;
  message: string;
  missing: string[];
}

const COMPOSITION: CompositionFinding[] = [];
let TYPES_CHECKED = 0;
let FIELDS_CHECKED = 0;

for (const d of ALL_DECLS) {
  const fields = readMessageFields(d.contract, d.message).map((f) => f.name);
  if (d.drafts.length === 0)
    COMPOSITION.push({ where: `${d.rel}:${d.line}`, draft: "(не назван)", message: d.message, missing: fields });
  for (const draftName of d.drafts) {
    const found = TYPES_BY_NAME.get(draftName) ?? [];
    if (found.length === 0) {
      COMPOSITION.push({
        where: `${d.rel}:${d.line}`,
        draft: draftName,
        message: d.message,
        missing: ["(тип с таким именем в дереве консоли не объявлен)"],
      });
      continue;
    }
    for (const { rel, type } of found) {
      TYPES_CHECKED += 1;
      FIELDS_CHECKED += fields.length;
      const missing = fields.filter((f) => !type.members.includes(f));
      if (missing.length > 0)
        COMPOSITION.push({ where: `${rel}:${type.line}`, draft: draftName, message: d.message, missing });
    }
  }
}

// ── синтетика для контроля детектора в обе стороны ──

const SITE_WITHOUT_DECLARATION = `
const save = async () => {
  await api.update(\`/vpc/v1/routeTables/\${id}\`, { static_routes: next, update_mask: "staticRoutes" });
};
`;

const SITE_WITH_DECLARATION = `
import type { SetReplacementDraft } from "@shared/lib/set-replacement-draft";
// Комментарий называет и static_routes, и SetReplacementDraft намеренно: гейт
// обязан читать исполняемую часть, а не текст.
export const DRAFT: SetReplacementDraft = {
  field: "static_routes",
  contract: "kacho/cloud/vpc/v1/route_table.proto",
  message: "StaticRoute",
  drafts: ["DraftRoute"],
};
const save = async () => {
  await api.update(\`/vpc/v1/routeTables/\${id}\`, { static_routes: next, update_mask: "staticRoutes" });
};
`;

const FRESH_BODY_NOT_A_REPLACEMENT = `
const add = async () => {
  await api.create("/iam/v1/accessBindings", { subjects: [{ type: "USER", id: userId }] });
};
`;

const DRAFT_MISSING_A_FIELD = `
export interface RouteEntry {
  destination_prefix: string;
  next_hop_address?: string;
  gateway_id?: string;
}
`;

const DRAFT_WITH_PRESENTATION_ONLY_MEMBER = `
export interface RouteEntry {
  destination_prefix: string;
  next_hop_address?: string;
  gateway_id?: string;
  labels?: Record<string, string>;
  /** Только для представления: в контракте такого поля нет. */
  _row_open?: boolean;
}
`;

const DRAFT_LEANING_ON_AN_INDEX_SIGNATURE = `
export interface RouteEntry {
  destination_prefix: string;
  next_hop_address?: string;
  gateway_id?: string;
  [k: string]: unknown;
}
`;

function membersOfSynthetic(src: string, name: string): TypeDeclaration {
  const scan = inspectSource("synthetic.ts", src, REPEATED);
  const t = scan.types.find((x) => x.name === name);
  if (!t) throw new Error(`синтетика не разобрана: тип ${name} не найден`);
  return t;
}

describe("состав черновика набора против состава контракта", () => {
  it(`объём осмотренного: контрактов ${CONTRACTS_READ} · сообщений ${MESSAGES_READ} · файлов консоли ${SOURCES.length} · мест полной замены ${ALL_SITES.length} · объявлений ${ALL_DECLS.length} · типов сверено ${TYPES_CHECKED} · полей сверено ${FIELDS_CHECKED}`, () => {
    // «Ноль находок» обязано быть отличимо от «ноль прочитанного»: переехавший
    // корень или переименованный каталог иначе делали бы все утверждения ниже
    // тождественно истинными.
    expect(CONTRACTS_READ).toBeGreaterThan(0);
    expect(MESSAGES_READ).toBeGreaterThan(0);
    expect(REPEATED.size).toBeGreaterThan(0);
    expect(SOURCES.length).toBeGreaterThan(0);
    // Приложения консоли, чьи исходники обязаны попасть в обход. Перечень пинится,
    // чтобы регрессия обхода не сузила охват молча.
    const apps = new Set(SCANS.map((s) => s.rel.split(path.sep)[0]));
    expect([...apps].sort()).toEqual(expect.arrayContaining(["compute", "iam", "nlb", "shared", "storage", "vpc"]));
    // Пустой перечень мест — законный исход (цель, а не поломка): утверждения
    // ниже на нём проходят. Здесь он не запрещается, а только называется.
  });

  it("собственная предпосылка: детектор находит место полной замены и молчит на свежесозданном теле", () => {
    // (а) верни дефект — гейт видит место и не видит объявления.
    const bad = inspectSource("bad.ts", SITE_WITHOUT_DECLARATION, REPEATED);
    expect({ sites: bad.sites.map((s) => s.field), decls: bad.declarations.length }).toEqual({
      sites: ["static_routes"],
      decls: 0,
    });
    // (б) законная конструкция той же формы — место объявлено, находки нет.
    const good = inspectSource("good.ts", SITE_WITH_DECLARATION, REPEATED);
    expect({
      sites: good.sites.map((s) => s.field),
      decls: good.declarations.map((d) => `${d.field}→${d.message}[${d.drafts.join(",")}]`),
    }).toEqual({
      sites: ["static_routes"],
      decls: ["static_routes→StaticRoute[DraftRoute]"],
    });
    // (в) тело, собранное здесь же, заменой ЗАГРУЖЕННОГО не является — молчим.
    const fresh = inspectSource("fresh.ts", FRESH_BODY_NOT_A_REPLACEMENT, REPEATED);
    expect(fresh.sites).toEqual([]);
  });

  it("собственная предпосылка: сверка состава называет НЕДОСТАЮЩЕЕ поле и молчит на лишнем", () => {
    const contract = readMessageFields("kacho/cloud/vpc/v1/route_table.proto", "StaticRoute").map((f) => f.name);
    expect(contract).toEqual(expect.arrayContaining(["destination_prefix", "next_hop_address", "gateway_id", "labels"]));

    // (а) поле контракта не названо — находка, и она называет ИМЕННО его.
    const short = membersOfSynthetic(DRAFT_MISSING_A_FIELD, "RouteEntry");
    expect(contract.filter((f) => !short.members.includes(f))).toEqual(["labels"]);

    // (б) член, которого в контракте нет вовсе (только для представления), —
    // законен: черновик вправе нести своё, но не вправе умалчивать чужое.
    const withExtra = membersOfSynthetic(DRAFT_WITH_PRESENTATION_ONLY_MEMBER, "RouteEntry");
    expect(contract.filter((f) => !withExtra.members.includes(f))).toEqual([]);
    expect(withExtra.members).toContain("_row_open");

    // (в) индексная сигнатура членом НЕ считается — это и есть надежда на
    // распространение объекта вместо утверждения.
    const indexed = membersOfSynthetic(DRAFT_LEANING_ON_AN_INDEX_SIGNATURE, "RouteEntry");
    expect(indexed.indexSignature).toBe(true);
    expect(contract.filter((f) => !indexed.members.includes(f))).toEqual(["labels"]);
  });

  it("каждое место полной замены объявлено", () => {
    expect(UNDECLARED).toEqual([]);
  });

  it("каждое объявление имеет предмет — иначе оно пережило свою работу", () => {
    expect(ORPHAN_DECLS).toEqual([]);
  });

  it("черновик называет КАЖДОЕ поле своего сообщения контракта", () => {
    // Один тип-черновик может быть назван несколькими объявлениями (одна форма
    // правит набор в двух местах) — находка при этом одна.
    const lines = [
      ...new Set(
        COMPOSITION.map((c) => `${c.where} — черновик ${c.draft} не называет ${c.message}.{${c.missing.join(", ")}}`),
      ),
    ].sort();
    expect(lines).toEqual([]);
  });
});
