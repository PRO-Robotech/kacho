// Таблица списка ресурсов. Два её собственных решения:
//
//   * строка кликабельна и ведёт на карточку — НО клик внутри кнопки, ссылки
//     или поля ввода строку не задевает. Иначе нажатие на меню действий уводит
//     на карточку раньше, чем откроется диалог: операция становится
//     недостижимой;
//   * пустая таблица говорит об этом словами вызывающего, а не молчит.

import { jest } from "@jest/globals";
import { fireEvent, render, screen } from "@testing-library/react";
import { ResourceTable, type Column } from "./ResourceTable";

interface Row {
  id: string;
  name: string;
}

const columns: Column<Row>[] = [
  { header: "Имя", cell: (r) => <span>{r.name}</span>, sortKey: "name" },
  {
    header: "Действия",
    cell: () => (
      <button type="button" onClick={(e) => e.stopPropagation()}>
        Меню
      </button>
    ),
  },
];

const rows: Row[] = [
  { id: "sub-1", name: "frontend" },
  { id: "sub-2", name: "backend" },
];

function renderTable(over: Partial<Parameters<typeof ResourceTable<Row>>[0]> = {}) {
  const onRowClick = jest.fn<(row: Row) => void>();
  render(<ResourceTable rows={rows} columns={columns} rowKey={(r) => r.id} onRowClick={onRowClick} {...over} />);
  return { onRowClick };
}

describe("ResourceTable", () => {
  it("рисует шапку и по строке на ресурс", () => {
    renderTable();

    expect(screen.getByText("Имя")).toBeInTheDocument();
    expect(screen.getByText("frontend")).toBeInTheDocument();
    expect(screen.getByText("backend")).toBeInTheDocument();
  });

  it("клик по строке ведёт на её ресурс", () => {
    const { onRowClick } = renderTable();

    fireEvent.click(screen.getByText("frontend"));

    expect(onRowClick).toHaveBeenCalledWith({ id: "sub-1", name: "frontend" });
  });

  it("клик по кнопке внутри строки на карточку НЕ уводит", () => {
    const { onRowClick } = renderTable();

    fireEvent.click(screen.getAllByRole("button", { name: "Меню" })[0]);

    expect(onRowClick).not.toHaveBeenCalled();
  });

  it("без обработчика строка кликом ничего не делает", () => {
    render(<ResourceTable rows={rows} columns={columns} rowKey={(r) => r.id} />);
    const row = screen.getByText("frontend").closest("tr")!;
    expect(row.style.cursor).toBe("");
  });

  it("пустой список объясняет себя словами вызывающего", () => {
    const { unmount } = render(<ResourceTable rows={[]} columns={columns} rowKey={(r: Row) => r.id} />);
    expect(screen.getByText("Ресурсов не найдено")).toBeInTheDocument();
    unmount();

    render(
      <ResourceTable rows={[]} columns={columns} rowKey={(r: Row) => r.id} empty={<span>Подсетей пока нет</span>} />,
    );
    expect(screen.getByText("Подсетей пока нет")).toBeInTheDocument();
  });

  it("ключ строки берётся у вызывающего", () => {
    // Ключ по индексу перемешал бы состояние строк при сортировке и обновлении
    // списка — раскрытая строка «переехала» бы на соседний ресурс.
    const rowKey = jest.fn((r: Row) => r.id);
    render(<ResourceTable rows={rows} columns={columns} rowKey={rowKey} />);
    expect(rowKey).toHaveBeenCalledWith(rows[0]);
    expect(rowKey).toHaveBeenCalledWith(rows[1]);
  });
});
