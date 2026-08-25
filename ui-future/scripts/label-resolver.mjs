#!/usr/bin/env node
// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

/**
 * ОБЩИЙ ИСТОЧНИК: как из исходника консоли достаётся текст, который видит
 * пользователь.
 *
 * ── Зачем отдельным файлом ──────────────────────────────────────────────────
 *
 * Разбор заведён гейтом языка подписей (#478, #1249) и дал там +220 подписей при
 * неизменной полосе разметки: подпись, вынесенную в переменную, собранную
 * шаблоном, склейкой, тернарником или умолчанием, регулярное выражение по тексту
 * не видит вовсе.
 *
 * Ровно та же слепота была у гейта внутреннего словаря (#1259) — он судит те же
 * позиции. Исходов было два: написать второй разбор либо завести один на обоих.
 * Второй разбор запрещён не вкусом, а замером: два разбора одного предмета
 * расходятся МОЛЧА, и расходятся они там, где расхождение не видно, — оба
 * отвечают «пусто» на пустом входе. В этом дереве такое уже случалось.
 *
 * ── Что этот файл НЕ решает ─────────────────────────────────────────────────
 *
 * Он отвечает на один вопрос: «какими строками это выражение может оказаться
 * перед пользователем». ЧЕМ считать такую строку — переводимым словом, именем
 * механизма, законным термином — решает вызывающий, и словари остаются у него.
 * Одна семантика на двоих была бы объединением по самой узкой стороне.
 *
 * ── Граница разбора, названная числом, а не умолчанием ──────────────────────
 *
 * Вызов, обращение к полю, любое иное вычисление дают ПУСТО: текста там нет, он
 * приходит из данных. Такие позиции считаются отдельно (`dataSites`) — «ноль
 * находок» обязано быть отличимо от «ноль прочитанного».
 */

import ts from "typescript";

/** Подстановка на месте вычисляемой части. Не буква и не цифра — разбору не мешает. */
export const HOLE = "…";

/** Предел раскрытия имён: защита от взаимных ссылок, а не ограничение смысла. */
export const MAX_RESOLVE_DEPTH = 8;

/**
 * Содержимое элемента верно́ ДОСЛОВНО и подписью не является. `code` здесь —
 * атрибут antd `Typography.Text code`, который рендерится в `<code>`.
 */
export const VERBATIM_CONTENT_TAGS = new Set(["style", "script", "pre", "code"]);

/**
 * Имена файла, объявленные значением: `const X = …`.
 *
 * Имя, объявленное ДВАЖДЫ, а также имя параметра и элемента деструктуризации,
 * из карты СНИМАЕТСЯ. Причина не в аккуратности: `{ label }` в пропсах
 * компонента затеняет модульный `const label`, и без этого правила подпись
 * судилась бы по чужому объявлению. Разбор областей видимости здесь не нужен —
 * достаточно отказаться судить неоднозначное: не судить безопаснее, чем судить
 * не то.
 */
export function collectBindings(sf) {
  const bound = new Map();
  const ambiguous = new Set();
  const claim = (name, init) => {
    if (bound.has(name) || ambiguous.has(name)) {
      bound.delete(name);
      ambiguous.add(name);
      return;
    }
    if (init) bound.set(name, init);
    else ambiguous.add(name);
  };
  const walk = (n) => {
    if (ts.isVariableDeclaration(n) && ts.isIdentifier(n.name)) claim(n.name.text, n.initializer);
    else if (ts.isParameter(n) && ts.isIdentifier(n.name)) claim(n.name.text, null);
    else if (ts.isBindingElement(n) && ts.isIdentifier(n.name)) claim(n.name.text, null);
    ts.forEachChild(n, walk);
  };
  walk(sf);
  return bound;
}

/** Прямой литерал — ровно то, что читала регулярным выражением первая редакция. */
export function isDirectLiteral(node) {
  if (!node) return false;
  if (ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node)) return true;
  if (ts.isJsxExpression(node) && node.expression) return isDirectLiteral(node.expression);
  return false;
}

/**
 * Все строки, которыми выражение МОЖЕТ оказаться перед пользователем.
 *
 * Тернарник даёт обе ветви — обе показываются, в разных состояниях. `||`/`??`
 * дают обе стороны: правая и есть подпись по умолчанию. Шаблон и склейка —
 * свой текст с `HOLE` на месте подстановки: так соседство слов сохраняется
 * («Создание: …»), а вычисленная часть не выдаётся за подпись.
 *
 * Вызов, обращение к полю, всё остальное — ПУСТО. Это граница, и она названа
 * числом в переписи, а не умолчана.
 */
export function literalsOf(node, bound, depth = 0, seen = new Set()) {
  if (!node || depth > MAX_RESOLVE_DEPTH) return [];
  if (ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node)) return [node.text];
  if (ts.isParenthesizedExpression(node) || ts.isAsExpression(node) || ts.isNonNullExpression(node))
    return literalsOf(node.expression, bound, depth + 1, seen);
  if (ts.isSatisfiesExpression(node)) return literalsOf(node.expression, bound, depth + 1, seen);
  if (ts.isJsxExpression(node)) return node.expression ? literalsOf(node.expression, bound, depth + 1, seen) : [];
  if (ts.isConditionalExpression(node))
    return [
      ...literalsOf(node.whenTrue, bound, depth + 1, seen),
      ...literalsOf(node.whenFalse, bound, depth + 1, seen),
    ];
  if (ts.isBinaryExpression(node)) {
    const op = node.operatorToken.kind;
    if (op === ts.SyntaxKind.BarBarToken || op === ts.SyntaxKind.QuestionQuestionToken)
      return [
        ...literalsOf(node.left, bound, depth + 1, seen),
        ...literalsOf(node.right, bound, depth + 1, seen),
      ];
    if (op === ts.SyntaxKind.PlusToken) {
      const left = literalsOf(node.left, bound, depth + 1, seen);
      const right = literalsOf(node.right, bound, depth + 1, seen);
      return [(left[0] ?? HOLE) + (right[0] ?? HOLE)];
    }
    return [];
  }
  if (ts.isTemplateExpression(node)) {
    let out = node.head.text;
    for (const span of node.templateSpans) out += HOLE + span.literal.text;
    return [out];
  }
  if (ts.isIdentifier(node)) {
    if (seen.has(node.text)) return [];
    const init = bound.get(node.text);
    if (!init) return [];
    const deeper = new Set(seen);
    deeper.add(node.text);
    return literalsOf(init, bound, depth + 1, deeper);
  }
  return [];
}

/** Элемент, чьё содержимое дословно (`<style>`, `<pre>`, `<code>`, antd `code`). */
export function isVerbatimContent(el) {
  if (!ts.isJsxElement(el)) return false;
  const opening = el.openingElement;
  const tag = opening.tagName.getText();
  if (VERBATIM_CONTENT_TAGS.has(tag) || VERBATIM_CONTENT_TAGS.has(tag.split(".").pop() ?? "")) return true;
  return opening.attributes.properties.some(
    (a) => ts.isJsxAttribute(a) && ts.isIdentifier(a.name) && a.name.text === "code",
  );
}

/**
 * Выражение — ЕДИНСТВЕННОЕ содержимое элемента, то есть его текст целиком.
 *
 * Различитель не косметический, он и держит границу между подписью и данными
 * в тексте JSX. Стоящее в одиночку выражение пользователь читает как текст
 * элемента; стоящее среди других детей — как значение, вставленное в фразу, а
 * фразу гейт уже прочитал сам.
 */
export function isSoleChild(expr) {
  const el = expr.parent;
  if (!el || !(ts.isJsxElement(el) || ts.isJsxFragment(el))) return false;
  if (isVerbatimContent(el)) return false;
  return el.children.every((c) => c === expr || (ts.isJsxText(c) && c.text.trim() === ""));
}

/**
 * Тексты одного файла, видимые пользователю: `{ line, kind, key, value, origin }`.
 *
 * Наборы имён передаёт ВЫЗЫВАЮЩИЙ — они и есть его предмет:
 *   · `labelKeys`      — свойства объекта И атрибуты JSX, несущие текст;
 *   · `labelAttrsOnly` — то же, но только как атрибут JSX (как свойство объекта
 *                        эти имена значат другое);
 *   · `valueKeys`      — позиции ПРИМЕРА значения: не текст, считаются отдельно;
 *   · `labelHelpers`   — вызовы, собирающие текст из аргументов;
 *   · `jsxText`        — читать ли текст элемента.
 *
 * `dataSites` — та самая граница: позиция есть, текста в ней нет. У текста
 * элемента это не считается: там кандидатов сотни, и почти все они — данные по
 * замыслу, а не потерянный текст.
 */
export function collectLabels(
  rel,
  source,
  { labelKeys, labelAttrsOnly = new Set(), valueKeys = new Set(), labelHelpers = new Set(), jsxText = true } = {},
) {
  const sf = ts.createSourceFile(
    rel,
    source,
    ts.ScriptTarget.Latest,
    /* setParentNodes */ true,
    rel.endsWith(".tsx") ? ts.ScriptKind.TSX : ts.ScriptKind.TS,
  );
  const bound = collectBindings(sf);
  const labels = [];
  let valueSites = 0;
  let dataSites = 0;

  const at = (node) => sf.getLineAndCharacterOfPosition(node.getStart(sf)).line + 1;

  const push = (node, kind, key, init, countData) => {
    const origin = isDirectLiteral(init) ? "разметка" : "вычислено";
    const values = literalsOf(init, bound);
    if (values.length === 0) {
      if (countData) dataSites++;
      return;
    }
    for (const value of values) labels.push({ line: at(node), kind, key, value, origin });
  };

  const walk = (node) => {
    if (ts.isJsxAttribute(node) && node.name) {
      const key = node.name.getText(sf);
      if (valueKeys.has(key)) valueSites++;
      else if (labelKeys.has(key) || labelAttrsOnly.has(key)) push(node, "атрибут JSX", key, node.initializer, true);
    }
    if (ts.isPropertyAssignment(node) && (ts.isIdentifier(node.name) || ts.isStringLiteral(node.name))) {
      const key = node.name.text;
      if (valueKeys.has(key)) valueSites++;
      else if (labelKeys.has(key)) push(node, "свойство", key, node.initializer, true);
    }
    if (jsxText && ts.isJsxText(node) && node.text.trim() !== "") {
      labels.push({ line: at(node), kind: "текст JSX", key: "", value: node.text.trim(), origin: "разметка" });
    }
    // Текст элемента, пришедший ВЫЧИСЛЕНИЕМ: `<Text>{valueLabel}</Text>`.
    // Только единственное содержимое элемента — см. `isSoleChild`.
    if (jsxText && ts.isJsxExpression(node) && node.expression && isSoleChild(node)) {
      push(node, "текст JSX", "", node.expression, false);
    }
    // Текст, собранный помощником: `labelWithInfo("Имя", "…")`.
    if (ts.isCallExpression(node) && ts.isIdentifier(node.expression) && labelHelpers.has(node.expression.text)) {
      for (const arg of node.arguments) push(arg, `аргумент ${node.expression.text}`, "", arg, true);
    }
    ts.forEachChild(node, walk);
  };
  walk(sf);
  return { labels, valueSites, dataSites };
}
