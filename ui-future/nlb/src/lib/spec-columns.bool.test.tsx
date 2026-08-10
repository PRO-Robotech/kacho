// Колонка над логическим полем — против того, что видит человек в таблице.
//
// Предмет: словарь форматов колонок логического варианта не несёт. Ветка "text"
// (и она же default) отсеивает только `null`/`undefined`/пустую строку, поэтому
// логическое `false` доезжает до `String(v)` и печатается литералом «false» —
// строкой чужого языка в русском интерфейсе, неотличимой на глаз от значения-
// строки. И это ровно тот случай, ради которого колонка заводится: регион,
// закрытый для размещения.
//
// Ground truth поля: proto/kacho/cloud/geo/v1/region.proto — `bool
// open_for_placement = 5`, публичная проекция.
//
// ГДЕ ЭТО РИСУЕТСЯ. Список регионов рисует ОБЩИЙ `ResourceListPage`, а он
// строит ячейки общим `@shared/lib/spec-columns`. Проба берёт ОБА сборщика —
// локальный и общий — и утверждает про КАЖДЫЙ одно и то же наблюдаемое: что
// показано в ячейке. Прежняя редакция вместо этого читала исходник общего
// рендера и искала в нём подстроки-образцы; такое утверждение говорит о
// символах файла, переживает любую перепись той же логики и не может упасть на
// изменении того, что человек видит.

import { render } from "@testing-library/react";
import type { ReactNode } from "react";

import { buildSpecColumns as buildShared } from "@shared/lib/spec-columns";

import { REGISTRY } from "./resource-registry";
import { buildSpecColumns } from "./spec-columns";

function textOf(node: ReactNode): string {
  return render(<div>{node}</div>).container.textContent ?? "";
}

type Builder = typeof buildSpecColumns;

function cellTextWith(build: Builder, header: RegExp, row: Record<string, unknown>): string {
  const col = build(REGISTRY["compute-regions"]).find((c) => header.test(c.header));
  if (!col) throw new Error(`колонки ${header} нет в списке регионов`);
  return textOf(col.cell(row));
}

/** Оба сборщика — локальный этого пакета и общий, которым рисует список. */
const BUILDERS: Array<[string, Builder]> = [
  ["локальный", buildSpecColumns],
  ["общий", buildShared as Builder],
];

const cellText = (header: RegExp, row: Record<string, unknown>) =>
  cellTextWith(buildSpecColumns, header, row);

describe("логическая колонка в списке регионов", () => {
  it.each(BUILDERS)("%s сборщик рисует закрытый регион одинаково", (_name, build) => {
    // Прежде это была «предпосылка», прочитанная из исходника общего рендера.
    // Теперь утверждается наблюдаемое у ОБОИХ: разъедутся — покраснеет тот, кто
    // разошёлся, и будет названо, какой именно.
    const text = cellTextWith(build, /размещ/i, { open_for_placement: false });
    expect(text).not.toContain("false");
    expect(text).not.toBe("");
  });

  it("предпосылка: колонка над логическим полем в списке есть", () => {
    expect(REGISTRY["compute-regions"].columns.some((c) => c.path === "open_for_placement")).toBe(true);
  });

  it("закрытый регион не печатает литерал из чужого языка", () => {
    const text = cellText(/размещ/i, { open_for_placement: false });
    expect(text).not.toContain("false");
    expect(text).not.toBe("");
  });

  it("открытый регион отличим от закрытого", () => {
    // Положительный близнец отрицания выше: запрет на «false» тривиально
    // выполняется пустой ячейкой, поэтому оба состояния обязаны быть РАЗНЫМИ и
    // непустыми.
    const yes = cellText(/размещ/i, { open_for_placement: true });
    const no = cellText(/размещ/i, { open_for_placement: false });
    expect(yes).not.toBe("");
    expect(yes).not.toBe(no);
    expect(yes).not.toContain("true");
  });

  it("отсутствующее значение остаётся прочерком, а не «нет»", () => {
    // Отсутствие и «ложь» — разные состояния: сервер, не приславший поле, не
    // утверждает, что регион закрыт.
    expect(cellText(/размещ/i, {})).toBe("—");
  });

  it("текстовая колонка соседней формы не задета", () => {
    // Контроль в другую сторону: правка логической ячейки не должна менять
    // рендер обычной строки тем же путём.
    expect(cellText(/идентификатор/i, { id: "reg-abc" })).toBe("reg-abc");
  });
});
