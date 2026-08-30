// Ячейка над НЕскалярным значением — против того, что видит человек.
//
// Предмет: у ресурсов Kachō поле строки ответа бывает вложенным объектом
// (`internal_ipv4_address`), картой (`labels`) или списком. Ветка "text" (она
// же default) отсеивает только `null`/`undefined`/пустую строку, поэтому
// объект доезжает до приведения к строке. `String(объект)` даёт
// `[object Object]` — ОДИН И ТОТ ЖЕ текст для любого объекта: ячейка, подстрока
// поиска и ключ сортировки перестают различать значения вообще, а человек
// видит слово, из которого ничего не следует.
//
// Решение об этом принято ОДИН раз — `@shared/lib/display-text`; ветка колонки
// обязана звать его, а не приводить значение своим способом.

import { render } from "@testing-library/react";
import type { ReactNode } from "react";

import { formatCellByFormat } from "./spec-columns";

function textOf(node: ReactNode): string {
  return render(<div>{node}</div>).container.textContent ?? "";
}

/** Ячейка колонки формата `text` (он же default) над значением `v`. */
function cell(v: unknown, format?: "text"): string {
  return textOf(formatCellByFormat({ header: "Значение", path: "value", format }, { value: v }));
}

describe("нескалярное значение в ячейке показывает СОСТАВ, а не [object Object]", () => {
  it("вложенный объект", () => {
    const shown = cell({ address: "10.0.0.4", subnet_id: "sub-1" });
    expect(shown).not.toContain("[object Object]");
    expect(shown).toContain("10.0.0.4");
  });

  it("список объектов в ветке text", () => {
    const shown = cell([{ key: "env", value: "prod" }], "text");
    expect(shown).not.toContain("[object Object]");
    expect(shown).toContain("prod");
  });

  // Тот же класс с другой стороны: у формата `list` каждый ЭЛЕМЕНТ приводился к
  // строке своим способом, поэтому список объектов печатался столбцом
  // одинаковых `[object Object]`. Чиним класс, а не то место, где он замечен.
  it("список объектов в ветке list", () => {
    const shown = textOf(
      formatCellByFormat({ header: "Список", path: "value", format: "list" }, { value: [{ cidr: "10.0.0.0/24" }] }),
    );
    expect(shown).not.toContain("[object Object]");
    expect(shown).toContain("10.0.0.0/24");
  });

  it("список строк остаётся собой", () => {
    const shown = textOf(
      formatCellByFormat(
        { header: "Список", path: "value", format: "list" },
        { value: ["10.0.0.0/24", "10.1.0.0/24"] },
      ),
    );
    expect(shown).toContain("10.0.0.0/24");
    expect(shown).toContain("10.1.0.0/24");
    expect(shown).not.toContain("[object Object]");
  });

  // Положительные контроли: сведение к общему решению не трогает скаляры и не
  // отнимает у пустого значения его прочерк. Без них проба «не содержит
  // [object Object]» зеленела бы и на ячейке, которая не показывает НИЧЕГО.
  it("скаляр остаётся собой", () => {
    expect(cell("net-42", "text")).toBe("net-42");
    expect(cell(7)).toBe("7");
  });

  it("пустое значение остаётся прочерком", () => {
    expect(cell(null, "text")).toBe("—");
    expect(cell("")).toBe("—");
  });
});
