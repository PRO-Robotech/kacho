// Редактор статических маршрутов формы создания таблицы маршрутизации.
// Собственной разметки не несёт — он ПЕРЕВОДЧИК между парами общего редактора и
// записями маршрута. Переводчик, перепутавший стороны, даёт форму, которая
// выглядит рабочей и отправляет префикс в поле следующего узла.

import { jest } from "@jest/globals";
import { fireEvent, render, screen } from "@testing-library/react";
import { RoutesEditor, type RouteEntry } from "./RoutesEditor";

describe("RoutesEditor", () => {
  it("показывает маршрут в своих колонках", () => {
    render(
      <RoutesEditor value={[{ destination_prefix: "10.0.0.0/24", next_hop_address: "10.0.0.1" }]} onChange={() => {}} />,
    );

    expect(screen.getByText("Префикс назначения")).toBeInTheDocument();
    expect(screen.getByText("Следующий узел")).toBeInTheDocument();
    expect(screen.getByDisplayValue("10.0.0.0/24")).toBeInTheDocument();
    expect(screen.getByDisplayValue("10.0.0.1")).toBeInTheDocument();
  });

  it("правка левой колонки меняет префикс, а не следующий узел", () => {
    const onChange = jest.fn<(next: RouteEntry[]) => void>();
    render(
      <RoutesEditor value={[{ destination_prefix: "10.0.0.0/24", next_hop_address: "10.0.0.1" }]} onChange={onChange} />,
    );

    fireEvent.change(screen.getByDisplayValue("10.0.0.0/24"), { target: { value: "10.1.0.0/16" } });

    expect(onChange).toHaveBeenCalledWith([{ destination_prefix: "10.1.0.0/16", next_hop_address: "10.0.0.1" }]);
  });

  it("правка правой колонки меняет следующий узел", () => {
    const onChange = jest.fn<(next: RouteEntry[]) => void>();
    render(
      <RoutesEditor value={[{ destination_prefix: "10.0.0.0/24", next_hop_address: "10.0.0.1" }]} onChange={onChange} />,
    );

    fireEvent.change(screen.getByDisplayValue("10.0.0.1"), { target: { value: "10.0.0.254" } });

    expect(onChange).toHaveBeenCalledWith([{ destination_prefix: "10.0.0.0/24", next_hop_address: "10.0.0.254" }]);
  });

  it("новая строка приходит пустыми полями маршрута, а не парой ключ-значение", () => {
    const onChange = jest.fn<(next: RouteEntry[]) => void>();
    render(<RoutesEditor value={[]} onChange={onChange} />);

    fireEvent.click(screen.getByRole("button", { name: "Добавить маршрут" }));

    expect(onChange).toHaveBeenCalledWith([{ destination_prefix: "", next_hop_address: "" }]);
  });

  it("выключенный редактор ничего не отдаёт", () => {
    const onChange = jest.fn();
    render(
      <RoutesEditor
        value={[{ destination_prefix: "10.0.0.0/24", next_hop_address: "10.0.0.1" }]}
        onChange={onChange}
        disabled
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "Добавить маршрут" }));

    expect(onChange).not.toHaveBeenCalled();
  });
});
