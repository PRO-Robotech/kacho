// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import ts from "typescript";

/**
 * Перепись МЕСТ ВЫПУСКА обращений консоли к краю — по узлам разбора, а не по
 * тексту (приёмка F8, Р10, §11.1, N18).
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ЗАЧЕМ
 *
 * Упорядочение вокруг глагола, ставящего носитель, держит модульная проба — но
 * она судит упорядочивающий транспорт, а не то, что через него идёт КАЖДОЕ
 * обращение вкладки. Место выпуска мимо транспорта глагол не видит: его чтение
 * в полёте не отменяется, и ответ края на прежний носитель гасит новый. Мест
 * выпуска до упорядочения было девять в семи файлах, и один браузерный путь
 * этого не держит.
 *
 * ЧТО СЧИТАЕТСЯ МЕСТОМ ВЫПУСКА
 *
 *   • ЧТЕНИЕ транспорта браузера — `fetch`, `sendBeacon`, `EventSource`,
 *     `XMLHttpRequest`, `WebSocket`, — а не только его вызов. Получивший
 *     транспорт выпускает обращение мимо упорядочения тем же способом, что и
 *     вызвавший его на месте: ссылка в переменной, привязка (`.bind`), передача
 *     аргументом, сокращённое свойство, доступ по строке и по постоянной,
 *     деструктуризация, построение и наследование конструктора. Глобальный
 *     объект — `window`, `globalThis`, `self`, их члены-окна (`top`, `parent`,
 *     `frames`, `opener`, `defaultView`) и ПСЕВДОНИМЫ, заведённые в том же файле;
 *   • доступ к глобальному объекту по ключу, которого разбор не знает, и
 *     глобальный объект, переданный значением (аргументом, в поле, в возврат):
 *     что с ним сделают, распознаватель не видит, а неизвестное — находка, а не
 *     прощённое;
 *   • локальное имя, совпадающее с именем транспорта: обращение по нему судилось
 *     бы по области видимости, которой разбор файла не строит, — такое имя
 *     переименовывают;
 *   • переход документа на путь края: `location.assign/replace(…)`,
 *     `window.open(…)`, присвоение `location`/`location.href` — в том числе по
 *     строке и через псевдоним, — чей адрес литерал или голова шаблона с путём края.
 *
 * НЕ находка — то, что транспорт не читает: типовое упоминание (`: EventSource`,
 * `typeof fetch` в типе), проверка наличия (`typeof fetch === "function"`,
 * `"fetch" in window`), ключ объекта и член НЕ глобального объекта
 * (`orderedTransport.fetch` — это и есть законный путь), доступ к глобальному
 * объекту по известному ключу, который не транспорт (ключ-символ состояния
 * вкладки).
 *
 * Место внутри упорядочивающего транспорта находкой не является — это и есть
 * транспорт, и перепись обязана его ВИДЕТЬ (иначе «ноль находок» не отличим от
 * «распознаватель слеп»). Всё прочее — находка, кроме названного поимённо с
 * основанием; исключение, которому нечего исключать, — само находка.
 * Комментарий находкой не является: судится узел.
 */

/** Упорядочивающий транспорт — единственный законный дом вызова транспорта. */
export const ORDERING_TRANSPORT = "shared/src/api/carrier-order.ts";

/** Путь края: API доменов (`/<домен>/v<N>/…`), операции, проверка живости края. */
const EDGE_PATH = /^\/(?:[a-z][a-z0-9-]*\/v\d+(?:[/?]|$)|operations(?:[/?]|$)|healthz$)/;

/** Адрес — путь края, в том числе с происхождением перед ним. */
export function isEdgePathText(text: string): boolean {
  return EDGE_PATH.test(text.replace(/^[a-z][a-z0-9+.-]*:\/\/[^/]+/i, ""));
}

/** Транспорт, живущий на глобальном объекте и под голым именем. */
const GLOBAL_TRANSPORT = new Set(["fetch", "EventSource", "XMLHttpRequest", "WebSocket"]);
/** `sendBeacon` живёт на `navigator`; имя однозначно на любом объекте. */
const BEACON = "sendBeacon";
/** Голое имя, чьё чтение — чтение транспорта. */
const BARE_TRANSPORT = new Set([...GLOBAL_TRANSPORT, BEACON]);
/** Глобальный объект под своим именем. */
const GLOBALS = new Set(["window", "globalThis", "self"]);
/**
 * Окно под именем, которое в коде консоли чаще бывает ЛОКАЛЬНЫМ (`parent` узла,
 * `top` прямоугольника): судится только ключом транспорта, а не как глобальный
 * объект, переданный значением, — иначе каждое дерево стало бы находкой.
 */
const WEAK_GLOBALS = new Set(["top", "parent", "frames", "opener"]);
/** Член глобального объекта, который сам — окно. */
const WINDOW_MEMBERS = new Set(["window", "self", "globalThis", "top", "parent", "frames", "opener"]);

export interface IssuanceFinding {
  file: string;
  line: number;
  what: string;
}

export interface IssuanceExcuse {
  file: string;
  what: RegExp;
  reason: string;
}

export interface IssuanceCensus {
  filesRead: number;
  /** Места выпуска внутри упорядочивающего транспорта. */
  transport: IssuanceFinding[];
  findings: IssuanceFinding[];
  excused: Array<IssuanceFinding & { reason: string }>;
  staleExcuses: string[];
}

function lineOf(source: ts.SourceFile, node: ts.Node): number {
  return source.getLineAndCharacterOfPosition(node.getStart(source)).line + 1;
}

/** Статическая голова адреса: литерал целиком либо голова шаблона. */
function addressHead(node: ts.Expression | undefined): string | null {
  if (!node) return null;
  if (ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node)) return node.text;
  if (ts.isTemplateExpression(node)) return node.head.text;
  return null;
}

/** Ключ члена: известное имя, символ либо то, чего разбор не знает. */
type Key = { kind: "name"; text: string } | { kind: "symbol" } | { kind: "unknown" };
const UNKNOWN: Key = { kind: "unknown" };
const SYMBOL: Key = { kind: "symbol" };

/** Что известно о файле до суда: постоянные-ключи и псевдонимы. */
interface FileScope {
  /** `const k = "fetch"` · `const KEY = Symbol.for(…)` — имя связано в файле ровно один раз. */
  consts: Map<string, Key>;
  /** Псевдонимы глобального объекта: `const g = globalThis as …`. */
  globals: Set<string>;
  /** Псевдонимы `location`: `const loc = window.location`. */
  locations: Set<string>;
}

/** Обёртки, не меняющие значения: скобки, приведение типа, `!`, `satisfies`. */
function isWrapper(node: ts.Node): node is ts.ParenthesizedExpression | ts.AsExpression | ts.NonNullExpression | ts.SatisfiesExpression | ts.TypeAssertion {
  return (
    ts.isParenthesizedExpression(node) ||
    ts.isAsExpression(node) ||
    ts.isNonNullExpression(node) ||
    ts.isSatisfiesExpression(node) ||
    ts.isTypeAssertionExpression(node)
  );
}

function unwrap(node: ts.Expression): ts.Expression {
  let e = node;
  while (isWrapper(e)) e = e.expression;
  return e;
}

/** Кому значение отдано — с пропуском обёрток. */
function consumerOf(node: ts.Node): { parent: ts.Node; child: ts.Node } {
  let child = node;
  while (child.parent && isWrapper(child.parent)) child = child.parent;
  return { parent: child.parent, child };
}

/** Как значение транспорта употреблено: вызвано, построено или взято ссылкой. */
function usageOf(node: ts.Node): string {
  const { parent, child } = consumerOf(node);
  if (ts.isCallExpression(parent) && parent.expression === child) return "вызов транспорта";
  if (ts.isNewExpression(parent) && parent.expression === child) return "построение";
  return "ссылка на транспорт";
}

function labelOf(node: ts.Expression): string {
  return unwrap(node).getText().replace(/\s+/g, "");
}

/** `Symbol(…)`, `Symbol.for(…)`, `Symbol.iterator` — ключ-символ, не имя. */
function isSymbolValue(node: ts.Expression): boolean {
  const e = unwrap(node);
  if (ts.isCallExpression(e)) {
    const c = unwrap(e.expression);
    return (
      (ts.isIdentifier(c) && c.text === "Symbol") ||
      (ts.isPropertyAccessExpression(c) && ts.isIdentifier(c.expression) && c.expression.text === "Symbol" && c.name.text === "for")
    );
  }
  return ts.isPropertyAccessExpression(e) && ts.isIdentifier(e.expression) && e.expression.text === "Symbol";
}

function keyOfExpression(node: ts.Expression, scope: FileScope): Key {
  const e = unwrap(node);
  if (ts.isStringLiteral(e) || ts.isNoSubstitutionTemplateLiteral(e) || ts.isNumericLiteral(e)) return { kind: "name", text: e.text };
  if (ts.isIdentifier(e)) return scope.consts.get(e.text) ?? UNKNOWN;
  return isSymbolValue(e) ? SYMBOL : UNKNOWN;
}

function memberKey(node: ts.PropertyAccessExpression | ts.ElementAccessExpression, scope: FileScope): Key {
  if (ts.isPropertyAccessExpression(node)) return { kind: "name", text: node.name.text };
  return keyOfExpression(node.argumentExpression, scope);
}

function propertyNameKey(name: ts.PropertyName, scope: FileScope): Key {
  if (ts.isComputedPropertyName(name)) return keyOfExpression(name.expression, scope);
  if (ts.isIdentifier(name) || ts.isPrivateIdentifier(name) || ts.isStringLiteral(name) || ts.isNumericLiteral(name) || ts.isNoSubstitutionTemplateLiteral(name)) {
    return { kind: "name", text: name.text };
  }
  return UNKNOWN;
}

function isMember(node: ts.Node): node is ts.PropertyAccessExpression | ts.ElementAccessExpression {
  return ts.isPropertyAccessExpression(node) || ts.isElementAccessExpression(node);
}

/**
 * Глобальный объект: `window`, `globalThis`, `self`, псевдоним файла, окно-член
 * (`window.top`, `window.frames[0]`), окно документа (`….defaultView`).
 * `weakToo` — принимать ли имена окна, которые чаще бывают локальными.
 */
function isGlobalObject(node: ts.Expression, scope: FileScope, weakToo: boolean): boolean {
  const e = unwrap(node);
  if (ts.isIdentifier(e)) return GLOBALS.has(e.text) || scope.globals.has(e.text) || (weakToo && WEAK_GLOBALS.has(e.text));
  if (!isMember(e)) return false;
  const key = memberKey(e, scope);
  if (key.kind !== "name") return false;
  if (key.text === "defaultView") return true;
  return (WINDOW_MEMBERS.has(key.text) || /^\d+$/.test(key.text)) && isGlobalObject(e.expression, scope, weakToo);
}

/** `location`, `window.location`, `document["location"]`, псевдоним `location`. */
function isLocation(node: ts.Expression, scope: FileScope): boolean {
  const e = unwrap(node);
  if (ts.isIdentifier(e)) return e.text === "location" || scope.locations.has(e.text);
  if (!isMember(e)) return false;
  const key = memberKey(e, scope);
  return key.kind === "name" && key.text === "location";
}

/** Имя, которое объявление СВЯЗЫВАЕТ в области видимости. */
function isDeclarationName(id: ts.Identifier): boolean {
  const p = id.parent;
  return (
    (ts.isVariableDeclaration(p) ||
      ts.isParameter(p) ||
      ts.isBindingElement(p) ||
      ts.isFunctionDeclaration(p) ||
      ts.isFunctionExpression(p) ||
      ts.isClassDeclaration(p) ||
      ts.isClassExpression(p) ||
      ts.isImportClause(p) ||
      ts.isImportSpecifier(p) ||
      ts.isNamespaceImport(p) ||
      ts.isImportEqualsDeclaration(p)) &&
    p.name === id
  );
}

/** Имя стоит КЛЮЧОМ (члена, свойства, разбора) либо меткой, а не значением. */
function isKeyPosition(id: ts.Identifier): boolean {
  const p = id.parent;
  if (ts.isPropertyAccessExpression(p)) return p.name === id;
  if (ts.isQualifiedName(p)) return p.right === id;
  if (
    (ts.isPropertyAssignment(p) ||
      ts.isMethodDeclaration(p) ||
      ts.isPropertyDeclaration(p) ||
      ts.isGetAccessorDeclaration(p) ||
      ts.isSetAccessorDeclaration(p) ||
      ts.isPropertySignature(p) ||
      ts.isMethodSignature(p) ||
      ts.isEnumMember(p) ||
      ts.isJsxAttribute(p)) &&
    p.name === id
  ) {
    return true;
  }
  // `{ fetch }` в разборе объекта — и ключ, и локальное имя: судит правило разбора.
  if (ts.isBindingElement(p)) return p.propertyName === id || (!p.propertyName && ts.isObjectBindingPattern(p.parent));
  if (ts.isLabeledStatement(p) || ts.isBreakOrContinueStatement(p)) return p.label === id;
  return ts.isMetaProperty(p);
}

/** Типовая позиция: транспорт в ней не читается. Наследование класса — значение. */
function isTypePosition(node: ts.Node): boolean {
  if (ts.isExpressionWithTypeArguments(node)) {
    const clause = node.parent;
    const classExtends =
      ts.isHeritageClause(clause) &&
      clause.token === ts.SyntaxKind.ExtendsKeyword &&
      (ts.isClassDeclaration(clause.parent) || ts.isClassExpression(clause.parent));
    return !classExtends;
  }
  return (
    ts.isTypeNode(node) ||
    ts.isInterfaceDeclaration(node) ||
    ts.isTypeAliasDeclaration(node) ||
    (ts.isImportDeclaration(node) && !!node.importClause?.isTypeOnly) ||
    (ts.isImportSpecifier(node) && node.isTypeOnly)
  );
}

/** Постоянные-ключи и псевдонимы файла; псевдонимы — до неподвижной точки. */
function scopeOf(source: ts.SourceFile): FileScope {
  const scope: FileScope = { consts: new Map(), globals: new Set(), locations: new Set() };
  const bindings = new Map<string, number>();
  const literal = new Map<string, Key>();
  const values: Array<{ name: string; value: ts.Expression }> = [];
  const collect = (node: ts.Node) => {
    if (ts.isIdentifier(node) && isDeclarationName(node)) bindings.set(node.text, (bindings.get(node.text) ?? 0) + 1);
    if ((ts.isVariableDeclaration(node) || ts.isParameter(node)) && ts.isIdentifier(node.name) && node.initializer) {
      values.push({ name: node.name.text, value: node.initializer });
      const constant = ts.isVariableDeclaration(node) && ts.isVariableDeclarationList(node.parent) && (node.parent.flags & ts.NodeFlags.Const) !== 0;
      const v = unwrap(node.initializer);
      if (constant && (ts.isStringLiteral(v) || ts.isNoSubstitutionTemplateLiteral(v))) literal.set(node.name.text, { kind: "name", text: v.text });
      else if (constant && isSymbolValue(v)) literal.set(node.name.text, SYMBOL);
    }
    if (ts.isBinaryExpression(node) && node.operatorToken.kind === ts.SyntaxKind.EqualsToken && ts.isIdentifier(node.left)) {
      values.push({ name: node.left.text, value: node.right });
    }
    ts.forEachChild(node, collect);
  };
  collect(source);
  // Имя, связанное в файле больше одного раза, в разных областях значит разное:
  // ключом-постоянной оно не считается, и доступ по нему — неизвестный.
  for (const [name, key] of literal) if (bindings.get(name) === 1) scope.consts.set(name, key);
  for (let changed = true; changed; ) {
    changed = false;
    for (const { name, value } of values) {
      if (!scope.globals.has(name) && isGlobalObject(value, scope, false)) {
        scope.globals.add(name);
        changed = true;
      }
      if (!scope.locations.has(name) && isLocation(value, scope)) {
        scope.locations.add(name);
        changed = true;
      }
    }
  }
  return scope;
}

/** Глобальный объект отдан туда, где разбор его не видит. */
function passesOn(id: ts.Identifier): boolean {
  const { parent: p, child } = consumerOf(id);
  if (isMember(p) && p.expression === child) return false;
  if (ts.isTypeOfExpression(p)) return false;
  // Псевдоним (`const g = globalThis`) и разбор (`const { fetch } = window`) судятся своими правилами.
  if ((ts.isVariableDeclaration(p) || ts.isParameter(p)) && p.initializer === child) return false;
  if (ts.isBinaryExpression(p)) {
    const op = p.operatorToken.kind;
    if (op === ts.SyntaxKind.EqualsToken) return !(p.right === child && ts.isIdentifier(p.left));
    if (op === ts.SyntaxKind.InKeyword) return p.right !== child;
    return !(
      op === ts.SyntaxKind.EqualsEqualsEqualsToken ||
      op === ts.SyntaxKind.ExclamationEqualsEqualsToken ||
      op === ts.SyntaxKind.EqualsEqualsToken ||
      op === ts.SyntaxKind.ExclamationEqualsToken
    );
  }
  return true;
}

/** Метка глобального объекта, из которого разбирается образец; `null` — не глобальный. */
function patternGlobal(pattern: ts.ObjectBindingPattern, scope: FileScope, weakToo: boolean): string | null {
  const holder = pattern.parent;
  if ((ts.isVariableDeclaration(holder) || ts.isParameter(holder)) && holder.initializer) {
    return isGlobalObject(holder.initializer, scope, weakToo) ? labelOf(holder.initializer) : null;
  }
  if (ts.isBindingElement(holder) && ts.isObjectBindingPattern(holder.parent) && holder.propertyName) {
    const key = propertyNameKey(holder.propertyName, scope);
    const outer = patternGlobal(holder.parent, scope, weakToo);
    const windowKey = key.kind === "name" && (WINDOW_MEMBERS.has(key.text) || key.text === "defaultView");
    return outer !== null && windowKey && key.kind === "name" ? `${outer}.${key.text}` : null;
  }
  return null;
}

/** Места выпуска ОДНОГО источника. */
export function issuancesIn(file: string, text: string): IssuanceFinding[] {
  const kind = /\.tsx$/.test(file) ? ts.ScriptKind.TSX : ts.ScriptKind.TS;
  const source = ts.createSourceFile(file, text, ts.ScriptTarget.Latest, true, kind);
  const scope = scopeOf(source);
  const found: IssuanceFinding[] = [];
  const push = (node: ts.Node, what: string) => found.push({ file, line: lineOf(source, node), what });
  const passedOn = (label: string) => `глобальный объект ${label} передан значением — что с ним сделают, распознавателю не видно`;
  const computed = (label: string) => `доступ к глобальному объекту ${label} по вычисленному ключу`;

  const visit = (node: ts.Node) => {
    if (isTypePosition(node)) return;

    if (ts.isIdentifier(node) && !isKeyPosition(node) && !ts.isTypeOfExpression(consumerOf(node).parent)) {
      if (BARE_TRANSPORT.has(node.text)) {
        push(node, isDeclarationName(node) ? `локальное имя ${node.text} заслоняет транспорт браузера` : `${usageOf(node)} ${node.text}`);
      } else if ((GLOBALS.has(node.text) || scope.globals.has(node.text)) && !isDeclarationName(node) && passesOn(node)) {
        push(node, passedOn(node.text));
      }
    }

    if (isMember(node) && !ts.isTypeOfExpression(consumerOf(node).parent)) {
      const key = memberKey(node, scope);
      if (key.kind === "name" && key.text === BEACON) push(node, `${usageOf(node)} ${BEACON}`);
      else if (key.kind === "name" && GLOBAL_TRANSPORT.has(key.text) && isGlobalObject(node.expression, scope, true)) {
        push(node, `${usageOf(node)} ${labelOf(node.expression)}.${key.text}`);
      } else if (key.kind === "unknown" && isGlobalObject(node.expression, scope, false)) {
        push(node, computed(labelOf(node.expression)));
      }
    }

    if (ts.isBindingElement(node) && ts.isObjectBindingPattern(node.parent)) {
      const strong = patternGlobal(node.parent, scope, false);
      const any = patternGlobal(node.parent, scope, true);
      if (node.dotDotDotToken) {
        if (strong !== null) push(node, passedOn(strong));
      } else {
        const key = node.propertyName
          ? propertyNameKey(node.propertyName, scope)
          : ts.isIdentifier(node.name)
            ? ({ kind: "name", text: node.name.text } as const)
            : UNKNOWN;
        if (key.kind === "name" && BARE_TRANSPORT.has(key.text)) {
          if (any !== null) push(node, `ссылка на транспорт ${any}.${key.text}`);
          else if (key.text === BEACON) push(node, `ссылка на транспорт ${BEACON}`);
          else if (!node.propertyName) push(node, `локальное имя ${key.text} заслоняет транспорт браузера`);
        } else if (key.kind === "unknown" && strong !== null) {
          push(node, computed(strong));
        }
      }
    }

    if (ts.isCallExpression(node)) {
      const callee = unwrap(node.expression);
      if (isMember(callee)) {
        const key = memberKey(callee, scope);
        const name = key.kind === "name" ? key.text : "";
        const navigates =
          ((name === "assign" || name === "replace") && isLocation(callee.expression, scope)) ||
          (name === "open" && isGlobalObject(callee.expression, scope, true));
        const head = addressHead(node.arguments[0]);
        if (navigates && head !== null && isEdgePathText(head)) push(node, `переход документа на путь края «${head}»`);
      }
    }

    if (ts.isBinaryExpression(node) && node.operatorToken.kind === ts.SyntaxKind.EqualsToken) {
      const target = unwrap(node.left);
      // Присвоение ПСЕВДОНИМУ `location` перевязывает имя, а не уводит документ.
      const assignsLocation = ts.isIdentifier(target)
        ? target.text === "location"
        : isMember(target) &&
          (isLocation(target, scope) ||
            (((k) => k.kind === "name" && k.text === "href")(memberKey(target, scope)) && isLocation(target.expression, scope)));
      const head = addressHead(node.right);
      if (assignsLocation && head !== null && isEdgePathText(head)) push(node, `переход документа на путь края «${head}»`);
    }

    ts.forEachChild(node, visit);
  };
  visit(source);
  return found;
}

/** Перепись набора источников. Пустой набор — отказ: вердикта нет. */
export function issuanceCensusOf(
  sources: ReadonlyArray<{ file: string; text: string }>,
  excuses: readonly IssuanceExcuse[],
): IssuanceCensus {
  if (sources.length === 0) throw new Error("перепись мест выпуска прочитала 0 файлов — вердикта нет");
  const census: IssuanceCensus = { filesRead: sources.length, transport: [], findings: [], excused: [], staleExcuses: [] };
  const used = new Set<IssuanceExcuse>();
  for (const { file, text } of sources) {
    for (const f of issuancesIn(file, text)) {
      if (file === ORDERING_TRANSPORT) {
        census.transport.push(f);
        continue;
      }
      const excuse = excuses.find((e) => e.file === f.file && e.what.test(f.what));
      if (excuse) {
        used.add(excuse);
        census.excused.push({ ...f, reason: excuse.reason });
      } else {
        census.findings.push(f);
      }
    }
  }
  census.staleExcuses = excuses.filter((e) => !used.has(e)).map((e) => `${e.file} · ${e.what.source} — ${e.reason}`);
  return census;
}

/** Строка находки с координатой. */
export function formatIssuance(f: IssuanceFinding): string {
  return `${f.file}:${f.line} ${f.what}`;
}
