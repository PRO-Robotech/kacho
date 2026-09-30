// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

import ts from "typescript";

/**
 * Читатели параметра адреса возврата — по УЗЛУ разбора, а не по образцу текста
 * (приёмка F6b, условие ревью безопасности о выходах с экрана подтверждения).
 *
 * Читатель — вызов `<что-то>.get(<ключ>)`, где ключ — имя `RETURN_TO_PARAM`
 * либо строковый литерал с его значением `returnTo`. Держатель предиката
 * `safeInternalPath` для адреса возврата экранов церемонии ОДИН — `useReturnTo`;
 * второй читатель завёл бы второе суждение о происхождении адреса, и выход,
 * читающий его мимо первого, уводил бы на присланный адрес молча.
 *
 * Комментарии и строки узлами вызова не являются — поэтому «// .get(RETURN_TO_PARAM)»
 * в тексте файла находкой не будет, а `params.get("returnTo")` будет.
 */

export interface ReturnToReader {
  file: string;
  line: number;
  text: string;
}

const PARAM_NAME = "RETURN_TO_PARAM";
const PARAM_VALUE = "returnTo";

export function returnToReaders(sources: ReadonlyArray<{ file: string; text: string }>): ReturnToReader[] {
  const out: ReturnToReader[] = [];
  for (const { file, text } of sources) {
    const kind = file.endsWith(".tsx") ? ts.ScriptKind.TSX : ts.ScriptKind.TS;
    const sf = ts.createSourceFile(file, text, ts.ScriptTarget.Latest, true, kind);
    const walk = (n: ts.Node) => {
      if (
        ts.isCallExpression(n) &&
        ts.isPropertyAccessExpression(n.expression) &&
        n.expression.name.text === "get" &&
        n.arguments.length === 1
      ) {
        const arg = n.arguments[0];
        const named =
          (ts.isIdentifier(arg) && arg.text === PARAM_NAME) ||
          (ts.isPropertyAccessExpression(arg) && arg.name.text === PARAM_NAME) ||
          ((ts.isStringLiteral(arg) || ts.isNoSubstitutionTemplateLiteral(arg)) && arg.text === PARAM_VALUE);
        if (named) {
          out.push({
            file,
            line: sf.getLineAndCharacterOfPosition(n.getStart(sf)).line + 1,
            text: n.getText(sf),
          });
        }
      }
      ts.forEachChild(n, walk);
    };
    walk(sf);
  }
  return out;
}
