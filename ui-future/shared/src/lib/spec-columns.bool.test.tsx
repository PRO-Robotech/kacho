// Булево значение в КОЛОНКЕ списка.
//
// Правило 6 модуля `ui.md`: булево свойство называется СЛЕДСТВИЕМ, а не словом
// «Да». В карточке это правило держит атом `BoolFact`, и он там применяется. В
// колонке списка держать было нечем: словарь форматов булева не знал вовсе,
// поэтому колонка попадала в ветку умолчания и печатала пользователю `true` —
// служебное слово чужого языка вместо факта о ресурсе.
//
// Producer у этого дефекта был живой и в самом ОБЩЕМ реестре: колонка «По
// умолчанию» типа диска объявлена `format:"text"` над булевым полем. То есть
// класс лечится не только для форков — общий печатал `true` наравне с ними.
//
// Отрицание здесь всегда в паре с положительным: «не печатает true» зеленело бы
// и на пустой ячейке.

import { render, screen } from "@testing-library/react";
import type { ResourceColumn } from "./resource-spec";
import { formatCellByFormat } from "./spec-columns";

const col = (over: Partial<ResourceColumn> = {}): ResourceColumn => ({
  header: "По умолчанию",
  path: "is_default",
  format: "bool",
  ...over,
});

describe("формат колонки «логическое»", () => {
  it("истина названа следствием, а не словом «Да»", () => {
    render(<div>{formatCellByFormat(col({ boolLabels: { yes: "Тип по умолчанию", no: "Обычный тип" } }), { is_default: true })}</div>);

    expect(screen.getByText("Тип по умолчанию")).toBeInTheDocument();
    expect(screen.queryByText("Да")).not.toBeInTheDocument();
    expect(screen.queryByText("true")).not.toBeInTheDocument();
  });

  it("ложь названа своим следствием, а не прочерком и не словом «Нет»", () => {
    // Ложь — это ФАКТ о ресурсе, а не отсутствие данных. Прочерк на её месте
    // читается как «сервер не ответил».
    render(<div>{formatCellByFormat(col({ boolLabels: { yes: "Тип по умолчанию", no: "Обычный тип" } }), { is_default: false })}</div>);

    expect(screen.getByText("Обычный тип")).toBeInTheDocument();
    expect(screen.queryByText("Нет")).not.toBeInTheDocument();
    expect(screen.queryByText("false")).not.toBeInTheDocument();
  });

  it("без объявленных подписей формат не выдумывает «Да»/«Нет»", () => {
    // Подписи — часть контракта колонки: назвать следствие может только тот, кто
    // знает предмет. Умолчание «Да»/«Нет» вернуло бы ровно тот дефект, ради
    // которого формат заведён, — только теперь объявленным и потому незаметным.
    render(<div data-testid="cell">{formatCellByFormat(col({ boolLabels: undefined }), { is_default: true })}</div>);

    expect(screen.queryByText("Да")).not.toBeInTheDocument();
    expect(screen.queryByText("true")).not.toBeInTheDocument();
  });

  it("значения нет вовсе — прочерк, а не «ложь» (положительный контроль)", () => {
    // Отсутствие поля и ложь — разные утверждения: первое про ответ сервера,
    // второе про ресурс. Печатать «Обычный тип» там, где сервер поля не прислал,
    // значило бы утверждать за него.
    render(
      <div data-testid="cell">{formatCellByFormat(col({ boolLabels: { yes: "Тип по умолчанию", no: "Обычный тип" } }), {})}</div>,
    );

    expect(screen.queryByText("Обычный тип")).not.toBeInTheDocument();
    expect(screen.getByTestId("cell")).toHaveTextContent("—");
  });

  it("текстовый формат по-прежнему печатает строку (контроль соседней ветки)", () => {
    render(<div>{formatCellByFormat(col({ format: "text", path: "name" }), { name: "gp2" })}</div>);
    expect(screen.getByText("gp2")).toBeInTheDocument();
  });
});
