// Гейт: сквозная проба консоли отличает СТРОКУ ДАННЫХ от устройства таблицы.
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
// # Что утверждается — ТРИ вещи, и ни одна не выводится из двух других
//
//   1. ПРЕДПОСЫЛКА. Установленная таблица рисует служебные строки именно теми
//      признаками, которыми проба их исключает. Признаки живут в чужом пакете и
//      меняются вместе с его версией; без этой проверки исключение перестанет
//      срабатывать МОЛЧА, и контроль вернётся к прежней пустоте.
//   2. ОБЪЯВЛЕНИЕ. Проба исключает обе формы и не считает строки данных сырым
//      `tbody tr`.
//   3. ПРЕДИКАТ. Селектор, взятый ИЗ САМОЙ ПРОБЫ, даёт 0 на теле пустой таблицы
//      и N на теле из N строк. Это контроль в обе стороны: без первой половины
//      гейт зеленел бы на селекторе, не отсекающем ничего; без второй — на
//      селекторе, не находящем ничего.
//
// Селектор НЕ переписывается здесь копией: он извлекается из объявления пробы.
// Две копии одного предиката разошлись бы молча — и разошлись бы ровно там, где
// расхождение не видно, потому что обе отвечают одинаково на исправном дереве.

import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const UI_ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../..");
const PROBE = path.join(UI_ROOT, "e2e/specs/console-forms.spec.ts");
const TABLE_PKG = path.join(UI_ROOT, "node_modules/@rc-component/table/es");
const MEASURE_ROW = path.join(TABLE_PKG, "Body/MeasureRow.js");
const BODY = path.join(TABLE_PKG, "Body/index.js");

/** Селектор строки данных — ИЗ пробы, а не отсюда. */
function declaredDataRowSelector(): string {
  const src = readFileSync(PROBE, "utf8");
  const m = /^const DATA_ROW = '([^']+)';$/m.exec(src);
  if (!m) {
    throw new Error(
      `в ${path.relative(UI_ROOT, PROBE)} нет объявления \`const DATA_ROW = '…';\`. ` +
        `Гейт судит предикат ПРОБЫ; без объявления судить нечего, и молчание здесь ` +
        `означало бы «проверено», а не «нечего проверять»`,
    );
  }
  return m[1];
}

/** Тело таблицы: служебные строки плюс `dataCount` строк данных. */
function tbodyWith(dataCount: number): HTMLElement {
  const root = document.createElement("div");
  root.className = "app-main";
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

describe("проба консоли отличает строку данных от устройства таблицы (#1070)", () => {
  it("предпосылка: установленная таблица рисует строку обмера тем признаком, которым проба её исключает", () => {
    const src = readFileSync(MEASURE_ROW, "utf8");
    // Утверждаются ОБА признака сразу и одним значением: разойдись они, отчёт
    // обязан назвать, какой именно перестал существовать. Имя файла входит в
    // сравниваемое значение — иначе «не нашли» было бы неотличимо от «читали
    // не тот файл».
    expect([
      `файл: ${path.relative(UI_ROOT, MEASURE_ROW)}`,
      `скрыта от доступности: ${/"aria-hidden":\s*"true"/.test(src)}`,
      `несёт свой класс: ${/`\$\{prefixCls\}-measure-row`/.test(src)}`,
    ]).toEqual([
      `файл: ${path.relative(UI_ROOT, MEASURE_ROW)}`,
      "скрыта от доступности: true",
      "несёт свой класс: true",
    ]);
  });

  it("предпосылка: пустой набор рисует строку-заполнитель тем признаком, которым проба её исключает", () => {
    const src = readFileSync(BODY, "utf8");
    expect([
      `файл: ${path.relative(UI_ROOT, BODY)}`,
      `несёт свой класс: ${/`\$\{prefixCls\}-placeholder`/.test(src)}`,
      `рисуется только на пустом наборе: ${/isEmpty:\s*true/.test(src)}`,
    ]).toEqual([
      `файл: ${path.relative(UI_ROOT, BODY)}`,
      "несёт свой класс: true",
      "рисуется только на пустом наборе: true",
    ]);
  });

  it("объявление: проба исключает ОБЕ формы и не считает строки данных сырым `tbody tr`", () => {
    const selector = declaredDataRowSelector();
    expect([
      `исключает строку обмера: ${selector.includes(':not([aria-hidden="true"])')}`,
      `исключает заполнитель: ${selector.includes(":not(.ant-table-placeholder)")}`,
    ]).toEqual(["исключает строку обмера: true", "исключает заполнитель: true"]);
  });

  it("предикат: на пустой таблице строк данных НОЛЬ, хотя `<tr>` в теле есть", () => {
    const selector = declaredDataRowSelector();
    const root = tbodyWith(0);
    // Контроль предпосылки самого гейта: если бы синтетика не несла служебных
    // строк, «ноль строк данных» вышло бы из пустоты, а не из исключения.
    expect(root.querySelectorAll("tbody tr").length).toBe(2);
    expect(document.querySelectorAll(selector).length).toBe(0);
  });

  it("предикат: на таблице из трёх строк строк данных ТРИ — исключение не съедает данные", () => {
    const selector = declaredDataRowSelector();
    const root = tbodyWith(3);
    expect(root.querySelectorAll("tbody tr").length).toBe(4);
    expect(document.querySelectorAll(selector).length).toBe(3);
  });

  it("перепись: объём осмотренного назван, чтобы «ноль находок» было отличимо от «ноль прочитанного»", () => {
    const selector = declaredDataRowSelector();
    const probe = readFileSync(PROBE, "utf8");
    const rawBodyRowCounts = probe.split("\n").filter((l) => /locator\(["'`][^"'`]*tbody tr["'`]\)/.test(l)).length;
    // Сырой счёт строк тела в пробе законен РОВНО в одном месте — в сообщении,
    // которое называет, сколько строк было служебными. Больше одного означает,
    // что счёт вернулся в контроль.
    // eslint-disable-next-line no-console
    console.log(
      `осмотрено: селектор пробы «${selector}»; ` +
        `мест сырого счёта \`tbody tr\` в пробе — ${rawBodyRowCounts}; ` +
        `файлов установленной таблицы прочитано — 2`,
    );
    expect(rawBodyRowCounts).toBeLessThanOrEqual(1);
  });
});
