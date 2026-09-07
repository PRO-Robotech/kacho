// Гейт: признак «строка данных против строки устройства» отделяет одно от
// другого, и установленная таблица помечает свои служебные строки именно так.
//
// # Предмет
//
// Тело общей таблицы всегда несёт служебные `<tr>`, в которых данных нет:
// строку обмера (рисуется, пока таблица меряет ширину столбцов) и, когда набор
// пуст, строку-заполнитель с текстом пустого состояния. Обе живут в том же
// `<tbody>`, что и строки данных.
//
// Значит положительный контроль вида «строк в теле таблицы больше нуля»
// выполняется ПУСТОЙ таблицей: у неё их ровно две. Проба, построенная на таком
// контроле, судит о пустой таблице — и все её отрицания («меню действий нет»,
// «флажков нет») становятся верны by construction.
//
// # Цена измерена, а не предположена (#1070)
//
// Прогон 2026-08-23: проба «в строках списка нет флажков и группового удаления»
// объявила, что у строк списка зон нет меню действий («строк 2»). Снимок
// страницы, снятый тем же отказом, показывал меню у КАЖДОЙ строки; на соседней
// ветке того же часа та же проба напечатала «строк 7, меню действий 6». Продукт
// был исправен — контроль пропустил вперёд ещё не дозагрузившуюся таблицу.
//
// # Что утверждается — ДВЕ вещи, и одна из другой не следует
//
//   1. ПРЕДПОСЫЛКА. Установленная таблица рисует служебные строки именно теми
//      признаками, которыми их отделяет общий источник. Признаки живут в чужом
//      пакете и меняются вместе с его версией; без этой проверки отделение
//      перестанет срабатывать МОЛЧА, и контроль вернётся к прежней пустоте.
//   2. ПРЕДИКАТ. Локатор общего источника даёт 0 на теле пустой таблицы и N на
//      теле из N строк. Это контроль в обе стороны: без первой половины гейт
//      зеленел бы на локаторе, не отсекающем ничего; без второй — на локаторе,
//      не находящем ничего.
//
// # Почему гейт ИМПОРТИРУЕТ признак, а не читает исходник пробы
//
// Первая редакция брала селектор чтением `e2e/specs/console-forms.spec.ts` как
// текста — чтобы у пробы и гейта не завелось двух копий предиката. Замысел
// верен, средство противоречило и замыслу, и дереву: гейт, заведённый ради
// сверки с СУЩЕСТВОМ, сверялся с ТЕКСТОМ, а дерево несёт правило, что проба
// интерфейса модуль консоли текстом не читает, и стережёт его гейтом
// `internal/repohygiene` `TestUITestsDoNotReadTheirOwnSourceAsText` (перепись на
// стволе: 354 пробы, читают с диска 50, обходят дерево 40, находок 0).
//
// Признак вынесен в общий источник, который ОБЕ стороны импортируют. Копий
// предиката не стало вовсе, поэтому «взять его из пробы» перестало быть нужным:
// расхождению неоткуда взяться by construction, и доказывать его отсутствие
// нечем — его нет.
//
// Чтение остаётся ровно там, где оно единственно возможно: у установленной
// таблицы. Её разметку не получить исполнением — вопрос «чем помечена строка,
// которую рисует чужой пакет» задаётся только его исходнику, и это `.js` чужого
// пакета, а не модуль консоли.

import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { STRUCTURAL_BODY_ROW_MARKS, censusOfBody, dataRowSelector } from "./console-table-rows";

const UI_ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../..");
const TABLE_PKG = path.join(UI_ROOT, "node_modules/@rc-component/table/es");
const MEASURE_ROW = path.join(TABLE_PKG, "Body/MeasureRow.js");
const BODY = path.join(TABLE_PKG, "Body/index.js");

const SCOPE = ".app-main";

/** Тело таблицы: служебные строки плюс `dataCount` строк данных. */
function tbodyWith(dataCount: number): HTMLElement {
  const root = document.createElement("div");
  root.className = SCOPE.slice(1);
  const rows = [
    // строка обмера — рисуется всегда
    `<tr aria-hidden="true" class="ant-table-measure-row" style="height:0"><td></td></tr>`,
    // заполнитель — только когда набор пуст, и текст у него ЕСТЬ
    ...(dataCount === 0 ? [`<tr class="ant-table-placeholder"><td>Нет данных</td></tr>`] : []),
    ...Array.from({ length: dataCount }, (_, i) => `<tr class="ant-table-row"><td>строка ${i}</td></tr>`),
  ];
  root.innerHTML = `<table><tbody>${rows.join("")}</tbody></table>`;
  document.body.append(root);
  return root;
}

afterEach(() => {
  document.body.innerHTML = "";
});

describe("строка данных отличима от строки устройства таблицы (#1070)", () => {
  it("предпосылка: установленная таблица рисует строку обмера тем признаком, которым её отделяют", () => {
    const src = readFileSync(MEASURE_ROW, "utf8");
    // Имя файла входит в сравниваемое значение: иначе «не нашли» было бы
    // неотличимо от «читали не тот файл».
    expect([
      `файл: ${path.relative(UI_ROOT, MEASURE_ROW)}`,
      `скрыта от доступности: ${/"aria-hidden":\s*"true"/.test(src)}`,
      `несёт свой класс: ${/`\$\{prefixCls\}-measure-row`/.test(src)}`,
      `признак объявлен общим источником: ${STRUCTURAL_BODY_ROW_MARKS.includes('[aria-hidden="true"]')}`,
    ]).toEqual([
      `файл: ${path.relative(UI_ROOT, MEASURE_ROW)}`,
      "скрыта от доступности: true",
      "несёт свой класс: true",
      "признак объявлен общим источником: true",
    ]);
  });

  it("предпосылка: пустой набор рисует строку-заполнитель тем признаком, которым её отделяют", () => {
    const src = readFileSync(BODY, "utf8");
    expect([
      `файл: ${path.relative(UI_ROOT, BODY)}`,
      `несёт свой класс: ${/`\$\{prefixCls\}-placeholder`/.test(src)}`,
      `рисуется только на пустом наборе: ${/isEmpty:\s*true/.test(src)}`,
      `признак объявлен общим источником: ${STRUCTURAL_BODY_ROW_MARKS.includes(".ant-table-placeholder")}`,
    ]).toEqual([
      `файл: ${path.relative(UI_ROOT, BODY)}`,
      "несёт свой класс: true",
      "рисуется только на пустом наборе: true",
      "признак объявлен общим источником: true",
    ]);
  });

  it("предикат: на пустой таблице строк данных НОЛЬ, хотя `<tr>` в теле есть", () => {
    const root = tbodyWith(0);
    // Контроль предпосылки самого гейта: не неси синтетика служебных строк,
    // «ноль строк данных» вышло бы из пустоты, а не из отделения.
    expect(root.querySelectorAll("tbody tr").length).toBe(2);
    expect(document.querySelectorAll(dataRowSelector(SCOPE)).length).toBe(0);
  });

  it("предикат: на таблице из трёх строк строк данных ТРИ — отделение не съедает данные", () => {
    const root = tbodyWith(3);
    expect(root.querySelectorAll("tbody tr").length).toBe(4);
    expect(document.querySelectorAll(dataRowSelector(SCOPE)).length).toBe(3);
  });

  it("перепись: объём осмотренного назван, чтобы «ноль находок» было отличимо от «ноль прочитанного»", () => {
    // eslint-disable-next-line no-console
    console.log(
      `осмотрено: признаков служебных строк объявлено ${STRUCTURAL_BODY_ROW_MARKS.length} ` +
        `(${STRUCTURAL_BODY_ROW_MARKS.join(", ")}); файлов установленной таблицы прочитано 2; ` +
        `локатор строк данных — «${dataRowSelector(SCOPE)}»`,
    );
    // Признак без своей предпосылки — слепая зона: каждому объявленному
    // признаку выше отвечает утверждение о том, что таблица его вправду ставит.
    expect(STRUCTURAL_BODY_ROW_MARKS.length).toBe(2);
  });
});

/**
 * # Предмет второго гейта (#2237)
 *
 * Отделить строку данных от строки устройства — половина дела. Вторая половина:
 * числа, которые проба сравнивает МЕЖДУ СОБОЙ, обязаны прийти с ОДНОГО
 * состояния страницы. Пока их снимали четырьмя отдельными обращениями, между
 * контролем и утверждением помещалась целая перерисовка списка, и прогон
 * 34142361500 выдал отказ, противоречащий сам себе: «строк данных 0, меню 48
 * (всего строк в теле таблицы 49)» — при исправном продукте.
 *
 * Инъекция ниже меняет РОВНО ОДИН факт против положительного близнеца: КОГДА
 * именно список перерисовался. Ни разметка, ни признаки, ни селекторы не
 * трогаются.
 */
describe("числа переписи приходят с одного состояния страницы (#2237)", () => {
  const MENU = '<td><span class="anticon anticon-more"></span></td>';

  /** Тело со строками данных: у каждой — своё меню действий, как в продукте. */
  function fill(root: HTMLElement, dataCount: number): void {
    const rows = [
      `<tr aria-hidden="true" class="ant-table-measure-row" style="height:0"><td></td></tr>`,
      ...(dataCount === 0 ? [`<tr class="ant-table-placeholder"><td>Нет данных</td></tr>`] : []),
      ...Array.from(
        { length: dataCount },
        (_, i) => `<tr class="ant-table-row"><td>строка ${i}</td>${MENU}</tr>`,
      ),
    ];
    root.innerHTML = `<table><tbody>${rows.join("")}</tbody></table>`;
  }

  function scopeRoot(): HTMLElement {
    const root = document.createElement("div");
    root.className = SCOPE.slice(1);
    document.body.append(root);
    return root;
  }

  const census = () => censusOfBody({ scope: SCOPE, dataSelector: dataRowSelector(SCOPE) });

  it("воспроизведение: ЧЕТЫРЕ отдельных чтения дают противоречивую тройку", () => {
    const root = scopeRoot();
    fill(root, 48);

    // Ровно тот порядок, что был у прежней редакции пробы, и ровно та
    // перерисовка, что видна в трассе прогона: список ушёл в загрузку между
    // контролем и утверждением и вернулся до следующих чтений.
    const control = document.querySelectorAll(dataRowSelector(SCOPE)).length;
    fill(root, 0);
    const dataRows = document.querySelectorAll(dataRowSelector(SCOPE)).length;
    fill(root, 48);
    const bodyRows = document.querySelectorAll(`${SCOPE} tbody tr`).length;
    const menus = document.querySelectorAll(`${SCOPE} tbody .anticon-more`).length;

    // Контроль пройден — и всё же утверждение сравнивает 48 с нулём.
    expect(control).toBeGreaterThan(0);
    expect([dataRows, bodyRows, menus]).toEqual([0, 49, 48]);
    expect(menus === dataRows).toBe(false);
  });

  it("одна перепись противоречивой тройки не даёт НИ ПРИ КАКОМ порядке подмен", () => {
    const root = scopeRoot();

    for (const dataCount of [48, 0, 3, 0, 1]) {
      fill(root, dataCount);
      const c = census();
      // Несущее: меню ровно столько же, сколько строк данных, потому что оба
      // числа сняты одним обходом. Прежняя форма это нарушала (проба выше:
      // 0 строк данных при 48 меню).
      expect(c.menus).toBe(c.dataRows);
      expect(c.dataRows).toBe(dataCount);
      expect(c.bodyRows).toBe(dataCount === 0 ? 2 : dataCount + 1);
      expect(c.checkboxes).toBe(0);
    }
  });

  it("положительный контроль НЕСУЩИЙ: на пустом теле «строк в теле» есть, а строк данных нет", () => {
    const root = scopeRoot();
    fill(root, 0);
    const c = census();

    // Слабый контроль («в теле есть строки») пустая таблица ВЫПОЛНЯЕТ — и все
    // отрицания пробы становятся верны by construction. Сильный («есть строки
    // данных») — не выполняет. Без этой пары «форма без содержания»
    // неотличима от исправной работы.
    expect(c.bodyRows).toBeGreaterThan(0);
    expect(c.dataRows).toBe(0);
    expect(c.menus).toBe(0);
  });

  it("дозагруженность: занятой считается область НАД ТАБЛИЦЕЙ, а не любая на странице", () => {
    const root = scopeRoot();
    fill(root, 3);
    expect(census().settled).toBe(true);

    // Соседний занятой виджет таблицы не накрывает — и признак не гасит.
    // Без этого сужения проба краснела бы там, где список давно готов.
    const elsewhere = document.createElement("div");
    elsewhere.setAttribute("aria-busy", "true");
    root.append(elsewhere);
    expect(census().settled).toBe(true);

    // А та же пометка НАД таблицей — гасит.
    const table = root.querySelector("table") as HTMLElement;
    const over = document.createElement("div");
    over.setAttribute("aria-busy", "true");
    table.replaceWith(over);
    over.append(table);
    expect(census().settled).toBe(false);
  });

  it("перепись: объём осмотренного назван", () => {
    const root = scopeRoot();
    fill(root, 5);
    const c = census();
    // eslint-disable-next-line no-console
    console.log(
      `осмотрено: тел таблицы 1; строк в теле ${c.bodyRows}, из них данных ${c.dataRows}, ` +
        `служебных ${c.bodyRows - c.dataRows}; меню ${c.menus}; флажков ${c.checkboxes}; ` +
        `дозагружена ${c.settled}`,
    );
    expect(c.dataRows).toBe(5);
  });
});
