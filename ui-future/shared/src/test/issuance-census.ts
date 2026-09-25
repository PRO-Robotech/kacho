// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import path from "node:path";
import ts from "typescript";

/**
 * Перепись МЕСТ ВЫПУСКА обращений консоли к краю — по узлам разбора, а не по
 * тексту (приёмка F8, Р10, §11.1, N18). БЫСТРАЯ ПОДСКАЗКА, А НЕ ДЕРЖАТЕЛЬ ПОЛНОТЫ.
 *
 * ─────────────────────────────────────────────────────────────────────────────
 * ЗАЧЕМ И ЧЕГО ОНА НЕ ОБЕЩАЕТ
 *
 * Упорядочение вокруг глагола, ставящего носитель, держит модульная проба — но
 * она судит упорядочивающий транспорт, а не то, что через него идёт КАЖДОЕ
 * обращение вкладки. Место выпуска мимо транспорта глагол не видит: его чтение
 * в полёте не отменяется, и ответ края на прежний носитель гасит новый. Мест
 * выпуска до упорядочения было девять в семи файлах (N18).
 *
 * Держатель того, что мимо транспорта не выпускается НИЧЕГО, — ИСПОЛНЕНИЕ, а не
 * эта перепись: страж `issuance-guard.ts` судит каждый вызов `fetch` окна в
 * пробах jest и в браузере сквозных проб по тому, выпустил ли его транспорт, —
 * какой бы формой записи код до `fetch` ни добрался. Прод-код, до которого
 * исполнение не дошло, судит правило линта (`issuance-ordering.eslint.config.js`).
 *
 * Распознавание текста полным не бывает: четыре круга проверки подряд находили
 * законную форму, которой распознаватель не знал (ссылка, привязка, доступ по
 * строке, окно фрейма, результат `open()`, `UIEvent.view`, `MessageEvent.source`),
 * и каждая дописка рождала следующую. Поэтому перепись ОСТАЁТСЯ тем, чем её
 * назвала приёмка (§11.1, N18), — переписью мест выпуска с числом и координатой,
 * быстрой и не требующей исполнения, — и НЕ притязает на полноту. Форма, которой
 * она не узнаёт, — предмет стража исполнения; дописывать распознаватель под
 * каждую новую форму не требуется, а «вне упорядочения 0» здесь значит «среди
 * узнанных форм — ноль», а не «мимо транспорта — ничего».
 *
 * ЧТО ПЕРЕПИСЬ УЗНАЁТ — перечень форм, а не определение места выпуска
 *
 *   • ЧТЕНИЕ транспорта браузера — `fetch`, `sendBeacon`, `EventSource`,
 *     `XMLHttpRequest`, `WebSocket`, — а не только его вызов. Получивший
 *     транспорт выпускает обращение мимо упорядочения тем же способом, что и
 *     вызвавший его на месте: ссылка в переменной, привязка (`.bind`), передача
 *     аргументом, сокращённое свойство, доступ по строке и по постоянной,
 *     деструктуризация, построение и наследование конструктора. Глобальный
 *     объект — `window`, `globalThis`, `self`, их члены-окна (`top`, `parent`,
 *     `frames`, `opener`), окно документа (`defaultView`), окно, которое ОТДАЁТ
 *     ЗНАЧЕНИЕМ API браузера (`contentWindow` фрейма и объекта, результат
 *     `window.open()`, голого `open()` и `document.open(url, имя, свойства)`,
 *     `UIEvent.view`, `MessageEvent.source`), и ПСЕВДОНИМЫ, заведённые в том же
 *     файле — присвоением и разбором;
 *   • ключ транспорта на получателе, которого разбор не опознал, — член, разбор,
 *     отражение (`Reflect.get(x, "fetch")`): окно отдаёт не только названный
 *     API (`event.currentTarget as Window`, возврат функции), и чьё это окно,
 *     разбору файла не видно. Законный получатель один — упорядочивающий
 *     транспорт, узнанный по ИМПОРТУ из своего модуля, а не по имени;
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
 * `"fetch" in window`), ключ объекта, ключ транспорта на упорядочивающем
 * транспорте (`orderedTransport.fetch` — это и есть законный путь), доступ к
 * глобальному объекту по известному ключу, который не транспорт (ключ-символ
 * состояния вкладки), результат `open()`, который не взят.
 *
 * Имена окна, которые в коде консоли чаще бывают ЛОКАЛЬНЫМИ (`top`, `parent`,
 * `frames`, `opener`, член `view`, член `source`), судятся ключом транспорта и
 * разбором, но не вычисленным ключом и не передачей значением: иначе каждое
 * дерево и каждый поток стали бы находкой.
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
/** Член ЛЮБОГО объекта, который API браузера отдаёт окном: окно документа, окно фрейма и объекта. */
const WINDOW_OF_ANY = new Set(["defaultView", "contentWindow"]);
/** Член, который окном бывает (`UIEvent.view`, `MessageEvent.source`), но чаще — локальное поле. */
const WEAK_WINDOW_OF_ANY = new Set(["view", "source"]);
/** Член, который сам — документ: `document.open(url, имя, свойства)` открывает окно. */
const DOCUMENT_MEMBERS = new Set(["document", "ownerDocument", "contentDocument"]);
/** Упорядочивающий транспорт — экспорт своего модуля; модуль узнаётся путём, а не именем. */
const ORDERED_EXPORT = "orderedTransport";
const ORDERING_MODULE = ORDERING_TRANSPORT.replace(/\.ts$/, "");
/** Отражение, читающее либо пишущее член по ключу — вторым аргументом. */
const REFLECTION = new Map([
  ["Reflect", new Set(["get", "set", "getOwnPropertyDescriptor", "defineProperty", "deleteProperty"])],
  ["Object", new Set(["getOwnPropertyDescriptor", "defineProperty"])],
]);

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
  /** Псевдонимы глобального объекта: `const g = globalThis as …`, `const w = frame.contentWindow`. */
  globals: Set<string>;
  /** Псевдонимы окна под именем, которое чаще бывает локальным: `const view = event.view`. */
  weakGlobals: Set<string>;
  /** Псевдонимы `location`: `const loc = window.location`. */
  locations: Set<string>;
  /** Псевдонимы `open` окна: `const o = window.open`. */
  openers: Set<string>;
  /** Имена упорядочивающего транспорта: импорт из его модуля и псевдоним. */
  ordered: Set<string>;
  /** Пространства имён модуля упорядочения: `import * as co from ".../carrier-order"`. */
  orderedNamespaces: Set<string>;
  /** Объявления каждого имени файла — для суда, заслонено ли глобальное имя. */
  declarations: Map<string, ts.Identifier[]>;
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

/** Значение выражения: без обёрток и без левой части запятой (`(0, window.open)`). */
function unwrap(node: ts.Expression): ts.Expression {
  let e = node;
  for (;;) {
    if (isWrapper(e)) e = e.expression;
    else if (ts.isBinaryExpression(e) && e.operatorToken.kind === ts.SyntaxKind.CommaToken) e = e.right;
    else return e;
  }
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
 * (`window.top`, `window.frames[0]`), окно, которое отдаёт API браузера
 * (`….defaultView`, `….contentWindow`, результат `open()`).
 * `weakToo` — принимать ли имена окна, которые чаще бывают локальными.
 */
function isGlobalObject(node: ts.Expression, scope: FileScope, weakToo: boolean): boolean {
  const e = unwrap(node);
  if (ts.isIdentifier(e)) {
    return (
      (GLOBALS.has(e.text) && !isShadowed(e, scope)) ||
      scope.globals.has(e.text) ||
      (weakToo && (WEAK_GLOBALS.has(e.text) || scope.weakGlobals.has(e.text)))
    );
  }
  if (ts.isCallExpression(e)) return opensWindow(e, scope);
  if (!isMember(e)) return false;
  const key = memberKey(e, scope);
  if (key.kind !== "name") return false;
  if (WINDOW_OF_ANY.has(key.text)) return true;
  if (weakToo && WEAK_WINDOW_OF_ANY.has(key.text)) return true;
  return (WINDOW_MEMBERS.has(key.text) || /^\d+$/.test(key.text)) && isGlobalObject(e.expression, scope, weakToo);
}

/** Документ: `document`, `….document`, `….ownerDocument`, `….contentDocument`. */
function isDocument(node: ts.Expression, scope: FileScope): boolean {
  const e = unwrap(node);
  if (ts.isIdentifier(e)) return e.text === "document" && !isShadowed(e, scope);
  if (!isMember(e)) return false;
  const key = memberKey(e, scope);
  return key.kind === "name" && DOCUMENT_MEMBERS.has(key.text);
}

/** `open` окна как значение: `window.open`, голое `open`, псевдоним, привязка. */
function isOpener(node: ts.Expression, scope: FileScope): boolean {
  const e = unwrap(node);
  if (ts.isIdentifier(e)) return (e.text === "open" && !isShadowed(e, scope)) || scope.openers.has(e.text);
  if (ts.isCallExpression(e)) {
    const c = unwrap(e.expression);
    return isMember(c) && ((k) => k.kind === "name" && k.text === "bind")(memberKey(c, scope)) && isOpener(c.expression, scope);
  }
  if (!isMember(e)) return false;
  const key = memberKey(e, scope);
  return key.kind === "name" && key.text === "open" && isGlobalObject(e.expression, scope, false);
}

/**
 * Вызов, который ОТКРЫВАЕТ окно и отдаёт его значением: `window.open(…)`, голое
 * `open(…)`, `….open.call/apply(…)`, псевдоним, `document.open(url, имя, свойства)`.
 */
function opensWindow(call: ts.CallExpression, scope: FileScope): boolean {
  const callee = unwrap(call.expression);
  if (isOpener(callee, scope)) return true;
  if (!isMember(callee)) return false;
  const key = memberKey(callee, scope);
  if (key.kind !== "name") return false;
  if ((key.text === "call" || key.text === "apply") && isOpener(callee.expression, scope)) return true;
  return key.text === "open" && call.arguments.length === 3 && isDocument(callee.expression, scope);
}

/** Упорядочивающий транспорт: имя, импортированное из его модуля, псевдоним, член пространства имён. */
function isOrderedTransport(node: ts.Expression, scope: FileScope): boolean {
  const e = unwrap(node);
  if (ts.isIdentifier(e)) return scope.ordered.has(e.text);
  if (!isMember(e)) return false;
  const key = memberKey(e, scope);
  const holder = unwrap(e.expression);
  return key.kind === "name" && key.text === ORDERED_EXPORT && ts.isIdentifier(holder) && scope.orderedNamespaces.has(holder.text);
}

/** Модуль, который импорт называет, — упорядочивающий транспорт (путь относительно корня консоли). */
function namesOrderingModule(file: string, specifier: string): boolean {
  const resolved = specifier.startsWith("@shared/")
    ? `shared/src/${specifier.slice("@shared/".length)}`
    : specifier.startsWith(".")
      ? path.posix.join(path.posix.dirname(file), specifier)
      : specifier;
  return resolved.replace(/\.(?:ts|tsx|js)$/, "") === ORDERING_MODULE;
}

/** Область видимости, в которой объявление связывает имя. */
function bindingScopeOf(id: ts.Identifier): ts.Node {
  let decl: ts.Node = id.parent;
  while (ts.isBindingElement(decl) || ts.isObjectBindingPattern(decl) || ts.isArrayBindingPattern(decl)) decl = decl.parent;
  if (ts.isParameter(decl)) return decl.parent;
  if (ts.isFunctionExpression(decl) || ts.isClassExpression(decl)) return decl;
  if (ts.isVariableDeclaration(decl)) {
    if (ts.isCatchClause(decl.parent)) return decl.parent;
    const list = decl.parent;
    const blockScoped = ts.isVariableDeclarationList(list) && (list.flags & ts.NodeFlags.BlockScoped) !== 0;
    for (let n: ts.Node = list; n.parent; n = n.parent) {
      if (blockScoped && (ts.isBlock(n.parent) || ts.isCaseBlock(n.parent) || ts.isModuleBlock(n.parent) || ts.isIterationStatement(n.parent, false))) {
        return n.parent;
      }
      if (ts.isFunctionLike(n.parent) || ts.isSourceFile(n.parent)) return n.parent;
    }
  }
  for (let n: ts.Node = decl; n.parent; n = n.parent) {
    if (ts.isBlock(n.parent) || ts.isModuleBlock(n.parent) || ts.isSourceFile(n.parent) || ts.isCaseBlock(n.parent)) return n.parent;
  }
  return id.getSourceFile();
}

/** Имя глобального объекта заслонено объявлением файла в области, где оно употреблено. */
function isShadowed(id: ts.Identifier, scope: FileScope): boolean {
  const declared = scope.declarations.get(id.text);
  if (!declared) return false;
  return declared.some((d) => {
    const area = bindingScopeOf(d);
    return id.pos >= area.pos && id.end <= area.end;
  });
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
function scopeOf(source: ts.SourceFile, file: string): FileScope {
  const scope: FileScope = {
    consts: new Map(),
    globals: new Set(),
    weakGlobals: new Set(),
    locations: new Set(),
    openers: new Set(),
    ordered: new Set(),
    orderedNamespaces: new Set(),
    declarations: new Map(),
  };
  const bindings = new Map<string, number>();
  const literal = new Map<string, Key>();
  const values: Array<{ name: string; value: ts.Expression }> = [];
  /** Имя, связанное разбором: `const { contentWindow: w } = frame`. */
  const destructured: Array<{ name: string; element: ts.BindingElement }> = [];
  const imported: Array<{ name: string; namespace: boolean }> = [];
  const collect = (node: ts.Node) => {
    if (ts.isIdentifier(node) && isDeclarationName(node)) {
      bindings.set(node.text, (bindings.get(node.text) ?? 0) + 1);
      scope.declarations.set(node.text, [...(scope.declarations.get(node.text) ?? []), node]);
    }
    if (ts.isBindingElement(node) && ts.isObjectBindingPattern(node.parent) && ts.isIdentifier(node.name)) {
      destructured.push({ name: node.name.text, element: node });
    }
    if (ts.isImportDeclaration(node) && ts.isStringLiteral(node.moduleSpecifier) && namesOrderingModule(file, node.moduleSpecifier.text)) {
      const clause = node.importClause;
      const named = clause?.namedBindings;
      if (named && ts.isNamespaceImport(named)) imported.push({ name: named.name.text, namespace: true });
      if (named && ts.isNamedImports(named)) {
        for (const spec of named.elements) {
          if ((spec.propertyName ?? spec.name).text === ORDERED_EXPORT && !spec.isTypeOnly) imported.push({ name: spec.name.text, namespace: false });
        }
      }
    }
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
  // Упорядочивающий транспорт — только имя, связанное в файле ровно один раз:
  // заслонённое локальным объявлением значит другое.
  for (const { name, namespace } of imported) if (bindings.get(name) === 1) (namespace ? scope.orderedNamespaces : scope.ordered).add(name);
  const grow = (set: Set<string>, name: string, yes: boolean): boolean => {
    if (!yes || set.has(name)) return false;
    set.add(name);
    return true;
  };
  for (let changed = true; changed; ) {
    changed = false;
    for (const { name, value } of values) {
      if (grow(scope.globals, name, isGlobalObject(value, scope, false))) changed = true;
      if (grow(scope.weakGlobals, name, isGlobalObject(value, scope, true))) changed = true;
      if (grow(scope.locations, name, isLocation(value, scope))) changed = true;
      if (grow(scope.openers, name, isOpener(value, scope))) changed = true;
      if (grow(scope.ordered, name, bindings.get(name) === 1 && isOrderedTransport(value, scope))) changed = true;
    }
    for (const { name, element } of destructured) {
      if (grow(scope.globals, name, windowByPattern(element, scope, false))) changed = true;
      if (grow(scope.weakGlobals, name, windowByPattern(element, scope, true))) changed = true;
    }
  }
  return scope;
}

/** Разбор отдаёт окно: `{ contentWindow: w } = frame`, `{ top } = window`, `{ view } = event`. */
function windowByPattern(element: ts.BindingElement, scope: FileScope, weakToo: boolean): boolean {
  if (element.dotDotDotToken) return false;
  const key = element.propertyName
    ? propertyNameKey(element.propertyName, scope)
    : ts.isIdentifier(element.name)
      ? ({ kind: "name", text: element.name.text } as const)
      : UNKNOWN;
  if (key.kind !== "name") return false;
  if (WINDOW_OF_ANY.has(key.text) || (weakToo && WEAK_WINDOW_OF_ANY.has(key.text))) return true;
  return (WINDOW_MEMBERS.has(key.text) || /^\d+$/.test(key.text)) && patternGlobal(element.parent as ts.ObjectBindingPattern, scope, weakToo) !== null;
}

/** Глобальный объект отдан туда, где разбор его не видит. */
function passesOn(value: ts.Expression): boolean {
  const { parent: p, child } = consumerOf(value);
  if (isMember(p) && p.expression === child) return false;
  if (ts.isTypeOfExpression(p)) return false;
  // Значение отброшено либо проверено на истинность — не отдано никому.
  if (ts.isExpressionStatement(p) || ts.isVoidExpression(p)) return false;
  if (ts.isPrefixUnaryExpression(p) && p.operator === ts.SyntaxKind.ExclamationToken) return false;
  if ((ts.isIfStatement(p) || ts.isWhileStatement(p)) && p.expression === child) return false;
  if (ts.isConditionalExpression(p) && p.condition === child) return false;
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

/** Что разбирается образцом — текстом пути: `frame`, `frame.contentWindow`. */
function patternSource(pattern: ts.ObjectBindingPattern): string | null {
  const holder = pattern.parent;
  if ((ts.isVariableDeclaration(holder) || ts.isParameter(holder)) && holder.initializer) return labelOf(holder.initializer);
  if (ts.isBindingElement(holder) && ts.isObjectBindingPattern(holder.parent) && holder.propertyName) {
    const outer = patternSource(holder.parent);
    return outer === null ? null : `${outer}.${holder.propertyName.getText().replace(/\s+/g, "")}`;
  }
  return null;
}

/** Метка глобального объекта, из которого разбирается образец; `null` — не глобальный. */
function patternGlobal(pattern: ts.ObjectBindingPattern, scope: FileScope, weakToo: boolean): string | null {
  const holder = pattern.parent;
  if ((ts.isVariableDeclaration(holder) || ts.isParameter(holder)) && holder.initializer) {
    return isGlobalObject(holder.initializer, scope, weakToo) ? labelOf(holder.initializer) : null;
  }
  if (ts.isBindingElement(holder) && ts.isObjectBindingPattern(holder.parent) && holder.propertyName) {
    const key = propertyNameKey(holder.propertyName, scope);
    if (key.kind !== "name") return null;
    // Окно, которое API отдаёт членом ЛЮБОГО объекта: разбираемое окном быть не обязано.
    if (WINDOW_OF_ANY.has(key.text) || (weakToo && WEAK_WINDOW_OF_ANY.has(key.text))) return patternSource(pattern);
    const outer = patternGlobal(holder.parent, scope, weakToo);
    return outer !== null && (WINDOW_MEMBERS.has(key.text) || /^\d+$/.test(key.text)) ? `${outer}.${key.text}` : null;
  }
  return null;
}

/** Образец разбирает упорядочивающий транспорт. */
function patternOrdered(pattern: ts.ObjectBindingPattern, scope: FileScope): boolean {
  const holder = pattern.parent;
  return (ts.isVariableDeclaration(holder) || ts.isParameter(holder)) && !!holder.initializer && isOrderedTransport(holder.initializer, scope);
}

/** Места выпуска ОДНОГО источника. */
export function issuancesIn(file: string, text: string): IssuanceFinding[] {
  const kind = /\.tsx$/.test(file) ? ts.ScriptKind.TSX : ts.ScriptKind.TS;
  const source = ts.createSourceFile(file, text, ts.ScriptTarget.Latest, true, kind);
  const scope = scopeOf(source, file);
  const found: IssuanceFinding[] = [];
  const push = (node: ts.Node, what: string) => found.push({ file, line: lineOf(source, node), what });
  const passedOn = (label: string) => `глобальный объект ${label} передан значением — что с ним сделают, распознавателю не видно`;
  const computed = (label: string) => `доступ к глобальному объекту ${label} по вычисленному ключу`;
  const unrecognised = (usage: string, label: string, key: string) =>
    `${usage} ${label}.${key} — получатель не опознан, и это не упорядочивающий транспорт`;

  const visit = (node: ts.Node) => {
    if (isTypePosition(node)) return;

    if (ts.isIdentifier(node) && !isKeyPosition(node) && !ts.isTypeOfExpression(consumerOf(node).parent)) {
      if (BARE_TRANSPORT.has(node.text)) {
        push(node, isDeclarationName(node) ? `локальное имя ${node.text} заслоняет транспорт браузера` : `${usageOf(node)} ${node.text}`);
      } else if (
        ((GLOBALS.has(node.text) && !isShadowed(node, scope)) || scope.globals.has(node.text)) &&
        !isDeclarationName(node) &&
        passesOn(node)
      ) {
        push(node, passedOn(node.text));
      }
    }

    if (isMember(node) && !ts.isTypeOfExpression(consumerOf(node).parent)) {
      const key = memberKey(node, scope);
      if (key.kind === "name" && key.text === BEACON) push(node, `${usageOf(node)} ${BEACON}`);
      else if (key.kind === "name" && GLOBAL_TRANSPORT.has(key.text)) {
        if (isGlobalObject(node.expression, scope, true)) push(node, `${usageOf(node)} ${labelOf(node.expression)}.${key.text}`);
        else if (!isOrderedTransport(node.expression, scope)) push(node, unrecognised(usageOf(node), labelOf(node.expression), key.text));
      } else if (key.kind === "unknown" && isGlobalObject(node.expression, scope, false)) {
        push(node, computed(labelOf(node.expression)));
      }
    }

    // Окно, отданное API браузера (член либо результат вызова), передано значением.
    if (
      (isMember(node) || ts.isCallExpression(node)) &&
      !ts.isTypeOfExpression(consumerOf(node).parent) &&
      isGlobalObject(node, scope, false) &&
      passesOn(node)
    ) {
      push(node, passedOn(labelOf(node)));
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
          else if (!patternOrdered(node.parent, scope)) {
            push(node, unrecognised("ссылка на транспорт", patternSource(node.parent) ?? "разбираемое", key.text));
          }
        } else if (key.kind === "unknown" && strong !== null) {
          push(node, computed(strong));
        }
      }
    }

    if (ts.isCallExpression(node)) {
      const callee = unwrap(node.expression);
      const name = isMember(callee) ? ((k) => (k.kind === "name" ? k.text : ""))(memberKey(callee, scope)) : "";
      const navigates =
        opensWindow(node, scope) ||
        (isMember(callee) &&
          (((name === "assign" || name === "replace") && isLocation(callee.expression, scope)) ||
            (name === "open" && isGlobalObject(callee.expression, scope, true))));
      // `open.call(окно, адрес)` — адрес вторым аргументом.
      const head = addressHead(name === "call" ? node.arguments[1] : node.arguments[0]);
      if (navigates && head !== null && isEdgePathText(head)) push(node, `переход документа на путь края «${head}»`);

      // Отражение по ключу транспорта: `Reflect.get(x, "fetch")` читает то же, что `x.fetch`.
      const holder = isMember(callee) ? unwrap(callee.expression) : null;
      if (holder && ts.isIdentifier(holder) && REFLECTION.get(holder.text)?.has(name) && node.arguments.length >= 2) {
        const [target, keyArg] = node.arguments;
        const key = keyOfExpression(keyArg, scope);
        if (key.kind === "name" && BARE_TRANSPORT.has(key.text) && !isOrderedTransport(target, scope)) {
          push(node, `ссылка на транспорт ${labelOf(target)}.${key.text} отражением`);
        }
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
