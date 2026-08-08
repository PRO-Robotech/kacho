// Редактор пар «ключ — значение»: на нём собраны метки ресурса и статические
// маршруты. Он управляемый — собственного состояния не держит и обязан отдавать
// вызывающему ПОЛНЫЙ следующий набор строк на каждое действие. Ошибка в индексе
// (правка не той строки, удаление соседней) видима только по данным, которые
// уедут в запрос: сам виджет при этом выглядит исправным.

import { jest } from "@jest/globals";
import { fireEvent, render, screen } from "@testing-library/react";
import { EditableKVTable, type KVRow } from "./EditableKVTable";

const colA = { header: "Ключ", placeholder: "env" };
const colB = { header: "Значение", placeholder: "prod" };

function renderTable(rows: KVRow[], onChange: (rows: KVRow[]) => void, disabled?: boolean) {
  return render(
    <EditableKVTable
      rows={rows}
      onChange={onChange}
      colA={colA}
      colB={colB}
      addLabel="Добавить метку"
      disabled={disabled}
    />,
  );
}

describe("EditableKVTable", () => {
  it("рисует заголовки колонок и по строке на пару", () => {
    renderTable(
      [
        { a: "env", b: "prod" },
        { a: "team", b: "net" },
      ],
      () => {},
    );

    expect(screen.getByText("Ключ")).toBeInTheDocument();
    expect(screen.getByText("Значение")).toBeInTheDocument();
    expect(screen.getAllByDisplayValue("env")).toHaveLength(1);
    expect(screen.getAllByDisplayValue("prod")).toHaveLength(1);
    expect(screen.getAllByLabelText("Удалить строку")).toHaveLength(2);
  });

  it("пустой набор рисует без строк, но с кнопкой добавления", () => {
    renderTable([], () => {});
    expect(screen.queryAllByLabelText("Удалить строку")).toHaveLength(0);
    expect(screen.getByRole("button", { name: "Добавить метку" })).toBeInTheDocument();
  });

  it("правит ровно ту строку, в которой печатают", () => {
    const onChange = jest.fn<(rows: KVRow[]) => void>();
    renderTable(
      [
        { a: "env", b: "prod" },
        { a: "team", b: "net" },
      ],
      onChange,
    );

    fireEvent.change(screen.getByDisplayValue("team"), { target: { value: "owner" } });

    expect(onChange).toHaveBeenCalledWith([
      { a: "env", b: "prod" },
      { a: "owner", b: "net" },
    ]);
  });

  it("правка значения не задевает ключа той же строки", () => {
    const onChange = jest.fn<(rows: KVRow[]) => void>();
    renderTable([{ a: "env", b: "prod" }], onChange);

    fireEvent.change(screen.getByDisplayValue("prod"), { target: { value: "stage" } });

    expect(onChange).toHaveBeenCalledWith([{ a: "env", b: "stage" }]);
  });

  it("удаляет ровно нажатую строку", () => {
    const onChange = jest.fn<(rows: KVRow[]) => void>();
    renderTable(
      [
        { a: "env", b: "prod" },
        { a: "team", b: "net" },
        { a: "tier", b: "web" },
      ],
      onChange,
    );

    fireEvent.click(screen.getAllByLabelText("Удалить строку")[1]);

    expect(onChange).toHaveBeenCalledWith([
      { a: "env", b: "prod" },
      { a: "tier", b: "web" },
    ]);
  });

  it("добавляет пустую пару в конец, не трогая существующие", () => {
    const onChange = jest.fn<(rows: KVRow[]) => void>();
    renderTable([{ a: "env", b: "prod" }], onChange);

    fireEvent.click(screen.getByRole("button", { name: "Добавить метку" }));

    expect(onChange).toHaveBeenCalledWith([
      { a: "env", b: "prod" },
      { a: "", b: "" },
    ]);
  });

  it("строки-дубликаты по ключу различимы и правятся по отдельности", () => {
    // Индекс, а не ключ, — единственный способ адресовать строку, пока
    // пользователь набирает: два пустых ключа существуют совершенно законно.
    const onChange = jest.fn<(rows: KVRow[]) => void>();
    renderTable(
      [
        { a: "", b: "" },
        { a: "", b: "" },
      ],
      onChange,
    );

    const keyInputs = screen.getAllByPlaceholderText("env");
    fireEvent.change(keyInputs[1], { target: { value: "team" } });

    expect(onChange).toHaveBeenCalledWith([
      { a: "", b: "" },
      { a: "team", b: "" },
    ]);
  });

  it("в выключенном состоянии не даёт ни править, ни удалять, ни добавлять", () => {
    const onChange = jest.fn<(rows: KVRow[]) => void>();
    renderTable([{ a: "env", b: "prod" }], onChange, true);

    expect(screen.getByDisplayValue("env")).toBeDisabled();
    expect(screen.getByLabelText("Удалить строку")).toBeDisabled();
    expect(screen.getByRole("button", { name: "Добавить метку" })).toBeDisabled();

    fireEvent.click(screen.getByLabelText("Удалить строку"));
    fireEvent.click(screen.getByRole("button", { name: "Добавить метку" }));
    expect(onChange).not.toHaveBeenCalled();
  });
});
