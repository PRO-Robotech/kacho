// Copyright (c) PRO-Robotech
// SPDX-License-Identifier: BUSL-1.1

// ЗАГОЛОВОК СТРАНИЦЫ УСТУПАЕТ ШИРИНУ ПОСЛЕДНИМ (#1547).
//
// ПРЕДМЕТ. Шапка страницы — одна строка из двух соседей: колонка заголовка и
// слот ручек. Колонка заголовка объявлена сжимаемой ДО НУЛЯ (`minWidth: 0`
// снимает у flex-ребёнка неявный минимум по содержимому) — иначе длинное имя
// ресурса вылезает за свою зону и обрезка многоточием не работает вовсе. Раз
// так, всякий сосед, отказавшийся уступать, отдаёт колонке заголовка ВЕСЬ
// недостаток ширины: она сжимается до нуля, и предмет страницы пропадает с
// экрана при живой разметке.
//
// ЧТО НАБЛЮДАЛОСЬ. Слот ручек держал `flex: "0 0 auto"` — ширину по содержимому
// и никакого предела. На списке IP-адресов (окно 1280, рабочая область 932)
// ручки занимали 1068 точек, заголовку доставалось 0. Две сквозные пробы
// консоли падали по 60 секунд с «element(s) not found», хотя протокол падения
// говорит обратное: `60 × locator resolved to <h3 …>IP-адреса</h3> — unexpected
// value "hidden"`. Заголовок был в разметке всё это время; у него была нулевая
// коробка. Прочитанное как «страница не открылась» на деле было «страница
// открылась, а её предмет схлопнут».
//
// ПОЧЕМУ ОБЪЯВЛЕНИЕ, А НЕ РЕНДЕР. Свойство геометрическое, а измерять его в этом
// харнессе нечем: в jsdom у всего нулевой размер, поэтому проба, монтирующая
// шапку, дала бы «ноль» и на исправном дереве, и на сломанном — то есть не
// различала бы ровно тот случай, ради которого пишется. Настоящую ширину видит
// только браузер, и там свойство уже закреплено сквозными пробами списка
// адресов. Здесь читается ОБЪЯВЛЕНИЕ — единственное место, где решение «кто
// уступает» принимается и может быть молча отозвано.
//
// ЧЕГО ГЕЙТ НЕ ЗАКРЫВАЕТ, И ЭТО СКАЗАНО ПРЯМО. Он судит ПОРЯДОК уступки, а не
// достаточность остатка: ряд ручек, выросший так, что заголовку остаётся две
// точки, объявлен верно, и гейт промолчит. Порог меряется браузером; гейт держит
// то, что от порога не зависит, — что недостаток ширины вообще делится.
//
// Комментарии снимаются перед разбором намеренно: объяснение выше называет
// `flex: "0 0 auto"` дословно, и гейт по сырому тексту краснел бы на собственном
// разборе — ровно тот класс, который он заведён ловить.

import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { stripComments } from "@shared/test/strip-comments";

const UI_ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../..");
const PAGE_HEAD = path.join(UI_ROOT, "shared/src/components/organisms/DetailShell/PageHead.tsx");
const LIST_PAGE = path.join(UI_ROOT, "shared/src/components/organisms/ResourceListPage/ResourceListPage.tsx");

/** Исходник без комментариев — судится исполняемая часть, а не текст. */
function code(file: string): string {
  return stripComments(readFileSync(file, "utf8"));
}

/**
 * Тело ближайшего `style={{ … }}` рядом с якорем.
 *
 * Якорь — то, чем элемент ЯВЛЯЕТСЯ (`className="kc-list-tools"`, открывающий тег
 * заголовка, вставленное поддерево `{actions}`), а не его место в файле:
 * перестановка соседей не должна молча переводить гейт на другой элемент.
 * `before` берёт последнее объявление до якоря — стиль стоит на родителе, а
 * якорь внутри него; `after` — первое после.
 */
function styleNear(src: string, anchor: string, dir: "before" | "after"): string | null {
  const at = src.indexOf(anchor);
  if (at < 0) return null;
  const open = dir === "before" ? src.lastIndexOf("style={{", at) : src.indexOf("style={{", at);
  if (open < 0) return null;
  let depth = 0;
  for (let i = open + "style={".length; i < src.length; i++) {
    if (src[i] === "{") depth++;
    else if (src[i] === "}") {
      depth--;
      if (depth === 0) return src.slice(open + "style={{".length, i);
    }
  }
  return null;
}

/** Объявлено ли, что элемент НЕ отдаёт ширину: `flexShrink: 0` либо нулевая
 *  вторая позиция сокращённой записи `flex: "<grow> 0 …"`. */
function refusesToShrink(style: string): boolean {
  if (/flexShrink:\s*0\b/.test(style)) return true;
  return /flex:\s*"\s*[\d.]+\s+0(\s|")/.test(style);
}

/** Может ли элемент сжаться НИЖЕ своего содержимого. Без `minWidth: 0` у
 *  flex-ребёнка остаётся неявный минимум по содержимому, и объявленная
 *  сжимаемость не срабатывает — та же дыра через вторую дверь. */
function collapsesBelowContent(style: string): boolean {
  return /minWidth:\s*0\b/.test(style);
}

describe("шапка страницы — заголовок уступает ширину последним", () => {
  it("перепись: оба соседа строки прочитаны, и колонка заголовка сжимаема до нуля", () => {
    // «Ноль нарушений» обязано быть отличимо от «ноль прочитанного»: переехавший
    // файл, переименованный класс или снятый заголовок иначе дали бы пустой
    // обход и зелёный вердикт.
    const head = code(PAGE_HEAD);
    const title = styleNear(head, "<Typography.Title", "before");
    const tools = styleNear(head, 'className="kc-list-tools"', "after");

    expect({ fileRead: head.length > 0 }).toEqual({ fileRead: true });
    expect({ titleColumnFound: title !== null }).toEqual({ titleColumnFound: true });
    expect({ toolsSlotFound: tools !== null }).toEqual({ toolsSlotFound: true });

    // Предпосылка запрета: класс существует ровно потому, что колонка заголовка
    // сжимаема до нуля. Перестанет быть — предмет исчезнет, и требование к
    // соседу придётся пересматривать, а не молча оставлять в силе.
    expect({ titleCollapsesToZero: collapsesBelowContent(title!) }).toEqual({ titleCollapsesToZero: true });
  });

  it("слот ручек в шапке ОТДАЁТ ширину: и объявленно, и ниже содержимого", () => {
    const tools = styleNear(code(PAGE_HEAD), 'className="kc-list-tools"', "after")!;

    expect({ slotRefusesToShrink: refusesToShrink(tools) }).toEqual({ slotRefusesToShrink: false });
    expect({ slotCollapsesBelowContent: collapsesBelowContent(tools) }).toEqual({ slotCollapsesBelowContent: true });
  });

  it("недостаток ширины уходит в сужающие ручки, а не в подписи кнопок", () => {
    // Решение принято вместе с починкой и записано там же: обрезанная подпись
    // действия («Зарезервир…») не говорит, что произойдёт по нажатию, тогда как
    // более узкое поле поиска остаётся собой. Перенос ряда в две строки отвергнут
    // отдельно — он занимает 72 точки против 44, объявленных высотой шапки, и
    // вылезал бы под крошки. Без этого утверждения обе половины решения
    // отзываются молча.
    const src = code(LIST_PAGE);
    const row = styleNear(src, 'className="kc-list-tools"', "after");
    const narrowing = styleNear(src, "{narrowing}", "before");
    const actions = styleNear(src, "{actions}", "before");

    expect({ rowFound: row !== null, narrowingFound: narrowing !== null, actionsFound: actions !== null }).toEqual({
      rowFound: true,
      narrowingFound: true,
      actionsFound: true,
    });

    expect({ rowWrapsToTwoLines: /flexWrap:\s*"wrap"/.test(row!) }).toEqual({ rowWrapsToTwoLines: false });
    expect({ rowCollapsesBelowContent: collapsesBelowContent(row!) }).toEqual({ rowCollapsesBelowContent: true });

    expect({ narrowingYields: collapsesBelowContent(narrowing!) && !refusesToShrink(narrowing!) }).toEqual({
      narrowingYields: true,
    });
    expect({ actionCaptionsKeepTheirWidth: refusesToShrink(actions!) }).toEqual({
      actionCaptionsKeepTheirWidth: true,
    });
  });
});
