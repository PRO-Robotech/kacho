// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// Кто решает «перемещать нечем» — факты одного файла, прочитанные из его
// СИНТАКСИЧЕСКОГО ДЕРЕВА.
//
// Разбор, а не поиск по образцу, здесь несущий: слово «Переместить» стоит в этом
// дереве и в прозе — в шапках проб, в объяснении самого перечня, в комментарии у
// пункта меню. Гейт по подстроке краснел бы на собственном объяснении и молчал
// бы на подписи, собранной из переменной. Узел `label: "Переместить"` — это
// ПРОИЗВОДИТЕЛЬ подписи; комментарий узлом не является by construction.

import ts from "typescript";

/** Подпись пункта-заглушки. Один литерал на дерево — им и опознаётся producer. */
export const MOVE_LABEL = "Переместить";

/** Канонический предикат: единственный, кто вправе решать этот вопрос. */
export const CANON_PREDICATE = "resourceIsMoveCapable";

export interface MoveStubFacts {
  /** Файл производит подпись пункта — есть узел `label: "Переместить"`. */
  producesMoveStub: boolean;
  /** Файл ссылается на канонический предикат. */
  usesCanonicalPredicate: boolean;
  /** Файл ОБЪЯВЛЯЕТ канонический предикат (единственный источник). */
  declaresCanonicalPredicate: boolean;
  /** Строки, где производится подпись, — для находки с координатой. */
  producerLines: number[];
}

/**
 * parseMoveStubSource — факты одного файла.
 *
 * `label` опознаётся как имя свойства объекта: и пункт меню строки, и пункт меню
 * карточки собираются объектным литералом. Значение берётся только строковым
 * литералом: подпись, собранная выражением, производителем этой подписи не
 * является и под утверждение не подпадает — граница названа, а не подразумевается.
 */
export function parseMoveStubSource(file: string, source: string): MoveStubFacts {
  const sf = ts.createSourceFile(file, source, ts.ScriptTarget.Latest, /* setParentNodes */ false, ts.ScriptKind.TSX);

  const producerLines: number[] = [];
  let usesCanonicalPredicate = false;
  let declaresCanonicalPredicate = false;

  const visit = (node: ts.Node): void => {
    if (
      ts.isPropertyAssignment(node) &&
      ts.isIdentifier(node.name) &&
      node.name.text === "label" &&
      ts.isStringLiteral(node.initializer) &&
      node.initializer.text === MOVE_LABEL
    ) {
      producerLines.push(sf.getLineAndCharacterOfPosition(node.getStart(sf)).line + 1);
    }

    if (ts.isFunctionDeclaration(node) && node.name?.text === CANON_PREDICATE) {
      declaresCanonicalPredicate = true;
    }

    if (ts.isIdentifier(node) && node.text === CANON_PREDICATE) {
      usesCanonicalPredicate = true;
    }

    ts.forEachChild(node, visit);
  };
  visit(sf);

  return {
    producesMoveStub: producerLines.length > 0,
    usesCanonicalPredicate,
    declaresCanonicalPredicate,
    producerLines,
  };
}
