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
// ГДЕ ЭТО РИСУЕТСЯ. Список регионов рисует ОБЩИЙ `ResourceListPage`
// (`@shared/components/organisms/ResourceListPage` — локальный модуль этого
// пакета его ре-экспортирует), а он строит ячейки общим
// `@shared/lib/spec-columns`. Импортировать общий модуль сюда нельзя: у
// `ui-future/shared` нет собственных node_modules, и его компоненты из тестов
// этого пакета не резолвятся (падает вся суита, сообщением не про себя).
// Поэтому проба идёт через локальный `buildSpecColumns` — у него ТОТ ЖЕ
// контракт, — а совпадение контракта с общим утверждается отдельно, чтением
// исходного файла общего рендера. Разъедутся — предпосылка покраснеет, и это
// будет видно, а не молча.

import { render } from "@testing-library/react";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import type { ReactNode } from "react";

import { REGISTRY } from "./resource-registry";
import { buildSpecColumns } from "./spec-columns";

const SHARED_SPEC_COLUMNS = join(process.cwd(), "..", "shared", "src", "lib", "spec-columns.tsx");

function textOf(node: ReactNode): string {
  return render(<div>{node}</div>).container.textContent ?? "";
}

function cellText(header: RegExp, row: Record<string, unknown>): string {
  const col = buildSpecColumns(REGISTRY["compute-regions"]).find((c) => header.test(c.header));
  if (!col) throw new Error(`колонки ${header} нет в списке регионов`);
  return textOf(col.cell(row));
}

describe("логическая колонка в списке регионов", () => {
  it("предпосылка: общий рендер списков уважает собственный render колонки", () => {
    // Без этого проба утверждала бы про рендер, который эту страницу не рисует.
    const shared = readFileSync(SHARED_SPEC_COLUMNS, "utf8");
    expect(shared).toMatch(/c\.render\s*\?\s*c\.render\(row\)/);
    // И вторая половина того же факта: ветка "text"/default общего рендера
    // логическое значение не различает — именно поэтому колонка несёт render.
    expect(shared).toMatch(/case "text":\s*\n\s*default:/);
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
