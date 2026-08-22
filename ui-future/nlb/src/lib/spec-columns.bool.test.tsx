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
// строит ячейки общим `@shared/lib/spec-columns`. Прежде их было ДВА — общий и
// форк этого модуля, — и проба сверяла показанное у каждого, чтобы расхождение
// не приземлилось молча. Форк снят: сборщик один, и первое же утверждение ниже
// требует именно ТОЖДЕСТВА. Оно способно упасть — вернуть в `./spec-columns`
// собственное тело, и проба покраснеет с именем предмета; остальные утверждения
// продолжают говорить о том, что человек видит в ячейке.
//
// Прежняя редакция вместо этого читала исходник общего рендера и искала в нём
// подстроки-образцы; такое утверждение говорит о символах файла, переживает
// любую перепись той же логики и не может упасть на изменении того, что человек
// видит.

import { render } from "@testing-library/react";
import type { ReactNode } from "react";

import { buildSpecColumns as buildShared } from "@shared/lib/spec-columns";

import { REGISTRY } from "./resource-registry";
import { buildSpecColumns } from "./spec-columns";

function textOf(node: ReactNode): string {
  return render(<div>{node}</div>).container.textContent ?? "";
}

function cellText(header: RegExp, row: Record<string, unknown>): string {
  const col = buildSpecColumns(REGISTRY["compute-regions"]).find((c) => header.test(c.header));
  if (!col) throw new Error(`колонки ${header} нет в списке регионов`);
  return textOf(col.cell(row));
}

describe("логическая колонка в списке регионов", () => {
  it("сборщик этого модуля — ТОТ ЖЕ, которым рисует список", () => {
    // Два сборщика об одном предмете расходятся молча, и расхождение видно
    // только на живой странице. Здесь оно наблюдаемо: форк вернётся — упадёт
    // это утверждение, а не пользователь.
    expect(buildSpecColumns).toBe(buildShared);
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

  it("оба исхода названы СЛЕДСТВИЕМ, а не «Да»/«Нет» (правило 6 ui.md)", () => {
    // «Да» отвечает на вопрос, которого пользователь не задавал: рядом с
    // заголовком «Открыт для размещения» оно не говорит ни что размещение
    // доступно, ни что регион закрыт. Пока общий словарь логического варианта
    // не нёс, ячейка держала свой «Да»/«Нет»; теперь предмета у него нет.
    const yes = cellText(/размещ/i, { open_for_placement: true });
    const no = cellText(/размещ/i, { open_for_placement: false });
    expect(yes).toMatch(/размещение доступно/i);
    expect(no).toMatch(/размещение закрыто/i);
    expect(yes).not.toMatch(/^\s*Да\s*$/);
    expect(no).not.toMatch(/^\s*Нет\s*$/);
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
