// Распознаватель: монтирует ли исходник ПОКАЗ уведомлений.
//
// Живёт отдельно от пробы дерева намеренно: предикат, который нельзя подать
// синтетическим входом, доказать инъекцией нельзя — его пришлось бы проверять
// правкой настоящих модулей, то есть на дереве, которое он же и судит.

import ts from "typescript";

/** Имя компонента показа — так его экспортирует общий модуль и все его копии. */
const DISPLAY_EXPORT = "Toaster";

/**
 * Кого этот исходник ввёл в область видимости под видом показа.
 *
 * `named` — локальные имена, привязанные к экспорту `Toaster` (в том числе
 * переименованные: `import { Toaster as Notifications }`).
 * `namespaces` — имена пространств (`import * as UI`), у которых показ
 * достаётся обращением к свойству.
 */
interface DisplayBindings {
  named: Set<string>;
  namespaces: Set<string>;
}

function collectBindings(source: ts.SourceFile): DisplayBindings {
  const named = new Set<string>();
  const namespaces = new Set<string>();
  for (const statement of source.statements) {
    if (!ts.isImportDeclaration(statement)) continue;
    const clause = statement.importClause;
    if (!clause?.namedBindings) continue;
    if (ts.isNamespaceImport(clause.namedBindings)) {
      namespaces.add(clause.namedBindings.name.text);
      continue;
    }
    for (const spec of clause.namedBindings.elements) {
      // Имя, под которым символ ЭКСПОРТИРОВАН, а не под которым он здесь зовётся:
      // у `Toaster as Notifications` первое — `propertyName`, второе — `name`.
      const exported = spec.propertyName?.text ?? spec.name.text;
      if (exported === DISPLAY_EXPORT) named.add(spec.name.text);
    }
  }
  return { named, namespaces };
}

/** Совпадает ли имя тега с одной из привязок показа. */
function tagIsDisplay(
  tag: ts.JsxTagNameExpression,
  bindings: DisplayBindings,
): boolean {
  if (ts.isIdentifier(tag)) return bindings.named.has(tag.text);
  // `<UI.Toaster />` — показ, взятый из пространства имён: слева привязка
  // пространства, справа имя экспорта.
  if (ts.isPropertyAccessExpression(tag)) {
    return (
      ts.isIdentifier(tag.expression) &&
      bindings.namespaces.has(tag.expression.text) &&
      tag.name.text === DISPLAY_EXPORT
    );
  }
  return false;
}

/**
 * Монтирует ли исходник показ уведомлений.
 *
 * Судит УЗЕЛ РАЗБОРА, а не написание. Прежде здесь стояло вхождение подстроки
 * `<Toaster`, и слепых зон у него было три, все тихие: подстрока совпадала на
 * соседе с похожим именем (`<ToasterPlaceholder`), на упоминании в комментарии и
 * на строковом литерале. То есть модуль мог не показывать ничего, а проба
 * молчала бы — ровно тот класс, ради которого она написана. Обнаружено тем, что
 * инъекция `<ToasterDISABLED` не покраснела.
 *
 * ЗАКОННЫЕ ФОРМЫ, которые распознаватель обязан знать, и каждая доказана
 * инъекцией в парной пробе:
 *
 *   · `<Toaster />` — самозакрывающийся тег (все семь модулей дерева сегодня);
 *   · `<Toaster>…</Toaster>` — тег с содержимым;
 *   · `import { Toaster as X }` + `<X />` — переименование при ввозе;
 *   · `import * as UI` + `<UI.Toaster />` — обращение через пространство имён.
 *
 * ГРАНИЦА НАЗВАНА: ввоз по умолчанию (`import Toaster from "…"`) показом здесь НЕ
 * считается. Имя такого ввоза произвольно и с экспортом не связано ничем, так что
 * признать его можно было бы только по ПУТИ модуля — то есть вернуться к суду по
 * написанию, от которого распознаватель и уходит. Формы этой в дереве нет: общий
 * модуль и все его копии экспортируют показ именованным.
 *
 * `.ts` показа не монтирует by construction — разметки в нём не бывает, поэтому
 * вид разбора выбирается по расширению: `.ts`, разобранный как `.tsx`, ломает
 * обобщённые типы (`<T>(x) => x`), а не находит показ.
 */
export function mountsDisplay(fileName: string, source: string): boolean {
  const kind = fileName.endsWith(".tsx") ? ts.ScriptKind.TSX : ts.ScriptKind.TS;
  if (kind === ts.ScriptKind.TS) return false;
  const parsed = ts.createSourceFile(
    fileName,
    source,
    ts.ScriptTarget.Latest,
    true,
    kind,
  );
  const bindings = collectBindings(parsed);
  if (bindings.named.size === 0 && bindings.namespaces.size === 0) return false;

  let found = false;
  const visit = (node: ts.Node): void => {
    if (found) return;
    if (
      ts.isJsxSelfClosingElement(node) &&
      tagIsDisplay(node.tagName, bindings)
    ) {
      found = true;
      return;
    }
    if (ts.isJsxOpeningElement(node) && tagIsDisplay(node.tagName, bindings)) {
      found = true;
      return;
    }
    ts.forEachChild(node, visit);
  };
  ts.forEachChild(parsed, visit);
  return found;
}
